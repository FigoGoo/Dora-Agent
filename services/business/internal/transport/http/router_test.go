package http

import (
	"context"
	"errors"
	nethttp "net/http"
	"net/http/httptest"
	"testing"
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
