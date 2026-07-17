package httpidentity

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/agent/internal/config"
)

const fixedCanonical = "agent_http_identity_assertion.v1\n" +
	"dora-business-service\n" +
	"dora.agent.http.v1\n" +
	"test-2026-07-a\n" +
	"019f0000-0000-7000-8000-000000000001\n" +
	"GET\n" +
	"/api/v1/agent/sessions/019f0000-0000-7000-8000-000000000005/events?after_seq=42\n" +
	"019f0000-0000-7000-8000-000000000002\n" +
	"019f0000-0000-7000-8000-000000000003\n" +
	"7\n" +
	"019f0000-0000-7000-8000-000000000004\n" +
	"019f0000-0000-7000-8000-000000000005\n" +
	"agent.session.events.read\n" +
	"1784011500123\n" +
	"1784011530123\n" +
	"AAECAwQFBgcICQoLDA0ODw"

type fixedClock struct{ now time.Time }

func (clock fixedClock) Now() time.Time { return clock.now }

type memoryReplayStore struct {
	mutex sync.Mutex
	seen  map[string]struct{}
	err   error
}

func (store *memoryReplayStore) ClaimIdentityNonce(_ context.Context, kid string, nonce []byte, _ time.Duration) (bool, error) {
	if store.err != nil {
		return false, store.err
	}
	key := kid + ":" + hex.EncodeToString(nonce)
	store.mutex.Lock()
	defer store.mutex.Unlock()
	if _, exists := store.seen[key]; exists {
		return false, nil
	}
	store.seen[key] = struct{}{}
	return true, nil
}

// TestVerifierMatchesFrozenVector 验证冻结 Canonical、Key 和 HMAC 向量以及全部可信 Claims。
func TestVerifierMatchesFrozenVector(t *testing.T) {
	key := fixedKey()
	signature := signCanonical(key, fixedCanonical)
	if signature != "a7bd082fd06e94d0e09eff76608f240dde6390692c714c9e23fc4983c736c374" {
		t.Fatalf("冻结签名=%s", signature)
	}
	verifier := newFixedVerifier(t, &memoryReplayStore{seen: make(map[string]struct{})})
	claims, err := verifier.Verify(context.Background(), fixedRequest(fixedHeaders(fixedCanonical, "test-2026-07-a", signature)))
	if err != nil {
		t.Fatalf("校验冻结向量失败: %v", err)
	}
	if claims.RequestID != "019f0000-0000-7000-8000-000000000001" ||
		claims.PrincipalUserID != "019f0000-0000-7000-8000-000000000002" ||
		claims.ProjectID != "019f0000-0000-7000-8000-000000000004" ||
		claims.AgentSessionID != "019f0000-0000-7000-8000-000000000005" || claims.WebSessionVersion != 7 {
		t.Fatalf("Claims 不符合冻结向量: %+v", claims)
	}
}

// TestVerifierFailsClosedAcrossBindingsAndDependency 验证路径绑定、重复 Header、Nonce 重放和 Redis 故障均失败关闭。
func TestVerifierFailsClosedAcrossBindingsAndDependency(t *testing.T) {
	signature := signCanonical(fixedKey(), fixedCanonical)
	headers := fixedHeaders(fixedCanonical, "test-2026-07-a", signature)
	store := &memoryReplayStore{seen: make(map[string]struct{})}
	verifier := newFixedVerifier(t, store)
	if _, err := verifier.Verify(context.Background(), fixedRequest(headers)); err != nil {
		t.Fatalf("首次校验失败: %v", err)
	}
	if _, err := verifier.Verify(context.Background(), fixedRequest(headers)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Nonce 重放错误=%v", err)
	}

	wrongTarget := fixedRequest(headers.Clone())
	wrongTarget.CanonicalTarget = "/api/v1/agent/sessions/019f0000-0000-7000-8000-000000000005/workspace"
	if _, err := newFixedVerifier(t, &memoryReplayStore{seen: make(map[string]struct{})}).Verify(context.Background(), wrongTarget); !errors.Is(err, ErrInvalid) {
		t.Fatalf("跨路径断言错误=%v", err)
	}

	multiple := headers.Clone()
	multiple.Add(HeaderKeyVersion, "test-2026-07-a")
	if _, err := newFixedVerifier(t, &memoryReplayStore{seen: make(map[string]struct{})}).Verify(context.Background(), fixedRequest(multiple)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("重复 kid Header 错误=%v", err)
	}

	unavailable := newFixedVerifier(t, &memoryReplayStore{seen: make(map[string]struct{}), err: errors.New("redis down")})
	if _, err := unavailable.Verify(context.Background(), fixedRequest(headers)); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Redis 故障错误=%v", err)
	}
}

// TestVerifierBindsToolCatalogToExactScopeTargetAndMethod 验证 Tool Catalog 断言只能用于规范 GET、tools Target 和专用 Scope。
func TestVerifierBindsToolCatalogToExactScopeTargetAndMethod(t *testing.T) {
	const target = "/api/v1/agent/sessions/019f0000-0000-7000-8000-000000000005/tools"
	canonical := strings.Replace(fixedCanonical,
		"/api/v1/agent/sessions/019f0000-0000-7000-8000-000000000005/events?after_seq=42", target, 1)
	canonical = strings.Replace(canonical, ScopeEventsRead, ScopeToolsRead, 1)
	headers := fixedHeaders(canonical, "test-2026-07-a", signCanonical(fixedKey(), canonical))
	request := fixedRequest(headers)
	request.CanonicalTarget = target
	request.Scope = ScopeToolsRead
	claims, err := newFixedVerifier(t, &memoryReplayStore{seen: make(map[string]struct{})}).Verify(context.Background(), request)
	if err != nil || claims.Scope != ScopeToolsRead || claims.AgentSessionID != request.AgentSessionID {
		t.Fatalf("Tool Catalog exact assertion rejected: claims=%+v err=%v", claims, err)
	}

	for _, mutate := range []func(*Request){
		func(candidate *Request) { candidate.Scope = ScopeWorkspaceRead },
		func(candidate *Request) { candidate.CanonicalTarget += "/extra" },
		func(candidate *Request) { candidate.Method = http.MethodPost },
	} {
		candidate := request
		candidate.Headers = headers.Clone()
		mutate(&candidate)
		if _, err := newFixedVerifier(t, &memoryReplayStore{seen: make(map[string]struct{})}).Verify(context.Background(), candidate); !errors.Is(err, ErrInvalid) {
			t.Fatalf("cross-bound Tool Catalog assertion was accepted: request=%+v err=%v", candidate, err)
		}
	}
}

// TestVerifierBindsCreationSpecPreviewToExactPOSTScopeAndInternalTarget 验证写断言不能降级为 GET、外部路径或其他 Scope。
func TestVerifierBindsCreationSpecPreviewToExactPOSTScopeAndInternalTarget(t *testing.T) {
	const sessionID = "019f0000-0000-7000-8000-000000000005"
	const target = "/internal/v1/workspaces/sessions/" + sessionID + "/creation-spec-previews"
	canonical := strings.Replace(fixedCanonical,
		"/api/v1/agent/sessions/"+sessionID+"/events?after_seq=42", target, 1)
	canonical = strings.Replace(canonical, "\nGET\n", "\nPOST\n", 1)
	canonical = strings.Replace(canonical, ScopeEventsRead, ScopeCreationSpecPreviewWrite, 1)
	headers := fixedHeaders(canonical, "test-2026-07-a", signCanonical(fixedKey(), canonical))
	request := Request{
		Headers: headers, Method: http.MethodPost, CanonicalTarget: target,
		Scope: ScopeCreationSpecPreviewWrite, AgentSessionID: sessionID,
	}
	claims, err := newFixedVerifier(t, &memoryReplayStore{seen: make(map[string]struct{})}).Verify(context.Background(), request)
	if err != nil || claims.Scope != ScopeCreationSpecPreviewWrite || claims.AgentSessionID != sessionID {
		t.Fatalf("CreationSpec Preview POST assertion rejected: claims=%+v err=%v", claims, err)
	}

	for _, mutate := range []func(*Request){
		func(candidate *Request) { candidate.Method = http.MethodGet },
		func(candidate *Request) { candidate.Scope = ScopeEventsRead },
		func(candidate *Request) {
			candidate.CanonicalTarget = "/api/v1/agent/sessions/" + sessionID + "/creation-spec-previews"
		},
		func(candidate *Request) { candidate.CanonicalTarget += "?preview=1" },
		func(candidate *Request) { candidate.AgentSessionID = "019f0000-0000-7000-8000-000000000006" },
	} {
		candidate := request
		candidate.Headers = headers.Clone()
		mutate(&candidate)
		if _, err := newFixedVerifier(t, &memoryReplayStore{seen: make(map[string]struct{})}).Verify(context.Background(), candidate); !errors.Is(err, ErrInvalid) {
			t.Fatalf("cross-bound CreationSpec Preview assertion accepted: request=%+v err=%v", candidate, err)
		}
	}
}

// TestVerifierBindsAnalyzeMaterialsPreviewToExactPOSTScopeAndInternalTarget 验证素材分析写断言不能跨 Method、路径或 Scope 重放。
func TestVerifierBindsAnalyzeMaterialsPreviewToExactPOSTScopeAndInternalTarget(t *testing.T) {
	const sessionID = "019f0000-0000-7000-8000-000000000005"
	const target = "/internal/v1/workspaces/sessions/" + sessionID + "/analyze-materials-previews"
	canonical := strings.Replace(fixedCanonical,
		"/api/v1/agent/sessions/"+sessionID+"/events?after_seq=42", target, 1)
	canonical = strings.Replace(canonical, "\nGET\n", "\nPOST\n", 1)
	canonical = strings.Replace(canonical, ScopeEventsRead, ScopeAnalyzeMaterialsPreviewWrite, 1)
	headers := fixedHeaders(canonical, "test-2026-07-a", signCanonical(fixedKey(), canonical))
	request := Request{
		Headers: headers, Method: http.MethodPost, CanonicalTarget: target,
		Scope: ScopeAnalyzeMaterialsPreviewWrite, AgentSessionID: sessionID,
	}
	claims, err := newFixedVerifier(t, &memoryReplayStore{seen: make(map[string]struct{})}).Verify(context.Background(), request)
	if err != nil || claims.Scope != ScopeAnalyzeMaterialsPreviewWrite || claims.AgentSessionID != sessionID {
		t.Fatalf("Analyze Materials Preview POST assertion rejected: claims=%+v err=%v", claims, err)
	}

	for _, mutate := range []func(*Request){
		func(candidate *Request) { candidate.Method = http.MethodGet },
		func(candidate *Request) { candidate.Scope = ScopeCreationSpecPreviewWrite },
		func(candidate *Request) {
			candidate.CanonicalTarget = "/api/v1/agent/sessions/" + sessionID + "/analyze-materials-previews"
		},
		func(candidate *Request) { candidate.CanonicalTarget += "?preview=1" },
		func(candidate *Request) { candidate.AgentSessionID = "019f0000-0000-7000-8000-000000000006" },
	} {
		candidate := request
		candidate.Headers = headers.Clone()
		mutate(&candidate)
		if _, err := newFixedVerifier(t, &memoryReplayStore{seen: make(map[string]struct{})}).Verify(context.Background(), candidate); !errors.Is(err, ErrInvalid) {
			t.Fatalf("cross-bound Analyze Materials Preview assertion accepted: request=%+v err=%v", candidate, err)
		}
	}
}

// TestVerifierBindsPlanStoryboardPreviewToExactPOSTScopeAndInternalTarget 验证 Storyboard 写断言不能跨 Method、路径或 Scope 重放。
func TestVerifierBindsPlanStoryboardPreviewToExactPOSTScopeAndInternalTarget(t *testing.T) {
	const sessionID = "019f0000-0000-7000-8000-000000000005"
	const target = "/internal/v1/workspaces/sessions/" + sessionID + "/plan-storyboard-previews"
	canonical := strings.Replace(fixedCanonical,
		"/api/v1/agent/sessions/"+sessionID+"/events?after_seq=42", target, 1)
	canonical = strings.Replace(canonical, "\nGET\n", "\nPOST\n", 1)
	canonical = strings.Replace(canonical, ScopeEventsRead, ScopePlanStoryboardPreviewWrite, 1)
	headers := fixedHeaders(canonical, "test-2026-07-a", signCanonical(fixedKey(), canonical))
	request := Request{
		Headers: headers, Method: http.MethodPost, CanonicalTarget: target,
		Scope: ScopePlanStoryboardPreviewWrite, AgentSessionID: sessionID,
	}
	claims, err := newFixedVerifier(t, &memoryReplayStore{seen: make(map[string]struct{})}).Verify(context.Background(), request)
	if err != nil || claims.Scope != ScopePlanStoryboardPreviewWrite || claims.AgentSessionID != sessionID {
		t.Fatalf("Plan Storyboard Preview POST assertion rejected: claims=%+v err=%v", claims, err)
	}

	for _, mutate := range []func(*Request){
		func(candidate *Request) { candidate.Method = http.MethodGet },
		func(candidate *Request) { candidate.Scope = ScopeCreationSpecPreviewWrite },
		func(candidate *Request) {
			candidate.CanonicalTarget = "/api/v1/agent/sessions/" + sessionID + "/plan-storyboard-previews"
		},
		func(candidate *Request) { candidate.CanonicalTarget += "?preview=1" },
		func(candidate *Request) { candidate.AgentSessionID = "019f0000-0000-7000-8000-000000000006" },
	} {
		candidate := request
		candidate.Headers = headers.Clone()
		mutate(&candidate)
		if _, err := newFixedVerifier(t, &memoryReplayStore{seen: make(map[string]struct{})}).Verify(context.Background(), candidate); !errors.Is(err, ErrInvalid) {
			t.Fatalf("cross-bound Plan Storyboard Preview assertion accepted: request=%+v err=%v", candidate, err)
		}
	}
}

// TestVerifierRejectsSignedButNonAllowlistedBinding 证明即使签名和 Request 自洽，也不能绕过 Scope 的路径白名单。
func TestVerifierRejectsSignedButNonAllowlistedBinding(t *testing.T) {
	const target = "/api/v1/agent/sessions/019f0000-0000-7000-8000-000000000005/arbitrary"
	canonical := strings.Replace(fixedCanonical,
		"/api/v1/agent/sessions/019f0000-0000-7000-8000-000000000005/events?after_seq=42", target, 1)
	headers := fixedHeaders(canonical, "test-2026-07-a", signCanonical(fixedKey(), canonical))
	request := fixedRequest(headers)
	request.CanonicalTarget = target
	if _, err := newFixedVerifier(t, &memoryReplayStore{seen: make(map[string]struct{})}).Verify(context.Background(), request); !errors.Is(err, ErrInvalid) {
		t.Fatalf("非白名单但自洽签名被接受: err=%v", err)
	}
}

// TestVerifierConcurrentNonceOnlyOneSucceeds 验证一百个并发同 Nonce 中只有一次能通过原子重放保护。
func TestVerifierConcurrentNonceOnlyOneSucceeds(t *testing.T) {
	store := &memoryReplayStore{seen: make(map[string]struct{})}
	verifier := newFixedVerifier(t, store)
	headers := fixedHeaders(fixedCanonical, "test-2026-07-a", signCanonical(fixedKey(), fixedCanonical))
	var successes atomic.Int64
	var waitGroup sync.WaitGroup
	for range 100 {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			if _, err := verifier.Verify(context.Background(), fixedRequest(headers)); err == nil {
				successes.Add(1)
			} else if !errors.Is(err, ErrInvalid) {
				t.Errorf("并发校验返回非预期错误: %v", err)
			}
		}()
	}
	waitGroup.Wait()
	if successes.Load() != 1 {
		t.Fatalf("并发成功数=%d，want 1", successes.Load())
	}
}

// TestVerifierAcceptsPreviousKeyWithoutKeyTraversal 验证轮换窗口接受明确 previous kid，未知 kid 不会遍历 active/previous 试签。
func TestVerifierAcceptsPreviousKeyWithoutKeyTraversal(t *testing.T) {
	previousKey := []byte("abcdef0123456789abcdef0123456789")
	previousCanonical := strings.Replace(fixedCanonical, "test-2026-07-a", "test-2026-06-z", 1)
	verifier, err := NewVerifier(config.HTTPIdentityConfig{
		ActiveKeyVersion: "test-2026-07-a", ActiveSecret: fixedKey(),
		PreviousKeyVersion: "test-2026-06-z", PreviousSecret: previousKey,
		MaxClockSkew: 5 * time.Second, ReplayTimeout: time.Second,
	}, &memoryReplayStore{seen: make(map[string]struct{})}, fixedClock{now: time.UnixMilli(1784011510000).UTC()})
	if err != nil {
		t.Fatalf("创建轮换 Verifier 失败: %v", err)
	}
	headers := fixedHeaders(previousCanonical, "test-2026-06-z", signCanonical(previousKey, previousCanonical))
	if claims, err := verifier.Verify(context.Background(), fixedRequest(headers)); err != nil || claims.KeyVersion != "test-2026-06-z" {
		t.Fatalf("previous kid claims=%+v err=%v", claims, err)
	}
	unknown := fixedHeaders(previousCanonical, "test-unknown", signCanonical(previousKey, previousCanonical))
	if _, err := verifier.Verify(context.Background(), fixedRequest(unknown)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("未知 kid 错误=%v", err)
	}
}

func newFixedVerifier(t *testing.T, store ReplayStore) *Verifier {
	t.Helper()
	verifier, err := NewVerifier(config.HTTPIdentityConfig{
		ActiveKeyVersion: "test-2026-07-a", ActiveSecret: fixedKey(),
		MaxClockSkew: 5 * time.Second, ReplayTimeout: time.Second,
	}, store, fixedClock{now: time.UnixMilli(1784011510000).UTC()})
	if err != nil {
		t.Fatalf("创建固定 Verifier 失败: %v", err)
	}
	return verifier
}

func fixedRequest(headers http.Header) Request {
	return Request{
		Headers: headers, Method: http.MethodGet,
		CanonicalTarget: "/api/v1/agent/sessions/019f0000-0000-7000-8000-000000000005/events?after_seq=42",
		Scope:           ScopeEventsRead, AgentSessionID: "019f0000-0000-7000-8000-000000000005",
	}
}

func fixedHeaders(canonical, kid, signature string) http.Header {
	headers := make(http.Header)
	headers.Set(HeaderAssertion, base64.RawURLEncoding.EncodeToString([]byte(canonical)))
	headers.Set(HeaderKeyVersion, kid)
	headers.Set(HeaderSignature, signature)
	return headers
}

func fixedKey() []byte {
	key := make([]byte, sha256.Size)
	for index := range key {
		key[index] = byte(index)
	}
	return key
}

func signCanonical(key []byte, canonical string) string {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(canonical))
	return hex.EncodeToString(mac.Sum(nil))
}
