// Package httpidentity 校验 Business→Agent 用户级短期 HTTP 身份断言与 Redis 一次性 Nonce。
package httpidentity

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/FigoGoo/Dora-Agent/agent/internal/config"
	"github.com/google/uuid"
)

const (
	// HeaderAssertion 承载无 Padding Base64URL 编码的 16 行 Canonical 断言。
	HeaderAssertion = "X-Dora-Identity-Assertion"
	// HeaderKeyVersion 承载 HMAC 密钥版本，并必须与 Canonical 第四行相同。
	HeaderKeyVersion = "X-Dora-Identity-Key-Version"
	// HeaderSignature 承载 HMAC-SHA256 的 64 位小写十六进制签名。
	HeaderSignature = "X-Dora-Identity-Signature"

	// ScopeWorkspaceRead 仅允许读取指定 Session 的 Workspace Snapshot。
	ScopeWorkspaceRead = "agent.session.workspace.read"
	// ScopeEventsRead 仅允许读取指定 Session 的 EventLog SSE。
	ScopeEventsRead = "agent.session.events.read"
	// ScopeToolsRead 仅允许读取指定 Session 的静态 Tool Definition Catalog。
	ScopeToolsRead = "agent.session.tools.read"
	// ScopeCreationSpecPreviewWrite 只允许向指定 Session 持久化一条 CreationSpec Preview Intent。
	ScopeCreationSpecPreviewWrite = "creation_spec.preview.write"
	// ScopeAnalyzeMaterialsPreviewWrite 只允许向指定 Session 持久化一条素材分析 Preview Intent。
	ScopeAnalyzeMaterialsPreviewWrite = "analyze_materials.preview.write"
	// ScopePlanStoryboardPreviewWrite 只允许向指定 Session 持久化一条 Storyboard Preview Intent。
	ScopePlanStoryboardPreviewWrite = "plan_storyboard.preview.write"
	// ScopeWritePromptsPreviewWrite 只允许向指定 Session 持久化一条 Prompt Preview Intent。
	ScopeWritePromptsPreviewWrite = "write_prompts.preview.write"
	// ScopeGenerateMediaPreviewWrite 只允许向指定 Session 入队 generate_media typed Intent。
	ScopeGenerateMediaPreviewWrite = "generate_media.preview.write"
	// ScopeAssembleOutputPreviewWrite 只允许向指定 Session 入队 assemble_output typed Intent。
	ScopeAssembleOutputPreviewWrite = "assemble_output.preview.write"

	assertionSchema              = "agent_http_identity_assertion.v1"
	assertionIssuer              = "dora-business-service"
	assertionAudience            = "dora.agent.http.v1"
	assertionLineCount           = 16
	maxAssertionBytes            = 2048
	maxAssertionTTL              = time.Minute
	maximumJavaScriptSafeInteger = uint64(1<<53 - 1)
)

var (
	// ErrInvalid 表示断言格式、签名、绑定、时间窗或 Nonce 重放无效。
	ErrInvalid = errors.New("internal HTTP identity assertion is invalid")
	// ErrUnavailable 表示 Redis 无法完成全实例一次性 Nonce 占有，必须失败关闭。
	ErrUnavailable = errors.New("internal HTTP identity assertion replay protection unavailable")
)

// Clock 提供可注入 UTC 时间，单次断言校验必须只冻结一次当前时间。
type Clock interface {
	// Now 返回当前时间；实现方应提供单调安全且可转换为 UTC 的时间值。
	Now() time.Time
}

// ReplayStore 原子占有已通过静态校验和 HMAC 的一次性 Nonce。
type ReplayStore interface {
	// ClaimIdentityNonce 对同一 kid+nonce 只允许一次成功；依赖故障返回 error，不得降级到进程内状态。
	ClaimIdentityNonce(ctx context.Context, kid string, nonce []byte, ttl time.Duration) (bool, error)
}

// Request 描述 Handler 已规范化并绑定到具体路由的断言校验输入。
type Request struct {
	// Headers 是内部 HTTP 请求头；Verifier 只读取冻结的三个身份 Header。
	Headers http.Header
	// Method 必须与 Scope 对应，只允许冻结白名单中的 GET 或 Preview POST。
	Method string
	// CanonicalTarget 是 Handler 从白名单路径和规范 Cursor 构造的唯一 Target。
	CanonicalTarget string
	// Scope 是当前白名单端点要求的精确权限。
	Scope string
	// AgentSessionID 是路由中的规范 UUIDv7，用于再次交叉绑定 Canonical。
	AgentSessionID string
}

// Claims 是 HMAC 与一次性 Nonce 均校验成功后的可信 Business 身份上下文。
type Claims struct {
	// KeyVersion 是此次签名明确选择的 active 或 previous kid。
	KeyVersion string
	// RequestID 是 Business 为内部调用生成的 UUIDv7。
	RequestID string
	// PrincipalUserID 是 Business 已认证用户 UUIDv7。
	PrincipalUserID string
	// WebSessionID 是签发断言时解析的 Business Web Session UUIDv7。
	WebSessionID string
	// WebSessionVersion 是签发断言时冻结的会话版本。
	WebSessionVersion int64
	// ProjectID 是 Business owned ready Binding 对应 Project UUIDv7。
	ProjectID string
	// AgentSessionID 是此次读取绑定的 Agent Session UUIDv7。
	AgentSessionID string
	// Scope 是此次调用唯一允许的最小读或 Preview 写权限。
	Scope string
	// IssuedAt 是断言签发 UTC 时间。
	IssuedAt time.Time
	// ExpiresAt 是 Handler 必须收紧请求与 SSE 连接的 UTC Deadline。
	ExpiresAt time.Time
}

// Verifier 按明确 kid 选择 HMAC Key，并在 Redis 中一次性占有 Nonce。
type Verifier struct {
	keys          map[string][]byte
	maxClockSkew  time.Duration
	replayTimeout time.Duration
	replayStore   ReplayStore
	clock         Clock
}

// NewVerifier 冻结 active/previous Keyring 与安全边界；缺少 Redis 或时间源时拒绝启动。
func NewVerifier(cfg config.HTTPIdentityConfig, replayStore ReplayStore, clock Clock) (*Verifier, error) {
	if replayStore == nil || clock == nil || len(cfg.ActiveSecret) != sha256.Size || !validKeyVersion(cfg.ActiveKeyVersion) ||
		cfg.MaxClockSkew <= 0 || cfg.MaxClockSkew > 5*time.Second || cfg.ReplayTimeout <= 0 {
		return nil, fmt.Errorf("create HTTP identity verifier: invalid dependency or config")
	}
	keys := map[string][]byte{cfg.ActiveKeyVersion: append([]byte(nil), cfg.ActiveSecret...)}
	if len(cfg.PreviousSecret) != 0 || cfg.PreviousKeyVersion != "" {
		if len(cfg.PreviousSecret) != sha256.Size || !validKeyVersion(cfg.PreviousKeyVersion) || cfg.PreviousKeyVersion == cfg.ActiveKeyVersion {
			return nil, fmt.Errorf("create HTTP identity verifier: previous key pair is invalid")
		}
		keys[cfg.PreviousKeyVersion] = append([]byte(nil), cfg.PreviousSecret...)
	}
	return &Verifier{
		keys: keys, maxClockSkew: cfg.MaxClockSkew, replayTimeout: cfg.ReplayTimeout,
		replayStore: replayStore, clock: clock,
	}, nil
}

// Verify 严格校验唯一 Header 编码、16 行 Canonical、路由/Scope/三重身份绑定、HMAC、时间窗与 Redis Nonce。
// 只有全部静态校验和 HMAC 通过后才访问 Redis，防止无效流量污染重放空间。
func (v *Verifier) Verify(ctx context.Context, request Request) (Claims, error) {
	assertionEncoded, ok := singleCanonicalHeader(request.Headers, HeaderAssertion)
	if !ok || base64.RawURLEncoding.DecodedLen(len(assertionEncoded)) > maxAssertionBytes {
		return Claims{}, ErrInvalid
	}
	canonical, err := base64.RawURLEncoding.Strict().DecodeString(assertionEncoded)
	if err != nil || len(canonical) == 0 || len(canonical) > maxAssertionBytes ||
		base64.RawURLEncoding.EncodeToString(canonical) != assertionEncoded {
		return Claims{}, ErrInvalid
	}
	kid, ok := singleCanonicalHeader(request.Headers, HeaderKeyVersion)
	if !ok {
		return Claims{}, ErrInvalid
	}
	signatureText, ok := singleCanonicalHeader(request.Headers, HeaderSignature)
	if !ok || len(signatureText) != sha256.Size*2 || strings.ToLower(signatureText) != signatureText {
		return Claims{}, ErrInvalid
	}
	signature, err := hex.DecodeString(signatureText)
	if err != nil || len(signature) != sha256.Size {
		return Claims{}, ErrInvalid
	}
	key, ok := v.keys[kid]
	if !ok {
		return Claims{}, ErrInvalid
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(canonical)
	if subtle.ConstantTimeCompare(signature, mac.Sum(nil)) != 1 {
		return Claims{}, ErrInvalid
	}

	claims, nonce, err := parseCanonical(string(canonical), kid, request)
	if err != nil {
		return Claims{}, ErrInvalid
	}
	now := v.clock.Now().UTC()
	if !claims.ExpiresAt.After(now) || !claims.ExpiresAt.After(claims.IssuedAt) ||
		claims.ExpiresAt.Sub(claims.IssuedAt) > maxAssertionTTL || claims.IssuedAt.After(now.Add(v.maxClockSkew)) {
		return Claims{}, ErrInvalid
	}

	// Redis TTL 覆盖 exp+skew，确保刚过期断言在允许时钟偏差内也不能跨实例重放。
	replayTTL := claims.ExpiresAt.Add(v.maxClockSkew).Sub(now)
	replayCtx, cancel := context.WithTimeout(ctx, v.replayTimeout)
	if deadline, hasDeadline := replayCtx.Deadline(); !hasDeadline || claims.ExpiresAt.Before(deadline) {
		cancel()
		replayCtx, cancel = context.WithDeadline(ctx, claims.ExpiresAt)
	}
	defer cancel()
	claimed, err := v.replayStore.ClaimIdentityNonce(replayCtx, kid, nonce, replayTTL)
	if err != nil {
		return Claims{}, ErrUnavailable
	}
	if !claimed {
		return Claims{}, ErrInvalid
	}
	return claims, nil
}

// parseCanonical 把通过 HMAC 的固定 16 行内容解析为唯一规范字段，并与 Handler 白名单路由交叉绑定。
func parseCanonical(canonical, headerKid string, request Request) (Claims, []byte, error) {
	if !validRequestBinding(request) {
		return Claims{}, nil, ErrInvalid
	}
	if strings.ContainsRune(canonical, '\r') || strings.HasSuffix(canonical, "\n") {
		return Claims{}, nil, ErrInvalid
	}
	lines := strings.Split(canonical, "\n")
	if len(lines) != assertionLineCount || lines[0] != assertionSchema || lines[1] != assertionIssuer ||
		lines[2] != assertionAudience || lines[3] != headerKid || lines[5] != request.Method ||
		lines[6] != request.CanonicalTarget || lines[12] != request.Scope {
		return Claims{}, nil, ErrInvalid
	}
	requestID, err := canonicalUUIDv7(lines[4])
	if err != nil {
		return Claims{}, nil, err
	}
	principalID, err := canonicalUUIDv7(lines[7])
	if err != nil {
		return Claims{}, nil, err
	}
	webSessionID, err := canonicalUUIDv7(lines[8])
	if err != nil {
		return Claims{}, nil, err
	}
	webSessionVersion, err := canonicalPositiveInt64(lines[9])
	if err != nil {
		return Claims{}, nil, err
	}
	projectID, err := canonicalUUIDv7(lines[10])
	if err != nil {
		return Claims{}, nil, err
	}
	agentSessionID, err := canonicalUUIDv7(lines[11])
	if err != nil || agentSessionID != request.AgentSessionID {
		return Claims{}, nil, ErrInvalid
	}
	iatMillis, err := canonicalPositiveInt64(lines[13])
	if err != nil {
		return Claims{}, nil, err
	}
	expMillis, err := canonicalPositiveInt64(lines[14])
	if err != nil {
		return Claims{}, nil, err
	}
	nonce, err := base64.RawURLEncoding.Strict().DecodeString(lines[15])
	if err != nil || len(nonce) != 16 || base64.RawURLEncoding.EncodeToString(nonce) != lines[15] {
		return Claims{}, nil, ErrInvalid
	}
	return Claims{
		KeyVersion: headerKid, RequestID: requestID, PrincipalUserID: principalID,
		WebSessionID: webSessionID, WebSessionVersion: webSessionVersion, ProjectID: projectID,
		AgentSessionID: agentSessionID, Scope: request.Scope,
		IssuedAt: time.UnixMilli(iatMillis).UTC(), ExpiresAt: time.UnixMilli(expMillis).UTC(),
	}, nonce, nil
}

// validRequestBinding 独立于 Handler 再次冻结 Scope、Method 与 Canonical Target 的唯一映射。
func validRequestBinding(request Request) bool {
	agentSessionID, err := canonicalUUIDv7(request.AgentSessionID)
	if err != nil {
		return false
	}
	workspaceTarget := "/api/v1/agent/sessions/" + agentSessionID + "/workspace"
	eventsPrefix := "/api/v1/agent/sessions/" + agentSessionID + "/events?after_seq="
	toolsTarget := "/api/v1/agent/sessions/" + agentSessionID + "/tools"
	creationSpecPreviewTarget := "/internal/v1/workspaces/sessions/" + agentSessionID + "/creation-spec-previews"
	analyzeMaterialsPreviewTarget := "/internal/v1/workspaces/sessions/" + agentSessionID + "/analyze-materials-previews"
	planStoryboardPreviewTarget := "/internal/v1/workspaces/sessions/" + agentSessionID + "/plan-storyboard-previews"
	writePromptsPreviewTarget := "/internal/v1/workspaces/sessions/" + agentSessionID + "/write-prompts-previews"
	generateMediaPreviewTarget := "/internal/v1/workspaces/sessions/" + agentSessionID + "/generate-media-previews"
	assembleOutputPreviewTarget := "/internal/v1/workspaces/sessions/" + agentSessionID + "/assemble-output-previews"
	switch request.Scope {
	case ScopeWorkspaceRead:
		return request.Method == http.MethodGet && request.CanonicalTarget == workspaceTarget
	case ScopeEventsRead:
		cursor := strings.TrimPrefix(request.CanonicalTarget, eventsPrefix)
		return request.Method == http.MethodGet && cursor != request.CanonicalTarget && canonicalNonNegativeInt(cursor)
	case ScopeToolsRead:
		return request.Method == http.MethodGet && request.CanonicalTarget == toolsTarget
	case ScopeCreationSpecPreviewWrite:
		return request.Method == http.MethodPost && request.CanonicalTarget == creationSpecPreviewTarget
	case ScopeAnalyzeMaterialsPreviewWrite:
		return request.Method == http.MethodPost && request.CanonicalTarget == analyzeMaterialsPreviewTarget
	case ScopePlanStoryboardPreviewWrite:
		return request.Method == http.MethodPost && request.CanonicalTarget == planStoryboardPreviewTarget
	case ScopeWritePromptsPreviewWrite:
		return request.Method == http.MethodPost && request.CanonicalTarget == writePromptsPreviewTarget
	case ScopeGenerateMediaPreviewWrite:
		return request.Method == http.MethodPost && request.CanonicalTarget == generateMediaPreviewTarget
	case ScopeAssembleOutputPreviewWrite:
		return request.Method == http.MethodPost && request.CanonicalTarget == assembleOutputPreviewTarget
	default:
		return false
	}
}

// singleCanonicalHeader 要求目标 Header 只出现一次，且值没有空白折叠或逗号合并的第二语义。
func singleCanonicalHeader(headers http.Header, name string) (string, bool) {
	values := headers.Values(name)
	if len(values) != 1 || values[0] == "" || strings.TrimSpace(values[0]) != values[0] || strings.Contains(values[0], ",") {
		return "", false
	}
	return values[0], true
}

// canonicalUUIDv7 拒绝大写、花括号和非 UUIDv7 表示，确保 Canonical 只有一种编码。
func canonicalUUIDv7(value string) (string, error) {
	parsed, err := uuid.Parse(value)
	if err != nil || parsed.Version() != 7 || parsed.String() != value {
		return "", ErrInvalid
	}
	return value, nil
}

// canonicalPositiveInt64 拒绝符号、前导零、零和溢出，固定十进制编码。
func canonicalPositiveInt64(value string) (int64, error) {
	if value == "" || value == "0" || (len(value) > 1 && value[0] == '0') {
		return 0, ErrInvalid
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 || strconv.FormatInt(parsed, 10) != value {
		return 0, ErrInvalid
	}
	return parsed, nil
}

// canonicalNonNegativeInt 固定 Event Cursor 为无前导零且不超过 JavaScript 安全整数的十进制。
func canonicalNonNegativeInt(value string) bool {
	if value == "0" {
		return true
	}
	if value == "" || value[0] < '1' || value[0] > '9' {
		return false
	}
	for index := 1; index < len(value); index++ {
		if value[index] < '0' || value[index] > '9' {
			return false
		}
	}
	parsed, err := strconv.ParseUint(value, 10, 64)
	return err == nil && parsed <= maximumJavaScriptSafeInteger
}

// validKeyVersion 固定 kid 为 1..64 字节小写 ASCII，避免 Header、Canonical 与 Redis Key 出现第二语义。
func validKeyVersion(value string) bool {
	if len(value) == 0 || len(value) > 64 {
		return false
	}
	for index, character := range []byte(value) {
		if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') ||
			(index > 0 && (character == '.' || character == '_' || character == '-')) {
			continue
		}
		return false
	}
	return true
}
