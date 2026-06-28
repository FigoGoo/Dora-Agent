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
