// Package businessmedia 实现 Agent→Business local-only 媒体 Prepare/Query 严格 JSON Client。
package businessmedia

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/FigoGoo/Dora-Agent/agent/internal/config"
	"github.com/FigoGoo/Dora-Agent/agent/internal/mediapreview"
)

const (
	preparePath          = "/internal/v1/media-preview-assets/prepare"
	queryPreparationPath = "/internal/v1/media-preview-assets/query-preparation"
	readinessPath        = "/internal/v1/media-preview-assets/readiness"
)

// Client 只向启动期冻结的 loopback Business 根地址发送有界 JSON 请求。
type Client struct {
	// baseURL 不含用户信息、Path、Query 或 Fragment。
	baseURL *url.URL
	// httpClient 禁止环境代理、Redirect 和无界等待。
	httpClient *http.Client
	// maxResponseBytes 限制单响应读取，避免依赖端放大内存。
	maxResponseBytes int64
}

// New 校验 media.runtime.v3preview1 与 loopback URL 后构造严格 Client。
func New(cfg config.MediaRuntimeConfig) (*Client, error) {
	parsed, err := url.Parse(cfg.BusinessBaseURL)
	if err != nil || cfg.Profile != mediapreview.Profile || parsed.Scheme != "http" || parsed.Host == "" ||
		parsed.User != nil || (parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" ||
		parsed.Fragment != "" || !isLoopbackHost(parsed.Hostname()) {
		return nil, fmt.Errorf("create Agent Business media client: invalid local-only configuration")
	}
	if cfg.CallTimeout <= 0 || cfg.MaxResponseBytes < 4096 || cfg.MaxResponseBytes > 1024*1024 {
		return nil, fmt.Errorf("create Agent Business media client: invalid budgets")
	}
	transport := &http.Transport{
		Proxy: nil,
		DialContext: (&net.Dialer{
			Timeout: cfg.CallTimeout,
		}).DialContext,
		ForceAttemptHTTP2: false,
	}
	return &Client{
		baseURL: parsed,
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   cfg.CallTimeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		maxResponseBytes: cfg.MaxResponseBytes,
	}, nil
}

// Close 关闭空闲连接；在途请求由父 Context 与 Bootstrap Drain 终止。
func (client *Client) Close() {
	if client != nil && client.httpClient != nil {
		client.httpClient.CloseIdleConnections()
	}
}

// Readiness 验证 Business 同 Profile、对象根、Prepare 与 Finalize 全部就绪。
func (client *Client) Readiness(ctx context.Context) error {
	payload, err := client.do(ctx, http.MethodGet, readinessPath, nil)
	if err != nil {
		return fmt.Errorf("probe Business media readiness: %w", err)
	}
	if _, err := mediapreview.DecodeBusinessReadiness(payload); err != nil {
		return err
	}
	return nil
}

// Prepare 调用唯一 Business Prepare 副作用，并严格复核原 request/command 与资源联合。
func (client *Client) Prepare(ctx context.Context, request mediapreview.PrepareRequest) (mediapreview.PrepareResult, error) {
	if mediapreview.ValidatePrepareRequest(request) != nil {
		return mediapreview.PrepareResult{}, mediapreview.ErrInvalidArgument
	}
	payload, err := client.do(ctx, http.MethodPost, preparePath, request)
	if err != nil {
		return mediapreview.PrepareResult{}, err
	}
	result, err := mediapreview.DecodePrepareResult(payload, request)
	if err != nil {
		// Prepare 已越过副作用边界；即使 200 Body 不可信也必须按原 command 查询。
		return mediapreview.PrepareResult{}, fmt.Errorf("%w: invalid Business Prepare response", mediapreview.ErrUnknownOutcome)
	}
	return result, nil
}

// QueryPreparation 按原 command/digest/Owner 查询，不重发 Prepare 或替换幂等键。
func (client *Client) QueryPreparation(
	ctx context.Context,
	query mediapreview.PrepareQuery,
) (mediapreview.PrepareQueryResult, error) {
	if mediapreview.ValidatePrepareQuery(query) != nil {
		return mediapreview.PrepareQueryResult{}, mediapreview.ErrInvalidArgument
	}
	payload, err := client.do(ctx, http.MethodPost, queryPreparationPath, query)
	if err != nil {
		return mediapreview.PrepareQueryResult{}, err
	}
	result, err := mediapreview.DecodePrepareQueryResult(payload, query)
	if err != nil {
		return mediapreview.PrepareQueryResult{}, fmt.Errorf("%w: invalid Business Query response", mediapreview.ErrUnknownOutcome)
	}
	return result, nil
}

// do 发送固定方法/路径、严格 Content-Type、大小上限和无 Redirect 请求。
func (client *Client) do(ctx context.Context, method string, endpointPath string, input any) ([]byte, error) {
	if client == nil || client.baseURL == nil || client.httpClient == nil {
		return nil, fmt.Errorf("Business media client is nil")
	}
	var body io.Reader
	if input != nil {
		encoded, err := mediapreview.CanonicalJSON(input)
		if err != nil {
			return nil, fmt.Errorf("encode Business media request: %w", err)
		}
		body = bytes.NewReader(encoded)
	}
	requestURL := *client.baseURL
	requestURL.Path = endpointPath
	request, err := http.NewRequestWithContext(ctx, method, requestURL.String(), body)
	if err != nil {
		return nil, fmt.Errorf("create Business media request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	if input != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("%w: Business media transport", mediapreview.ErrUnknownOutcome)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, client.maxResponseBytes))
		switch response.StatusCode {
		case http.StatusConflict:
			return nil, fmt.Errorf("%w: Business media conflict", mediapreview.ErrBusinessConflict)
		case http.StatusBadRequest, http.StatusNotFound, http.StatusUnprocessableEntity:
			return nil, fmt.Errorf("%w: Business media rejected request", mediapreview.ErrBusinessPermanent)
		default:
			return nil, fmt.Errorf("%w: Business media HTTP status", mediapreview.ErrUnknownOutcome)
		}
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return nil, fmt.Errorf("%w: Business media response Content-Type", mediapreview.ErrUnknownOutcome)
	}
	payload, err := io.ReadAll(io.LimitReader(response.Body, client.maxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%w: read Business media response", mediapreview.ErrUnknownOutcome)
	}
	if int64(len(payload)) > client.maxResponseBytes {
		return nil, fmt.Errorf("%w: Business media response exceeds budget", mediapreview.ErrUnknownOutcome)
	}
	return payload, nil
}

// isLoopbackHost 只信任 localhost 字面量或明确 loopback IP，不执行 DNS 解析。
func isLoopbackHost(host string) bool {
	if strings.EqualFold(strings.TrimSpace(host), "localhost") {
		return true
	}
	parsed := net.ParseIP(strings.TrimSpace(host))
	return parsed != nil && parsed.IsLoopback()
}

var _ mediapreview.BusinessClient = (*Client)(nil)
