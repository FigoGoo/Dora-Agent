package foundationrpc

import (
	"context"
	"errors"

	"github.com/FigoGoo/Dora-Agent/business/internal/creationspec"
	"github.com/FigoGoo/Dora-Agent/business/kitex_gen/foundationv1"
)

const (
	// CreationSpec Preview RPC 稳定错误码只表达可操作分类，不暴露 GORM、PostgreSQL 或内部拓扑。
	notFoundCode            = "NOT_FOUND"
	versionConflictCode     = "PROJECT_VERSION_CONFLICT"
	idempotencyConflictCode = "IDEMPOTENCY_CONFLICT"
	persistenceCode         = "PERSISTENCE_UNAVAILABLE"
	previewUnavailableCode  = "PREVIEW_UNAVAILABLE"
	featureDisabledCode     = "FEATURE_DISABLED"
)

// GetCreationSpecContextPreviewV1 校验 RPC 版本、Request ID 与 Project Owner 后返回最小安全上下文。
func (handler *Handler) GetCreationSpecContextPreviewV1(ctx context.Context, request *foundationv1.GetCreationSpecContextPreviewRequestV1) (*foundationv1.GetCreationSpecContextPreviewResponseV1, error) {
	if !handler.creationSpecEnabled {
		return nil, creationSpecServiceError(featureDisabledCode, "CreationSpec 开发预览未启用", false)
	}
	if handler.creationSpec == nil {
		return nil, creationSpecServiceError(previewUnavailableCode, "CreationSpec Preview 暂时不可用", true)
	}
	if request == nil || !validCreationSpecEnvelope(request.SchemaVersion, request.RequestId) ||
		!creationspec.CanonicalUUIDv7(request.UserId) || !creationspec.CanonicalUUIDv7(request.ProjectId) {
		return nil, invalidArgument("CreationSpec 上下文请求无效")
	}
	result, err := handler.creationSpec.GetContext(ctx, creationspec.ContextQuery{UserID: request.UserId, ProjectID: request.ProjectId})
	if err != nil {
		return nil, mapCreationSpecServiceError(err)
	}
	handler.logger.InfoContext(ctx, "读取 CreationSpec Preview Project 上下文成功",
		"request_id", request.RequestId, "project_id", result.ProjectID, "project_version", result.Version)
	return &foundationv1.GetCreationSpecContextPreviewResponseV1{
		SchemaVersion: foundationv1.CREATION_SPEC_PREVIEW_SCHEMA_VERSION,
		RequestId:     request.RequestId, ProjectId: result.ProjectID,
		ProjectVersion: result.Version, ProjectTitle: result.Title,
	}, nil
}

// SaveCreationSpecDraftPreviewV1 将 Thrift DTO 显式转换为领域 Command，并返回首次创建或同语义重放结果。
func (handler *Handler) SaveCreationSpecDraftPreviewV1(ctx context.Context, request *foundationv1.SaveCreationSpecDraftPreviewRequestV1) (*foundationv1.SaveCreationSpecDraftPreviewResponseV1, error) {
	if !handler.creationSpecEnabled {
		return nil, creationSpecServiceError(featureDisabledCode, "CreationSpec 开发预览未启用", false)
	}
	if handler.creationSpec == nil {
		return nil, creationSpecServiceError(previewUnavailableCode, "CreationSpec Preview 暂时不可用", true)
	}
	if request == nil || !validCreationSpecEnvelope(request.SchemaVersion, request.RequestId) || request.Content == nil {
		return nil, invalidArgument("CreationSpec 保存请求无效")
	}
	content, err := creationSpecContentFromRPC(request.Content)
	if err != nil {
		return nil, invalidArgument("CreationSpec 保存内容无效")
	}
	result, err := handler.creationSpec.SaveDraft(ctx, creationspec.SaveCommand{
		CommandID: request.CommandId, RequestDigestHex: request.RequestDigest,
		UserID: request.UserId, ProjectID: request.ProjectId,
		ExpectedProjectVersion: request.ExpectedProjectVersion, ToolCallID: request.ToolCallId,
		PromptVersion: request.PromptVersion, ValidatorVersion: request.ValidatorVersion, Content: content,
	})
	if err != nil {
		return nil, mapCreationSpecServiceError(err)
	}
	disposition := foundationv1.CreationSpecPreviewCommandDispositionV1_CREATED
	if result.Disposition == creationspec.CommandDispositionReplayed {
		disposition = foundationv1.CreationSpecPreviewCommandDispositionV1_REPLAYED
	}
	handler.logger.InfoContext(ctx, "保存 CreationSpec Preview Draft 成功",
		"request_id", request.RequestId, "command_id", request.CommandId,
		"creation_spec_id", result.Draft.ID, "disposition", string(result.Disposition),
		"content_digest", result.Draft.ContentDigest.Hex())
	return &foundationv1.SaveCreationSpecDraftPreviewResponseV1{
		SchemaVersion: foundationv1.CREATION_SPEC_PREVIEW_SCHEMA_VERSION,
		RequestId:     request.RequestId, CommandId: request.CommandId, Disposition: disposition,
		Resource: creationSpecResourceToRPC(result.Draft),
	}, nil
}

// QueryCreationSpecDraftCommandPreviewV1 只使用原 command_id 与摘要查询权威回执，不创建或改写 Draft。
func (handler *Handler) QueryCreationSpecDraftCommandPreviewV1(ctx context.Context, request *foundationv1.QueryCreationSpecDraftCommandPreviewRequestV1) (*foundationv1.QueryCreationSpecDraftCommandPreviewResponseV1, error) {
	if !handler.creationSpecEnabled {
		return nil, creationSpecServiceError(featureDisabledCode, "CreationSpec 开发预览未启用", false)
	}
	if handler.creationSpec == nil {
		return nil, creationSpecServiceError(previewUnavailableCode, "CreationSpec Preview 暂时不可用", true)
	}
	if request == nil || !validCreationSpecEnvelope(request.SchemaVersion, request.RequestId) {
		return nil, invalidArgument("CreationSpec 命令查询请求无效")
	}
	result, err := handler.creationSpec.QueryCommand(
		ctx, request.CommandId, request.RequestDigest, request.UserId, request.ProjectId,
	)
	if err != nil {
		return nil, mapCreationSpecServiceError(err)
	}
	response := &foundationv1.QueryCreationSpecDraftCommandPreviewResponseV1{
		SchemaVersion: foundationv1.CREATION_SPEC_PREVIEW_SCHEMA_VERSION,
		RequestId:     request.RequestId, CommandId: request.CommandId,
	}
	switch result.Status {
	case creationspec.QueryStatusNotFound:
		response.Status = foundationv1.CreationSpecPreviewQueryStatusV1_NOT_FOUND
	case creationspec.QueryStatusConflict:
		response.Status = foundationv1.CreationSpecPreviewQueryStatusV1_CONFLICT
	case creationspec.QueryStatusCompleted:
		response.Status = foundationv1.CreationSpecPreviewQueryStatusV1_COMPLETED
		response.Resource = creationSpecResourceToRPC(*result.Draft)
	default:
		return nil, creationSpecServiceError(persistenceCode, "CreationSpec Preview 存储暂时不可用", true)
	}
	handler.logger.InfoContext(ctx, "查询 CreationSpec Preview 保存命令成功",
		"request_id", request.RequestId, "command_id", request.CommandId, "status", string(result.Status))
	return response, nil
}

// validCreationSpecEnvelope 校验 Preview RPC 版本与规范 UUIDv7 Request ID。
func validCreationSpecEnvelope(schemaVersion string, requestID string) bool {
	return schemaVersion == foundationv1.CREATION_SPEC_PREVIEW_SCHEMA_VERSION && creationspec.CanonicalUUIDv7(requestID)
}

// creationSpecContentFromRPC 显式转换 Thrift Content，并由领域层复核全部枚举、Unicode 与数量边界。
func creationSpecContentFromRPC(value *foundationv1.CreationSpecPreviewContentV1) (creationspec.Content, error) {
	if value == nil || value.Phases == nil || value.Constraints == nil || value.AcceptanceCriteria == nil {
		return creationspec.Content{}, creationspec.ErrInvalidInput
	}
	deliverable, err := deliverableFromRPC(value.DeliverableType)
	if err != nil {
		return creationspec.Content{}, err
	}
	phases := make([]creationspec.Phase, len(value.Phases))
	for index, phase := range value.Phases {
		if phase == nil {
			return creationspec.Content{}, creationspec.ErrInvalidInput
		}
		phases[index] = creationspec.Phase{Key: phase.Key, Title: phase.Title, Objective: phase.Objective, Output: phase.Output}
	}
	content := creationspec.Content{
		Title: value.Title, Goal: value.Goal, DeliverableType: deliverable,
		Audience: value.Audience, Locale: value.Locale, Phases: phases,
		Constraints:        cloneCreationSpecRequiredStrings(value.Constraints),
		AcceptanceCriteria: cloneCreationSpecRequiredStrings(value.AcceptanceCriteria),
	}
	if err := creationspec.ValidateContent(content); err != nil {
		return creationspec.Content{}, err
	}
	return content, nil
}

// deliverableFromRPC 把 Thrift 枚举转换为不依赖生成代码的领域稳定代码。
func deliverableFromRPC(value foundationv1.CreationSpecPreviewDeliverableTypeV1) (creationspec.DeliverableType, error) {
	switch value {
	case foundationv1.CreationSpecPreviewDeliverableTypeV1_VIDEO:
		return creationspec.DeliverableTypeVideo, nil
	case foundationv1.CreationSpecPreviewDeliverableTypeV1_IMAGE_SET:
		return creationspec.DeliverableTypeImageSet, nil
	case foundationv1.CreationSpecPreviewDeliverableTypeV1_AUDIO:
		return creationspec.DeliverableTypeAudio, nil
	case foundationv1.CreationSpecPreviewDeliverableTypeV1_MIXED:
		return creationspec.DeliverableTypeMixed, nil
	default:
		return "", creationspec.ErrInvalidInput
	}
}

// creationSpecResourceToRPC 将领域 Draft 显式映射为只包含安全内容与资源引用的 Thrift DTO。
func creationSpecResourceToRPC(draft creationspec.Draft) *foundationv1.CreationSpecDraftPreviewResourceV1 {
	phases := make([]*foundationv1.CreationSpecPreviewPhaseV1, len(draft.Content.Phases))
	for index, phase := range draft.Content.Phases {
		phases[index] = &foundationv1.CreationSpecPreviewPhaseV1{
			Key: phase.Key, Title: phase.Title, Objective: phase.Objective, Output: phase.Output,
		}
	}
	return &foundationv1.CreationSpecDraftPreviewResourceV1{
		CreationSpecId: draft.ID, ProjectId: draft.ProjectID, Version: draft.Version,
		Status: draft.Status, ContentDigest: draft.ContentDigest.Hex(),
		Content: &foundationv1.CreationSpecPreviewContentV1{
			Title: draft.Content.Title, Goal: draft.Content.Goal,
			DeliverableType: deliverableToRPC(draft.Content.DeliverableType),
			Audience:        draft.Content.Audience, Locale: draft.Content.Locale, Phases: phases,
			Constraints:        cloneCreationSpecRequiredStrings(draft.Content.Constraints),
			AcceptanceCriteria: cloneCreationSpecRequiredStrings(draft.Content.AcceptanceCriteria),
		},
	}
}

// cloneCreationSpecRequiredStrings 保留 Thrift required list 的“必填但可为空”语义，防止合法 [] 在协议与领域间退化为 nil。
func cloneCreationSpecRequiredStrings(values []string) []string { return append([]string{}, values...) }

// deliverableToRPC 把已校验领域交付物类型映射为 Thrift 枚举。
func deliverableToRPC(value creationspec.DeliverableType) foundationv1.CreationSpecPreviewDeliverableTypeV1 {
	switch value {
	case creationspec.DeliverableTypeVideo:
		return foundationv1.CreationSpecPreviewDeliverableTypeV1_VIDEO
	case creationspec.DeliverableTypeImageSet:
		return foundationv1.CreationSpecPreviewDeliverableTypeV1_IMAGE_SET
	case creationspec.DeliverableTypeAudio:
		return foundationv1.CreationSpecPreviewDeliverableTypeV1_AUDIO
	case creationspec.DeliverableTypeMixed:
		return foundationv1.CreationSpecPreviewDeliverableTypeV1_MIXED
	default:
		return 0
	}
}

// mapCreationSpecServiceError 把稳定领域错误映射为安全 Thrift 业务异常。
func mapCreationSpecServiceError(err error) error {
	switch {
	case errors.Is(err, creationspec.ErrInvalidInput):
		return invalidArgument("CreationSpec Preview 请求无效")
	case errors.Is(err, creationspec.ErrNotFound):
		return creationSpecServiceError(notFoundCode, "Project 不存在或不可访问", false)
	case errors.Is(err, creationspec.ErrVersionConflict):
		return creationSpecServiceError(versionConflictCode, "Project 版本已变化", false)
	case errors.Is(err, creationspec.ErrIdempotencyConflict):
		return creationSpecServiceError(idempotencyConflictCode, "命令已用于不同的 CreationSpec 请求", false)
	case errors.Is(err, context.Canceled):
		return creationSpecServiceError(persistenceCode, "CreationSpec Preview 请求已取消", true)
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, creationspec.ErrPersistence):
		return creationSpecServiceError(persistenceCode, "CreationSpec Preview 存储暂时不可用", true)
	default:
		return creationSpecServiceError(persistenceCode, "CreationSpec Preview 存储暂时不可用", true)
	}
}

// creationSpecServiceError 构造不暴露内部实现的稳定 Thrift 业务异常。
func creationSpecServiceError(code string, message string, retryable bool) error {
	return &foundationv1.FoundationServiceExceptionV1{Code: code, Message: message, Retryable: retryable}
}
