package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"
)

type rawPoint struct {
	Type   string       `json:"type"`
	Metric string       `json:"metric"`
	Data   rawPointData `json:"data"`
}

type rawPointData struct {
	Time  string            `json:"time"`
	Value float64           `json:"value"`
	Tags  map[string]string `json:"tags,omitempty"`
}

type MetricCollector struct {
	rawPath     string
	summaryPath string
	rawFile     *os.File
	rawWriter   *bufio.Writer
	mu          sync.Mutex
	startedAt   time.Time
	counters    map[string]float64
	trends      map[string][]float64
	httpReqs    float64
	httpFailed  float64
	checkPasses int
	checkFails  int
}

func newMetricCollector(rawPath, summaryPath string) (*MetricCollector, error) {
	rawFile, err := os.Create(rawPath)
	if err != nil {
		return nil, fmt.Errorf("create raw metrics file: %w", err)
	}
	return &MetricCollector{
		rawPath:     rawPath,
		summaryPath: summaryPath,
		rawFile:     rawFile,
		rawWriter:   bufio.NewWriterSize(rawFile, 1<<20),
		startedAt:   time.Now().UTC(),
		counters:    map[string]float64{},
		trends:      map[string][]float64{},
	}, nil
}

func (m *MetricCollector) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.rawWriter != nil {
		if err := m.rawWriter.Flush(); err != nil {
			return err
		}
	}
	if m.rawFile != nil {
		return m.rawFile.Close()
	}
	return nil
}

func (m *MetricCollector) RecordHTTP(scenario, name string, startedAt time.Time, statusCode int, expected bool, err error) {
	durationMs := time.Since(startedAt).Seconds() * 1000
	statusText := fmt.Sprintf("%d", statusCode)
	if statusCode == 0 {
		statusText = "0"
	}
	tags := map[string]string{
		"scenario":          scenario,
		"name":              name,
		"endpoint":          name,
		"status":            statusText,
		"expected_response": boolTag(expected),
	}
	m.recordPoint("http_reqs", 1, tags)
	m.recordPoint("http_req_duration", durationMs, tags)
	failed := 0.0
	if !expected || err != nil {
		failed = 1.0
	}
	m.recordPoint("http_req_failed", failed, tags)

	m.mu.Lock()
	m.httpReqs += 1
	m.httpFailed += failed
	m.mu.Unlock()
}

func (m *MetricCollector) RecordCheck(scenario, name string, ok bool) {
	value := 0.0
	if ok {
		value = 1
	}
	m.recordPoint("checks", value, map[string]string{
		"scenario": scenario,
		"name":     name,
	})
	m.mu.Lock()
	if ok {
		m.checkPasses += 1
	} else {
		m.checkFails += 1
	}
	m.mu.Unlock()
}

func (m *MetricCollector) RecordCounter(metric string, value float64, scenario string) {
	m.recordPoint(metric, value, map[string]string{"scenario": scenario})
	m.mu.Lock()
	m.counters[metric] += value
	m.mu.Unlock()
}

func (m *MetricCollector) RecordTrend(metric string, value float64, scenario string) {
	m.recordPoint(metric, value, map[string]string{"scenario": scenario})
	m.mu.Lock()
	m.trends[metric] = append(m.trends[metric], value)
	m.mu.Unlock()
}

func (m *MetricCollector) WriteSummary() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	elapsed := time.Since(m.startedAt).Seconds()
	if elapsed <= 0 {
		elapsed = 1
	}

	metrics := map[string]map[string]map[string]float64{
		"http_reqs": {
			"values": {
				"count": m.httpReqs,
				"rate":  m.httpReqs / elapsed,
			},
		},
		"http_req_failed": {
			"values": {
				"rate": ratio(m.httpFailed, m.httpReqs),
			},
		},
		"checks": {
			"values": {
				"passes": float64(m.checkPasses),
				"fails":  float64(m.checkFails),
				"value":  ratio(float64(m.checkPasses), float64(m.checkPasses+m.checkFails)),
			},
		},
	}

	for name, value := range m.counters {
		metrics[name] = map[string]map[string]float64{
			"values": {
				"count": value,
				"rate":  value / elapsed,
			},
		}
	}

	for name, values := range m.trends {
		metrics[name] = map[string]map[string]float64{
			"values": trendValues(values),
		}
	}

	payload := map[string]any{
		"metrics": metrics,
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal summary: %w", err)
	}
	return os.WriteFile(m.summaryPath, data, 0o644)
}

func (m *MetricCollector) recordPoint(metric string, value float64, tags map[string]string) {
	point := rawPoint{
		Type:   "Point",
		Metric: metric,
		Data: rawPointData{
			Time:  time.Now().UTC().Format(time.RFC3339Nano),
			Value: value,
			Tags:  tags,
		},
	}
	encoded, err := json.Marshal(point)
	if err != nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	_, _ = m.rawWriter.Write(encoded)
	_ = m.rawWriter.WriteByte('\n')
}

func (m *MetricCollector) PrintPhaseSummary(phase string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	elapsed := time.Since(m.startedAt).Round(time.Millisecond)
	fmt.Printf("Phase %s summary\n", phase)
	fmt.Printf("  Requests: %.0f total, %.2f%% failures\n", m.httpReqs, ratio(m.httpFailed, m.httpReqs)*100)
	fmt.Printf("  Checks: %d passed, %d failed\n", m.checkPasses, m.checkFails)

	keys := make([]string, 0, len(m.trends))
	for key := range m.trends {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		stats := trendValues(m.trends[key])
		fmt.Printf("  %s: avg=%.2f p95=%.2f p99=%.2f max=%.2f\n",
			key,
			stats["avg"],
			stats["p(95)"],
			stats["p(99)"],
			stats["max"],
		)
	}

	counterKeys := make([]string, 0, len(m.counters))
	for key := range m.counters {
		counterKeys = append(counterKeys, key)
	}
	sort.Strings(counterKeys)
	for _, key := range counterKeys {
		fmt.Printf("  %s: %.0f\n", key, m.counters[key])
	}
	fmt.Printf("  Elapsed: %s\n", elapsed)
}

func ratio(numerator, denominator float64) float64 {
	if denominator <= 0 {
		return 0
	}
	return numerator / denominator
}

func trendValues(values []float64) map[string]float64 {
	result := map[string]float64{
		"avg":   0,
		"min":   0,
		"med":   0,
		"p(90)": 0,
		"p(95)": 0,
		"p(99)": 0,
		"max":   0,
	}
	if len(values) == 0 {
		return result
	}
	sortedValues := append([]float64(nil), values...)
	sort.Float64s(sortedValues)
	total := 0.0
	for _, value := range sortedValues {
		total += value
	}
	result["avg"] = total / float64(len(sortedValues))
	result["min"] = sortedValues[0]
	result["med"] = percentile(sortedValues, 50)
	result["p(90)"] = percentile(sortedValues, 90)
	result["p(95)"] = percentile(sortedValues, 95)
	result["p(99)"] = percentile(sortedValues, 99)
	result["max"] = sortedValues[len(sortedValues)-1]
	return result
}

func percentile(values []float64, percentileValue float64) float64 {
	if len(values) == 0 {
		return 0
	}
	if len(values) == 1 {
		return values[0]
	}
	index := (float64(len(values)) - 1) * percentileValue / 100
	lower := int(index)
	upper := lower
	if index != float64(lower) {
		upper = lower + 1
	}
	if upper >= len(values) {
		return values[len(values)-1]
	}
	if lower == upper {
		return values[lower]
	}
	weight := index - float64(lower)
	return values[lower] + (values[upper]-values[lower])*weight
}

func boolTag(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
