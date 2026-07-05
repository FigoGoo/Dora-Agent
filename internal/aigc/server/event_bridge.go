package server

import (
	"context"
	"strings"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/a2ui"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/generation"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/session"
	aigctools "github.com/FigoGoo/Dora-Agent/internal/aigc/tools"
)

type JobWakeupHandler interface {
	Wakeup(ctx context.Context, event session.JobWakeupEvent) error
}

type GenerationEventPublisher struct {
	Broker a2ui.EventPublisher
	Wakeup JobWakeupHandler
	Now    func() time.Time
}

func (p GenerationEventPublisher) Publish(ctx context.Context, event generation.WorkerEvent) error {
	if p.Broker == nil {
		return nil
	}
	createdAt := event.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now()
		if p.Now != nil {
			createdAt = p.Now()
		}
	}
	if wakeup, ok := jobWakeupFromWorkerEvent(event); ok && p.Wakeup != nil {
		go func() {
			_ = p.Wakeup.Wakeup(context.Background(), wakeup)
		}()
	}
	return p.Broker.Publish(ctx, a2ui.SSEEvent{
		ID:           event.ID,
		SessionID:    event.SessionID,
		Event:        a2uiEventName(event.Event),
		SurfaceID:    event.SurfaceID,
		DataModelKey: event.DataModelKey,
		Payload:      a2uiPayload(event.Payload),
		CreatedAt:    createdAt,
	})
}

func a2uiEventName(event string) string {
	switch strings.TrimSpace(event) {
	case generation.EventJobStatus:
		return a2ui.EventJobStatus
	case generation.EventStoryboardPatch:
		return a2ui.EventStoryboardPatch
	default:
		return strings.TrimSpace(event)
	}
}

func jobWakeupFromWorkerEvent(event generation.WorkerEvent) (session.JobWakeupEvent, bool) {
	if event.Event != generation.EventJobStatus {
		return session.JobWakeupEvent{}, false
	}
	payload, ok := event.Payload.(generation.JobStatusPayload)
	if !ok || !isWakeupStatus(payload.Status) {
		return session.JobWakeupEvent{}, false
	}
	return session.JobWakeupEvent{
		SessionID:  payload.SessionID,
		JobID:      payload.JobID,
		Status:     payload.Status,
		AssetIDs:   append([]string(nil), payload.ResultAssetIDs...),
		ErrorCode:  payload.ErrorCode,
		ToolCallID: payload.ToolCallID,
		StageKey:   payload.StageKey,
	}, true
}

func a2uiPayload(payload any) any {
	switch p := payload.(type) {
	case generation.StoryboardPatchPayload:
		return a2ui.StoryboardPatchPayload{
			StoryboardID: p.StoryboardID,
			BaseVersion:  p.BaseVersion,
			NextVersion:  p.NextVersion,
			Ops:          append([]aigctools.JSONPatchOp(nil), p.Ops...),
			Source:       p.Source,
			ToolCallID:   p.ToolCallID,
		}
	default:
		return payload
	}
}
