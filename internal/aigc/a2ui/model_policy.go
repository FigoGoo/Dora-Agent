package a2ui

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"
)

// ErrModelAuthoredApproval marks an A2UI envelope in which the model tries to
// imitate the durable Approval surface. Authoritative Approval cards are
// published by the server and intentionally use the ordinary protocol parser,
// not this model-output policy.
var ErrModelAuthoredApproval = errors.New("model-authored approval UI is forbidden")

// ValidateModelAuthoredActionEnvelope applies policy that is specific to
// assistant-authored A2UI. It must not be folded into
// ParseActionEnvelopeContent: system-published Approval cards legitimately
// carry approval_id and a decision SingleChoice.
func ValidateModelAuthoredActionEnvelope(envelope ActionEnvelope) error {
	for actionIndex, action := range envelope.Actions {
		if action.Type != ActionAppendCard || action.Card == nil {
			continue
		}
		if containsJSONKey(action.Card.Data, "approvalid") {
			return fmt.Errorf("%w: action %d carries approval_id", ErrModelAuthoredApproval, actionIndex)
		}
		if containsPseudoApprovalInstruction(action.Card.Title) {
			return fmt.Errorf("%w: action %d card title asks for a chat decision", ErrModelAuthoredApproval, actionIndex)
		}
		if containsPseudoApprovalInstruction(action.Card.Message) {
			return fmt.Errorf("%w: action %d card message asks for a chat decision", ErrModelAuthoredApproval, actionIndex)
		}
		for componentIndex, component := range action.Card.Components {
			for kind, value := range component.Component {
				switch kind {
				case ComponentText, ComponentMarkdown:
					if containsPseudoApprovalInstruction(jsonStringField(value, "value")) || containsPseudoApprovalInstruction(boundComponentText(action.Card.Data, value)) {
						return fmt.Errorf("%w: action %d component %d asks for a chat decision", ErrModelAuthoredApproval, actionIndex, componentIndex)
					}
				case ComponentSingleChoice:
					if singleChoiceImitatesApproval(value) {
						return fmt.Errorf("%w: action %d component %d imitates an approval decision", ErrModelAuthoredApproval, actionIndex, componentIndex)
					}
				}
			}
		}
	}
	return nil
}

// ParseModelAuthoredActionEnvelopeContent combines strict protocol parsing
// with the assistant-only policy used by retry, projection and history.
func ParseModelAuthoredActionEnvelopeContent(content string) (ActionEnvelope, bool) {
	envelope, ok := ParseActionEnvelopeContent(content)
	if !ok || ValidateModelAuthoredActionEnvelope(envelope) != nil {
		return ActionEnvelope{}, false
	}
	return envelope, true
}

func jsonObject(value any) map[string]any {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var object map[string]any
	if json.Unmarshal(raw, &object) != nil {
		return nil
	}
	return object
}

func jsonStringField(value any, key string) string {
	object := jsonObject(value)
	text, _ := object[key].(string)
	return strings.TrimSpace(text)
}

func boundComponentText(data any, component any) string {
	dataKey := jsonStringField(component, "dataKey")
	if dataKey == "" {
		dataKey = jsonStringField(component, "data_key")
	}
	if dataKey == "" {
		return ""
	}
	value, _ := jsonObject(data)[dataKey].(string)
	return strings.TrimSpace(value)
}

func containsJSONKey(value any, normalizedKey string) bool {
	if value == nil {
		return false
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return false
	}
	var decoded any
	if json.Unmarshal(raw, &decoded) != nil {
		return false
	}
	var walk func(any) bool
	walk = func(current any) bool {
		switch typed := current.(type) {
		case map[string]any:
			for key, child := range typed {
				key = strings.ToLower(strings.TrimSpace(key))
				key = strings.NewReplacer("_", "", "-", "").Replace(key)
				if key == normalizedKey || walk(child) {
					return true
				}
			}
		case []any:
			for _, child := range typed {
				if walk(child) {
					return true
				}
			}
		}
		return false
	}
	return walk(decoded)
}

func singleChoiceImitatesApproval(value any) bool {
	object := jsonObject(value)
	key := normalizeChoiceToken(fmt.Sprint(object["key"]))
	if key == "decision" || key == "approval" || key == "approvaldecision" || key == "reviewdecision" {
		return true
	}
	options, _ := object["options"].([]any)
	hasApproveLabel, hasRejectLabel := false, false
	for _, rawOption := range options {
		option, _ := rawOption.(map[string]any)
		stableValue := normalizeChoiceToken(fmt.Sprint(option["value"]))
		switch stableValue {
		case "approve", "approved", "accept", "accepted", "confirm", "confirmed", "reject", "rejected", "decline", "declined", "deny", "denied", "确认", "同意", "批准", "拒绝", "驳回":
			return true
		}
		label := strings.TrimSpace(fmt.Sprint(option["label"]))
		if isApprovalLabel(label) {
			hasApproveLabel = true
		}
		if isRejectionLabel(label) {
			hasRejectLabel = true
		}
	}
	return hasApproveLabel && hasRejectLabel
}

func normalizeChoiceToken(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	return strings.NewReplacer("_", "", "-", "", " ", "").Replace(value)
}

func isApprovalLabel(value string) bool {
	value = strings.TrimSpace(value)
	return value == "确认" || value == "同意" || value == "批准" || value == "通过"
}

func isRejectionLabel(value string) bool {
	value = strings.TrimSpace(value)
	return value == "拒绝" || value == "拒绝并修改" || value == "驳回" || value == "不同意"
}

type directiveVocabulary struct {
	verbs             []string
	decisions         []string
	negativeBefore    []string
	negativeAfter     []string
	progressAfter     []string
	requireWordBounds bool
}

var approvalDirectiveVocabularies = []directiveVocabulary{
	{
		verbs:          []string{"回复", "发送", "输入", "键入", "回答"},
		decisions:      []string{"确认", "同意", "批准", "拒绝"},
		negativeBefore: []string{"不要", "无需", "不必", "不能", "无法", "禁止", "切勿", "请勿", "不应", "不可"},
		negativeAfter:  []string{"不会", "不能", "无法", "不等于", "不代表", "不视为", "并非", "不是", "不算", "只是", "仅是", "仅仅是"},
		progressAfter:  []string{"即可", "后", "以便", "开始", "继续", "进入", "生成", "执行", "推进", "提交", "完成", "采纳"},
	},
	{
		verbs:             []string{"reply", "send", "type", "enter"},
		decisions:         []string{"confirm", "approve", "reject"},
		negativeBefore:    []string{"do not", "don't", "cannot", "can't", "never", "no need to"},
		negativeAfter:     []string{"does not", "will not", "cannot", "can't", "is not", "only"},
		progressAfter:     []string{"to continue", "and continue", "then", "to proceed", "to start"},
		requireWordBounds: true,
	},
}

// containsPseudoApprovalInstruction recognizes only an explicit chat action
// followed by an approval decision token. It deliberately does not reject
// ordinary copy such as “请确认邮箱地址”, “请回复确认您的邮箱是否正确”, or an
// explanation that replying “确认” does not complete approval.
func containsPseudoApprovalInstruction(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	lower := strings.ToLower(value)
	for _, vocabulary := range approvalDirectiveVocabularies {
		for _, verb := range vocabulary.verbs {
			searchFrom := 0
			for searchFrom < len(lower) {
				relative := strings.Index(lower[searchFrom:], verb)
				if relative < 0 {
					break
				}
				index := searchFrom + relative
				searchFrom = index + len(verb)
				if vocabulary.requireWordBounds && !asciiWordBounded(lower, index, len(verb)) {
					continue
				}
				if negativeDirectiveImmediatelyBefore(lastRunes(lower[:index], 48), vocabulary.negativeBefore) {
					continue
				}
				if directiveTailMatches(lower[searchFrom:], vocabulary) {
					return true
				}
			}
		}
	}
	return false
}

func directiveTailMatches(tail string, vocabulary directiveVocabulary) bool {
	tail = strings.TrimLeftFunc(tail, isApprovalLeadingSeparator)
	quoted := false
	if first, ok := firstRune(tail); ok && isOpeningQuote(first) {
		quoted = true
		tail = strings.TrimLeftFunc(strings.TrimPrefix(tail, string(first)), isApprovalLeadingSeparator)
	}
	for _, decision := range vocabulary.decisions {
		if !strings.HasPrefix(tail, decision) {
			continue
		}
		if vocabulary.requireWordBounds && len(tail) > len(decision) && isASCIIWord(rune(tail[len(decision)])) {
			continue
		}
		remainder := strings.TrimLeftFunc(tail[len(decision):], isApprovalClosingSeparator)
		semanticRemainder := strings.TrimLeftFunc(remainder, isSentenceSeparator)
		if hasAnyPrefix(semanticRemainder, vocabulary.negativeAfter) {
			return false
		}
		if quoted || remainder == "" {
			return true
		}
		first, _ := firstRune(remainder)
		if isSentencePunctuation(first) || hasAnyPrefix(semanticRemainder, vocabulary.progressAfter) {
			return true
		}
	}
	return false
}

func hasAnyPrefix(value string, candidates []string) bool {
	for _, candidate := range candidates {
		if strings.HasPrefix(value, candidate) {
			return true
		}
	}
	return false
}

func negativeDirectiveImmediatelyBefore(value string, candidates []string) bool {
	value = strings.TrimSpace(value)
	for _, candidate := range candidates {
		index := strings.LastIndex(value, candidate)
		if index < 0 {
			continue
		}
		bridge := strings.TrimSpace(value[index+len(candidate):])
		bridge = strings.NewReplacer(
			"只需", "", "please", "", "directly", "", "just", "", "in the chat", "", "by message", "",
			"请", "", "再", "", "直接", "", "仅", "", "只", "", "在聊天中", "", "在对话中", "", "通过聊天", "", "用文字", "", "以文字", "",
		).Replace(bridge)
		bridge = strings.TrimFunc(bridge, func(char rune) bool {
			return unicode.IsSpace(char) || strings.ContainsRune("，,：:*_`", char)
		})
		if bridge == "" {
			return true
		}
	}
	return false
}

func lastRunes(value string, count int) string {
	runes := []rune(value)
	if len(runes) <= count {
		return value
	}
	return string(runes[len(runes)-count:])
}

func firstRune(value string) (rune, bool) {
	for _, char := range value {
		return char, true
	}
	return 0, false
}

func asciiWordBounded(value string, index, size int) bool {
	if index > 0 && isASCIIWord(rune(value[index-1])) {
		return false
	}
	after := index + size
	return after >= len(value) || !isASCIIWord(rune(value[after]))
}

func isASCIIWord(char rune) bool {
	return char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '_'
}

func isApprovalLeadingSeparator(char rune) bool {
	return unicode.IsSpace(char) || strings.ContainsRune(":：*_`", char)
}

func isApprovalClosingSeparator(char rune) bool {
	return unicode.IsSpace(char) || strings.ContainsRune("」』”\"'’*_`", char)
}

func isSentenceSeparator(char rune) bool {
	return isApprovalClosingSeparator(char) || isSentencePunctuation(char)
}

func isSentencePunctuation(char rune) bool {
	return strings.ContainsRune("，。；！？,.!?;:：、", char)
}

func isOpeningQuote(char rune) bool {
	return strings.ContainsRune("「『“\"'‘", char)
}
