package businessrpc

import (
	"context"
	"errors"
	"fmt"

	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/planstoryboard"
	"github.com/FigoGoo/Dora-Agent/agent/kitex_gen/foundationv1"
)

// GetStoryboardPlanningContext 调用只读 Owner RPC，并把生成类型显式映射为 Graph DTO。
func (c *Client) GetStoryboardPlanningContext(ctx context.Context, trusted planstoryboard.TrustedContext) (planstoryboard.PlanningContext, error) {
	if c.storyboardPreview == nil {
		return planstoryboard.PlanningContext{}, fmt.Errorf("get storyboard planning context: preview client is disabled")
	}
	request := &foundationv1.GetStoryboardPlanningContextPreviewRequestV1{
		SchemaVersion: foundationv1.STORYBOARD_PREVIEW_RPC_SCHEMA_VERSION,
		RequestId:     trusted.RequestID,
		UserId:        trusted.UserID,
		ProjectId:     trusted.ProjectID,
		CreationSpecRef: &foundationv1.StoryboardPreviewCreationSpecRefV1{
			CreationSpecId: trusted.CreationSpecRef.ID,
			Version:        trusted.CreationSpecRef.Version,
			ContentDigest:  trusted.CreationSpecRef.ContentDigest,
		},
	}
	requestCtx, cancel := context.WithTimeout(ctx, c.config.RequestTimeout)
	defer cancel()
	response, err := c.storyboardPreview.GetStoryboardPlanningContextPreviewV1(requestCtx, request)
	if err != nil {
		return planstoryboard.PlanningContext{}, mapStoryboardPreviewRPCError(err, storyboardPreviewRead)
	}
	if response == nil || response.SchemaVersion != foundationv1.STORYBOARD_PREVIEW_RPC_SCHEMA_VERSION ||
		response.RequestId != trusted.RequestID || response.ProjectId != trusted.ProjectID ||
		response.ProjectVersion < 1 || response.ProjectTitle == "" || response.CreationSpec == nil {
		return planstoryboard.PlanningContext{}, planstoryboard.ErrBusinessTechnical
	}
	resource, err := mapStoryboardCreationSpec(response.CreationSpec)
	if err != nil || resource.ID != trusted.CreationSpecRef.ID || resource.Version != trusted.CreationSpecRef.Version ||
		resource.ContentDigest != trusted.CreationSpecRef.ContentDigest {
		return planstoryboard.PlanningContext{}, planstoryboard.ErrBusinessTechnical
	}
	return planstoryboard.PlanningContext{
		ProjectID: response.ProjectId, ProjectVersion: response.ProjectVersion,
		ProjectTitle: response.ProjectTitle, CreationSpec: resource,
	}, nil
}

// SaveStoryboardDraft 调用唯一写 RPC；传输或响应歧义一律返回 Unknown Outcome，禁止换键。
func (c *Client) SaveStoryboardDraft(ctx context.Context, command planstoryboard.DraftCommand) (planstoryboard.SaveDisposition, planstoryboard.Resource, error) {
	if c.storyboardPreview == nil {
		return "", planstoryboard.Resource{}, fmt.Errorf("save storyboard preview: client is disabled")
	}
	trusted := command.TrustedContext
	content, err := mapStoryboardContentToProtocol(command.Content)
	if err != nil {
		return "", planstoryboard.Resource{}, planstoryboard.ErrBusinessTechnical
	}
	request := &foundationv1.SaveStoryboardDraftPreviewRequestV1{
		SchemaVersion:          foundationv1.STORYBOARD_PREVIEW_RPC_SCHEMA_VERSION,
		RequestId:              trusted.RequestID,
		CommandId:              trusted.BusinessCommandID,
		RequestDigest:          command.RequestDigest,
		UserId:                 trusted.UserID,
		ProjectId:              trusted.ProjectID,
		ExpectedProjectVersion: command.DomainContext.ProjectVersion,
		CreationSpecRef: &foundationv1.StoryboardPreviewCreationSpecRefV1{
			CreationSpecId: trusted.CreationSpecRef.ID,
			Version:        trusted.CreationSpecRef.Version,
			ContentDigest:  trusted.CreationSpecRef.ContentDigest,
		},
		ToolCallId: trusted.ToolCallID, PromptVersion: trusted.PromptVersion,
		ValidatorVersion: trusted.ValidatorVersion, Content: content,
	}
	requestCtx, cancel := context.WithTimeout(ctx, c.config.RequestTimeout)
	defer cancel()
	response, err := c.storyboardPreview.SaveStoryboardDraftPreviewV1(requestCtx, request)
	if err != nil {
		return "", planstoryboard.Resource{}, mapStoryboardPreviewRPCError(err, storyboardPreviewSave)
	}
	if response == nil || response.SchemaVersion != request.SchemaVersion || response.RequestId != request.RequestId ||
		response.CommandId != request.CommandId || response.Resource == nil {
		return "", planstoryboard.Resource{}, planstoryboard.ErrBusinessUnknownOutcome
	}
	disposition := planstoryboard.SaveDisposition("")
	switch response.Disposition {
	case foundationv1.StoryboardPreviewCommandDispositionV1_CREATED:
		disposition = planstoryboard.SaveDispositionCreated
	case foundationv1.StoryboardPreviewCommandDispositionV1_REPLAYED:
		disposition = planstoryboard.SaveDispositionReplayed
	default:
		return "", planstoryboard.Resource{}, planstoryboard.ErrBusinessUnknownOutcome
	}
	resource, err := mapStoryboardResource(response.Resource)
	if err != nil {
		return "", planstoryboard.Resource{}, planstoryboard.ErrBusinessUnknownOutcome
	}
	return disposition, resource, nil
}

// QueryStoryboardDraftCommand 只查询原 command_id、digest、user 与 project 的首次结果。
func (c *Client) QueryStoryboardDraftCommand(ctx context.Context, command planstoryboard.DraftCommand) (string, *planstoryboard.Resource, error) {
	if c.storyboardPreview == nil {
		return "", nil, fmt.Errorf("query storyboard preview: client is disabled")
	}
	trusted := command.TrustedContext
	request := &foundationv1.QueryStoryboardDraftCommandPreviewRequestV1{
		SchemaVersion: foundationv1.STORYBOARD_PREVIEW_RPC_SCHEMA_VERSION,
		RequestId:     trusted.RequestID, CommandId: trusted.BusinessCommandID, RequestDigest: command.RequestDigest,
		UserId: trusted.UserID, ProjectId: trusted.ProjectID,
	}
	requestCtx, cancel := context.WithTimeout(ctx, c.config.RequestTimeout)
	defer cancel()
	response, err := c.storyboardPreview.QueryStoryboardDraftCommandPreviewV1(requestCtx, request)
	if err != nil {
		return "", nil, mapStoryboardPreviewRPCError(err, storyboardPreviewQuery)
	}
	if response == nil || response.SchemaVersion != request.SchemaVersion || response.RequestId != request.RequestId ||
		response.CommandId != request.CommandId {
		return "", nil, planstoryboard.ErrBusinessUnknownOutcome
	}
	switch response.Status {
	case foundationv1.StoryboardPreviewQueryStatusV1_NOT_FOUND:
		if response.Resource != nil {
			return "", nil, planstoryboard.ErrBusinessUnknownOutcome
		}
		return "not_found", nil, nil
	case foundationv1.StoryboardPreviewQueryStatusV1_CONFLICT:
		if response.Resource != nil {
			return "", nil, planstoryboard.ErrBusinessUnknownOutcome
		}
		return "conflict", nil, nil
	case foundationv1.StoryboardPreviewQueryStatusV1_COMPLETED:
		if response.Resource == nil {
			return "", nil, planstoryboard.ErrBusinessUnknownOutcome
		}
		resource, mapErr := mapStoryboardResource(response.Resource)
		if mapErr != nil {
			return "", nil, planstoryboard.ErrBusinessUnknownOutcome
		}
		return "completed", &resource, nil
	default:
		return "", nil, planstoryboard.ErrBusinessUnknownOutcome
	}
}

// mapStoryboardCreationSpec 显式转换权威 CreationSpec Draft，并拒绝未知 enum 或 nil 集合。
func mapStoryboardCreationSpec(resource *foundationv1.CreationSpecDraftPreviewResourceV1) (planstoryboard.CreationSpecResource, error) {
	if resource == nil || resource.Content == nil || resource.Content.Phases == nil ||
		resource.Content.Constraints == nil || resource.Content.AcceptanceCriteria == nil {
		return planstoryboard.CreationSpecResource{}, fmt.Errorf("map storyboard creation spec: invalid resource")
	}
	deliverable, ok := mapDeliverableFromProtocol(resource.Content.DeliverableType)
	if !ok {
		return planstoryboard.CreationSpecResource{}, fmt.Errorf("map storyboard creation spec: invalid deliverable")
	}
	phases := make([]planstoryboard.CreationSpecPhase, len(resource.Content.Phases))
	for index, phase := range resource.Content.Phases {
		if phase == nil {
			return planstoryboard.CreationSpecResource{}, fmt.Errorf("map storyboard creation spec: nil phase")
		}
		phases[index] = planstoryboard.CreationSpecPhase{Key: phase.Key, Title: phase.Title, Objective: phase.Objective, Output: phase.Output}
	}
	return planstoryboard.CreationSpecResource{
		ID: resource.CreationSpecId, ProjectID: resource.ProjectId, Version: resource.Version,
		Status: resource.Status, ContentDigest: resource.ContentDigest,
		Content: planstoryboard.CreationSpecContent{
			Title: resource.Content.Title, Goal: resource.Content.Goal, DeliverableType: deliverable,
			Audience: resource.Content.Audience, Locale: resource.Content.Locale, Phases: phases,
			Constraints:        cloneStoryboardStrings(resource.Content.Constraints),
			AcceptanceCriteria: cloneStoryboardStrings(resource.Content.AcceptanceCriteria),
		},
	}, nil
}

// mapStoryboardContentToProtocol 把严格 Graph Content 显式映射到 Owner IDL。
func mapStoryboardContentToProtocol(content planstoryboard.Content) (*foundationv1.StoryboardPreviewContentV1, error) {
	sections := make([]*foundationv1.StoryboardPreviewSectionV1, len(content.Sections))
	for index, section := range content.Sections {
		sections[index] = &foundationv1.StoryboardPreviewSectionV1{Key: section.Key, Title: section.Title, Objective: section.Objective}
	}
	elements := make([]*foundationv1.StoryboardPreviewElementV1, len(content.Elements))
	for index, element := range content.Elements {
		elementType, ok := mapStoryboardElementTypeToProtocol(element.ElementType)
		if !ok {
			return nil, fmt.Errorf("map storyboard content: invalid element type")
		}
		elements[index] = &foundationv1.StoryboardPreviewElementV1{
			Key: element.Key, SectionKey: element.SectionKey, Order: int32(element.Order),
			ElementType: elementType, Title: element.Title,
			NarrativePurpose: element.NarrativePurpose, DurationSeconds: int32(element.DurationSeconds),
			SourcePhaseKey: element.SourcePhaseKey, DependencyKeys: cloneStoryboardStrings(element.DependencyKeys),
		}
	}
	slots := make([]*foundationv1.StoryboardPreviewSlotV1, len(content.Slots))
	for index, slot := range content.Slots {
		slotType, ok := mapStoryboardSlotTypeToProtocol(slot.SlotType)
		if !ok {
			return nil, fmt.Errorf("map storyboard content: invalid slot type")
		}
		slots[index] = &foundationv1.StoryboardPreviewSlotV1{
			Key: slot.Key, ElementKey: slot.ElementKey, SlotType: slotType,
			Purpose: slot.Purpose, Required: slot.Required,
		}
	}
	return &foundationv1.StoryboardPreviewContentV1{Title: content.Title, Summary: content.Summary, Sections: sections, Elements: elements, Slots: slots}, nil
}

// mapStoryboardResource 把 Business Draft 响应转换为 Graph 安全资源。
func mapStoryboardResource(resource *foundationv1.StoryboardDraftPreviewResourceV1) (planstoryboard.Resource, error) {
	if resource == nil || resource.CreationSpecRef == nil || resource.Content == nil {
		return planstoryboard.Resource{}, fmt.Errorf("map storyboard resource: missing field")
	}
	content, err := mapStoryboardContentFromProtocol(resource.Content)
	if err != nil {
		return planstoryboard.Resource{}, err
	}
	return planstoryboard.Resource{
		StoryboardPreviewID: resource.StoryboardPreviewId, ProjectID: resource.ProjectId,
		CreationSpecRef: planstoryboard.CreationSpecRef{
			ID: resource.CreationSpecRef.CreationSpecId, Version: resource.CreationSpecRef.Version,
			ContentDigest: resource.CreationSpecRef.ContentDigest,
		},
		Version: resource.Version, Status: resource.Status, ContentDigest: resource.ContentDigest, Content: content,
	}, nil
}

// mapStoryboardContentFromProtocol 显式转换完整 Storyboard 内容并拒绝 nil 子项和未知 enum。
func mapStoryboardContentFromProtocol(value *foundationv1.StoryboardPreviewContentV1) (planstoryboard.Content, error) {
	if value == nil || value.Sections == nil || value.Elements == nil || value.Slots == nil {
		return planstoryboard.Content{}, fmt.Errorf("map storyboard content: invalid collection")
	}
	sections := make([]planstoryboard.Section, len(value.Sections))
	for index, section := range value.Sections {
		if section == nil {
			return planstoryboard.Content{}, fmt.Errorf("map storyboard content: nil section")
		}
		sections[index] = planstoryboard.Section{Key: section.Key, Title: section.Title, Objective: section.Objective}
	}
	elements := make([]planstoryboard.Element, len(value.Elements))
	for index, element := range value.Elements {
		if element == nil {
			return planstoryboard.Content{}, fmt.Errorf("map storyboard content: nil element")
		}
		elementType, ok := mapStoryboardElementTypeFromProtocol(element.ElementType)
		if !ok {
			return planstoryboard.Content{}, fmt.Errorf("map storyboard content: invalid element type")
		}
		elements[index] = planstoryboard.Element{
			Key: element.Key, SectionKey: element.SectionKey, Order: int(element.Order), ElementType: elementType,
			Title: element.Title, NarrativePurpose: element.NarrativePurpose, DurationSeconds: int(element.DurationSeconds),
			SourcePhaseKey: element.SourcePhaseKey, DependencyKeys: cloneStoryboardStrings(element.DependencyKeys),
		}
	}
	slots := make([]planstoryboard.Slot, len(value.Slots))
	for index, slot := range value.Slots {
		if slot == nil {
			return planstoryboard.Content{}, fmt.Errorf("map storyboard content: nil slot")
		}
		slotType, ok := mapStoryboardSlotTypeFromProtocol(slot.SlotType)
		if !ok {
			return planstoryboard.Content{}, fmt.Errorf("map storyboard content: invalid slot type")
		}
		slots[index] = planstoryboard.Slot{
			Key: slot.Key, ElementKey: slot.ElementKey, SlotType: slotType, Purpose: slot.Purpose, Required: slot.Required,
		}
	}
	return planstoryboard.Content{Title: value.Title, Summary: value.Summary, Sections: sections, Elements: elements, Slots: slots}, nil
}

// cloneStoryboardStrings 复制 required list，并保留“空但非 nil”与缺失字段的区别。
func cloneStoryboardStrings(values []string) []string {
	if values == nil {
		return nil
	}
	cloned := make([]string, len(values))
	copy(cloned, values)
	return cloned
}

// mapStoryboardElementTypeToProtocol 映射 Validator 已约束的元素枚举。
func mapStoryboardElementTypeToProtocol(value string) (foundationv1.StoryboardPreviewElementTypeV1, bool) {
	switch value {
	case "scene":
		return foundationv1.StoryboardPreviewElementTypeV1_SCENE, true
	case "shot":
		return foundationv1.StoryboardPreviewElementTypeV1_SHOT, true
	case "narration":
		return foundationv1.StoryboardPreviewElementTypeV1_NARRATION, true
	case "caption":
		return foundationv1.StoryboardPreviewElementTypeV1_CAPTION, true
	case "audio":
		return foundationv1.StoryboardPreviewElementTypeV1_AUDIO, true
	default:
		return 0, false
	}
}

// mapStoryboardElementTypeFromProtocol 拒绝未知元素枚举。
func mapStoryboardElementTypeFromProtocol(value foundationv1.StoryboardPreviewElementTypeV1) (string, bool) {
	switch value {
	case foundationv1.StoryboardPreviewElementTypeV1_SCENE:
		return "scene", true
	case foundationv1.StoryboardPreviewElementTypeV1_SHOT:
		return "shot", true
	case foundationv1.StoryboardPreviewElementTypeV1_NARRATION:
		return "narration", true
	case foundationv1.StoryboardPreviewElementTypeV1_CAPTION:
		return "caption", true
	case foundationv1.StoryboardPreviewElementTypeV1_AUDIO:
		return "audio", true
	default:
		return "", false
	}
}

// mapStoryboardSlotTypeToProtocol 映射 Validator 已约束的槽枚举。
func mapStoryboardSlotTypeToProtocol(value string) (foundationv1.StoryboardPreviewSlotTypeV1, bool) {
	switch value {
	case "image":
		return foundationv1.StoryboardPreviewSlotTypeV1_IMAGE, true
	case "video":
		return foundationv1.StoryboardPreviewSlotTypeV1_VIDEO, true
	case "audio":
		return foundationv1.StoryboardPreviewSlotTypeV1_AUDIO, true
	case "voiceover":
		return foundationv1.StoryboardPreviewSlotTypeV1_VOICEOVER, true
	case "caption":
		return foundationv1.StoryboardPreviewSlotTypeV1_CAPTION, true
	default:
		return 0, false
	}
}

// mapStoryboardSlotTypeFromProtocol 拒绝未知槽枚举。
func mapStoryboardSlotTypeFromProtocol(value foundationv1.StoryboardPreviewSlotTypeV1) (string, bool) {
	switch value {
	case foundationv1.StoryboardPreviewSlotTypeV1_IMAGE:
		return "image", true
	case foundationv1.StoryboardPreviewSlotTypeV1_VIDEO:
		return "video", true
	case foundationv1.StoryboardPreviewSlotTypeV1_AUDIO:
		return "audio", true
	case foundationv1.StoryboardPreviewSlotTypeV1_VOICEOVER:
		return "voiceover", true
	case foundationv1.StoryboardPreviewSlotTypeV1_CAPTION:
		return "caption", true
	default:
		return "", false
	}
}

type storyboardPreviewOperation string

const (
	storyboardPreviewRead  storyboardPreviewOperation = "read"
	storyboardPreviewSave  storyboardPreviewOperation = "save"
	storyboardPreviewQuery storyboardPreviewOperation = "query"
)

// mapStoryboardPreviewRPCError 按只读、写入和权威查询解释 Business 错误所有权。
func mapStoryboardPreviewRPCError(err error, operation storyboardPreviewOperation) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		if operation == storyboardPreviewRead {
			return err
		}
		return planstoryboard.ErrBusinessUnknownOutcome
	}
	var serviceErr *foundationv1.FoundationServiceExceptionV1
	if errors.As(err, &serviceErr) {
		switch serviceErr.Code {
		case "NOT_FOUND":
			return planstoryboard.ErrBusinessNotFound
		case "CREATION_SPEC_VERSION_CONFLICT":
			return planstoryboard.ErrBusinessCreationSpecConflict
		case "PROJECT_VERSION_CONFLICT", "IDEMPOTENCY_CONFLICT", "INVALID_ARGUMENT":
			return planstoryboard.ErrBusinessConflict
		case "FEATURE_DISABLED":
			if operation == storyboardPreviewQuery {
				return planstoryboard.ErrBusinessUnknownOutcome
			}
			return planstoryboard.ErrBusinessDisabled
		case "PERSISTENCE_UNAVAILABLE":
			if operation != storyboardPreviewRead {
				return planstoryboard.ErrBusinessUnknownOutcome
			}
			return planstoryboard.ErrBusinessTechnical
		case "PREVIEW_UNAVAILABLE":
			if operation == storyboardPreviewQuery {
				return planstoryboard.ErrBusinessUnknownOutcome
			}
			return planstoryboard.ErrBusinessTechnical
		}
	}
	if operation != storyboardPreviewRead {
		return planstoryboard.ErrBusinessUnknownOutcome
	}
	return planstoryboard.ErrBusinessTechnical
}

var _ planstoryboard.BusinessContextReader = (*Client)(nil)
var _ planstoryboard.BusinessDraftStore = (*Client)(nil)
