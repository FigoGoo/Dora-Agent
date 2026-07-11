package a2ui

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestModelAuthoredPolicyRejectsPseudoApprovalCopy(t *testing.T) {
	tests := []struct {
		name      string
		component string
		message   string
	}{
		{name: "markdown reply confirm", component: `{"Markdown":{"value":"✅ 如果满意，请回复「确认」开始生成素材"}}`},
		{name: "text send agree", component: `{"Text":{"value":"请发送“同意”继续下一阶段"}}`},
		{name: "english type reject", component: `{"Text":{"value":"Type \"reject\" to continue."}}`},
		{name: "card message input reject", component: `{"Text":{"value":"故事板详情"}}`, message: "输入拒绝即可返回修改"},
		{name: "unrelated negation before directive", component: `{"Text":{"value":"如果无法修改，请回复确认继续生成"}}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			content := modelPolicyEnvelope(test.component, test.message, `{}`)
			envelope, ok := ParseActionEnvelopeContent(content)
			if !ok {
				t.Fatalf("test envelope is not structurally valid: %s", content)
			}
			if err := ValidateModelAuthoredActionEnvelope(envelope); !errors.Is(err, ErrModelAuthoredApproval) {
				t.Fatalf("ValidateModelAuthoredActionEnvelope() error = %v", err)
			}
			if _, ok := ParseModelAuthoredActionEnvelopeContent(content); ok {
				t.Fatal("model parser accepted pseudo Approval copy")
			}
		})
	}

	boundContent := modelPolicyEnvelope(`{"Markdown":{"dataKey":"details"}}`, "", `{"details":"请回复「确认」继续生成"}`)
	if _, ok := ParseModelAuthoredActionEnvelopeContent(boundContent); ok {
		t.Fatal("model parser accepted pseudo Approval copy referenced through dataKey")
	}
	titleEnvelope, ok := ParseActionEnvelopeContent(modelPolicyEnvelope(`{"Text":{"value":"故事板详情"}}`, "", `{}`))
	if !ok {
		t.Fatal("title test envelope is not structurally valid")
	}
	titleEnvelope.Actions[0].Card.Title = "请回复“确认”继续"
	if err := ValidateModelAuthoredActionEnvelope(titleEnvelope); !errors.Is(err, ErrModelAuthoredApproval) {
		t.Fatalf("pseudo Approval title error = %v", err)
	}
}

func TestModelAuthoredPolicyRejectsApprovalLikeSingleChoiceAndApprovalID(t *testing.T) {
	tests := map[string]string{
		"decision key":    `{"SingleChoice":{"key":"decision","options":[{"value":"yes","label":"是"},{"value":"no","label":"否"}]}}`,
		"status values":   `{"SingleChoice":{"key":"review","options":[{"value":"approved","label":"继续"},{"value":"rejected","label":"修改"}]}}`,
		"semantic labels": `{"SingleChoice":{"key":"review","options":[{"value":"yes","label":"确认"},{"value":"no","label":"拒绝并修改"}]}}`,
	}
	for name, component := range tests {
		t.Run(name, func(t *testing.T) {
			envelope, ok := ParseActionEnvelopeContent(modelPolicyEnvelope(component, "", `{}`))
			if !ok {
				t.Fatal("test envelope is not structurally valid")
			}
			if err := ValidateModelAuthoredActionEnvelope(envelope); !errors.Is(err, ErrModelAuthoredApproval) {
				t.Fatalf("ValidateModelAuthoredActionEnvelope() error = %v", err)
			}
		})
	}

	content := modelPolicyEnvelope(`{"Text":{"value":"故事板已生成"}}`, "", `{"nested":{"approval_id":"approval-1"}}`)
	envelope, ok := ParseActionEnvelopeContent(content)
	if !ok {
		t.Fatal("authoritative-shaped test envelope is not structurally valid")
	}
	if err := ValidateModelAuthoredActionEnvelope(envelope); !errors.Is(err, ErrModelAuthoredApproval) {
		t.Fatalf("model-authored approval_id error = %v", err)
	}
	// The generic parser remains available to the trusted system publisher.
	if _, ok := ParseActionEnvelopeContent(content); !ok {
		t.Fatal("generic protocol parser must not reject system-published Approval data")
	}
}

func TestModelAuthoredPolicyPreservesDescriptionsAndNonApprovalQuestions(t *testing.T) {
	allowed := []string{
		"### 故事板详情\n\n故事板已生成，请在系统审核卡中提交决定。",
		"普通聊天回复“确认”不会完成审批。",
		"请勿回复“确认”；请使用系统审核卡。",
		"请勿在聊天中直接回复确认；请使用系统审核卡。",
		"请回复确认您的邮箱地址是否正确。",
		"请确认产品名称和投放平台。",
		"Replying \"confirm\" does not complete approval.",
	}
	for _, text := range allowed {
		content := modelPolicyEnvelope(`{"Markdown":{"value":`+mustJSONString(t, text)+`}}`, "", `{}`)
		if _, ok := ParseModelAuthoredActionEnvelopeContent(content); !ok {
			t.Fatalf("model policy rejected legitimate copy %q", text)
		}
	}

	genericChoice := `{"SingleChoice":{"key":"visual_style","label":"视觉风格","options":[{"value":"tech","label":"科技感"},{"value":"warm","label":"温暖生活感"}]}}`
	if _, ok := ParseModelAuthoredActionEnvelopeContent(modelPolicyEnvelope(genericChoice, "", `{}`)); !ok {
		t.Fatal("model policy rejected a non-approval SingleChoice")
	}
}

func modelPolicyEnvelope(component, message, data string) string {
	messageField := ""
	if message != "" {
		messageField = `,"message":` + strconvQuote(message)
	}
	return `{"a2ui_version":"1.0","actions":[{"type":"append_card","surface":"chat","card_id":"result","card":{"root":"root"` + messageField + `,"data":` + data + `,"components":[{"id":"root","component":{"Card":{"children":["content"]}}},{"id":"content","component":` + component + `}]}}]}`
}

func mustJSONString(t *testing.T, value string) string {
	t.Helper()
	return strconvQuote(value)
}

func strconvQuote(value string) string {
	raw, _ := json.Marshal(value)
	return string(raw)
}
