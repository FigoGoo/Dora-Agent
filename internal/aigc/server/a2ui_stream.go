package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/schema"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/a2ui"
	aigctools "github.com/FigoGoo/Dora-Agent/internal/aigc/tools"
)

const a2uiRootComponentID = "root-col"
const a2uiProgressSurfaceID = "agent-progress"
const a2uiStageConfirmationSurfaceID = "stage-confirmation"

type chatA2UISurface struct {
	surfaceID string
	children  []string
	nextIndex int
}

func newChatA2UISurface(sessionID string) *chatA2UISurface {
	return &chatA2UISurface{surfaceID: "chat-" + strings.TrimSpace(sessionID)}
}

func (s *chatA2UISurface) beginEvent() aigctools.RenderEventHint {
	return aigctools.RenderEventHint{
		Event:        a2ui.EventBeginRendering,
		SurfaceID:    s.surfaceID,
		DataModelKey: "root",
		Payload: map[string]any{
			"surfaceId":  s.surfaceID,
			"surface_id": s.surfaceID,
			"root":       a2uiRootComponentID,
		},
	}
}

func (s *chatA2UISurface) eventsFromAgentEvent(event AgentEvent) []aigctools.RenderEventHint {
	switch event.Event {
	case a2ui.EventChatDelta:
		return s.assistantEvents(event)
	case a2ui.EventChatMessage:
		return s.cardEvents("Agent", payloadText(event.Payload))
	case a2ui.EventToolProgress:
		return s.toolProgressSurfaceEvents(event)
	default:
		return []aigctools.RenderEventHint{{
			Event:        event.Event,
			SurfaceID:    event.SurfaceID,
			DataModelKey: event.DataModelKey,
			Payload:      event.Payload,
		}}
	}
}

func (s *chatA2UISurface) assistantEvents(event AgentEvent) []aigctools.RenderEventHint {
	content := strings.TrimSpace(event.AssistantText)
	if content == "" {
		content = payloadText(event.Payload)
	}
	parsed := parseA2UIContent(content)
	if parsed.found {
		out := make([]aigctools.RenderEventHint, 0, len(parsed.events)+2)
		out = append(out, s.messageCardEvents(schema.Assistant, parsed.displayText)...)
		out = append(out, parsed.events...)
		return out
	}
	if events := s.productBriefRequestEvents(content); len(events) > 0 {
		return events
	}
	if events := s.stageConfirmationEvents(content); len(events) > 0 {
		return events
	}
	return s.messageCardEvents(schema.Assistant, content)
}

func (s *chatA2UISurface) toolProgressSurfaceEvents(event AgentEvent) []aigctools.RenderEventHint {
	progress := summarizeToolProgress(event)
	if progress.title == "" {
		return nil
	}
	status := progress.status
	if status == "" {
		status = "running"
	}
	description := progress.description
	if description == "" {
		description = progress.title
	}

	return []aigctools.RenderEventHint{{
		Event:        a2ui.EventSurfaceUpdate,
		SurfaceID:    a2uiProgressSurfaceID,
		DataModelKey: "progress",
		Payload: map[string]any{
			"surfaceId":  a2uiProgressSurfaceID,
			"surface_id": a2uiProgressSurfaceID,
			"root":       "progress-root",
			"title":      "执行进度",
			"status":     status,
			"components": []map[string]any{
				a2uiComponent("progress-root", "Card", map[string]any{"children": []string{"progress-title", "progress-summary", "progress-steps"}}),
				a2uiComponent("progress-title", "Text", map[string]any{"value": "执行进度", "usageHint": "title"}),
				a2uiComponent("progress-summary", "Text", map[string]any{"value": description, "usageHint": "body"}),
				a2uiComponent("progress-steps", "VerticalSteps", map[string]any{"steps": []map[string]any{
					{"title": "Agent 分析", "status": "done"},
					{"title": progress.title, "status": status, "description": description},
				}}),
			},
		},
	}}
}

func (s *chatA2UISurface) productBriefRequestEvents(content string) []aigctools.RenderEventHint {
	if !looksLikeProductBriefRequest(content) {
		return nil
	}
	return []aigctools.RenderEventHint{{
		Event:        a2ui.EventSurfaceUpdate,
		SurfaceID:    "brief-intake",
		DataModelKey: "brief",
		Payload: map[string]any{
			"surfaceId":    "brief-intake",
			"surface_id":   "brief-intake",
			"root":         "root",
			"title":        "补充产品信息",
			"message":      "请补充商品宣传短片的基础信息，我收到后继续产品分析。",
			"submit_label": "提交信息",
			"components": []map[string]any{
				a2uiComponent("root", "Card", map[string]any{"children": []string{"title", "product", "selling", "brand", "platform", "specs", "style", "image", "steps"}}),
				a2uiComponent("title", "Text", map[string]any{"value": "请补充商品宣传短片信息", "usageHint": "title"}),
				a2uiComponent("product", "TextInput", map[string]any{"key": "product_name", "label": "产品名称/品类", "required": true, "placeholder": "例如：智能手表 Pro Max"}),
				a2uiComponent("selling", "TextInput", map[string]any{"key": "selling_points", "label": "核心卖点", "required": true, "multiline": true, "placeholder": "例如：超长续航30天，高清通话，IP68防水"}),
				a2uiComponent("brand", "TextInput", map[string]any{"key": "brand_name", "label": "品牌名称", "required": true, "placeholder": "例如：TechBrand"}),
				a2uiComponent("platform", "MultiChoice", map[string]any{
					"key":   "platforms",
					"label": "目标平台",
					"options": []map[string]any{
						{"value": "douyin", "label": "抖音"},
						{"value": "xiaohongshu", "label": "小红书"},
						{"value": "taobao", "label": "淘宝"},
						{"value": "website", "label": "官网"},
						{"value": "youtube", "label": "YouTube"},
					},
				}),
				a2uiComponent("specs", "TextInput", map[string]any{"key": "duration_ratio", "label": "视频比例与时长", "required": true, "placeholder": "例如：9:16 竖屏 15秒 / 16:9 横屏 30秒"}),
				a2uiComponent("style", "SingleChoice", map[string]any{
					"key":   "visual_style",
					"label": "视觉风格",
					"options": []map[string]any{
						{"value": "tech", "label": "高级科技感"},
						{"value": "warm", "label": "温暖生活感"},
						{"value": "minimal", "label": "极简纯净"},
						{"value": "cool", "label": "潮酷街头"},
						{"value": "luxury", "label": "奢华质感"},
					},
				}),
				a2uiComponent("image", "TextInput", map[string]any{"key": "image_info", "label": "产品图片/Logo 信息", "required": false, "placeholder": "可选：例如已有3张产品外观图和1张Logo图"}),
				a2uiComponent("steps", "VerticalSteps", map[string]any{"steps": []map[string]any{
					{"title": "Agent 分析", "status": "running"},
					{"title": "资产配置", "status": "pending"},
					{"title": "故事板规划", "status": "pending"},
				}}),
			},
		},
	}}
}

func (s *chatA2UISurface) stageConfirmationEvents(content string) []aigctools.RenderEventHint {
	if !looksLikeStageConfirmation(content) {
		return nil
	}
	return []aigctools.RenderEventHint{
		{
			Event:        a2ui.EventSurfaceUpdate,
			SurfaceID:    a2uiStageConfirmationSurfaceID,
			DataModelKey: "confirmation",
			Payload: map[string]any{
				"surfaceId":    a2uiStageConfirmationSurfaceID,
				"surface_id":   a2uiStageConfirmationSurfaceID,
				"root":         "confirm-root",
				"title":        "请确认当前阶段结果",
				"message":      "确认后我会继续下一阶段；需要调整也可以直接写修改意见。",
				"submit_label": "提交确认",
				"components": []map[string]any{
					a2uiComponent("confirm-root", "Card", map[string]any{"children": []string{"confirm-title", "confirm-summary", "confirm-decision", "confirm-note", "confirm-steps"}}),
					a2uiComponent("confirm-title", "Text", map[string]any{"value": "请确认当前阶段结果", "usageHint": "title"}),
					a2uiComponent("confirm-summary", "Text", map[string]any{"dataKey": "summary", "usageHint": "body"}),
					a2uiComponent("confirm-decision", "SingleChoice", map[string]any{
						"key":      "decision",
						"label":    "处理方式",
						"required": true,
						"options": []map[string]any{
							{"value": "confirm", "label": "确认，继续下一阶段"},
							{"value": "revise", "label": "需要调整"},
						},
					}),
					a2uiComponent("confirm-note", "TextInput", map[string]any{"key": "note", "label": "调整意见", "required": false, "multiline": true, "placeholder": "可选：写下需要修改的标题、风格、时长、品牌信息等"}),
					a2uiComponent("confirm-steps", "VerticalSteps", map[string]any{"steps": stageConfirmationSteps(content)}),
				},
			},
		},
		{
			Event:        a2ui.EventDataModelUpdate,
			SurfaceID:    a2uiStageConfirmationSurfaceID,
			DataModelKey: "confirmation",
			Payload: map[string]any{
				"surfaceId":  a2uiStageConfirmationSurfaceID,
				"surface_id": a2uiStageConfirmationSurfaceID,
				"contents": []map[string]any{
					{"key": "summary", "value": content, "valueString": content},
				},
			},
		},
	}
}

func (s *chatA2UISurface) messageCardEvents(role schema.RoleType, content string) []aigctools.RenderEventHint {
	return s.cardEvents(roleToA2UILabel(role), content)
}

func (s *chatA2UISurface) cardEvents(label string, content string) []aigctools.RenderEventHint {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}

	idx := s.nextIndex
	s.nextIndex++

	cardID := fmt.Sprintf("msg-%d-card", idx)
	colID := fmt.Sprintf("msg-%d-col", idx)
	roleID := fmt.Sprintf("msg-%d-role", idx)
	contentID := fmt.Sprintf("msg-%d-content", idx)
	dataKey := fmt.Sprintf("%s/msg-%d", s.surfaceID, idx)
	s.children = append(s.children, cardID)

	return []aigctools.RenderEventHint{
		{
			Event:        a2ui.EventSurfaceUpdate,
			SurfaceID:    s.surfaceID,
			DataModelKey: "components",
			Payload: map[string]any{
				"surfaceId":  s.surfaceID,
				"surface_id": s.surfaceID,
				"components": []map[string]any{
					columnComponent(a2uiRootComponentID, s.children),
					cardComponent(cardID, []string{colID}),
					columnComponent(colID, []string{roleID, contentID}),
					textComponent(roleID, label, "", "caption"),
					textComponent(contentID, "", dataKey, "body"),
				},
			},
		},
		{
			Event:        a2ui.EventDataModelUpdate,
			SurfaceID:    s.surfaceID,
			DataModelKey: dataKey,
			Payload: map[string]any{
				"surfaceId":  s.surfaceID,
				"surface_id": s.surfaceID,
				"contents": []map[string]any{
					{
						"key":         dataKey,
						"valueString": content,
						"value":       content,
					},
				},
			},
		},
	}
}

func columnComponent(id string, children []string) map[string]any {
	return map[string]any{
		"id": id,
		"component": map[string]any{
			"Column": map[string]any{"children": append([]string(nil), children...)},
		},
	}
}

func cardComponent(id string, children []string) map[string]any {
	return map[string]any{
		"id": id,
		"component": map[string]any{
			"Card": map[string]any{"children": append([]string(nil), children...)},
		},
	}
}

func textComponent(id string, value string, dataKey string, usageHint string) map[string]any {
	return map[string]any{
		"id": id,
		"component": map[string]any{
			"Text": map[string]any{
				"value":     value,
				"dataKey":   dataKey,
				"usageHint": usageHint,
			},
		},
	}
}

func a2uiComponent(id string, kind string, value map[string]any) map[string]any {
	return map[string]any{
		"id":        id,
		"component": map[string]any{kind: value},
	}
}

func payloadText(payload any) string {
	values := payloadMap(payload)
	for _, key := range []string{"text", "delta", "content", "message", "title"} {
		if value := payloadString(values, key); value != "" {
			return value
		}
	}
	return ""
}

func looksLikeProductBriefRequest(content string) bool {
	content = strings.ToLower(strings.TrimSpace(content))
	if content == "" {
		return false
	}
	hasProduct := strings.Contains(content, "产品") || strings.Contains(content, "商品")
	hasSellingPoint := strings.Contains(content, "核心卖点") || strings.Contains(content, "卖点")
	hasPlatformOrStyle := strings.Contains(content, "目标平台") || strings.Contains(content, "视觉风格") || strings.Contains(content, "视频时长")
	asksUser := strings.Contains(content, "告诉我") || strings.Contains(content, "提供") || strings.Contains(content, "补充") || strings.Contains(content, "填写")
	return hasProduct && hasSellingPoint && hasPlatformOrStyle && asksUser
}

func looksLikeStageConfirmation(content string) bool {
	content = strings.ToLower(strings.TrimSpace(content))
	if content == "" {
		return false
	}
	hasConfirmation := strings.Contains(content, "请您确认") || strings.Contains(content, "请确认") || strings.Contains(content, "是否符合")
	hasStageArtifact := strings.Contains(content, "final_video_spec") ||
		strings.Contains(content, "视频规格") ||
		strings.Contains(content, "故事板") ||
		strings.Contains(content, "参考图") ||
		strings.Contains(content, "关键帧")
	return hasConfirmation && hasStageArtifact
}

func stageConfirmationSteps(content string) []map[string]any {
	content = strings.ToLower(content)
	switch {
	case strings.Contains(content, "final_video_spec") || strings.Contains(content, "视频规格"):
		return []map[string]any{
			{"title": "编写 Final_Video_Spec", "status": "done"},
			{"title": "等待用户确认", "status": "running"},
			{"title": "生成故事板", "status": "pending"},
		}
	case strings.Contains(content, "故事板"):
		return []map[string]any{
			{"title": "生成故事板", "status": "done"},
			{"title": "等待用户确认", "status": "running"},
			{"title": "配置元素资产", "status": "pending"},
		}
	case strings.Contains(content, "参考图") || strings.Contains(content, "关键帧"):
		return []map[string]any{
			{"title": "生成素材", "status": "done"},
			{"title": "等待用户确认", "status": "running"},
			{"title": "继续生成镜头", "status": "pending"},
		}
	default:
		return []map[string]any{
			{"title": "当前阶段完成", "status": "done"},
			{"title": "等待用户确认", "status": "running"},
		}
	}
}

func renderEventsFromA2UIEnvelope(content string) []aigctools.RenderEventHint {
	return parseA2UIContent(content).events
}

func displayTextWithoutA2UIEnvelope(content string) string {
	return parseA2UIContent(content).displayText
}

type parsedA2UIContent struct {
	events      []aigctools.RenderEventHint
	displayText string
	found       bool
}

func parseA2UIContent(content string) parsedA2UIContent {
	content = strings.TrimSpace(content)
	if content == "" {
		return parsedA2UIContent{}
	}
	stripped := trimJSONCodeFence(content)

	if events, found := parseA2UIJSON([]byte(stripped)); found {
		return parsedA2UIContent{events: events, found: true}
	}

	events, start, end, found := findEmbeddedA2UIJSON(stripped)
	if !found {
		return parsedA2UIContent{displayText: content}
	}
	displayText := joinDisplayTextAroundEnvelope(stripped[:start], stripped[end:])
	return parsedA2UIContent{events: events, displayText: displayText, found: true}
}

func parseA2UIJSON(raw []byte) ([]aigctools.RenderEventHint, bool) {
	var envelope struct {
		Events []aigctools.RenderEventHint `json:"a2ui_events"`
	}
	if err := json.Unmarshal(raw, &envelope); err == nil {
		var probe map[string]any
		if probeErr := json.Unmarshal(raw, &probe); probeErr == nil {
			if _, ok := probe["a2ui_events"]; ok {
				return normalizeA2UIEvents(envelope.Events), true
			}
		}
	}

	var single aigctools.RenderEventHint
	if err := json.Unmarshal(raw, &single); err == nil && strings.HasPrefix(single.Event, "a2ui.") {
		return normalizeA2UIEvents([]aigctools.RenderEventHint{single}), true
	}
	return nil, false
}

func findEmbeddedA2UIJSON(content string) ([]aigctools.RenderEventHint, int, int, bool) {
	for start := 0; start < len(content); start++ {
		idx := strings.IndexByte(content[start:], '{')
		if idx < 0 {
			return nil, 0, 0, false
		}
		idx += start

		var raw json.RawMessage
		dec := json.NewDecoder(strings.NewReader(content[idx:]))
		if err := dec.Decode(&raw); err == nil {
			if events, found := parseA2UIJSON(bytes.TrimSpace(raw)); found {
				return events, idx, idx + int(dec.InputOffset()), true
			}
		}
		start = idx
	}
	return nil, 0, 0, false
}

func joinDisplayTextAroundEnvelope(prefix string, suffix string) string {
	prefix = strings.TrimRight(strings.TrimSpace(prefix), ":：,，")
	suffix = strings.TrimLeft(strings.TrimSpace(suffix), ":：,，")
	if prefix == "" {
		return suffix
	}
	if suffix == "" {
		return prefix
	}
	return prefix + "\n\n" + suffix
}

func normalizeA2UIEvents(events []aigctools.RenderEventHint) []aigctools.RenderEventHint {
	out := make([]aigctools.RenderEventHint, 0, len(events))
	for _, event := range events {
		event.Event = strings.TrimSpace(event.Event)
		if !strings.HasPrefix(event.Event, "a2ui.") && event.Event != a2ui.EventStoryboardPatch {
			continue
		}
		if event.SurfaceID == "" {
			event.SurfaceID = payloadString(payloadMap(event.Payload), "surface_id")
		}
		if event.SurfaceID == "" {
			event.SurfaceID = payloadString(payloadMap(event.Payload), "surfaceId")
		}
		out = append(out, event)
	}
	return out
}

func trimJSONCodeFence(content string) string {
	if !strings.HasPrefix(content, "```") {
		return content
	}
	lines := strings.Split(content, "\n")
	if len(lines) < 3 {
		return content
	}
	if !strings.HasPrefix(strings.TrimSpace(lines[len(lines)-1]), "```") {
		return content
	}
	return strings.TrimSpace(strings.Join(lines[1:len(lines)-1], "\n"))
}

func toolProgressCardText(event AgentEvent) (string, string) {
	values := payloadMap(event.Payload)
	if event.Message != nil && event.Message.Role == schema.Assistant && len(event.Message.ToolCalls) > 0 {
		parts := make([]string, 0, len(event.Message.ToolCalls))
		for _, call := range event.Message.ToolCalls {
			if call.Function.Name == "" {
				continue
			}
			text := call.Function.Name
			if call.Function.Arguments != "" {
				text += "\n" + truncateRunes(call.Function.Arguments, 400)
			}
			parts = append(parts, text)
		}
		return "tool call", strings.Join(parts, "\n\n")
	}

	toolName := payloadString(values, "tool_name")
	content := payloadText(event.Payload)
	if event.Message != nil && event.Message.Role == schema.Tool {
		if toolName == "" {
			toolName = event.Message.ToolName
		}
		if content == "" {
			content = event.Message.Content
		}
	}
	if content == "" && toolName != "" {
		content = toolName
	}
	if content != "" && toolName != "" && !strings.Contains(content, toolName) {
		content = toolName + "\n" + content
	}
	if content == "" {
		return "", ""
	}
	return "tool progress", truncateRunes(content, 500)
}

type toolProgressSummary struct {
	title       string
	description string
	status      string
}

func summarizeToolProgress(event AgentEvent) toolProgressSummary {
	if event.Message != nil && event.Message.Role == schema.Assistant && len(event.Message.ToolCalls) > 0 {
		names := toolCallNames(event.Message.ToolCalls)
		if len(names) == 0 {
			return toolProgressSummary{title: "业务工具执行中", description: "Agent 正在调用业务工具。", status: "running"}
		}
		return toolProgressSummary{
			title:       strings.Join(names, "、") + " 执行中",
			description: "Agent 正在调用 " + strings.Join(names, "、") + "。",
			status:      "running",
		}
	}

	toolName := ""
	content := payloadText(event.Payload)
	values := payloadMap(event.Payload)
	if event.Message != nil && event.Message.Role == schema.Tool {
		toolName = strings.TrimSpace(event.Message.ToolName)
		content = event.Message.Content
	}
	if toolName == "" {
		toolName = payloadString(values, "tool_name")
	}
	if toolName == "" {
		toolName = "业务工具"
	}

	result := summarizeToolResult(toolName, content)
	if result.title != "" {
		return result
	}
	return toolProgressSummary{
		title:       toolName + " 已返回结果",
		description: toolName + " 已返回业务结果，Agent 将继续分析下一步。",
		status:      "done",
	}
}

func summarizeToolResult(toolName string, content string) toolProgressSummary {
	content = strings.TrimSpace(content)
	if content == "" {
		return toolProgressSummary{}
	}
	var envelope struct {
		Status             string                       `json:"status"`
		ArtifactIDs        []string                     `json:"artifact_ids"`
		PatchEventIDs      []string                     `json:"patch_event_ids"`
		NextConfirmationID string                       `json:"next_confirmation_id"`
		Error              *aigctools.ToolErrorEnvelope `json:"error"`
	}
	if err := json.Unmarshal([]byte(content), &envelope); err != nil {
		return toolProgressSummary{}
	}
	if envelope.Error != nil {
		if envelope.Error.ToolKey != "" {
			toolName = envelope.Error.ToolKey
		}
		message := strings.TrimSpace(envelope.Error.UserMessage)
		if message == "" {
			message = toolName + " 执行失败，需要 Agent 重新组织参数。"
		}
		return toolProgressSummary{
			title:       toolName + " 需要修正",
			description: message,
			status:      "failed",
		}
	}
	switch strings.TrimSpace(envelope.Status) {
	case aigctools.ToolStatusOK:
		description := toolName + " 已完成。"
		if len(envelope.ArtifactIDs) > 0 {
			description = fmt.Sprintf("%s 已完成，生成 %d 个资产。", toolName, len(envelope.ArtifactIDs))
		}
		return toolProgressSummary{title: toolName + " 完成", description: description, status: "done"}
	case aigctools.ToolStatusQueued:
		description := toolName + " 已创建异步生成任务。"
		if envelope.NextConfirmationID != "" {
			description = toolName + " 已暂停，等待用户确认。"
		}
		return toolProgressSummary{title: toolName + " 已排队", description: description, status: "running"}
	case aigctools.ToolStatusError:
		return toolProgressSummary{title: toolName + " 需要修正", description: toolName + " 执行失败，需要 Agent 重新组织参数。", status: "failed"}
	default:
		return toolProgressSummary{}
	}
}

func toolCallNames(calls []schema.ToolCall) []string {
	seen := map[string]bool{}
	names := make([]string, 0, len(calls))
	for _, call := range calls {
		name := strings.TrimSpace(call.Function.Name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	return names
}

func roleToA2UILabel(role schema.RoleType) string {
	switch role {
	case schema.User:
		return "You"
	case schema.Assistant:
		return "Agent"
	case schema.Tool:
		return "Tool"
	case schema.System:
		return "System"
	default:
		if role != "" {
			return string(role)
		}
		return "Agent"
	}
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "..."
}
