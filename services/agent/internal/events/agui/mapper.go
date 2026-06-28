package agui

import (
	"errors"
	"strings"
	"time"

	"github.com/FigoGoo/Dora-Agent/services/agent/internal/domain/event"
)

type Input struct {
	EventID     string
	Type        string
	SessionID   string
	RunID       string
	ProjectID   string
	SpaceID     string
	ActorUserID string
	Sequence    int64
	Component   string
	TraceID     string
	Payload     map[string]any
}

func BuildEnvelope(in Input, now time.Time) (event.Envelope, error) {
	if strings.TrimSpace(in.EventID) == "" || strings.TrimSpace(in.Type) == "" || in.Sequence <= 0 {
		return event.Envelope{}, errors.New("event_id, type and positive sequence are required")
	}
	if !event.IsCanonical(in.Type) {
		return event.Envelope{}, errors.New("event type is not canonical")
	}
	payload := in.Payload
	if payload == nil {
		payload = map[string]any{}
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	component := in.Component
	if component == "" {
		component = "agent"
	}
	return event.Envelope{
		EventID: in.EventID, Type: in.Type, SessionID: in.SessionID, RunID: in.RunID, ProjectID: in.ProjectID,
		SpaceID: in.SpaceID, ActorUserID: in.ActorUserID, Sequence: in.Sequence, Timestamp: now.UTC(),
		Component: component, TraceID: in.TraceID, Payload: payload,
	}, nil
}
