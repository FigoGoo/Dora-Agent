package workspace

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/FigoGoo/Dora-Agent/agent/internal/event"
	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/analyzematerials"
	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/plancreationspec"
	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/planstoryboard"
	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/writeprompts"
	"github.com/FigoGoo/Dora-Agent/agent/internal/session"
	"github.com/google/uuid"
)

const (
	maxJavaScriptSafeInteger  = int64(1<<53 - 1)
	maxTurnOutputSummaryBytes = 2000
)

var stableTurnErrorCodePattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)
var sha256DigestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Service 编排一致性 Snapshot 解密与持久 EventLog 强类型投影，不持有 HTTP 或 Redis 状态。
type Service struct {
	repository Repository
	decryptor  ContentDecryptor
	limits     SnapshotLimits
	eventBytes int
}

// NewService 校验 Repository、Decryptor 和所有有界读取参数后创建只读 Workspace 用例。
func NewService(repository Repository, decryptor ContentDecryptor, limits SnapshotLimits, maxEventBytes int) (*Service, error) {
	if repository == nil || decryptor == nil || limits.MaxMessages <= 0 || limits.MaxInputs <= 0 ||
		limits.MaxMediaPreviews < 0 || limits.MaxMediaPreviews > 16 || maxEventBytes <= 0 {
		return nil, fmt.Errorf("create Workspace service: invalid dependency or limits")
	}
	return &Service{repository: repository, decryptor: decryptor, limits: limits, eventBytes: maxEventBytes}, nil
}

// LoadSnapshot 在 Repository 一致性事务完成后逐条认证解密内存记录，并原子构造非 null 数组 DTO。
// 任一正文失败会丢弃整个结果，禁止跳过消息或返回占位内容。
func (s *Service) LoadSnapshot(ctx context.Context, identity Identity, requestID string) (Snapshot, error) {
	record, err := s.repository.LoadSnapshot(ctx, identity, s.limits)
	if err != nil {
		return Snapshot{}, err
	}
	if record.Session.ID != identity.SessionID || record.Session.UserID != identity.UserID || record.Session.ProjectID != identity.ProjectID {
		return Snapshot{}, ErrNotFound
	}
	if !validSnapshotBounds(record) || !isCanonicalUUIDv7(record.Session.ID) || !isCanonicalUUIDv7(record.Session.ProjectID) ||
		!isCanonicalUUIDv7(record.Session.UserID) {
		// 身份不匹配才折叠为 404；损坏的权威状态必须以 503 暴露给运维，不能伪装为资源不存在。
		return Snapshot{}, ErrPersistence
	}
	messages := make([]MessageDTO, 0, len(record.Messages))
	messageIDs := make(map[string]struct{}, len(record.Messages))
	previousMessageSeq := int64(0)
	for _, message := range record.Messages {
		if message.SessionID != identity.SessionID || !isCanonicalUUIDv7(message.ID) ||
			message.Seq != previousMessageSeq+1 || message.Seq > maxJavaScriptSafeInteger ||
			message.Role != string(session.MessageRoleUser) || message.CreatedAt.IsZero() {
			return Snapshot{}, ErrPersistence
		}
		plaintext, openErr := s.decryptor.Open(ctx, message.Content, message.ContentDigest)
		if openErr != nil {
			if errors.Is(openErr, context.Canceled) || errors.Is(openErr, context.DeadlineExceeded) {
				return Snapshot{}, openErr
			}
			return Snapshot{}, ErrContentUnavailable
		}
		messages = append(messages, MessageDTO{
			ID: message.ID, MessageSeq: message.Seq, Role: message.Role,
			Content: string(plaintext), CreatedAt: message.CreatedAt.UTC(),
		})
		messageIDs[message.ID] = struct{}{}
		previousMessageSeq = message.Seq
	}
	inputs := make([]InputDTO, 0, len(record.Inputs))
	previousInputSeq := int64(0)
	for _, input := range record.Inputs {
		if input.SessionID != identity.SessionID || !isCanonicalUUIDv7(input.ID) ||
			input.EnqueueSeq != previousInputSeq+1 || input.EnqueueSeq > maxJavaScriptSafeInteger ||
			!validInputSource(input.SourceType) || !validInputStatus(input.Status) ||
			input.AvailableAt.IsZero() || input.CreatedAt.IsZero() || input.UpdatedAt.IsZero() {
			return Snapshot{}, ErrPersistence
		}
		var messageID *string
		if input.SourceType == string(session.InputSourceTypeAnalyzeMaterialsPreview) ||
			input.SourceType == string(session.InputSourceTypePlanStoryboardPreview) ||
			input.SourceType == string(session.InputSourceTypeWritePromptsPreview) ||
			input.SourceType == string(session.InputSourceTypeGenerateMediaPreviewRequest) ||
			input.SourceType == string(session.InputSourceTypeAssembleOutputPreviewRequest) ||
			input.SourceType == string(session.InputSourceTypeMediaJobPreviewTerminal) {
			if input.MessageID != nil {
				return Snapshot{}, ErrPersistence
			}
		} else {
			if input.MessageID == nil || !isCanonicalUUIDv7(*input.MessageID) {
				return Snapshot{}, ErrPersistence
			}
			if _, exists := messageIDs[*input.MessageID]; !exists {
				return Snapshot{}, ErrPersistence
			}
			value := *input.MessageID
			messageID = &value
		}
		inputs = append(inputs, InputDTO{
			ID: input.ID, MessageID: messageID, SourceType: input.SourceType, Status: input.Status,
			EnqueueSeq: input.EnqueueSeq, AvailableAt: input.AvailableAt.UTC(),
			CreatedAt: input.CreatedAt.UTC(), UpdatedAt: input.UpdatedAt.UTC(),
		})
		previousInputSeq = input.EnqueueSeq
	}
	var preview *plancreationspec.Card
	if record.CreationSpecPreview != nil {
		mapped, mapErr := mapCreationSpecPreview(*record.CreationSpecPreview, identity)
		if mapErr != nil {
			return Snapshot{}, ErrPersistence
		}
		preview = &mapped
	}
	var latestTurnOutput *TurnOutputDTO
	if record.LatestTurnOutput != nil {
		mapped, mapErr := mapTurnOutput(*record.LatestTurnOutput, identity, inputSequences(record.Inputs))
		if mapErr != nil {
			return Snapshot{}, ErrPersistence
		}
		latestTurnOutput = &mapped
	}
	var analyzeMaterialsPreview *event.AnalyzeMaterialsPreviewCardPayload
	if record.AnalyzeMaterialsPreview != nil {
		mapped, mapErr := mapAnalyzeMaterialsPreview(
			*record.AnalyzeMaterialsPreview,
			identity,
			inputSequences(record.Inputs),
		)
		if mapErr != nil {
			return Snapshot{}, ErrPersistence
		}
		analyzeMaterialsPreview = &mapped
	}
	var planStoryboardPreview *event.PlanStoryboardPreviewCardPayload
	if record.PlanStoryboardPreview != nil {
		mapped, mapErr := mapPlanStoryboardPreview(
			*record.PlanStoryboardPreview,
			identity,
			inputSequences(record.Inputs),
		)
		if mapErr != nil {
			return Snapshot{}, ErrPersistence
		}
		planStoryboardPreview = &mapped
	}
	var writePromptsPreview *event.WritePromptsPreviewCardPayload
	if record.WritePromptsPreview != nil {
		mapped, mapErr := mapWritePromptsPreview(
			*record.WritePromptsPreview,
			identity,
			inputSequences(record.Inputs),
		)
		if mapErr != nil {
			return Snapshot{}, ErrPersistence
		}
		writePromptsPreview = &mapped
	}
	mediaPreviews := make([]event.MediaPreviewCardPayload, 0, len(record.MediaPreviews))
	if len(record.MediaPreviews) > s.limits.MaxMediaPreviews {
		return Snapshot{}, ErrPersistence
	}
	previousMediaSeq := int64(0)
	for _, persisted := range record.MediaPreviews {
		if previousMediaSeq != 0 && persisted.Seq <= previousMediaSeq {
			return Snapshot{}, ErrPersistence
		}
		mapped, mapErr := mapMediaPreview(persisted, identity)
		if mapErr != nil {
			return Snapshot{}, ErrPersistence
		}
		mediaPreviews = append(mediaPreviews, mapped)
		previousMediaSeq = persisted.Seq
	}
	schemaVersion := SnapshotSchemaVersionV4
	if s.limits.MaxMediaPreviews > 0 {
		schemaVersion = SnapshotSchemaVersionV5
	}
	return Snapshot{
		SchemaVersion: schemaVersion, RequestID: requestID,
		Session: SessionDTO{
			ID: record.Session.ID, ProjectID: record.Session.ProjectID, Status: record.Session.Status,
			Version: record.Session.Version, CreatedAt: record.Session.CreatedAt.UTC(), UpdatedAt: record.Session.UpdatedAt.UTC(),
		},
		Messages: messages, Inputs: inputs, CreationSpecPreview: preview, LatestTurnOutput: latestTurnOutput,
		AnalyzeMaterialsPreview: analyzeMaterialsPreview,
		PlanStoryboardPreview:   planStoryboardPreview,
		WritePromptsPreview:     writePromptsPreview,
		MediaPreviews:           mediaPreviews,
		EventHighWatermark:      record.EventHighWatermark,
		MinAvailableSeq:         record.MinAvailableSeq,
	}, nil
}

func inputSequences(inputs []InputRecord) map[string]int64 {
	sequences := make(map[string]int64, len(inputs))
	for _, input := range inputs {
		sequences[input.ID] = input.EnqueueSeq
	}
	return sequences
}

func mapTurnOutput(record TurnOutputRecord, identity Identity, inputSequences map[string]int64) (TurnOutputDTO, error) {
	if record.SessionID != identity.SessionID || !isCanonicalUUIDv7(record.SessionID) ||
		!isCanonicalUUIDv7(record.TurnID) || !isCanonicalUUIDv7(record.RunID) || !isCanonicalUUIDv7(record.SourceInputID) ||
		record.SourceEnqueueSeq <= 0 || record.SourceEnqueueSeq > maxJavaScriptSafeInteger ||
		record.ProjectionVersion <= 0 || record.ProjectionVersion > maxJavaScriptSafeInteger || record.UpdatedAt.IsZero() ||
		len(record.PayloadJSON) == 0 || len(record.PayloadJSON) > maxTurnOutputSummaryBytes*2 {
		return TurnOutputDTO{}, ErrProjectionInvalid
	}
	if inputSequences[record.SourceInputID] != record.SourceEnqueueSeq {
		return TurnOutputDTO{}, ErrProjectionInvalid
	}
	switch record.SchemaVersion {
	case event.DirectResponseCardSchemaVersionV1:
		var payload event.SessionTurnDirectResponsePayload
		if err := strictDecode(record.PayloadJSON, &payload); err != nil || !validDirectResponsePayload(payload) ||
			record.Status != event.DirectResponseCompletedStatus || payload.Status != record.Status ||
			payload.TurnID != record.TurnID || payload.RunID != record.RunID || payload.InputID != record.SourceInputID {
			return TurnOutputDTO{}, ErrProjectionInvalid
		}
		return TurnOutputDTO{
			SchemaVersion: payload.SchemaVersion, TurnID: payload.TurnID, RunID: payload.RunID, InputID: payload.InputID,
			Status: payload.Status, MessageCode: payload.MessageCode, Summary: payload.Summary,
			AvailableActions: append([]string(nil), payload.AvailableActions...),
		}, nil
	case event.FailureCardSchemaVersionV1:
		var payload event.SessionTurnFailurePayload
		if err := strictDecode(record.PayloadJSON, &payload); err != nil || !validFailurePayload(payload, record.Status) ||
			(record.Status != event.TurnFailedStatus && record.Status != event.TurnRecoveryPendingStatus) ||
			payload.TurnID != record.TurnID || payload.RunID != record.RunID || payload.InputID != record.SourceInputID {
			return TurnOutputDTO{}, ErrProjectionInvalid
		}
		return TurnOutputDTO{
			SchemaVersion: payload.SchemaVersion, TurnID: payload.TurnID, RunID: payload.RunID, InputID: payload.InputID,
			Status: payload.Status, ErrorCode: payload.ErrorCode, Retryable: payload.Retryable, Summary: payload.Summary,
		}, nil
	default:
		return TurnOutputDTO{}, ErrProjectionInvalid
	}
}

func validTurnSummary(value string) bool {
	return value != "" && utf8.ValidString(value) && len([]byte(value)) <= maxTurnOutputSummaryBytes
}

// LoadEventBatch 从 PostgreSQL 权威日志加载有界批次，校验 Cursor、水位、连续 Seq 和每条强类型投影。
func (s *Service) LoadEventBatch(ctx context.Context, identity Identity, cursor int64, limit int) (EventBatch, error) {
	if cursor < 0 || cursor > maxJavaScriptSafeInteger || limit <= 0 {
		return EventBatch{}, ErrInvalidCursor
	}
	record, err := s.repository.LoadEventBatch(ctx, identity, cursor, limit)
	if err != nil {
		return EventBatch{}, err
	}
	if record.LastSeq < 0 || record.LastSeq > maxJavaScriptSafeInteger || record.MinAvailableSeq <= 0 ||
		record.MinAvailableSeq > record.LastSeq+1 || len(record.Events) > limit {
		return EventBatch{LatestSeq: record.LastSeq, MinAvailableSeq: record.MinAvailableSeq}, ErrProjectionInvalid
	}
	if cursor > record.LastSeq {
		return EventBatch{}, ErrInvalidCursor
	}
	if cursor < record.MinAvailableSeq {
		return EventBatch{LatestSeq: record.LastSeq, MinAvailableSeq: record.MinAvailableSeq}, ErrCursorExpired
	}
	projected := make([]ProjectedEvent, 0, len(record.Events))
	expectedSeq := cursor + 1
	for _, persisted := range record.Events {
		if persisted.Seq != expectedSeq || persisted.Seq > record.LastSeq {
			return EventBatch{LatestSeq: record.LastSeq, MinAvailableSeq: record.MinAvailableSeq}, ErrEventGap
		}
		eventDTO, projectErr := projectEvent(persisted, identity, s.eventBytes)
		if projectErr != nil {
			return EventBatch{LatestSeq: record.LastSeq, MinAvailableSeq: record.MinAvailableSeq}, projectErr
		}
		projected = append(projected, eventDTO)
		expectedSeq++
	}
	if len(record.Events) == 0 && cursor < record.LastSeq {
		// 高水位声明仍有未读事件却返回空批，若错误发送 Ready 会造成永久丢事件或忙循环，必须 Reset。
		return EventBatch{LatestSeq: record.LastSeq, MinAvailableSeq: record.MinAvailableSeq}, ErrEventGap
	}
	return EventBatch{LatestSeq: record.LastSeq, MinAvailableSeq: record.MinAvailableSeq, Events: projected}, nil
}

// validSnapshotBounds 校验 Snapshot 的生命周期、时间和 JavaScript 精度边界。
func validSnapshotBounds(record SnapshotRecord) bool {
	return (record.Session.Status == string(session.StatusActive) || record.Session.Status == string(session.StatusArchived)) &&
		record.Session.Version > 0 && record.Session.Version <= maxJavaScriptSafeInteger &&
		!record.Session.CreatedAt.IsZero() && !record.Session.UpdatedAt.IsZero() &&
		record.EventHighWatermark >= 0 && record.EventHighWatermark <= maxJavaScriptSafeInteger &&
		record.MinAvailableSeq > 0 && record.MinAvailableSeq <= record.EventHighWatermark+1
}

// validInputStatus 固定当前 Migration 已允许的 Session Input 状态集合。
func validInputStatus(value string) bool {
	switch session.InputStatus(value) {
	case session.InputStatusPending, session.InputStatusClaimed, session.InputStatusRunning,
		session.InputStatusRetryWait, session.InputStatusRecoveryPending, session.InputStatusResolved, session.InputStatusDead:
		return true
	default:
		return false
	}
}

func validInputSource(value string) bool {
	return value == string(session.InputSourceTypeUserMessage) || value == string(session.InputSourceTypeCreationSpecPreview) ||
		value == string(session.InputSourceTypeAnalyzeMaterialsPreview) ||
		value == string(session.InputSourceTypePlanStoryboardPreview) ||
		value == string(session.InputSourceTypeWritePromptsPreview) ||
		value == string(session.InputSourceTypeGenerateMediaPreviewRequest) ||
		value == string(session.InputSourceTypeAssembleOutputPreviewRequest) ||
		value == string(session.InputSourceTypeMediaJobPreviewTerminal)
}

func mapMediaPreview(record MediaPreviewRecord, identity Identity) (event.MediaPreviewCardPayload, error) {
	if record.SessionID != identity.SessionID || !isCanonicalUUIDv7(record.EventID) ||
		!isCanonicalUUIDv7(record.SessionID) || !isCanonicalUUIDv7(record.AggregateID) || record.Seq <= 0 ||
		record.AggregateType != string(event.AggregateTypeSessionInput) || record.AggregateVersion != 1 ||
		record.CreatedAt.IsZero() || len(record.PayloadJSON) == 0 || len(record.PayloadJSON) > 64*1024 {
		return event.MediaPreviewCardPayload{}, ErrProjectionInvalid
	}
	card, err := decodeMediaPreviewCard(record.PayloadJSON)
	if err != nil || !isCanonicalUUIDv7(card.InputID) || !isCanonicalUUIDv7(card.TurnID) ||
		!isCanonicalUUIDv7(card.RunID) || !isCanonicalUUIDv7(card.ToolCallID) || card.UpdatedAt.IsZero() ||
		card.UpdatedAt.Location() != time.UTC {
		return event.MediaPreviewCardPayload{}, ErrProjectionInvalid
	}
	if (event.Type(record.EventType) == event.TypeMediaPreviewAccepted ||
		(event.Type(record.EventType) == event.TypeMediaPreviewFailed && card.JobID == "")) && card.InputID != record.AggregateID {
		return event.MediaPreviewCardPayload{}, ErrProjectionInvalid
	}
	var validated event.Record
	switch event.Type(record.EventType) {
	case event.TypeMediaPreviewAccepted:
		validated, err = event.NewMediaPreviewAccepted(record.EventID, record.SessionID, record.EventID, record.AggregateID, card, record.CreatedAt)
	case event.TypeMediaPreviewCompleted:
		validated, err = event.NewMediaPreviewCompleted(record.EventID, record.SessionID, record.EventID, record.AggregateID, card, record.CreatedAt)
		if err == nil && !strings.HasPrefix(card.ContentURL, "/api/v1/projects/"+identity.ProjectID+"/") {
			err = ErrProjectionInvalid
		}
	case event.TypeMediaPreviewFailed:
		validated, err = event.NewMediaPreviewFailed(record.EventID, record.SessionID, record.EventID, record.AggregateID, card, record.CreatedAt)
	case event.TypeMediaPreviewRuntimeFailed:
		validated, err = event.NewMediaPreviewRuntimeFailed(record.EventID, record.SessionID, record.EventID, record.AggregateID, card, record.CreatedAt)
	default:
		return event.MediaPreviewCardPayload{}, ErrProjectionInvalid
	}
	if err != nil || validated.Type != event.Type(record.EventType) {
		return event.MediaPreviewCardPayload{}, ErrProjectionInvalid
	}
	return card, nil
}

func decodeMediaPreviewCard(data []byte) (event.MediaPreviewCardPayload, error) {
	if err := rejectDuplicateJSONFields(data); err != nil {
		return event.MediaPreviewCardPayload{}, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return event.MediaPreviewCardPayload{}, err
	}
	var status string
	if err := json.Unmarshal(fields["status"], &status); err != nil {
		return event.MediaPreviewCardPayload{}, err
	}
	common := []string{"schema_version", "input_id", "turn_id", "run_id", "tool_call_id", "tool_key", "status", "result_code", "updated_at"}
	expected := append([]string(nil), common...)
	switch status {
	case "accepted":
		expected = append(expected, "operation_id", "batch_id", "asset_ref")
	case "completed":
		expected = append(expected, "operation_id", "batch_id", "job_id", "asset_ref", "content_url")
	case "failed":
		expected = append(expected, "error_code")
		if _, terminal := fields["job_id"]; terminal {
			expected = append(expected, "operation_id", "batch_id", "job_id", "asset_ref")
		}
	default:
		return event.MediaPreviewCardPayload{}, ErrProjectionInvalid
	}
	if len(fields) != len(expected) {
		return event.MediaPreviewCardPayload{}, ErrProjectionInvalid
	}
	for _, name := range expected {
		if _, ok := fields[name]; !ok {
			return event.MediaPreviewCardPayload{}, ErrProjectionInvalid
		}
	}
	var card event.MediaPreviewCardPayload
	if err := strictDecode(data, &card); err != nil {
		return event.MediaPreviewCardPayload{}, err
	}
	return card, nil
}

func mapWritePromptsPreview(
	record WritePromptsPreviewRecord,
	identity Identity,
	inputSequences map[string]int64,
) (event.WritePromptsPreviewCardPayload, error) {
	if record.SessionID != identity.SessionID || !isCanonicalUUIDv7(record.SessionID) ||
		!isCanonicalUUIDv7(record.SourceInputID) || !isCanonicalUUIDv7(record.TurnID) ||
		!isCanonicalUUIDv7(record.RunID) || !isCanonicalUUIDv7(record.ToolCallID) || record.SourceEnqueueSeq <= 0 ||
		record.SourceEnqueueSeq > maxJavaScriptSafeInteger || record.AggregateVersion != 1 ||
		record.CreatedAt.IsZero() || len(record.PayloadJSON) == 0 || len(record.PayloadJSON) > 128*1024 ||
		inputSequences[record.SourceInputID] != record.SourceEnqueueSeq {
		return event.WritePromptsPreviewCardPayload{}, ErrProjectionInvalid
	}
	expectedStatus, expectedFailureKind, exists := writePromptsEventOutcome(record.EventType)
	if !exists {
		return event.WritePromptsPreviewCardPayload{}, ErrProjectionInvalid
	}
	card, err := decodeWritePromptsPreviewCard(record.PayloadJSON)
	if err != nil || card.InputID != record.SourceInputID || card.TurnID != record.TurnID ||
		card.RunID != record.RunID || card.ToolCallID != record.ToolCallID || card.Status != expectedStatus ||
		card.FailureKind != expectedFailureKind || !validWritePromptsPreviewCard(card) ||
		(card.Status == "completed" && card.ProjectID != identity.ProjectID) {
		return event.WritePromptsPreviewCardPayload{}, ErrProjectionInvalid
	}
	return card, nil
}

func writePromptsEventOutcome(eventType string) (string, string, bool) {
	switch event.Type(eventType) {
	case event.TypeWritePromptsPreviewCompleted:
		return "completed", "", true
	case event.TypeWritePromptsPreviewFailed:
		return "failed", event.WritePromptsPreviewFailureKindTool, true
	case event.TypeWritePromptsPreviewRuntimeFailed:
		return "failed", event.WritePromptsPreviewFailureKindRuntime, true
	default:
		return "", "", false
	}
}

func decodeWritePromptsPreviewCard(data []byte) (event.WritePromptsPreviewCardPayload, error) {
	if err := rejectDuplicateJSONFields(data); err != nil {
		return event.WritePromptsPreviewCardPayload{}, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return event.WritePromptsPreviewCardPayload{}, err
	}
	var status string
	if err := json.Unmarshal(fields["status"], &status); err != nil {
		return event.WritePromptsPreviewCardPayload{}, err
	}
	common := []string{
		"schema_version", "input_id", "turn_id", "run_id", "tool_call_id", "status", "result_code", "updated_at",
	}
	var expected []string
	switch status {
	case "completed":
		expected = append(common,
			"prompt_preview_id", "project_id", "storyboard_preview_ref", "version", "content_digest",
			"target_count", "prompts",
		)
	case "failed":
		expected = append(common, "failure_kind", "summary", "retryable")
	default:
		return event.WritePromptsPreviewCardPayload{}, ErrProjectionInvalid
	}
	if len(fields) != len(expected) {
		return event.WritePromptsPreviewCardPayload{}, ErrProjectionInvalid
	}
	for _, field := range expected {
		if _, exists := fields[field]; !exists {
			return event.WritePromptsPreviewCardPayload{}, ErrProjectionInvalid
		}
	}
	var card event.WritePromptsPreviewCardPayload
	if err := strictDecode(data, &card); err != nil {
		return event.WritePromptsPreviewCardPayload{}, err
	}
	return card, nil
}

func validWritePromptsPreviewCard(card event.WritePromptsPreviewCardPayload) bool {
	if card.SchemaVersion != event.WritePromptsPreviewCardSchemaVersionV1 ||
		!isCanonicalUUIDv7(card.InputID) || !isCanonicalUUIDv7(card.TurnID) ||
		!isCanonicalUUIDv7(card.RunID) || !isCanonicalUUIDv7(card.ToolCallID) ||
		card.UpdatedAt.IsZero() || card.UpdatedAt.Location() != time.UTC {
		return false
	}
	if card.Status == "completed" || card.FailureKind == event.WritePromptsPreviewFailureKindTool {
		return writeprompts.ValidateCard(card) == nil
	}
	return card.Status == "failed" && card.FailureKind == event.WritePromptsPreviewFailureKindRuntime &&
		card.ResultCode == "WRITE_PROMPTS_RUNTIME_FAILED" && validTurnSummary(card.Summary) && card.Retryable != nil &&
		card.PromptPreviewID == "" && card.ProjectID == "" && card.StoryboardPreviewRef == nil && card.Version == 0 &&
		card.ContentDigest == "" && card.TargetCount == 0 && len(card.Prompts) == 0
}

func mapPlanStoryboardPreview(
	record PlanStoryboardPreviewRecord,
	identity Identity,
	inputSequences map[string]int64,
) (event.PlanStoryboardPreviewCardPayload, error) {
	if record.SessionID != identity.SessionID || !isCanonicalUUIDv7(record.SessionID) ||
		!isCanonicalUUIDv7(record.SourceInputID) || !isCanonicalUUIDv7(record.TurnID) ||
		!isCanonicalUUIDv7(record.RunID) || !isCanonicalUUIDv7(record.ToolCallID) || record.SourceEnqueueSeq <= 0 ||
		record.SourceEnqueueSeq > maxJavaScriptSafeInteger || record.AggregateVersion != 1 ||
		record.CreatedAt.IsZero() || len(record.PayloadJSON) == 0 || len(record.PayloadJSON) > 64*1024 ||
		inputSequences[record.SourceInputID] != record.SourceEnqueueSeq {
		return event.PlanStoryboardPreviewCardPayload{}, ErrProjectionInvalid
	}
	expectedStatus, expectedFailureKind, exists := planStoryboardEventOutcome(record.EventType)
	if !exists {
		return event.PlanStoryboardPreviewCardPayload{}, ErrProjectionInvalid
	}
	card, err := decodePlanStoryboardPreviewCard(record.PayloadJSON)
	if err != nil || card.InputID != record.SourceInputID || card.TurnID != record.TurnID ||
		card.RunID != record.RunID || card.ToolCallID != record.ToolCallID || card.Status != expectedStatus ||
		card.FailureKind != expectedFailureKind || !validPlanStoryboardPreviewCard(card) ||
		(card.Status == "completed" && card.ProjectID != identity.ProjectID) {
		return event.PlanStoryboardPreviewCardPayload{}, ErrProjectionInvalid
	}
	return card, nil
}

func planStoryboardEventOutcome(eventType string) (string, string, bool) {
	switch event.Type(eventType) {
	case event.TypePlanStoryboardPreviewCompleted:
		return "completed", "", true
	case event.TypePlanStoryboardPreviewFailed:
		return "failed", event.PlanStoryboardPreviewFailureKindTool, true
	case event.TypePlanStoryboardPreviewRuntimeFailed:
		return "failed", event.PlanStoryboardPreviewFailureKindRuntime, true
	default:
		return "", "", false
	}
}

func decodePlanStoryboardPreviewCard(data []byte) (event.PlanStoryboardPreviewCardPayload, error) {
	if err := rejectDuplicateJSONFields(data); err != nil {
		return event.PlanStoryboardPreviewCardPayload{}, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return event.PlanStoryboardPreviewCardPayload{}, err
	}
	var status string
	if err := json.Unmarshal(fields["status"], &status); err != nil {
		return event.PlanStoryboardPreviewCardPayload{}, err
	}
	common := []string{
		"schema_version", "input_id", "turn_id", "run_id", "tool_call_id", "status", "result_code", "updated_at",
	}
	var expected []string
	switch status {
	case "completed":
		expected = append(common,
			"storyboard_preview_id", "project_id", "creation_spec_ref", "version", "content_digest",
			"title", "summary", "sections", "elements", "slots",
		)
	case "failed":
		expected = append(common, "failure_kind", "summary", "retryable")
	default:
		return event.PlanStoryboardPreviewCardPayload{}, ErrProjectionInvalid
	}
	if len(fields) != len(expected) {
		return event.PlanStoryboardPreviewCardPayload{}, ErrProjectionInvalid
	}
	for _, field := range expected {
		if _, exists := fields[field]; !exists {
			return event.PlanStoryboardPreviewCardPayload{}, ErrProjectionInvalid
		}
	}
	var card event.PlanStoryboardPreviewCardPayload
	if err := strictDecode(data, &card); err != nil {
		return event.PlanStoryboardPreviewCardPayload{}, err
	}
	return card, nil
}

func validPlanStoryboardPreviewCard(card event.PlanStoryboardPreviewCardPayload) bool {
	if card.SchemaVersion != event.PlanStoryboardPreviewCardSchemaVersionV1 ||
		!isCanonicalUUIDv7(card.InputID) || !isCanonicalUUIDv7(card.TurnID) ||
		!isCanonicalUUIDv7(card.RunID) || !isCanonicalUUIDv7(card.ToolCallID) ||
		card.UpdatedAt.IsZero() || card.UpdatedAt.Location() != time.UTC {
		return false
	}
	if card.Status == "completed" {
		if card.ResultCode != planstoryboard.ResultCodeCompleted || card.FailureKind != "" || card.Retryable != nil ||
			card.CreationSpecRef == nil || card.Sections == nil || card.Elements == nil || card.Slots == nil {
			return false
		}
		draft := planstoryboard.Card{
			SchemaVersion: planstoryboard.CardSchemaVersion, StoryboardPreviewID: card.StoryboardPreviewID,
			ProjectID: card.ProjectID, CreationSpecRef: *card.CreationSpecRef, Version: card.Version, Status: "draft",
			ContentDigest: card.ContentDigest, Title: card.Title, Summary: card.Summary,
			Sections: *card.Sections, Elements: *card.Elements, Slots: *card.Slots, UpdatedAt: card.UpdatedAt,
		}
		return planstoryboard.ValidateCard(draft) == nil
	}
	return card.Status == "failed" && validTurnSummary(card.Summary) &&
		validPlanStoryboardFailureCode(card.ResultCode, card.FailureKind) &&
		card.Retryable != nil && card.StoryboardPreviewID == "" && card.ProjectID == "" &&
		card.CreationSpecRef == nil && card.Version == 0 && card.ContentDigest == "" && card.Title == "" &&
		card.Sections == nil && card.Elements == nil && card.Slots == nil
}

func validPlanStoryboardFailureCode(code, failureKind string) bool {
	if failureKind == event.PlanStoryboardPreviewFailureKindRuntime {
		return code == "PLAN_STORYBOARD_RUNTIME_FAILED"
	}
	if failureKind != event.PlanStoryboardPreviewFailureKindTool {
		return false
	}
	switch code {
	case planstoryboard.ResultCodeInvalidArgument,
		planstoryboard.ResultCodeCreationSpecNotFound,
		planstoryboard.ResultCodeCreationSpecConflict,
		planstoryboard.ResultCodeCandidateInvalid,
		planstoryboard.ResultCodeDependencyInvalid,
		planstoryboard.ResultCodeBusinessConflict,
		planstoryboard.ResultCodeBusinessDisabled,
		planstoryboard.ResultCodeInternal:
		return true
	default:
		return false
	}
}

func mapAnalyzeMaterialsPreview(
	record AnalyzeMaterialsPreviewRecord,
	identity Identity,
	inputSequences map[string]int64,
) (event.AnalyzeMaterialsPreviewCardPayload, error) {
	if record.SessionID != identity.SessionID || !isCanonicalUUIDv7(record.SessionID) ||
		!isCanonicalUUIDv7(record.SourceInputID) || !isCanonicalUUIDv7(record.TurnID) ||
		!isCanonicalUUIDv7(record.RunID) || !isCanonicalUUIDv7(record.ToolCallID) ||
		record.SourceEnqueueSeq <= 0 || record.SourceEnqueueSeq > maxJavaScriptSafeInteger ||
		record.SchemaVersion != event.AnalyzeMaterialsPreviewCardSchemaVersionV1 ||
		!sha256DigestPattern.MatchString(record.ResultDigest) ||
		record.ProjectionVersion != 1 || record.CreatedAt.IsZero() ||
		len(record.PayloadJSON) == 0 || len(record.PayloadJSON) > 64*1024 ||
		inputSequences[record.SourceInputID] != record.SourceEnqueueSeq {
		return event.AnalyzeMaterialsPreviewCardPayload{}, ErrProjectionInvalid
	}
	var card event.AnalyzeMaterialsPreviewCardPayload
	if err := strictDecode(record.PayloadJSON, &card); err != nil ||
		card.SchemaVersion != record.SchemaVersion || card.InputID != record.SourceInputID ||
		card.TurnID != record.TurnID || card.RunID != record.RunID || card.ToolCallID != record.ToolCallID ||
		card.Status != record.Status {
		return event.AnalyzeMaterialsPreviewCardPayload{}, ErrProjectionInvalid
	}
	if !validAnalyzeMaterialsPreviewCard(card, record.OutcomeKind) {
		return event.AnalyzeMaterialsPreviewCardPayload{}, ErrProjectionInvalid
	}
	return card, nil
}

func validAnalyzeMaterialsPreviewCard(card event.AnalyzeMaterialsPreviewCardPayload, outcomeKind string) bool {
	switch outcomeKind {
	case "tool_completed", "tool_partial", "tool_failed":
		expectedStatus := map[string]string{
			"tool_completed": "completed", "tool_partial": "partial", "tool_failed": "failed",
		}[outcomeKind]
		expectedFailureKind := ""
		if outcomeKind == "tool_failed" {
			expectedFailureKind = event.AnalyzeMaterialsPreviewFailureKindTool
		}
		if card.Status != expectedStatus || card.FailureKind != expectedFailureKind {
			return false
		}
		result := analyzematerials.Result{
			SchemaVersion: analyzematerials.ResultSchemaVersion, Status: card.Status, ResultCode: card.ResultCode,
			Analysis: card.Analysis, Coverage: card.Coverage, EvidenceRefs: card.EvidenceRefs,
			InvocationRef: analyzematerials.InvocationRef{ToolCallID: card.ToolCallID},
			Summary:       card.Summary, Retryable: card.Retryable,
		}
		if analyzematerials.ValidateResult(result) != nil {
			return false
		}
	case "runtime_failed":
		if card.Status != "failed" || card.FailureKind != event.AnalyzeMaterialsPreviewFailureKindRuntime ||
			!stableTurnErrorCodePattern.MatchString(card.ResultCode) || card.Analysis != nil || card.Coverage != nil ||
			len(card.EvidenceRefs) != 0 || card.Retryable == nil || !validTurnSummary(card.Summary) {
			return false
		}
	default:
		return false
	}
	return true
}

func mapCreationSpecPreview(record CreationSpecPreviewRecord, identity Identity) (plancreationspec.Card, error) {
	var phases []plancreationspec.Phase
	var constraints []string
	var acceptance []string
	if err := strictDecode(record.PhasesJSON, &phases); err != nil || phases == nil {
		return plancreationspec.Card{}, ErrProjectionInvalid
	}
	if err := strictDecode(record.ConstraintsJSON, &constraints); err != nil || constraints == nil {
		return plancreationspec.Card{}, ErrProjectionInvalid
	}
	if err := strictDecode(record.AcceptanceJSON, &acceptance); err != nil || acceptance == nil {
		return plancreationspec.Card{}, ErrProjectionInvalid
	}
	audience := ""
	if record.Audience != nil {
		audience = *record.Audience
	}
	card := plancreationspec.Card{
		SchemaVersion: record.SchemaVersion, CreationSpecID: record.CreationSpecID,
		ProjectID: record.ProjectID, Version: record.Version, Status: record.Status,
		ContentDigest: record.ContentDigest, Title: record.Title, Goal: record.Goal,
		DeliverableType: record.DeliverableType, Audience: audience, Locale: record.Locale,
		Phases: phases, Constraints: constraints, AcceptanceCriteria: acceptance, UpdatedAt: record.UpdatedAt.UTC(),
	}
	if record.ProjectID != identity.ProjectID || plancreationspec.ValidateCard(card) != nil {
		return plancreationspec.Card{}, ErrProjectionInvalid
	}
	return card, nil
}

// eventEnvelopeWire 是持久 Event 对外 JSON 的固定字段顺序 DTO。
type eventEnvelopeWire struct {
	// SchemaVersion 固定为 workspace.event.v1。
	SchemaVersion string `json:"schema_version"`
	// PayloadSchemaVersion 固定为 session.event.v1。
	PayloadSchemaVersion string `json:"payload_schema_version"`
	// EventID 是持久 Event UUIDv7。
	EventID string `json:"event_id"`
	// SessionID 是所属 Session UUIDv7。
	SessionID string `json:"session_id"`
	// ProjectID 是绑定 Business Project UUIDv7。
	ProjectID string `json:"project_id"`
	// Seq 是 EventLog 单调序号。
	Seq int64 `json:"seq"`
	// Event 是冻结事件名。
	Event string `json:"event"`
	// OccurredAt 是事件冻结 UTC 时间。
	OccurredAt time.Time `json:"occurred_at"`
	// AggregateType 是事件关联聚合类型。
	AggregateType string `json:"aggregate_type"`
	// AggregateID 是关联聚合 UUIDv7。
	AggregateID string `json:"aggregate_id"`
	// AggregateVersion 是事件观察到的聚合版本。
	AggregateVersion int64 `json:"aggregate_version"`
	// Payload 是经过强类型解码和重新编码的安全载荷。
	Payload json.RawMessage `json:"payload"`
}

// projectEvent 对冻结白名单事件执行强类型解码、交叉绑定并重建安全 Envelope。
func projectEvent(record EventRecord, identity Identity, maxBytes int) (ProjectedEvent, error) {
	if !isCanonicalUUIDv7(record.EventID) || record.SessionID != identity.SessionID || record.Seq <= 0 ||
		record.Seq > maxJavaScriptSafeInteger || record.SchemaVersion != event.SchemaVersionV1 ||
		record.AggregateVersion <= 0 || record.AggregateVersion > maxJavaScriptSafeInteger || record.CreatedAt.IsZero() ||
		len(record.Payload) == 0 || len(record.Payload) > maxBytes {
		return ProjectedEvent{}, ErrProjectionInvalid
	}
	var normalizedPayload []byte
	switch event.Type(record.EventType) {
	case event.TypeSessionCreated:
		var payload event.SessionCreatedPayload
		if err := strictDecode(record.Payload, &payload); err != nil || !isCanonicalUUIDv7(payload.SessionID) ||
			!isCanonicalUUIDv7(payload.ProjectID) || payload.SessionID != identity.SessionID ||
			payload.ProjectID != identity.ProjectID || payload.Version != record.AggregateVersion ||
			payload.Status != string(session.StatusActive) ||
			record.AggregateType != string(event.AggregateTypeSession) || record.AggregateID != identity.SessionID {
			return ProjectedEvent{}, ErrProjectionInvalid
		}
		normalizedPayload, _ = json.Marshal(payload)
	case event.TypeSessionInputAccepted:
		var payload event.SessionInputAcceptedPayload
		if err := strictDecode(record.Payload, &payload); err != nil || !isCanonicalUUIDv7(payload.SessionID) ||
			!isCanonicalUUIDv7(payload.InputID) || !isCanonicalUUIDv7(payload.MessageID) ||
			payload.SessionID != identity.SessionID || payload.InputID != record.AggregateID ||
			payload.EnqueueSeq <= 0 || payload.EnqueueSeq > maxJavaScriptSafeInteger || payload.Status != string(session.InputStatusPending) ||
			record.AggregateType != string(event.AggregateTypeSessionInput) || record.AggregateVersion != 1 {
			return ProjectedEvent{}, ErrProjectionInvalid
		}
		normalizedPayload, _ = json.Marshal(payload)
	case event.TypeAnalyzeMaterialsPreviewAccepted:
		var payload event.AnalyzeMaterialsPreviewAcceptedPayload
		if err := strictDecode(record.Payload, &payload); err != nil ||
			!isCanonicalUUIDv7(payload.InputID) || !isCanonicalUUIDv7(payload.SessionID) ||
			!isCanonicalUUIDv7(payload.TurnID) || !isCanonicalUUIDv7(payload.RunID) ||
			!isCanonicalUUIDv7(payload.RequestID) || !isCanonicalUUIDv7(payload.ToolCallID) ||
			payload.SessionID != identity.SessionID || payload.InputID != record.AggregateID ||
			payload.SourceType != event.SourceKindAnalyzeMaterialsPreview ||
			!sha256DigestPattern.MatchString(payload.IntentDigest) || !sha256DigestPattern.MatchString(payload.ContextDigest) ||
			record.AggregateType != string(event.AggregateTypeSessionInput) || record.AggregateVersion != 1 {
			return ProjectedEvent{}, ErrProjectionInvalid
		}
		normalizedPayload, _ = json.Marshal(payload)
	case event.TypePlanStoryboardPreviewAccepted:
		var payload event.PlanStoryboardPreviewAcceptedPayload
		if err := strictDecode(record.Payload, &payload); err != nil ||
			payload.SchemaVersion != event.PlanStoryboardPreviewAcceptedSchemaVersionV1 ||
			!isCanonicalUUIDv7(payload.InputID) || !isCanonicalUUIDv7(payload.TurnID) ||
			!isCanonicalUUIDv7(payload.RunID) || !isCanonicalUUIDv7(payload.ToolCallID) ||
			!isCanonicalUUIDv7(payload.BusinessCommandID) || !isCanonicalUUIDv7(payload.CreationSpecID) ||
			payload.CreationSpecVersion != 1 || !sha256DigestPattern.MatchString(payload.IntentDigest) ||
			!sha256DigestPattern.MatchString(payload.ContextDigest) ||
			!sha256DigestPattern.MatchString(payload.CreationSpecContentDigest) ||
			payload.InputID != record.AggregateID ||
			record.AggregateType != string(event.AggregateTypePlanStoryboardPreview) || record.AggregateVersion != 1 {
			return ProjectedEvent{}, ErrProjectionInvalid
		}
		normalizedPayload, _ = json.Marshal(payload)
	case event.TypeWritePromptsPreviewAccepted:
		var payload event.WritePromptsPreviewAcceptedPayload
		if err := strictDecode(record.Payload, &payload); err != nil ||
			payload.SchemaVersion != event.WritePromptsPreviewAcceptedSchemaVersionV1 ||
			!isCanonicalUUIDv7(payload.InputID) || !isCanonicalUUIDv7(payload.TurnID) ||
			!isCanonicalUUIDv7(payload.RunID) || !isCanonicalUUIDv7(payload.ToolCallID) ||
			!isCanonicalUUIDv7(payload.BusinessCommandID) || !isCanonicalUUIDv7(payload.StoryboardPreviewID) ||
			payload.StoryboardPreviewVersion != 1 || !sha256DigestPattern.MatchString(payload.IntentDigest) ||
			!sha256DigestPattern.MatchString(payload.ContextDigest) ||
			!sha256DigestPattern.MatchString(payload.StoryboardPreviewContentDigest) ||
			payload.InputID != record.AggregateID ||
			record.AggregateType != string(event.AggregateTypeWritePromptsPreview) || record.AggregateVersion != 1 {
			return ProjectedEvent{}, ErrProjectionInvalid
		}
		normalizedPayload, _ = json.Marshal(payload)
	case event.TypeCreationSpecPreviewCompleted:
		var payload plancreationspec.Card
		if err := strictDecode(record.Payload, &payload); err != nil ||
			plancreationspec.ValidateCard(payload) != nil || payload.ProjectID != identity.ProjectID ||
			payload.CreationSpecID != record.AggregateID || payload.Version != record.AggregateVersion ||
			record.AggregateType != string(event.AggregateTypeCreationSpec) {
			return ProjectedEvent{}, ErrProjectionInvalid
		}
		normalizedPayload, _ = json.Marshal(payload)
	case event.TypeCreationSpecPreviewFailed:
		var payload event.CreationSpecPreviewFailedPayload
		if err := strictDecode(record.Payload, &payload); err != nil ||
			!isCanonicalUUIDv7(payload.InputID) || payload.InputID != record.AggregateID ||
			payload.ResultCode == "" || len(payload.ResultCode) > 64 || payload.Summary == "" || len(payload.Summary) > 2000 ||
			record.AggregateType != string(event.AggregateTypeSessionInput) || record.AggregateVersion != 1 {
			return ProjectedEvent{}, ErrProjectionInvalid
		}
		normalizedPayload, _ = json.Marshal(payload)
	case event.TypeSessionTurnCompleted:
		var payload event.SessionTurnDirectResponsePayload
		if err := strictDecode(record.Payload, &payload); err != nil ||
			!validDirectResponsePayload(payload) ||
			payload.TurnID != record.AggregateID ||
			record.AggregateType != string(event.AggregateTypeSessionTurn) {
			return ProjectedEvent{}, ErrProjectionInvalid
		}
		normalizedPayload, _ = json.Marshal(payload)
	case event.TypeSessionTurnFailed, event.TypeSessionTurnRecoveryPending:
		var payload event.SessionTurnFailurePayload
		expectedStatus := event.TurnFailedStatus
		if event.Type(record.EventType) == event.TypeSessionTurnRecoveryPending {
			expectedStatus = event.TurnRecoveryPendingStatus
		}
		if err := strictDecode(record.Payload, &payload); err != nil ||
			!validFailurePayload(payload, expectedStatus) ||
			payload.TurnID != record.AggregateID ||
			record.AggregateType != string(event.AggregateTypeSessionTurn) {
			return ProjectedEvent{}, ErrProjectionInvalid
		}
		normalizedPayload, _ = json.Marshal(payload)
	case event.TypeAnalyzeMaterialsPreviewCompleted, event.TypeAnalyzeMaterialsPreviewPartial,
		event.TypeAnalyzeMaterialsPreviewFailed, event.TypeAnalyzeMaterialsPreviewRuntimeFailed:
		var card event.AnalyzeMaterialsPreviewCardPayload
		if err := strictDecode(record.Payload, &card); err != nil ||
			card.TurnID != record.AggregateID ||
			record.AggregateType != string(event.AggregateTypeSessionTurn) || record.AggregateVersion != 1 {
			return ProjectedEvent{}, ErrProjectionInvalid
		}
		outcomeKind := map[event.Type]string{
			event.TypeAnalyzeMaterialsPreviewCompleted:     "tool_completed",
			event.TypeAnalyzeMaterialsPreviewPartial:       "tool_partial",
			event.TypeAnalyzeMaterialsPreviewFailed:        "tool_failed",
			event.TypeAnalyzeMaterialsPreviewRuntimeFailed: "runtime_failed",
		}[event.Type(record.EventType)]
		if card.SchemaVersion != event.AnalyzeMaterialsPreviewCardSchemaVersionV1 ||
			!isCanonicalUUIDv7(card.InputID) || !isCanonicalUUIDv7(card.TurnID) ||
			!isCanonicalUUIDv7(card.RunID) || !isCanonicalUUIDv7(card.ToolCallID) ||
			!validAnalyzeMaterialsPreviewCard(card, outcomeKind) {
			return ProjectedEvent{}, ErrProjectionInvalid
		}
		normalizedPayload, _ = json.Marshal(card)
	case event.TypePlanStoryboardPreviewCompleted, event.TypePlanStoryboardPreviewFailed,
		event.TypePlanStoryboardPreviewRuntimeFailed:
		card, err := decodePlanStoryboardPreviewCard(record.Payload)
		expectedStatus, expectedFailureKind, exists := planStoryboardEventOutcome(record.EventType)
		if err != nil || !exists || !isCanonicalUUIDv7(record.PlanTurnID) ||
			!isCanonicalUUIDv7(record.PlanRunID) || !isCanonicalUUIDv7(record.PlanToolCallID) ||
			card.InputID != record.AggregateID || card.TurnID != record.PlanTurnID ||
			card.RunID != record.PlanRunID || card.ToolCallID != record.PlanToolCallID || card.Status != expectedStatus ||
			card.FailureKind != expectedFailureKind ||
			record.AggregateType != string(event.AggregateTypePlanStoryboardPreview) || record.AggregateVersion != 1 ||
			!validPlanStoryboardPreviewCard(card) ||
			(card.Status == "completed" && card.ProjectID != identity.ProjectID) {
			return ProjectedEvent{}, ErrProjectionInvalid
		}
		normalizedPayload, _ = json.Marshal(card)
	case event.TypeWritePromptsPreviewCompleted, event.TypeWritePromptsPreviewFailed,
		event.TypeWritePromptsPreviewRuntimeFailed:
		card, err := decodeWritePromptsPreviewCard(record.Payload)
		expectedStatus, expectedFailureKind, exists := writePromptsEventOutcome(record.EventType)
		if err != nil || !exists || !isCanonicalUUIDv7(record.WriteTurnID) ||
			!isCanonicalUUIDv7(record.WriteRunID) || !isCanonicalUUIDv7(record.WriteToolCallID) ||
			card.InputID != record.AggregateID || card.TurnID != record.WriteTurnID ||
			card.RunID != record.WriteRunID || card.ToolCallID != record.WriteToolCallID || card.Status != expectedStatus ||
			card.FailureKind != expectedFailureKind ||
			record.AggregateType != string(event.AggregateTypeWritePromptsPreview) || record.AggregateVersion != 1 ||
			!validWritePromptsPreviewCard(card) ||
			(card.Status == "completed" && card.ProjectID != identity.ProjectID) {
			return ProjectedEvent{}, ErrProjectionInvalid
		}
		normalizedPayload, _ = json.Marshal(card)
	case event.TypeMediaPreviewAccepted, event.TypeMediaPreviewCompleted,
		event.TypeMediaPreviewFailed, event.TypeMediaPreviewRuntimeFailed:
		card, err := mapMediaPreview(MediaPreviewRecord{Seq: record.Seq, EventID: record.EventID,
			SessionID: record.SessionID, EventType: record.EventType, AggregateType: record.AggregateType,
			AggregateID: record.AggregateID, AggregateVersion: record.AggregateVersion,
			PayloadJSON: record.Payload, CreatedAt: record.CreatedAt}, identity)
		if err != nil {
			return ProjectedEvent{}, ErrProjectionInvalid
		}
		normalizedPayload, _ = json.Marshal(card)
	default:
		return ProjectedEvent{}, ErrProjectionInvalid
	}
	wire := eventEnvelopeWire{
		SchemaVersion: EventEnvelopeSchemaVersionV1, PayloadSchemaVersion: event.SchemaVersionV1,
		EventID: record.EventID, SessionID: identity.SessionID, ProjectID: identity.ProjectID,
		Seq: record.Seq, Event: record.EventType, OccurredAt: record.CreatedAt.UTC(),
		AggregateType: record.AggregateType, AggregateID: record.AggregateID,
		AggregateVersion: record.AggregateVersion, Payload: normalizedPayload,
	}
	data, err := json.Marshal(wire)
	if err != nil || len(data) > maxBytes || len(data) > math.MaxInt32 {
		return ProjectedEvent{}, ErrProjectionInvalid
	}
	return ProjectedEvent{Seq: record.Seq, Event: record.EventType, Data: data}, nil
}

func validDirectResponsePayload(payload event.SessionTurnDirectResponsePayload) bool {
	return payload.SchemaVersion == event.DirectResponseCardSchemaVersionV1 &&
		isCanonicalUUIDv7(payload.TurnID) && isCanonicalUUIDv7(payload.RunID) && isCanonicalUUIDv7(payload.InputID) &&
		payload.Status == event.DirectResponseCompletedStatus && payload.MessageCode == event.DirectResponseMessageCode &&
		payload.Summary == event.DirectResponseSummary && len(payload.AvailableActions) == 1 &&
		payload.AvailableActions[0] == event.DirectResponseActionOpenToolbox
}

func validFailurePayload(payload event.SessionTurnFailurePayload, expectedStatus string) bool {
	return payload.SchemaVersion == event.FailureCardSchemaVersionV1 &&
		isCanonicalUUIDv7(payload.TurnID) && isCanonicalUUIDv7(payload.RunID) && isCanonicalUUIDv7(payload.InputID) &&
		payload.Status == expectedStatus && stableTurnErrorCodePattern.MatchString(payload.ErrorCode) &&
		validTurnSummary(payload.Summary)
}

// isCanonicalUUIDv7 拒绝大小写、花括号和非 v7 UUID，确保前端标识只有一种编码。
func isCanonicalUUIDv7(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed.Version() == 7 && parsed.String() == value
}

// strictDecode 拒绝未知字段、多个 JSON 值和非强类型 Payload。
func strictDecode(data []byte, target any) error {
	if err := rejectDuplicateJSONFields(data); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("payload contains trailing JSON")
	}
	return nil
}

// rejectDuplicateJSONFields 递归拒绝所有 Object 层级的重复字段、null 和尾随值。
func rejectDuplicateJSONFields(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := consumeUniqueJSONValue(decoder); err != nil {
		return err
	}
	if token, err := decoder.Token(); !errors.Is(err, io.EOF) || token != nil {
		return fmt.Errorf("payload contains trailing JSON")
	}
	return nil
}

func consumeUniqueJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	if token == nil {
		return fmt.Errorf("payload contains null")
	}
	delim, isDelim := token.(json.Delim)
	if !isDelim {
		return nil
	}
	switch delim {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			fieldToken, err := decoder.Token()
			if err != nil {
				return err
			}
			field, ok := fieldToken.(string)
			if !ok {
				return fmt.Errorf("payload field name is invalid")
			}
			if _, exists := seen[field]; exists {
				return fmt.Errorf("payload contains duplicate field")
			}
			seen[field] = struct{}{}
			if err := consumeUniqueJSONValue(decoder); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim('}') {
			return fmt.Errorf("payload object is not closed")
		}
	case '[':
		for decoder.More() {
			if err := consumeUniqueJSONValue(decoder); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim(']') {
			return fmt.Errorf("payload array is not closed")
		}
	default:
		return fmt.Errorf("payload delimiter is invalid")
	}
	return nil
}
