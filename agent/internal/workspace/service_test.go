package workspace

import (
	"bytes"
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
	testSessionID       = "019f0000-0000-7000-8000-000000000005"
	testProjectID       = "019f0000-0000-7000-8000-000000000004"
	testUserID          = "019f0000-0000-7000-8000-000000000002"
	testMessageID       = "019f0000-0000-7000-8000-000000000006"
	testInputID         = "019f0000-0000-7000-8000-000000000007"
	testEventID         = "019f0000-0000-7000-8000-000000000008"
	testTurnID          = "019f0000-0000-7000-8000-000000000009"
	testRunID           = "019f0000-0000-7000-8000-000000000010"
	testToolCallID      = "019f0000-0000-7000-8000-000000000011"
	testRequestID       = "019f0000-0000-7000-8000-000000000012"
	testTerminalEventID = "019f0000-0000-7000-8000-000000000013"
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

func TestLoadSnapshotV4PreservesV3AndAddsNullableWritePrompts(t *testing.T) {
	now := time.Date(2026, 7, 17, 8, 0, 0, 0, time.UTC)
	base := SnapshotRecord{
		Session: SessionRecord{
			ID: testSessionID, ProjectID: testProjectID, UserID: testUserID,
			Status: string(session.StatusActive), Version: 1, CreatedAt: now, UpdatedAt: now,
		},
		Messages: []MessageRecord{{
			ID: testMessageID, SessionID: testSessionID, Seq: 1, Role: string(session.MessageRoleUser), CreatedAt: now,
		}},
		Inputs: []InputRecord{{
			ID: testInputID, SessionID: testSessionID, MessageID: stringPointer(testMessageID),
			SourceType: string(session.InputSourceTypeUserMessage), Status: string(session.InputStatusResolved),
			EnqueueSeq: 1, AvailableAt: now, CreatedAt: now, UpdatedAt: now,
		}},
		EventHighWatermark: 3, MinAvailableSeq: 1,
	}
	repository := &fakeRepository{snapshot: base}
	service := newTestService(t, repository, fakeDecryptor{plaintext: []byte("创建一支短片")})
	snapshot, err := service.LoadSnapshot(context.Background(), testIdentity(), testEventID)
	if err != nil || snapshot.SchemaVersion != SnapshotSchemaVersionV4 || snapshot.LatestTurnOutput != nil ||
		snapshot.PlanStoryboardPreview != nil || snapshot.WritePromptsPreview != nil {
		t.Fatalf("nullable V4 Snapshot=%+v err=%v", snapshot, err)
	}
	repository.snapshot.LatestTurnOutput = &TurnOutputRecord{
		SessionID: testSessionID, SourceInputID: testInputID, SourceEnqueueSeq: 1,
		TurnID: testTurnID, RunID: testRunID,
		SchemaVersion: event.DirectResponseCardSchemaVersionV1, Status: event.DirectResponseCompletedStatus,
		PayloadJSON: mustJSON(t, event.SessionTurnDirectResponsePayload{
			SchemaVersion: event.DirectResponseCardSchemaVersionV1,
			TurnID:        testTurnID, RunID: testRunID, InputID: testInputID,
			Status: event.DirectResponseCompletedStatus, MessageCode: event.DirectResponseMessageCode,
			Summary: event.DirectResponseSummary, AvailableActions: []string{event.DirectResponseActionOpenToolbox},
		}),
		ProjectionVersion: 1, UpdatedAt: now,
	}
	snapshot, err = service.LoadSnapshot(context.Background(), testIdentity(), testEventID)
	if err != nil || snapshot.LatestTurnOutput == nil {
		t.Fatalf("Direct Response Snapshot=%+v err=%v", snapshot, err)
	}
	encoded, marshalErr := json.Marshal(snapshot)
	if marshalErr != nil || !strings.Contains(string(encoded), `"schema_version":"session.workspace.v4"`) ||
		!strings.Contains(string(encoded), `"latest_turn_output":{"schema_version":"session.turn.direct_response.card.v1"`) ||
		!strings.Contains(string(encoded), `"plan_storyboard_preview":null`) ||
		!strings.Contains(string(encoded), `"write_prompts_preview":null`) || strings.Contains(string(encoded), `"error_code"`) {
		t.Fatalf("V4 Snapshot Card exact-set 错误: body=%s err=%v", encoded, marshalErr)
	}
}

func TestSnapshotMarshalPreservesV1AndV2ExactSets(t *testing.T) {
	snapshot := Snapshot{Messages: []MessageDTO{}, Inputs: []InputDTO{}}
	tests := []struct {
		version string
		fields  int
	}{
		{SnapshotSchemaVersionV1, 8},
		{SnapshotSchemaVersionV2, 10},
		{SnapshotSchemaVersionV3, 11},
		{SnapshotSchemaVersionV4, 12},
	}
	for _, test := range tests {
		snapshot.SchemaVersion = test.version
		encoded, err := json.Marshal(snapshot)
		if err != nil {
			t.Fatalf("marshal %s: %v", test.version, err)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(encoded, &fields); err != nil || len(fields) != test.fields {
			t.Fatalf("%s exact-set 字段=%d want=%d body=%s err=%v", test.version, len(fields), test.fields, encoded, err)
		}
		_, hasPlan := fields["plan_storyboard_preview"]
		if hasPlan != (test.version == SnapshotSchemaVersionV3 || test.version == SnapshotSchemaVersionV4) {
			t.Fatalf("%s plan_storyboard_preview 存在性错误: %s", test.version, encoded)
		}
		_, hasWritePrompts := fields["write_prompts_preview"]
		if hasWritePrompts != (test.version == SnapshotSchemaVersionV4) {
			t.Fatalf("%s write_prompts_preview 存在性错误: %s", test.version, encoded)
		}
	}
}

func TestLoadSnapshotRejectsMixedOrUnboundLatestTurnOutput(t *testing.T) {
	now := time.Date(2026, 7, 17, 8, 0, 0, 0, time.UTC)
	record := SnapshotRecord{
		Session: SessionRecord{
			ID: testSessionID, ProjectID: testProjectID, UserID: testUserID,
			Status: string(session.StatusActive), Version: 1, CreatedAt: now, UpdatedAt: now,
		},
		Messages: []MessageRecord{{
			ID: testMessageID, SessionID: testSessionID, Seq: 1, Role: string(session.MessageRoleUser), CreatedAt: now,
		}},
		Inputs: []InputRecord{{
			ID: testInputID, SessionID: testSessionID, MessageID: stringPointer(testMessageID),
			SourceType: string(session.InputSourceTypeUserMessage), Status: string(session.InputStatusResolved),
			EnqueueSeq: 1, AvailableAt: now, CreatedAt: now, UpdatedAt: now,
		}},
		LatestTurnOutput: &TurnOutputRecord{
			SessionID: testSessionID, SourceInputID: testInputID, SourceEnqueueSeq: 1,
			TurnID: testTurnID, RunID: testRunID,
			SchemaVersion: event.DirectResponseCardSchemaVersionV1, Status: event.DirectResponseCompletedStatus,
			PayloadJSON: []byte(`{"schema_version":"session.turn.direct_response.card.v1","turn_id":"` + testTurnID +
				`","run_id":"` + testRunID + `","input_id":"` + testInputID +
				`","status":"completed","message_code":"creation_request_received","summary":"` + event.DirectResponseSummary +
				`","available_actions":["open_toolbox"],"error_code":"MUST_NOT_LEAK"}`),
			ProjectionVersion: 1, UpdatedAt: now,
		},
		EventHighWatermark: 3, MinAvailableSeq: 1,
	}
	repository := &fakeRepository{snapshot: record}
	service := newTestService(t, repository, fakeDecryptor{plaintext: []byte("创建一支短片")})
	if _, err := service.LoadSnapshot(context.Background(), testIdentity(), testEventID); !errors.Is(err, ErrPersistence) {
		t.Fatalf("混合 Card 错误=%v", err)
	}
	repository.snapshot.LatestTurnOutput.PayloadJSON = mustJSON(t, event.SessionTurnDirectResponsePayload{
		SchemaVersion: event.DirectResponseCardSchemaVersionV1,
		TurnID:        testTurnID, RunID: testRunID, InputID: "019f0000-0000-7000-8000-000000000099",
		Status: event.DirectResponseCompletedStatus, MessageCode: event.DirectResponseMessageCode,
		Summary: event.DirectResponseSummary, AvailableActions: []string{event.DirectResponseActionOpenToolbox},
	})
	if _, err := service.LoadSnapshot(context.Background(), testIdentity(), testEventID); !errors.Is(err, ErrPersistence) {
		t.Fatalf("未绑定 Input Card 错误=%v", err)
	}
}

func TestLoadSnapshotProjectsAnalyzeMaterialsWithoutMessage(t *testing.T) {
	now := time.Date(2026, 7, 17, 9, 0, 0, 0, time.UTC)
	retryable := true
	card := event.AnalyzeMaterialsPreviewCardPayload{
		SchemaVersion: event.AnalyzeMaterialsPreviewCardSchemaVersionV1,
		InputID:       testInputID, TurnID: testTurnID, RunID: testRunID, ToolCallID: testToolCallID,
		Status: "failed", ResultCode: "MODEL_TEMPORARILY_UNAVAILABLE",
		FailureKind: event.AnalyzeMaterialsPreviewFailureKindRuntime,
		Summary:     "素材分析暂时不可用，请稍后重试。", Retryable: &retryable,
	}
	repository := &fakeRepository{snapshot: SnapshotRecord{
		Session: SessionRecord{
			ID: testSessionID, ProjectID: testProjectID, UserID: testUserID,
			Status: string(session.StatusActive), Version: 1, CreatedAt: now, UpdatedAt: now,
		},
		Inputs: []InputRecord{{
			ID: testInputID, SessionID: testSessionID, MessageID: nil,
			SourceType: string(session.InputSourceTypeAnalyzeMaterialsPreview), Status: string(session.InputStatusDead),
			EnqueueSeq: 1, AvailableAt: now, CreatedAt: now, UpdatedAt: now,
		}},
		AnalyzeMaterialsPreview: &AnalyzeMaterialsPreviewRecord{
			SessionID: testSessionID, SourceInputID: testInputID, SourceEnqueueSeq: 1,
			TurnID: testTurnID, RunID: testRunID, ToolCallID: testToolCallID,
			SchemaVersion: event.AnalyzeMaterialsPreviewCardSchemaVersionV1,
			OutcomeKind:   "runtime_failed", Status: "failed", ResultDigest: strings.Repeat("a", 64),
			PayloadJSON: mustJSON(t, card), ProjectionVersion: 1, CreatedAt: now,
		},
		EventHighWatermark: 2, MinAvailableSeq: 1,
	}}
	service := newTestService(t, repository, fakeDecryptor{})
	snapshot, err := service.LoadSnapshot(context.Background(), testIdentity(), testRequestID)
	if err != nil || snapshot.AnalyzeMaterialsPreview == nil || len(snapshot.Inputs) != 1 || snapshot.Inputs[0].MessageID != nil {
		t.Fatalf("投影 Analyze Materials Snapshot 失败: snapshot=%+v err=%v", snapshot, err)
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil || !strings.Contains(string(encoded), `"message_id":null`) ||
		!strings.Contains(string(encoded), `"analyze_materials_preview":{"schema_version":"analyze_materials.preview.card.v1"`) {
		t.Fatalf("Analyze Materials Snapshot wire 错误: %s err=%v", encoded, err)
	}

	repository.snapshot.Inputs[0].MessageID = stringPointer(testMessageID)
	if _, err := service.LoadSnapshot(context.Background(), testIdentity(), testRequestID); !errors.Is(err, ErrPersistence) {
		t.Fatalf("Analyze Materials Input 接受 message_id: %v", err)
	}
}

func TestLoadEventBatchProjectsAnalyzeMaterialsExactSet(t *testing.T) {
	now := time.Date(2026, 7, 17, 9, 0, 0, 0, time.UTC)
	accepted, err := event.NewAnalyzeMaterialsPreviewAccepted(testEventID, event.AnalyzeMaterialsPreviewAcceptedPayload{
		InputID: testInputID, SessionID: testSessionID, TurnID: testTurnID, RunID: testRunID,
		RequestID: testRequestID, SourceType: event.SourceKindAnalyzeMaterialsPreview,
		IntentDigest: strings.Repeat("a", 64), ToolCallID: testToolCallID, ContextDigest: strings.Repeat("b", 64),
	}, now)
	if err != nil {
		t.Fatalf("构造 accepted 事件失败: %v", err)
	}
	retryable := true
	terminal, err := event.NewAnalyzeMaterialsPreviewRuntimeFailed(
		testTerminalEventID, testSessionID, testRequestID,
		event.AnalyzeMaterialsPreviewCardPayload{
			SchemaVersion: event.AnalyzeMaterialsPreviewCardSchemaVersionV1,
			InputID:       testInputID, TurnID: testTurnID, RunID: testRunID, ToolCallID: testToolCallID,
			Status: "failed", ResultCode: "MODEL_TEMPORARILY_UNAVAILABLE",
			FailureKind: event.AnalyzeMaterialsPreviewFailureKindRuntime,
			Summary:     "素材分析暂时不可用，请稍后重试。", Retryable: &retryable,
		}, 1, now)
	if err != nil {
		t.Fatalf("构造 terminal 事件失败: %v", err)
	}
	repository := &fakeRepository{batch: EventBatchRecord{
		LastSeq: 3, MinAvailableSeq: 1,
		Events: []EventRecord{
			toWorkspaceEventRecord(accepted, 2), toWorkspaceEventRecord(terminal, 3),
		},
	}}
	service := newTestService(t, repository, fakeDecryptor{})
	batch, err := service.LoadEventBatch(context.Background(), testIdentity(), 1, 10)
	if err != nil || len(batch.Events) != 2 || batch.Events[0].Event != string(event.TypeAnalyzeMaterialsPreviewAccepted) ||
		batch.Events[1].Event != string(event.TypeAnalyzeMaterialsPreviewRuntimeFailed) {
		t.Fatalf("投影 Analyze Materials Event 失败: batch=%+v err=%v", batch, err)
	}

	repository.batch.Events[0].Payload = append([]byte(`{"secret":"must-not-pass",`), accepted.PayloadJSON[1:]...)
	if _, err := service.LoadEventBatch(context.Background(), testIdentity(), 1, 10); !errors.Is(err, ErrProjectionInvalid) {
		t.Fatalf("accepted 未拒绝未知字段: %v", err)
	}
}

func TestPlanStoryboardSnapshotAndSSEReplaySameTerminalCard(t *testing.T) {
	now := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)
	retryable := false
	card := event.PlanStoryboardPreviewCardPayload{
		SchemaVersion: event.PlanStoryboardPreviewCardSchemaVersionV1,
		InputID:       testInputID, TurnID: testTurnID, RunID: testRunID, ToolCallID: testToolCallID,
		Status: "failed", ResultCode: "PLAN_STORYBOARD_RUNTIME_FAILED", UpdatedAt: now,
		FailureKind: event.PlanStoryboardPreviewFailureKindRuntime,
		Summary:     "故事板规划运行时暂时无法完成", Retryable: &retryable,
	}
	terminal, err := event.NewPlanStoryboardPreviewRuntimeFailed(
		testTerminalEventID, testSessionID, testRequestID, card, 1, now,
	)
	if err != nil {
		t.Fatalf("构造 Storyboard terminal Card 失败: %v", err)
	}
	terminalEvent := toWorkspaceEventRecord(terminal, 2)
	terminalEvent.PlanTurnID = testTurnID
	terminalEvent.PlanRunID = testRunID
	terminalEvent.PlanToolCallID = testToolCallID
	repository := &fakeRepository{
		snapshot: SnapshotRecord{
			Session: SessionRecord{
				ID: testSessionID, ProjectID: testProjectID, UserID: testUserID,
				Status: string(session.StatusActive), Version: 1, CreatedAt: now, UpdatedAt: now,
			},
			Inputs: []InputRecord{{
				ID: testInputID, SessionID: testSessionID, SourceType: string(session.InputSourceTypePlanStoryboardPreview),
				Status: string(session.InputStatusDead), EnqueueSeq: 1, AvailableAt: now, CreatedAt: now, UpdatedAt: now,
			}},
			PlanStoryboardPreview: &PlanStoryboardPreviewRecord{
				SessionID: testSessionID, SourceInputID: testInputID, SourceEnqueueSeq: 1,
				TurnID: testTurnID, RunID: testRunID, ToolCallID: testToolCallID,
				EventType: string(event.TypePlanStoryboardPreviewRuntimeFailed), PayloadJSON: terminal.PayloadJSON,
				AggregateVersion: 1, CreatedAt: now,
			},
			EventHighWatermark: 2, MinAvailableSeq: 1,
		},
		batch: EventBatchRecord{
			LastSeq: 2, MinAvailableSeq: 1,
			Events: []EventRecord{terminalEvent},
		},
	}
	service := newTestService(t, repository, fakeDecryptor{})
	snapshot, err := service.LoadSnapshot(context.Background(), testIdentity(), testRequestID)
	if err != nil || snapshot.SchemaVersion != SnapshotSchemaVersionV4 || snapshot.PlanStoryboardPreview == nil ||
		snapshot.WritePromptsPreview != nil ||
		len(snapshot.Inputs) != 1 || snapshot.Inputs[0].MessageID != nil {
		t.Fatalf("硬刷新 Storyboard terminal Card 失败: snapshot=%+v err=%v", snapshot, err)
	}
	batch, err := service.LoadEventBatch(context.Background(), testIdentity(), 1, 10)
	if err != nil || len(batch.Events) != 1 || batch.Events[0].Event != string(event.TypePlanStoryboardPreviewRuntimeFailed) {
		t.Fatalf("SSE 重放 Storyboard 事件失败: batch=%+v err=%v", batch, err)
	}
	var snapshotWire map[string]json.RawMessage
	encodedSnapshot, _ := json.Marshal(snapshot)
	if err := json.Unmarshal(encodedSnapshot, &snapshotWire); err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(batch.Events[0].Data, &envelope); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(snapshotWire["plan_storyboard_preview"], envelope.Payload) {
		t.Fatalf("Snapshot/SSE Card 不同义:\nsnapshot=%s\nevent=%s", snapshotWire["plan_storyboard_preview"], envelope.Payload)
	}
	for _, forbidden := range []string{"prompt", "intent", "provider", "access_scope", "business_command"} {
		if bytes.Contains(envelope.Payload, []byte(forbidden)) {
			t.Fatalf("terminal Card 泄漏禁用字段 %q: %s", forbidden, envelope.Payload)
		}
	}
	repository.batch.Events[0].PlanTurnID = "019f0000-0000-7000-8000-000000000099"
	if _, err := service.LoadEventBatch(context.Background(), testIdentity(), 1, 10); !errors.Is(err, ErrProjectionInvalid) {
		t.Fatalf("SSE Storyboard Card 接受与冻结 Context 不一致的 turn_id: %v", err)
	}
	repository.batch.Events[0].PlanTurnID = testTurnID
	repository.snapshot.PlanStoryboardPreview.TurnID = "019f0000-0000-7000-8000-000000000099"
	if _, err := service.LoadSnapshot(context.Background(), testIdentity(), testRequestID); !errors.Is(err, ErrPersistence) {
		t.Fatalf("Storyboard Card 接受与冻结 Context 不一致的 turn_id: %v", err)
	}
	repository.snapshot.PlanStoryboardPreview.TurnID = testTurnID

	var corrupted map[string]any
	if err := json.Unmarshal(terminal.PayloadJSON, &corrupted); err != nil {
		t.Fatal(err)
	}
	corrupted["storyboard_preview_id"] = ""
	repository.snapshot.PlanStoryboardPreview.PayloadJSON, _ = json.Marshal(corrupted)
	if _, err := service.LoadSnapshot(context.Background(), testIdentity(), testRequestID); !errors.Is(err, ErrPersistence) {
		t.Fatalf("failed Card 接受 completed-only 字段: %v", err)
	}
}

func TestWritePromptsSnapshotAndSSEReplaySameTerminalCard(t *testing.T) {
	now := time.Date(2026, 7, 17, 11, 0, 0, 0, time.UTC)
	retryable := true
	card := event.WritePromptsPreviewCardPayload{
		SchemaVersion: event.WritePromptsPreviewCardSchemaVersionV1,
		InputID:       testInputID, TurnID: testTurnID, RunID: testRunID, ToolCallID: testToolCallID,
		Status: "failed", ResultCode: "WRITE_PROMPTS_RUNTIME_FAILED", UpdatedAt: now,
		FailureKind: event.WritePromptsPreviewFailureKindRuntime,
		Summary:     "提示词生成运行时暂时无法完成", Retryable: &retryable,
	}
	terminal, err := event.NewWritePromptsPreviewRuntimeFailed(
		testTerminalEventID, testSessionID, testRequestID, card, 1, now,
	)
	if err != nil {
		t.Fatalf("构造 Write Prompts terminal Card 失败: %v", err)
	}
	terminalEvent := toWorkspaceEventRecord(terminal, 2)
	terminalEvent.WriteTurnID = testTurnID
	terminalEvent.WriteRunID = testRunID
	terminalEvent.WriteToolCallID = testToolCallID
	repository := &fakeRepository{
		snapshot: SnapshotRecord{
			Session: SessionRecord{
				ID: testSessionID, ProjectID: testProjectID, UserID: testUserID,
				Status: string(session.StatusActive), Version: 1, CreatedAt: now, UpdatedAt: now,
			},
			Inputs: []InputRecord{{
				ID: testInputID, SessionID: testSessionID, SourceType: string(session.InputSourceTypeWritePromptsPreview),
				Status: string(session.InputStatusDead), EnqueueSeq: 1, AvailableAt: now, CreatedAt: now, UpdatedAt: now,
			}},
			WritePromptsPreview: &WritePromptsPreviewRecord{
				SessionID: testSessionID, SourceInputID: testInputID, SourceEnqueueSeq: 1,
				TurnID: testTurnID, RunID: testRunID, ToolCallID: testToolCallID,
				EventType: string(event.TypeWritePromptsPreviewRuntimeFailed), PayloadJSON: terminal.PayloadJSON,
				AggregateVersion: 1, CreatedAt: now,
			},
			EventHighWatermark: 2, MinAvailableSeq: 1,
		},
		batch: EventBatchRecord{LastSeq: 2, MinAvailableSeq: 1, Events: []EventRecord{terminalEvent}},
	}
	service := newTestService(t, repository, fakeDecryptor{})
	snapshot, err := service.LoadSnapshot(context.Background(), testIdentity(), testRequestID)
	if err != nil || snapshot.SchemaVersion != SnapshotSchemaVersionV4 || snapshot.WritePromptsPreview == nil ||
		len(snapshot.Inputs) != 1 || snapshot.Inputs[0].MessageID != nil {
		t.Fatalf("硬刷新 Write Prompts terminal Card 失败: snapshot=%+v err=%v", snapshot, err)
	}
	batch, err := service.LoadEventBatch(context.Background(), testIdentity(), 1, 10)
	if err != nil || len(batch.Events) != 1 || batch.Events[0].Event != string(event.TypeWritePromptsPreviewRuntimeFailed) {
		t.Fatalf("SSE 重放 Write Prompts 事件失败: batch=%+v err=%v", batch, err)
	}
	var snapshotWire map[string]json.RawMessage
	encodedSnapshot, _ := json.Marshal(snapshot)
	if err := json.Unmarshal(encodedSnapshot, &snapshotWire); err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(batch.Events[0].Data, &envelope); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(snapshotWire["write_prompts_preview"], envelope.Payload) {
		t.Fatalf("Snapshot/SSE Prompt Card 不同义:\nsnapshot=%s\nevent=%s", snapshotWire["write_prompts_preview"], envelope.Payload)
	}
	repository.batch.Events[0].WriteToolCallID = "019f0000-0000-7000-8000-000000000099"
	if _, err := service.LoadEventBatch(context.Background(), testIdentity(), 1, 10); !errors.Is(err, ErrProjectionInvalid) {
		t.Fatalf("SSE Prompt Card 接受与冻结 Context 不一致的 tool_call_id: %v", err)
	}
	repository.batch.Events[0].WriteToolCallID = testToolCallID

	var corrupted map[string]any
	if err := json.Unmarshal(terminal.PayloadJSON, &corrupted); err != nil {
		t.Fatal(err)
	}
	corrupted["prompt_preview_id"] = ""
	repository.snapshot.WritePromptsPreview.PayloadJSON, _ = json.Marshal(corrupted)
	if _, err := service.LoadSnapshot(context.Background(), testIdentity(), testRequestID); !errors.Is(err, ErrPersistence) {
		t.Fatalf("failed Prompt Card 接受 completed-only 字段: %v", err)
	}
}

func TestLoadEventBatchProjectsSessionTurnExactSet(t *testing.T) {
	now := time.Date(2026, 7, 17, 8, 0, 0, 0, time.UTC)
	direct := event.SessionTurnDirectResponsePayload{
		SchemaVersion: event.DirectResponseCardSchemaVersionV1,
		TurnID:        testTurnID, RunID: testRunID, InputID: testInputID,
		Status: event.DirectResponseCompletedStatus, MessageCode: event.DirectResponseMessageCode,
		Summary: event.DirectResponseSummary, AvailableActions: []string{event.DirectResponseActionOpenToolbox},
	}
	payload, err := json.Marshal(direct)
	if err != nil {
		t.Fatalf("编码 Direct Response 失败: %v", err)
	}
	repository := &fakeRepository{batch: EventBatchRecord{
		LastSeq: 3, MinAvailableSeq: 1,
		Events: []EventRecord{{
			EventID: testEventID, SessionID: testSessionID, Seq: 3,
			EventType: string(event.TypeSessionTurnCompleted), SchemaVersion: event.SchemaVersionV1,
			AggregateType: string(event.AggregateTypeSessionTurn), AggregateID: testTurnID,
			AggregateVersion: 2, Payload: payload, CreatedAt: now,
		}},
	}}
	service := newTestService(t, repository, fakeDecryptor{})
	batch, err := service.LoadEventBatch(context.Background(), testIdentity(), 2, 10)
	if err != nil || len(batch.Events) != 1 || batch.Events[0].Event != string(event.TypeSessionTurnCompleted) {
		t.Fatalf("投影 session.turn.completed 失败: batch=%+v err=%v", batch, err)
	}

	failure := event.SessionTurnFailurePayload{
		SchemaVersion: event.FailureCardSchemaVersionV1,
		TurnID:        testTurnID, RunID: testRunID, InputID: testInputID,
		Status: event.TurnRecoveryPendingStatus, ErrorCode: "MODEL_RESULT_UNKNOWN", Retryable: true,
		Summary: "处理结果正在恢复，请稍后查看。",
	}
	repository.batch.Events[0].EventType = string(event.TypeSessionTurnRecoveryPending)
	repository.batch.Events[0].Payload, _ = json.Marshal(failure)
	if _, err := service.LoadEventBatch(context.Background(), testIdentity(), 2, 10); err != nil {
		t.Fatalf("投影 session.turn.recovery_pending 失败: %v", err)
	}
	repository.batch.Events[0].EventType = string(event.TypeSessionTurnFailed)
	if _, err := service.LoadEventBatch(context.Background(), testIdentity(), 2, 10); !errors.Is(err, ErrProjectionInvalid) {
		t.Fatalf("failed Event 接受 recovery_pending Card: %v", err)
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

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("编码测试 JSON 失败: %v", err)
	}
	return encoded
}

func toWorkspaceEventRecord(record event.Record, seq int64) EventRecord {
	return EventRecord{
		EventID: record.EventID, SessionID: record.SessionID, Seq: seq,
		EventType: string(record.Type), SchemaVersion: record.SchemaVersion,
		AggregateType: string(record.AggregateType), AggregateID: record.AggregateID,
		AggregateVersion: record.AggregateVersion, Payload: record.PayloadJSON, CreatedAt: record.CreatedAt,
	}
}
