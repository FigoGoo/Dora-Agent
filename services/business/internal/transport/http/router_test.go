package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	nethttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/logger"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/metrics"
)

func TestHealthReadyAndTraceHeader(t *testing.T) {
	router := NewRouter(RouterOptions{Ready: func(ctx context.Context) error { return nil }})
	resp := httptest.NewRecorder()
	req := httptest.NewRequest(nethttp.MethodGet, "/readyz", nil)
	req.Header.Set("X-Trace-Id", "trace-ready")
	router.ServeHTTP(resp, req)
	if resp.Code != nethttp.StatusOK {
		t.Fatalf("ready status=%d body=%s", resp.Code, resp.Body.String())
	}
	if resp.Header().Get("X-Trace-Id") != "trace-ready" {
		t.Fatalf("trace header not propagated")
	}
}

func TestReadyUnready(t *testing.T) {
	router := NewRouter(RouterOptions{Ready: func(ctx context.Context) error { return errors.New("db down") }})
	resp := httptest.NewRecorder()
	req := httptest.NewRequest(nethttp.MethodGet, "/readyz", nil)
	router.ServeHTTP(resp, req)
	if resp.Code != nethttp.StatusServiceUnavailable {
		t.Fatalf("ready status=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestRequestLogIncludesRequiredFieldSet(t *testing.T) {
	var buf bytes.Buffer
	log := logger.New(&buf, "business", "test", "debug")
	router := NewRouter(RouterOptions{Logger: log})
	resp := httptest.NewRecorder()
	req := httptest.NewRequest(nethttp.MethodGet, "/healthz", nil)
	req.Header.Set("X-Trace-Id", "trace-log")
	req.Header.Set("X-Request-Id", "req-log")
	router.ServeHTTP(resp, req)
	if resp.Code != nethttp.StatusOK {
		t.Fatalf("health status=%d body=%s", resp.Code, resp.Body.String())
	}

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("decode log json: %v", err)
	}
	if entry["msg"] != "business_http_request" {
		t.Fatalf("unexpected log message: %#v", entry)
	}
	fields := append([]string{}, logger.BaseFields...)
	fields = append(fields, logger.HTTPRequestFields...)
	for _, field := range fields {
		if _, ok := entry[field]; !ok {
			t.Fatalf("missing log field %q in %#v", field, entry)
		}
	}
	if entry[logger.FieldTraceID] != "trace-log" || entry[logger.FieldRequestID] != "req-log" || entry[logger.FieldMethod] != nethttp.MethodGet || entry[logger.FieldPath] != "/healthz" {
		t.Fatalf("unexpected log fields: %#v", entry)
	}
	if got, ok := entry[logger.FieldStatus].(float64); !ok || got != nethttp.StatusOK {
		t.Fatalf("unexpected status field: %#v", entry[logger.FieldStatus])
	}
	if _, ok := entry[logger.FieldLatencyMS].(float64); !ok {
		t.Fatalf("unexpected latency field: %#v", entry[logger.FieldLatencyMS])
	}
}

func TestMetricsEndpointExposesHTTPRequestCounterGaugeAndHistogram(t *testing.T) {
	registry := metrics.NewRegistry()
	router := NewRouter(RouterOptions{Metrics: registry})
	resp := httptest.NewRecorder()
	req := httptest.NewRequest(nethttp.MethodGet, "/healthz", nil)
	router.ServeHTTP(resp, req)
	if resp.Code != nethttp.StatusOK {
		t.Fatalf("health status=%d body=%s", resp.Code, resp.Body.String())
	}

	metricsResp := httptest.NewRecorder()
	metricsReq := httptest.NewRequest(nethttp.MethodGet, "/metrics", nil)
	router.ServeHTTP(metricsResp, metricsReq)
	if metricsResp.Code != nethttp.StatusOK {
		t.Fatalf("metrics status=%d body=%s", metricsResp.Code, metricsResp.Body.String())
	}
	body := metricsResp.Body.String()
	for _, want := range []string{
		`business_http_requests_total{method="GET",path="/healthz",status="200"} 1`,
		`business_http_request_duration_ms_count{method="GET",path="/healthz",status="200"} 1`,
		`business_http_inflight_requests 1`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected %q in metrics:\n%s", want, body)
		}
	}
}
