package pr1

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

const (
	VisibilityUserVisible     = "user_visible"
	VisibilityInternalSummary = "internal_summary"
	VisibilityAdminOnly       = "admin_only"
)

var (
	eventTypePattern            = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$`)
	payloadSchemaVersionPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+\.v[0-9]+$`)
)

type AGUIEnvelope struct {
	EventID              string         `json:"event_id"`
	EventType            string         `json:"event_type"`
	SchemaVersion        string         `json:"schema_version"`
	PayloadSchemaVersion string         `json:"payload_schema_version"`
	ProjectID            string         `json:"project_id"`
	SpaceID              *string        `json:"space_id,omitempty"`
	ActorUserID          *string        `json:"actor_user_id,omitempty"`
	SessionID            string         `json:"session_id"`
	RunID                string         `json:"run_id"`
	Seq                  int64          `json:"seq"`
	CreatedAt            time.Time      `json:"created_at"`
	Visibility           string         `json:"visibility,omitempty"`
	DedupeKey            string         `json:"dedupe_key"`
	PayloadDigest        string         `json:"payload_digest,omitempty"`
	TraceID              *string        `json:"trace_id,omitempty"`
	Payload              map[string]any `json:"payload"`
}

type AGUIInput struct {
	EventID              string
	EventType            string
	PayloadSchemaVersion string
	ProjectID            string
	SpaceID              string
	ActorUserID          string
	SessionID            string
	RunID                string
	Seq                  int64
	CreatedAt            time.Time
	Visibility           string
	DedupeKey            string
	PayloadDigest        string
	TraceID              string
	Payload              map[string]any
}

func BuildAGUIEnvelope(input AGUIInput) (AGUIEnvelope, error) {
	if input.CreatedAt.IsZero() {
		input.CreatedAt = time.Now().UTC()
	}
	if input.Payload == nil {
		input.Payload = map[string]any{}
	}
	if input.PayloadSchemaVersion == "" && input.EventType != "" {
		input.PayloadSchemaVersion = input.EventType + ".v1"
	}
	if input.DedupeKey == "" && input.RunID != "" && input.EventType != "" && input.Seq > 0 {
		input.DedupeKey = DedupeKey(input.RunID, input.EventType, input.Seq)
	}
	if input.Visibility == "" {
		input.Visibility = VisibilityUserVisible
	}
	envelope := AGUIEnvelope{
		EventID:              strings.TrimSpace(input.EventID),
		EventType:            strings.TrimSpace(input.EventType),
		SchemaVersion:        SchemaVersionAGUIEvent,
		PayloadSchemaVersion: strings.TrimSpace(input.PayloadSchemaVersion),
		ProjectID:            strings.TrimSpace(input.ProjectID),
		SessionID:            strings.TrimSpace(input.SessionID),
		RunID:                strings.TrimSpace(input.RunID),
		Seq:                  input.Seq,
		CreatedAt:            input.CreatedAt.UTC(),
		Visibility:           input.Visibility,
		DedupeKey:            strings.TrimSpace(input.DedupeKey),
		PayloadDigest:        strings.TrimSpace(input.PayloadDigest),
		Payload:              input.Payload,
	}
	if value := strings.TrimSpace(input.SpaceID); value != "" {
		envelope.SpaceID = &value
	}
	if value := strings.TrimSpace(input.ActorUserID); value != "" {
		envelope.ActorUserID = &value
	}
	if value := strings.TrimSpace(input.TraceID); value != "" {
		envelope.TraceID = &value
	}
	if err := ValidateAGUIEnvelope(envelope); err != nil {
		return AGUIEnvelope{}, err
	}
	return envelope, nil
}

func ValidateAGUIEnvelope(envelope AGUIEnvelope) error {
	if envelope.EventID == "" {
		return errors.New("event_id is required")
	}
	if !strings.HasPrefix(envelope.EventID, "evt_") {
		return fmt.Errorf("event_id must start with evt_: %q", envelope.EventID)
	}
	if envelope.EventType == "" || !eventTypePattern.MatchString(envelope.EventType) {
		return fmt.Errorf("invalid event_type %q", envelope.EventType)
	}
	if envelope.SchemaVersion != SchemaVersionAGUIEvent {
		return fmt.Errorf("schema_version must be %s", SchemaVersionAGUIEvent)
	}
	if envelope.PayloadSchemaVersion == "" || !payloadSchemaVersionPattern.MatchString(envelope.PayloadSchemaVersion) {
		return fmt.Errorf("invalid payload_schema_version %q", envelope.PayloadSchemaVersion)
	}
	if envelope.PayloadSchemaVersion != envelope.EventType+".v1" {
		return fmt.Errorf("payload_schema_version %q does not match event_type %q", envelope.PayloadSchemaVersion, envelope.EventType)
	}
	if envelope.ProjectID == "" || envelope.SessionID == "" || envelope.RunID == "" {
		return errors.New("project_id, session_id and run_id are required")
	}
	if envelope.Seq <= 0 {
		return errors.New("seq must be positive")
	}
	if envelope.CreatedAt.IsZero() {
		return errors.New("created_at is required")
	}
	if envelope.Visibility != "" && !isAllowed(envelope.Visibility, []string{
		VisibilityUserVisible,
		VisibilityInternalSummary,
		VisibilityAdminOnly,
	}) {
		return fmt.Errorf("invalid visibility %q", envelope.Visibility)
	}
	if envelope.DedupeKey == "" {
		return errors.New("dedupe_key is required")
	}
	if expected := DedupeKey(envelope.RunID, envelope.EventType, envelope.Seq); envelope.DedupeKey != expected {
		return fmt.Errorf("dedupe_key must be %q", expected)
	}
	if envelope.PayloadDigest != "" {
		if err := ValidateDigest(envelope.PayloadDigest); err != nil {
			return fmt.Errorf("payload_digest: %w", err)
		}
	}
	if envelope.Payload == nil {
		return errors.New("payload is required")
	}
	return nil
}

func ValidateAGUISequence(events []AGUIEnvelope) error {
	if len(events) == 0 {
		return errors.New("events are required")
	}
	seen := make(map[string]struct{}, len(events))
	runID := events[0].RunID
	for index, event := range events {
		if err := ValidateAGUIEnvelope(event); err != nil {
			return fmt.Errorf("event %d: %w", index+1, err)
		}
		if event.RunID != runID {
			return fmt.Errorf("event %d: mixed run_id %q and %q", index+1, runID, event.RunID)
		}
		expectedSeq := int64(index + 1)
		if event.Seq != expectedSeq {
			return fmt.Errorf("event %d: expected seq %d, got %d", index+1, expectedSeq, event.Seq)
		}
		if _, ok := seen[event.DedupeKey]; ok {
			return fmt.Errorf("event %d: duplicate dedupe_key %q", index+1, event.DedupeKey)
		}
		seen[event.DedupeKey] = struct{}{}
	}
	return nil
}

func DedupeKey(runID, eventType string, seq int64) string {
	return fmt.Sprintf("%s:%s:%d", runID, eventType, seq)
}
