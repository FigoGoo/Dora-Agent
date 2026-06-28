package http

import (
	"context"
	"errors"
	"io"
	nethttp "net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthAndReady(t *testing.T) {
	router := NewRouter(RouterOptions{Ready: func(ctx context.Context) error { return nil }})

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(nethttp.MethodGet, "/healthz", nil)
	router.ServeHTTP(resp, req)
	if resp.Code != nethttp.StatusOK {
		t.Fatalf("health status=%d body=%s", resp.Code, resp.Body.String())
	}

	resp = httptest.NewRecorder()
	req = httptest.NewRequest(nethttp.MethodGet, "/readyz", nil)
	router.ServeHTTP(resp, req)
	if resp.Code != nethttp.StatusOK {
		t.Fatalf("ready status=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestTraceparentHeaderTakesPrecedence(t *testing.T) {
	router := NewRouter(RouterOptions{Ready: func(ctx context.Context) error { return nil }})
	resp := httptest.NewRecorder()
	req := httptest.NewRequest(nethttp.MethodGet, "/readyz", nil)
	req.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	req.Header.Set("X-Trace-Id", "legacy-trace")
	router.ServeHTTP(resp, req)
	if resp.Code != nethttp.StatusOK {
		t.Fatalf("ready status=%d body=%s", resp.Code, resp.Body.String())
	}
	if got := resp.Header().Get("X-Trace-Id"); got != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("x-trace-id=%q", got)
	}
	if got := resp.Header().Get("traceparent"); got != "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01" {
		t.Fatalf("traceparent=%q", got)
	}
}

func TestReadyUnready(t *testing.T) {
	router := NewRouter(RouterOptions{Ready: func(ctx context.Context) error { return errors.New("db down") }})
	resp := httptest.NewRecorder()
	req := httptest.NewRequest(nethttp.MethodGet, "/readyz", nil)
	router.ServeHTTP(resp, req)
	body, _ := io.ReadAll(resp.Body)
	if resp.Code != nethttp.StatusServiceUnavailable {
		t.Fatalf("ready status=%d body=%s", resp.Code, string(body))
	}
}
