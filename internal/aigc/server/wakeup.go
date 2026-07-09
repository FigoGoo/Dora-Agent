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

// JobWakeupRunnerConfig 汇总生成任务完成后自动续跑 Agent 所需的外部依赖。
type JobWakeupRunnerConfig struct {
	Store         SessionStore
	Invoker       AgentInvoker
	Events        a2ui.EventPublisher
	SessionValues func(session.SessionRecord) map[string]any
	MessageWindow session.MessageWindow
	NewID         func() string
	Now           func() time.Time
}

// JobWakeupRunner 在媒体生成任务进入终态后，向会话追加系统消息并驱动 Agent 继续分析。
type JobWakeupRunner struct {
	cfg JobWakeupRunnerConfig
}

// NewJobWakeupRunner 创建 wakeup runner，并补齐 ID、时间和历史窗口默认值。
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

// Wakeup 校验任务终态事件，写入上下文消息，然后用当前会话历史重新唤醒 Agent。
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

// publishAgentEvents 消费唤醒后的 Agent 事件，只发布最新完整回复并持久化可见消息。
func (r *JobWakeupRunner) publishAgentEvents(ctx context.Context, sessionID string, runID string, events <-chan AgentEvent) error {
	var assistant strings.Builder
	var latestAssistantEvent *AgentEvent
	chatSurface := newChatA2UISurface(sessionID)
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
		// 唤醒流程也可能带回历史完整消息；只保留最新回复，避免任务完成后重复刷卡片。
		if isCompleteAssistantMessageEvent(event) {
			if event.AssistantText == "" {
				event.AssistantText = event.Message.Content
			}
			latest := event
			latestAssistantEvent = &latest
			continue
		}
		if event.AssistantText != "" {
			assistant.WriteString(event.AssistantText)
		}
		if event.Event == a2ui.EventChatDelta || event.Event == a2ui.EventChatMessage {
			if event.Message != nil {
				if shouldPersistImmediately(event.Message) {
					if _, err := r.appendSchemaMessage(ctx, sessionID, runID, event.Message); err != nil {
						return err
					}
				}
			}
			continue
		}
		for _, renderEvent := range chatSurface.eventsFromAgentEvent(event) {
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
		if event.Message != nil {
			if shouldPersistImmediately(event.Message) {
				if _, err := r.appendSchemaMessage(ctx, sessionID, runID, event.Message); err != nil {
					return err
				}
			}
		}
		if event.Err != nil {
			return event.Err
		}
	}

	if latestAssistantEvent != nil {
		rewritten := assistantEventWithA2UIInstanceCardIDs(*latestAssistantEvent, r.cfg.NewID)
		latestAssistantEvent = &rewritten
		if err := r.publishRenderEvents(ctx, sessionID, runID, &seq, chatSurface.eventsFromAgentEvent(*latestAssistantEvent)); err != nil {
			return err
		}
		return r.appendAssistantEventMessage(ctx, sessionID, runID, latestAssistantEvent)
	}

	assistantText := assistant.String()
	if assistantText == "" {
		return nil
	}
	if rewritten, ok := contentWithA2UIInstanceCardIDs(assistantText, r.cfg.NewID); ok {
		assistantText = rewritten
	}
	if err := r.publishRenderEvents(ctx, sessionID, runID, &seq, chatSurface.assistantEvents(AgentEvent{
		Event:         a2ui.EventChatDelta,
		AssistantText: assistantText,
	})); err != nil {
		return err
	}
	assistantText = strings.TrimSpace(displayTextWithoutA2UIEnvelope(assistantText))
	if assistantText == "" {
		return nil
	}
	assistantMessage := schema.AssistantMessage(assistantText, nil)
	_, err := r.appendSchemaMessage(ctx, sessionID, runID, assistantMessage)
	return err
}

// publishRenderEvents 按顺序把内部渲染事件写入 broker，保证前端 timeline 顺序稳定。
func (r *JobWakeupRunner) publishRenderEvents(ctx context.Context, sessionID string, runID string, seq *int64, events []a2ui.RenderEventHint) error {
	for _, renderEvent := range events {
		if err := r.cfg.Events.Publish(ctx, a2ui.SSEEvent{
			ID:           r.cfg.NewID(),
			SessionID:    sessionID,
			RunID:        runID,
			Seq:          *seq,
			Event:        renderEvent.Event,
			SurfaceID:    renderEvent.SurfaceID,
			DataModelKey: renderEvent.DataModelKey,
			Payload:      renderEvent.Payload,
			CreatedAt:    r.cfg.Now(),
		}); err != nil {
			return err
		}
		*seq = *seq + 1
	}
	return nil
}

// appendAssistantEventMessage 持久化最新 assistant 完整消息；纯 A2UI JSON 作为结构化历史保留。
func (r *JobWakeupRunner) appendAssistantEventMessage(ctx context.Context, sessionID string, runID string, event *AgentEvent) error {
	if event == nil || event.Message == nil {
		return nil
	}
	content := strings.TrimSpace(event.AssistantText)
	if content == "" {
		content = strings.TrimSpace(event.Message.Content)
	}
	content = displayTextWithoutA2UIEnvelope(content)
	if content == "" {
		return nil
	}
	message := schema.AssistantMessage(content, nil)
	if event.Message.Content == content {
		message = event.Message
	}
	_, err := r.appendSchemaMessage(ctx, sessionID, runID, message)
	return err
}

// appendSchemaMessage 把 Eino schema.Message 转成会话消息记录并写入存储。
func (r *JobWakeupRunner) appendSchemaMessage(ctx context.Context, sessionID string, runID string, message *schema.Message) (session.MessageRecord, error) {
	record, err := schemaMessageRecord(r.cfg.NewID(), sessionID, runID, message, r.cfg.Now())
	if err != nil {
		return session.MessageRecord{}, err
	}
	return r.cfg.Store.AppendMessage(ctx, record)
}

// isWakeupStatus 判断任务状态是否需要触发 Agent 续跑。
func isWakeupStatus(status string) bool {
	switch status {
	case generation.StatusSucceeded, generation.StatusFailed, generation.StatusCancelled:
		return true
	default:
		return false
	}
}

// wakeupContent 生成给 Agent 的系统上下文，告知它任务已完成并继续下一步决策。
func wakeupContent(wakeup session.JobWakeupEvent) string {
	raw, err := json.Marshal(wakeup)
	if err != nil {
		return fmt.Sprintf("AIGC generation job finished: job_id=%s status=%s", wakeup.JobID, wakeup.Status)
	}
	return "AIGC generation job finished. Use this event as runtime context, inspect the current storyboard/session state, and decide the next Skill step without regenerating completed assets unless the user asks for changes.\n" + string(raw)
}
