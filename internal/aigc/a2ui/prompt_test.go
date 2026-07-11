package a2ui

import (
	"strings"
	"testing"
)

func TestAgentInstructionTeachesOnlyActionProtocol(t *testing.T) {
	instruction := AgentInstruction()
	if !strings.Contains(instruction, `{"a2ui_version":"1.0","actions":[`) {
		t.Fatalf("AgentInstruction() lost action envelope example: %s", instruction)
	}
	for _, required := range []string{"原生 Tool Calling", "不要用文本、XML、DSML", "json.Unmarshal"} {
		if !strings.Contains(instruction, required) {
			t.Fatalf("AgentInstruction() should mention structured output requirement %q: %s", required, instruction)
		}
	}
	for _, forbidden := range []string{"response_format.type=json_object", "a2ui_events", "render_events", "a2ui_type", "a2ui_hint", "surface_update", "data_model_update"} {
		if strings.Contains(instruction, forbidden) {
			t.Fatalf("AgentInstruction() contains forbidden legacy or tool UI field %q: %s", forbidden, instruction)
		}
	}
}

func TestAgentInstructionForcesProductIntakeAsPureJSON(t *testing.T) {
	instruction := AgentInstruction()
	for _, required := range []string{
		"电商广告视频",
		"商品宣传短片",
		"brief-intake",
		"产品名称/品类",
		"只输出一个 JSON 对象",
		"禁止把 A2UI JSON 放进 Markdown",
		"禁止使用 HTML",
		"details",
	} {
		if !strings.Contains(instruction, required) {
			t.Fatalf("AgentInstruction() should contain %q: %s", required, instruction)
		}
	}
}

func TestAgentInstructionKeepsDurableApprovalOutOfChatForms(t *testing.T) {
	instruction := AgentInstruction()
	for _, required := range []string{
		"Capability 返回 waiting_user",
		"候选素材只由左侧故事板统一确认",
		"权威卡携带 approval_id",
		"“确认/拒绝” SingleChoice",
		"当前协议没有独立 Button 组件",
		"请回复确认",
		"普通聊天文字“确认”只是一条 UserMessage",
		"永远不代表审批已提交",
		"不要为每个候选素材追加聊天审核卡",
	} {
		if !strings.Contains(instruction, required) {
			t.Fatalf("AgentInstruction() should contain %q: %s", required, instruction)
		}
	}
}
