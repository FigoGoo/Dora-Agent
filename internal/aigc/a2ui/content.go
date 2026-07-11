package a2ui

import (
	"encoding/json"
	"strings"
)

// ParseActionEnvelopeContent 校验 Agent 是否直出了纯 A2UI ActionEnvelope JSON。
// Card 内部可以自由组合 Text、Markdown 和表单组件，但 JSON 外层不能混入自然语言或代码块。
func ParseActionEnvelopeContent(content string) (ActionEnvelope, bool) {
	content = strings.TrimSpace(content)
	if content == "" || !strings.HasPrefix(content, "{") || !strings.HasSuffix(content, "}") {
		return ActionEnvelope{}, false
	}
	var envelope ActionEnvelope
	if err := json.Unmarshal([]byte(content), &envelope); err != nil {
		return ActionEnvelope{}, false
	}
	if strings.TrimSpace(envelope.Version) == "" || len(envelope.Actions) == 0 {
		return ActionEnvelope{}, false
	}
	envelope = NormalizeActionEnvelope(envelope)
	if envelope.Version != Version1 || !validateActionEnvelope(envelope) {
		return ActionEnvelope{}, false
	}
	return envelope, true
}

// NormalizeActionEnvelope 补齐协议默认值，但不改写 Agent 生成的卡片组件树。
func NormalizeActionEnvelope(envelope ActionEnvelope) ActionEnvelope {
	envelope.Version = strings.TrimSpace(envelope.Version)
	for index := range envelope.Actions {
		action := &envelope.Actions[index]
		action.Type = strings.TrimSpace(action.Type)
		action.Surface = strings.TrimSpace(action.Surface)
		action.CardID = strings.TrimSpace(action.CardID)
		action.MessageID = strings.TrimSpace(action.MessageID)
		action.Ref = strings.TrimSpace(action.Ref)
		action.ActionID = strings.TrimSpace(action.ActionID)
		if action.Target != nil {
			action.Target.Surface = strings.TrimSpace(action.Target.Surface)
			action.Target.CardID = strings.TrimSpace(action.Target.CardID)
			action.Target.MessageID = strings.TrimSpace(action.Target.MessageID)
			action.Target.Ref = strings.TrimSpace(action.Target.Ref)
		}
		if action.Surface == "" {
			action.Surface = "chat"
		}
		normalizeCard(action.Card)
	}
	return envelope
}

// EnsureActionInstanceIDs 把 Agent 给出的业务 card_id 扩展成实例级 card_id。
// 最终下发和持久化的 card_id 是唯一值，用户提交后也用它关闭某一张具体卡。
func EnsureActionInstanceIDs(envelope ActionEnvelope, newID func() string) ActionEnvelope {
	envelope = NormalizeActionEnvelope(envelope)
	if newID == nil {
		return envelope
	}
	for index := range envelope.Actions {
		action := &envelope.Actions[index]
		if action.Type != ActionAppendCard || cardIDHasInstanceSuffix(action.CardID) {
			continue
		}
		id := strings.TrimSpace(newID())
		if id == "" {
			continue
		}
		if action.CardID == "" {
			action.CardID = id
			continue
		}
		action.CardID = action.CardID + ":" + id
	}
	return envelope
}

func cardIDHasInstanceSuffix(cardID string) bool {
	index := strings.LastIndex(cardID, ":")
	if index < 0 || index == len(cardID)-1 {
		return false
	}
	suffix := cardID[index+1:]
	if len(suffix) != 32 {
		return false
	}
	for _, char := range suffix {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') && (char < 'A' || char > 'F') {
			return false
		}
	}
	return true
}

// validateActionEnvelope 校验消息卡是否真的继承 Card 协议结构。
func validateActionEnvelope(envelope ActionEnvelope) bool {
	for _, action := range envelope.Actions {
		switch action.Type {
		case ActionAppendCard:
			if !validCard(action.Card) {
				return false
			}
		case ActionUpdateCard:
			// update_card 可以更新聊天卡，也可以把 payload 投影到 storyboard/tool_runs。
			// 具体 target/payload 结构由对应 surface 消费方继续校验。
		default:
			return false
		}
	}
	return true
}

// normalizeCard 清理 Card 公共字段，避免空格造成更新定位不稳定。
func normalizeCard(card *Card) {
	if card == nil {
		return
	}
	card.Type = strings.TrimSpace(card.Type)
	card.Title = strings.TrimSpace(card.Title)
	card.Message = strings.TrimSpace(card.Message)
	card.Status = strings.TrimSpace(card.Status)
	card.Root = strings.TrimSpace(card.Root)
	card.SubmitLabel = strings.TrimSpace(card.SubmitLabel)
	for index := range card.Components {
		card.Components[index].ID = strings.TrimSpace(card.Components[index].ID)
	}
}

// validCard 要求 append_card 的根组件必须是 Card 容器，叶子控件只能嵌套在 Card 之内。
func validCard(card *Card) bool {
	if card == nil || strings.TrimSpace(card.Root) == "" || len(card.Components) == 0 {
		return false
	}
	for _, component := range card.Components {
		if component.ID == card.Root {
			_, ok := component.Component[ComponentCard]
			return ok
		}
	}
	return false
}
