package metrics

import (
	"bytes"
	"strings"
	"testing"
)

func TestRegistryExposesCounterGaugeAndHistogram(t *testing.T) {
	registry := NewRegistry()
	labels := map[string]string{"method": "GET", "path": "/healthz", "status": "200"}
	registry.IncCounter(HTTPRequestsTotal, labels, 1)
	registry.IncCounter(HTTPRequestsTotal, labels, 1)
	registry.AddGauge(HTTPInflightRequests, nil, 1)
	registry.AddGauge(HTTPInflightRequests, nil, -1)
	registry.ObserveHistogram(HTTPRequestDuration, labels, 12)
	registry.ObserveHistogram(HTTPRequestDuration, labels, 18)

	var buf bytes.Buffer
	if err := registry.WritePrometheus(&buf); err != nil {
		t.Fatalf("write metrics: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		`business_http_requests_total{method="GET",path="/healthz",status="200"} 2`,
		`business_http_request_duration_ms_count{method="GET",path="/healthz",status="200"} 2`,
		`business_http_request_duration_ms_sum{method="GET",path="/healthz",status="200"} 30`,
		`business_http_inflight_requests 0`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q in metrics:\n%s", want, out)
		}
	}
}
