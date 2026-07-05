package turncontext

import (
	"context"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/session"
)

const (
	SessionIDValueKey     = "aigc.session.id"
	SessionUserIDValueKey = "aigc.session.user_id"
	SessionSkillIDKey     = "aigc.session.skill_id"
	SessionTitleValueKey  = "aigc.session.title"
	SessionStatusValueKey = "aigc.session.status"

	turnContextExtraKey = "aigc_turn_context"
)

type Middleware struct {
	*adk.BaseChatModelAgentMiddleware
	Builder *Builder
}

func NewMiddleware(builder *Builder) adk.ChatModelAgentMiddleware {
	return &Middleware{
		BaseChatModelAgentMiddleware: &adk.BaseChatModelAgentMiddleware{},
		Builder:                      builder,
	}
}

func SessionValues(record session.SessionRecord) map[string]any {
	return map[string]any{
		SessionIDValueKey:     record.ID,
		SessionUserIDValueKey: record.UserID,
		SessionSkillIDKey:     record.SkillID,
		SessionTitleValueKey:  record.Title,
		SessionStatusValueKey: record.Status,
	}
}

func (m *Middleware) BeforeModelRewriteState(ctx context.Context, state *adk.ChatModelAgentState, _ *adk.ModelContext) (context.Context, *adk.ChatModelAgentState, error) {
	if m == nil || m.Builder == nil || state == nil {
		return ctx, state, nil
	}
	record := sessionRecordFromValues(ctx)
	if strings.TrimSpace(record.ID) == "" {
		return ctx, state, nil
	}
	prompt, err := m.Builder.Build(ctx, record)
	if err != nil {
		return ctx, nil, err
	}
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return ctx, state, nil
	}

	next := *state
	next.Messages = prependTurnContext(removeInjectedTurnContext(state.Messages), prompt)
	return ctx, &next, nil
}

func sessionRecordFromValues(ctx context.Context) session.SessionRecord {
	return session.SessionRecord{
		ID:      sessionValueString(ctx, SessionIDValueKey),
		UserID:  sessionValueString(ctx, SessionUserIDValueKey),
		SkillID: sessionValueString(ctx, SessionSkillIDKey),
		Title:   sessionValueString(ctx, SessionTitleValueKey),
		Status:  sessionValueString(ctx, SessionStatusValueKey),
	}
}

func sessionValueString(ctx context.Context, key string) string {
	value, ok := adk.GetSessionValue(ctx, key)
	if !ok || value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}

func removeInjectedTurnContext(messages []*schema.Message) []*schema.Message {
	out := make([]*schema.Message, 0, len(messages))
	for _, message := range messages {
		if message == nil || hasTurnContextExtra(message) {
			continue
		}
		out = append(out, message)
	}
	return out
}

func prependTurnContext(messages []*schema.Message, prompt string) []*schema.Message {
	contextMessage := schema.SystemMessage(prompt)
	contextMessage.Extra = map[string]any{turnContextExtraKey: true}
	out := make([]*schema.Message, 0, len(messages)+1)
	out = append(out, contextMessage)
	out = append(out, messages...)
	return out
}

func hasTurnContextExtra(message *schema.Message) bool {
	if message.Extra == nil {
		return false
	}
	value, ok := message.Extra[turnContextExtraKey]
	if !ok {
		return false
	}
	enabled, ok := value.(bool)
	return ok && enabled
}
