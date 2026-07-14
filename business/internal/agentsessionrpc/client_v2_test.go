package agentsessionrpc

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/business/internal/project"
	"github.com/FigoGoo/Dora-Agent/business/internal/projectdispatchv2"
	"github.com/FigoGoo/Dora-Agent/business/internal/projectskillbinding"
	"github.com/FigoGoo/Dora-Agent/business/kitex_gen/sessionv1"
	"github.com/cloudwego/kitex/client/callopt"
	"github.com/google/uuid"
)

type fakeProtocolV2 struct {
	*fakeProtocol
	ensureV2Request *sessionv1.EnsureProjectSessionRequestV2
	ensureV2Result  *sessionv1.EnsureProjectSessionResponseV2
	ensureV2Err     error
	queryV2Request  *sessionv1.QueryProjectSessionCommandRequestV2
	queryV2Result   *sessionv1.QueryProjectSessionCommandResponseV2
	queryV2Err      error
}

func (protocol *fakeProtocolV2) EnsureProjectSessionV2(_ context.Context, request *sessionv1.EnsureProjectSessionRequestV2, _ ...callopt.Option) (*sessionv1.EnsureProjectSessionResponseV2, error) {
	protocol.ensureV2Request = request
	return protocol.ensureV2Result, protocol.ensureV2Err
}
func (protocol *fakeProtocolV2) QueryProjectSessionCommandV2(_ context.Context, request *sessionv1.QueryProjectSessionCommandRequestV2, _ ...callopt.Option) (*sessionv1.QueryProjectSessionCommandResponseV2, error) {
	protocol.queryV2Request = request
	return protocol.queryV2Result, protocol.queryV2Err
}

func TestClientEnsureV2MapsExplicitEmptyWithoutV1Fallback(t *testing.T) {
	payload := emptyPayloadV2(t)
	sessionID, _ := uuid.NewV7()
	protocol := &fakeProtocolV2{fakeProtocol: &fakeProtocol{}}
	protocol.ensureV2Result = &sessionv1.EnsureProjectSessionResponseV2{
		SchemaVersion: sessionv1.ENSURE_PROJECT_SESSION_SCHEMA_VERSION_V2,
		RequestId:     testRPCV2RequestID,
		Disposition:   sessionv1.EnsureDispositionV1_CREATED,
		Receipt: &sessionv1.ProjectSessionReceiptV2{
			CommandId: payload.CommandID, SessionId: sessionID.String(), ResultVersion: 2,
			CompletedAtUnixMs: time.Now().UnixMilli(), SkillSnapshotDigest: payload.SkillSnapshot.SnapshotSetDigest,
			SkillCount: 0,
		},
	}
	client := &Client{protocol: protocol, config: ClientConfig{RequestTimeout: time.Second}, auth: testAuthenticator(t)}
	receipt, err := client.EnsureV2(context.Background(), testRPCV2RequestID, payload)
	if err != nil {
		t.Fatalf("Ensure v2 失败: %v", err)
	}
	if protocol.ensureV2Request == nil || protocol.ensureV2Request.SkillSnapshot == nil ||
		protocol.ensureV2Request.SkillSnapshot.Skills == nil || len(protocol.ensureV2Request.SkillSnapshot.Skills) != 0 ||
		protocol.ensureV2Request.RequestDigest != payload.RequestDigest || receipt.SkillCount != 0 ||
		protocol.ensureRequest != nil {
		t.Fatalf("Ensure v2 映射或降级漂移: request=%+v receipt=%+v v1=%+v", protocol.ensureV2Request, receipt, protocol.ensureRequest)
	}
}

func TestClientQueryV2MapsThreeStatesAndRejectsMalformedReceipt(t *testing.T) {
	payload := emptyPayloadV2(t)
	queryID, _ := uuid.NewV7()
	sessionID, _ := uuid.NewV7()
	protocol := &fakeProtocolV2{fakeProtocol: &fakeProtocol{}}
	client := &Client{
		protocol: protocol, config: ClientConfig{RequestTimeout: time.Second},
		idgen: fixedIDGenerator{value: queryID.String()}, auth: testAuthenticator(t),
	}
	digest, _ := projectskillbinding.DigestFromHex(payload.RequestDigest)
	protocol.queryV2Result = &sessionv1.QueryProjectSessionCommandResponseV2{
		SchemaVersion: sessionv1.QUERY_PROJECT_SESSION_COMMAND_SCHEMA_VERSION_V2,
		RequestId:     queryID.String(), Status: sessionv1.QueryProjectSessionCommandStatusV1_NOT_FOUND,
	}
	result, err := client.QueryV2(context.Background(), payload.CommandID, digest)
	if err != nil || result.Status != project.QueryStatusNotFound || protocol.queryRequest != nil {
		t.Fatalf("Query v2 not_found 漂移: result=%+v err=%v", result, err)
	}
	protocol.queryV2Result.Status = sessionv1.QueryProjectSessionCommandStatusV1_CONFLICT
	result, err = client.QueryV2(context.Background(), payload.CommandID, digest)
	if err != nil || result.Status != project.QueryStatusConflict {
		t.Fatalf("Query v2 conflict 漂移: result=%+v err=%v", result, err)
	}
	protocol.queryV2Result.Status = sessionv1.QueryProjectSessionCommandStatusV1_COMPLETED
	protocol.queryV2Result.Receipt = &sessionv1.ProjectSessionReceiptV2{
		CommandId: payload.CommandID, SessionId: sessionID.String(), ResultVersion: 2,
		CompletedAtUnixMs: time.Now().UnixMilli(), SkillSnapshotDigest: payload.SkillSnapshot.SnapshotSetDigest,
		SkillCount: 0,
	}
	result, err = client.QueryV2(context.Background(), payload.CommandID, digest)
	if err != nil || result.Status != project.QueryStatusCompleted || result.Receipt == nil {
		t.Fatalf("Query v2 completed 漂移: result=%+v err=%v", result, err)
	}
	protocol.queryV2Result.Receipt.SkillCount = 33
	if _, err := client.QueryV2(context.Background(), payload.CommandID, digest); !errors.Is(err, project.ErrInvalidAgentReceipt) {
		t.Fatalf("畸形 v2 Receipt 未失败关闭: %v", err)
	}
}

func TestClientV2UnavailableDoesNotCallV1(t *testing.T) {
	payload := emptyPayloadV2(t)
	protocol := &fakeProtocol{}
	client := &Client{protocol: protocol, config: ClientConfig{RequestTimeout: time.Second}, auth: testAuthenticator(t)}
	if _, err := client.EnsureV2(context.Background(), testRPCV2RequestID, payload); !errors.Is(err, project.ErrAgentSessionUnavailable) {
		t.Fatalf("缺少 v2 capability 未返回 unavailable: %v", err)
	}
	if protocol.ensureRequest != nil || protocol.queryRequest != nil {
		t.Fatalf("v2 capability 缺失时调用了 v1: ensure=%+v query=%+v", protocol.ensureRequest, protocol.queryRequest)
	}
}

const testRPCV2RequestID = "019f0000-0000-7000-8000-000000000090"

func emptyPayloadV2(t *testing.T) projectskillbinding.SessionBootstrapOutboxPayloadV2 {
	t.Helper()
	projectID, _ := uuid.NewV7()
	ownerID, _ := uuid.NewV7()
	commandID, _ := uuid.NewV7()
	skills := make([]projectskillbinding.PublishedSkillSnapshotRefV1, 0)
	_, snapshotDigest, err := projectskillbinding.CanonicalSnapshotSetV1(skills)
	if err != nil {
		t.Fatalf("empty Snapshot canonical 失败: %v", err)
	}
	snapshot := projectskillbinding.SessionSkillSnapshotV1{
		SchemaVersion: projectskillbinding.SessionSnapshotSchemaVersionV1,
		SnapshotKind:  projectskillbinding.SnapshotKindEmpty, SkillCount: 0,
		SnapshotSetDigest: snapshotDigest.Hex(), Skills: skills,
	}
	_, requestDigest, err := projectskillbinding.CanonicalEnsureRequestV2(
		projectID.String(), ownerID.String(), false, projectskillbinding.Digest{}, snapshot,
	)
	if err != nil {
		t.Fatalf("Ensure v2 canonical 失败: %v", err)
	}
	return projectskillbinding.SessionBootstrapOutboxPayloadV2{
		SchemaVersion: projectskillbinding.OutboxPayloadSchemaVersionV2,
		CommandID:     commandID.String(), ProjectID: projectID.String(), OwnerUserID: ownerID.String(),
		CreationSource: project.QuickCreateCreationSource, SkillSnapshot: snapshot,
		AcceptedAtUnixMS: time.Now().UnixMilli(), RequestDigest: requestDigest.Hex(),
	}
}

var _ projectdispatchv2.AgentClient = (*Client)(nil)
