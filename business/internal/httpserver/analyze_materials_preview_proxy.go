package httpserver

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"sort"
	"unicode/utf8"

	"github.com/FigoGoo/Dora-Agent/business/internal/agentidentity"
	"github.com/gin-gonic/gin"
)

const (
	// analyzeMaterialsPreviewIntentSchema 是浏览器、Business BFF 与 Agent 共用的严格素材分析 Intent 版本。
	analyzeMaterialsPreviewIntentSchema = "analyze_materials.preview.intent.v1"
	// analyzeMaterialsPreviewEnqueueSchema 是只确认持久化入队的 202 回执版本。
	analyzeMaterialsPreviewEnqueueSchema        = "analyze_materials.preview.enqueue.v1"
	maximumAnalyzeMaterialsEnqueueResponseBytes = 8 << 10
)

// analyzeMaterialsExpectedAssetRequest 把目标 Asset 与用户提交时看到的版本绑定，阻止执行期静默漂移。
type analyzeMaterialsExpectedAssetRequest struct {
	// AssetID 是规范小写 UUIDv7，必须属于 AssetIDs exact-set。
	AssetID string `json:"asset_id"`
	// AssetVersion 是大于等于一的业务版本。
	AssetVersion int64 `json:"asset_version"`
}

// analyzeMaterialsPreviewIntentRequest 是 BFF 唯一接受并重新规范编码的素材分析 Preview DTO。
type analyzeMaterialsPreviewIntentRequest struct {
	// SchemaVersion 固定为 analyze_materials.preview.intent.v1。
	SchemaVersion string `json:"schema_version"`
	// AssetIDs 是一至八个规范小写 UUIDv7 exact-set。
	AssetIDs []string `json:"asset_ids"`
	// AnalysisGoal 是一至一千个 NFC 字符的分析目标。
	AnalysisGoal string `json:"analysis_goal"`
	// FocusDimensions 是 content、visual、narrative、risk 的非空去重子集。
	FocusDimensions []string `json:"focus_dimensions"`
	// OutputLanguage 只允许 zh-CN 或 en-US。
	OutputLanguage string `json:"output_language"`
	// ExpectedAssets 必填且必须与 AssetIDs exact-set 相等。
	ExpectedAssets []analyzeMaterialsExpectedAssetRequest `json:"expected_assets"`
}

// analyzeMaterialsPreviewEnqueueResponse 是 Agent 可靠持久化 typed Input 后的最小 202 DTO。
type analyzeMaterialsPreviewEnqueueResponse struct {
	// SchemaVersion 固定为 analyze_materials.preview.enqueue.v1。
	SchemaVersion string `json:"schema_version"`
	// RequestID 与 Business 身份断言 Request ID 完全一致。
	RequestID string `json:"request_id"`
	// SessionID 是路由绑定的 Agent Session UUIDv7。
	SessionID string `json:"session_id"`
	// InputID 是 Agent 已持久化的 Session Input UUIDv7。
	InputID string `json:"input_id"`
	// TurnID 是本次 typed Input 预分配的稳定 Turn UUIDv7。
	TurnID string `json:"turn_id"`
	// RunID 是本次 typed Input 预分配的稳定 Run UUIDv7。
	RunID string `json:"run_id"`
	// ToolCallID 是唯一 analyze_materials ToolCall 的预分配 UUIDv7。
	ToolCallID string `json:"tool_call_id"`
	// Status 固定为 pending，不冒充 Tool 已完成。
	Status string `json:"status"`
	// Replayed 表示同义 Idempotency-Key 命中了原入队事务。
	Replayed bool `json:"replayed"`
}

// analyzeMaterialsPreview 严格校验显式 Intent、幂等键与 Owner，再以专用 POST Scope 调用 Agent。
func (handler *AgentProxyHandler) analyzeMaterialsPreview(c *gin.Context) {
	requestID, ok := handler.newAgentRequestID(c)
	if !ok {
		return
	}
	if !handler.analyzeMaterialsEnabled {
		handler.writeAgentError(c, http.StatusNotFound, "PREVIEW_DISABLED", "素材分析开发预览未启用", requestID, false)
		return
	}
	sessionID := c.Param("session_id")
	if !canonicalUUIDv7(sessionID) || c.Request.URL.RawQuery != "" {
		handler.writeAgentError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "Session 标识无效", requestID, false)
		return
	}
	idempotencyValues := c.Request.Header.Values("Idempotency-Key")
	if len(idempotencyValues) != 1 || !canonicalUUIDv7(idempotencyValues[0]) {
		handler.writeAgentError(c, http.StatusBadRequest, "IDEMPOTENCY_KEY_INVALID", "幂等键无效", requestID, false)
		return
	}
	_, canonicalBody, ok := handler.decodeAnalyzeMaterialsPreviewIntent(c, requestID)
	if !ok {
		return
	}
	target := "/internal/v1/workspaces/sessions/" + sessionID + "/analyze-materials-previews"
	request, ok := handler.prepareBoundUpstreamRequest(
		c, requestID, sessionID, http.MethodPost, target, agentidentity.ScopeAnalyzeMaterialsPreviewWrite,
		"application/json", "application/json", bytes.NewReader(canonicalBody),
	)
	if !ok {
		return
	}
	request.Header.Set("Idempotency-Key", idempotencyValues[0])
	requestContext, cancel := contextWithProxyTimeout(request, handler.requestTimeout)
	defer cancel()
	response, err := handler.client.Do(request.WithContext(requestContext))
	if err != nil || response == nil || response.Body == nil {
		handler.writeAgentError(c, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", "素材分析预览依赖暂时不可用", requestID, true)
		return
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		handler.proxyUpstreamError(c, response, requestID)
		return
	}
	contentTypes := response.Header.Values("Content-Type")
	if len(contentTypes) != 1 {
		handler.writeAgentError(c, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", "素材分析预览依赖暂时不可用", requestID, true)
		return
	}
	mediaType, _, err := mime.ParseMediaType(contentTypes[0])
	if err != nil || mediaType != "application/json" {
		handler.writeAgentError(c, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", "素材分析预览依赖暂时不可用", requestID, true)
		return
	}
	body, err := readBoundedBody(response.Body, maximumAnalyzeMaterialsEnqueueResponseBytes)
	if err != nil {
		handler.writeAgentError(c, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", "素材分析预览依赖暂时不可用", requestID, true)
		return
	}
	enqueue, err := decodeAnalyzeMaterialsPreviewEnqueue(body)
	if err != nil || enqueue.RequestID != requestID || enqueue.SessionID != sessionID {
		handler.writeAgentError(c, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", "素材分析预览依赖暂时不可用", requestID, true)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusAccepted, enqueue)
}

// decodeAnalyzeMaterialsPreviewIntent 执行有界读取、全层重复键、null、NFC、枚举和 exact-set 校验。
func (handler *AgentProxyHandler) decodeAnalyzeMaterialsPreviewIntent(c *gin.Context, requestID string) (analyzeMaterialsPreviewIntentRequest, []byte, bool) {
	contentTypes := c.Request.Header.Values("Content-Type")
	if len(contentTypes) != 1 {
		handler.writeAgentError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "素材分析预览请求格式无效", requestID, false)
		return analyzeMaterialsPreviewIntentRequest{}, nil, false
	}
	mediaType, _, err := mime.ParseMediaType(contentTypes[0])
	if err != nil || mediaType != "application/json" {
		handler.writeAgentError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "素材分析预览请求格式无效", requestID, false)
		return analyzeMaterialsPreviewIntentRequest{}, nil, false
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, handler.previewBodyBytes)
	raw, err := io.ReadAll(c.Request.Body)
	trimmed := bytes.TrimSpace(raw)
	if err != nil || len(trimmed) == 0 || trimmed[0] != '{' || !utf8.Valid(raw) || !validJSONSurrogateEscapes(raw) {
		handler.writeAgentError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "素材分析预览请求格式无效", requestID, false)
		return analyzeMaterialsPreviewIntentRequest{}, nil, false
	}
	duplicate, err := hasDuplicateJSONKey(raw)
	if err != nil || duplicate {
		handler.writeAgentError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "素材分析预览请求格式无效", requestID, false)
		return analyzeMaterialsPreviewIntentRequest{}, nil, false
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		handler.writeAgentError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "素材分析预览请求格式无效", requestID, false)
		return analyzeMaterialsPreviewIntentRequest{}, nil, false
	}
	for _, required := range []string{"schema_version", "asset_ids", "analysis_goal", "focus_dimensions", "output_language", "expected_assets"} {
		value, exists := fields[required]
		if !exists || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			handler.writeAgentError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "素材分析预览请求格式无效", requestID, false)
			return analyzeMaterialsPreviewIntentRequest{}, nil, false
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var intent analyzeMaterialsPreviewIntentRequest
	if err := decoder.Decode(&intent); err != nil || ensureJSONEOF(decoder) != nil || !validAnalyzeMaterialsPreviewIntent(intent) {
		handler.writeAgentError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "素材分析预览请求格式无效", requestID, false)
		return analyzeMaterialsPreviewIntentRequest{}, nil, false
	}
	// BFF 与 Agent 都使用排序后的 exact-set 编码，令同义请求只有一种字节表示。
	sort.Strings(intent.AssetIDs)
	sort.Strings(intent.FocusDimensions)
	sort.Slice(intent.ExpectedAssets, func(left, right int) bool {
		return intent.ExpectedAssets[left].AssetID < intent.ExpectedAssets[right].AssetID
	})
	canonical, err := json.Marshal(intent)
	if err != nil {
		handler.writeAgentError(c, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", "素材分析预览依赖暂时不可用", requestID, true)
		return analyzeMaterialsPreviewIntentRequest{}, nil, false
	}
	return intent, canonical, true
}

// validAnalyzeMaterialsPreviewIntent 校验冻结 Intent 语义，并要求版本 exact-set 与目标 exact-set 完全一致。
func validAnalyzeMaterialsPreviewIntent(intent analyzeMaterialsPreviewIntentRequest) bool {
	if intent.SchemaVersion != analyzeMaterialsPreviewIntentSchema || !validPreviewText(intent.AnalysisGoal, 1, 1000, false) ||
		len(intent.AssetIDs) < 1 || len(intent.AssetIDs) > 8 || intent.AssetIDs == nil ||
		len(intent.FocusDimensions) < 1 || len(intent.FocusDimensions) > 4 || intent.FocusDimensions == nil ||
		len(intent.ExpectedAssets) != len(intent.AssetIDs) || intent.ExpectedAssets == nil ||
		(intent.OutputLanguage != "zh-CN" && intent.OutputLanguage != "en-US") {
		return false
	}
	assets := make(map[string]struct{}, len(intent.AssetIDs))
	for _, assetID := range intent.AssetIDs {
		if !canonicalUUIDv7(assetID) {
			return false
		}
		if _, duplicated := assets[assetID]; duplicated {
			return false
		}
		assets[assetID] = struct{}{}
	}
	focuses := make(map[string]struct{}, len(intent.FocusDimensions))
	for _, focus := range intent.FocusDimensions {
		if focus != "content" && focus != "visual" && focus != "narrative" && focus != "risk" {
			return false
		}
		if _, duplicated := focuses[focus]; duplicated {
			return false
		}
		focuses[focus] = struct{}{}
	}
	expected := make(map[string]struct{}, len(intent.ExpectedAssets))
	for _, item := range intent.ExpectedAssets {
		if item.AssetVersion < 1 {
			return false
		}
		if _, target := assets[item.AssetID]; !target {
			return false
		}
		if _, duplicated := expected[item.AssetID]; duplicated {
			return false
		}
		expected[item.AssetID] = struct{}{}
	}
	return len(expected) == len(assets)
}

// decodeAnalyzeMaterialsPreviewEnqueue 严格验证 Agent 202 DTO，拒绝伪完成状态和额外字段。
func decodeAnalyzeMaterialsPreviewEnqueue(raw []byte) (analyzeMaterialsPreviewEnqueueResponse, error) {
	if !utf8.Valid(raw) || !validJSONSurrogateEscapes(raw) {
		return analyzeMaterialsPreviewEnqueueResponse{}, errors.New("invalid analyze materials enqueue encoding")
	}
	duplicate, err := hasDuplicateJSONKey(raw)
	if err != nil || duplicate {
		return analyzeMaterialsPreviewEnqueueResponse{}, errors.New("invalid analyze materials enqueue object")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var response analyzeMaterialsPreviewEnqueueResponse
	if err := decoder.Decode(&response); err != nil || ensureJSONEOF(decoder) != nil ||
		response.SchemaVersion != analyzeMaterialsPreviewEnqueueSchema || response.Status != "pending" ||
		!canonicalUUIDv7(response.RequestID) || !canonicalUUIDv7(response.SessionID) || !canonicalUUIDv7(response.InputID) ||
		!canonicalUUIDv7(response.TurnID) || !canonicalUUIDv7(response.RunID) || !canonicalUUIDv7(response.ToolCallID) {
		return analyzeMaterialsPreviewEnqueueResponse{}, errors.New("invalid analyze materials enqueue response")
	}
	return response, nil
}

// hasDuplicateJSONKey 检查任意嵌套对象的重复 Key，并同时证明输入只有一个完整 JSON 值。
func hasDuplicateJSONKey(raw []byte) (bool, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	duplicate, err := readJSONValueForDuplicateKeys(decoder)
	if err != nil {
		return false, err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return false, errors.New("trailing JSON token")
		}
		return false, err
	}
	return duplicate, nil
}

// readJSONValueForDuplicateKeys 递归读取一个 JSON 值，并对每个对象维护独立 Key 集合。
func readJSONValueForDuplicateKeys(decoder *json.Decoder) (bool, error) {
	token, err := decoder.Token()
	if err != nil {
		return false, err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return false, nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		duplicate := false
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return false, err
			}
			key, ok := keyToken.(string)
			if !ok {
				return false, errors.New("invalid JSON object key")
			}
			if _, exists := seen[key]; exists {
				duplicate = true
			}
			seen[key] = struct{}{}
			childDuplicate, err := readJSONValueForDuplicateKeys(decoder)
			if err != nil {
				return false, err
			}
			duplicate = duplicate || childDuplicate
		}
		closing, err := decoder.Token()
		return duplicate, validateJSONClosingDelimiter(closing, '}', err)
	case '[':
		duplicate := false
		for decoder.More() {
			childDuplicate, err := readJSONValueForDuplicateKeys(decoder)
			if err != nil {
				return false, err
			}
			duplicate = duplicate || childDuplicate
		}
		closing, err := decoder.Token()
		return duplicate, validateJSONClosingDelimiter(closing, ']', err)
	default:
		return false, errors.New("invalid JSON opening delimiter")
	}
}

// validateJSONClosingDelimiter 校验递归读取结束于与开头匹配的唯一关闭符。
func validateJSONClosingDelimiter(token json.Token, expected json.Delim, err error) error {
	if err != nil {
		return err
	}
	actual, ok := token.(json.Delim)
	if !ok || actual != expected {
		return errors.New("invalid JSON closing delimiter")
	}
	return nil
}
