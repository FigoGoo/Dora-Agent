// Package events provides the durable, session-scoped event log used as the
// source of truth for the external A2UI SSE stream.
package events

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	ErrEventNotFound       = errors.New("session event not found")
	ErrIdempotencyConflict = errors.New("session event idempotency conflict")
)

type ProducerKind string

const (
	ProducerAgentAction     ProducerKind = "agent_action"
	ProducerRunnerInterrupt ProducerKind = "runner_interrupt"
	ProducerDomainProjector ProducerKind = "domain_projector"
	ProducerSessionRuntime  ProducerKind = "session_runtime"
)

const MaxEventIDLength = 128

// SessionEvent is one externally visible SSE row. Seq is assigned by the
// store and is monotonic within a session. SourceKey is the producer's stable
// idempotency key; ProjectionIndex distinguishes multiple rows from one
// source event.
type SessionEvent struct {
	SessionID       string          `json:"session_id" gorm:"primaryKey;size:128"`
	Seq             int64           `json:"seq" gorm:"primaryKey;autoIncrement:false"`
	EventID         string          `json:"event_id" gorm:"size:128;uniqueIndex"`
	EventType       string          `json:"event_type" gorm:"size:96;index"`
	ProducerKind    ProducerKind    `json:"producer_kind" gorm:"size:64;uniqueIndex:uidx_aigc_session_event_source,priority:2"`
	SourceKey       string          `json:"source_key" gorm:"size:512;uniqueIndex:uidx_aigc_session_event_source,priority:3"`
	ProjectionIndex int             `json:"projection_index" gorm:"uniqueIndex:uidx_aigc_session_event_source,priority:4"`
	SurfaceID       string          `json:"surface_id,omitempty" gorm:"size:128"`
	DataModelKey    string          `json:"data_model_key,omitempty" gorm:"size:256"`
	Payload         json.RawMessage `json:"payload" gorm:"column:payload_json;type:jsonb"`
	CreatedAt       time.Time       `json:"created_at"`
}

func (SessionEvent) TableName() string { return "aigc_session_event_log" }

type sessionEventCounter struct {
	SessionID string    `gorm:"primaryKey;size:128"`
	NextSeq   int64     `gorm:"column:next_seq"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

func (sessionEventCounter) TableName() string { return "aigc_session_event_counters" }

// AppendResult reports whether a new row was appended. A retry with the same
// idempotency identity returns the original row and Appended=false.
type AppendResult struct {
	Event    SessionEvent `json:"event"`
	Appended bool         `json:"appended"`
}

type TailOptions struct {
	AfterSeq int64
	Limit    int
}

func MarshalPayload(value any) (json.RawMessage, error) {
	if value == nil {
		return json.RawMessage(`{}`), nil
	}
	if raw, ok := value.(json.RawMessage); ok {
		if !json.Valid(raw) {
			return nil, fmt.Errorf("event payload must be valid JSON")
		}
		return append(json.RawMessage(nil), raw...), nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("marshal event payload: %w", err)
	}
	return raw, nil
}

func normalizeEvent(event SessionEvent) (SessionEvent, error) {
	event.SessionID = strings.TrimSpace(event.SessionID)
	event.EventID = strings.TrimSpace(event.EventID)
	event.EventType = strings.TrimSpace(event.EventType)
	event.ProducerKind = ProducerKind(strings.TrimSpace(string(event.ProducerKind)))
	event.SourceKey = strings.TrimSpace(event.SourceKey)
	event.SurfaceID = strings.TrimSpace(event.SurfaceID)
	event.DataModelKey = strings.TrimSpace(event.DataModelKey)
	if event.SessionID == "" {
		return SessionEvent{}, fmt.Errorf("session id is required")
	}
	if event.EventID == "" {
		return SessionEvent{}, fmt.Errorf("event id is required")
	}
	if utf8.RuneCountInString(event.EventID) > MaxEventIDLength {
		return SessionEvent{}, fmt.Errorf("event id cannot exceed %d characters", MaxEventIDLength)
	}
	if event.EventType == "" {
		return SessionEvent{}, fmt.Errorf("event type is required")
	}
	if event.ProducerKind == "" {
		return SessionEvent{}, fmt.Errorf("producer kind is required")
	}
	if event.SourceKey == "" {
		return SessionEvent{}, fmt.Errorf("source key is required")
	}
	if event.ProjectionIndex < 0 {
		return SessionEvent{}, fmt.Errorf("projection index cannot be negative")
	}
	if len(event.Payload) == 0 {
		event.Payload = json.RawMessage(`{}`)
	}
	if !json.Valid(event.Payload) {
		return SessionEvent{}, fmt.Errorf("event payload must be valid JSON")
	}
	event.Payload = append(json.RawMessage(nil), event.Payload...)
	// Seq and CreatedAt are owned by the store.
	event.Seq = 0
	event.CreatedAt = time.Time{}
	return event, nil
}

func sameIdentity(existing, requested SessionEvent) bool {
	return existing.SessionID == requested.SessionID &&
		existing.EventID == requested.EventID &&
		existing.EventType == requested.EventType &&
		existing.ProducerKind == requested.ProducerKind &&
		existing.SourceKey == requested.SourceKey &&
		existing.ProjectionIndex == requested.ProjectionIndex &&
		existing.SurfaceID == requested.SurfaceID &&
		existing.DataModelKey == requested.DataModelKey &&
		jsonEqual(existing.Payload, requested.Payload)
}

func jsonEqual(left, right json.RawMessage) bool {
	var leftValue, rightValue any
	if err := json.Unmarshal(left, &leftValue); err != nil {
		return false
	}
	if err := json.Unmarshal(right, &rightValue); err != nil {
		return false
	}
	return reflect.DeepEqual(leftValue, rightValue)
}

func sourceIdentity(event SessionEvent) string {
	return fmt.Sprintf("%s\x00%s\x00%s\x00%d", event.SessionID, event.ProducerKind, event.SourceKey, event.ProjectionIndex)
}
