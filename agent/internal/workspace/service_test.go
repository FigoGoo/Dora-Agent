package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/agent/internal/event"
	"github.com/FigoGoo/Dora-Agent/agent/internal/session"
)

const (
	testSessionID = "019f0000-0000-7000-8000-000000000005"
	testProjectID = "019f0000-0000-7000-8000-000000000004"
	testUserID    = "019f0000-0000-7000-8000-000000000002"
	testMessageID = "019f0000-0000-7000-8000-000000000006"
	testInputID   = "019f0000-0000-7000-8000-000000000007"
	testEventID   = "019f0000-0000-7000-8000-000000000008"
)

type fakeRepository struct {
	snapshot SnapshotRecord
	snapErr  error
	batch    EventBatchRecord
	batchErr error
}

func (repository *fakeRepository) LoadSnapshot(context.Context, Identity, SnapshotLimits) (SnapshotRecord, error) {
	return repository.snapshot, repository.snapErr
}

func (repository *fakeRepository) LoadEventBatch(context.Context, Identity, int64, int) (EventBatchRecord, error) {
	return repository.batch, repository.batchErr
}

type fakeDecryptor struct {
	plaintext []byte
	err       error
}

func (decryptor fakeDecryptor) Open(context.Context, session.ProtectedContent, string) ([]byte, error) {
	return append([]byte(nil), decryptor.plaintext...), decryptor.err
}

// TestLoadSnapshotReturnsNonNullArraysAndSeparatesAuthorization 验证空 Prompt 数组编码为 []，且身份不匹配与损坏投影分别映射 404/503。
func TestLoadSnapshotReturnsNonNullArraysAndSeparatesAuthorization(t *testing.T) {
	now := time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC)
	record := SnapshotRecord{
		Session: SessionRecord{
			ID: testSessionID, ProjectID: testProjectID, UserID: testUserID,
			Status: string(session.StatusActive), Version: 1, CreatedAt: now, UpdatedAt: now,
		},
		EventHighWatermark: 1, MinAvailableSeq: 1,
	}
	repository := &fakeRepository{snapshot: record}
	service := newTestService(t, repository, fakeDecryptor{})
	snapshot, err := service.LoadSnapshot(context.Background(), testIdentity(), "019f0000-0000-7000-8000-000000000001")
	if err != nil {
		t.Fatalf("加载空 Snapshot 失败: %v", err)
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("编码 Snapshot 失败: %v", err)
	}
	if !strings.Contains(string(encoded), `"messages":[]`) || !strings.Contains(string(encoded), `"inputs":[]`) {
		t.Fatalf("空集合被编码为 null: %s", encoded)
	}

	mismatch := testIdentity()
	mismatch.ProjectID = "019f0000-0000-7000-8000-000000000009"
	if _, err := service.LoadSnapshot(context.Background(), mismatch, "request"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("身份不匹配错误=%v", err)
	}
	repository.snapshot.Session.Version = 0
	if _, err := service.LoadSnapshot(context.Background(), testIdentity(), "request"); !errors.Is(err, ErrPersistence) {
		t.Fatalf("损坏投影错误=%v", err)
	}
}

// TestLoadSnapshotFailsWholeResponseOnContentError 验证任一正文解密失败会关闭完整 Snapshot，不返回占位消息。
func TestLoadSnapshotFailsWholeResponseOnContentError(t *testing.T) {
	now := time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC)
	repository := &fakeRepository{snapshot: SnapshotRecord{
		Session: SessionRecord{
			ID: testSessionID, ProjectID: testProjectID, UserID: testUserID,
			Status: string(session.StatusActive), Version: 1, CreatedAt: now, UpdatedAt: now,
		},
		Messages: []MessageRecord{{
			ID: testMessageID, SessionID: testSessionID, Seq: 1, Role: string(session.MessageRoleUser), CreatedAt: now,
		}},
		Inputs: []InputRecord{{
			ID: testInputID, SessionID: testSessionID, MessageID: stringPointer(testMessageID),
			SourceType: string(session.InputSourceTypeUserMessage), Status: string(session.InputStatusPending),
			EnqueueSeq: 1, AvailableAt: now, CreatedAt: now, UpdatedAt: now,
		}},
		EventHighWatermark: 2, MinAvailableSeq: 1,
	}}
	service := newTestService(t, repository, fakeDecryptor{err: session.ErrContentUnavailable})
	if _, err := service.LoadSnapshot(context.Background(), testIdentity(), "request"); !errors.Is(err, ErrContentUnavailable) {
		t.Fatalf("正文失败错误=%v", err)
	}
}

// TestLoadEventBatchProjectsStrictEnvelopeAndDetectsGaps 验证 id/seq/event 三者一致，并拒绝空批高水位、超量和越界事件。
func TestLoadEventBatchProjectsStrictEnvelopeAndDetectsGaps(t *testing.T) {
	now := time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC)
	payload, err := json.Marshal(event.SessionInputAcceptedPayload{
		SessionID: testSessionID, InputID: testInputID, MessageID: testMessageID,
		EnqueueSeq: 1, Status: string(session.InputStatusPending),
	})
	if err != nil {
		t.Fatalf("编码事件 Fixture 失败: %v", err)
	}
	valid := EventRecord{
		EventID: testEventID, SessionID: testSessionID, Seq: 2,
		EventType: string(event.TypeSessionInputAccepted), SchemaVersion: event.SchemaVersionV1,
		AggregateType: string(event.AggregateTypeSessionInput), AggregateID: testInputID,
		AggregateVersion: 1, Payload: payload, CreatedAt: now,
	}
	repository := &fakeRepository{batch: EventBatchRecord{LastSeq: 2, MinAvailableSeq: 1, Events: []EventRecord{valid}}}
	service := newTestService(t, repository, fakeDecryptor{})
	batch, err := service.LoadEventBatch(context.Background(), testIdentity(), 1, 10)
	if err != nil || len(batch.Events) != 1 {
		t.Fatalf("投影有效事件失败: batch=%+v err=%v", batch, err)
	}
	var wire struct {
		Seq   int64  `json:"seq"`
		Event string `json:"event"`
	}
	if err := json.Unmarshal(batch.Events[0].Data, &wire); err != nil || wire.Seq != batch.Events[0].Seq || wire.Event != batch.Events[0].Event {
		t.Fatalf("SSE/JSON 事件不一致: projected=%+v wire=%+v err=%v", batch.Events[0], wire, err)
	}

	repository.batch = EventBatchRecord{LastSeq: 3, MinAvailableSeq: 1}
	if _, err := service.LoadEventBatch(context.Background(), testIdentity(), 2, 10); !errors.Is(err, ErrEventGap) {
		t.Fatalf("空批高水位错误=%v", err)
	}
	repository.batch = EventBatchRecord{LastSeq: 3, MinAvailableSeq: 1, Events: []EventRecord{valid, valid}}
	if _, err := service.LoadEventBatch(context.Background(), testIdentity(), 1, 1); !errors.Is(err, ErrProjectionInvalid) {
		t.Fatalf("超量批次错误=%v", err)
	}
	tooHigh := valid
	tooHigh.Seq = 3
	repository.batch = EventBatchRecord{LastSeq: 2, MinAvailableSeq: 1, Events: []EventRecord{tooHigh}}
	if _, err := service.LoadEventBatch(context.Background(), testIdentity(), 1, 10); !errors.Is(err, ErrEventGap) {
		t.Fatalf("事件超过高水位错误=%v", err)
	}
}

// TestLoadEventBatchRejectsUnknownPayloadField 验证未知 Payload 字段不会穿透到 workspace.event.v1。
func TestLoadEventBatchRejectsUnknownPayloadField(t *testing.T) {
	for _, payload := range []string{
		`{"session_id":"` + testSessionID + `","input_id":"` + testInputID + `","message_id":"` + testMessageID + `","enqueue_seq":1,"status":"pending","secret":"no"}`,
		`{"session_id":"` + testSessionID + `","session_id":"` + testSessionID + `","input_id":"` + testInputID + `","message_id":"` + testMessageID + `","enqueue_seq":1,"status":"pending"}`,
	} {
		repository := &fakeRepository{batch: EventBatchRecord{
			LastSeq: 2, MinAvailableSeq: 1,
			Events: []EventRecord{{
				EventID: testEventID, SessionID: testSessionID, Seq: 2,
				EventType: string(event.TypeSessionInputAccepted), SchemaVersion: event.SchemaVersionV1,
				AggregateType: string(event.AggregateTypeSessionInput), AggregateID: testInputID, AggregateVersion: 1,
				Payload: []byte(payload), CreatedAt: time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC),
			}},
		}}
		service := newTestService(t, repository, fakeDecryptor{})
		if _, err := service.LoadEventBatch(context.Background(), testIdentity(), 1, 10); !errors.Is(err, ErrProjectionInvalid) {
			t.Fatalf("非法字段 Payload=%s 错误=%v", payload, err)
		}
	}
}

func newTestService(t *testing.T, repository Repository, decryptor ContentDecryptor) *Service {
	t.Helper()
	service, err := NewService(repository, decryptor, SnapshotLimits{MaxMessages: 10, MaxInputs: 10}, 64<<10)
	if err != nil {
		t.Fatalf("创建 Workspace Service 失败: %v", err)
	}
	return service
}

func testIdentity() Identity {
	return Identity{UserID: testUserID, ProjectID: testProjectID, SessionID: testSessionID}
}

func stringPointer(value string) *string { return &value }
