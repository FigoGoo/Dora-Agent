package server

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/a2ui"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/generation"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/session"
	aigctools "github.com/FigoGoo/Dora-Agent/internal/aigc/tools"
)

// JobWakeupHandler 接收后台生成任务完成事件，并触发 Agent 自动续跑。
type JobWakeupHandler interface {
	Wakeup(ctx context.Context, event session.JobWakeupEvent) error
}

// GenerationEventPublisher 把 worker 事件发布到 A2UI broker，并在终态时唤醒 Agent。
type GenerationEventPublisher struct {
	Broker a2ui.EventPublisher
	Wakeup JobWakeupHandler
	Now    func() time.Time
}

// Publish 标准化 worker 事件时间戳，必要时异步唤醒 Agent，然后写入 SSE broker。
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
	a2uiEvent := workerEventAsA2UIEvent(event, createdAt)
	if strings.TrimSpace(a2uiEvent.Event) == "" {
		return nil
	}
	return p.Broker.Publish(ctx, a2uiEvent)
}

// jobWakeupFromWorkerEvent 从 job.status 终态事件中提取 Agent 续跑所需上下文。
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

// a2uiPayload 把 worker 领域 payload 转成前端可消费的 A2UI payload。
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

// workerEventAsA2UIEvent 把后台任务事件桥接成统一 Action 协议；未知 worker 事件不再透传给前端。
func workerEventAsA2UIEvent(event generation.WorkerEvent, createdAt time.Time) a2ui.SSEEvent {
	switch strings.TrimSpace(event.Event) {
	case generation.EventJobStatus:
		payload, _ := event.Payload.(generation.JobStatusPayload)
		return a2ui.SSEEvent{
			ID:        event.ID,
			SessionID: event.SessionID,
			Event:     a2ui.EventAction,
			Payload:   toolRunUpdateEnvelope(payload),
			CreatedAt: createdAt,
		}
	case generation.EventStoryboardPatch:
		payload := a2uiPayload(event.Payload)
		dataModelKey := strings.TrimSpace(event.DataModelKey)
		if dataModelKey == "" {
			dataModelKey = "storyboard"
		}
		return a2ui.SSEEvent{
			ID:        event.ID,
			SessionID: event.SessionID,
			Event:     a2ui.EventAction,
			Payload:   storyboardPatchUpdateEnvelope(dataModelKey, payload),
			CreatedAt: createdAt,
		}
	default:
		return a2ui.SSEEvent{}
	}
}

// toolRunUpdateEnvelope 使用固定 card_id 更新同一个工具运行卡，避免相同任务重复出现。
func toolRunUpdateEnvelope(payload generation.JobStatusPayload) a2ui.ActionEnvelope {
	key := toolRunDataModelKey(payload)
	return a2ui.ActionEnvelope{
		Version: a2ui.Version1,
		Actions: []a2ui.Action{{
			Type:    a2ui.ActionUpdateCard,
			Surface: "tool_runs",
			Target:  &a2ui.ActionTarget{Surface: "tool_runs", CardID: key},
			Payload: map[string]any{
				"tool_run": toolRunViewModel(payload),
			},
		}},
	}
}

// storyboardPatchUpdateEnvelope 把故事板 JSON Patch 放进 update_card，由前端按 ops 更新左侧故事板。
func storyboardPatchUpdateEnvelope(dataModelKey string, payload any) a2ui.ActionEnvelope {
	dataModelKey = strings.TrimSpace(dataModelKey)
	if dataModelKey == "" {
		dataModelKey = "storyboard"
	}
	return a2ui.ActionEnvelope{
		Version: a2ui.Version1,
		Actions: []a2ui.Action{{
			Type:    a2ui.ActionUpdateCard,
			Surface: "storyboard",
			Target:  &a2ui.ActionTarget{Surface: "storyboard", CardID: dataModelKey},
			Payload: map[string]any{
				"patch": payload,
			},
		}},
	}
}

// toolRunDataModelKey 生成工具运行卡的稳定 key，确保同一任务状态只更新同一张卡。
func toolRunDataModelKey(payload generation.JobStatusPayload) string {
	if isGenerationProvider(payload.Provider) {
		return mediaToolRunDataModelKey
	}
	id := strings.TrimSpace(payload.ToolCallID)
	if id == "" {
		id = strings.TrimSpace(payload.JobID)
	}
	if id == "" {
		return "tool_run"
	}
	return "tool_run:" + id
}

// toolRunViewModel 把任务状态整理成前端工具进度组件使用的视图模型。
func toolRunViewModel(payload generation.JobStatusPayload) map[string]any {
	displayName := toolDisplayName(payload.Provider)
	toolKey := payload.Provider
	runKey := ""
	if isGenerationProvider(payload.Provider) {
		displayName = mediaToolRunDisplayName
		toolKey = aigctools.MediaGeneratorToolKey
		runKey = aigctools.MediaGeneratorToolKey
	}
	stageLabel := stageDisplayName(payload.StageKey, payload.TargetType, payload.TargetID)
	nodeDisplayName := stageLabel
	if isGenerationProvider(payload.Provider) {
		nodeDisplayName = mediaProviderNodeDisplayName(payload.Provider, stageLabel)
	}
	summary := stageLabel
	if isGenerationProvider(payload.Provider) {
		summary = nodeDisplayName
	}
	if summary == "" {
		summary = fmt.Sprintf("%s %s", displayName, statusDisplayName(payload.Status))
	}
	nodeKey := strings.TrimSpace(payload.JobID)
	if nodeKey == "" {
		nodeKey = strings.TrimSpace(payload.StageKey)
	}
	return map[string]any{
		"job_id":           payload.JobID,
		"session_id":       payload.SessionID,
		"tool_call_id":     payload.ToolCallID,
		"tool_key":         toolKey,
		"run_key":          runKey,
		"provider":         payload.Provider,
		"display_name":     displayName,
		"status":           payload.Status,
		"summary":          summary,
		"target_type":      payload.TargetType,
		"target_id":        payload.TargetID,
		"result_asset_ids": append([]string(nil), payload.ResultAssetIDs...),
		"error_code":       payload.ErrorCode,
		"error_message":    payload.ErrorMessage,
		"nodes": []map[string]any{
			{
				"node_key":     nodeKey,
				"display_name": nodeDisplayName,
				"status":       nodeStatus(payload.Status),
			},
		},
	}
}

const (
	mediaToolRunDataModelKey = "tool_run:media_generator"
	mediaToolRunDisplayName  = "Media Assets"
)

// isGenerationProvider 判断 provider 是否属于媒体生成聚合卡的一部分。
func isGenerationProvider(provider string) bool {
	switch strings.TrimSpace(provider) {
	case generation.ProviderImage2, generation.ProviderSeedance, generation.ProviderAudio:
		return true
	default:
		return false
	}
}

// mediaProviderNodeDisplayName 组合媒体生成 provider 和阶段名，作为步骤节点标题。
func mediaProviderNodeDisplayName(provider string, stageLabel string) string {
	providerName := toolDisplayName(provider)
	stageLabel = strings.TrimSpace(stageLabel)
	if stageLabel == "" {
		return providerName
	}
	return providerName + " · " + stageLabel
}

// toolDisplayName 把 provider key 转成用户可读的工具名称。
func toolDisplayName(provider string) string {
	switch strings.TrimSpace(provider) {
	case generation.ProviderImage2:
		return "Image2"
	case generation.ProviderSeedance:
		return "Seedance 2.0"
	case generation.ProviderAudio:
		return "Audio Generator"
	default:
		if strings.TrimSpace(provider) != "" {
			return strings.TrimSpace(provider)
		}
		return "Tool"
	}
}

// stageDisplayName 把阶段 key 和目标对象拼成用户可读的阶段名称。
func stageDisplayName(stageKey string, targetType string, targetID string) string {
	stageKey = strings.TrimSpace(stageKey)
	if stageKey == "" {
		stageKey = "generate_" + strings.TrimSpace(targetType)
	}
	stageKey = strings.ReplaceAll(stageKey, "_", " ")
	targetID = strings.TrimSpace(targetID)
	if targetID == "" {
		return stageKey
	}
	return stageKey + " · " + targetID
}

// statusDisplayName 把任务状态 key 转成中文状态文案。
func statusDisplayName(status string) string {
	switch strings.TrimSpace(status) {
	case generation.StatusQueued:
		return "已排队"
	case generation.StatusRunning:
		return "生成中"
	case generation.StatusSucceeded:
		return "已完成"
	case generation.StatusFailed:
		return "失败"
	case generation.StatusCancelled:
		return "已取消"
	default:
		return strings.TrimSpace(status)
	}
}

// nodeStatus 把后端任务状态映射成前端步骤条节点状态。
func nodeStatus(status string) string {
	switch strings.TrimSpace(status) {
	case generation.StatusSucceeded:
		return "succeeded"
	case generation.StatusFailed:
		return "failed"
	case generation.StatusRunning:
		return "running"
	case generation.StatusQueued:
		return "pending"
	default:
		return strings.TrimSpace(status)
	}
}
