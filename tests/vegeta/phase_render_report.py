#!/usr/bin/env python3

from __future__ import annotations

import argparse
import html
import json
import math
import os
import re
from collections import Counter, defaultdict
from datetime import datetime
from pathlib import Path
from typing import Iterable


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Render a combined vegeta + pprof HTML dashboard.")
    parser.add_argument("--summary", required=True)
    parser.add_argument("--raw", required=True)
    parser.add_argument("--report", required=True)
    parser.add_argument("--metadata", required=True)
    parser.add_argument("--async-jobs", required=True)
    parser.add_argument("--phase-state", required=True)
    parser.add_argument("--pprof-dir", required=True)
    parser.add_argument("--goroutine", required=True)
    parser.add_argument("--heap-top", required=True)
    parser.add_argument("--heap-inuse-top", required=True)
    return parser.parse_args()


def read_json(path: str) -> dict:
    if not os.path.exists(path):
        return {}
    with open(path, "r", encoding="utf-8") as handle:
        return json.load(handle)


def read_text(path: str) -> str:
    if not os.path.exists(path):
        return ""
    with open(path, "r", encoding="utf-8", errors="replace") as handle:
        return handle.read()


def parse_time_bucket(raw_value: str) -> str:
    if not raw_value:
        return ""
    value = raw_value.replace("Z", "+00:00")
    try:
        parsed = datetime.fromisoformat(value)
    except ValueError:
        return raw_value[:19]
    return parsed.strftime("%H:%M:%S")


def percentile(values: list[float], percentile_value: float) -> float:
    if not values:
        return 0.0
    sorted_values = sorted(values)
    if len(sorted_values) == 1:
        return sorted_values[0]
    index = (len(sorted_values) - 1) * percentile_value / 100.0
    lower = math.floor(index)
    upper = math.ceil(index)
    if lower == upper:
        return sorted_values[int(index)]
    weight = index - lower
    return sorted_values[lower] + (sorted_values[upper] - sorted_values[lower]) * weight


def format_number(value: float | int) -> str:
    if isinstance(value, int):
        return f"{value:,}"
    if value >= 1000:
        return f"{value:,.2f}"
    if value >= 100:
        return f"{value:.1f}"
    if value >= 10:
        return f"{value:.2f}"
    return f"{value:.3f}"


def format_ms(value: float) -> str:
    return f"{value:.2f} ms"


def format_rate(value: float) -> str:
    return f"{value:.2f}/s"


def format_pct(value: float) -> str:
    return f"{value:.2f}%"


def metric_value(summary: dict, metric_name: str, key: str, default: float = 0.0) -> float:
    metric = summary.get("metrics", {}).get(metric_name, {})
    values = metric.get("values", {})
    try:
        return float(values.get(key, default))
    except (TypeError, ValueError):
        return default


# Counter metric names that represent async / operations work
OPERATION_COUNTERS = {
    "phase_users_created", "phase_homes_created", "phase_devices_enrolled",
    "phase_devices_deleted", "phase_homes_delete_requested",
    "phase_users_deleted_self", "phase_users_deleted_admin",
    "phase_google_oauth_authorized", "phase_google_oauth_token_exchanged",
    "phase_google_oauth_enrolled",
    "phase_google_signin_created", "phase_google_signin_enrolled",
}
ASYNC_TREND_METRICS = {"async_home_ready_duration", "async_home_ready_polls"}
ASYNC_COUNTER_METRICS = {
    "async_home_ready_success", "async_home_ready_timeout",
    "async_home_ready_failed_state", "async_home_ready_early",
}

# Friendly display names for counters
COUNTER_DISPLAY = {
    "phase_users_created": "Users created",
    "phase_homes_created": "Homes created",
    "phase_devices_enrolled": "Devices enrolled",
    "phase_devices_deleted": "Devices deleted",
    "phase_homes_delete_requested": "Home deletes requested",
    "phase_users_deleted_self": "Users self-deleted",
    "phase_users_deleted_admin": "Users admin-deleted",
    "phase_google_oauth_authorized": "Google OAuth authorized",
    "phase_google_oauth_token_exchanged": "Google OAuth tokens exchanged",
    "phase_google_oauth_enrolled": "Google OAuth enrolled",
    "phase_google_signin_created": "Google Sign-In users created",
    "phase_google_signin_enrolled": "Google Sign-In enrolled",
}


# ---------------------------------------------------------------------------
# ScenarioData: per-scenario or overall aggregated raw metric data
# ---------------------------------------------------------------------------
class ScenarioData:
    def __init__(self) -> None:
        self.per_second: dict[str, dict] = defaultdict(
            lambda: {"requests": 0.0, "errors": 0.0, "duration_total": 0.0, "duration_count": 0}
        )
        self.endpoints: dict[str, dict] = defaultdict(
            lambda: {"count": 0, "errors": 0, "durations": [], "statuses": Counter()}
        )
        self.overall_statuses: Counter[str] = Counter()
        self.all_durations: list[float] = []
        self.total_errors: int = 0
        # Async readiness
        self.async_durations: list[float] = []
        self.async_polls: list[float] = []
        self.async_success: int = 0
        self.async_timeout: int = 0
        self.async_failed_state: int = 0
        self.async_early: int = 0
        # Operation counters per time bucket
        self.counters_per_second: dict[str, dict[str, float]] = defaultdict(lambda: defaultdict(float))
        # Total counters
        self.counter_totals: Counter[str] = Counter()

    def add_http_req(self, bucket: str, value: float) -> None:
        self.per_second[bucket]["requests"] += value

    def add_http_failed(self, bucket: str, value: float) -> None:
        self.per_second[bucket]["errors"] += value

    def add_http_duration(self, bucket: str, endpoint_name: str, duration: float, status: str, is_error: bool) -> None:
        self.per_second[bucket]["duration_total"] += duration
        self.per_second[bucket]["duration_count"] += 1
        ep = self.endpoints[endpoint_name]
        ep["count"] += 1
        ep["durations"].append(duration)
        self.all_durations.append(duration)
        ep["statuses"][status] += 1
        self.overall_statuses[status] += 1
        if is_error:
            ep["errors"] += 1
            self.total_errors += 1

    def add_async_duration(self, value: float) -> None:
        self.async_durations.append(value)

    def add_async_polls(self, value: float) -> None:
        self.async_polls.append(value)

    def add_async_counter(self, metric: str, value: float) -> None:
        if metric == "async_home_ready_success":
            self.async_success += int(value)
        elif metric == "async_home_ready_timeout":
            self.async_timeout += int(value)
        elif metric == "async_home_ready_failed_state":
            self.async_failed_state += int(value)
        elif metric == "async_home_ready_early":
            self.async_early += int(value)

    def add_operation_counter(self, bucket: str, metric: str, value: float) -> None:
        self.counters_per_second[metric][bucket] += value
        self.counter_totals[metric] += int(value)

    def build_series(self) -> tuple[list[tuple[str, float]], list[tuple[str, float]], list[tuple[str, float]]]:
        request_series: list[tuple[str, float]] = []
        latency_series: list[tuple[str, float]] = []
        error_series: list[tuple[str, float]] = []
        for bucket in sorted(self.per_second.keys()):
            d = self.per_second[bucket]
            request_series.append((bucket, d["requests"]))
            latency_avg = d["duration_total"] / d["duration_count"] if d["duration_count"] > 0 else 0.0
            latency_series.append((bucket, latency_avg))
            error_series.append((bucket, d["errors"]))
        return request_series, latency_series, error_series

    def build_counter_series(self, metric: str) -> list[tuple[str, float]]:
        buckets = self.counters_per_second.get(metric, {})
        return [(b, buckets[b]) for b in sorted(buckets.keys())]

    def active_counters(self) -> list[str]:
        return [m for m in sorted(self.counter_totals.keys()) if m in OPERATION_COUNTERS and self.counter_totals[m] > 0]

    def build_endpoint_rows(self) -> list[dict]:
        num_seconds = len(self.per_second) or 1
        rows: list[dict] = []
        for name, stats in self.endpoints.items():
            durations = stats["durations"]
            rows.append({
                "endpoint": name,
                "count": stats["count"],
                "throughput": stats["count"] / num_seconds,
                "avg": sum(durations) / len(durations) if durations else 0.0,
                "p95": percentile(durations, 95),
                "p99": percentile(durations, 99),
                "max": max(durations) if durations else 0.0,
                "errors": stats["errors"],
                "error_rate": (stats["errors"] / stats["count"] * 100.0) if stats["count"] else 0.0,
                "statuses": stats["statuses"],
            })
        rows.sort(key=lambda r: (-r["p95"], r["endpoint"]))
        return rows

    def build_overall_stats(self) -> dict:
        num_seconds = len(self.per_second) or 1
        total_reqs = sum(d["requests"] for d in self.per_second.values())
        return {
            "total_requests": total_reqs,
            "total_errors": self.total_errors,
            "avg_latency": (sum(self.all_durations) / len(self.all_durations)) if self.all_durations else 0.0,
            "p95_latency": percentile(self.all_durations, 95),
            "p99_latency": percentile(self.all_durations, 99),
            "throughput": total_reqs / num_seconds,
        }

    def build_async_stats(self) -> dict:
        samples = self.async_success + self.async_timeout + self.async_failed_state
        return {
            "samples": samples,
            "avg": (sum(self.async_durations) / len(self.async_durations)) if self.async_durations else 0.0,
            "p95": percentile(self.async_durations, 95),
            "p99": percentile(self.async_durations, 99),
            "poll_avg": (sum(self.async_polls) / len(self.async_polls)) if self.async_polls else 0.0,
            "poll_p95": percentile(self.async_polls, 95),
            "success": self.async_success,
            "timeout": self.async_timeout,
            "failed_state": self.async_failed_state,
            "early": self.async_early,
        }


def load_raw_metrics(path: str) -> tuple[ScenarioData, dict[str, ScenarioData], list[str]]:
    overall = ScenarioData()
    scenarios: dict[str, ScenarioData] = {}
    scenario_order: list[str] = []

    with open(path, "r", encoding="utf-8", errors="replace") as handle:
        for line in handle:
            line = line.strip()
            if not line:
                continue
            try:
                record = json.loads(line)
            except json.JSONDecodeError:
                continue
            if record.get("type") != "Point":
                continue

            metric_name = record.get("metric", "")
            data = record.get("data", {})
            tags = data.get("tags") or {}
            scenario = tags.get("scenario", "")
            bucket = parse_time_bucket(data.get("time", ""))

            if scenario and scenario not in scenarios:
                scenarios[scenario] = ScenarioData()
                scenario_order.append(scenario)

            targets = [overall]
            if scenario and scenario in scenarios:
                targets.append(scenarios[scenario])

            if metric_name == "http_reqs" and bucket:
                value = float(data.get("value", 0.0))
                for t in targets:
                    t.add_http_req(bucket, value)

            elif metric_name == "http_req_failed" and bucket:
                value = float(data.get("value", 0.0))
                for t in targets:
                    t.add_http_failed(bucket, value)

            elif metric_name == "http_req_duration" and bucket:
                endpoint_name = tags.get("name") or tags.get("url") or "unknown"
                duration = float(data.get("value", 0.0))
                status = str(tags.get("status", "unknown"))
                is_error = tags.get("expected_response") == "false" or status.startswith("4") or status.startswith("5")
                for t in targets:
                    t.add_http_duration(bucket, endpoint_name, duration, status, is_error)

            elif metric_name == "async_home_ready_duration":
                value = float(data.get("value", 0.0))
                for t in targets:
                    t.add_async_duration(value)

            elif metric_name == "async_home_ready_polls":
                value = float(data.get("value", 0.0))
                for t in targets:
                    t.add_async_polls(value)

            elif metric_name in ASYNC_COUNTER_METRICS:
                value = float(data.get("value", 0.0))
                for t in targets:
                    t.add_async_counter(metric_name, value)

            elif metric_name in OPERATION_COUNTERS and bucket:
                value = float(data.get("value", 0.0))
                for t in targets:
                    t.add_operation_counter(bucket, metric_name, value)

    return overall, scenarios, scenario_order


def parse_pprof_table(text: str) -> tuple[list[str], list[dict]]:
    summary_lines: list[str] = []
    rows: list[dict] = []
    in_table = False
    for raw_line in text.splitlines():
        line = raw_line.rstrip()
        if not line:
            continue
        if line.lstrip().startswith("flat") or re.match(r"^\s*flat\s+flat%", line):
            in_table = True
            continue
        if not in_table:
            summary_lines.append(line)
            continue
        parts = line.split()
        if len(parts) < 6:
            continue
        rows.append({
            "flat": parts[0], "flat_pct": parts[1], "sum_pct": parts[2],
            "cum": parts[3], "cum_pct": parts[4], "function": " ".join(parts[5:]),
        })
    return summary_lines, rows


def parse_goroutine_dump(text: str) -> tuple[int, Counter[str], str]:
    goroutine_lines = re.findall(r"^goroutine \d+ \[(.*?)\]:", text, flags=re.MULTILINE)
    states = Counter(goroutine_lines)
    excerpt = "\n".join(text.splitlines()[:120])
    return len(goroutine_lines), states, excerpt


def load_phase_cpu_profiles(pprof_dir: str) -> list[tuple[str, list[str], list[dict]]]:
    profiles = []
    pprof_path = Path(pprof_dir)
    if not pprof_path.is_dir():
        return profiles
    for top_file in sorted(pprof_path.glob("*-cpu-top.txt")):
        phase_name = top_file.name.replace("-cpu-top.txt", "")
        summary_lines, rows = parse_pprof_table(read_text(str(top_file)))
        profiles.append((phase_name, summary_lines, rows))
    return profiles


# ---------------------------------------------------------------------------
# HTML helpers
# ---------------------------------------------------------------------------
def escape(value: str) -> str:
    return html.escape(value, quote=True)


def stat_card(label: str, value: str) -> str:
    return f"""
    <div class="stat-card">
      <div class="stat-label">{escape(label)}</div>
      <div class="stat-value">{escape(value)}</div>
    </div>"""


def svg_line_chart(title: str, points: list[tuple[str, float]], color: str) -> str:
    if not points:
        return f'<div class="empty-chart">No data for {escape(title)}</div>'
    width, height = 960, 280
    pl, pr, pt, pb = 60, 20, 20, 45
    pw, ph = width - pl - pr, height - pt - pb
    values = [v for _, v in points]
    mx, mn = max(values), min(values)
    if math.isclose(mx, mn):
        mx = mn + 1.0

    def xp(i: int) -> float:
        return pl + pw / 2 if len(points) == 1 else pl + (pw * i / (len(points) - 1))

    def yp(v: float) -> float:
        return pt + ph - ((v - mn) / (mx - mn) * ph)

    poly = " ".join(f"{xp(i):.2f},{yp(v):.2f}" for i, (_, v) in enumerate(points))
    step = max(1, math.ceil(len(points) / min(6, len(points))))
    xt = "".join(
        f'<text x="{xp(i):.2f}" y="{height - 12}" text-anchor="middle" class="axis-label">{escape(points[i][0])}</text>'
        for i in range(0, len(points), step)
    )
    yt = []
    for ti in range(5):
        r = ti / 4
        v = mx - (mx - mn) * r
        y = pt + ph * r
        yt.append(
            f'<line x1="{pl}" y1="{y:.2f}" x2="{width - pr}" y2="{y:.2f}" class="grid-line"/>'
            f'<text x="{pl - 8}" y="{y + 4:.2f}" text-anchor="end" class="axis-label">{escape(format_number(v))}</text>'
        )
    return f"""
    <section class="chart-card">
      <h3>{escape(title)}</h3>
      <svg viewBox="0 0 {width} {height}" class="line-chart" role="img" aria-label="{escape(title)}">
        {''.join(yt)}
        <polyline fill="none" stroke="{color}" stroke-width="3" points="{poly}"/>
        {xt}
      </svg>
    </section>"""


def svg_horizontal_bar_chart(title: str, items: list[tuple[str, float]], color: str, formatter) -> str:
    if not items:
        return f'<div class="empty-chart">No data for {escape(title)}</div>'
    width, rh, lw = 960, 32, 320
    cw = width - lw - 120
    h = 50 + rh * len(items)
    mv = max(v for _, v in items) or 1.0
    rows = []
    for i, (label, v) in enumerate(items):
        y = 30 + i * rh
        bw = (v / mv) * cw
        rows.append(
            f'<text x="10" y="{y + 15}" class="bar-label">{escape(label)}</text>'
            f'<rect x="{lw}" y="{y}" width="{bw:.2f}" height="20" rx="4" fill="{color}"/>'
            f'<text x="{lw + bw + 8:.2f}" y="{y + 15}" class="bar-value">{escape(formatter(v))}</text>'
        )
    return f"""
    <section class="chart-card">
      <h3>{escape(title)}</h3>
      <svg viewBox="0 0 {width} {h}" class="bar-chart" role="img" aria-label="{escape(title)}">
        {''.join(rows)}
      </svg>
    </section>"""


def render_table(headers: list[str], rows: Iterable[Iterable[str]]) -> str:
    hh = "".join(f"<th>{escape(h)}</th>" for h in headers)
    body = []
    for row in rows:
        body.append("<tr>" + "".join(f"<td>{cell}</td>" for cell in row) + "</tr>")
    return f"""
    <div class="table-wrapper">
      <table><thead><tr>{hh}</tr></thead><tbody>{''.join(body)}</tbody></table>
    </div>"""


def render_pprof_section(title: str, summary_lines: list[str], rows: list[dict]) -> str:
    sh = "".join(f"<li>{escape(l)}</li>" for l in summary_lines)
    tr = [[escape(r["flat"]), escape(r["flat_pct"]), escape(r["cum"]), escape(r["cum_pct"]), escape(r["function"])] for r in rows[:15]]
    fb = ""
    if not tr and summary_lines:
        fb = f"<pre>{escape(chr(10).join(summary_lines))}</pre>"
    return f"""
    <section class="panel">
      <h2>{escape(title)}</h2>
      <ul class="pprof-summary-list">{sh}</ul>
      {render_table(["Flat", "Flat %", "Cum", "Cum %", "Function"], tr)}
      {fb}
    </section>"""


def render_endpoint_table(endpoint_rows: list[dict]) -> str:
    table_rows = []
    for row in endpoint_rows:
        status_html = "<br>".join(
            f"{escape(s)}: {c}" for s, c in sorted(row["statuses"].items(), key=lambda x: x[0])
        )
        table_rows.append([
            escape(row["endpoint"]),
            escape(format_number(row["count"])),
            escape(format_rate(row["throughput"])),
            escape(format_ms(row["avg"])),
            escape(format_ms(row["p95"])),
            escape(format_ms(row["p99"])),
            escape(format_ms(row["max"])),
            escape(format_pct(row["error_rate"])),
            status_html,
        ])
    return render_table(
        ["Endpoint", "Requests", "Throughput", "Average", "p95", "p99", "Max", "Error rate", "Statuses"],
        table_rows,
    )


def render_load_charts(req_s, lat_s, err_s) -> str:
    return f"""
      <div class="chart-grid">
        {svg_line_chart("Requests per second", req_s, "#60a5fa")}
        {svg_line_chart("Average latency over time (ms)", lat_s, "#34d399")}
        {svg_line_chart("Errors per second", err_s, "#f87171")}
      </div>"""


def render_endpoint_charts(endpoint_rows: list[dict]) -> str:
    p95 = [(r["endpoint"], r["p95"]) for r in endpoint_rows[:12]]
    tp = sorted([(r["endpoint"], r["throughput"]) for r in endpoint_rows], key=lambda x: x[1], reverse=True)[:12]
    return f"""
      <div class="chart-grid">
        {svg_horizontal_bar_chart("Top endpoints by p95 latency", p95, "#f59e0b", format_ms)}
        {svg_horizontal_bar_chart("Top endpoints by throughput", tp, "#60a5fa", format_rate)}
      </div>"""


def render_operation_charts(data: ScenarioData) -> str:
    counters = data.active_counters()
    if not counters:
        return ""
    charts = []
    colors = ["#60a5fa", "#34d399", "#f59e0b", "#f87171", "#a78bfa", "#fb923c", "#38bdf8"]
    for i, metric in enumerate(counters):
        series = data.build_counter_series(metric)
        label = COUNTER_DISPLAY.get(metric, metric)
        total = data.counter_totals[metric]
        color = colors[i % len(colors)]
        charts.append(svg_line_chart(f"{label} per second (total: {total:,})", series, color))
    return f"""
      <h3>Operations Throughput</h3>
      <div class="chart-grid">{''.join(charts)}</div>"""


def render_async_section(data: ScenarioData) -> str:
    a = data.build_async_stats()
    if a["samples"] == 0 and not data.async_durations:
        return ""
    cards = [
        stat_card("Ready samples", format_number(a["samples"])),
        stat_card("Ready avg", format_ms(a["avg"])),
        stat_card("Ready p95", format_ms(a["p95"])),
        stat_card("Ready p99", format_ms(a["p99"])),
        stat_card("Ready success", format_number(a["success"])),
        stat_card("Ready timeouts", format_number(a["timeout"])),
        stat_card("Ready failed-state", format_number(a["failed_state"])),
        stat_card("Early-ready violations", format_number(a["early"])),
        stat_card("Avg polls", format_number(a["poll_avg"])),
    ]
    return f"""
      <h3>Async Readiness Timers</h3>
      <div class="stats-grid">{''.join(cards)}</div>"""


def render_scenario_section(
    scenario_name: str,
    data: ScenarioData,
    cpu_profile: tuple[str, list[str], list[dict]] | None,
) -> str:
    req_s, lat_s, err_s = data.build_series()
    ep_rows = data.build_endpoint_rows()
    stats = data.build_overall_stats()

    cards = [
        stat_card("Requests", format_number(stats["total_requests"])),
        stat_card("Throughput", format_rate(stats["throughput"])),
        stat_card("Errors", format_number(stats["total_errors"])),
        stat_card("Avg latency", format_ms(stats["avg_latency"])),
        stat_card("p95 latency", format_ms(stats["p95_latency"])),
        stat_card("p99 latency", format_ms(stats["p99_latency"])),
    ]

    async_html = render_async_section(data)
    ops_html = render_operation_charts(data)

    cpu_html = ""
    if cpu_profile:
        _, sl, cr = cpu_profile
        cpu_html = render_pprof_section("CPU Hot Paths", sl, cr)

    st_rows = [[escape(s), escape(format_number(c))] for s, c in sorted(data.overall_statuses.items())]
    display = scenario_name.replace("_", " ").title()

    return f"""
    <section class="panel scenario-panel" id="scenario-{escape(scenario_name)}">
      <h2>{escape(display)}</h2>
      <div class="stats-grid">{''.join(cards)}</div>
      {async_html}
      {ops_html}
      {render_load_charts(req_s, lat_s, err_s)}
      <h3>Endpoint Performance</h3>
      {render_endpoint_charts(ep_rows)}
      {render_endpoint_table(ep_rows)}
      <h3>HTTP Status Breakdown</h3>
      {render_table(["Status", "Count"], st_rows)}
      {cpu_html}
    </section>"""


# ---------------------------------------------------------------------------
# Main report
# ---------------------------------------------------------------------------
def render_report(args: argparse.Namespace) -> str:
    summary = read_json(args.summary)
    metadata = read_json(args.metadata)
    async_jobs = read_json(args.async_jobs)
    read_json(args.phase_state)  # loaded for completeness

    overall, scenarios, scenario_order = load_raw_metrics(args.raw)
    heap_summary_lines, heap_rows = parse_pprof_table(read_text(args.heap_top))
    heap_inuse_summary_lines, heap_inuse_rows = parse_pprof_table(read_text(args.heap_inuse_top))
    goroutine_count, goroutine_states, goroutine_excerpt = parse_goroutine_dump(read_text(args.goroutine))
    phase_cpu_profiles = load_phase_cpu_profiles(args.pprof_dir)

    cpu_profile_map: dict[str, tuple[str, list[str], list[dict]]] = {p[0]: p for p in phase_cpu_profiles}

    # ── Overall stats (summary JSON with raw fallback) ───────────────────
    raw_stats = overall.build_overall_stats()
    raw_async = overall.build_async_stats()

    total_requests = metric_value(summary, "http_reqs", "count", raw_stats["total_requests"])
    failure_rate = metric_value(summary, "http_req_failed", "rate", 0.0) * 100.0
    if failure_rate == 0.0 and raw_stats["total_requests"] > 0:
        failure_rate = raw_stats["total_errors"] / raw_stats["total_requests"] * 100.0
    avg_latency = metric_value(summary, "http_req_duration", "avg", raw_stats["avg_latency"])
    p95_latency = metric_value(summary, "http_req_duration", "p(95)", raw_stats["p95_latency"])
    p99_latency = metric_value(summary, "http_req_duration", "p(99)", raw_stats["p99_latency"])
    throughput = metric_value(summary, "http_reqs", "rate", raw_stats["throughput"])

    async_ready_avg = metric_value(summary, "async_home_ready_duration", "avg", raw_async["avg"])
    async_ready_p95 = metric_value(summary, "async_home_ready_duration", "p(95)", raw_async["p95"])
    async_ready_p99 = metric_value(summary, "async_home_ready_duration", "p(99)", raw_async["p99"])
    async_ready_poll_avg = metric_value(summary, "async_home_ready_polls", "avg", raw_async["poll_avg"])
    async_ready_poll_p95 = metric_value(summary, "async_home_ready_polls", "p(95)", raw_async["poll_p95"])
    async_ready_success = int(metric_value(summary, "async_home_ready_success", "count", float(raw_async["success"])))
    async_ready_timeout = int(metric_value(summary, "async_home_ready_timeout", "count", float(raw_async["timeout"])))
    async_ready_failed_state = int(metric_value(summary, "async_home_ready_failed_state", "count", float(raw_async["failed_state"])))
    async_ready_early = int(metric_value(summary, "async_home_ready_early", "count", float(raw_async["early"])))
    async_ready_samples = async_ready_success + async_ready_timeout + async_ready_failed_state

    async_completed = bool(async_jobs.get("all_async_jobs_completed"))
    async_remaining_jobs = int(async_jobs.get("remaining_job_total", 0) or 0)
    async_remaining_homes = int(async_jobs.get("remaining_home_total", 0) or 0)
    async_global_jobs = int(async_jobs.get("global_job_total", 0) or 0)
    async_timed_out = bool(async_jobs.get("timed_out"))
    async_error = str(async_jobs.get("error", "") or "")
    async_scope = async_jobs.get("scope", {})
    async_scope_started = str(async_scope.get("run_started_at", metadata.get("run_started_at", "")) or "")
    async_captured_at = str(async_jobs.get("captured_at", "") or "")
    async_drain_wait = async_jobs.get("drain_wait_seconds", metadata.get("async_drain_wait_seconds", ""))

    async_status_message = "All async home jobs drained successfully within the verification window."
    if not async_jobs:
        async_status_message = "Async audit artifact was not found."
    elif not async_completed:
        async_status_message = "Async work remained after the drain window or the audit encountered an error."
    if async_error:
        async_status_message = f"{async_status_message} Audit error: {async_error}"

    phases = metadata.get("phases", [])
    phase_count = len(phases)
    total_iterations = sum(p.get("iterations", 0) for p in phases)

    # ── Cards ─────────────────────────────────────────────────────────────
    overview_cards = [
        stat_card("Total requests", format_number(total_requests)),
        stat_card("Failure rate", format_pct(failure_rate)),
        stat_card("Average latency", format_ms(avg_latency)),
        stat_card("p95 latency", format_ms(p95_latency)),
        stat_card("p99 latency", format_ms(p99_latency)),
        stat_card("Throughput", format_rate(throughput)),
        stat_card("Phases", str(phase_count)),
        stat_card("Total iterations", format_number(total_iterations)),
        stat_card(
            "Users / Homes / Devices",
            f'{metadata.get("stress_user_count", "?")} / '
            f'{int(metadata.get("stress_user_count", 0) or 0) * int(metadata.get("stress_homes_per_user", 0) or 0)} / '
            f'{int(metadata.get("stress_user_count", 0) or 0) * int(metadata.get("stress_homes_per_user", 0) or 0) * int(metadata.get("stress_devices_per_home", 0) or 0)}',
        ),
    ]
    async_cards = [
        stat_card("Async drain result", "PASS" if async_completed else "FAIL"),
        stat_card("Ready samples", format_number(async_ready_samples)),
        stat_card("Ready avg", format_ms(async_ready_avg)),
        stat_card("Ready p95", format_ms(async_ready_p95)),
        stat_card("Ready p99", format_ms(async_ready_p99)),
        stat_card("Ready success", format_number(async_ready_success)),
        stat_card("Ready timeouts", format_number(async_ready_timeout)),
        stat_card("Ready failed-state", format_number(async_ready_failed_state)),
        stat_card("Early-ready violations", format_number(async_ready_early)),
        stat_card("Remaining jobs", format_number(async_remaining_jobs)),
        stat_card("Remaining homes", format_number(async_remaining_homes)),
        stat_card("Avg polls", format_number(async_ready_poll_avg)),
        stat_card("Global queue rows", format_number(async_global_jobs)),
    ]

    # ── Overall series and tables ─────────────────────────────────────────
    overall_ep_rows = overall.build_endpoint_rows()
    overall_req_s, overall_lat_s, overall_err_s = overall.build_series()

    status_rows = [[escape(s), escape(format_number(c))] for s, c in sorted(overall.overall_statuses.items())]
    goroutine_rows = [[escape(s), escape(format_number(c))] for s, c in goroutine_states.most_common()]

    async_job_rows = [
        [escape(str(r.get("operation", ""))), escape(str(r.get("status", ""))), escape(format_number(int(r.get("count", 0) or 0)))]
        for r in async_jobs.get("operation_status_counts", [])
    ] or [[escape("none"), escape("-"), escape("0")]]

    async_home_state_rows = [
        [escape(s), escape(format_number(int(c or 0)))]
        for s, c in sorted((async_jobs.get("home_state_counts") or {}).items())
    ] or [[escape("none"), escape("0")]]

    job_cols = ["Job ID", "Home", "Operation", "Status", "Attempts", "Next run", "Claimed at", "Last error"]
    empty_job = [escape("none")] + [escape("-")] * 3 + [escape("0")] + [escape("-")] * 3

    def job_row(r: dict) -> list[str]:
        return [
            escape(str(r.get("id", ""))), escape(str(r.get("home_id", ""))),
            escape(str(r.get("operation", ""))), escape(str(r.get("status", ""))),
            escape(format_number(int(r.get("attempts", 0) or 0))),
            escape(str(r.get("next_run_at", "") or "-")),
            escape(str(r.get("claimed_at", "") or "-")),
            escape(str(r.get("last_error", "") or "-")),
        ]

    active_async_rows = [job_row(r) for r in async_jobs.get("active_jobs", [])] or [empty_job]
    failed_async_rows = [job_row(r) for r in async_jobs.get("failed_jobs", [])] or [empty_job]

    # Phase config table
    phase_table_rows = []
    for p in phases:
        phase_table_rows.append([
            escape(str(p.get("name", ""))),
            escape(format_number(int(p.get("iterations", 0)))),
            escape(format_number(int(p.get("workers", 0)))),
            escape(f'{p.get("start_rps", 0):.0f}'),
            escape(f'{p.get("peak_rps", 0):.0f}'),
            escape(f'{p.get("ramp_up_seconds", 0)}s'),
            escape(f'{p.get("hold_seconds", 0)}s'),
            escape(f'{p.get("ramp_down_seconds", 0)}s'),
            escape(str(p.get("max_duration", ""))),
        ])

    # Artifact links
    artifact_links = [
        ('artifacts/k6-summary.json', 'Summary JSON'),
        ('artifacts/k6-raw.ndjson', 'Raw metrics'),
        ('artifacts/async-jobs.json', 'Async jobs audit'),
        ('artifacts/run-metadata.json', 'Run metadata'),
        ('artifacts/phase-state.json', 'Phase state'),
        ('pprof/goroutine.txt', 'Goroutine dump'),
        ('pprof/heap.pb.gz', 'Heap profile'),
        ('pprof/heap-top.txt', 'Heap alloc top'),
        ('pprof/heap-inuse-top.txt', 'Heap in-use top'),
    ]
    for p in phases:
        n = p.get("name", "")
        artifact_links += [(f'pprof/{n}-cpu.pb.gz', f'{n} CPU profile'), (f'pprof/{n}-cpu-top.txt', f'{n} CPU top')]

    links_html = "".join(f'<li><a href="{escape(p)}">{escape(l)}</a></li>' for p, l in artifact_links)
    async_error_html = f"<pre>{escape(async_error)}</pre>" if async_error else ""

    # Metadata grid
    meta_items = [
        ("Run ID", str(metadata.get("run_id", "unknown"))),
        ("Runner", str(metadata.get("runner_suite", "vegeta"))),
        ("Base URL", str(metadata.get("base_url", ""))),
        ("pprof URL", str(metadata.get("pprof_base_url", ""))),
        ("Firmware host", str(metadata.get("caddy_base_url", ""))),
        ("Users", str(metadata.get("stress_user_count", ""))),
        ("Homes/user", str(metadata.get("stress_homes_per_user", ""))),
        ("Devices/home", str(metadata.get("stress_devices_per_home", ""))),
        ("Delete devices/user", str(metadata.get("stress_delete_devices_per_user", ""))),
        ("Delete homes/user", str(metadata.get("stress_delete_homes_per_user", ""))),
        ("Fulfillment req/device", str(metadata.get("stress_fulfillment_requests_per_device", ""))),
        ("Async drain window", f'{metadata.get("async_drain_wait_seconds", "")}s'),
    ]
    metadata_html = "".join(f"<div><strong>{escape(l)}:</strong> {escape(v)}</div>" for l, v in meta_items)

    # Scenario nav
    scenario_nav_html = ""
    if scenario_order:
        navs = [f'<a href="#scenario-{escape(s)}" class="scenario-nav-link">{escape(s.replace("_"," ").title())}</a>' for s in scenario_order]
        scenario_nav_html = f'<div class="scenario-nav"><strong>Scenarios:</strong> {" ".join(navs)}</div>'

    # Overall operations throughput charts
    overall_ops_html = render_operation_charts(overall)

    # Per-scenario sections (CPU profiles go here ONLY, not repeated above)
    scenario_sections_html = ""
    for sn in scenario_order:
        scenario_sections_html += render_scenario_section(sn, scenarios[sn], cpu_profile_map.get(sn))

    return f"""<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8"/>
  <meta name="viewport" content="width=device-width, initial-scale=1"/>
  <title>IoT Backend Stress Dashboard</title>
  <style>
    :root {{
      --bg: #0b1020; --panel: #121a30; --panel-alt: #17213d;
      --text: #e7ecf7; --muted: #94a3b8; --accent: #60a5fa;
      --accent-2: #34d399; --accent-3: #f59e0b; --danger: #f87171;
      --border: rgba(148,163,184,0.22);
    }}
    *{{ box-sizing:border-box }}
    body{{ margin:0; font-family:"Segoe UI",Arial,sans-serif;
      background:linear-gradient(180deg,#08101d 0%,#111827 100%);
      color:var(--text); line-height:1.5 }}
    main{{ max-width:1400px; margin:0 auto; padding:32px 24px 64px }}
    h1,h2,h3{{ margin-top:0 }}
    .subtitle{{ color:var(--muted); margin-bottom:24px }}
    .panel{{ background:rgba(18,26,48,0.88); border:1px solid var(--border);
      border-radius:18px; padding:24px; margin-bottom:24px;
      box-shadow:0 24px 60px rgba(0,0,0,0.22) }}
    .scenario-panel{{ border-left:4px solid var(--accent) }}
    .stats-grid{{ display:grid; grid-template-columns:repeat(auto-fit,minmax(200px,1fr));
      gap:16px; margin-bottom:24px }}
    .stat-card{{ background:var(--panel-alt); border:1px solid var(--border);
      border-radius:14px; padding:16px 18px }}
    .stat-label{{ color:var(--muted); font-size:13px; text-transform:uppercase;
      letter-spacing:0.08em; margin-bottom:8px }}
    .stat-value{{ font-size:28px; font-weight:700 }}
    .chart-grid{{ display:grid; grid-template-columns:1fr; gap:18px }}
    .chart-card{{ background:var(--panel-alt); border:1px solid var(--border);
      border-radius:16px; padding:16px }}
    .line-chart,.bar-chart{{ width:100%; height:auto; display:block }}
    .grid-line{{ stroke:rgba(148,163,184,0.18); stroke-width:1 }}
    .axis-label{{ fill:#b8c4da; font-size:11px }}
    .bar-label,.bar-value{{ fill:#dce5f4; font-size:12px }}
    .table-wrapper{{ overflow-x:auto }}
    table{{ width:100%; border-collapse:collapse; font-size:14px }}
    th,td{{ text-align:left; padding:10px 12px;
      border-bottom:1px solid rgba(148,163,184,0.12); vertical-align:top }}
    th{{ color:var(--muted); font-size:12px; text-transform:uppercase; letter-spacing:0.08em }}
    code,pre{{ font-family:"SFMono-Regular",Consolas,"Liberation Mono",monospace }}
    pre{{ background:#0a1224; border:1px solid var(--border); border-radius:12px;
      padding:14px; overflow-x:auto; color:#dbe8ff; white-space:pre-wrap; word-break:break-word }}
    .pprof-summary-list{{ margin:0 0 18px; padding-left:18px; color:var(--muted) }}
    .split-grid{{ display:grid; grid-template-columns:repeat(auto-fit,minmax(480px,1fr)); gap:20px }}
    .links-list{{ display:grid; grid-template-columns:repeat(auto-fit,minmax(220px,1fr));
      gap:8px 18px; padding-left:18px }}
    a{{ color:#93c5fd }}
    .empty-chart{{ color:var(--muted); padding:12px }}
    .metadata-grid{{ display:grid; grid-template-columns:repeat(auto-fit,minmax(220px,1fr));
      gap:10px 16px; color:var(--muted); margin-bottom:18px }}
    .section-intro{{ color:var(--muted); margin-top:-8px; margin-bottom:18px }}
    .status-banner{{ border:1px solid var(--border); border-radius:14px;
      padding:14px 16px; margin-bottom:18px; font-weight:600 }}
    .status-banner.ok{{ background:rgba(52,211,153,0.12); border-color:rgba(52,211,153,0.34) }}
    .status-banner.fail{{ background:rgba(248,113,113,0.12); border-color:rgba(248,113,113,0.34) }}
    .scenario-nav{{ margin:18px 0; padding:14px 18px; background:var(--panel-alt);
      border:1px solid var(--border); border-radius:14px }}
    .scenario-nav-link{{ display:inline-block; margin:4px 8px 4px 0; padding:4px 12px;
      border-radius:8px; background:rgba(96,165,250,0.12); text-decoration:none; font-size:14px }}
    .scenario-nav-link:hover{{ background:rgba(96,165,250,0.24) }}
    .scenario-divider{{ margin:48px 0 24px; padding:16px 24px;
      background:linear-gradient(90deg,rgba(96,165,250,0.18),transparent); border-radius:12px }}
    .scenario-divider h2{{ margin:0; color:var(--accent) }}
  </style>
</head>
<body>
  <main>
    <!-- ═══ Overview ═══ -->
    <section class="panel">
      <h1>IoT Backend Stress Test Dashboard</h1>
      <p class="subtitle">Combined Vegeta phase load-test and Go pprof profile report.</p>
      <div class="metadata-grid">{metadata_html}</div>
      <div class="stats-grid">{''.join(overview_cards)}</div>
      {scenario_nav_html}
      <ul class="links-list">{links_html}</ul>
    </section>

    <!-- ═══ Phase config ═══ -->
    <section class="panel">
      <h2>Phase Configuration</h2>
      <p class="section-intro">Each phase runs sequentially with its own vegeta attack profile.</p>
      {render_table(
        ["Phase","Iterations","Workers","Start RPS","Peak RPS","Ramp up","Hold","Ramp down","Max duration"],
        phase_table_rows,
      )}
    </section>

    <!-- ═══ Async jobs audit ═══ -->
    <section class="panel">
      <h2>Async Jobs</h2>
      <p class="section-intro">Readiness timings come from polling after each home create. Queue drain results come from the post-run PostgreSQL audit scoped to homes and jobs created after {escape(async_scope_started)}.</p>
      <div class="status-banner {"ok" if async_completed else "fail"}">{escape(async_status_message)}</div>
      <div class="stats-grid">{''.join(async_cards)}</div>
      <div class="split-grid">
        <div><h3>Remaining jobs by operation and status</h3>
          {render_table(["Operation","Status","Count"], async_job_rows)}</div>
        <div><h3>Remaining homes by MQTT state</h3>
          {render_table(["State","Count"], async_home_state_rows)}</div>
      </div>
      <div class="split-grid">
        <div><h3>Active async jobs</h3>{render_table(job_cols, active_async_rows)}</div>
        <div><h3>Failed async jobs</h3>{render_table(job_cols, failed_async_rows)}</div>
      </div>
      <p class="section-intro">Audit captured at {escape(async_captured_at or "unknown")} after a {escape(str(async_drain_wait))}s drain window. Average ready polling rounds: {escape(format_number(async_ready_poll_avg))}; p95 polls: {escape(format_number(async_ready_poll_p95))}. Timed out: {escape(str(async_timed_out))}.</p>
      {async_error_html}
    </section>

    <!-- ═══ Overall load overview ═══ -->
    <section class="panel">
      <h2>Overall Load Overview</h2>
      <p class="section-intro">Aggregate view across all phases: request volume, latency, and failures over time.</p>
      {render_load_charts(overall_req_s, overall_lat_s, overall_err_s)}
    </section>

    <!-- ═══ Overall operations throughput ═══ -->
    <section class="panel">
      <h2>Operations Throughput</h2>
      <p class="section-intro">Rate of entity creation/deletion operations across the entire run.</p>
      {overall_ops_html}
    </section>

    <!-- ═══ Overall endpoint performance ═══ -->
    <section class="panel">
      <h2>Overall Endpoint Performance</h2>
      <p class="section-intro">Per-endpoint timings and throughput aggregated across all phases.</p>
      {render_endpoint_charts(overall_ep_rows)}
      {render_endpoint_table(overall_ep_rows)}
    </section>

    <!-- ═══ Status breakdown ═══ -->
    <section class="panel">
      <h2>Overall Status Breakdown</h2>
      <div class="split-grid">
        <div><h3>HTTP statuses</h3>{render_table(["Status","Count"], status_rows)}</div>
        <div><h3>Goroutine states</h3>{render_table(["State","Count"], goroutine_rows)}</div>
      </div>
    </section>

    <!-- ═══ Profiling (heap + goroutines only, CPU is per-scenario) ═══ -->
    {render_pprof_section("Heap Allocation Hot Paths", heap_summary_lines, heap_rows)}
    {render_pprof_section("Heap In-Use View", heap_inuse_summary_lines, heap_inuse_rows)}

    <section class="panel">
      <h2>Goroutine Dump Excerpt</h2>
      <p class="section-intro">Total goroutines captured: {escape(str(goroutine_count))}</p>
      <pre>{escape(goroutine_excerpt)}</pre>
    </section>

    <!-- ═══ Per-scenario breakdown ═══ -->
    <div class="scenario-divider">
      <h2>Per-Scenario Breakdown</h2>
      <p style="color:var(--muted);margin:4px 0 0">Each scenario shows its own RPS, latency, errors, endpoint throughput, operations rate, async timers, and CPU hot paths.</p>
    </div>

    {scenario_sections_html}
  </main>
</body>
</html>
"""


def main() -> None:
    args = parse_args()
    report_html = render_report(args)
    Path(args.report).write_text(report_html, encoding="utf-8")


if __name__ == "__main__":
    main()
