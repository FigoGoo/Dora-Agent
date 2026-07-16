// Package foundationrpc 实现无业务副作用的 Foundation RPC v1 协议边界。
package foundationrpc

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/FigoGoo/Dora-Agent/business/internal/config"
	"github.com/FigoGoo/Dora-Agent/business/kitex_gen/foundationv1"
	"github.com/google/uuid"
)

const (
	invalidArgumentCode = "INVALID_ARGUMENT"
	maxIdentityLength   = 128
)

// Clock 提供可测试的 UTC 当前时间；Foundation Handler 不读取进程外状态。
type Clock interface {
	// Now 返回当前时间，调用方会转换为 UTC Unix 毫秒。
	Now() time.Time
}

// Handler 校验 Foundation Probe DTO，并回显实际处理请求的 Business 实例身份。
type Handler struct {
	identity config.ServiceConfig
	clock    Clock
	logger   *slog.Logger
}

// NewHandler 创建 Foundation RPC Handler；身份、时钟和日志器缺失时返回错误。
func NewHandler(identity config.ServiceConfig, clock Clock, logger *slog.Logger) (*Handler, error) {
	if strings.TrimSpace(identity.Name) == "" || strings.TrimSpace(identity.Version) == "" ||
		strings.TrimSpace(identity.Environment) == "" || strings.TrimSpace(identity.InstanceID) == "" {
		return nil, fmt.Errorf("Business 服务身份配置无效")
	}
	if clock == nil || logger == nil {
		return nil, fmt.Errorf("Foundation Handler 依赖缺失")
	}
	return &Handler{identity: identity, clock: clock, logger: logger}, nil
}

// Probe 严格校验只读探针请求，并返回可用于跨服务证据关联的实例回执。
func (h *Handler) Probe(_ context.Context, request *foundationv1.FoundationProbeRequestV1) (*foundationv1.FoundationProbeResponseV1, error) {
	if err := validateRequest(request); err != nil {
		return nil, err
	}

	// Probe 只能回显进程启动时冻结的身份，不读取业务数据，防止基础联调接口演变为旁路查询。
	response := &foundationv1.FoundationProbeResponseV1{
		SchemaVersion:    foundationv1.FOUNDATION_SCHEMA_VERSION,
		RequestId:        request.RequestId,
		ServiceName:      h.identity.Name,
		ServiceVersion:   h.identity.Version,
		Environment:      h.identity.Environment,
		InstanceId:       h.identity.InstanceID,
		ReceivedAtUnixMs: h.clock.Now().UTC().UnixMilli(),
	}
	h.logger.Info("Foundation RPC Probe 成功",
		"request_id", request.RequestId,
		"caller_service", request.CallerService,
		"instance_id", h.identity.InstanceID,
	)
	return response, nil
}

// validateRequest 拒绝未知版本、非 UUIDv7 标识和无界调用方字符串，保持 v1 失败关闭。
func validateRequest(request *foundationv1.FoundationProbeRequestV1) error {
	if request == nil {
		return invalidArgument("Foundation Probe 请求不能为空")
	}
	if request.SchemaVersion != foundationv1.FOUNDATION_SCHEMA_VERSION {
		return invalidArgument("不支持的 Foundation RPC 版本")
	}
	requestID, err := uuid.Parse(request.RequestId)
	if err != nil || requestID.Version() != 7 {
		return invalidArgument("request_id 必须是 UUIDv7")
	}
	if strings.TrimSpace(request.CallerService) == "" || len(request.CallerService) > maxIdentityLength {
		return invalidArgument("caller_service 长度无效")
	}
	if strings.TrimSpace(request.CallerVersion) == "" || len(request.CallerVersion) > maxIdentityLength {
		return invalidArgument("caller_version 长度无效")
	}
	if request.SentAtUnixMs <= 0 {
		return invalidArgument("sent_at_unix_ms 必须大于零")
	}
	return nil
}

// invalidArgument 构造不暴露内部实现的稳定 Thrift 业务异常。
func invalidArgument(message string) error {
	return &foundationv1.FoundationServiceExceptionV1{
		Code: invalidArgumentCode, Message: message, Retryable: false,
	}
}
