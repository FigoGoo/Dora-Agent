package orchestration

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/capability"
)

// LightTemplate 是编排库初始模板的轻直出子集（终版 §2.2/§6.1 第 2 步）：
// 单节点 generate_media(session_deliverable)。深编排模板（视频规划等）
// 仍由五能力流程承载，不在此表。
type LightTemplate struct {
	Key         string   `json:"key"`
	Name        string   `json:"name"`
	Description string   `json:"description"` // 命中判定依据
	Node        PlanNode `json:"node"`        // Arguments 中 <PROMPT> 为参数槽
}

var LightTemplates = []LightTemplate{
	{
		Key: "image_creation", Name: "图片创作",
		Description: "从想法/文本/参考快速生成图片；单张/多张皆为参数，无需故事板。",
		Node:        PlanNode{Kind: NodeKindCapability, ToolKey: capability.GenerateMediaToolKey, Arguments: json.RawMessage(`{"target":"session_deliverable","media_kind":"image","prompt":"<PROMPT>"}`)},
	},
	{
		Key: "video_creation", Name: "视频创作（浅）",
		Description: "从单条想法直出短视频；需要叙事结构/多镜头时改走五阶段深流程。",
		Node:        PlanNode{Kind: NodeKindCapability, ToolKey: capability.GenerateMediaToolKey, Arguments: json.RawMessage(`{"target":"session_deliverable","media_kind":"video","prompt":"<PROMPT>"}`)},
	},
	{
		Key: "music_creation", Name: "音乐创作",
		Description: "按风格/情绪/时长生成音乐。",
		Node:        PlanNode{Kind: NodeKindCapability, ToolKey: capability.GenerateMediaToolKey, Arguments: json.RawMessage(`{"target":"session_deliverable","media_kind":"music","prompt":"<PROMPT>"}`)},
	},
	{
		Key: "audio_creation", Name: "音频创作",
		Description: "文本变语音（旁白/播报）。",
		Node:        PlanNode{Kind: NodeKindCapability, ToolKey: capability.GenerateMediaToolKey, Arguments: json.RawMessage(`{"target":"session_deliverable","media_kind":"audio","prompt":"<PROMPT>"}`)},
	},
}

// LightTemplateCatalogText 生成注入 system prompt 的目录（M2 v1 形态：
// 目录整体注入上下文，Agent 直接判命中）。
func LightTemplateCatalogText() string {
	var builder strings.Builder
	builder.WriteString("轻直出模板目录（需求命中其一且无叙事结构要求时，直接调用 generate_media，参数按模板骨架补 prompt；深需求走五阶段）：\n")
	for _, template := range LightTemplates {
		builder.WriteString(fmt.Sprintf("- %s（%s）：%s 参数骨架 %s\n", template.Key, template.Name, template.Description, string(template.Node.Arguments)))
	}
	return builder.String()
}
