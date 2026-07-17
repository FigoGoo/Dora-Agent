package foundationrpc

import (
	"context"
	"errors"

	"github.com/FigoGoo/Dora-Agent/business/internal/promptpreview"
	"github.com/FigoGoo/Dora-Agent/business/internal/storyboardpreview"
	"github.com/FigoGoo/Dora-Agent/business/kitex_gen/foundationv1"
)

const promptStoryboardVersionConflictCode = "VERSION_CONFLICT"

// GetPromptGenerationContextPreviewV1 校验 RPC 信封和精确 Storyboard Preview 引用后返回最小生成上下文。
func (handler *Handler) GetPromptGenerationContextPreviewV1(ctx context.Context, request *foundationv1.GetPromptGenerationContextPreviewRequestV1) (*foundationv1.GetPromptGenerationContextPreviewResponseV1, error) {
	if !handler.promptPreviewEnabled {
		return nil, creationSpecServiceError(featureDisabledCode, "Prompt 开发预览未启用", false)
	}
	if handler.promptPreview == nil {
		return nil, creationSpecServiceError(previewUnavailableCode, "Prompt Preview 暂时不可用", true)
	}
	if request == nil || request.StoryboardPreviewRef == nil ||
		!validPromptPreviewEnvelope(request.SchemaVersion, request.RequestId) ||
		!promptpreview.CanonicalUUIDv7(request.UserId) || !promptpreview.CanonicalUUIDv7(request.ProjectId) {
		return nil, invalidArgument("Prompt 生成上下文请求无效")
	}
	reference, err := promptStoryboardRefFromRPC(request.StoryboardPreviewRef)
	if err != nil {
		return nil, invalidArgument("Prompt Storyboard Preview 引用无效")
	}
	result, err := handler.promptPreview.GetGenerationContext(ctx, promptpreview.ContextQuery{
		UserID: request.UserId, ProjectID: request.ProjectId, StoryboardPreviewRef: reference,
	})
	if err != nil {
		return nil, mapPromptPreviewServiceError(err)
	}
	if err := promptpreview.ValidateGenerationContext(result); err != nil ||
		result.ProjectID != request.ProjectId || result.Storyboard.ProjectID != request.ProjectId ||
		result.Storyboard.UserID != request.UserId || result.Storyboard.ID != reference.ID ||
		result.Storyboard.Version != reference.Version || result.Storyboard.ContentDigest.Hex() != reference.ContentDigest {
		return nil, creationSpecServiceError(persistenceCode, "Prompt Preview 存储暂时不可用", true)
	}
	resource := promptGenerationStoryboardResourceToRPC(result.Storyboard)
	handler.logger.InfoContext(ctx, "读取 Prompt Preview 生成上下文成功",
		"request_id", request.RequestId, "project_id", result.ProjectID,
		"storyboard_preview_id", result.Storyboard.ID, "storyboard_preview_version", result.Storyboard.Version,
		"content_digest", result.Storyboard.ContentDigest.Hex())
	return &foundationv1.GetPromptGenerationContextPreviewResponseV1{
		SchemaVersion: foundationv1.PROMPT_PREVIEW_RPC_SCHEMA_VERSION, RequestId: request.RequestId,
		ProjectId: result.ProjectID, ProjectVersion: result.ProjectVersion, ProjectTitle: result.ProjectTitle,
		StoryboardPreview: resource,
	}, nil
}

// SavePromptDraftPreviewV1 显式转换 Thrift DTO，并保存或重放不可变 Prompt Preview Draft。
func (handler *Handler) SavePromptDraftPreviewV1(ctx context.Context, request *foundationv1.SavePromptDraftPreviewRequestV1) (*foundationv1.SavePromptDraftPreviewResponseV1, error) {
	if !handler.promptPreviewEnabled {
		return nil, creationSpecServiceError(featureDisabledCode, "Prompt 开发预览未启用", false)
	}
	if handler.promptPreview == nil {
		return nil, creationSpecServiceError(previewUnavailableCode, "Prompt Preview 暂时不可用", true)
	}
	if request == nil || request.StoryboardPreviewRef == nil || request.Content == nil ||
		!validPromptPreviewEnvelope(request.SchemaVersion, request.RequestId) {
		return nil, invalidArgument("Prompt 保存请求无效")
	}
	reference, err := promptStoryboardRefFromRPC(request.StoryboardPreviewRef)
	if err != nil {
		return nil, invalidArgument("Prompt Storyboard Preview 引用无效")
	}
	content, err := promptPreviewContentFromRPC(request.Content)
	if err != nil || content.SourceStoryboardPreviewRef != reference {
		return nil, invalidArgument("Prompt 保存内容无效")
	}
	result, err := handler.promptPreview.SaveDraft(ctx, promptpreview.SaveCommand{
		CommandID: request.CommandId, RequestDigestHex: request.RequestDigest,
		UserID: request.UserId, ProjectID: request.ProjectId, ExpectedProjectVersion: request.ExpectedProjectVersion,
		StoryboardPreviewRef: reference, ToolCallID: request.ToolCallId, PromptVersion: request.PromptVersion,
		ValidatorVersion: request.ValidatorVersion, ExactSetValidatorVersion: request.ExactSetValidatorVersion,
		ExactTargetSetDigestHex: request.ExactTargetSetDigest, Content: content,
	})
	if err != nil {
		return nil, mapPromptPreviewServiceError(err)
	}
	var disposition foundationv1.PromptPreviewCommandDispositionV1
	switch result.Disposition {
	case promptpreview.CommandDispositionCreated:
		disposition = foundationv1.PromptPreviewCommandDispositionV1_CREATED
	case promptpreview.CommandDispositionReplayed:
		disposition = foundationv1.PromptPreviewCommandDispositionV1_REPLAYED
	default:
		return nil, creationSpecServiceError(persistenceCode, "Prompt Preview 存储暂时不可用", true)
	}
	resource, err := promptPreviewResourceToRPC(result.Draft)
	if err != nil || result.Draft.UserID != request.UserId || result.Draft.ProjectID != request.ProjectId ||
		result.Draft.StoryboardPreviewRef != reference || result.Draft.SourceToolCallID != request.ToolCallId ||
		result.Draft.SourcePromptVersion != request.PromptVersion ||
		result.Draft.SourceValidatorVersion != request.ValidatorVersion ||
		result.Draft.SourceExactSetValidatorVersion != request.ExactSetValidatorVersion ||
		result.Draft.ExactTargetSetDigest.Hex() != request.ExactTargetSetDigest {
		return nil, creationSpecServiceError(persistenceCode, "Prompt Preview 存储暂时不可用", true)
	}
	handler.logger.InfoContext(ctx, "保存 Prompt Preview Draft 成功",
		"request_id", request.RequestId, "command_id", request.CommandId,
		"prompt_preview_id", result.Draft.ID, "disposition", string(result.Disposition),
		"content_digest", result.Draft.ContentDigest.Hex(),
		"exact_target_set_digest", result.Draft.ExactTargetSetDigest.Hex())
	return &foundationv1.SavePromptDraftPreviewResponseV1{
		SchemaVersion: foundationv1.PROMPT_PREVIEW_RPC_SCHEMA_VERSION, RequestId: request.RequestId,
		CommandId: request.CommandId, Disposition: disposition, Resource: resource,
	}, nil
}

// QueryPromptDraftCommandPreviewV1 只查询原命令和摘要，不创建或改写 Prompt Draft。
func (handler *Handler) QueryPromptDraftCommandPreviewV1(ctx context.Context, request *foundationv1.QueryPromptDraftCommandPreviewRequestV1) (*foundationv1.QueryPromptDraftCommandPreviewResponseV1, error) {
	if !handler.promptPreviewEnabled {
		return nil, creationSpecServiceError(featureDisabledCode, "Prompt 开发预览未启用", false)
	}
	if handler.promptPreview == nil {
		return nil, creationSpecServiceError(previewUnavailableCode, "Prompt Preview 暂时不可用", true)
	}
	if request == nil || !validPromptPreviewEnvelope(request.SchemaVersion, request.RequestId) {
		return nil, invalidArgument("Prompt 命令查询请求无效")
	}
	result, err := handler.promptPreview.QueryCommand(
		ctx, request.CommandId, request.RequestDigest, request.UserId, request.ProjectId,
	)
	if err != nil {
		return nil, mapPromptPreviewServiceError(err)
	}
	response := &foundationv1.QueryPromptDraftCommandPreviewResponseV1{
		SchemaVersion: foundationv1.PROMPT_PREVIEW_RPC_SCHEMA_VERSION,
		RequestId:     request.RequestId, CommandId: request.CommandId,
	}
	switch result.Status {
	case promptpreview.QueryStatusNotFound:
		if result.Draft != nil {
			return nil, creationSpecServiceError(persistenceCode, "Prompt Preview 存储暂时不可用", true)
		}
		response.Status = foundationv1.PromptPreviewQueryStatusV1_NOT_FOUND
	case promptpreview.QueryStatusConflict:
		if result.Draft != nil {
			return nil, creationSpecServiceError(persistenceCode, "Prompt Preview 存储暂时不可用", true)
		}
		response.Status = foundationv1.PromptPreviewQueryStatusV1_CONFLICT
	case promptpreview.QueryStatusCompleted:
		if result.Draft == nil || result.Draft.UserID != request.UserId || result.Draft.ProjectID != request.ProjectId {
			return nil, creationSpecServiceError(persistenceCode, "Prompt Preview 存储暂时不可用", true)
		}
		resource, err := promptPreviewResourceToRPC(*result.Draft)
		if err != nil {
			return nil, creationSpecServiceError(persistenceCode, "Prompt Preview 存储暂时不可用", true)
		}
		response.Status = foundationv1.PromptPreviewQueryStatusV1_COMPLETED
		response.Resource = resource
	default:
		return nil, creationSpecServiceError(persistenceCode, "Prompt Preview 存储暂时不可用", true)
	}
	handler.logger.InfoContext(ctx, "查询 Prompt Preview 保存命令成功",
		"request_id", request.RequestId, "command_id", request.CommandId, "status", string(result.Status))
	return response, nil
}

// validPromptPreviewEnvelope 校验 Prompt Preview RPC 版本和规范 UUIDv7 Request ID。
func validPromptPreviewEnvelope(schemaVersion string, requestID string) bool {
	return schemaVersion == foundationv1.PROMPT_PREVIEW_RPC_SCHEMA_VERSION && promptpreview.CanonicalUUIDv7(requestID)
}

// promptStoryboardRefFromRPC 把 Thrift 精确引用转换为 Prompt Preview 领域值。
func promptStoryboardRefFromRPC(value *foundationv1.PromptPreviewStoryboardRefV1) (promptpreview.StoryboardPreviewRef, error) {
	if value == nil {
		return promptpreview.StoryboardPreviewRef{}, promptpreview.ErrInvalidInput
	}
	reference := promptpreview.StoryboardPreviewRef{
		ID: value.StoryboardPreviewId, Version: value.Version, ContentDigest: value.ContentDigest,
	}
	if !promptpreview.ValidateStoryboardPreviewRef(reference) {
		return promptpreview.StoryboardPreviewRef{}, promptpreview.ErrInvalidInput
	}
	return reference, nil
}

// promptStoryboardRefToRPC 映射精确 Storyboard Preview 引用。
func promptStoryboardRefToRPC(reference promptpreview.StoryboardPreviewRef) *foundationv1.PromptPreviewStoryboardRefV1 {
	return &foundationv1.PromptPreviewStoryboardRefV1{
		StoryboardPreviewId: reference.ID, Version: reference.Version, ContentDigest: reference.ContentDigest,
	}
}

// promptGenerationStoryboardResourceToRPC 将权威 Storyboard 快照裁剪为 Prompt 生成所需最小字段。
func promptGenerationStoryboardResourceToRPC(snapshot promptpreview.StoryboardSnapshot) *foundationv1.PromptGenerationStoryboardResourcePreviewV1 {
	elements := make([]*foundationv1.PromptGenerationStoryboardElementPreviewV1, len(snapshot.Content.Elements))
	for index, element := range snapshot.Content.Elements {
		elements[index] = &foundationv1.PromptGenerationStoryboardElementPreviewV1{
			ElementLocalKey: element.Key, Order: element.Order, Title: element.Title,
			NarrativePurpose: element.NarrativePurpose,
		}
	}
	slots := make([]*foundationv1.PromptGenerationStoryboardSlotPreviewV1, len(snapshot.Content.Slots))
	for index, slot := range snapshot.Content.Slots {
		slots[index] = &foundationv1.PromptGenerationStoryboardSlotPreviewV1{
			TargetLocalKey: slot.Key, ElementLocalKey: slot.ElementKey, SlotType: storyboardSlotTypeToRPC(slot.Type),
			Purpose: slot.Purpose, Required: slot.Required,
		}
	}
	return &foundationv1.PromptGenerationStoryboardResourcePreviewV1{
		StoryboardPreviewId: snapshot.ID, ProjectId: snapshot.ProjectID, Version: snapshot.Version,
		Status: snapshot.Status, SchemaVersion: snapshot.SchemaVersion, ContentDigest: snapshot.ContentDigest.Hex(),
		Content: &foundationv1.PromptGenerationStoryboardContentPreviewV1{
			Title: snapshot.Content.Title, Summary: snapshot.Content.Summary, Elements: elements, Slots: slots,
		},
	}
}

// promptPreviewContentFromRPC 显式转换 Prompt Thrift Content，并由领域层复核文本、枚举和目标结构。
func promptPreviewContentFromRPC(value *foundationv1.PromptPreviewDraftContentV1) (promptpreview.Content, error) {
	if value == nil || value.SourceStoryboardPreviewRef == nil || value.Prompts == nil {
		return promptpreview.Content{}, promptpreview.ErrInvalidInput
	}
	reference, err := promptStoryboardRefFromRPC(value.SourceStoryboardPreviewRef)
	if err != nil {
		return promptpreview.Content{}, err
	}
	prompts := make([]promptpreview.PromptEntry, len(value.Prompts))
	for index, entry := range value.Prompts {
		if entry == nil || entry.NegativeConstraints == nil {
			return promptpreview.Content{}, promptpreview.ErrInvalidInput
		}
		slotType, err := storyboardSlotTypeFromRPC(entry.SlotType)
		if err != nil {
			return promptpreview.Content{}, err
		}
		mediaKind, err := promptMediaKindFromRPC(entry.MediaKind)
		if err != nil {
			return promptpreview.Content{}, err
		}
		prompts[index] = promptpreview.PromptEntry{
			TargetLocalKey: entry.TargetLocalKey, ElementLocalKey: entry.ElementLocalKey,
			SlotType: string(slotType), MediaKind: mediaKind, Purpose: entry.Purpose, Required: entry.Required,
			PositivePrompt:      entry.PositivePrompt,
			NegativeConstraints: append([]string{}, entry.NegativeConstraints...),
			OutputLanguage:      entry.OutputLanguage,
		}
	}
	content := promptpreview.Content{
		SchemaVersion: value.SchemaVersion, Mode: value.Mode,
		SourceStoryboardPreviewRef: reference, Prompts: prompts,
	}
	if err := promptpreview.ValidateContent(content); err != nil {
		return promptpreview.Content{}, err
	}
	return content, nil
}

// promptPreviewResourceToRPC 先复核领域 Draft，再映射安全 Prompt Preview Resource。
func promptPreviewResourceToRPC(draft promptpreview.Draft) (*foundationv1.PromptPreviewDraftResourceV1, error) {
	if err := promptpreview.ValidateDraft(draft); err != nil {
		return nil, err
	}
	return &foundationv1.PromptPreviewDraftResourceV1{
		PromptPreviewId: draft.ID, ProjectId: draft.ProjectID,
		StoryboardPreviewRef: promptStoryboardRefToRPC(draft.StoryboardPreviewRef),
		Version:              draft.Version, Status: draft.Status, ContentDigest: draft.ContentDigest.Hex(),
		ExactTargetSetDigest: draft.ExactTargetSetDigest.Hex(), Content: promptPreviewContentToRPC(draft.Content),
	}, nil
}

// promptPreviewContentToRPC 映射严格 Prompt Preview Content，并保留合法空负面约束列表。
func promptPreviewContentToRPC(content promptpreview.Content) *foundationv1.PromptPreviewDraftContentV1 {
	prompts := make([]*foundationv1.PromptPreviewDraftEntryV1, len(content.Prompts))
	for index, entry := range content.Prompts {
		prompts[index] = &foundationv1.PromptPreviewDraftEntryV1{
			TargetLocalKey: entry.TargetLocalKey, ElementLocalKey: entry.ElementLocalKey,
			SlotType:  storyboardSlotTypeToRPC(storyboardpreview.SlotType(entry.SlotType)),
			MediaKind: promptMediaKindToRPC(entry.MediaKind), Purpose: entry.Purpose, Required: entry.Required,
			PositivePrompt:      entry.PositivePrompt,
			NegativeConstraints: append([]string{}, entry.NegativeConstraints...),
			OutputLanguage:      entry.OutputLanguage,
		}
	}
	return &foundationv1.PromptPreviewDraftContentV1{
		SchemaVersion: content.SchemaVersion, Mode: content.Mode,
		SourceStoryboardPreviewRef: promptStoryboardRefToRPC(content.SourceStoryboardPreviewRef), Prompts: prompts,
	}
}

// promptMediaKindFromRPC 把 Thrift 媒体枚举转换为领域稳定代码。
func promptMediaKindFromRPC(value foundationv1.PromptPreviewMediaKindV1) (string, error) {
	switch value {
	case foundationv1.PromptPreviewMediaKindV1_IMAGE:
		return "image", nil
	case foundationv1.PromptPreviewMediaKindV1_VIDEO:
		return "video", nil
	case foundationv1.PromptPreviewMediaKindV1_AUDIO:
		return "audio", nil
	case foundationv1.PromptPreviewMediaKindV1_TEXT:
		return "text", nil
	default:
		return "", promptpreview.ErrInvalidInput
	}
}

// promptMediaKindToRPC 把已校验领域媒体类型映射为 Thrift 枚举。
func promptMediaKindToRPC(value string) foundationv1.PromptPreviewMediaKindV1 {
	switch value {
	case "image":
		return foundationv1.PromptPreviewMediaKindV1_IMAGE
	case "video":
		return foundationv1.PromptPreviewMediaKindV1_VIDEO
	case "audio":
		return foundationv1.PromptPreviewMediaKindV1_AUDIO
	case "text":
		return foundationv1.PromptPreviewMediaKindV1_TEXT
	default:
		return 0
	}
}

// mapPromptPreviewServiceError 把稳定领域错误映射为安全 Thrift 业务异常。
func mapPromptPreviewServiceError(err error) error {
	switch {
	case errors.Is(err, promptpreview.ErrInvalidInput):
		return invalidArgument("Prompt Preview 请求无效")
	case errors.Is(err, promptpreview.ErrNotFound):
		return creationSpecServiceError(notFoundCode, "Project 或 Storyboard Preview 不存在或不可访问", false)
	case errors.Is(err, promptpreview.ErrProjectVersionConflict):
		return creationSpecServiceError(versionConflictCode, "Project 版本已变化", false)
	case errors.Is(err, promptpreview.ErrStoryboardVersionConflict):
		return creationSpecServiceError(promptStoryboardVersionConflictCode, "Storyboard Preview 版本或摘要已变化", false)
	case errors.Is(err, promptpreview.ErrIdempotencyConflict):
		return creationSpecServiceError(idempotencyConflictCode, "命令已用于不同的 Prompt 请求", false)
	case errors.Is(err, context.Canceled):
		return creationSpecServiceError(persistenceCode, "Prompt Preview 请求已取消", true)
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, promptpreview.ErrPersistence):
		return creationSpecServiceError(persistenceCode, "Prompt Preview 存储暂时不可用", true)
	default:
		return creationSpecServiceError(persistenceCode, "Prompt Preview 存储暂时不可用", true)
	}
}
