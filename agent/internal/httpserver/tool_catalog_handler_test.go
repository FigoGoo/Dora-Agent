package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/agent/internal/httpidentity"
	"github.com/FigoGoo/Dora-Agent/agent/internal/tool"
	"github.com/gin-gonic/gin"
)

// toolCatalogTestVerifier 记录 Handler 传入的规范身份请求，并返回预置的 Claims 或错误。
type toolCatalogTestVerifier struct {
	claims   httpidentity.Claims
	err      error
	requests []httpidentity.Request
}

// Verify 保留 Handler 的 Scope/Target/Session 绑定输入，供契约断言使用。
func (verifier *toolCatalogTestVerifier) Verify(_ context.Context, request httpidentity.Request) (httpidentity.Claims, error) {
	verifier.requests = append(verifier.requests, request)
	return verifier.claims, verifier.err
}

// newToolCatalogTestRouter 创建只注册 Tool Catalog 路径的测试 Router。
func newToolCatalogTestRouter(t *testing.T, verifier IdentityVerifier) (*ToolCatalogHandler, *gin.Engine) {
	t.Helper()
	gin.SetMode(gin.ReleaseMode)
	handler, err := NewToolCatalogHandler(verifier, tool.NewCatalogProvider(), serverTestIDs{})
	if err != nil {
		t.Fatalf("create Tool Catalog test handler: %v", err)
	}
	router := gin.New()
	handler.Register(router)
	return handler, router
}

// TestToolCatalogHandlerReturnsExactNoStoreCatalog 验证成功响应的请求标识、六项 exact catalog、大小上限与禁止缓存语义。
func TestToolCatalogHandlerReturnsExactNoStoreCatalog(t *testing.T) {
	verifier := &toolCatalogTestVerifier{claims: httpidentity.Claims{
		RequestID:       "019f0000-0000-7000-8000-000000000011",
		PrincipalUserID: "019f0000-0000-7000-8000-000000000012",
		ProjectID:       "019f0000-0000-7000-8000-000000000013",
		AgentSessionID:  handlerSessionID,
		Scope:           httpidentity.ScopeToolsRead,
		ExpiresAt:       time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC),
	}}
	_, router := newToolCatalogTestRouter(t, verifier)
	target := "/api/v1/agent/sessions/" + handlerSessionID + "/tools"
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, target, nil))
	if recorder.Code != http.StatusOK || recorder.Header().Get("Cache-Control") != "no-store" ||
		recorder.Header().Get("Content-Type") != "application/json; charset=utf-8" {
		t.Fatalf("Tool Catalog response drifted: status=%d headers=%v body=%s", recorder.Code, recorder.Header(), recorder.Body.String())
	}
	if recorder.Body.Len() > maxToolCatalogResponseBytes {
		t.Fatalf("Tool Catalog response exceeded bound: bytes=%d", recorder.Body.Len())
	}
	decoder := json.NewDecoder(recorder.Body)
	decoder.DisallowUnknownFields()
	var response tool.DefinitionCatalogResponse
	if err := decoder.Decode(&response); err != nil {
		t.Fatalf("decode Tool Catalog response: %v", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		t.Fatalf("Tool Catalog response has trailing JSON: %v", err)
	}
	wantItems := tool.NewCatalogProvider().ListDefinitions()
	if response.SchemaVersion != tool.DefinitionCatalogSchemaVersionV1 || response.RequestID != verifier.claims.RequestID ||
		!reflect.DeepEqual(response.Items, wantItems) {
		t.Fatalf("Tool Catalog response is not exact: %+v", response)
	}
	if len(verifier.requests) != 1 {
		t.Fatalf("identity verifier calls=%d want=1", len(verifier.requests))
	}
	identityRequest := verifier.requests[0]
	if identityRequest.Method != http.MethodGet || identityRequest.CanonicalTarget != target ||
		identityRequest.Scope != httpidentity.ScopeToolsRead || identityRequest.AgentSessionID != handlerSessionID {
		t.Fatalf("Tool Catalog identity binding drifted: %+v", identityRequest)
	}
}

// TestToolCatalogHandlerRejectsNonCanonicalRouteBeforeIdentity 验证 UUIDv7、Query 和 Escaped Path 在消耗一次性 Nonce 之前失败关闭。
func TestToolCatalogHandlerRejectsNonCanonicalRouteBeforeIdentity(t *testing.T) {
	canonicalTarget := "/api/v1/agent/sessions/" + handlerSessionID + "/tools"
	for _, testCase := range []struct {
		name   string
		target string
	}{
		{name: "non UUIDv7", target: "/api/v1/agent/sessions/550e8400-e29b-41d4-a716-446655440000/tools"},
		{name: "uppercase UUID", target: "/api/v1/agent/sessions/019F0000-0000-7000-8000-000000000005/tools"},
		{name: "query forbidden", target: canonicalTarget + "?unexpected=1"},
		{name: "empty query marker forbidden", target: canonicalTarget + "?"},
		{name: "escaped path forbidden", target: "/api/v1/agent/sessions/%30" + handlerSessionID[1:] + "/tools"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			verifier := &toolCatalogTestVerifier{}
			_, router := newToolCatalogTestRouter(t, verifier)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, testCase.target, nil))
			if recorder.Code != http.StatusBadRequest || recorder.Header().Get("Cache-Control") != "no-store" ||
				!strings.Contains(recorder.Body.String(), `"code":"INVALID_ARGUMENT"`) {
				t.Fatalf("non-canonical route was not rejected: status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			if len(verifier.requests) != 0 {
				t.Fatalf("non-canonical route consumed identity Nonce: requests=%+v", verifier.requests)
			}
		})
	}
}

// TestToolCatalogHandlerMapsIdentityFailuresWithoutCatalog 验证无效断言与 Nonce 依赖故障返回不同稳定错误，且不泄漏目录。
func TestToolCatalogHandlerMapsIdentityFailuresWithoutCatalog(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		err       error
		status    int
		code      string
		retryable bool
	}{
		{name: "invalid", err: httpidentity.ErrInvalid, status: http.StatusUnauthorized, code: errorCodeInternalIdentityInvalid},
		{name: "unavailable", err: httpidentity.ErrUnavailable, status: http.StatusServiceUnavailable, code: errorCodeIdentityAssertionUnavailable, retryable: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			verifier := &toolCatalogTestVerifier{err: testCase.err}
			_, router := newToolCatalogTestRouter(t, verifier)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet,
				"/api/v1/agent/sessions/"+handlerSessionID+"/tools", nil))
			var response ErrorResponse
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatalf("decode identity failure: %v", err)
			}
			if recorder.Code != testCase.status || recorder.Header().Get("Cache-Control") != "no-store" ||
				response.Error.Code != testCase.code || response.Error.Retryable != testCase.retryable {
				t.Fatalf("identity failure drifted: status=%d response=%+v", recorder.Code, response)
			}
			if strings.Contains(recorder.Body.String(), tool.DefinitionCatalogSchemaVersionV1) ||
				strings.Contains(recorder.Body.String(), "plan_creation_spec") {
				t.Fatalf("identity failure leaked Tool Catalog: %s", recorder.Body.String())
			}
		})
	}
}

// TestNewToolCatalogHandlerRequiresDependencies 验证组装缺少身份、Provider 或 RequestID 生成器时阻止启动。
func TestNewToolCatalogHandlerRequiresDependencies(t *testing.T) {
	verifier := &toolCatalogTestVerifier{}
	provider := tool.NewCatalogProvider()
	for _, testCase := range []struct {
		name     string
		verifier IdentityVerifier
		provider *tool.CatalogProvider
		ids      IDGenerator
	}{
		{name: "missing verifier", provider: provider, ids: serverTestIDs{}},
		{name: "missing provider", verifier: verifier, ids: serverTestIDs{}},
		{name: "missing IDs", verifier: verifier, provider: provider},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := NewToolCatalogHandler(testCase.verifier, testCase.provider, testCase.ids); err == nil {
				t.Fatal("missing Tool Catalog dependency was accepted")
			}
		})
	}
}
