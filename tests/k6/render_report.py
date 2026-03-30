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
    parser = argparse.ArgumentParser(description="Render a combined k6 + pprof HTML dashboard.")
    parser.add_argument("--summary", required=True)
    parser.add_argument("--raw", required=True)
    parser.add_argument("--report", required=True)
    parser.add_argument("--metadata", required=True)
    parser.add_argument("--cpu-top", required=True)
    parser.add_argument("--cpu-cum", required=True)
    parser.add_argument("--heap-top", required=True)
    parser.add_argument("--heap-inuse-top", required=True)
    parser.add_argument("--cpu-svg", required=True)
    parser.add_argument("--heap-svg", required=True)
    parser.add_argument("--goroutine", required=True)
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
    lower_value = sorted_values[lower]
    upper_value = sorted_values[upper]
    weight = index - lower
    return lower_value + (upper_value - lower_value) * weight


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


def load_raw_metrics(path: str) -> tuple[list[tuple[str, float]], list[tuple[str, float]], list[tuple[str, float]], list[dict], Counter, dict]:
    per_second = defaultdict(lambda: {"requests": 0.0, "errors": 0.0, "duration_total": 0.0, "duration_count": 0})
    endpoints: dict[str, dict] = defaultdict(lambda: {
        "count": 0,
        "errors": 0,
        "durations": [],
        "statuses": Counter(),
    })
    overall_statuses: Counter[str] = Counter()
    all_durations: list[float] = []
    total_errors = 0

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
            bucket = parse_time_bucket(data.get("time", ""))
            if not bucket:
                continue

            if metric_name == "http_reqs":
                per_second[bucket]["requests"] += float(data.get("value", 0.0))

            elif metric_name == "http_req_failed":
                per_second[bucket]["errors"] += float(data.get("value", 0.0))

            elif metric_name == "http_req_duration":
                endpoint_name = tags.get("name") or tags.get("url") or "unknown"
                duration = float(data.get("value", 0.0))
                status = str(tags.get("status", "unknown"))

                per_second[bucket]["duration_total"] += duration
                per_second[bucket]["duration_count"] += 1

                endpoint_stats = endpoints[endpoint_name]
                endpoint_stats["count"] += 1
                endpoint_stats["durations"].append(duration)
                all_durations.append(duration)
                endpoint_stats["statuses"][status] += 1
                overall_statuses[status] += 1

                if tags.get("expected_response") == "false" or status.startswith("4") or status.startswith("5"):
                    endpoint_stats["errors"] += 1
                    total_errors += 1

    request_series: list[tuple[str, float]] = []
    latency_series: list[tuple[str, float]] = []
    error_series: list[tuple[str, float]] = []

    for bucket in sorted(per_second.keys()):
        request_series.append((bucket, per_second[bucket]["requests"]))
        latency_avg = 0.0
        if per_second[bucket]["duration_count"] > 0:
            latency_avg = per_second[bucket]["duration_total"] / per_second[bucket]["duration_count"]
        latency_series.append((bucket, latency_avg))
        error_series.append((bucket, per_second[bucket]["errors"]))

    endpoint_rows: list[dict] = []
    for endpoint_name, stats in endpoints.items():
        durations = stats["durations"]
        endpoint_rows.append({
            "endpoint": endpoint_name,
            "count": stats["count"],
            "avg": sum(durations) / len(durations) if durations else 0.0,
            "p95": percentile(durations, 95),
            "p99": percentile(durations, 99),
            "max": max(durations) if durations else 0.0,
            "errors": stats["errors"],
            "error_rate": (stats["errors"] / stats["count"] * 100.0) if stats["count"] else 0.0,
            "statuses": stats["statuses"],
        })

    endpoint_rows.sort(key=lambda item: (-item["p95"], item["endpoint"]))
    overall_stats = {
        "total_requests": sum(row["count"] for row in endpoint_rows),
        "total_errors": total_errors,
        "avg_latency": (sum(all_durations) / len(all_durations)) if all_durations else 0.0,
        "p95_latency": percentile(all_durations, 95),
        "p99_latency": percentile(all_durations, 99),
        "throughput": sum(value for _, value in request_series) / len(request_series) if request_series else 0.0,
    }
    return request_series, latency_series, error_series, endpoint_rows, overall_statuses, overall_stats


def load_svg_fragment(path: str) -> str:
    content = read_text(path)
    if "<svg" not in content:
        return ""
    return content[content.index("<svg") :]


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
            "flat": parts[0],
            "flat_pct": parts[1],
            "sum_pct": parts[2],
            "cum": parts[3],
            "cum_pct": parts[4],
            "function": " ".join(parts[5:]),
        })

    return summary_lines, rows


def parse_goroutine_dump(text: str) -> tuple[int, Counter[str], str]:
    goroutine_lines = re.findall(r"^goroutine \d+ \[(.*?)\]:", text, flags=re.MULTILINE)
    states = Counter(goroutine_lines)
    excerpt = "\n".join(text.splitlines()[:120])
    return len(goroutine_lines), states, excerpt


def escape(value: str) -> str:
    return html.escape(value, quote=True)


def stat_card(label: str, value: str) -> str:
    return f"""
    <div class="stat-card">
      <div class="stat-label">{escape(label)}</div>
      <div class="stat-value">{escape(value)}</div>
    </div>
    """


def svg_line_chart(title: str, points: list[tuple[str, float]], color: str) -> str:
    if not points:
        return f'<div class="empty-chart">No data for {escape(title)}</div>'

    width = 960
    height = 280
    padding_left = 60
    padding_right = 20
    padding_top = 20
    padding_bottom = 45
    plot_width = width - padding_left - padding_right
    plot_height = height - padding_top - padding_bottom

    values = [value for _, value in points]
    max_value = max(values) if values else 0.0
    min_value = min(values) if values else 0.0
    if math.isclose(max_value, min_value):
        max_value = min_value + 1.0

    def x_position(index: int) -> float:
        if len(points) == 1:
            return padding_left + plot_width / 2
        return padding_left + (plot_width * index / (len(points) - 1))

    def y_position(value: float) -> float:
        return padding_top + plot_height - ((value - min_value) / (max_value - min_value) * plot_height)

    polyline_points = " ".join(
        f"{x_position(index):.2f},{y_position(value):.2f}" for index, (_, value) in enumerate(points)
    )

    tick_count = min(6, len(points))
    tick_step = max(1, math.ceil(len(points) / tick_count))
    x_ticks = []
    for index in range(0, len(points), tick_step):
        label, _ = points[index]
        x_ticks.append(
            f'<text x="{x_position(index):.2f}" y="{height - 12}" text-anchor="middle" class="axis-label">{escape(label)}</text>'
        )

    y_ticks = []
    for tick_index in range(5):
        ratio = tick_index / 4
        value = max_value - ((max_value - min_value) * ratio)
        y = padding_top + plot_height * ratio
        y_ticks.append(
            f'<line x1="{padding_left}" y1="{y:.2f}" x2="{width - padding_right}" y2="{y:.2f}" class="grid-line" />'
            f'<text x="{padding_left - 8}" y="{y + 4:.2f}" text-anchor="end" class="axis-label">{escape(format_number(value))}</text>'
        )

    return f"""
    <section class="chart-card">
      <h3>{escape(title)}</h3>
      <svg viewBox="0 0 {width} {height}" class="line-chart" role="img" aria-label="{escape(title)}">
        {''.join(y_ticks)}
        <polyline fill="none" stroke="{color}" stroke-width="3" points="{polyline_points}" />
        {''.join(x_ticks)}
      </svg>
    </section>
    """


def svg_horizontal_bar_chart(title: str, items: list[tuple[str, float]], color: str, formatter) -> str:
    if not items:
        return f'<div class="empty-chart">No data for {escape(title)}</div>'

    width = 960
    row_height = 32
    label_width = 320
    chart_width = width - label_width - 120
    height = 50 + row_height * len(items)
    max_value = max(value for _, value in items) or 1.0

    rows = []
    for index, (label, value) in enumerate(items):
        y = 30 + index * row_height
        bar_width = (value / max_value) * chart_width
        rows.append(
            f'<text x="10" y="{y + 15}" class="bar-label">{escape(label)}</text>'
            f'<rect x="{label_width}" y="{y}" width="{bar_width:.2f}" height="20" rx="4" fill="{color}" />'
            f'<text x="{label_width + bar_width + 8:.2f}" y="{y + 15}" class="bar-value">{escape(formatter(value))}</text>'
        )

    return f"""
    <section class="chart-card">
      <h3>{escape(title)}</h3>
      <svg viewBox="0 0 {width} {height}" class="bar-chart" role="img" aria-label="{escape(title)}">
        {''.join(rows)}
      </svg>
    </section>
    """


def render_table(headers: list[str], rows: Iterable[Iterable[str]]) -> str:
    header_html = "".join(f"<th>{escape(header)}</th>" for header in headers)
    body_html = []
    for row in rows:
        body_html.append("<tr>" + "".join(f"<td>{cell}</td>" for cell in row) + "</tr>")
    return f"""
    <div class="table-wrapper">
      <table>
        <thead><tr>{header_html}</tr></thead>
        <tbody>
          {''.join(body_html)}
        </tbody>
      </table>
    </div>
    """


def render_pprof_section(title: str, summary_lines: list[str], rows: list[dict], svg_fragment: str) -> str:
    summary_html = "".join(f"<li>{escape(line)}</li>" for line in summary_lines)
    table_rows = [
        [
            escape(row["flat"]),
            escape(row["flat_pct"]),
            escape(row["cum"]),
            escape(row["cum_pct"]),
            escape(row["function"]),
        ]
        for row in rows[:15]
    ]
    fallback_html = ""
    if not table_rows and summary_lines:
        fallback_html = f"<pre>{escape(chr(10).join(summary_lines))}</pre>"

    svg_html = ""
    if svg_fragment:
        svg_html = f'<div class="svg-wrapper">{svg_fragment}</div>'

    return f"""
    <section class="panel">
      <h2>{escape(title)}</h2>
      <ul class="pprof-summary-list">{summary_html}</ul>
      {render_table(["Flat", "Flat %", "Cum", "Cum %", "Function"], table_rows)}
      {fallback_html}
      {svg_html}
    </section>
    """


def render_report(args: argparse.Namespace) -> str:
    summary = read_json(args.summary)
    metadata = read_json(args.metadata)

    request_series, latency_series, error_series, endpoint_rows, overall_statuses, overall_stats = load_raw_metrics(args.raw)
    cpu_summary_lines, cpu_rows = parse_pprof_table(read_text(args.cpu_top))
    cpu_cum_summary_lines, cpu_cum_rows = parse_pprof_table(read_text(args.cpu_cum))
    heap_summary_lines, heap_rows = parse_pprof_table(read_text(args.heap_top))
    heap_inuse_summary_lines, heap_inuse_rows = parse_pprof_table(read_text(args.heap_inuse_top))
    goroutine_count, goroutine_states, goroutine_excerpt = parse_goroutine_dump(read_text(args.goroutine))

    total_requests = metric_value(summary, "http_reqs", "count", overall_stats["total_requests"])
    failure_rate = metric_value(summary, "http_req_failed", "rate", 0.0) * 100.0
    if failure_rate == 0.0 and overall_stats["total_requests"] > 0:
        failure_rate = overall_stats["total_errors"] / overall_stats["total_requests"] * 100.0
    avg_latency = metric_value(summary, "http_req_duration", "avg", overall_stats["avg_latency"])
    p95_latency = metric_value(summary, "http_req_duration", "p(95)", overall_stats["p95_latency"])
    p99_latency = metric_value(summary, "http_req_duration", "p(99)", overall_stats["p99_latency"])
    throughput = metric_value(summary, "http_reqs", "rate", overall_stats["throughput"])

    overview_cards = [
        stat_card("Total requests", format_number(total_requests)),
        stat_card("Failure rate", format_pct(failure_rate)),
        stat_card("Average latency", format_ms(avg_latency)),
        stat_card("p95 latency", format_ms(p95_latency)),
        stat_card("p99 latency", format_ms(p99_latency)),
        stat_card("Throughput", format_rate(throughput)),
    ]

    endpoint_table_rows = []
    for row in endpoint_rows:
        status_html = "<br>".join(
            f"{escape(status)}: {count}" for status, count in sorted(row["statuses"].items(), key=lambda item: item[0])
        )
        endpoint_table_rows.append([
            escape(row["endpoint"]),
            escape(format_number(row["count"])),
            escape(format_ms(row["avg"])),
            escape(format_ms(row["p95"])),
            escape(format_ms(row["p99"])),
            escape(format_ms(row["max"])),
            escape(format_pct(row["error_rate"])),
            status_html,
        ])

    status_rows = [[escape(status), escape(format_number(count))] for status, count in sorted(overall_statuses.items())]
    goroutine_rows = [[escape(state), escape(format_number(count))] for state, count in goroutine_states.most_common()]

    top_p95_items = [(row["endpoint"], row["p95"]) for row in endpoint_rows[:12]]
    top_avg_items = sorted(
        [(row["endpoint"], row["avg"]) for row in endpoint_rows],
        key=lambda item: item[1],
        reverse=True,
    )[:12]

    cpu_svg = load_svg_fragment(args.cpu_svg)
    heap_svg = load_svg_fragment(args.heap_svg)

    k6_links = [
        ('artifacts/k6-summary.json', 'k6 summary JSON'),
        ('artifacts/k6-raw.ndjson', 'k6 raw metrics'),
        ('pprof/cpu.pb.gz', 'CPU profile'),
        ('pprof/heap.pb.gz', 'heap profile'),
        ('pprof/goroutine.txt', 'goroutine dump'),
    ]
    links_html = "".join(
        f'<li><a href="{escape(path)}">{escape(label)}</a></li>' for path, label in k6_links
    )

    return f"""<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>IoT Backend Stress Dashboard</title>
  <style>
    :root {{
      --bg: #0b1020;
      --panel: #121a30;
      --panel-alt: #17213d;
      --text: #e7ecf7;
      --muted: #94a3b8;
      --accent: #60a5fa;
      --accent-2: #34d399;
      --accent-3: #f59e0b;
      --danger: #f87171;
      --border: rgba(148, 163, 184, 0.22);
    }}
    * {{
      box-sizing: border-box;
    }}
    body {{
      margin: 0;
      font-family: "Segoe UI", Arial, sans-serif;
      background: linear-gradient(180deg, #08101d 0%, #111827 100%);
      color: var(--text);
      line-height: 1.5;
    }}
    main {{
      max-width: 1400px;
      margin: 0 auto;
      padding: 32px 24px 64px;
    }}
    h1, h2, h3 {{
      margin-top: 0;
    }}
    .subtitle {{
      color: var(--muted);
      margin-bottom: 24px;
    }}
    .panel {{
      background: rgba(18, 26, 48, 0.88);
      border: 1px solid var(--border);
      border-radius: 18px;
      padding: 24px;
      margin-bottom: 24px;
      box-shadow: 0 24px 60px rgba(0, 0, 0, 0.22);
    }}
    .stats-grid {{
      display: grid;
      grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
      gap: 16px;
      margin-bottom: 24px;
    }}
    .stat-card {{
      background: var(--panel-alt);
      border: 1px solid var(--border);
      border-radius: 14px;
      padding: 16px 18px;
    }}
    .stat-label {{
      color: var(--muted);
      font-size: 13px;
      text-transform: uppercase;
      letter-spacing: 0.08em;
      margin-bottom: 8px;
    }}
    .stat-value {{
      font-size: 28px;
      font-weight: 700;
    }}
    .chart-grid {{
      display: grid;
      grid-template-columns: 1fr;
      gap: 18px;
    }}
    .chart-card {{
      background: var(--panel-alt);
      border: 1px solid var(--border);
      border-radius: 16px;
      padding: 16px;
    }}
    .line-chart, .bar-chart {{
      width: 100%;
      height: auto;
      display: block;
    }}
    .grid-line {{
      stroke: rgba(148, 163, 184, 0.18);
      stroke-width: 1;
    }}
    .axis-label {{
      fill: #b8c4da;
      font-size: 11px;
    }}
    .bar-label, .bar-value {{
      fill: #dce5f4;
      font-size: 12px;
    }}
    .table-wrapper {{
      overflow-x: auto;
    }}
    table {{
      width: 100%;
      border-collapse: collapse;
      font-size: 14px;
    }}
    th, td {{
      text-align: left;
      padding: 10px 12px;
      border-bottom: 1px solid rgba(148, 163, 184, 0.12);
      vertical-align: top;
    }}
    th {{
      color: var(--muted);
      font-size: 12px;
      text-transform: uppercase;
      letter-spacing: 0.08em;
    }}
    code, pre {{
      font-family: "SFMono-Regular", Consolas, "Liberation Mono", monospace;
    }}
    pre {{
      background: #0a1224;
      border: 1px solid var(--border);
      border-radius: 12px;
      padding: 14px;
      overflow-x: auto;
      color: #dbe8ff;
      white-space: pre-wrap;
      word-break: break-word;
    }}
    .pprof-summary-list {{
      margin: 0 0 18px;
      padding-left: 18px;
      color: var(--muted);
    }}
    .split-grid {{
      display: grid;
      grid-template-columns: repeat(auto-fit, minmax(480px, 1fr));
      gap: 20px;
    }}
    .links-list {{
      display: grid;
      grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
      gap: 8px 18px;
      padding-left: 18px;
    }}
    a {{
      color: #93c5fd;
    }}
    .svg-wrapper {{
      margin-top: 18px;
      background: white;
      border-radius: 12px;
      padding: 12px;
      overflow: auto;
    }}
    .empty-chart {{
      color: var(--muted);
      padding: 12px;
    }}
    .metadata-grid {{
      display: grid;
      grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
      gap: 10px 16px;
      color: var(--muted);
      margin-bottom: 18px;
    }}
    .section-intro {{
      color: var(--muted);
      margin-top: -8px;
      margin-bottom: 18px;
    }}
  </style>
</head>
<body>
  <main>
    <section class="panel">
      <h1>IoT Backend Stress Test Dashboard</h1>
      <p class="subtitle">Combined k6 load-test and Go pprof profile report.</p>
      <div class="metadata-grid">
        <div><strong>Run ID:</strong> {escape(str(metadata.get("run_id", "unknown")))}</div>
        <div><strong>Base URL:</strong> {escape(str(metadata.get("base_url", "")))}</div>
        <div><strong>pprof URL:</strong> {escape(str(metadata.get("pprof_base_url", "")))}</div>
        <div><strong>Firmware host:</strong> {escape(str(metadata.get("caddy_base_url", "")))}</div>
        <div><strong>Product:</strong> {escape(str(metadata.get("k6_product_name", "")))}</div>
        <div><strong>Load:</strong> {escape(str(metadata.get("k6_vus", "")))} VUs for {escape(str(metadata.get("k6_duration", "")))}</div>
        <div><strong>CPU capture:</strong> {escape(str(metadata.get("pprof_cpu_seconds", "")))}s</div>
      </div>
      <div class="stats-grid">
        {''.join(overview_cards)}
      </div>
      <ul class="links-list">
        {links_html}
      </ul>
    </section>

    <section class="panel">
      <h2>Load Overview</h2>
      <p class="section-intro">These charts summarize how request volume, latency, and failures changed during the run.</p>
      <div class="chart-grid">
        {svg_line_chart("Requests per second", request_series, "#60a5fa")}
        {svg_line_chart("Average latency over time", latency_series, "#34d399")}
        {svg_line_chart("Errors per second", error_series, "#f87171")}
      </div>
    </section>

    <section class="panel">
      <h2>Endpoint Performance</h2>
      <p class="section-intro">Per-endpoint timings derived from tagged k6 request metrics.</p>
      <div class="chart-grid">
        {svg_horizontal_bar_chart("Top endpoints by p95 latency", top_p95_items, "#f59e0b", format_ms)}
        {svg_horizontal_bar_chart("Top endpoints by average latency", top_avg_items, "#60a5fa", format_ms)}
      </div>
      {render_table(
        ["Endpoint", "Requests", "Average", "p95", "p99", "Max", "Error rate", "Statuses"],
        endpoint_table_rows,
      )}
    </section>

    <section class="panel">
      <h2>Status Breakdown</h2>
      <div class="split-grid">
        <div>
          <h3>HTTP statuses</h3>
          {render_table(["Status", "Count"], status_rows)}
        </div>
        <div>
          <h3>Goroutine states</h3>
          {render_table(["State", "Count"], goroutine_rows)}
        </div>
      </div>
    </section>

    {render_pprof_section("CPU Hot Paths", cpu_summary_lines, cpu_rows, cpu_svg)}
    {render_pprof_section("CPU Cumulative View", cpu_cum_summary_lines, cpu_cum_rows, "")}
    {render_pprof_section("Heap Allocation Hot Paths", heap_summary_lines, heap_rows, heap_svg)}
    {render_pprof_section("Heap In-Use View", heap_inuse_summary_lines, heap_inuse_rows, "")}

    <section class="panel">
      <h2>Goroutine Dump Excerpt</h2>
      <p class="section-intro">Total goroutines captured: {escape(str(goroutine_count))}</p>
      <pre>{escape(goroutine_excerpt)}</pre>
    </section>
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
