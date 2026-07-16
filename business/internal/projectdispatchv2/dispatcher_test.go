package projectdispatchv2

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/business/internal/project"
	"github.com/FigoGoo/Dora-Agent/business/internal/projectskillbinding"
	"github.com/FigoGoo/Dora-Agent/business/internal/promptcrypto"
)

const (
	testCommandID    = "019f0000-0000-7000-8000-000000000001"
	testProjectID    = "019f0000-0000-7000-8000-000000000002"
	testOwnerID      = "019f0000-0000-7000-8000-000000000003"
	testResolutionID = "019f0000-0000-7000-8000-000000000004"
	testRequestID    = "019f0000-0000-7000-8000-000000000005"
	testSessionID    = "019f0000-0000-7000-8000-000000000006"
)

type repositoryStub struct {
	delivered *project.EnsureSessionReceipt
	retried   bool
	deadCode  string
}

func (repository *repositoryStub) ClaimNext(context.Context, string, time.Time, time.Time) (project.SessionOutbox, error) {
	return project.SessionOutbox{}, errors.New("not used")
}
func (repository *repositoryStub) MarkDelivered(_ context.Context, _ project.SessionOutbox, receipt project.EnsureSessionReceipt, _ time.Time) error {
	repository.delivered = &receipt
	return nil
}
func (repository *repositoryStub) MarkRetry(_ context.Context, _ project.SessionOutbox, _, _ time.Time) error {
	repository.retried = true
	return nil
}
func (repository *repositoryStub) MarkDead(_ context.Context, _ project.SessionOutbox, code string, _ time.Time) error {
	repository.deadCode = code
	return nil
}

type clientStub struct {
	ensurePayload projectskillbinding.SessionBootstrapOutboxPayloadV2
	ensureCalls   int
	ensureReceipt Receipt
	ensureErr     error
	queryCalls    int
	queryResult   QueryResult
	queryErr      error
}

func (client *clientStub) EnsureV2(_ context.Context, _ string, payload projectskillbinding.SessionBootstrapOutboxPayloadV2) (Receipt, error) {
	client.ensureCalls++
	client.ensurePayload = payload
	return client.ensureReceipt, client.ensureErr
}
func (client *clientStub) QueryV2(_ context.Context, _ string, _ projectskillbinding.Digest) (QueryResult, error) {
	client.queryCalls++
	return client.queryResult, client.queryErr
}

type idStub struct{ value string }

func (generator idStub) New() (string, error) { return generator.value, nil }

func TestDispatcherDeliversStrictlyOpenedV2Payload(t *testing.T) {
	outbox, payload, protector := encryptedOutboxFixture(t)
	repository := &repositoryStub{}
	client := &clientStub{ensureReceipt: validReceiptForOutbox(outbox)}
	dispatcher, err := New(repository, client, protector, idStub{value: testRequestID}, Config{
		RetryDelay: time.Second, Limits: projectskillbinding.DefaultLimitsV1(),
	})
	if err != nil {
		t.Fatalf("创建 v2 Dispatcher 失败: %v", err)
	}
	if err := dispatcher.DispatchClaimedV2(context.Background(), outbox, time.Now().UTC()); err != nil {
		t.Fatalf("派发 v2 失败: %v", err)
	}
	if client.ensureCalls != 1 || client.queryCalls != 0 || repository.delivered == nil || repository.retried || repository.deadCode != "" ||
		client.ensurePayload.RequestDigest != payload.RequestDigest || client.ensurePayload.SkillSnapshot.SnapshotSetDigest != payload.SkillSnapshot.SnapshotSetDigest {
		t.Fatalf("v2 派发状态漂移: client=%+v repository=%+v", client, repository)
	}
	if repository.delivered.SkillSnapshotDigest != outbox.SkillSnapshotDigest || repository.delivered.SkillCount != 0 {
		t.Fatalf("交付 Receipt 未冻结 Snapshot metadata: %+v", repository.delivered)
	}
}

func TestDispatcherRecoveryQueriesBeforeRetryingSamePayload(t *testing.T) {
	outbox, payload, protector := encryptedOutboxFixture(t)
	outbox.RecoveryRequired = true
	repository := &repositoryStub{}
	client := &clientStub{queryResult: QueryResult{Status: project.QueryStatusNotFound}, ensureReceipt: validReceiptForOutbox(outbox)}
	dispatcher, _ := New(repository, client, protector, idStub{value: testRequestID}, Config{
		RetryDelay: time.Second, Limits: projectskillbinding.DefaultLimitsV1(),
	})
	if err := dispatcher.DispatchClaimedV2(context.Background(), outbox, time.Now().UTC()); err != nil {
		t.Fatalf("恢复 v2 失败: %v", err)
	}
	if client.queryCalls != 1 || client.ensureCalls != 1 || repository.delivered == nil ||
		client.ensurePayload.RequestDigest != payload.RequestDigest {
		t.Fatalf("恢复顺序或原负载重放漂移: client=%+v repository=%+v", client, repository)
	}
}

func TestDispatcherKeepsCiphertextWhenQueryUnavailable(t *testing.T) {
	outbox, _, protector := encryptedOutboxFixture(t)
	outbox.RecoveryRequired = true
	repository := &repositoryStub{}
	client := &clientStub{queryErr: project.ErrAgentSessionUnavailable}
	dispatcher, _ := New(repository, client, protector, idStub{value: testRequestID}, Config{
		RetryDelay: time.Second, Limits: projectskillbinding.DefaultLimitsV1(),
	})
	if err := dispatcher.DispatchClaimedV2(context.Background(), outbox, time.Now().UTC()); err != nil {
		t.Fatalf("Query unavailable 释放重试失败: %v", err)
	}
	if !repository.retried || repository.delivered != nil || repository.deadCode != "" || client.ensureCalls != 0 ||
		outbox.EncryptedPayload == nil || len(outbox.EncryptedPayload.Ciphertext) == 0 {
		t.Fatalf("Query unavailable 未保留同一密文: repository=%+v outbox=%+v", repository, outbox)
	}
}

func TestDispatcherRejectsTamperedPayloadAndReceipt(t *testing.T) {
	t.Run("ciphertext", func(t *testing.T) {
		outbox, _, protector := encryptedOutboxFixture(t)
		outbox.EncryptedPayload.Ciphertext[0] ^= 0xff
		repository := &repositoryStub{}
		client := &clientStub{}
		dispatcher, _ := New(repository, client, protector, idStub{value: testRequestID}, Config{
			RetryDelay: time.Second, Limits: projectskillbinding.DefaultLimitsV1(),
		})
		if err := dispatcher.DispatchClaimedV2(context.Background(), outbox, time.Now().UTC()); err != nil {
			t.Fatalf("损坏密文收敛失败: %v", err)
		}
		if repository.deadCode != dispatchErrorV2Payload || client.ensureCalls != 0 {
			t.Fatalf("损坏密文未在 RPC 前失败关闭: code=%s calls=%d", repository.deadCode, client.ensureCalls)
		}
	})
	t.Run("snapshot receipt", func(t *testing.T) {
		outbox, _, protector := encryptedOutboxFixture(t)
		repository := &repositoryStub{}
		bad := validReceiptForOutbox(outbox)
		bad.SkillCount = 1
		client := &clientStub{ensureReceipt: bad}
		dispatcher, _ := New(repository, client, protector, idStub{value: testRequestID}, Config{
			RetryDelay: time.Second, Limits: projectskillbinding.DefaultLimitsV1(),
		})
		if err := dispatcher.DispatchClaimedV2(context.Background(), outbox, time.Now().UTC()); err != nil {
			t.Fatalf("畸形 Receipt 收敛失败: %v", err)
		}
		if repository.deadCode != dispatchErrorV2Receipt || repository.delivered != nil {
			t.Fatalf("畸形 Receipt 被交付: repository=%+v", repository)
		}
	})
}

func encryptedOutboxFixture(t *testing.T) (project.SessionOutbox, projectskillbinding.SessionBootstrapOutboxPayloadV2, *promptcrypto.BootstrapV2AESGCMProtector) {
	t.Helper()
	emptySkills := make([]projectskillbinding.PublishedSkillSnapshotRefV1, 0)
	_, snapshotDigest, err := projectskillbinding.CanonicalSnapshotSetV1(emptySkills)
	if err != nil {
		t.Fatalf("计算 empty Snapshot 失败: %v", err)
	}
	snapshot := projectskillbinding.SessionSkillSnapshotV1{
		SchemaVersion: projectskillbinding.SessionSnapshotSchemaVersionV1,
		SnapshotKind:  projectskillbinding.SnapshotKindEmpty, SkillCount: 0,
		SnapshotSetDigest: snapshotDigest.Hex(), Skills: emptySkills,
	}
	_, requestDigest, err := projectskillbinding.CanonicalEnsureRequestV2(testProjectID, testOwnerID, false, projectskillbinding.Digest{}, snapshot)
	if err != nil {
		t.Fatalf("计算 Ensure v2 摘要失败: %v", err)
	}
	payload := projectskillbinding.SessionBootstrapOutboxPayloadV2{
		SchemaVersion: projectskillbinding.OutboxPayloadSchemaVersionV2,
		CommandID:     testCommandID, ProjectID: testProjectID, OwnerUserID: testOwnerID,
		CreationSource: project.QuickCreateCreationSource, PromptPresent: false,
		InitialPrompt: "", PromptDigest: "", SkillSnapshot: snapshot,
		AcceptedAtUnixMS: time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC).UnixMilli(), RequestDigest: requestDigest.Hex(),
	}
	plaintext, payloadDigest, err := projectskillbinding.CanonicalOutboxPayloadV2(payload)
	if err != nil {
		t.Fatalf("编码 Outbox v2 失败: %v", err)
	}
	aad, err := projectskillbinding.CanonicalOutboxAADV2(projectskillbinding.OutboxAADV2{
		SchemaVersion: payload.SchemaVersion, CommandID: payload.CommandID, ProjectID: payload.ProjectID,
		OwnerUserID: payload.OwnerUserID, RequestDigest: requestDigest.Hex(), SkillSnapshotDigest: snapshotDigest.Hex(),
	})
	if err != nil {
		t.Fatalf("编码 Outbox AAD 失败: %v", err)
	}
	protector, err := promptcrypto.NewBootstrapV2AESGCMProtector(bytes.Repeat([]byte{0x52}, 32), "bootstrap-v2")
	if err != nil {
		t.Fatalf("创建保护器失败: %v", err)
	}
	envelope, err := protector.Protect(context.Background(), plaintext, aad)
	if err != nil {
		t.Fatalf("加密 Outbox v2 失败: %v", err)
	}
	requestProjectDigest := toProjectDigest(requestDigest)
	snapshotProjectDigest := toProjectDigest(snapshotDigest)
	payloadProjectDigest := toProjectDigest(payloadDigest)
	leaseOwner := "dispatcher-test"
	leaseUntil := time.Now().UTC().Add(time.Minute)
	bindingVersion := int64(1)
	resolutionID := testResolutionID
	outbox := project.SessionOutbox{
		ID: testCommandID, EventType: project.EnsureSessionEventType, SchemaVersion: project.EnsureSessionSchemaVersionV2,
		AggregateID: testProjectID, OwnerUserID: testOwnerID, RequestDigest: requestProjectDigest,
		HasInitialPrompt: false,
		EncryptedPayload: &project.EncryptedPayload{
			Algorithm: envelope.Algorithm, KeyVersion: envelope.KeyVersion,
			Nonce: envelope.Nonce, Ciphertext: envelope.CiphertextAndTag, PayloadDigest: payloadProjectDigest,
		},
		SkillSnapshotDigest: snapshotProjectDigest, SkillCount: 0,
		BindingSetVersion: &bindingVersion, ResolutionID: &resolutionID,
		Status: project.OutboxStatusProcessing, AvailableAt: time.Now().UTC(), LeaseOwner: &leaseOwner,
		LeaseVersion: 1, LeaseExpiresAt: &leaseUntil, AttemptCount: 1, MaxAttempts: 5,
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	return outbox, payload, protector
}

func validReceiptForOutbox(outbox project.SessionOutbox) Receipt {
	return Receipt{
		CommandID: outbox.ID, SessionID: testSessionID,
		SkillSnapshotDigest: toBindingDigest(outbox.SkillSnapshotDigest), SkillCount: outbox.SkillCount,
	}
}

func toProjectDigest(value projectskillbinding.Digest) project.Digest {
	var result project.Digest
	copy(result[:], value[:])
	return result
}
