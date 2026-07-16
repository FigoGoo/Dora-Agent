package httpserver

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/worker/internal/config"
	"github.com/FigoGoo/Dora-Agent/worker/internal/health"
)

// TestReadinessTransitions 验证 Worker 依赖完成前后 Readiness 的失败关闭行为。
func TestReadinessTransitions(t *testing.T) {
	state := health.NewState()
	server, err := New(config.HTTPConfig{
		Address: ":0", HeaderTimeout: time.Second, ReadTimeout: time.Second,
		WriteTimeout: time.Second, IdleTimeout: time.Second, MaxHeaderBytes: 1024,
	}, config.ServiceConfig{Name: "worker-test", Version: "test"}, state)
	if err != nil {
		t.Fatalf("创建测试服务器失败: %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("未就绪状态码错误: got %d", recorder.Code)
	}
	state.SetReady(true)
	recorder = httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("就绪状态码错误: got %d", recorder.Code)
	}
}
