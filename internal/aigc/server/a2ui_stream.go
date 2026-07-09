package server

import (
	"encoding/json"
	"strings"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/a2ui"
)

// chatA2UISurface 负责校验 Agent 直出的 A2UI action，并透传给前端。
type chatA2UISurface struct{}

// newChatA2UISurface 创建聊天渲染器；sessionID 由外层 SSE 事件承载。
func newChatA2UISurface(_ string) *chatA2UISurface {
	return &chatA2UISurface{}
}

// eventsFromAgentEvent 将 AgentEvent 分发成内部渲染事件，非 A2UI 事件不会被猜测成 UI。
func (s *chatA2UISurface) eventsFromAgentEvent(event AgentEvent) []a2ui.RenderEventHint {
	switch event.Event {
	case a2ui.EventChatDelta, a2ui.EventChatMessage:
		return s.assistantEvents(event)
	case a2ui.EventAction, a2ui.EventInterruptRequest, a2ui.EventError:
		return []a2ui.RenderEventHint{{
			Event:        event.Event,
			SurfaceID:    event.SurfaceID,
			DataModelKey: event.DataModelKey,
			Payload:      event.Payload,
		}}
	default:
		return nil
	}
}

// assistantEvents 只接受 Agent 直出的纯 A2UI ActionEnvelope。
// 普通说明必须由 Agent 放进 append_card 的 Card/Text/Markdown 组件树，后端不再包装消息卡。
func (s *chatA2UISurface) assistantEvents(event AgentEvent) []a2ui.RenderEventHint {
	content := strings.TrimSpace(event.AssistantText)
	if content == "" && event.Message != nil {
		content = strings.TrimSpace(event.Message.Content)
	}
	if content == "" {
		content = payloadText(event.Payload)
	}
	if content == "" {
		return nil
	}

	if envelope, ok := a2ui.ParseActionEnvelopeContent(content); ok {
		return []a2ui.RenderEventHint{actionEnvelopeEvent(envelope)}
	}
	return protocolErrorEvent("Agent 输出不是合法 A2UI ActionEnvelope")
}

// actionEnvelopeEvent 把 ActionEnvelope 包成统一 SSE action 事件。
func actionEnvelopeEvent(envelope a2ui.ActionEnvelope) a2ui.RenderEventHint {
	if envelope.Version == "" {
		envelope.Version = a2ui.Version1
	}
	return a2ui.RenderEventHint{
		Event:   a2ui.EventAction,
		Payload: envelope,
	}
}

// protocolErrorEvent 在 Agent 违反 A2UI 直出协议时返回错误事件，避免把坏协议渲染成普通消息。
func protocolErrorEvent(message string) []a2ui.RenderEventHint {
	return []a2ui.RenderEventHint{{
		Event: a2ui.EventError,
		Payload: map[string]any{
			"code":    "invalid_a2ui_action_envelope",
			"message": message,
		},
	}}
}

// payloadText 从通用 payload 中提取可能承载 ActionEnvelope 的文本，不做普通文本兜底渲染。
func payloadText(payload any) string {
	values := payloadMap(payload)
	for _, key := range []string{"text", "delta", "content", "message", "title"} {
		if value := payloadString(values, key); value != "" {
			return value
		}
	}
	return ""
}

// displayTextWithoutA2UIEnvelope 只保留可恢复的纯 A2UI ActionEnvelope 历史。
// 非协议 assistant 文本不进入历史，避免刷新后被当作普通消息兜底渲染。
func displayTextWithoutA2UIEnvelope(content string) string {
	content = strings.TrimSpace(content)
	if _, ok := a2ui.ParseActionEnvelopeContent(content); ok {
		return content
	}
	return ""
}

// assistantEventWithA2UIInstanceCardIDs 在 assistant A2UI 消息进入 SSE/历史前补实例级 card_id。
func assistantEventWithA2UIInstanceCardIDs(event AgentEvent, newID func() string) AgentEvent {
	content := strings.TrimSpace(event.AssistantText)
	if content == "" && event.Message != nil {
		content = strings.TrimSpace(event.Message.Content)
	}
	if content == "" {
		content = payloadText(event.Payload)
	}
	rewritten, ok := contentWithA2UIInstanceCardIDs(content, newID)
	if !ok {
		return event
	}
	event.AssistantText = rewritten
	if event.Message != nil {
		message := *event.Message
		message.Content = rewritten
		event.Message = &message
	}
	return event
}

// contentWithA2UIInstanceCardIDs 把纯 ActionEnvelope JSON 改写成实例级 card_id 的可恢复内容。
func contentWithA2UIInstanceCardIDs(content string, newID func() string) (string, bool) {
	envelope, ok := a2ui.ParseActionEnvelopeContent(content)
	if !ok {
		return "", false
	}
	envelope = a2ui.EnsureActionInstanceIDs(envelope, newID)
	raw, err := json.Marshal(envelope)
	if err != nil {
		return "", false
	}
	return string(raw), true
}
