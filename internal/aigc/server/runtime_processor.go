package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/eino/schema"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/a2ui"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/agentcontrol"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/approval"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/approvalruntime"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/capability"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/orchestration"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/session"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/sessionruntime"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/storyboard"
)

type approvalCheckpointStore interface {
	GetCheckpointMappingByApproval(context.Context, string) (session.CheckpointMapping, error)
	TransitionCheckpointMapping(context.Context, string, string, int64, string, int) (session.CheckpointMapping, error)
}

type DurableAgentProcessorConfig struct {
	HTTPConfig      Config
	ApprovalRuntime *approvalruntime.Service
	TurnOutputs     TurnOutputReceiptStore
}

type DurableAgentProcessor struct{ cfg DurableAgentProcessorConfig }

type resumeDeferredError struct {
	approvalID string
	retryAt    time.Time
}

func (e *resumeDeferredError) Error() string {
	return fmt.Sprintf("approval continuation %s is owned by another executor until %s", e.approvalID, e.retryAt.UTC().Format(time.RFC3339Nano))
}

type TurnOutputReceiptStore interface {
	SaveTurnOutput(context.Context, sessionruntime.Fence, string, json.RawMessage, string) (sessionruntime.SessionTurnRun, error)
}

func NewDurableAgentProcessor(config DurableAgentProcessorConfig) (*DurableAgentProcessor, error) {
	if config.HTTPConfig.Store == nil || config.HTTPConfig.Invoker == nil || config.HTTPConfig.Events == nil {
		return nil, fmt.Errorf("session store, Agent invoker and event publisher are required")
	}
	if config.HTTPConfig.Now == nil {
		config.HTTPConfig.Now = time.Now
	}
	if config.HTTPConfig.NewID == nil {
		config.HTTPConfig.NewID = randomID
	}
	if config.HTTPConfig.Approvals != nil && config.ApprovalRuntime == nil {
		return nil, fmt.Errorf("approval runtime is required when approval-backed resume is configured")
	}
	if config.TurnOutputs == nil && config.HTTPConfig.RuntimeStore != nil {
		config.TurnOutputs = config.HTTPConfig.RuntimeStore
	}
	return &DurableAgentProcessor{cfg: config}, nil
}

func (p *DurableAgentProcessor) Process(ctx context.Context, input sessionruntime.SessionInput, turn sessionruntime.SessionTurnRun, fence sessionruntime.Fence) (sessionruntime.TurnResult, error) {
	httpCfg := p.cfg.HTTPConfig
	record, err := httpCfg.Store.GetSession(ctx, fence.SessionID)
	if err != nil {
		return sessionruntime.TurnResult{}, err
	}
	ctx = capability.WithCommandContext(ctx, capability.CommandContext{
		UserID: record.UserID, SessionID: fence.SessionID, RunID: turn.RunnerRunID,
		RequestID: turn.TurnID, IdempotencyKey: input.InputIdentity().InputID,
		TraceID: turn.RunnerRunID,
	})
	httpCfg.NewID = deterministicTurnIDGenerator(turn.TurnID)
	resumeInput, isResume := input.(sessionruntime.ResumeRequested)
	runnerBacked := true
	if batch, ok := input.(sessionruntime.BatchContinuationResult); ok && !batch.NeedsAgentExplanation {
		runnerBacked = false
	}
	var (
		events       []AgentEvent
		interrupted  bool
		checkpointID = turn.RunnerRunID
		outputDigest string
	)
	if runnerBacked && len(turn.OutputPayload) > 0 {
		if isResume {
			if err := p.validateResumeReceipt(ctx, httpCfg, fence.SessionID, resumeInput); err != nil {
				return sessionruntime.TurnResult{}, err
			}
		}
		events, interrupted, checkpointID, err = decodeDurableAgentOutput(turn.OutputPayload)
		if err != nil {
			return sessionruntime.TurnResult{}, err
		}
		outputDigest = strings.TrimSpace(turn.OutputDigest)
		if outputDigest == "" {
			outputDigest = digestRawJSON(turn.OutputPayload)
		}
	} else {
		var stream <-chan AgentEvent
		switch value := input.(type) {
		case sessionruntime.UserMessage:
			throughSeq := durableMessageBoundary(turn, value.ContextMessageSeq)
			messages, loadErr := p.messages(ctx, fence.SessionID, throughSeq, value.MessageID)
			if loadErr != nil {
				return sessionruntime.TurnResult{}, loadErr
			}
			request := AgentInvokeRequest{Messages: messages, CheckpointID: turn.RunnerRunID}
			if httpCfg.SessionValues != nil {
				request.SessionValues = httpCfg.SessionValues(record)
			}
			stream, err = httpCfg.Invoker.Invoke(ctx, request)
		case sessionruntime.ResumeRequested:
			stream, err = p.resume(ctx, httpCfg, record, value, turn)
		case sessionruntime.ApprovalContinuationResult:
			if !turn.ContextSeqFrozen && httpCfg.RuntimeStore != nil {
				turn, err = httpCfg.RuntimeStore.FreezeTurnContextFromTerminalUserInputs(ctx, fence, turn.TurnID)
				if err != nil {
					return sessionruntime.TurnResult{}, err
				}
			}
			throughSeq := durableMessageBoundary(turn, value.ContextMessageSeq)
			messages, loadErr := p.messages(ctx, fence.SessionID, throughSeq, "")
			if loadErr != nil {
				return sessionruntime.TurnResult{}, loadErr
			}
			trustedEvent, encodeErr := json.Marshal(map[string]any{
				"approval_id": value.ApprovalID, "decision_version": value.DecisionVersion,
				"execution_epoch": value.ExecutionEpoch, "requested_decision": value.RequestedDecision,
				"effective_status": value.EffectiveStatus, "artifact_type": value.ArtifactType,
				"artifact_id": value.ArtifactID, "artifact_version": value.ArtifactVersion,
				"storyboard_id": value.StoryboardID, "storyboard_version": value.StoryboardVersion,
				"command_kind": value.CommandKind, "command_result": json.RawMessage(value.CommandResult),
			})
			if encodeErr != nil {
				return sessionruntime.TurnResult{}, encodeErr
			}
			nextDirective, directiveErr := approvalContinuationNextCapabilityDirective(value)
			if directiveErr != nil {
				return sessionruntime.TurnResult{}, directiveErr
			}
			trustedInstruction := fmt.Sprintf("这是系统内部的可信持久化审批续作事件，不是用户消息：%s。冻结的审批命令已经确定性执行，禁止重复执行该命令或重复创建同一审批。请重新读取当前会话中的最新 Spec、Storyboard、Artifact 与生成状态。%s不得声称用户发送了新的聊天消息。", trustedEvent, approvalContinuationNextStageInstruction(value))
			if nextDirective != "" {
				trustedInstruction += "下方机器指令只允许恰好一次 Tool 执行；历史中出现其稳定 ToolCall 和对应 Tool result 后即视为已完成，禁止再次调用同名 Capability。\n" + nextDirective
			}
			messages = append(messages, schema.SystemMessage(trustedInstruction))
			request := AgentInvokeRequest{Messages: messages, CheckpointID: turn.RunnerRunID}
			if httpCfg.SessionValues != nil {
				request.SessionValues = httpCfg.SessionValues(record)
			}
			stream, err = httpCfg.Invoker.Invoke(ctx, request)
		case sessionruntime.BatchContinuationResult:
			if !value.NeedsAgentExplanation {
				return sessionruntime.TurnResult{Outcome: sessionruntime.TurnOutcomeCommit, OutputDigest: digestJSON(value)}, nil
			}
			if !turn.ContextSeqFrozen && httpCfg.RuntimeStore != nil {
				turn, err = httpCfg.RuntimeStore.FreezeTurnContextFromTerminalUserInputs(ctx, fence, turn.TurnID)
				if err != nil {
					return sessionruntime.TurnResult{}, err
				}
			}
			throughSeq := durableMessageBoundary(turn, value.ContextMessageSeq)
			messages, loadErr := p.messages(ctx, fence.SessionID, throughSeq, "")
			if loadErr != nil {
				return sessionruntime.TurnResult{}, loadErr
			}
			trustedResult := strings.TrimSpace(string(value.Result))
			if trustedResult == "" {
				trustedResult = "{}"
			}
			messages = append(messages, schema.SystemMessage(fmt.Sprintf("这是系统内部的可信持久化 Batch 续作事件，不是用户消息：生成批次 %s（operation=%s，status=%s，approval=%s）已确定性完成。可信批次结果（包含每个任务的资产、状态与费用）=%s。请只解释这些已持久化结果和费用，不要重复创建审核或任务，也不得声称用户发送了新的聊天消息。", value.BatchID, value.OperationID, value.StageStatus, value.ApprovalID, trustedResult)))
			request := AgentInvokeRequest{Messages: messages, CheckpointID: turn.RunnerRunID}
			if httpCfg.SessionValues != nil {
				request.SessionValues = httpCfg.SessionValues(record)
			}
			stream, err = httpCfg.Invoker.Invoke(ctx, request)
		default:
			return sessionruntime.TurnResult{}, fmt.Errorf("unsupported durable input %T", input)
		}
		if err != nil {
			return sessionruntime.TurnResult{}, err
		}
		publishLiveProgress := true
		if resume, ok := input.(sessionruntime.ResumeRequested); ok && strings.TrimSpace(resume.ApprovalID) != "" {
			// Approval-bound Tool completion is not authoritative until the frozen
			// continuation commits. Buffer this resume's progress so a claimed or
			// failed continuation cannot expose a premature "completed" state.
			publishLiveProgress = false
		}
		events, interrupted, checkpointID, err = p.collectAgentEvents(ctx, httpCfg, fence.SessionID, turn, stream, publishLiveProgress)
		if err != nil {
			return sessionruntime.TurnResult{}, err
		}
		if runnerBacked && p.cfg.TurnOutputs != nil {
			payload, digest, encodeErr := encodeDurableAgentOutput(events, interrupted, checkpointID)
			if encodeErr != nil {
				return sessionruntime.TurnResult{}, encodeErr
			}
			saved, saveErr := p.cfg.TurnOutputs.SaveTurnOutput(ctx, fence, turn.TurnID, payload, digest)
			if saveErr != nil {
				return sessionruntime.TurnResult{}, saveErr
			}
			turn = saved
			outputDigest = strings.TrimSpace(saved.OutputDigest)
			if outputDigest == "" {
				outputDigest = digest
			}
		}
	}
	if outputDigest == "" {
		outputDigest = digestJSON(events)
	}
	var (
		resumeMapping session.CheckpointMapping
		hasResume     bool
	)
	if resume, ok := input.(sessionruntime.ResumeRequested); ok {
		hasResume = true
		resumeMapping, err = p.prepareResumeCompletion(ctx, httpCfg, fence.SessionID, resume)
		if err != nil {
			var deferred *resumeDeferredError
			if errors.As(err, &deferred) {
				return sessionruntime.TurnResult{
					Outcome:      sessionruntime.TurnOutcomeRetry,
					RetryAt:      deferred.retryAt,
					Failure:      sessionruntime.Failure{Code: "approval_continuation_claimed", Message: deferred.Error()},
					OutputDigest: outputDigest,
				}, nil
			}
			return sessionruntime.TurnResult{}, err
		}
	}
	buffer := make(chan AgentEvent, len(events))
	for _, event := range events {
		buffer <- event
	}
	close(buffer)
	if err := httpCfg.publishAgentEvents(ctx, fence.SessionID, turn.RunnerRunID, buffer); err != nil {
		return sessionruntime.TurnResult{}, err
	}
	if hasResume {
		if err := p.completeResume(ctx, httpCfg, resumeMapping); err != nil {
			return sessionruntime.TurnResult{}, err
		}
	}
	if interrupted {
		return sessionruntime.TurnResult{Outcome: sessionruntime.TurnOutcomeWaitingInterrupt, RunnerCheckpointID: checkpointID, OutputDigest: outputDigest}, nil
	}
	return sessionruntime.TurnResult{Outcome: sessionruntime.TurnOutcomeCommit, OutputDigest: outputDigest}, nil
}

// approvalContinuationNextStageInstruction maps an applied approval result to
// the only deterministic next capability. Non-approved and unknown artifacts
// deliberately remain non-prescriptive so a terminal receipt cannot force an
// invalid workflow transition.
func approvalContinuationNextStageInstruction(value sessionruntime.ApprovalContinuationResult) string {
	terminalNoop, terminalErr := approvalContinuationIsTerminalNoop(value)
	if !strings.EqualFold(strings.TrimSpace(value.EffectiveStatus), string(approval.StatusApproved)) || terminalNoop || terminalErr != nil {
		return "本次审批未形成可推进的已应用状态，不强制推进到下一个 Capability；准确解释结果并按当前状态决定停止、等待用户输入或重新规划。"
	}
	// 谓词错误在文字指示通道历来按 false 继续（机器指令通道才传播），
	// 保持原策略。谓词自身对不相关 artifact_type 走 fast-path 不解码。
	productionComplete, _ := approvalContinuationProductionComplete(value)
	return orchestration.DecideApprovalContinuation(orchestration.ApprovalContinuationInput{
		ArtifactType: strings.TrimSpace(value.ArtifactType),
		Guards: map[string]bool{
			orchestration.GuardArtifactVersionGt1: value.ArtifactVersion > 1,
			orchestration.GuardProductionComplete: productionComplete,
		},
	}).Instruction
}

func approvalContinuationNextCapabilityDirective(value sessionruntime.ApprovalContinuationResult) (string, error) {
	if !strings.EqualFold(strings.TrimSpace(value.EffectiveStatus), string(approval.StatusApproved)) {
		return "", nil
	}
	terminalNoop, err := approvalContinuationIsTerminalNoop(value)
	if err != nil {
		return "", err
	}
	if terminalNoop {
		return "", nil
	}

	productionComplete, completeErr := approvalContinuationProductionComplete(value)
	if completeErr != nil {
		return "", completeErr
	}
	decision := orchestration.DecideApprovalContinuation(orchestration.ApprovalContinuationInput{
		ArtifactType: strings.TrimSpace(value.ArtifactType),
		Guards: map[string]bool{
			orchestration.GuardArtifactVersionGt1: value.ArtifactVersion > 1,
			orchestration.GuardProductionComplete: productionComplete,
		},
	})
	if decision.Node == nil {
		return "", nil
	}
	return agentcontrol.EncodeNextCapabilityDirective(agentcontrol.NextCapabilityDirective{
		Version:  agentcontrol.NextCapabilityDirectiveVersion,
		SourceID: fmt.Sprintf("approval:%s:%d:%d", strings.TrimSpace(value.ApprovalID), value.DecisionVersion, value.ExecutionEpoch),
		Tool:     decision.Node.ToolKey, Arguments: decision.Node.Arguments,
	})
}

// approvalContinuationProductionComplete examines only the aggregate frozen
// in the applied approval command receipt. Missing legacy fields conservatively
// return false; malformed present aggregates fail closed. Candidate bindings,
// unresolved required inputs, or any provider-backed slot that is not backed
// by the matching active binding all prevent automatic assembly.
func approvalContinuationProductionComplete(value sessionruntime.ApprovalContinuationResult) (bool, error) {
	artifactType := strings.TrimSpace(value.ArtifactType)
	commandKind := strings.TrimSpace(value.CommandKind)
	if (artifactType == "storyboard_revision" && commandKind != "PromoteStoryboardRevision") ||
		(artifactType == "candidate_asset" && commandKind != "ActivateArtifactBinding") {
		return false, nil
	}
	raw := []byte(strings.TrimSpace(string(value.CommandResult)))
	if len(raw) == 0 {
		return false, nil
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return false, fmt.Errorf("decode approval continuation command result: %w", err)
	}
	aggregateRaw, exists := envelope["aggregate"]
	if !exists || len(aggregateRaw) == 0 || string(aggregateRaw) == "null" {
		return false, nil
	}
	var aggregate storyboard.StoryboardAggregate
	if err := json.Unmarshal(aggregateRaw, &aggregate); err != nil {
		return false, fmt.Errorf("decode approval continuation storyboard aggregate: %w", err)
	}
	if strings.TrimSpace(aggregate.ID) == "" || strings.TrimSpace(aggregate.ActiveRevisionID) == "" {
		return false, nil
	}
	if strings.TrimSpace(aggregate.PendingRevisionID) != "" {
		return false, nil
	}
	revision, err := aggregate.ActiveRevision()
	if err != nil {
		return false, fmt.Errorf("load frozen active storyboard revision: %w", err)
	}
	activeBindings := make(map[string]storyboard.ArtifactBinding)
	for _, binding := range aggregate.Bindings {
		if binding.State == storyboard.BindingStateCandidate {
			return false, nil
		}
		if binding.State == storyboard.BindingStateActive {
			activeBindings[binding.ID] = binding
		}
	}
	sawProviderSlot := false
	for _, module := range revision.Modules {
		for _, element := range module.Elements {
			for _, slot := range element.AssetSlots {
				if slot.Status == storyboard.AssetSlotStatusCandidate || len(slot.CandidateIDs) > 0 {
					return false, nil
				}
				providerBacked := approvalProviderBackedMediaKind(slot.MediaKind)
				if providerBacked {
					sawProviderSlot = true
				}
				if !providerBacked && !slot.Required {
					continue
				}
				if slot.Status != storyboard.AssetSlotStatusActive || strings.TrimSpace(slot.ActiveBindingID) == "" {
					return false, nil
				}
				binding, ok := activeBindings[slot.ActiveBindingID]
				if !ok || binding.TargetID != element.ID || binding.AssetSlot != slot.Key || strings.TrimSpace(binding.AssetID) == "" {
					return false, nil
				}
			}
		}
	}
	return sawProviderSlot, nil
}

func approvalProviderBackedMediaKind(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "image", "illustration", "keyframe", "video", "audio", "music", "voice":
		return true
	default:
		return false
	}
}

func approvalContinuationIsTerminalNoop(value sessionruntime.ApprovalContinuationResult) (bool, error) {
	if strings.EqualFold(strings.TrimSpace(value.CommandKind), "SupersededApprovalNoop") {
		return true, nil
	}
	raw := []byte(strings.TrimSpace(string(value.CommandResult)))
	if len(raw) == 0 {
		return false, nil
	}
	var result struct {
		Status     string `json:"status"`
		Superseded bool   `json:"superseded"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return false, fmt.Errorf("decode approval continuation command result: %w", err)
	}
	if result.Superseded {
		return true, nil
	}
	switch strings.ToLower(strings.TrimSpace(result.Status)) {
	case "stale", "superseded", "cancelled", "rejected":
		return true, nil
	default:
		return false, nil
	}
}

func durableMessageBoundary(turn sessionruntime.SessionTurnRun, fallback int64) *int64 {
	if turn.ContextSeqFrozen || turn.ContextMessageSeq > 0 {
		value := turn.ContextMessageSeq
		return &value
	}
	if fallback > 0 {
		value := fallback
		return &value
	}
	return nil
}

func (p *DurableAgentProcessor) messages(ctx context.Context, sessionID string, throughSeq *int64, currentMessageID string) ([]*schema.Message, error) {
	window := p.cfg.HTTPConfig.MessageWindow
	if throughSeq != nil {
		window.ThroughSeq = throughSeq
	}
	window.CurrentMessageID = strings.TrimSpace(currentMessageID)
	records, err := p.cfg.HTTPConfig.Store.ListMessages(ctx, sessionID, window)
	if err != nil {
		return nil, err
	}
	return recordsToSchemaMessages(records), nil
}

func (p *DurableAgentProcessor) resume(ctx context.Context, httpCfg Config, record session.SessionRecord, input sessionruntime.ResumeRequested, turn sessionruntime.SessionTurnRun) (<-chan AgentEvent, error) {
	if strings.TrimSpace(input.ApprovalID) == "" {
		return p.resumeInterrupt(ctx, httpCfg, record, input, turn)
	}
	return p.resumeApproval(ctx, httpCfg, record, input, turn)
}

func (p *DurableAgentProcessor) resumeApproval(ctx context.Context, httpCfg Config, record session.SessionRecord, input sessionruntime.ResumeRequested, turn sessionruntime.SessionTurnRun) (<-chan AgentEvent, error) {
	if httpCfg.Approvals == nil {
		return nil, fmt.Errorf("approval store is required for resume")
	}
	approvalRecord, err := httpCfg.Approvals.Get(ctx, input.ApprovalID)
	if err != nil {
		return nil, err
	}
	if approvalRecord.SessionID != record.ID || approvalRecord.DecisionVersion != input.DecisionVersion || approvalRecord.ExecutionMode != approval.ExecutionModeInterrupt {
		return nil, fmt.Errorf("approval resume fence rejected")
	}
	decision, err := httpCfg.Approvals.GetDecision(ctx, input.ApprovalID, input.DecisionVersion)
	if err != nil {
		return nil, err
	}
	checkpoints, ok := httpCfg.Checkpoints.(approvalCheckpointStore)
	if !ok {
		return nil, fmt.Errorf("approval-addressable checkpoint store is required")
	}
	mapping, err := checkpoints.GetCheckpointMappingByApproval(ctx, input.ApprovalID)
	if err != nil {
		return nil, err
	}
	if mapping.ID != input.MappingID || mapping.MappingEpoch != input.MappingEpoch || mapping.RunnerCheckpointID == "" {
		return nil, fmt.Errorf("checkpoint mapping fence rejected")
	}
	switch mapping.Status {
	case session.CheckpointStatusPending, session.CheckpointStatusResumeQueued:
		next, transitionErr := checkpoints.TransitionCheckpointMapping(ctx, mapping.ID, mapping.Status, mapping.MappingEpoch, session.CheckpointStatusResuming, input.DecisionVersion)
		if transitionErr != nil {
			return nil, transitionErr
		}
		mapping = next
	case session.CheckpointStatusResuming:
		// A recovered durable input may re-enter after the process stopped in
		// the external Runner call. The stable Turn/Input IDs make all Tool
		// commands idempotent, so this is a deliberate at-least-once retry.
	case session.CheckpointStatusResumeApplied, session.CheckpointStatusResumed:
		return closedAgentEventStream(), nil
	default:
		return nil, fmt.Errorf("checkpoint status %q cannot be resumed", mapping.Status)
	}
	target := map[string]any{"approval_id": input.ApprovalID, "decision_version": input.DecisionVersion, "decision": decision.RequestedDecision, "effective_status": decision.EffectiveStatus}
	request := AgentResumeRequest{CheckpointID: mapping.RunnerCheckpointID, Targets: map[string]any{mapping.InterruptID: target}}
	if httpCfg.SessionValues != nil {
		request.SessionValues = httpCfg.SessionValues(record)
	}
	_ = turn
	return httpCfg.Invoker.Resume(ctx, request)
}

func (p *DurableAgentProcessor) resumeInterrupt(ctx context.Context, httpCfg Config, record session.SessionRecord, input sessionruntime.ResumeRequested, turn sessionruntime.SessionTurnRun) (<-chan AgentEvent, error) {
	checkpoints, ok := httpCfg.Checkpoints.(CheckpointTransitionStore)
	if !ok || httpCfg.Checkpoints == nil {
		return nil, fmt.Errorf("checkpoint transition store is required for durable resume")
	}
	mapping, err := httpCfg.Checkpoints.GetCheckpointMapping(ctx, record.ID, input.InterruptID)
	if err != nil {
		return nil, err
	}
	if mapping.ID != input.MappingID || mapping.MappingEpoch != input.MappingEpoch || mapping.SessionID != record.ID ||
		mapping.Scope != session.CheckpointScopeRunner || mapping.InterruptID != input.InterruptID || mapping.RunnerCheckpointID != input.CheckpointID || strings.TrimSpace(mapping.ApprovalID) != "" {
		return nil, fmt.Errorf("checkpoint mapping fence rejected")
	}
	switch mapping.Status {
	case session.CheckpointStatusPending, session.CheckpointStatusResumeQueued:
		mapping, err = checkpoints.TransitionCheckpointMapping(ctx, mapping.ID, mapping.Status, mapping.MappingEpoch, session.CheckpointStatusResuming, mapping.DecisionVersion)
		if err != nil {
			return nil, err
		}
	case session.CheckpointStatusResuming:
		// See resumeApproval: an interrupted external call is retried with the
		// same durable turn identity, so downstream Tool commands replay.
	case session.CheckpointStatusResumeApplied, session.CheckpointStatusResumed:
		return closedAgentEventStream(), nil
	default:
		return nil, fmt.Errorf("checkpoint status %q cannot be resumed", mapping.Status)
	}
	var target any
	if err := json.Unmarshal(input.Data, &target); err != nil {
		return nil, fmt.Errorf("decode durable resume data: %w", err)
	}
	request := AgentResumeRequest{CheckpointID: mapping.RunnerCheckpointID, Targets: map[string]any{mapping.InterruptID: target}}
	if httpCfg.SessionValues != nil {
		request.SessionValues = httpCfg.SessionValues(record)
	}
	_ = turn
	return httpCfg.Invoker.Resume(ctx, request)
}

// prepareResumeCompletion validates that the external Runner is in-flight or
// already has a later receipt and, for approval-bound resumes, applies the
// frozen domain continuation.
// It intentionally runs before projecting the frozen Agent output so users
// cannot observe a success response before the authoritative business command
// has committed.
func (p *DurableAgentProcessor) prepareResumeCompletion(ctx context.Context, httpCfg Config, sessionID string, input sessionruntime.ResumeRequested) (session.CheckpointMapping, error) {
	var (
		mapping    session.CheckpointMapping
		transition CheckpointTransitionStore
		err        error
	)
	if strings.TrimSpace(input.ApprovalID) != "" {
		checkpoints, ok := httpCfg.Checkpoints.(approvalCheckpointStore)
		if !ok {
			return session.CheckpointMapping{}, fmt.Errorf("approval-addressable checkpoint store is required")
		}
		mapping, err = checkpoints.GetCheckpointMappingByApproval(ctx, input.ApprovalID)
		transition = checkpoints
	} else {
		transition, _ = httpCfg.Checkpoints.(CheckpointTransitionStore)
		if transition == nil || httpCfg.Checkpoints == nil {
			return session.CheckpointMapping{}, fmt.Errorf("checkpoint transition store is required for durable resume")
		}
		mapping, err = httpCfg.Checkpoints.GetCheckpointMapping(ctx, sessionID, input.InterruptID)
	}
	if err != nil {
		return session.CheckpointMapping{}, err
	}
	if mapping.ID != input.MappingID || mapping.MappingEpoch != input.MappingEpoch {
		return session.CheckpointMapping{}, fmt.Errorf("checkpoint mapping fence rejected")
	}
	if strings.TrimSpace(input.ApprovalID) == "" && (mapping.SessionID != sessionID || mapping.Scope != session.CheckpointScopeRunner || mapping.RunnerCheckpointID != input.CheckpointID || mapping.InterruptID != input.InterruptID || strings.TrimSpace(mapping.ApprovalID) != "") {
		return session.CheckpointMapping{}, fmt.Errorf("checkpoint mapping fence rejected")
	}
	if mapping.Status != session.CheckpointStatusResuming && mapping.Status != session.CheckpointStatusResumeApplied && mapping.Status != session.CheckpointStatusResumed {
		return session.CheckpointMapping{}, fmt.Errorf("checkpoint status %q has no applied resume receipt", mapping.Status)
	}
	if strings.TrimSpace(input.ApprovalID) != "" {
		if p.cfg.ApprovalRuntime == nil {
			return session.CheckpointMapping{}, fmt.Errorf("approval runtime is required to apply a resumed approval")
		}
		continuation, loadErr := httpCfg.Approvals.GetContinuation(ctx, input.ApprovalID, input.DecisionVersion)
		if loadErr != nil {
			return session.CheckpointMapping{}, loadErr
		}
		applied, applyErr := p.cfg.ApprovalRuntime.Apply(ctx, continuation)
		if applyErr != nil {
			return session.CheckpointMapping{}, applyErr
		}
		if !applied {
			latest, latestErr := p.cfg.ApprovalRuntime.GetContinuation(ctx, input.ApprovalID, input.DecisionVersion)
			if latestErr != nil {
				return session.CheckpointMapping{}, latestErr
			}
			now := httpCfg.Now().UTC()
			retryAt := now.Add(250 * time.Millisecond)
			if latest.LeaseUntil != nil && latest.LeaseUntil.After(now) {
				retryAt = latest.LeaseUntil.UTC().Add(100 * time.Millisecond)
			}
			return session.CheckpointMapping{}, &resumeDeferredError{approvalID: input.ApprovalID, retryAt: retryAt}
		}
	}
	return mapping, nil
}

// completeResume makes the user-visible completion terminal only after the
// frozen Agent events were projected successfully.
func (p *DurableAgentProcessor) completeResume(ctx context.Context, httpCfg Config, mapping session.CheckpointMapping) error {
	transition, ok := httpCfg.Checkpoints.(CheckpointTransitionStore)
	if !ok || transition == nil {
		return fmt.Errorf("checkpoint transition store is required for durable resume")
	}
	if mapping.Status == session.CheckpointStatusResuming {
		var err error
		mapping, err = transition.TransitionCheckpointMapping(ctx, mapping.ID, mapping.Status, mapping.MappingEpoch, session.CheckpointStatusResumeApplied, mapping.DecisionVersion)
		if err != nil {
			return err
		}
	}
	if mapping.Status == session.CheckpointStatusResumeApplied {
		var err error
		mapping, err = transition.TransitionCheckpointMapping(ctx, mapping.ID, mapping.Status, mapping.MappingEpoch, session.CheckpointStatusResumed, mapping.DecisionVersion)
		if err != nil {
			return err
		}
	}
	return httpCfg.publishInterruptResolved(ctx, mapping.SessionID, mapping.RunnerCheckpointID, mapping.InterruptID)
}

func (p *DurableAgentProcessor) validateResumeReceipt(ctx context.Context, httpCfg Config, sessionID string, input sessionruntime.ResumeRequested) error {
	var (
		mapping session.CheckpointMapping
		err     error
	)
	if strings.TrimSpace(input.ApprovalID) != "" {
		checkpoints, ok := httpCfg.Checkpoints.(approvalCheckpointStore)
		if !ok {
			return fmt.Errorf("approval-addressable checkpoint store is required")
		}
		mapping, err = checkpoints.GetCheckpointMappingByApproval(ctx, input.ApprovalID)
		if err == nil && (mapping.ApprovalID != input.ApprovalID || mapping.DecisionVersion != input.DecisionVersion) {
			return fmt.Errorf("checkpoint mapping fence rejected")
		}
	} else {
		if httpCfg.Checkpoints == nil {
			return fmt.Errorf("checkpoint store is required for durable resume")
		}
		mapping, err = httpCfg.Checkpoints.GetCheckpointMapping(ctx, sessionID, input.InterruptID)
		if err == nil && (mapping.SessionID != sessionID || mapping.Scope != session.CheckpointScopeRunner || mapping.RunnerCheckpointID != input.CheckpointID || mapping.InterruptID != input.InterruptID || strings.TrimSpace(mapping.ApprovalID) != "") {
			return fmt.Errorf("checkpoint mapping fence rejected")
		}
	}
	if err != nil {
		return err
	}
	if mapping.ID != input.MappingID || mapping.MappingEpoch != input.MappingEpoch {
		return fmt.Errorf("checkpoint mapping fence rejected")
	}
	if mapping.Status != session.CheckpointStatusResuming && mapping.Status != session.CheckpointStatusResumeApplied && mapping.Status != session.CheckpointStatusResumed {
		return fmt.Errorf("checkpoint status %q has no Runner output receipt", mapping.Status)
	}
	return nil
}

func closedAgentEventStream() <-chan AgentEvent {
	stream := make(chan AgentEvent)
	close(stream)
	return stream
}

func (p *DurableAgentProcessor) collectAgentEvents(ctx context.Context, httpCfg Config, sessionID string, turn sessionruntime.SessionTurnRun, stream <-chan AgentEvent, publishProgress bool) ([]AgentEvent, bool, string, error) {
	events := make([]AgentEvent, 0)
	interrupted, checkpointID := false, turn.RunnerRunID
	for event := range stream {
		if event.Event == a2ui.EventToolProgress && publishProgress {
			// Live progress is a latency hint backed by stable event IDs. A
			// transient projection failure must not abort an already-running Tool;
			// the event remains unmarked and is authoritatively replayed after the
			// complete Turn output receipt is frozen.
			if err := publishLiveToolProgress(ctx, httpCfg, sessionID, turn, event); err == nil {
				event.ProgressPublished = true
			}
		}
		events = append(events, event)
		if event.Err != nil {
			return events, interrupted, checkpointID, event.Err
		}
		if event.Event == a2ui.EventInterruptRequest {
			interrupted = true
			if values := payloadMap(event.Payload); payloadString(values, "checkpoint_id") != "" {
				checkpointID = payloadString(values, "checkpoint_id")
			}
		}
	}
	return events, interrupted, checkpointID, nil
}

func publishLiveToolProgress(ctx context.Context, httpCfg Config, sessionID string, turn sessionruntime.SessionTurnRun, event AgentEvent) error {
	if httpCfg.Events == nil {
		return nil
	}
	hints := newChatA2UISurface(sessionID).eventsFromAgentEvent(event)
	for index, hint := range hints {
		if hint.Event != a2ui.EventAction {
			continue
		}
		raw, _ := json.Marshal(hint.Payload)
		sum := sha256.Sum256(append([]byte(fmt.Sprintf("%s\x00%d\x00", turn.TurnID, index)), raw...))
		if err := httpCfg.Events.Publish(ctx, a2ui.SSEEvent{ID: "progress_" + hex.EncodeToString(sum[:12]), SessionID: sessionID, RunID: turn.RunnerRunID, Event: a2ui.EventAction, SurfaceID: hint.SurfaceID, DataModelKey: hint.DataModelKey, Payload: hint.Payload, CreatedAt: httpCfg.Now()}); err != nil {
			return err
		}
	}
	return nil
}

func deterministicTurnIDGenerator(turnID string) func() string {
	ordinal := 0
	return func() string {
		ordinal++
		sum := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%d", turnID, ordinal)))
		return "evt_" + hex.EncodeToString(sum[:12])
	}
}

type durableAgentOutputReceipt struct {
	Version      int                        `json:"version"`
	Interrupted  bool                       `json:"interrupted"`
	CheckpointID string                     `json:"checkpoint_id"`
	Events       []durableAgentEventReceipt `json:"events"`
}

type durableAgentEventReceipt struct {
	Event             string          `json:"event"`
	SurfaceID         string          `json:"surface_id,omitempty"`
	DataModelKey      string          `json:"data_model_key,omitempty"`
	Payload           json.RawMessage `json:"payload"`
	AssistantText     string          `json:"assistant_text,omitempty"`
	Message           *schema.Message `json:"message,omitempty"`
	ProgressPublished bool            `json:"progress_published,omitempty"`
}

func encodeDurableAgentOutput(events []AgentEvent, interrupted bool, checkpointID string) (json.RawMessage, string, error) {
	receipt := durableAgentOutputReceipt{Version: 1, Interrupted: interrupted, CheckpointID: strings.TrimSpace(checkpointID), Events: make([]durableAgentEventReceipt, 0, len(events))}
	for _, event := range events {
		if event.Err != nil {
			return nil, "", fmt.Errorf("cannot freeze failed Agent event: %w", event.Err)
		}
		content := strings.TrimSpace(event.AssistantText)
		if content == "" && event.Message != nil && event.Message.Role == schema.Assistant && len(event.Message.ToolCalls) == 0 {
			content = strings.TrimSpace(event.Message.Content)
		}
		if envelope, ok := a2ui.ParseActionEnvelopeContent(content); ok {
			if err := a2ui.ValidateModelAuthoredActionEnvelope(envelope); err != nil {
				return nil, "", fmt.Errorf("cannot freeze forbidden assistant A2UI: %w", err)
			}
		}
		payload, err := json.Marshal(event.Payload)
		if err != nil {
			return nil, "", fmt.Errorf("freeze Agent event payload: %w", err)
		}
		receipt.Events = append(receipt.Events, durableAgentEventReceipt{
			Event: event.Event, SurfaceID: event.SurfaceID, DataModelKey: event.DataModelKey,
			Payload: payload, AssistantText: event.AssistantText, Message: event.Message,
			ProgressPublished: event.ProgressPublished,
		})
	}
	raw, err := json.Marshal(receipt)
	if err != nil {
		return nil, "", fmt.Errorf("freeze Agent output: %w", err)
	}
	return raw, digestRawJSON(raw), nil
}

func decodeDurableAgentOutput(raw json.RawMessage) ([]AgentEvent, bool, string, error) {
	if !json.Valid(raw) {
		return nil, false, "", fmt.Errorf("durable Agent output receipt is invalid JSON")
	}
	var receipt durableAgentOutputReceipt
	if err := json.Unmarshal(raw, &receipt); err != nil {
		return nil, false, "", fmt.Errorf("decode durable Agent output receipt: %w", err)
	}
	if receipt.Version != 1 {
		return nil, false, "", fmt.Errorf("unsupported durable Agent output receipt version %d", receipt.Version)
	}
	events := make([]AgentEvent, 0, len(receipt.Events))
	for _, frozen := range receipt.Events {
		var payload any
		if len(frozen.Payload) > 0 {
			if err := json.Unmarshal(frozen.Payload, &payload); err != nil {
				return nil, false, "", fmt.Errorf("decode frozen Agent event payload: %w", err)
			}
		}
		events = append(events, AgentEvent{
			Event: frozen.Event, SurfaceID: frozen.SurfaceID, DataModelKey: frozen.DataModelKey,
			Payload: payload, AssistantText: frozen.AssistantText, Message: frozen.Message,
			ProgressPublished: frozen.ProgressPublished,
		})
	}
	return events, receipt.Interrupted, strings.TrimSpace(receipt.CheckpointID), nil
}

func digestRawJSON(raw json.RawMessage) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func digestJSON(value any) string {
	raw, _ := json.Marshal(value)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

var _ sessionruntime.Processor = (*DurableAgentProcessor)(nil)
