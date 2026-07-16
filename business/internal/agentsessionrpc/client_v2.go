package agentsessionrpc

import (
	"context"
	"crypto/subtle"

	"github.com/FigoGoo/Dora-Agent/business/internal/project"
	"github.com/FigoGoo/Dora-Agent/business/internal/projectdispatchv2"
	"github.com/FigoGoo/Dora-Agent/business/internal/projectskillbinding"
	"github.com/FigoGoo/Dora-Agent/business/kitex_gen/sessionv1"
	"github.com/cloudwego/kitex/client/callopt"
)

// protocolClientV2 隔离追加的 Agent-owned V2 生成方法，保留现有 V1 测试替身兼容。
type protocolClientV2 interface {
	EnsureProjectSessionV2(ctx context.Context, request *sessionv1.EnsureProjectSessionRequestV2, callOptions ...callopt.Option) (*sessionv1.EnsureProjectSessionResponseV2, error)
	QueryProjectSessionCommandV2(ctx context.Context, request *sessionv1.QueryProjectSessionCommandRequestV2, callOptions ...callopt.Option) (*sessionv1.QueryProjectSessionCommandResponseV2, error)
}

// EnsureV2 逐字段映射已经由 Dispatcher 严格解密复核的 payload，并且绝不调用 V1。
func (client *Client) EnsureV2(ctx context.Context, requestID string, payload projectskillbinding.SessionBootstrapOutboxPayloadV2) (projectdispatchv2.Receipt, error) {
	protocol, ok := client.protocol.(protocolClientV2)
	if !ok {
		return projectdispatchv2.Receipt{}, project.ErrAgentSessionUnavailable
	}
	request, err := mapEnsureRequestV2(requestID, payload)
	if err != nil {
		return projectdispatchv2.Receipt{}, project.ErrAgentSessionInvalid
	}
	requestCtx, cancel := context.WithTimeout(ctx, client.config.RequestTimeout)
	defer cancel()
	requestCtx = client.auth.withAuthentication(
		requestCtx, "EnsureProjectSessionV2", request.RequestId, request.CommandId, request.RequestDigest,
	)
	response, err := protocol.EnsureProjectSessionV2(requestCtx, request)
	if err != nil {
		return projectdispatchv2.Receipt{}, mapAgentServiceError(err)
	}
	return mapEnsureResponseV2(request, response)
}

// QueryV2 核对原 v2 command_id/digest；unknown method 或旧 Agent 只表现为 unavailable，不降级 Query v1。
func (client *Client) QueryV2(ctx context.Context, commandID string, expectedDigest projectskillbinding.Digest) (projectdispatchv2.QueryResult, error) {
	protocol, ok := client.protocol.(protocolClientV2)
	if !ok {
		return projectdispatchv2.QueryResult{}, project.ErrAgentSessionUnavailable
	}
	requestID, err := client.idgen.New()
	if err != nil || !isUUIDv7(requestID) || !isUUIDv7(commandID) || expectedDigest == (projectskillbinding.Digest{}) {
		return projectdispatchv2.QueryResult{}, project.ErrAgentSessionInvalid
	}
	request := &sessionv1.QueryProjectSessionCommandRequestV2{
		SchemaVersion: sessionv1.QUERY_PROJECT_SESSION_COMMAND_SCHEMA_VERSION_V2,
		RequestId:     requestID, CommandId: commandID, ExpectedRequestDigest: expectedDigest.Hex(),
	}
	requestCtx, cancel := context.WithTimeout(ctx, client.config.RequestTimeout)
	defer cancel()
	requestCtx = client.auth.withAuthentication(
		requestCtx, "QueryProjectSessionCommandV2", request.RequestId, request.CommandId, request.ExpectedRequestDigest,
	)
	response, err := protocol.QueryProjectSessionCommandV2(requestCtx, request)
	if err != nil {
		return projectdispatchv2.QueryResult{}, mapAgentServiceError(err)
	}
	return mapQueryResponseV2(request, response)
}

func mapEnsureRequestV2(requestID string, payload projectskillbinding.SessionBootstrapOutboxPayloadV2) (*sessionv1.EnsureProjectSessionRequestV2, error) {
	if !isUUIDv7(requestID) || payload.SchemaVersion != projectskillbinding.OutboxPayloadSchemaVersionV2 ||
		!isUUIDv7(payload.CommandID) || payload.AcceptedAtUnixMS <= 0 {
		return nil, project.ErrAgentSessionInvalid
	}
	promptDigest, err := projectskillbinding.DigestFromHex(payload.PromptDigest)
	if !payload.PromptPresent {
		promptDigest = projectskillbinding.Digest{}
		err = nil
	}
	if err != nil {
		return nil, project.ErrAgentSessionInvalid
	}
	_, requestDigest, err := projectskillbinding.CanonicalEnsureRequestV2(
		payload.ProjectID, payload.OwnerUserID, payload.PromptPresent, promptDigest, payload.SkillSnapshot,
	)
	if err != nil || !equalDigestV2(payload.RequestDigest, requestDigest.Hex()) {
		return nil, project.ErrAgentSessionInvalid
	}
	snapshot, err := mapSkillSnapshotV2(payload.SkillSnapshot)
	if err != nil {
		return nil, err
	}
	var prompt *string
	if payload.PromptPresent {
		value := payload.InitialPrompt
		prompt = &value
	}
	return &sessionv1.EnsureProjectSessionRequestV2{
		SchemaVersion: sessionv1.ENSURE_PROJECT_SESSION_SCHEMA_VERSION_V2,
		RequestId:     requestID, CommandId: payload.CommandID, RequestDigest: requestDigest.Hex(),
		ProjectId: payload.ProjectID, OwnerUserId: payload.OwnerUserID,
		CreationSource: sessionv1.CreationSourceV1_QUICK_CREATE,
		InitialPrompt:  prompt, PromptDigest: payload.PromptDigest, SkillSnapshot: snapshot,
		RequestedAtUnixMs: payload.AcceptedAtUnixMS,
	}, nil
}

func mapSkillSnapshotV2(snapshot projectskillbinding.SessionSkillSnapshotV1) (*sessionv1.SessionSkillSnapshotV1, error) {
	var kind sessionv1.SessionSkillSnapshotKindV1
	switch snapshot.SnapshotKind {
	case projectskillbinding.SnapshotKindEmpty:
		kind = sessionv1.SessionSkillSnapshotKindV1_EMPTY
	case projectskillbinding.SnapshotKindPublishedRefs:
		kind = sessionv1.SessionSkillSnapshotKindV1_PUBLISHED_REFS
	default:
		return nil, project.ErrAgentSessionInvalid
	}
	result := &sessionv1.SessionSkillSnapshotV1{
		SchemaVersion: snapshot.SchemaVersion, SnapshotKind: kind,
		SkillCount: int32(snapshot.SkillCount), SnapshotSetDigest: snapshot.SnapshotSetDigest,
		Skills: make([]*sessionv1.PublishedSkillSnapshotRefV1, len(snapshot.Skills)),
	}
	for index, item := range snapshot.Skills {
		mapped, err := mapPublishedSkillV2(item)
		if err != nil {
			return nil, err
		}
		result.Skills[index] = mapped
	}
	return result, nil
}

func mapPublishedSkillV2(item projectskillbinding.PublishedSkillSnapshotRefV1) (*sessionv1.PublishedSkillSnapshotRefV1, error) {
	if item.Namespace != projectskillbinding.SkillNamespaceUser {
		return nil, project.ErrAgentSessionInvalid
	}
	runtimeContent, err := mapRuntimeContentV2(item.RuntimeContent)
	if err != nil {
		return nil, err
	}
	result := &sessionv1.PublishedSkillSnapshotRefV1{
		LoadOrder: int32(item.LoadOrder), Priority: int32(item.Priority), Namespace: sessionv1.SkillNamespaceV1_USER,
		SkillId: item.SkillID, PublisherUserId: item.PublisherUserID, PublishedSnapshotId: item.PublishedSnapshotID,
		PublicationRevision: item.PublicationRevision, DefinitionSchemaVersion: item.DefinitionSchemaVersion,
		ContentDigest: item.ContentDigest, RuntimeContentSchemaVersion: item.RuntimeContentSchemaVersion,
		RuntimeContentDigest: item.RuntimeContentDigest, RuntimeContent: runtimeContent,
		AllowedGraphToolKeys:     append([]string{}, item.AllowedGraphToolKeys...),
		PublicToolRefs:           make([]*sessionv1.PublicToolSnapshotRefV1, len(item.PublicToolRefs)),
		PermissionSnapshotDigest: item.PermissionSnapshotDigest, RuntimePolicyRef: item.RuntimePolicyRef,
		GovernanceEpoch: item.GovernanceEpoch, PublishedAtUnixMs: item.PublishedAtUnixMS,
	}
	for index, ref := range item.PublicToolRefs {
		result.PublicToolRefs[index] = &sessionv1.PublicToolSnapshotRefV1{RefId: ref.RefID, RefDigest: ref.RefDigest}
	}
	return result, nil
}

func mapRuntimeContentV2(content projectskillbinding.SkillRuntimeContentV1) (*sessionv1.SkillRuntimeContentV1, error) {
	guidance := func(value projectskillbinding.CapabilityGuidanceV1) (*sessionv1.CapabilityGuidanceV1, error) {
		var applicability sessionv1.SkillGuidanceApplicabilityV1
		switch value.Applicability {
		case "enabled":
			applicability = sessionv1.SkillGuidanceApplicabilityV1_ENABLED
		case "not_applicable":
			applicability = sessionv1.SkillGuidanceApplicabilityV1_NOT_APPLICABLE
		default:
			return nil, project.ErrAgentSessionInvalid
		}
		return &sessionv1.CapabilityGuidanceV1{
			Applicability: applicability, Guidance: value.Guidance, NotApplicableReason: value.NotApplicableReason,
		}, nil
	}
	values := []projectskillbinding.CapabilityGuidanceV1{
		content.PlanCreationSpec, content.AnalyzeMaterials, content.PlanStoryboard,
		content.GenerateMedia, content.WritePrompts, content.AssembleOutput,
	}
	mapped := make([]*sessionv1.CapabilityGuidanceV1, len(values))
	for index, value := range values {
		item, err := guidance(value)
		if err != nil {
			return nil, err
		}
		mapped[index] = item
	}
	result := &sessionv1.SkillRuntimeContentV1{
		SchemaVersion: content.SchemaVersion, Name: content.Name,
		InputDescription: content.InputDescription, OutputDescription: content.OutputDescription,
		InvocationRules:  content.InvocationRules,
		PlanCreationSpec: mapped[0], AnalyzeMaterials: mapped[1], PlanStoryboard: mapped[2],
		GenerateMedia: mapped[3], WritePrompts: mapped[4], AssembleOutput: mapped[5],
		Examples:       make([]*sessionv1.SkillExampleV1, len(content.Examples)),
		StarterPrompts: append([]string{}, content.StarterPrompts...),
	}
	for index, example := range content.Examples {
		result.Examples[index] = &sessionv1.SkillExampleV1{Input: example.Input, Output: example.Output}
	}
	return result, nil
}

func mapEnsureResponseV2(request *sessionv1.EnsureProjectSessionRequestV2, response *sessionv1.EnsureProjectSessionResponseV2) (projectdispatchv2.Receipt, error) {
	if response == nil || response.SchemaVersion != sessionv1.ENSURE_PROJECT_SESSION_SCHEMA_VERSION_V2 ||
		response.RequestId != request.RequestId || (response.Disposition != sessionv1.EnsureDispositionV1_CREATED &&
		response.Disposition != sessionv1.EnsureDispositionV1_REPLAYED) {
		return projectdispatchv2.Receipt{}, project.ErrInvalidAgentReceipt
	}
	receipt, err := mapReceiptV2(request.CommandId, request.SkillSnapshot.SnapshotSetDigest, request.SkillSnapshot.SkillCount, request.InitialPrompt != nil, response.Receipt)
	if err != nil {
		return projectdispatchv2.Receipt{}, err
	}
	receipt.Replayed = response.Disposition == sessionv1.EnsureDispositionV1_REPLAYED
	return receipt, nil
}

func mapQueryResponseV2(request *sessionv1.QueryProjectSessionCommandRequestV2, response *sessionv1.QueryProjectSessionCommandResponseV2) (projectdispatchv2.QueryResult, error) {
	if response == nil || response.SchemaVersion != sessionv1.QUERY_PROJECT_SESSION_COMMAND_SCHEMA_VERSION_V2 || response.RequestId != request.RequestId {
		return projectdispatchv2.QueryResult{}, project.ErrInvalidAgentReceipt
	}
	switch response.Status {
	case sessionv1.QueryProjectSessionCommandStatusV1_NOT_FOUND:
		if response.Receipt != nil {
			return projectdispatchv2.QueryResult{}, project.ErrInvalidAgentReceipt
		}
		return projectdispatchv2.QueryResult{Status: project.QueryStatusNotFound}, nil
	case sessionv1.QueryProjectSessionCommandStatusV1_CONFLICT:
		if response.Receipt != nil {
			return projectdispatchv2.QueryResult{}, project.ErrInvalidAgentReceipt
		}
		return projectdispatchv2.QueryResult{Status: project.QueryStatusConflict}, nil
	case sessionv1.QueryProjectSessionCommandStatusV1_COMPLETED:
		receipt, err := mapReceiptV2(request.CommandId, "", -1, false, response.Receipt)
		if err != nil {
			return projectdispatchv2.QueryResult{}, err
		}
		return projectdispatchv2.QueryResult{Status: project.QueryStatusCompleted, Receipt: &receipt}, nil
	default:
		return projectdispatchv2.QueryResult{}, project.ErrInvalidAgentReceipt
	}
}

func mapReceiptV2(commandID, expectedSnapshotDigest string, expectedSkillCount int32, expectedPrompt bool, receipt *sessionv1.ProjectSessionReceiptV2) (projectdispatchv2.Receipt, error) {
	if receipt == nil || receipt.CommandId != commandID || !isUUIDv7(receipt.CommandId) || !isUUIDv7(receipt.SessionId) ||
		receipt.ResultVersion != 2 || receipt.CompletedAtUnixMs <= 0 || receipt.SkillCount < 0 || receipt.SkillCount > 32 ||
		(receipt.MessageId == nil) != (receipt.InputId == nil) {
		return projectdispatchv2.Receipt{}, project.ErrInvalidAgentReceipt
	}
	digest, err := projectskillbinding.DigestFromHex(receipt.SkillSnapshotDigest)
	if err != nil || (receipt.SkillCount == 0) != (receipt.SkillSnapshotDigest == project.SHA256Digest([]byte("[]")).Hex()) {
		return projectdispatchv2.Receipt{}, project.ErrInvalidAgentReceipt
	}
	if expectedSnapshotDigest != "" && (!equalDigestV2(receipt.SkillSnapshotDigest, expectedSnapshotDigest) || receipt.SkillCount != expectedSkillCount) {
		return projectdispatchv2.Receipt{}, project.ErrInvalidAgentReceipt
	}
	if expectedSnapshotDigest != "" && expectedPrompt != (receipt.InputId != nil) {
		return projectdispatchv2.Receipt{}, project.ErrInvalidAgentReceipt
	}
	if receipt.InputId != nil && (!isUUIDv7(*receipt.InputId) || !isUUIDv7(*receipt.MessageId)) {
		return projectdispatchv2.Receipt{}, project.ErrInvalidAgentReceipt
	}
	return projectdispatchv2.Receipt{
		CommandID: receipt.CommandId, SessionID: receipt.SessionId, InputID: cloneOptionalString(receipt.InputId),
		SkillSnapshotDigest: digest, SkillCount: receipt.SkillCount,
	}, nil
}

func equalDigestV2(left, right string) bool {
	return len(left) == len(right) && subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}

var _ projectdispatchv2.AgentClient = (*Client)(nil)
