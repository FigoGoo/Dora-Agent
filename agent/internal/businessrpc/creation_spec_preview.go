package businessrpc

import (
	"context"
	"errors"
	"fmt"

	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/plancreationspec"
	"github.com/FigoGoo/Dora-Agent/agent/kitex_gen/foundationv1"
)

// GetCreationSpecContext 调用 Preview 只读 RPC，并把生成类型显式映射为 Graph 消费 DTO。
func (c *Client) GetCreationSpecContext(
	ctx context.Context,
	requestID string,
	userID string,
	projectID string,
) (plancreationspec.DomainContext, error) {
	requestCtx, cancel := context.WithTimeout(ctx, c.config.RequestTimeout)
	defer cancel()
	if c.preview == nil {
		return plancreationspec.DomainContext{}, fmt.Errorf("get creation spec context: preview client is disabled")
	}
	response, err := c.preview.GetCreationSpecContextPreviewV1(requestCtx, &foundationv1.GetCreationSpecContextPreviewRequestV1{
		SchemaVersion: foundationv1.CREATION_SPEC_PREVIEW_SCHEMA_VERSION,
		RequestId:     requestID, UserId: userID, ProjectId: projectID,
	})
	if err != nil {
		return plancreationspec.DomainContext{}, mapPreviewRPCError(err, previewRPCRead)
	}
	if response == nil || response.SchemaVersion != foundationv1.CREATION_SPEC_PREVIEW_SCHEMA_VERSION ||
		response.RequestId != requestID || response.ProjectId != projectID || response.ProjectVersion < 1 || response.ProjectTitle == "" {
		return plancreationspec.DomainContext{}, fmt.Errorf("get creation spec context: invalid Business response")
	}
	return plancreationspec.DomainContext{
		ProjectID: response.ProjectId, ProjectVersion: response.ProjectVersion, ProjectTitle: response.ProjectTitle,
	}, nil
}

// SaveCreationSpecDraft 调用唯一写 RPC；任何非确定传输失败都标记 Unknown Outcome，禁止 Graph 换键重试。
func (c *Client) SaveCreationSpecDraft(
	ctx context.Context,
	command plancreationspec.DraftCommand,
) (plancreationspec.SaveDisposition, plancreationspec.Resource, error) {
	request := &foundationv1.SaveCreationSpecDraftPreviewRequestV1{
		SchemaVersion:          foundationv1.CREATION_SPEC_PREVIEW_SCHEMA_VERSION,
		RequestId:              command.TrustedContext.RequestID,
		CommandId:              command.TrustedContext.BusinessCommandID,
		RequestDigest:          command.RequestDigest,
		UserId:                 command.TrustedContext.UserID,
		ProjectId:              command.TrustedContext.ProjectID,
		ExpectedProjectVersion: command.DomainContext.ProjectVersion,
		ToolCallId:             command.TrustedContext.ToolCallID,
		PromptVersion:          command.TrustedContext.PromptVersion,
		ValidatorVersion:       command.TrustedContext.ValidatorVersion,
		Content:                mapContentToProtocol(command.Content),
	}
	requestCtx, cancel := context.WithTimeout(ctx, c.config.RequestTimeout)
	defer cancel()
	if c.preview == nil {
		return "", plancreationspec.Resource{}, fmt.Errorf("save creation spec draft: preview client is disabled")
	}
	response, err := c.preview.SaveCreationSpecDraftPreviewV1(requestCtx, request)
	if err != nil {
		return "", plancreationspec.Resource{}, mapPreviewRPCError(err, previewRPCSave)
	}
	if response == nil || response.SchemaVersion != foundationv1.CREATION_SPEC_PREVIEW_SCHEMA_VERSION ||
		response.RequestId != request.RequestId || response.CommandId != request.CommandId || response.Resource == nil {
		return "", plancreationspec.Resource{}, plancreationspec.ErrBusinessUnknownOutcome
	}
	disposition := plancreationspec.SaveDisposition("")
	switch response.Disposition {
	case foundationv1.CreationSpecPreviewCommandDispositionV1_CREATED:
		disposition = plancreationspec.SaveDispositionCreated
	case foundationv1.CreationSpecPreviewCommandDispositionV1_REPLAYED:
		disposition = plancreationspec.SaveDispositionReplayed
	default:
		return "", plancreationspec.Resource{}, plancreationspec.ErrBusinessUnknownOutcome
	}
	resource, err := mapProtocolResource(response.Resource)
	if err != nil {
		return "", plancreationspec.Resource{}, plancreationspec.ErrBusinessUnknownOutcome
	}
	return disposition, resource, nil
}

// QueryCreationSpecDraftCommand 只使用原 command_id、request_digest、user 与 project 核对 Save Unknown Outcome。
func (c *Client) QueryCreationSpecDraftCommand(
	ctx context.Context,
	command plancreationspec.DraftCommand,
) (string, *plancreationspec.Resource, error) {
	request := &foundationv1.QueryCreationSpecDraftCommandPreviewRequestV1{
		SchemaVersion: foundationv1.CREATION_SPEC_PREVIEW_SCHEMA_VERSION,
		RequestId:     command.TrustedContext.RequestID,
		CommandId:     command.TrustedContext.BusinessCommandID,
		RequestDigest: command.RequestDigest,
		UserId:        command.TrustedContext.UserID,
		ProjectId:     command.TrustedContext.ProjectID,
	}
	requestCtx, cancel := context.WithTimeout(ctx, c.config.RequestTimeout)
	defer cancel()
	if c.preview == nil {
		return "", nil, fmt.Errorf("query creation spec command: preview client is disabled")
	}
	response, err := c.preview.QueryCreationSpecDraftCommandPreviewV1(requestCtx, request)
	if err != nil {
		return "", nil, mapPreviewRPCError(err, previewRPCQuery)
	}
	if response == nil || response.SchemaVersion != foundationv1.CREATION_SPEC_PREVIEW_SCHEMA_VERSION ||
		response.RequestId != request.RequestId || response.CommandId != request.CommandId {
		return "", nil, plancreationspec.ErrBusinessUnknownOutcome
	}
	switch response.Status {
	case foundationv1.CreationSpecPreviewQueryStatusV1_NOT_FOUND:
		if response.Resource != nil {
			return "", nil, plancreationspec.ErrBusinessUnknownOutcome
		}
		return "not_found", nil, nil
	case foundationv1.CreationSpecPreviewQueryStatusV1_CONFLICT:
		if response.Resource != nil {
			return "", nil, plancreationspec.ErrBusinessUnknownOutcome
		}
		return "conflict", nil, nil
	case foundationv1.CreationSpecPreviewQueryStatusV1_COMPLETED:
		if response.Resource == nil {
			return "", nil, plancreationspec.ErrBusinessUnknownOutcome
		}
		resource, mapErr := mapProtocolResource(response.Resource)
		if mapErr != nil {
			return "", nil, plancreationspec.ErrBusinessUnknownOutcome
		}
		return "completed", &resource, nil
	default:
		return "", nil, plancreationspec.ErrBusinessUnknownOutcome
	}
}

// mapContentToProtocol 在唯一 RPC 边界显式转换稳定 DTO，生成类型不进入 Graph State。
func mapContentToProtocol(content plancreationspec.Content) *foundationv1.CreationSpecPreviewContentV1 {
	phases := make([]*foundationv1.CreationSpecPreviewPhaseV1, len(content.Phases))
	for index, phase := range content.Phases {
		phases[index] = &foundationv1.CreationSpecPreviewPhaseV1{
			Key: phase.Key, Title: phase.Title, Objective: phase.Objective, Output: phase.Output,
		}
	}
	return &foundationv1.CreationSpecPreviewContentV1{
		Title: content.Title, Goal: content.Goal, DeliverableType: mapDeliverableToProtocol(content.DeliverableType),
		Audience: content.Audience, Locale: content.Locale, Phases: phases,
		Constraints:        cloneCreationSpecRequiredStrings(content.Constraints),
		AcceptanceCriteria: cloneCreationSpecRequiredStrings(content.AcceptanceCriteria),
	}
}

// mapProtocolResource 对 Business 响应执行 nil、枚举和 DTO 显式转换；完整领域校验由 Graph Validator 复核。
func mapProtocolResource(resource *foundationv1.CreationSpecDraftPreviewResourceV1) (plancreationspec.Resource, error) {
	if resource == nil || resource.Content == nil {
		return plancreationspec.Resource{}, fmt.Errorf("map creation spec resource: content is missing")
	}
	content, err := mapProtocolContent(resource.Content)
	if err != nil {
		return plancreationspec.Resource{}, err
	}
	return plancreationspec.Resource{
		ID: resource.CreationSpecId, ProjectID: resource.ProjectId, Version: resource.Version,
		Status: resource.Status, ContentDigest: resource.ContentDigest, Content: content,
	}, nil
}

// mapProtocolContent 把 Thrift Content 显式收敛为 Agent DTO，并拒绝 nil Phase 或未知 enum。
func mapProtocolContent(content *foundationv1.CreationSpecPreviewContentV1) (plancreationspec.Content, error) {
	deliverable, ok := mapDeliverableFromProtocol(content.DeliverableType)
	if !ok || content.Phases == nil || content.Constraints == nil || content.AcceptanceCriteria == nil {
		return plancreationspec.Content{}, fmt.Errorf("map creation spec content: invalid enum or collection")
	}
	phases := make([]plancreationspec.Phase, len(content.Phases))
	for index, phase := range content.Phases {
		if phase == nil {
			return plancreationspec.Content{}, fmt.Errorf("map creation spec content: nil phase")
		}
		phases[index] = plancreationspec.Phase{
			Key: phase.Key, Title: phase.Title, Objective: phase.Objective, Output: phase.Output,
		}
	}
	return plancreationspec.Content{
		Title: content.Title, Goal: content.Goal, DeliverableType: deliverable,
		Audience: content.Audience, Locale: content.Locale, Phases: phases,
		Constraints:        cloneCreationSpecRequiredStrings(content.Constraints),
		AcceptanceCriteria: cloneCreationSpecRequiredStrings(content.AcceptanceCriteria),
	}, nil
}

// cloneCreationSpecRequiredStrings 保留 Thrift required list 的“必填但可为空”语义，禁止跨 RPC 映射时把 [] 退化为 nil。
func cloneCreationSpecRequiredStrings(values []string) []string { return append([]string{}, values...) }

// mapDeliverableToProtocol 映射已由 Validator 约束的稳定枚举。
func mapDeliverableToProtocol(value string) foundationv1.CreationSpecPreviewDeliverableTypeV1 {
	switch value {
	case "video":
		return foundationv1.CreationSpecPreviewDeliverableTypeV1_VIDEO
	case "image_set":
		return foundationv1.CreationSpecPreviewDeliverableTypeV1_IMAGE_SET
	case "audio":
		return foundationv1.CreationSpecPreviewDeliverableTypeV1_AUDIO
	default:
		return foundationv1.CreationSpecPreviewDeliverableTypeV1_MIXED
	}
}

// mapDeliverableFromProtocol 拒绝 Thrift 未知枚举，不把它默认成 mixed。
func mapDeliverableFromProtocol(value foundationv1.CreationSpecPreviewDeliverableTypeV1) (string, bool) {
	switch value {
	case foundationv1.CreationSpecPreviewDeliverableTypeV1_VIDEO:
		return "video", true
	case foundationv1.CreationSpecPreviewDeliverableTypeV1_IMAGE_SET:
		return "image_set", true
	case foundationv1.CreationSpecPreviewDeliverableTypeV1_AUDIO:
		return "audio", true
	case foundationv1.CreationSpecPreviewDeliverableTypeV1_MIXED:
		return "mixed", true
	default:
		return "", false
	}
}

type previewRPCOperation string

const (
	previewRPCRead  previewRPCOperation = "read"
	previewRPCSave  previewRPCOperation = "save"
	previewRPCQuery previewRPCOperation = "query"
)

// mapPreviewRPCError 按调用阶段解释 exact Business code；Save 写前拒绝不得伪装 Unknown Outcome。
func mapPreviewRPCError(err error, operation previewRPCOperation) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		if operation == previewRPCSave || operation == previewRPCQuery {
			return plancreationspec.ErrBusinessUnknownOutcome
		}
		return err
	}
	var serviceErr *foundationv1.FoundationServiceExceptionV1
	if errors.As(err, &serviceErr) {
		switch serviceErr.Code {
		case "NOT_FOUND":
			return plancreationspec.ErrBusinessNotFound
		case "PROJECT_VERSION_CONFLICT", "IDEMPOTENCY_CONFLICT":
			return plancreationspec.ErrBusinessConflict
		case "FEATURE_DISABLED":
			if operation == previewRPCQuery {
				return plancreationspec.ErrBusinessUnknownOutcome
			}
			return plancreationspec.ErrBusinessDisabled
		case "PREVIEW_UNAVAILABLE":
			if operation == previewRPCQuery {
				return plancreationspec.ErrBusinessUnknownOutcome
			}
			return plancreationspec.ErrBusinessTechnical
		case "PERSISTENCE_UNAVAILABLE":
			if operation == previewRPCSave || operation == previewRPCQuery {
				return plancreationspec.ErrBusinessUnknownOutcome
			}
			return plancreationspec.ErrBusinessTechnical
		case "INVALID_ARGUMENT":
			return plancreationspec.ErrBusinessConflict
		default:
			if operation == previewRPCSave || operation == previewRPCQuery {
				return plancreationspec.ErrBusinessUnknownOutcome
			}
			return plancreationspec.ErrBusinessTechnical
		}
	}
	if operation == previewRPCSave || operation == previewRPCQuery {
		return plancreationspec.ErrBusinessUnknownOutcome
	}
	return plancreationspec.ErrBusinessTechnical
}

var _ plancreationspec.BusinessClient = (*Client)(nil)
