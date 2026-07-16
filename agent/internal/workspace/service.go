package workspace

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"time"

	"github.com/FigoGoo/Dora-Agent/agent/internal/event"
	"github.com/FigoGoo/Dora-Agent/agent/internal/session"
	"github.com/google/uuid"
)

const maxJavaScriptSafeInteger = int64(1<<53 - 1)

// Service 编排一致性 Snapshot 解密与持久 EventLog 强类型投影，不持有 HTTP 或 Redis 状态。
type Service struct {
	repository Repository
	decryptor  ContentDecryptor
	limits     SnapshotLimits
	eventBytes int
}

// NewService 校验 Repository、Decryptor 和所有有界读取参数后创建只读 Workspace 用例。
func NewService(repository Repository, decryptor ContentDecryptor, limits SnapshotLimits, maxEventBytes int) (*Service, error) {
	if repository == nil || decryptor == nil || limits.MaxMessages <= 0 || limits.MaxInputs <= 0 || maxEventBytes <= 0 {
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
		if input.SessionID != identity.SessionID || input.MessageID == nil || *input.MessageID == "" ||
			!isCanonicalUUIDv7(input.ID) || !isCanonicalUUIDv7(*input.MessageID) ||
			input.EnqueueSeq != previousInputSeq+1 || input.EnqueueSeq > maxJavaScriptSafeInteger ||
			input.SourceType != string(session.InputSourceTypeUserMessage) || !validInputStatus(input.Status) ||
			input.AvailableAt.IsZero() || input.CreatedAt.IsZero() || input.UpdatedAt.IsZero() {
			return Snapshot{}, ErrPersistence
		}
		if _, exists := messageIDs[*input.MessageID]; !exists {
			return Snapshot{}, ErrPersistence
		}
		inputs = append(inputs, InputDTO{
			ID: input.ID, MessageID: *input.MessageID, SourceType: input.SourceType, Status: input.Status,
			EnqueueSeq: input.EnqueueSeq, AvailableAt: input.AvailableAt.UTC(),
			CreatedAt: input.CreatedAt.UTC(), UpdatedAt: input.UpdatedAt.UTC(),
		})
		previousInputSeq = input.EnqueueSeq
	}
	return Snapshot{
		SchemaVersion: SnapshotSchemaVersionV1, RequestID: requestID,
		Session: SessionDTO{
			ID: record.Session.ID, ProjectID: record.Session.ProjectID, Status: record.Session.Status,
			Version: record.Session.Version, CreatedAt: record.Session.CreatedAt.UTC(), UpdatedAt: record.Session.UpdatedAt.UTC(),
		},
		Messages: messages, Inputs: inputs, EventHighWatermark: record.EventHighWatermark,
		MinAvailableSeq: record.MinAvailableSeq,
	}, nil
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
		session.InputStatusRetryWait, session.InputStatusResolved, session.InputStatusDead:
		return true
	default:
		return false
	}
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

// isCanonicalUUIDv7 拒绝大小写、花括号和非 v7 UUID，确保前端标识只有一种编码。
func isCanonicalUUIDv7(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed.Version() == 7 && parsed.String() == value
}

// strictDecode 拒绝未知字段、多个 JSON 值和非强类型 Payload。
func strictDecode(data []byte, target any) error {
	if err := rejectDuplicateTopLevelFields(data); err != nil {
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

// rejectDuplicateTopLevelFields 拒绝 JSON Object 中同名字段的 last-write-wins 第二语义。
// W0 两类 Payload 都是扁平对象；未来引入嵌套结构时必须同步升级为递归唯一字段校验。
func rejectDuplicateTopLevelFields(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return fmt.Errorf("payload must be a JSON object")
	}
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
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return err
		}
	}
	if token, err = decoder.Token(); err != nil || token != json.Delim('}') {
		return fmt.Errorf("payload object is not closed")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("payload contains trailing JSON")
	}
	return nil
}
