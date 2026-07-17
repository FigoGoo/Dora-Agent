// Package businessmedia 调用 loopback Business 媒体 Preview 内部 JSON 端点。
package businessmedia

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/FigoGoo/Dora-Agent/worker/internal/config"
	"github.com/FigoGoo/Dora-Agent/worker/internal/mediajob"
	"github.com/google/uuid"
)

const (
	finalizePath          = "/internal/v1/media-preview-assets/finalize"
	queryFinalizationPath = "/internal/v1/media-preview-assets/query-finalization"
	readinessPath         = "/internal/v1/media-preview-assets/readiness"
)

// Client 只向启动时冻结的 loopback Business Base URL 发送严格 JSON 请求。
type Client struct {
	// baseURL 是无 Path/Query/Fragment 的 loopback HTTP URL。
	baseURL *url.URL
	// httpClient 禁止 Redirect 且不使用环境代理。
	httpClient *http.Client
	// maxResponseBytes 是单响应硬读取上限。
	maxResponseBytes int64
}

// New 校验 loopback Base URL 并构造不继承代理、禁止 Redirect 的有界 HTTP Client。
func New(cfg config.MediaRuntimeConfig) (*Client, error) {
	parsed, err := url.Parse(cfg.BusinessBaseURL)
	if err != nil || parsed.Scheme != "http" || parsed.Host == "" || parsed.User != nil ||
		(parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" ||
		!isLoopbackHost(parsed.Hostname()) {
		return nil, fmt.Errorf("create Business media client: invalid loopback base URL")
	}
	if cfg.BusinessCallTimeout <= 0 || cfg.MaxResponseBytes <= 0 {
		return nil, fmt.Errorf("create Business media client: invalid budgets")
	}
	transport := &http.Transport{
		Proxy: nil,
		DialContext: (&net.Dialer{
			Timeout: cfg.BusinessCallTimeout,
		}).DialContext,
		ForceAttemptHTTP2: false,
	}
	return &Client{
		baseURL: parsed,
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   cfg.BusinessCallTimeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		maxResponseBytes: cfg.MaxResponseBytes,
	}, nil
}

// Close 关闭 HTTP Transport 的空闲连接；当前进行中的请求由 Processor Drain 或 Context 取消。
func (c *Client) Close() {
	if c == nil || c.httpClient == nil {
		return
	}
	c.httpClient.CloseIdleConnections()
}

// Readiness 严格验证 Business 同 Profile、对象根、Prepare 和 Finalize 能力全部就绪。
func (c *Client) Readiness(ctx context.Context) error {
	var response mediajob.BusinessReadinessV1
	if err := c.doJSON(ctx, http.MethodGet, readinessPath, nil, &response); err != nil {
		return err
	}
	if response.SchemaVersion != mediajob.ReadinessSchemaV1 ||
		response.Profile != config.MediaRuntimeProfileV3Preview1 ||
		!response.ObjectRootReady || !response.Prepare || !response.Finalize {
		return fmt.Errorf("Business media readiness contract mismatch")
	}
	return nil
}

// Finalize 提交严格 ready/failed 联合请求，并校验回显、版本和 Asset 权威摘要。
func (c *Client) Finalize(ctx context.Context, request mediajob.FinalizeRequestV1) (mediajob.FinalizeResultV1, error) {
	if err := validateFinalizeRequest(request); err != nil {
		return mediajob.FinalizeResultV1{}, err
	}
	var response mediajob.FinalizeResultV1
	if err := c.doJSON(ctx, http.MethodPost, finalizePath, request, &response); err != nil {
		return mediajob.FinalizeResultV1{}, err
	}
	if err := validateFinalizeResult(response, request.RequestID, request.CommandID, request.TerminalStatus); err != nil {
		return mediajob.FinalizeResultV1{}, err
	}
	if request.TerminalStatus == "ready" && request.Output != nil &&
		(response.AssetRef.ContentDigest != request.Output.ContentDigest ||
			response.AssetRef.SizeBytes != request.Output.SizeBytes || response.AssetRef.MIMEType != request.Output.MIMEType) {
		return mediajob.FinalizeResultV1{}, fmt.Errorf("Business finalize Asset output mismatch")
	}
	return response, nil
}

// QueryFinalization 按原 command/digest/preparation 查询，并校验 not_found/completed/conflict 严格联合。
func (c *Client) QueryFinalization(ctx context.Context, request mediajob.QueryFinalizationRequestV1) (mediajob.QueryFinalizationResultV1, error) {
	if request.SchemaVersion != mediajob.QueryFinalizationRequestSchemaV1 || !isUUIDv7(request.RequestID) ||
		!isUUIDv7(request.CommandID) || !isUUIDv7(request.PreparationID) || !isDigest(request.RequestDigest) {
		return mediajob.QueryFinalizationResultV1{}, fmt.Errorf("invalid Business query-finalization request")
	}
	var response mediajob.QueryFinalizationResultV1
	if err := c.doJSON(ctx, http.MethodPost, queryFinalizationPath, request, &response); err != nil {
		return mediajob.QueryFinalizationResultV1{}, err
	}
	if response.SchemaVersion != mediajob.QueryFinalizationResultSchemaV1 || response.RequestID != request.RequestID {
		return mediajob.QueryFinalizationResultV1{}, fmt.Errorf("Business query-finalization response mismatch")
	}
	switch response.Status {
	case "not_found":
		if response.Result != nil || response.ErrorCode != "" {
			return mediajob.QueryFinalizationResultV1{}, fmt.Errorf("invalid Business not_found finalization union")
		}
	case "completed":
		if response.Result == nil || response.ErrorCode != "" {
			return mediajob.QueryFinalizationResultV1{}, fmt.Errorf("invalid Business completed finalization union")
		}
		if err := validateFinalizeResult(*response.Result, request.RequestID, request.CommandID, response.Result.AssetRef.Status); err != nil {
			return mediajob.QueryFinalizationResultV1{}, err
		}
	case "conflict":
		if response.Result != nil || response.ErrorCode != "IDEMPOTENCY_CONFLICT" {
			return mediajob.QueryFinalizationResultV1{}, fmt.Errorf("invalid Business conflict finalization union")
		}
	default:
		return mediajob.QueryFinalizationResultV1{}, fmt.Errorf("unknown Business query-finalization status")
	}
	return response, nil
}

// doJSON 发送固定路径请求，限制响应大小并拒绝非 JSON、重复 key、未知字段和尾随 token。
func (c *Client) doJSON(ctx context.Context, method string, endpointPath string, input any, output any) error {
	if c == nil || c.baseURL == nil || c.httpClient == nil {
		return fmt.Errorf("Business media client is nil")
	}
	var body io.Reader
	if input != nil {
		payload, err := json.Marshal(input)
		if err != nil {
			return fmt.Errorf("encode Business media request: %w", err)
		}
		body = bytes.NewReader(payload)
	}
	requestURL := *c.baseURL
	requestURL.Path = endpointPath
	request, err := http.NewRequestWithContext(ctx, method, requestURL.String(), body)
	if err != nil {
		return fmt.Errorf("create Business media request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	if input != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("%w: Business media request", mediajob.ErrOutcomeUnknown)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, c.maxResponseBytes))
		if response.StatusCode >= 500 || response.StatusCode == http.StatusRequestTimeout || response.StatusCode == http.StatusTooManyRequests {
			return fmt.Errorf("%w: Business media HTTP status %d", mediajob.ErrOutcomeUnknown, response.StatusCode)
		}
		return fmt.Errorf("Business media HTTP status %d", response.StatusCode)
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return fmt.Errorf("Business media response is not application/json")
	}
	payload, err := io.ReadAll(io.LimitReader(response.Body, c.maxResponseBytes+1))
	if err != nil {
		return fmt.Errorf("%w: read Business media response", mediajob.ErrOutcomeUnknown)
	}
	if int64(len(payload)) > c.maxResponseBytes {
		return fmt.Errorf("Business media response exceeds byte budget")
	}
	if err := rejectDuplicateJSONKeys(payload); err != nil {
		return fmt.Errorf("invalid Business media JSON: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(output); err != nil {
		return fmt.Errorf("decode strict Business media response: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return fmt.Errorf("Business media response contains trailing JSON")
	}
	return nil
}

// validateFinalizeRequest 校验 ready/failed 严格联合及全部 UUIDv7、Fence 和摘要字段。
func validateFinalizeRequest(request mediajob.FinalizeRequestV1) error {
	if request.SchemaVersion != mediajob.FinalizeRequestSchemaV1 || !isUUIDv7(request.RequestID) ||
		!isUUIDv7(request.CommandID) || !isUUIDv7(request.PreparationID) || !isUUIDv7(request.OperationID) ||
		!isUUIDv7(request.BatchID) || !isUUIDv7(request.JobID) || !isUUIDv7(request.AttemptID) ||
		!isDigest(request.RequestDigest) || request.Fence <= 0 {
		return fmt.Errorf("invalid Business finalize request")
	}
	switch request.TerminalStatus {
	case "ready":
		if request.Output == nil || request.ErrorCode != "" || !validFinalizeOutput(*request.Output) {
			return fmt.Errorf("invalid Business ready finalize union")
		}
	case "failed":
		if request.Output != nil || !isAllowedErrorCode(request.ErrorCode) {
			return fmt.Errorf("invalid Business failed finalize union")
		}
	default:
		return fmt.Errorf("invalid Business finalize terminal status")
	}
	return nil
}

// validateFinalizeResult 校验 Business 权威 Asset/Receipt 回显；expectedStatus 可为 ready、failed 或 Asset status。
func validateFinalizeResult(response mediajob.FinalizeResultV1, requestID uuid.UUID, commandID uuid.UUID, expectedStatus string) error {
	if response.SchemaVersion != mediajob.FinalizeResultSchemaV1 || response.RequestID != requestID ||
		response.CommandID != commandID || (response.Disposition != "created" && response.Disposition != "replayed") ||
		!isUUIDv7(response.FinalizationReceiptID) || !isUUIDv7(response.AssetRef.AssetID) ||
		response.AssetRef.Version != 1 || response.CompletedAt.IsZero() {
		return fmt.Errorf("Business finalize response mismatch")
	}
	if expectedStatus == "ready" || expectedStatus == "failed" {
		if response.AssetRef.Status != expectedStatus {
			return fmt.Errorf("Business finalize Asset status mismatch")
		}
	}
	validMediaPair := (response.AssetRef.MediaKind == "image" && response.AssetRef.MIMEType == "image/png") ||
		(response.AssetRef.MediaKind == "video" && response.AssetRef.MIMEType == "video/mp4")
	if !validMediaPair {
		return fmt.Errorf("invalid Business Asset media pair")
	}
	if response.AssetRef.Status == "ready" {
		if !isDigest(response.AssetRef.ContentDigest) || response.AssetRef.SizeBytes <= 0 ||
			(response.AssetRef.MediaKind != "image" && response.AssetRef.MediaKind != "video") {
			return fmt.Errorf("invalid Business ready Asset ref")
		}
	} else if response.AssetRef.Status != "failed" || response.AssetRef.ContentDigest != "" || response.AssetRef.SizeBytes != 0 {
		return fmt.Errorf("invalid Business failed Asset ref")
	}
	return nil
}

// validFinalizeOutput 校验 PNG/MP4 固定媒体联合，禁止自定义尺寸、codec 或时长。
func validFinalizeOutput(output mediajob.FinalizeOutputV1) bool {
	if !isDigest(output.ContentDigest) || output.SizeBytes <= 0 || output.Width != 640 || output.Height != 360 {
		return false
	}
	if output.MIMEType == "image/png" {
		return output.DurationMS == 0 && output.Codec == "" && output.PixelFormat == ""
	}
	return output.MIMEType == "video/mp4" && output.DurationMS >= 1900 && output.DurationMS <= 2100 &&
		output.Codec == "h264" && output.PixelFormat == "yuv420p"
}

// rejectDuplicateJSONKeys 递归遍历 JSON Token，拒绝任意对象层级的重复字段和尾随值。
func rejectDuplicateJSONKeys(payload []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	if err := consumeJSONValue(decoder); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return fmt.Errorf("trailing JSON token")
	}
	return nil
}

// consumeJSONValue 消费一个完整 JSON 值，并在每个对象内维护独立 key 集合。
func consumeJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("object key is not a string")
			}
			if _, exists := seen[key]; exists {
				return fmt.Errorf("duplicate JSON key")
			}
			seen[key] = struct{}{}
			if err := consumeJSONValue(decoder); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim('}') {
			return fmt.Errorf("invalid JSON object")
		}
	case '[':
		for decoder.More() {
			if err := consumeJSONValue(decoder); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim(']') {
			return fmt.Errorf("invalid JSON array")
		}
	default:
		return fmt.Errorf("unexpected JSON delimiter")
	}
	return nil
}

// isUUIDv7 仅接受 RFC 4122 variant 的 UUIDv7。
func isUUIDv7(value uuid.UUID) bool {
	return value != uuid.Nil && value.Version() == 7 && value.Variant() == uuid.RFC4122
}

// isDigest 校验无前缀 64 位 lowercase SHA-256。
func isDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

// isAllowedErrorCode 限制 failed Finalize 只携带跨 Module 契约白名单错误码。
func isAllowedErrorCode(code string) bool {
	switch code {
	case "FEATURE_DISABLED", "INVALID_ARGUMENT", "NOT_FOUND", "VERSION_CONFLICT", "IDEMPOTENCY_CONFLICT",
		"DEPENDENCY_NOT_READY", "UNSUPPORTED_PROFILE", "LEASE_LOST", "FENCE_STALE", "ARTIFACT_INVALID",
		"FFMPEG_UNAVAILABLE", "EXECUTION_TIMEOUT", "UNKNOWN_OUTCOME", "INTERNAL":
		return true
	default:
		return false
	}
}

// isLoopbackHost 只信任 localhost 字面量或明确 loopback IP，不解析外部 DNS。
func isLoopbackHost(host string) bool {
	if strings.EqualFold(strings.TrimSpace(host), "localhost") {
		return true
	}
	parsed := net.ParseIP(strings.TrimSpace(host))
	return parsed != nil && parsed.IsLoopback()
}

var _ mediajob.BusinessClient = (*Client)(nil)
