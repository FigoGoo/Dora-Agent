package event

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/analyzematerials"
	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/planstoryboard"
)

// TestSessionEventsDoNotExposePrompt 验证 W0 事件载荷只包含安全投影，不泄漏 Prompt 正文。
func TestSessionEventsDoNotExposePrompt(t *testing.T) {
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	created, err := NewSessionCreated("event-1", "session-1", "project-1", "active", "command-1", 1, now)
	if err != nil {
		t.Fatalf("创建 Session Event 失败: %v", err)
	}
	accepted, err := NewSessionInputAccepted("event-2", "session-1", "input-1", "message-1", "command-1", "pending", 1, now)
	if err != nil {
		t.Fatalf("创建 Input Event 失败: %v", err)
	}
	for _, record := range []Record{created, accepted} {
		if bytes.Contains(record.PayloadJSON, []byte("secret prompt")) {
			t.Fatalf("事件载荷泄漏 Prompt: %s", record.PayloadJSON)
		}
		if record.CreatedAt.Location() != time.UTC {
			t.Fatalf("事件时间未转换为 UTC: %v", record.CreatedAt.Location())
		}
	}
	if created.ProjectionIndex != 0 || accepted.ProjectionIndex != 1 {
		t.Fatalf("投影顺序不稳定: created=%d accepted=%d", created.ProjectionIndex, accepted.ProjectionIndex)
	}
}

func TestSessionTurnEventsFreezeSafeCardsAndAggregateBinding(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	turnID := "019f0000-0000-7000-8000-000000000009"
	runID := "019f0000-0000-7000-8000-000000000010"
	inputID := "019f0000-0000-7000-8000-000000000007"
	direct := SessionTurnDirectResponsePayload{
		SchemaVersion: DirectResponseCardSchemaVersionV1,
		TurnID:        turnID, RunID: runID, InputID: inputID,
		Status: DirectResponseCompletedStatus, MessageCode: DirectResponseMessageCode,
		Summary: DirectResponseSummary, AvailableActions: []string{DirectResponseActionOpenToolbox},
	}
	completed, err := NewSessionTurnCompleted("event-3", "session-1", runID, direct, 3, now)
	if err != nil {
		t.Fatalf("创建 completed Event 失败: %v", err)
	}
	failure := SessionTurnFailurePayload{
		SchemaVersion: FailureCardSchemaVersionV1,
		TurnID:        turnID, RunID: runID, InputID: inputID,
		Status: TurnFailedStatus, ErrorCode: "MODEL_RESPONSE_INVALID", Retryable: false, Summary: "暂时无法完成处理。",
	}
	failed, err := NewSessionTurnFailed("event-4", "session-1", runID, failure, 4, now)
	if err != nil {
		t.Fatalf("创建 failed Event 失败: %v", err)
	}
	failure.Status = TurnRecoveryPendingStatus
	failure.ErrorCode = "MODEL_RESULT_UNKNOWN"
	failure.Retryable = true
	failure.Summary = "处理结果正在恢复，请稍后查看。"
	recovery, err := NewSessionTurnRecoveryPending("event-5", "session-1", runID, failure, 5, now)
	if err != nil {
		t.Fatalf("创建 recovery Event 失败: %v", err)
	}

	for _, record := range []Record{completed, failed, recovery} {
		if record.AggregateType != AggregateTypeSessionTurn || record.AggregateID != turnID ||
			record.SourceKind != SourceKindUserMessageRuntime || record.ProjectionIndex != 0 ||
			record.CreatedAt.Location() != time.UTC {
			t.Fatalf("Turn Event 聚合或来源不稳定: %+v", record)
		}
		if !json.Valid(record.PayloadJSON) || bytes.Contains(record.PayloadJSON, []byte("secret prompt")) ||
			bytes.Contains(record.PayloadJSON, []byte("provider_payload")) {
			t.Fatalf("Turn Event 载荷不安全: %s", record.PayloadJSON)
		}
	}
	if completed.Type != TypeSessionTurnCompleted || completed.AggregateVersion != 3 ||
		failed.Type != TypeSessionTurnFailed || recovery.Type != TypeSessionTurnRecoveryPending {
		t.Fatalf("Turn Event 类型或版本错误: completed=%+v failed=%+v recovery=%+v", completed, failed, recovery)
	}
}

func TestSessionTurnEventConstructorsRejectStatusOrActionDrift(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	direct := SessionTurnDirectResponsePayload{
		SchemaVersion: DirectResponseCardSchemaVersionV1,
		TurnID:        "019f0000-0000-7000-8000-000000000009",
		RunID:         "019f0000-0000-7000-8000-000000000010",
		InputID:       "019f0000-0000-7000-8000-000000000007",
		Status:        DirectResponseCompletedStatus, MessageCode: DirectResponseMessageCode,
		Summary: DirectResponseSummary, AvailableActions: []string{"run_tool"},
	}
	if _, err := NewSessionTurnCompleted("event", "session", "source", direct, 1, now); err == nil {
		t.Fatal("未知 Direct Response action 未失败关闭")
	}
	failure := SessionTurnFailurePayload{
		SchemaVersion: FailureCardSchemaVersionV1,
		TurnID:        direct.TurnID, RunID: direct.RunID, InputID: direct.InputID,
		Status: TurnRecoveryPendingStatus, ErrorCode: "MODEL_RESULT_UNKNOWN", Summary: "安全摘要",
	}
	if _, err := NewSessionTurnFailed("event", "session", "source", failure, 1, now); err == nil {
		t.Fatal("failed Event 接受 recovery_pending Card")
	}
	if _, err := NewSessionTurnRecoveryPending("event", "session", "source", SessionTurnFailurePayload{}, 1, now); err == nil {
		t.Fatal("空 Failure Card 未失败关闭")
	}
}

func TestAnalyzeMaterialsPreviewAcceptedUsesIndependentExactSafePayload(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	payload := AnalyzeMaterialsPreviewAcceptedPayload{
		InputID:       "019f0000-0000-7000-8000-000000000007",
		SessionID:     "019f0000-0000-7000-8000-000000000005",
		TurnID:        "019f0000-0000-7000-8000-000000000009",
		RunID:         "019f0000-0000-7000-8000-000000000010",
		RequestID:     "019f0000-0000-7000-8000-000000000011",
		SourceType:    SourceKindAnalyzeMaterialsPreview,
		IntentDigest:  strings.Repeat("a", 64),
		ToolCallID:    "019f0000-0000-7000-8000-000000000012",
		ContextDigest: strings.Repeat("b", 64),
	}
	record, err := NewAnalyzeMaterialsPreviewAccepted(
		"019f0000-0000-7000-8000-000000000013", payload, now,
	)
	if err != nil {
		t.Fatalf("创建 analyze_materials.preview.accepted 失败: %v", err)
	}
	if record.Type != TypeAnalyzeMaterialsPreviewAccepted || record.SourceKind != SourceKindAnalyzeMaterialsPreview ||
		record.SourceID != payload.RequestID || record.AggregateType != AggregateTypeSessionInput ||
		record.AggregateID != payload.InputID || record.AggregateVersion != 1 || record.CreatedAt.Location() != time.UTC {
		t.Fatalf("accepted Event 绑定漂移: %+v", record)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(record.PayloadJSON, &fields); err != nil {
		t.Fatalf("accepted Payload 非法: %v", err)
	}
	wantFields := []string{
		"input_id", "session_id", "turn_id", "run_id", "request_id", "source_type",
		"intent_digest", "tool_call_id", "context_digest",
	}
	if len(fields) != len(wantFields) {
		t.Fatalf("accepted Payload 字段数=%d payload=%s", len(fields), record.PayloadJSON)
	}
	for _, field := range wantFields {
		if _, exists := fields[field]; !exists {
			t.Fatalf("accepted Payload 缺少 %s: %s", field, record.PayloadJSON)
		}
	}
	if _, exists := fields["message_id"]; exists {
		t.Fatalf("accepted Payload 复用了 message_id: %s", record.PayloadJSON)
	}

	payload.IntentDigest = "sha256:" + strings.Repeat("a", 64)
	if _, err := NewAnalyzeMaterialsPreviewAccepted("019f0000-0000-7000-8000-000000000013", payload, now); err == nil {
		t.Fatal("非规范 Intent digest 被 accepted Event 接受")
	}
}

func TestAnalyzeMaterialsPreviewToolAndRuntimeFailuresRemainDistinctSafeCards(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	falseValue := false
	base := AnalyzeMaterialsPreviewCardPayload{
		SchemaVersion: AnalyzeMaterialsPreviewCardSchemaVersionV1,
		InputID:       "019f0000-0000-7000-8000-000000000007",
		TurnID:        "019f0000-0000-7000-8000-000000000009",
		RunID:         "019f0000-0000-7000-8000-000000000010",
		ToolCallID:    "019f0000-0000-7000-8000-000000000012",
		Status:        "failed",
		ResultCode:    analyzematerials.ResultCodeMaterialsNotAvailable,
		FailureKind:   AnalyzeMaterialsPreviewFailureKindTool,
		Summary: analyzematerials.ErrorSummary(
			analyzematerials.NewContractError(analyzematerials.ResultCodeMaterialsNotAvailable, nil),
		),
		Retryable: &falseValue,
	}
	toolFailed, err := NewAnalyzeMaterialsPreviewFailed(
		"019f0000-0000-7000-8000-000000000013",
		"019f0000-0000-7000-8000-000000000005",
		"019f0000-0000-7000-8000-000000000010",
		base, 1, now,
	)
	if err != nil {
		t.Fatalf("创建 Tool failed Event 失败: %v", err)
	}

	runtimeCard := base
	runtimeCard.ResultCode = "ANALYZE_MATERIALS_RUNTIME_RETRY_EXHAUSTED"
	runtimeCard.FailureKind = AnalyzeMaterialsPreviewFailureKindRuntime
	runtimeCard.Summary = "素材分析运行时暂时无法完成"
	runtimeCard.Retryable = &falseValue
	runtimeFailed, err := NewAnalyzeMaterialsPreviewRuntimeFailed(
		"019f0000-0000-7000-8000-000000000014",
		"019f0000-0000-7000-8000-000000000005",
		"019f0000-0000-7000-8000-000000000010",
		runtimeCard, 1, now,
	)
	if err != nil {
		t.Fatalf("创建 Runtime failed Event 失败: %v", err)
	}
	if toolFailed.Type != TypeAnalyzeMaterialsPreviewFailed || runtimeFailed.Type != TypeAnalyzeMaterialsPreviewRuntimeFailed ||
		toolFailed.AggregateID != base.TurnID || runtimeFailed.AggregateID != base.TurnID {
		t.Fatalf("两类 failed Event 未严格区分: tool=%+v runtime=%+v", toolFailed, runtimeFailed)
	}
	for _, record := range []Record{toolFailed, runtimeFailed} {
		if !json.Valid(record.PayloadJSON) || bytes.Contains(record.PayloadJSON, []byte("provider")) ||
			bytes.Contains(record.PayloadJSON, []byte("ciphertext")) || bytes.Contains(record.PayloadJSON, []byte("intent")) {
			t.Fatalf("failed Event 载荷不安全: %s", record.PayloadJSON)
		}
	}

	if _, err := NewAnalyzeMaterialsPreviewRuntimeFailed(
		"019f0000-0000-7000-8000-000000000015",
		"019f0000-0000-7000-8000-000000000005",
		"019f0000-0000-7000-8000-000000000010",
		base, 1, now,
	); err == nil {
		t.Fatal("Runtime failed Event 接受 Tool failure_kind")
	}
	if _, err := NewAnalyzeMaterialsPreviewCompleted(
		"019f0000-0000-7000-8000-000000000015",
		"019f0000-0000-7000-8000-000000000005",
		"019f0000-0000-7000-8000-000000000010",
		base, 1, now,
	); err == nil {
		t.Fatal("completed Event 接受 failed Card")
	}
	if _, err := NewAnalyzeMaterialsPreviewPartial(
		"019f0000-0000-7000-8000-000000000015",
		"019f0000-0000-7000-8000-000000000005",
		"019f0000-0000-7000-8000-000000000010",
		base, 1, now,
	); err == nil {
		t.Fatal("partial Event 接受 failed Card")
	}
}

func TestPlanStoryboardCompletedCardKeepsExactSetAndUnicodeScalarBoundary(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	sections := []planstoryboard.Section{{Key: "section_1", Title: "主体", Objective: "建立叙事"}}
	elements := []planstoryboard.Element{{
		Key: "element_1", SectionKey: "section_1", Order: 1, ElementType: "scene", Title: "开场",
		NarrativePurpose: "建立氛围", DurationSeconds: 30, SourcePhaseKey: "phase_1", DependencyKeys: []string{},
	}}
	slots := []planstoryboard.Slot{}
	content := planstoryboard.Content{
		Title: "多字节摘要故事板", Summary: strings.Repeat("界", 900),
		Sections: sections, Elements: elements, Slots: slots,
	}
	digest, err := planstoryboard.ContentDigest(content)
	if err != nil {
		t.Fatalf("计算合法多字节 Storyboard Content 摘要失败: %v", err)
	}
	creationRef := planstoryboard.CreationSpecRef{
		ID: "019f0000-0000-7000-8000-000000000016", Version: 1, ContentDigest: strings.Repeat("a", 64),
	}
	card := PlanStoryboardPreviewCardPayload{
		SchemaVersion: PlanStoryboardPreviewCardSchemaVersionV1,
		InputID:       "019f0000-0000-7000-8000-000000000007", TurnID: "019f0000-0000-7000-8000-000000000009",
		RunID: "019f0000-0000-7000-8000-000000000010", ToolCallID: "019f0000-0000-7000-8000-000000000012",
		Status: "completed", ResultCode: planstoryboard.ResultCodeCompleted, UpdatedAt: now,
		StoryboardPreviewID: "019f0000-0000-7000-8000-000000000017",
		ProjectID:           "019f0000-0000-7000-8000-000000000004", CreationSpecRef: &creationRef,
		Version: 1, ContentDigest: digest, Title: content.Title, Summary: content.Summary,
		Sections: &sections, Elements: &elements, Slots: &slots,
	}
	record, err := NewPlanStoryboardPreviewCompleted(
		"019f0000-0000-7000-8000-000000000013", "019f0000-0000-7000-8000-000000000005",
		"019f0000-0000-7000-8000-000000000014", card, 1, now,
	)
	if err != nil {
		t.Fatalf("2700-byte/900-scalar completed Summary 被拒绝: %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(record.PayloadJSON, &fields); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"schema_version", "input_id", "turn_id", "run_id", "tool_call_id", "status", "result_code", "updated_at",
		"storyboard_preview_id", "project_id", "creation_spec_ref", "version", "content_digest", "title", "summary",
		"sections", "elements", "slots",
	}
	if len(fields) != len(want) {
		t.Fatalf("completed Card 字段数=%d want=%d payload=%s", len(fields), len(want), record.PayloadJSON)
	}
	for _, field := range want {
		if _, exists := fields[field]; !exists {
			t.Fatalf("completed Card 缺少 %s: %s", field, record.PayloadJSON)
		}
	}
	if _, exists := fields["failure_kind"]; exists {
		t.Fatalf("completed Card 泄漏 failure_kind: %s", record.PayloadJSON)
	}

	retryable := false
	failed := PlanStoryboardPreviewCardPayload{
		SchemaVersion: PlanStoryboardPreviewCardSchemaVersionV1,
		InputID:       card.InputID, TurnID: card.TurnID, RunID: card.RunID, ToolCallID: card.ToolCallID,
		Status: "failed", ResultCode: "UNAPPROVED_RESULT_CODE", UpdatedAt: now,
		FailureKind: PlanStoryboardPreviewFailureKindTool, Summary: "安全失败摘要", Retryable: &retryable,
	}
	if _, err := NewPlanStoryboardPreviewFailed(
		"019f0000-0000-7000-8000-000000000018", "019f0000-0000-7000-8000-000000000005",
		"019f0000-0000-7000-8000-000000000014", failed, 1, now,
	); err == nil {
		t.Fatal("Tool failed Card 接受未批准 result_code")
	}
}
