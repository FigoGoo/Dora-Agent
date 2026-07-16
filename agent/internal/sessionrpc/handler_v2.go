package sessionrpc

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"time"

	"github.com/FigoGoo/Dora-Agent/agent/internal/session"
	"github.com/FigoGoo/Dora-Agent/agent/internal/skill"
	"github.com/FigoGoo/Dora-Agent/agent/kitex_gen/sessionv1"
	"github.com/google/uuid"
)

const (
	errorCodeCommandConflictV2              = "COMMAND_CONFLICT"
	errorCodeCommandVersionConflictV2       = "COMMAND_VERSION_CONFLICT"
	errorCodeSnapshotDigestMismatchV2       = "SNAPSHOT_DIGEST_MISMATCH"
	errorCodeSnapshotLimitExceededV2        = "SNAPSHOT_LIMIT_EXCEEDED"
	errorCodeContentProtectionUnavailableV2 = "CONTENT_PROTECTION_UNAVAILABLE"
)

// EnsureProjectSessionV2 独立规范化并重算 Runtime、Snapshot 与 Ensure 摘要后调用 V2 Session 用例。
// V2 失败不会调用 V1；Runtime Content、Prompt 和完整请求都不得进入错误或日志。
func (h *Handler) EnsureProjectSessionV2(ctx context.Context, request *sessionv1.EnsureProjectSessionRequestV2) (*sessionv1.EnsureProjectSessionResponseV2, error) {
	requestID := safeRequestIDFromEnsureV2(request)
	command, err := mapEnsureRequestV2WithLimits(request, h.skillSnapshotLimits)
	if err != nil {
		return nil, mapV2TransportError(err, requestID)
	}
	result, err := h.service.EnsureProjectSessionV2(ctx, command)
	if err != nil {
		return nil, mapServiceErrorV2(err, requestID)
	}
	response, err := mapEnsureResponseV2(command, result, h.skillSnapshotLimits.MaxItems)
	if err != nil {
		return nil, newServiceError(errorCodeInternal, "Session 服务内部错误", false, requestID)
	}
	return response, nil
}

// QueryProjectSessionCommandV2 只读核对 V2 命令；跨版本 Command 命中由领域层稳定返回版本冲突。
func (h *Handler) QueryProjectSessionCommandV2(ctx context.Context, request *sessionv1.QueryProjectSessionCommandRequestV2) (*sessionv1.QueryProjectSessionCommandResponseV2, error) {
	requestID := safeRequestIDFromQueryV2(request)
	command, err := mapQueryRequestV2(request)
	if err != nil {
		return nil, newServiceError(errorCodeInvalidArgument, "Session 命令查询不符合 v2 契约", false, requestID)
	}
	result, err := h.service.QueryProjectSessionCommand(ctx, command)
	if err != nil {
		return nil, mapServiceErrorV2(err, requestID)
	}
	response, err := mapQueryResponseV2(command.RequestID, command.CommandID, result, h.skillSnapshotLimits.MaxItems)
	if err != nil {
		return nil, newServiceError(errorCodeInternal, "Session 服务内部错误", false, requestID)
	}
	return response, nil
}

func mapEnsureRequestV2(request *sessionv1.EnsureProjectSessionRequestV2) (session.EnsureCommandV2, error) {
	return mapEnsureRequestV2WithLimits(request, skill.DefaultLimitsProfileV1())
}

func mapEnsureRequestV2WithLimits(request *sessionv1.EnsureProjectSessionRequestV2, limits skill.LimitsProfileV1) (session.EnsureCommandV2, error) {
	if request == nil || request.SchemaVersion != sessionv1.ENSURE_PROJECT_SESSION_SCHEMA_VERSION_V2 {
		return session.EnsureCommandV2{}, fmt.Errorf("unsupported Ensure v2 schema")
	}
	if !isCanonicalUUIDv7V2(request.RequestId) || !isCanonicalUUIDv7V2(request.CommandId) ||
		!isCanonicalUUIDv7V2(request.ProjectId) || !isCanonicalUUIDv7V2(request.OwnerUserId) {
		return session.EnsureCommandV2{}, fmt.Errorf("v2 IDs must be canonical UUIDv7")
	}
	if request.CreationSource != sessionv1.CreationSourceV1_QUICK_CREATE || request.RequestedAtUnixMs <= 0 {
		return session.EnsureCommandV2{}, fmt.Errorf("unsupported v2 source or timestamp")
	}
	snapshot, err := mapSkillSnapshotV2(request.SkillSnapshot)
	if err != nil {
		return session.EnsureCommandV2{}, err
	}
	initialPrompt := request.GetInitialPrompt()
	canonical, err := skill.CanonicalEnsureProjectSessionV2(skill.EnsureProjectSessionInputV2{
		SchemaVersion:  request.SchemaVersion,
		ProjectID:      request.ProjectId,
		OwnerUserID:    request.OwnerUserId,
		CreationSource: skill.CreationSourceQuickCreate,
		InitialPrompt:  initialPrompt,
		SkillSnapshot:  snapshot,
	}, limits)
	if err != nil {
		return session.EnsureCommandV2{}, err
	}
	if !equalDigestV2(request.RequestDigest, canonical.RequestDigest.Hex()) ||
		!equalDigestV2(request.PromptDigest, canonical.PromptDigest) {
		return session.EnsureCommandV2{}, fmt.Errorf("%w: ensure or prompt digest", skill.ErrDigestMismatch)
	}
	return session.EnsureCommandV2{
		SchemaVersion:  request.SchemaVersion,
		RequestID:      request.RequestId,
		CommandID:      request.CommandId,
		RequestDigest:  canonical.RequestDigest.Hex(),
		ProjectID:      request.ProjectId,
		OwnerUserID:    request.OwnerUserId,
		CreationSource: skill.CreationSourceQuickCreate,
		InitialPrompt:  canonical.NormalizedPrompt,
		PromptDigest:   canonical.PromptDigest,
		SkillSnapshot:  canonical.SkillSnapshot,
		RequestedAt:    time.UnixMilli(request.RequestedAtUnixMs).UTC(),
	}, nil
}

func mapSkillSnapshotV2(snapshot *sessionv1.SessionSkillSnapshotV1) (skill.SessionSkillSnapshotV1, error) {
	if snapshot == nil {
		return skill.SessionSkillSnapshotV1{}, fmt.Errorf("skill_snapshot is required")
	}
	kind, err := mapSnapshotKindV2(snapshot.SnapshotKind)
	if err != nil {
		return skill.SessionSkillSnapshotV1{}, err
	}
	result := skill.SessionSkillSnapshotV1{
		SchemaVersion:     snapshot.SchemaVersion,
		SnapshotKind:      kind,
		SkillCount:        snapshot.SkillCount,
		SnapshotSetDigest: snapshot.SnapshotSetDigest,
	}
	if snapshot.Skills != nil {
		result.Skills = make([]skill.PublishedSkillSnapshotRefV1, len(snapshot.Skills))
		for index, item := range snapshot.Skills {
			mapped, mapErr := mapPublishedSkillV2(item)
			if mapErr != nil {
				return skill.SessionSkillSnapshotV1{}, fmt.Errorf("skill[%d]: %w", index, mapErr)
			}
			result.Skills[index] = mapped
		}
	}
	return result, nil
}

func mapPublishedSkillV2(item *sessionv1.PublishedSkillSnapshotRefV1) (skill.PublishedSkillSnapshotRefV1, error) {
	if item == nil || item.RuntimeContent == nil {
		return skill.PublishedSkillSnapshotRefV1{}, fmt.Errorf("published Skill and Runtime Content are required")
	}
	namespace, err := mapNamespaceV2(item.Namespace)
	if err != nil {
		return skill.PublishedSkillSnapshotRefV1{}, err
	}
	runtimeContent, err := mapRuntimeContentV2(item.RuntimeContent)
	if err != nil {
		return skill.PublishedSkillSnapshotRefV1{}, err
	}
	result := skill.PublishedSkillSnapshotRefV1{
		LoadOrder:                   item.LoadOrder,
		Priority:                    item.Priority,
		Namespace:                   namespace,
		SkillID:                     item.SkillId,
		PublisherUserID:             item.PublisherUserId,
		PublishedSnapshotID:         item.PublishedSnapshotId,
		PublicationRevision:         item.PublicationRevision,
		DefinitionSchemaVersion:     item.DefinitionSchemaVersion,
		ContentDigest:               item.ContentDigest,
		RuntimeContentSchemaVersion: item.RuntimeContentSchemaVersion,
		RuntimeContentDigest:        item.RuntimeContentDigest,
		RuntimeContent:              runtimeContent,
		PermissionSnapshotDigest:    item.PermissionSnapshotDigest,
		RuntimePolicyRef:            item.RuntimePolicyRef,
		GovernanceEpoch:             item.GovernanceEpoch,
		PublishedAtUnixMS:           item.PublishedAtUnixMs,
	}
	if item.AllowedGraphToolKeys != nil {
		result.AllowedGraphToolKeys = append([]string(nil), item.AllowedGraphToolKeys...)
	}
	if item.PublicToolRefs != nil {
		result.PublicToolRefs = make([]skill.PublicToolSnapshotRefV1, len(item.PublicToolRefs))
		for index, ref := range item.PublicToolRefs {
			if ref == nil {
				return skill.PublishedSkillSnapshotRefV1{}, fmt.Errorf("public_tool_refs[%d] is nil", index)
			}
			result.PublicToolRefs[index] = skill.PublicToolSnapshotRefV1{RefID: ref.RefId, RefDigest: ref.RefDigest}
		}
	}
	return result, nil
}

func mapRuntimeContentV2(content *sessionv1.SkillRuntimeContentV1) (skill.SkillRuntimeContentV1, error) {
	planCreation, err := mapGuidanceV2(content.PlanCreationSpec)
	if err != nil {
		return skill.SkillRuntimeContentV1{}, err
	}
	analyzeMaterials, err := mapGuidanceV2(content.AnalyzeMaterials)
	if err != nil {
		return skill.SkillRuntimeContentV1{}, err
	}
	planStoryboard, err := mapGuidanceV2(content.PlanStoryboard)
	if err != nil {
		return skill.SkillRuntimeContentV1{}, err
	}
	generateMedia, err := mapGuidanceV2(content.GenerateMedia)
	if err != nil {
		return skill.SkillRuntimeContentV1{}, err
	}
	writePrompts, err := mapGuidanceV2(content.WritePrompts)
	if err != nil {
		return skill.SkillRuntimeContentV1{}, err
	}
	assembleOutput, err := mapGuidanceV2(content.AssembleOutput)
	if err != nil {
		return skill.SkillRuntimeContentV1{}, err
	}
	result := skill.SkillRuntimeContentV1{
		SchemaVersion:     content.SchemaVersion,
		Name:              content.Name,
		InputDescription:  content.InputDescription,
		OutputDescription: content.OutputDescription,
		InvocationRules:   content.InvocationRules,
		PlanCreationSpec:  planCreation,
		AnalyzeMaterials:  analyzeMaterials,
		PlanStoryboard:    planStoryboard,
		GenerateMedia:     generateMedia,
		WritePrompts:      writePrompts,
		AssembleOutput:    assembleOutput,
	}
	if content.Examples != nil {
		result.Examples = make([]skill.SkillExampleV1, len(content.Examples))
		for index, example := range content.Examples {
			if example == nil {
				return skill.SkillRuntimeContentV1{}, fmt.Errorf("examples[%d] is nil", index)
			}
			result.Examples[index] = skill.SkillExampleV1{Input: example.Input, Output: example.Output}
		}
	}
	if content.StarterPrompts != nil {
		result.StarterPrompts = append([]string(nil), content.StarterPrompts...)
	}
	return result, nil
}

func mapGuidanceV2(value *sessionv1.CapabilityGuidanceV1) (skill.CapabilityGuidanceV1, error) {
	if value == nil {
		return skill.CapabilityGuidanceV1{}, fmt.Errorf("capability guidance is required")
	}
	var applicability skill.SkillGuidanceApplicabilityV1
	switch value.Applicability {
	case sessionv1.SkillGuidanceApplicabilityV1_ENABLED:
		applicability = skill.SkillGuidanceEnabledV1
	case sessionv1.SkillGuidanceApplicabilityV1_NOT_APPLICABLE:
		applicability = skill.SkillGuidanceNotApplicableV1
	default:
		return skill.CapabilityGuidanceV1{}, fmt.Errorf("unsupported guidance applicability")
	}
	return skill.CapabilityGuidanceV1{
		Applicability:       applicability,
		Guidance:            value.Guidance,
		NotApplicableReason: value.NotApplicableReason,
	}, nil
}

func mapSnapshotKindV2(value sessionv1.SessionSkillSnapshotKindV1) (skill.SessionSkillSnapshotKindV1, error) {
	switch value {
	case sessionv1.SessionSkillSnapshotKindV1_EMPTY:
		return skill.SessionSkillSnapshotKindEmptyV1, nil
	case sessionv1.SessionSkillSnapshotKindV1_PUBLISHED_REFS:
		return skill.SessionSkillSnapshotKindPublishedRefsV1, nil
	default:
		return "", fmt.Errorf("unsupported snapshot kind")
	}
}

func mapNamespaceV2(value sessionv1.SkillNamespaceV1) (skill.SkillNamespaceV1, error) {
	switch value {
	case sessionv1.SkillNamespaceV1_USER:
		return skill.SkillNamespaceUserV1, nil
	case sessionv1.SkillNamespaceV1_SYSTEM:
		return skill.SkillNamespaceSystemV1, nil
	default:
		return "", fmt.Errorf("unsupported Skill namespace")
	}
}

func mapQueryRequestV2(request *sessionv1.QueryProjectSessionCommandRequestV2) (session.QueryCommand, error) {
	if request == nil || request.SchemaVersion != sessionv1.QUERY_PROJECT_SESSION_COMMAND_SCHEMA_VERSION_V2 ||
		!isCanonicalUUIDv7V2(request.RequestId) || !isCanonicalUUIDv7V2(request.CommandId) ||
		!isLowerSHA256(request.ExpectedRequestDigest) {
		return session.QueryCommand{}, fmt.Errorf("invalid Query v2 request")
	}
	return session.QueryCommand{
		SchemaVersion:         request.SchemaVersion,
		RequestID:             request.RequestId,
		CommandID:             request.CommandId,
		ExpectedRequestDigest: request.ExpectedRequestDigest,
		ExpectedCommandType:   session.CommandTypeEnsureProjectSessionV2,
	}, nil
}

func mapEnsureResponseV2(command session.EnsureCommandV2, result session.EnsureResult, maxItems int) (*sessionv1.EnsureProjectSessionResponseV2, error) {
	if result.CommandID != command.CommandID || !isCanonicalUUIDv7V2(result.SessionID) ||
		result.ResultVersion != session.ResultVersionV2 || result.AcceptedAt.IsZero() ||
		result.SkillSnapshotDigest != command.SkillSnapshot.SnapshotSetDigest ||
		result.SkillCount != int(command.SkillSnapshot.SkillCount) ||
		(result.MessageID == nil) != (result.InputID == nil) ||
		(command.InitialPrompt != "") != (result.InputID != nil) {
		return nil, fmt.Errorf("invalid Ensure v2 result")
	}
	disposition, err := mapDispositionV2(result.Disposition)
	if err != nil {
		return nil, err
	}
	receipt, err := mapReceiptV2(result, maxItems)
	if err != nil {
		return nil, err
	}
	return &sessionv1.EnsureProjectSessionResponseV2{
		SchemaVersion: sessionv1.ENSURE_PROJECT_SESSION_SCHEMA_VERSION_V2,
		RequestId:     command.RequestID,
		Disposition:   disposition,
		Receipt:       receipt,
	}, nil
}

func mapQueryResponseV2(requestID, commandID string, result session.QueryCommandResult, maxItems int) (*sessionv1.QueryProjectSessionCommandResponseV2, error) {
	response := &sessionv1.QueryProjectSessionCommandResponseV2{
		SchemaVersion: sessionv1.QUERY_PROJECT_SESSION_COMMAND_SCHEMA_VERSION_V2,
		RequestId:     requestID,
	}
	switch result.Status {
	case session.QueryCommandStatusNotFound:
		if result.Receipt != nil {
			return nil, fmt.Errorf("not_found Query carried Receipt")
		}
		response.Status = sessionv1.QueryProjectSessionCommandStatusV1_NOT_FOUND
	case session.QueryCommandStatusConflict:
		if result.Receipt != nil {
			return nil, fmt.Errorf("conflict Query carried Receipt")
		}
		response.Status = sessionv1.QueryProjectSessionCommandStatusV1_CONFLICT
	case session.QueryCommandStatusCompleted:
		if result.Receipt == nil || result.Receipt.CommandID != commandID || result.Receipt.ResultVersion != session.ResultVersionV2 {
			return nil, fmt.Errorf("completed Query missing V2 Receipt")
		}
		receipt, err := mapReceiptV2(*result.Receipt, maxItems)
		if err != nil {
			return nil, err
		}
		response.Status = sessionv1.QueryProjectSessionCommandStatusV1_COMPLETED
		response.Receipt = receipt
	default:
		return nil, fmt.Errorf("unsupported Query status")
	}
	return response, nil
}

func mapReceiptV2(result session.EnsureResult, maxItems int) (*sessionv1.ProjectSessionReceiptV2, error) {
	if !isCanonicalUUIDv7V2(result.CommandID) || !isCanonicalUUIDv7V2(result.SessionID) ||
		!isLowerSHA256(result.SkillSnapshotDigest) || result.SkillCount < 0 || result.SkillCount > maxItems || result.AcceptedAt.IsZero() ||
		(result.MessageID == nil) != (result.InputID == nil) {
		return nil, fmt.Errorf("invalid V2 Receipt")
	}
	if (result.SkillCount == 0) != (result.SkillSnapshotDigest == skill.EmptySnapshotSetDigestHex) {
		return nil, fmt.Errorf("invalid V2 Receipt snapshot count/digest")
	}
	if result.MessageID != nil && (!isCanonicalUUIDv7V2(*result.MessageID) || !isCanonicalUUIDv7V2(*result.InputID)) {
		return nil, fmt.Errorf("invalid V2 Message/Input IDs")
	}
	return &sessionv1.ProjectSessionReceiptV2{
		CommandId:           result.CommandID,
		SessionId:           result.SessionID,
		MessageId:           cloneStringPointerV2(result.MessageID),
		InputId:             cloneStringPointerV2(result.InputID),
		ResultVersion:       int32(result.ResultVersion),
		CompletedAtUnixMs:   result.AcceptedAt.UTC().UnixMilli(),
		SkillSnapshotDigest: result.SkillSnapshotDigest,
		SkillCount:          int32(result.SkillCount),
	}, nil
}

func mapDispositionV2(value session.EnsureDisposition) (sessionv1.EnsureDispositionV1, error) {
	switch value {
	case session.EnsureDispositionCreated:
		return sessionv1.EnsureDispositionV1_CREATED, nil
	case session.EnsureDispositionReplayed:
		return sessionv1.EnsureDispositionV1_REPLAYED, nil
	default:
		return 0, fmt.Errorf("unsupported Ensure disposition")
	}
}

func mapV2TransportError(err error, requestID string) error {
	switch {
	case errors.Is(err, skill.ErrLimitExceeded):
		return newServiceError(errorCodeSnapshotLimitExceededV2, "Session Skill Snapshot 超过接收上限", false, requestID)
	case errors.Is(err, skill.ErrDigestMismatch):
		return newServiceError(errorCodeSnapshotDigestMismatchV2, "Session Skill Snapshot 摘要不匹配", false, requestID)
	default:
		return newServiceError(errorCodeInvalidArgument, "Session 创建请求不符合 v2 契约", false, requestID)
	}
}

func mapServiceErrorV2(err error, requestID string) error {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return newServiceError(errorCodeDeadlineExceeded, "Session 请求处理超时", true, requestID)
	case errors.Is(err, context.Canceled):
		return newServiceError(errorCodeRequestCanceled, "Session 请求已取消", false, requestID)
	case errors.Is(err, session.ErrCommandVersionConflict):
		return newServiceError(errorCodeCommandVersionConflictV2, "Session 命令协议版本冲突", false, requestID)
	case errors.Is(err, session.ErrCommandConflict):
		return newServiceError(errorCodeCommandConflictV2, "Session 命令幂等语义冲突", false, requestID)
	case errors.Is(err, session.ErrProjectSessionConflict):
		return newServiceError(errorCodeProjectSessionConflict, "Project 已绑定其他 Session 命令", false, requestID)
	case errors.Is(err, session.ErrSnapshotLimitExceeded), errors.Is(err, skill.ErrLimitExceeded):
		return newServiceError(errorCodeSnapshotLimitExceededV2, "Session Skill Snapshot 超过接收上限", false, requestID)
	case errors.Is(err, session.ErrSnapshotIntegrity), errors.Is(err, skill.ErrDigestMismatch):
		return newServiceError(errorCodeSnapshotDigestMismatchV2, "Session Skill Snapshot 摘要不匹配", false, requestID)
	case errors.Is(err, session.ErrContentProtection), errors.Is(err, session.ErrContentUnavailable):
		return newServiceError(errorCodeContentProtectionUnavailableV2, "Session 内容保护暂时不可用", true, requestID)
	case errors.Is(err, session.ErrPersistence):
		return newServiceError(errorCodePersistenceUnavailable, "Session 持久化暂时不可用", true, requestID)
	case errors.Is(err, session.ErrInvalidCommand):
		return newServiceError(errorCodeInvalidArgument, "Session 请求不符合 v2 契约", false, requestID)
	default:
		return newServiceError(errorCodeInternal, "Session 服务内部错误", false, requestID)
	}
}

func safeRequestIDFromEnsureV2(request *sessionv1.EnsureProjectSessionRequestV2) string {
	if request != nil && isCanonicalUUIDv7V2(request.RequestId) {
		return request.RequestId
	}
	return ""
}

func safeRequestIDFromQueryV2(request *sessionv1.QueryProjectSessionCommandRequestV2) string {
	if request != nil && isCanonicalUUIDv7V2(request.RequestId) {
		return request.RequestId
	}
	return ""
}

func isCanonicalUUIDv7V2(value string) bool {
	id, err := uuid.Parse(value)
	return err == nil && id.Version() == 7 && id.String() == value
}

func equalDigestV2(left, right string) bool {
	return len(left) == len(right) && subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}

func cloneStringPointerV2(value *string) *string {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}
