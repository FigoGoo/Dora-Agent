package businessrpc

import (
	"context"
	"errors"

	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/writeprompts"
	"github.com/FigoGoo/Dora-Agent/agent/kitex_gen/foundationv1"
)

// GetPromptGenerationContext 读取 Owner 校验后的 Project 与 Storyboard Preview 一致快照。
// Adapter 只保留 Graph 所需的最小 Source 投影，并使用完整 Business Aggregate 摘要复核可信引用。
func (c *Client) GetPromptGenerationContext(ctx context.Context, trusted writeprompts.TrustedContext) (writeprompts.GenerationContext, error) {
	if c.promptPreview == nil {
		return writeprompts.GenerationContext{}, writeprompts.ErrBusinessTechnical
	}
	if err := writeprompts.ValidateTrustedContext(trusted); err != nil {
		return writeprompts.GenerationContext{}, writeprompts.ErrBusinessTechnical
	}
	request := &foundationv1.GetPromptGenerationContextPreviewRequestV1{
		SchemaVersion: foundationv1.PROMPT_PREVIEW_RPC_SCHEMA_VERSION,
		RequestId:     trusted.RequestID,
		UserId:        trusted.UserID,
		ProjectId:     trusted.ProjectID,
		StoryboardPreviewRef: &foundationv1.PromptPreviewStoryboardRefV1{
			StoryboardPreviewId: trusted.StoryboardPreviewRef.ID,
			Version:             trusted.StoryboardPreviewRef.Version,
			ContentDigest:       trusted.StoryboardPreviewRef.ContentDigest,
		},
	}
	requestCtx, cancel := context.WithTimeout(ctx, c.config.RequestTimeout)
	defer cancel()
	response, err := c.promptPreview.GetPromptGenerationContextPreviewV1(requestCtx, request)
	if err != nil {
		return writeprompts.GenerationContext{}, mapPromptPreviewRPCError(err, promptPreviewRead)
	}
	if response == nil || response.SchemaVersion != request.SchemaVersion || response.RequestId != request.RequestId ||
		response.ProjectId != request.ProjectId || response.StoryboardPreview == nil {
		return writeprompts.GenerationContext{}, writeprompts.ErrBusinessTechnical
	}
	storyboard, err := mapPromptGenerationStoryboard(response.StoryboardPreview)
	if err != nil {
		return writeprompts.GenerationContext{}, writeprompts.ErrBusinessTechnical
	}
	result := writeprompts.GenerationContext{
		ProjectID:      response.ProjectId,
		ProjectVersion: response.ProjectVersion,
		ProjectTitle:   response.ProjectTitle,
		Storyboard:     storyboard,
	}
	if err := writeprompts.ValidateGenerationContext(result, trusted); err != nil {
		return writeprompts.GenerationContext{}, writeprompts.ErrBusinessTechnical
	}
	return result, nil
}

// SavePromptPreviewDraft 调用唯一 Prompt Preview 写 RPC。
// 请求发出后的传输、响应形状或资源校验歧义一律返回 Unknown Outcome，禁止 Adapter 重发或更换命令键。
func (c *Client) SavePromptPreviewDraft(ctx context.Context, command writeprompts.DraftCommand) (writeprompts.SaveDisposition, writeprompts.Resource, error) {
	if c.promptPreview == nil {
		return "", writeprompts.Resource{}, writeprompts.ErrBusinessTechnical
	}
	recomputedDigest, err := writeprompts.SaveRequestDigest(command)
	if err != nil || recomputedDigest != command.RequestDigest {
		return "", writeprompts.Resource{}, writeprompts.ErrBusinessTechnical
	}
	content, err := mapPromptContentToProtocol(command.Content)
	if err != nil {
		return "", writeprompts.Resource{}, writeprompts.ErrBusinessTechnical
	}
	trusted := command.TrustedContext
	request := &foundationv1.SavePromptDraftPreviewRequestV1{
		SchemaVersion:          foundationv1.PROMPT_PREVIEW_RPC_SCHEMA_VERSION,
		RequestId:              trusted.RequestID,
		CommandId:              trusted.BusinessCommandID,
		RequestDigest:          command.RequestDigest,
		UserId:                 trusted.UserID,
		ProjectId:              trusted.ProjectID,
		ExpectedProjectVersion: command.DomainContext.ProjectVersion,
		StoryboardPreviewRef: &foundationv1.PromptPreviewStoryboardRefV1{
			StoryboardPreviewId: trusted.StoryboardPreviewRef.ID,
			Version:             trusted.StoryboardPreviewRef.Version,
			ContentDigest:       trusted.StoryboardPreviewRef.ContentDigest,
		},
		ToolCallId:               trusted.ToolCallID,
		PromptVersion:            trusted.PromptVersion,
		ValidatorVersion:         trusted.ValidatorVersion,
		ExactSetValidatorVersion: trusted.ExactSetValidatorVersion,
		ExactTargetSetDigest:     command.ExactTargetSetDigest,
		Content:                  content,
	}
	requestCtx, cancel := context.WithTimeout(ctx, c.config.RequestTimeout)
	defer cancel()
	response, err := c.promptPreview.SavePromptDraftPreviewV1(requestCtx, request)
	if err != nil {
		return "", writeprompts.Resource{}, mapPromptPreviewRPCError(err, promptPreviewSave)
	}
	if response == nil || response.SchemaVersion != request.SchemaVersion || response.RequestId != request.RequestId ||
		response.CommandId != request.CommandId || response.Resource == nil {
		return "", writeprompts.Resource{}, writeprompts.ErrBusinessUnknownOutcome
	}
	disposition := writeprompts.SaveDisposition("")
	switch response.Disposition {
	case foundationv1.PromptPreviewCommandDispositionV1_CREATED:
		disposition = writeprompts.SaveDispositionCreated
	case foundationv1.PromptPreviewCommandDispositionV1_REPLAYED:
		disposition = writeprompts.SaveDispositionReplayed
	default:
		return "", writeprompts.Resource{}, writeprompts.ErrBusinessUnknownOutcome
	}
	resource, err := mapPromptResource(response.Resource)
	if err != nil || writeprompts.ValidateResourceForCommand(resource, command) != nil {
		return "", writeprompts.Resource{}, writeprompts.ErrBusinessUnknownOutcome
	}
	return disposition, resource, nil
}

// QueryPromptPreviewCommand 只查询原 command_id、request_digest、user 与 project 的首次权威结果。
// Query 不创建新副作用；任何非权威或不完整响应都保持 Unknown Outcome，交由持久化恢复流程处理。
func (c *Client) QueryPromptPreviewCommand(ctx context.Context, command writeprompts.DraftCommand) (string, *writeprompts.Resource, error) {
	if c.promptPreview == nil {
		return "", nil, writeprompts.ErrBusinessTechnical
	}
	recomputedDigest, err := writeprompts.SaveRequestDigest(command)
	if err != nil || recomputedDigest != command.RequestDigest {
		return "", nil, writeprompts.ErrBusinessTechnical
	}
	trusted := command.TrustedContext
	request := &foundationv1.QueryPromptDraftCommandPreviewRequestV1{
		SchemaVersion: foundationv1.PROMPT_PREVIEW_RPC_SCHEMA_VERSION,
		RequestId:     trusted.RequestID,
		CommandId:     trusted.BusinessCommandID,
		RequestDigest: command.RequestDigest,
		UserId:        trusted.UserID,
		ProjectId:     trusted.ProjectID,
	}
	requestCtx, cancel := context.WithTimeout(ctx, c.config.RequestTimeout)
	defer cancel()
	response, err := c.promptPreview.QueryPromptDraftCommandPreviewV1(requestCtx, request)
	if err != nil {
		return "", nil, mapPromptPreviewRPCError(err, promptPreviewQuery)
	}
	if response == nil || response.SchemaVersion != request.SchemaVersion || response.RequestId != request.RequestId ||
		response.CommandId != request.CommandId {
		return "", nil, writeprompts.ErrBusinessUnknownOutcome
	}
	switch response.Status {
	case foundationv1.PromptPreviewQueryStatusV1_NOT_FOUND:
		if response.Resource != nil {
			return "", nil, writeprompts.ErrBusinessUnknownOutcome
		}
		return "not_found", nil, nil
	case foundationv1.PromptPreviewQueryStatusV1_CONFLICT:
		if response.Resource != nil {
			return "", nil, writeprompts.ErrBusinessUnknownOutcome
		}
		return "conflict", nil, nil
	case foundationv1.PromptPreviewQueryStatusV1_COMPLETED:
		if response.Resource == nil {
			return "", nil, writeprompts.ErrBusinessUnknownOutcome
		}
		resource, mapErr := mapPromptResource(response.Resource)
		if mapErr != nil || writeprompts.ValidateResourceForCommand(resource, command) != nil {
			return "", nil, writeprompts.ErrBusinessUnknownOutcome
		}
		return "completed", &resource, nil
	default:
		return "", nil, writeprompts.ErrBusinessUnknownOutcome
	}
}

// mapPromptGenerationStoryboard 把生成协议中的最小安全 Source 投影转换为 Graph DTO。
// Source digest 属于完整 Business Aggregate，因此这里只传递摘要，不以裁剪投影重新计算。
func mapPromptGenerationStoryboard(value *foundationv1.PromptGenerationStoryboardResourcePreviewV1) (writeprompts.StoryboardResource, error) {
	if value == nil || value.Content == nil || value.Content.Elements == nil || value.Content.Slots == nil {
		return writeprompts.StoryboardResource{}, writeprompts.ErrBusinessTechnical
	}
	elements := make([]writeprompts.StoryboardElement, len(value.Content.Elements))
	for index, element := range value.Content.Elements {
		if element == nil {
			return writeprompts.StoryboardResource{}, writeprompts.ErrBusinessTechnical
		}
		elements[index] = writeprompts.StoryboardElement{
			Key: element.ElementLocalKey, Order: int(element.Order), Title: element.Title,
			NarrativePurpose: element.NarrativePurpose,
		}
	}
	slots := make([]writeprompts.StoryboardSlot, len(value.Content.Slots))
	for index, slot := range value.Content.Slots {
		if slot == nil {
			return writeprompts.StoryboardResource{}, writeprompts.ErrBusinessTechnical
		}
		slotType, ok := mapStoryboardSlotTypeFromProtocol(slot.SlotType)
		if !ok {
			return writeprompts.StoryboardResource{}, writeprompts.ErrBusinessTechnical
		}
		slots[index] = writeprompts.StoryboardSlot{
			Key: slot.TargetLocalKey, ElementKey: slot.ElementLocalKey, SlotType: slotType,
			Purpose: slot.Purpose, Required: slot.Required,
		}
	}
	return writeprompts.StoryboardResource{
		ID: value.StoryboardPreviewId, ProjectID: value.ProjectId, Version: value.Version,
		Status: value.Status, ContentDigest: value.ContentDigest,
		Content: writeprompts.StoryboardContent{
			SchemaVersion: value.SchemaVersion, Title: value.Content.Title, Summary: value.Content.Summary,
			Elements: elements, Slots: slots,
		},
	}, nil
}

// mapPromptContentToProtocol 显式映射双 Validator 已通过的 Prompt Draft Content。
func mapPromptContentToProtocol(content writeprompts.Content) (*foundationv1.PromptPreviewDraftContentV1, error) {
	if err := writeprompts.ValidateContent(content); err != nil {
		return nil, err
	}
	prompts := make([]*foundationv1.PromptPreviewDraftEntryV1, len(content.Prompts))
	for index, prompt := range content.Prompts {
		slotType, slotOK := mapStoryboardSlotTypeToProtocol(prompt.SlotType)
		mediaKind, mediaOK := mapPromptMediaKindToProtocol(prompt.MediaKind)
		if !slotOK || !mediaOK {
			return nil, writeprompts.ErrBusinessTechnical
		}
		prompts[index] = &foundationv1.PromptPreviewDraftEntryV1{
			TargetLocalKey: prompt.TargetLocalKey, ElementLocalKey: prompt.ElementLocalKey,
			SlotType: slotType, MediaKind: mediaKind, Purpose: prompt.Purpose, Required: prompt.Required,
			PositivePrompt: prompt.PositivePrompt, NegativeConstraints: clonePromptPreviewStrings(prompt.NegativeConstraints),
			OutputLanguage: prompt.OutputLanguage,
		}
	}
	return &foundationv1.PromptPreviewDraftContentV1{
		SchemaVersion: content.SchemaVersion, Mode: content.Mode,
		SourceStoryboardPreviewRef: mapPromptStoryboardRefToProtocol(content.SourceStoryboardPreviewRef),
		Prompts:                    prompts,
	}, nil
}

// mapPromptContentFromProtocol 显式恢复权威 Prompt Draft Content，并拒绝 nil 集合、nil 子项和未知枚举。
func mapPromptContentFromProtocol(content *foundationv1.PromptPreviewDraftContentV1) (writeprompts.Content, error) {
	if content == nil || content.SourceStoryboardPreviewRef == nil || content.Prompts == nil {
		return writeprompts.Content{}, writeprompts.ErrBusinessTechnical
	}
	prompts := make([]writeprompts.PromptEntry, len(content.Prompts))
	for index, prompt := range content.Prompts {
		if prompt == nil || prompt.NegativeConstraints == nil {
			return writeprompts.Content{}, writeprompts.ErrBusinessTechnical
		}
		slotType, slotOK := mapStoryboardSlotTypeFromProtocol(prompt.SlotType)
		mediaKind, mediaOK := mapPromptMediaKindFromProtocol(prompt.MediaKind)
		if !slotOK || !mediaOK {
			return writeprompts.Content{}, writeprompts.ErrBusinessTechnical
		}
		prompts[index] = writeprompts.PromptEntry{
			TargetLocalKey: prompt.TargetLocalKey, ElementLocalKey: prompt.ElementLocalKey,
			SlotType: slotType, MediaKind: mediaKind, Purpose: prompt.Purpose, Required: prompt.Required,
			PositivePrompt: prompt.PositivePrompt, NegativeConstraints: clonePromptPreviewStrings(prompt.NegativeConstraints),
			OutputLanguage: prompt.OutputLanguage,
		}
	}
	return writeprompts.Content{
		SchemaVersion: content.SchemaVersion, Mode: content.Mode,
		SourceStoryboardPreviewRef: mapPromptStoryboardRefFromProtocol(content.SourceStoryboardPreviewRef),
		Prompts:                    prompts,
	}, nil
}

// mapPromptResource 把 Business Prompt Preview Draft 转换为 Graph 的安全冻结资源。
func mapPromptResource(value *foundationv1.PromptPreviewDraftResourceV1) (writeprompts.Resource, error) {
	if value == nil || value.StoryboardPreviewRef == nil || value.Content == nil {
		return writeprompts.Resource{}, writeprompts.ErrBusinessTechnical
	}
	content, err := mapPromptContentFromProtocol(value.Content)
	if err != nil {
		return writeprompts.Resource{}, err
	}
	return writeprompts.Resource{
		PromptPreviewID: value.PromptPreviewId, ProjectID: value.ProjectId,
		StoryboardPreviewRef: mapPromptStoryboardRefFromProtocol(value.StoryboardPreviewRef),
		Version:              value.Version, Status: value.Status, ContentDigest: value.ContentDigest,
		ExactTargetSetDigest: value.ExactTargetSetDigest, Content: content,
	}, nil
}

// mapPromptStoryboardRefToProtocol 显式映射冻结 Storyboard Preview 引用。
func mapPromptStoryboardRefToProtocol(value writeprompts.StoryboardPreviewRef) *foundationv1.PromptPreviewStoryboardRefV1 {
	return &foundationv1.PromptPreviewStoryboardRefV1{
		StoryboardPreviewId: value.ID, Version: value.Version, ContentDigest: value.ContentDigest,
	}
}

// mapPromptStoryboardRefFromProtocol 显式恢复冻结 Storyboard Preview 引用。
func mapPromptStoryboardRefFromProtocol(value *foundationv1.PromptPreviewStoryboardRefV1) writeprompts.StoryboardPreviewRef {
	return writeprompts.StoryboardPreviewRef{ID: value.StoryboardPreviewId, Version: value.Version, ContentDigest: value.ContentDigest}
}

// mapPromptMediaKindToProtocol 映射 Validator 已约束的媒体类型。
func mapPromptMediaKindToProtocol(value string) (foundationv1.PromptPreviewMediaKindV1, bool) {
	switch value {
	case "image":
		return foundationv1.PromptPreviewMediaKindV1_IMAGE, true
	case "video":
		return foundationv1.PromptPreviewMediaKindV1_VIDEO, true
	case "audio":
		return foundationv1.PromptPreviewMediaKindV1_AUDIO, true
	case "text":
		return foundationv1.PromptPreviewMediaKindV1_TEXT, true
	default:
		return 0, false
	}
}

// mapPromptMediaKindFromProtocol 拒绝协议新增但尚未审核的媒体类型。
func mapPromptMediaKindFromProtocol(value foundationv1.PromptPreviewMediaKindV1) (string, bool) {
	switch value {
	case foundationv1.PromptPreviewMediaKindV1_IMAGE:
		return "image", true
	case foundationv1.PromptPreviewMediaKindV1_VIDEO:
		return "video", true
	case foundationv1.PromptPreviewMediaKindV1_AUDIO:
		return "audio", true
	case foundationv1.PromptPreviewMediaKindV1_TEXT:
		return "text", true
	default:
		return "", false
	}
}

// clonePromptPreviewStrings 复制 required list，并保留“空但非 nil”与缺失字段的区别。
func clonePromptPreviewStrings(values []string) []string {
	if values == nil {
		return nil
	}
	result := make([]string, len(values))
	copy(result, values)
	return result
}

type promptPreviewOperation string

const (
	promptPreviewRead  promptPreviewOperation = "read"
	promptPreviewSave  promptPreviewOperation = "save"
	promptPreviewQuery promptPreviewOperation = "query"
)

// mapPromptPreviewRPCError 按只读、写入与权威查询阶段解释 Business 错误所有权。
// 已发出的 Save 与未完成的 Query 无法证明命令未提交，因此未知传输错误必须保留 Unknown Outcome。
func mapPromptPreviewRPCError(err error, operation promptPreviewOperation) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		if operation == promptPreviewRead {
			return err
		}
		return writeprompts.ErrBusinessUnknownOutcome
	}
	var serviceErr *foundationv1.FoundationServiceExceptionV1
	if errors.As(err, &serviceErr) {
		switch serviceErr.Code {
		case "NOT_FOUND":
			return writeprompts.ErrBusinessNotFound
		case "VERSION_CONFLICT":
			return writeprompts.ErrBusinessStoryboardConflict
		case "PROJECT_VERSION_CONFLICT", "IDEMPOTENCY_CONFLICT", "INVALID_ARGUMENT":
			return writeprompts.ErrBusinessConflict
		case "FEATURE_DISABLED":
			if operation == promptPreviewQuery {
				return writeprompts.ErrBusinessUnknownOutcome
			}
			return writeprompts.ErrBusinessDisabled
		case "PERSISTENCE_UNAVAILABLE":
			if operation != promptPreviewRead {
				return writeprompts.ErrBusinessUnknownOutcome
			}
			return writeprompts.ErrBusinessTechnical
		case "PREVIEW_UNAVAILABLE":
			if operation == promptPreviewQuery {
				return writeprompts.ErrBusinessUnknownOutcome
			}
			return writeprompts.ErrBusinessTechnical
		}
	}
	if operation != promptPreviewRead {
		return writeprompts.ErrBusinessUnknownOutcome
	}
	return writeprompts.ErrBusinessTechnical
}

var _ writeprompts.BusinessContextReader = (*Client)(nil)
var _ writeprompts.BusinessDraftStore = (*Client)(nil)
