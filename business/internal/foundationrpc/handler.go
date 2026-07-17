// Package foundationrpc 实现 Foundation RPC v1 协议边界：Probe 保持无业务副作用，显式开发开关下开放 CreationSpec Preview 子集。
package foundationrpc

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/FigoGoo/Dora-Agent/business/internal/assetanalysis"
	"github.com/FigoGoo/Dora-Agent/business/internal/config"
	"github.com/FigoGoo/Dora-Agent/business/internal/creationspec"
	"github.com/FigoGoo/Dora-Agent/business/internal/promptpreview"
	"github.com/FigoGoo/Dora-Agent/business/internal/storyboardpreview"
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

// Handler 承载无副作用 Probe 与受开发开关保护的 CreationSpec Preview RPC，并统一映射 Business 实例身份和安全错误。
type Handler struct {
	identity             config.ServiceConfig
	clock                Clock
	logger               *slog.Logger
	creationSpec         CreationSpecService
	creationSpecEnabled  bool
	assetAnalysis        AssetAnalysisService
	assetAnalysisEnabled bool
	storyboardPreview    StoryboardPreviewService
	storyboardEnabled    bool
	promptPreview        PromptPreviewService
	promptPreviewEnabled bool
}

// CreationSpecService 定义 Foundation Preview RPC 消费的最小 Business 应用边界。
type CreationSpecService interface {
	// GetContext 校验 Project Owner 并返回生成所需的最小上下文。
	GetContext(ctx context.Context, query creationspec.ContextQuery) (creationspec.ProjectContext, error)
	// SaveDraft 以 command_id first-write-wins 语义保存严格 Draft。
	SaveDraft(ctx context.Context, command creationspec.SaveCommand) (creationspec.SaveResult, error)
	// QueryCommand 只查询原命令与摘要，用于收敛保存 RPC 的 Unknown Outcome。
	QueryCommand(ctx context.Context, commandID string, requestDigestHex string, userID string, projectID string) (creationspec.QueryResult, error)
}

// AssetAnalysisService 定义 Foundation 素材分析输入预览消费的最小领域边界。
type AssetAnalysisService interface {
	// BatchGet 返回授权 exact-set 的完整、稳定素材证据快照。
	BatchGet(ctx context.Context, query assetanalysis.Query) (assetanalysis.Snapshot, error)
}

// StoryboardPreviewService 定义 Foundation Storyboard Development Preview 消费的最小应用边界。
type StoryboardPreviewService interface {
	// GetPlanningContext 校验 Owner 和 CreationSpec 精确引用并返回联合快照。
	GetPlanningContext(ctx context.Context, query storyboardpreview.ContextQuery) (storyboardpreview.PlanningContext, error)
	// SaveDraft 以 command_id first-write-wins 语义保存严格 Storyboard Draft。
	SaveDraft(ctx context.Context, command storyboardpreview.SaveCommand) (storyboardpreview.SaveResult, error)
	// QueryCommand 只查询原命令与摘要，用于收敛保存 RPC Unknown Outcome。
	QueryCommand(ctx context.Context, commandID string, requestDigestHex string, userID string, projectID string) (storyboardpreview.QueryResult, error)
}

// PromptPreviewService 定义 Foundation Prompt Development Preview 消费的最小应用边界。
type PromptPreviewService interface {
	// GetGenerationContext 校验 Owner 和 Storyboard Preview 精确引用并返回一致快照。
	GetGenerationContext(ctx context.Context, query promptpreview.ContextQuery) (promptpreview.GenerationContext, error)
	// SaveDraft 以 command_id first-write-wins 语义保存严格 Prompt Draft。
	SaveDraft(ctx context.Context, command promptpreview.SaveCommand) (promptpreview.SaveResult, error)
	// QueryCommand 只查询原命令与摘要，用于收敛保存 RPC Unknown Outcome。
	QueryCommand(ctx context.Context, commandID string, requestDigestHex string, userID string, projectID string) (promptpreview.QueryResult, error)
}

// NewHandler 创建默认 Probe-only 的 Foundation RPC Handler；可选 CreationSpec Service 只为专用构造复用依赖校验。
// 仅传入 Service 永远不会打开 Preview，避免调用方绕过显式开发开关。
func NewHandler(identity config.ServiceConfig, clock Clock, logger *slog.Logger, creationSpecServices ...CreationSpecService) (*Handler, error) {
	if strings.TrimSpace(identity.Name) == "" || strings.TrimSpace(identity.Version) == "" ||
		strings.TrimSpace(identity.Environment) == "" || strings.TrimSpace(identity.InstanceID) == "" {
		return nil, fmt.Errorf("Business 服务身份配置无效")
	}
	if clock == nil || logger == nil {
		return nil, fmt.Errorf("Foundation Handler 依赖缺失")
	}
	if len(creationSpecServices) > 1 || (len(creationSpecServices) == 1 && creationSpecServices[0] == nil) {
		return nil, fmt.Errorf("Foundation Handler CreationSpec 依赖无效")
	}
	var creationSpec CreationSpecService
	if len(creationSpecServices) == 1 {
		creationSpec = creationSpecServices[0]
	}
	return &Handler{
		identity: identity, clock: clock, logger: logger, creationSpec: creationSpec,
		creationSpecEnabled: false,
	}, nil
}

// NewHandlerWithCreationSpecPreview 创建 Runtime Foundation Handler，并把三组 Preview RPC 绑定到显式开发开关。
// enabled=false 时即使 Service 已接线也绝不调用领域层，避免 HTTP 关闭但 RPC 写入口仍开放。
func NewHandlerWithCreationSpecPreview(identity config.ServiceConfig, clock Clock, logger *slog.Logger, creationSpec CreationSpecService, enabled bool) (*Handler, error) {
	if creationSpec == nil {
		return nil, fmt.Errorf("Foundation Handler CreationSpec 依赖缺失")
	}
	handler, err := NewHandler(identity, clock, logger, creationSpec)
	if err != nil {
		return nil, err
	}
	handler.creationSpecEnabled = enabled
	return handler, nil
}

// NewHandlerWithDevelopmentPreviews 为 Runtime 同时接线两个彼此独立、默认关闭的本地开发预览门禁。
// 原 NewHandler 与 NewHandlerWithCreationSpecPreview 保持不变，既有调用方和测试无需迁移。
func NewHandlerWithDevelopmentPreviews(
	identity config.ServiceConfig,
	clock Clock,
	logger *slog.Logger,
	creationSpec CreationSpecService,
	creationSpecEnabled bool,
	assetAnalysis AssetAnalysisService,
	assetAnalysisEnabled bool,
) (*Handler, error) {
	if assetAnalysis == nil {
		return nil, fmt.Errorf("Foundation Handler AssetAnalysis 依赖缺失")
	}
	handler, err := NewHandlerWithCreationSpecPreview(identity, clock, logger, creationSpec, creationSpecEnabled)
	if err != nil {
		return nil, err
	}
	handler.assetAnalysis = assetAnalysis
	handler.assetAnalysisEnabled = assetAnalysisEnabled
	return handler, nil
}

// NewHandlerWithAllDevelopmentPreviews 在保留既有构造兼容性的同时注入 Storyboard Preview 能力。
// storyboardEnabled 必须由后续本地 Runtime 配置显式开启；M1 Bootstrap 固定传 false 以保持失败关闭。
func NewHandlerWithAllDevelopmentPreviews(
	identity config.ServiceConfig,
	clock Clock,
	logger *slog.Logger,
	creationSpec CreationSpecService,
	creationSpecEnabled bool,
	assetAnalysis AssetAnalysisService,
	assetAnalysisEnabled bool,
	storyboardPreview StoryboardPreviewService,
	storyboardEnabled bool,
) (*Handler, error) {
	if storyboardPreview == nil {
		return nil, fmt.Errorf("Foundation Handler Storyboard Preview 依赖缺失")
	}
	handler, err := NewHandlerWithDevelopmentPreviews(
		identity, clock, logger, creationSpec, creationSpecEnabled, assetAnalysis, assetAnalysisEnabled,
	)
	if err != nil {
		return nil, err
	}
	handler.storyboardPreview = storyboardPreview
	handler.storyboardEnabled = storyboardEnabled
	return handler, nil
}

// NewHandlerWithAllDevelopmentPreviewsAndPromptPreview 在兼容既有构造器的同时注入 Prompt Preview 能力。
// promptPreviewEnabled 必须由后续本地 Runtime 配置显式开启；现有 Bootstrap 继续调用旧构造器，因此默认不注入也不开门。
func NewHandlerWithAllDevelopmentPreviewsAndPromptPreview(
	identity config.ServiceConfig,
	clock Clock,
	logger *slog.Logger,
	creationSpec CreationSpecService,
	creationSpecEnabled bool,
	assetAnalysis AssetAnalysisService,
	assetAnalysisEnabled bool,
	storyboardPreview StoryboardPreviewService,
	storyboardEnabled bool,
	promptPreview PromptPreviewService,
	promptPreviewEnabled bool,
) (*Handler, error) {
	if promptPreview == nil {
		return nil, fmt.Errorf("Foundation Handler Prompt Preview 依赖缺失")
	}
	handler, err := NewHandlerWithAllDevelopmentPreviews(
		identity, clock, logger, creationSpec, creationSpecEnabled,
		assetAnalysis, assetAnalysisEnabled, storyboardPreview, storyboardEnabled,
	)
	if err != nil {
		return nil, err
	}
	handler.promptPreview = promptPreview
	handler.promptPreviewEnabled = promptPreviewEnabled
	return handler, nil
}

// Probe 严格校验只读探针请求，并返回可用于跨服务证据关联的实例回执。
func (h *Handler) Probe(_ context.Context, request *foundationv1.FoundationProbeRequestV1) (*foundationv1.FoundationProbeResponseV1, error) {
	if err := validateRequest(request); err != nil {
		return nil, err
	}

	// Probe 只能回显进程启动时冻结的身份，不读取业务数据，防止基础联调接口演变为旁路查询。
	storyboardEnabled := h.storyboardEnabled
	storyboardProfile := ""
	if h.storyboardEnabled {
		storyboardProfile = foundationv1.PLAN_STORYBOARD_RUNTIME_PROFILE
	}
	promptEnabled := h.promptPreviewEnabled
	promptProfile := ""
	if h.promptPreviewEnabled {
		promptProfile = foundationv1.WRITE_PROMPTS_RUNTIME_PROFILE
	}
	response := &foundationv1.FoundationProbeResponseV1{
		SchemaVersion:                foundationv1.FOUNDATION_SCHEMA_VERSION,
		RequestId:                    request.RequestId,
		ServiceName:                  h.identity.Name,
		ServiceVersion:               h.identity.Version,
		Environment:                  h.identity.Environment,
		InstanceId:                   h.identity.InstanceID,
		ReceivedAtUnixMs:             h.clock.Now().UTC().UnixMilli(),
		PlanStoryboardRuntimeEnabled: &storyboardEnabled,
		PlanStoryboardRuntimeProfile: &storyboardProfile,
		WritePromptsRuntimeEnabled:   &promptEnabled,
		WritePromptsRuntimeProfile:   &promptProfile,
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
