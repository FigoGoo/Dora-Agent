package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/eino/schema"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/a2ui"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/generation"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/session"
)

type JobWakeupRunnerConfig struct {
	Store         SessionStore
	Invoker       AgentInvoker
	Events        a2ui.EventPublisher
	SessionValues func(session.SessionRecord) map[string]any
	MessageWindow session.MessageWindow
	NewID         func() string
	Now           func() time.Time
}

type JobWakeupRunner struct {
	cfg JobWakeupRunnerConfig
}

func NewJobWakeupRunner(cfg JobWakeupRunnerConfig) *JobWakeupRunner {
	if cfg.NewID == nil {
		cfg.NewID = randomID
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.MessageWindow.Limit == 0 {
		cfg.MessageWindow.Limit = 40
	}
	return &JobWakeupRunner{cfg: cfg}
}

func (r *JobWakeupRunner) Wakeup(ctx context.Context, wakeup session.JobWakeupEvent) error {
	if r == nil || r.cfg.Store == nil || r.cfg.Invoker == nil {
		return fmt.Errorf("job wakeup runner store and invoker are required")
	}
	if r.cfg.Events == nil {
		return fmt.Errorf("job wakeup event publisher is required")
	}
	wakeup.SessionID = strings.TrimSpace(wakeup.SessionID)
	wakeup.JobID = strings.TrimSpace(wakeup.JobID)
	if wakeup.SessionID == "" {
		return fmt.Errorf("session id is required")
	}
	if wakeup.JobID == "" {
		return fmt.Errorf("job id is required")
	}
	if !isWakeupStatus(wakeup.Status) {
		return nil
	}

	sessionRecord, err := r.cfg.Store.GetSession(ctx, wakeup.SessionID)
	if err != nil {
		return err
	}
	runID := r.cfg.NewID()
	wakeupMessage := schema.SystemMessage(wakeupContent(wakeup))
	if _, err := r.appendSchemaMessage(ctx, wakeup.SessionID, runID, wakeupMessage); err != nil {
		return err
	}

	records, err := r.cfg.Store.ListMessages(ctx, wakeup.SessionID, r.cfg.MessageWindow)
	if err != nil {
		return err
	}
	invokeReq := AgentInvokeRequest{
		Messages:     recordsToSchemaMessages(records),
		CheckpointID: wakeup.SessionID,
	}
	if r.cfg.SessionValues != nil {
		invokeReq.SessionValues = r.cfg.SessionValues(sessionRecord)
	}
	events, err := r.cfg.Invoker.Invoke(ctx, invokeReq)
	if err != nil {
		return err
	}
	return r.publishAgentEvents(ctx, wakeup.SessionID, runID, events)
}

func (r *JobWakeupRunner) publishAgentEvents(ctx context.Context, sessionID string, runID string, events <-chan AgentEvent) error {
	var assistant strings.Builder
	var assistantMessages []*schema.Message
	seq := int64(1)
	for event := range events {
		if event.Event == "" {
			event.Event = a2ui.EventChatDelta
		}
		if event.Err != nil {
			event.Event = a2ui.EventError
			if event.Payload == nil {
				event.Payload = map[string]any{"message": event.Err.Error()}
			}
		}
		if event.AssistantText != "" {
			assistant.WriteString(event.AssistantText)
		}
		if err := r.cfg.Events.Publish(ctx, a2ui.SSEEvent{
			ID:           r.cfg.NewID(),
			SessionID:    sessionID,
			RunID:        runID,
			Seq:          seq,
			Event:        event.Event,
			SurfaceID:    event.SurfaceID,
			DataModelKey: event.DataModelKey,
			Payload:      event.Payload,
			CreatedAt:    r.cfg.Now(),
		}); err != nil {
			return err
		}
		seq++
		if event.Message != nil {
			for _, renderEvent := range renderEventsFromToolMessage(event.Message) {
				if renderEvent.Event == "" {
					continue
				}
				if err := r.cfg.Events.Publish(ctx, a2ui.SSEEvent{
					ID:           r.cfg.NewID(),
					SessionID:    sessionID,
					RunID:        runID,
					Seq:          seq,
					Event:        renderEvent.Event,
					SurfaceID:    renderEvent.SurfaceID,
					DataModelKey: renderEvent.DataModelKey,
					Payload:      renderEvent.Payload,
					CreatedAt:    r.cfg.Now(),
				}); err != nil {
					return err
				}
				seq++
			}
			if shouldPersistImmediately(event.Message) {
				if _, err := r.appendSchemaMessage(ctx, sessionID, runID, event.Message); err != nil {
					return err
				}
			} else if event.Message.Role == schema.Assistant && len(event.Message.ToolCalls) == 0 {
				assistantMessages = append(assistantMessages, event.Message)
			}
		}
		if event.Err != nil {
			return event.Err
		}
	}

	assistantText := assistant.String()
	if assistantText == "" {
		return nil
	}
	assistantMessage := schema.AssistantMessage(assistantText, nil)
	if len(assistantMessages) == 1 && assistantMessages[0].Content == assistantText {
		assistantMessage = assistantMessages[0]
	}
	_, err := r.appendSchemaMessage(ctx, sessionID, runID, assistantMessage)
	return err
}

func (r *JobWakeupRunner) appendSchemaMessage(ctx context.Context, sessionID string, runID string, message *schema.Message) (session.MessageRecord, error) {
	record, err := schemaMessageRecord(r.cfg.NewID(), sessionID, runID, message, r.cfg.Now())
	if err != nil {
		return session.MessageRecord{}, err
	}
	return r.cfg.Store.AppendMessage(ctx, record)
}

func isWakeupStatus(status string) bool {
	switch status {
	case generation.StatusSucceeded, generation.StatusFailed, generation.StatusCancelled:
		return true
	default:
		return false
	}
}

func wakeupContent(wakeup session.JobWakeupEvent) string {
	raw, err := json.Marshal(wakeup)
	if err != nil {
		return fmt.Sprintf("AIGC generation job finished: job_id=%s status=%s", wakeup.JobID, wakeup.Status)
	}
	return "AIGC generation job finished. Use this event as runtime context, inspect the current storyboard/session state, and decide the next Skill step without regenerating completed assets unless the user asks for changes.\n" + string(raw)
}
