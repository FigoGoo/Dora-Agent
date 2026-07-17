package foundationrpc

import (
	"context"
	"errors"

	"github.com/FigoGoo/Dora-Agent/business/internal/creationspec"
	"github.com/FigoGoo/Dora-Agent/business/internal/storyboardpreview"
	"github.com/FigoGoo/Dora-Agent/business/kitex_gen/foundationv1"
)

const (
	storyboardProjectVersionConflictCode      = "PROJECT_VERSION_CONFLICT"
	storyboardCreationSpecVersionConflictCode = "CREATION_SPEC_VERSION_CONFLICT"
	storyboardIdempotencyConflictCode         = "IDEMPOTENCY_CONFLICT"
)

// GetStoryboardPlanningContextPreviewV1 校验 RPC 信封和精确 CreationSpec 引用后返回 Owner 联合快照。
func (handler *Handler) GetStoryboardPlanningContextPreviewV1(ctx context.Context, request *foundationv1.GetStoryboardPlanningContextPreviewRequestV1) (*foundationv1.GetStoryboardPlanningContextPreviewResponseV1, error) {
	if !handler.storyboardEnabled {
		return nil, creationSpecServiceError(featureDisabledCode, "Storyboard 开发预览未启用", false)
	}
	if handler.storyboardPreview == nil {
		return nil, creationSpecServiceError(previewUnavailableCode, "Storyboard Preview 暂时不可用", true)
	}
	if request == nil || request.CreationSpecRef == nil || !validStoryboardEnvelope(request.SchemaVersion, request.RequestId) ||
		!storyboardpreview.CanonicalUUIDv7(request.UserId) || !storyboardpreview.CanonicalUUIDv7(request.ProjectId) {
		return nil, invalidArgument("Storyboard 规划上下文请求无效")
	}
	reference, err := storyboardCreationSpecRefFromRPC(request.CreationSpecRef)
	if err != nil {
		return nil, invalidArgument("Storyboard CreationSpec 引用无效")
	}
	result, err := handler.storyboardPreview.GetPlanningContext(ctx, storyboardpreview.ContextQuery{
		UserID: request.UserId, ProjectID: request.ProjectId, CreationSpecRef: reference,
	})
	if err != nil {
		return nil, mapStoryboardPreviewServiceError(err)
	}
	creationSpecResource, err := storyboardCreationSpecSnapshotToRPC(result.CreationSpec)
	if err != nil {
		return nil, creationSpecServiceError(persistenceCode, "Storyboard Preview 存储暂时不可用", true)
	}
	handler.logger.InfoContext(ctx, "读取 Storyboard Preview 规划上下文成功",
		"request_id", request.RequestId, "project_id", result.ProjectID,
		"creation_spec_id", result.CreationSpec.ID, "creation_spec_version", result.CreationSpec.Version)
	return &foundationv1.GetStoryboardPlanningContextPreviewResponseV1{
		SchemaVersion: foundationv1.STORYBOARD_PREVIEW_RPC_SCHEMA_VERSION, RequestId: request.RequestId,
		ProjectId: result.ProjectID, ProjectVersion: result.ProjectVersion, ProjectTitle: result.ProjectTitle,
		CreationSpec: creationSpecResource,
	}, nil
}

// SaveStoryboardDraftPreviewV1 显式转换 Thrift DTO，并保存或重放不可变 Storyboard Preview Draft。
func (handler *Handler) SaveStoryboardDraftPreviewV1(ctx context.Context, request *foundationv1.SaveStoryboardDraftPreviewRequestV1) (*foundationv1.SaveStoryboardDraftPreviewResponseV1, error) {
	if !handler.storyboardEnabled {
		return nil, creationSpecServiceError(featureDisabledCode, "Storyboard 开发预览未启用", false)
	}
	if handler.storyboardPreview == nil {
		return nil, creationSpecServiceError(previewUnavailableCode, "Storyboard Preview 暂时不可用", true)
	}
	if request == nil || request.CreationSpecRef == nil || request.Content == nil ||
		!validStoryboardEnvelope(request.SchemaVersion, request.RequestId) {
		return nil, invalidArgument("Storyboard 保存请求无效")
	}
	reference, err := storyboardCreationSpecRefFromRPC(request.CreationSpecRef)
	if err != nil {
		return nil, invalidArgument("Storyboard CreationSpec 引用无效")
	}
	content, err := storyboardContentFromRPC(request.Content)
	if err != nil {
		return nil, invalidArgument("Storyboard 保存内容无效")
	}
	result, err := handler.storyboardPreview.SaveDraft(ctx, storyboardpreview.SaveCommand{
		CommandID: request.CommandId, RequestDigestHex: request.RequestDigest,
		UserID: request.UserId, ProjectID: request.ProjectId, ExpectedProjectVersion: request.ExpectedProjectVersion,
		CreationSpecRef: reference, ToolCallID: request.ToolCallId, PromptVersion: request.PromptVersion,
		ValidatorVersion: request.ValidatorVersion, Content: content,
	})
	if err != nil {
		return nil, mapStoryboardPreviewServiceError(err)
	}
	disposition := foundationv1.StoryboardPreviewCommandDispositionV1_CREATED
	if result.Disposition == storyboardpreview.CommandDispositionReplayed {
		disposition = foundationv1.StoryboardPreviewCommandDispositionV1_REPLAYED
	}
	handler.logger.InfoContext(ctx, "保存 Storyboard Preview Draft 成功",
		"request_id", request.RequestId, "command_id", request.CommandId,
		"storyboard_preview_id", result.Draft.ID, "disposition", string(result.Disposition),
		"content_digest", result.Draft.ContentDigest.Hex())
	return &foundationv1.SaveStoryboardDraftPreviewResponseV1{
		SchemaVersion: foundationv1.STORYBOARD_PREVIEW_RPC_SCHEMA_VERSION, RequestId: request.RequestId,
		CommandId: request.CommandId, Disposition: disposition, Resource: storyboardResourceToRPC(result.Draft),
	}, nil
}

// QueryStoryboardDraftCommandPreviewV1 只查询原命令和摘要，不创建或改写 Storyboard Draft。
func (handler *Handler) QueryStoryboardDraftCommandPreviewV1(ctx context.Context, request *foundationv1.QueryStoryboardDraftCommandPreviewRequestV1) (*foundationv1.QueryStoryboardDraftCommandPreviewResponseV1, error) {
	if !handler.storyboardEnabled {
		return nil, creationSpecServiceError(featureDisabledCode, "Storyboard 开发预览未启用", false)
	}
	if handler.storyboardPreview == nil {
		return nil, creationSpecServiceError(previewUnavailableCode, "Storyboard Preview 暂时不可用", true)
	}
	if request == nil || !validStoryboardEnvelope(request.SchemaVersion, request.RequestId) {
		return nil, invalidArgument("Storyboard 命令查询请求无效")
	}
	result, err := handler.storyboardPreview.QueryCommand(
		ctx, request.CommandId, request.RequestDigest, request.UserId, request.ProjectId,
	)
	if err != nil {
		return nil, mapStoryboardPreviewServiceError(err)
	}
	response := &foundationv1.QueryStoryboardDraftCommandPreviewResponseV1{
		SchemaVersion: foundationv1.STORYBOARD_PREVIEW_RPC_SCHEMA_VERSION,
		RequestId:     request.RequestId, CommandId: request.CommandId,
	}
	switch result.Status {
	case storyboardpreview.QueryStatusNotFound:
		response.Status = foundationv1.StoryboardPreviewQueryStatusV1_NOT_FOUND
	case storyboardpreview.QueryStatusConflict:
		response.Status = foundationv1.StoryboardPreviewQueryStatusV1_CONFLICT
	case storyboardpreview.QueryStatusCompleted:
		response.Status = foundationv1.StoryboardPreviewQueryStatusV1_COMPLETED
		response.Resource = storyboardResourceToRPC(*result.Draft)
	default:
		return nil, creationSpecServiceError(persistenceCode, "Storyboard Preview 存储暂时不可用", true)
	}
	return response, nil
}

// validStoryboardEnvelope 校验 Storyboard Preview RPC 版本和规范 UUIDv7 Request ID。
func validStoryboardEnvelope(schemaVersion string, requestID string) bool {
	return schemaVersion == foundationv1.STORYBOARD_PREVIEW_RPC_SCHEMA_VERSION && storyboardpreview.CanonicalUUIDv7(requestID)
}

// storyboardCreationSpecRefFromRPC 把 Thrift 精确引用转换为领域值。
func storyboardCreationSpecRefFromRPC(value *foundationv1.StoryboardPreviewCreationSpecRefV1) (storyboardpreview.CreationSpecRef, error) {
	if value == nil {
		return storyboardpreview.CreationSpecRef{}, storyboardpreview.ErrInvalidInput
	}
	digest, err := storyboardpreview.ParseDigest(value.ContentDigest)
	if err != nil {
		return storyboardpreview.CreationSpecRef{}, err
	}
	reference := storyboardpreview.CreationSpecRef{ID: value.CreationSpecId, Version: value.Version, ContentDigest: digest}
	if !storyboardpreview.ValidateCreationSpecRef(reference) {
		return storyboardpreview.CreationSpecRef{}, storyboardpreview.ErrInvalidInput
	}
	return reference, nil
}

// storyboardContentFromRPC 显式转换 Storyboard Thrift Content，并由领域层复核全部结构和 DAG。
func storyboardContentFromRPC(value *foundationv1.StoryboardPreviewContentV1) (storyboardpreview.Content, error) {
	if value == nil || value.Sections == nil || value.Elements == nil || value.Slots == nil {
		return storyboardpreview.Content{}, storyboardpreview.ErrInvalidInput
	}
	sections := make([]storyboardpreview.Section, len(value.Sections))
	for index, section := range value.Sections {
		if section == nil {
			return storyboardpreview.Content{}, storyboardpreview.ErrInvalidInput
		}
		sections[index] = storyboardpreview.Section{Key: section.Key, Title: section.Title, Objective: section.Objective}
	}
	elements := make([]storyboardpreview.Element, len(value.Elements))
	for index, element := range value.Elements {
		if element == nil || element.DependencyKeys == nil {
			return storyboardpreview.Content{}, storyboardpreview.ErrInvalidInput
		}
		elementType, err := storyboardElementTypeFromRPC(element.ElementType)
		if err != nil {
			return storyboardpreview.Content{}, err
		}
		elements[index] = storyboardpreview.Element{
			Key: element.Key, SectionKey: element.SectionKey, Order: element.Order, Type: elementType,
			Title: element.Title, NarrativePurpose: element.NarrativePurpose,
			DurationSeconds: element.DurationSeconds, SourcePhaseKey: element.SourcePhaseKey,
			DependencyKeys: append([]string{}, element.DependencyKeys...),
		}
	}
	slots := make([]storyboardpreview.Slot, len(value.Slots))
	for index, slot := range value.Slots {
		if slot == nil {
			return storyboardpreview.Content{}, storyboardpreview.ErrInvalidInput
		}
		slotType, err := storyboardSlotTypeFromRPC(slot.SlotType)
		if err != nil {
			return storyboardpreview.Content{}, err
		}
		slots[index] = storyboardpreview.Slot{
			Key: slot.Key, ElementKey: slot.ElementKey, Type: slotType, Purpose: slot.Purpose, Required: slot.Required,
		}
	}
	content := storyboardpreview.Content{Title: value.Title, Summary: value.Summary, Sections: sections, Elements: elements, Slots: slots}
	if err := storyboardpreview.ValidateContent(content); err != nil {
		return storyboardpreview.Content{}, err
	}
	return content, nil
}

// storyboardResourceToRPC 显式映射安全 Draft Resource，不暴露 Owner、来源内部版本或模型原文。
func storyboardResourceToRPC(draft storyboardpreview.Draft) *foundationv1.StoryboardDraftPreviewResourceV1 {
	return &foundationv1.StoryboardDraftPreviewResourceV1{
		StoryboardPreviewId: draft.ID, ProjectId: draft.ProjectID,
		CreationSpecRef: storyboardCreationSpecRefToRPC(draft.CreationSpecRef), Version: draft.Version,
		Status: draft.Status, ContentDigest: draft.ContentDigest.Hex(), Content: storyboardContentToRPC(draft.Content),
	}
}

// storyboardCreationSpecSnapshotToRPC 把权威 CreationSpec 快照映射为现有安全 Resource DTO。
func storyboardCreationSpecSnapshotToRPC(snapshot storyboardpreview.CreationSpecSnapshot) (*foundationv1.CreationSpecDraftPreviewResourceV1, error) {
	digest, err := creationspec.ParseDigest(snapshot.ContentDigest.Hex())
	if err != nil {
		return nil, err
	}
	return creationSpecResourceToRPC(creationspec.Draft{
		ID: snapshot.ID, ProjectID: snapshot.ProjectID, UserID: snapshot.UserID,
		Status: snapshot.Status, Version: snapshot.Version, SchemaVersion: snapshot.SchemaVersion,
		Content: snapshot.Content, ContentDigest: digest,
	}), nil
}

// storyboardCreationSpecRefToRPC 映射精确 CreationSpec 引用。
func storyboardCreationSpecRefToRPC(reference storyboardpreview.CreationSpecRef) *foundationv1.StoryboardPreviewCreationSpecRefV1 {
	return &foundationv1.StoryboardPreviewCreationSpecRefV1{
		CreationSpecId: reference.ID, Version: reference.Version, ContentDigest: reference.ContentDigest.Hex(),
	}
}

// storyboardContentToRPC 映射严格 Storyboard Preview Content。
func storyboardContentToRPC(content storyboardpreview.Content) *foundationv1.StoryboardPreviewContentV1 {
	sections := make([]*foundationv1.StoryboardPreviewSectionV1, len(content.Sections))
	for index, section := range content.Sections {
		sections[index] = &foundationv1.StoryboardPreviewSectionV1{Key: section.Key, Title: section.Title, Objective: section.Objective}
	}
	elements := make([]*foundationv1.StoryboardPreviewElementV1, len(content.Elements))
	for index, element := range content.Elements {
		elements[index] = &foundationv1.StoryboardPreviewElementV1{
			Key: element.Key, SectionKey: element.SectionKey, Order: element.Order,
			ElementType: storyboardElementTypeToRPC(element.Type), Title: element.Title,
			NarrativePurpose: element.NarrativePurpose, DurationSeconds: element.DurationSeconds,
			SourcePhaseKey: element.SourcePhaseKey, DependencyKeys: append([]string{}, element.DependencyKeys...),
		}
	}
	slots := make([]*foundationv1.StoryboardPreviewSlotV1, len(content.Slots))
	for index, slot := range content.Slots {
		slots[index] = &foundationv1.StoryboardPreviewSlotV1{
			Key: slot.Key, ElementKey: slot.ElementKey, SlotType: storyboardSlotTypeToRPC(slot.Type),
			Purpose: slot.Purpose, Required: slot.Required,
		}
	}
	return &foundationv1.StoryboardPreviewContentV1{
		Title: content.Title, Summary: content.Summary, Sections: sections, Elements: elements, Slots: slots,
	}
}

// storyboardElementTypeFromRPC 把 Thrift 元素枚举转换为领域稳定代码。
func storyboardElementTypeFromRPC(value foundationv1.StoryboardPreviewElementTypeV1) (storyboardpreview.ElementType, error) {
	switch value {
	case foundationv1.StoryboardPreviewElementTypeV1_SCENE:
		return storyboardpreview.ElementTypeScene, nil
	case foundationv1.StoryboardPreviewElementTypeV1_SHOT:
		return storyboardpreview.ElementTypeShot, nil
	case foundationv1.StoryboardPreviewElementTypeV1_NARRATION:
		return storyboardpreview.ElementTypeNarration, nil
	case foundationv1.StoryboardPreviewElementTypeV1_CAPTION:
		return storyboardpreview.ElementTypeCaption, nil
	case foundationv1.StoryboardPreviewElementTypeV1_AUDIO:
		return storyboardpreview.ElementTypeAudio, nil
	default:
		return "", storyboardpreview.ErrInvalidInput
	}
}

// storyboardElementTypeToRPC 把已校验领域元素类型映射为 Thrift 枚举。
func storyboardElementTypeToRPC(value storyboardpreview.ElementType) foundationv1.StoryboardPreviewElementTypeV1 {
	switch value {
	case storyboardpreview.ElementTypeScene:
		return foundationv1.StoryboardPreviewElementTypeV1_SCENE
	case storyboardpreview.ElementTypeShot:
		return foundationv1.StoryboardPreviewElementTypeV1_SHOT
	case storyboardpreview.ElementTypeNarration:
		return foundationv1.StoryboardPreviewElementTypeV1_NARRATION
	case storyboardpreview.ElementTypeCaption:
		return foundationv1.StoryboardPreviewElementTypeV1_CAPTION
	case storyboardpreview.ElementTypeAudio:
		return foundationv1.StoryboardPreviewElementTypeV1_AUDIO
	default:
		return 0
	}
}

// storyboardSlotTypeFromRPC 把 Thrift 槽枚举转换为领域稳定代码。
func storyboardSlotTypeFromRPC(value foundationv1.StoryboardPreviewSlotTypeV1) (storyboardpreview.SlotType, error) {
	switch value {
	case foundationv1.StoryboardPreviewSlotTypeV1_IMAGE:
		return storyboardpreview.SlotTypeImage, nil
	case foundationv1.StoryboardPreviewSlotTypeV1_VIDEO:
		return storyboardpreview.SlotTypeVideo, nil
	case foundationv1.StoryboardPreviewSlotTypeV1_AUDIO:
		return storyboardpreview.SlotTypeAudio, nil
	case foundationv1.StoryboardPreviewSlotTypeV1_VOICEOVER:
		return storyboardpreview.SlotTypeVoiceover, nil
	case foundationv1.StoryboardPreviewSlotTypeV1_CAPTION:
		return storyboardpreview.SlotTypeCaption, nil
	default:
		return "", storyboardpreview.ErrInvalidInput
	}
}

// storyboardSlotTypeToRPC 把已校验领域槽类型映射为 Thrift 枚举。
func storyboardSlotTypeToRPC(value storyboardpreview.SlotType) foundationv1.StoryboardPreviewSlotTypeV1 {
	switch value {
	case storyboardpreview.SlotTypeImage:
		return foundationv1.StoryboardPreviewSlotTypeV1_IMAGE
	case storyboardpreview.SlotTypeVideo:
		return foundationv1.StoryboardPreviewSlotTypeV1_VIDEO
	case storyboardpreview.SlotTypeAudio:
		return foundationv1.StoryboardPreviewSlotTypeV1_AUDIO
	case storyboardpreview.SlotTypeVoiceover:
		return foundationv1.StoryboardPreviewSlotTypeV1_VOICEOVER
	case storyboardpreview.SlotTypeCaption:
		return foundationv1.StoryboardPreviewSlotTypeV1_CAPTION
	default:
		return 0
	}
}

// mapStoryboardPreviewServiceError 把稳定领域错误映射为安全 Thrift 业务异常。
func mapStoryboardPreviewServiceError(err error) error {
	switch {
	case errors.Is(err, storyboardpreview.ErrInvalidInput):
		return invalidArgument("Storyboard Preview 请求无效")
	case errors.Is(err, storyboardpreview.ErrNotFound):
		return creationSpecServiceError(notFoundCode, "Project 或 CreationSpec 不存在或不可访问", false)
	case errors.Is(err, storyboardpreview.ErrProjectVersionConflict):
		return creationSpecServiceError(storyboardProjectVersionConflictCode, "Project 版本已变化", false)
	case errors.Is(err, storyboardpreview.ErrCreationSpecVersionConflict):
		return creationSpecServiceError(storyboardCreationSpecVersionConflictCode, "CreationSpec 版本或摘要已变化", false)
	case errors.Is(err, storyboardpreview.ErrIdempotencyConflict):
		return creationSpecServiceError(storyboardIdempotencyConflictCode, "命令已用于不同的 Storyboard 请求", false)
	case errors.Is(err, context.Canceled):
		return creationSpecServiceError(persistenceCode, "Storyboard Preview 请求已取消", true)
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, storyboardpreview.ErrPersistence):
		return creationSpecServiceError(persistenceCode, "Storyboard Preview 存储暂时不可用", true)
	default:
		return creationSpecServiceError(persistenceCode, "Storyboard Preview 存储暂时不可用", true)
	}
}
