package metrics

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
)

const (
	HTTPRequestsTotal    = "business_http_requests_total"
	HTTPRequestDuration  = "business_http_request_duration_ms"
	HTTPInflightRequests = "business_http_inflight_requests"
)

type Registry struct {
	mu         sync.Mutex
	counters   map[string]float64
	gauges     map[string]float64
	histograms map[string]*histogram
}

type histogram struct {
	Count int64
	Sum   float64
}

func NewRegistry() *Registry {
	return &Registry{
		counters:   map[string]float64{},
		gauges:     map[string]float64{},
		histograms: map[string]*histogram{},
	}
}

func (r *Registry) IncCounter(name string, labels map[string]string, delta float64) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.counters[seriesKey(name, labels)] += delta
}

func (r *Registry) AddGauge(name string, labels map[string]string, delta float64) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.gauges[seriesKey(name, labels)] += delta
}

func (r *Registry) ObserveHistogram(name string, labels map[string]string, value float64) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	key := seriesKey(name, labels)
	bucket := r.histograms[key]
	if bucket == nil {
		bucket = &histogram{}
		r.histograms[key] = bucket
	}
	bucket.Count++
	bucket.Sum += value
}

func (r *Registry) WritePrometheus(w io.Writer) error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	lines := make([]string, 0, len(r.counters)+len(r.gauges)+len(r.histograms)*2)
	for key, value := range r.counters {
		lines = append(lines, fmt.Sprintf("%s %g", key, value))
	}
	for key, value := range r.gauges {
		lines = append(lines, fmt.Sprintf("%s %g", key, value))
	}
	for key, value := range r.histograms {
		lines = append(lines, fmt.Sprintf("%s %d", metricNameSuffix(key, "_count"), value.Count))
		lines = append(lines, fmt.Sprintf("%s %g", metricNameSuffix(key, "_sum"), value.Sum))
	}
	sort.Strings(lines)
	for _, line := range lines {
		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
	}
	return nil
}

func metricNameSuffix(series string, suffix string) string {
	labelStart := strings.IndexByte(series, '{')
	if labelStart < 0 {
		return series + suffix
	}
	return series[:labelStart] + suffix + series[labelStart:]
}

func seriesKey(name string, labels map[string]string) string {
	if len(labels) == 0 {
		return name
	}
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf(`%s="%s"`, key, sanitizeLabelValue(labels[key])))
	}
	return name + "{" + strings.Join(parts, ",") + "}"
}

func sanitizeLabelValue(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `"`, `\"`)
	value = strings.ReplaceAll(value, "\n", `\n`)
	return value
}
