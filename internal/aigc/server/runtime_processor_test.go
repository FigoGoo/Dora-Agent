package server

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/schema"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/a2ui"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/agentcontrol"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/approval"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/approvalruntime"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/capability"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/session"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/sessionruntime"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/storyboard"
)

func TestDurableAgentProcessorRequiresApprovalRuntimeWhenApprovalsConfigured(t *testing.T) {
	store := newFakeSessionStore()
	_, err := NewDurableAgentProcessor(DurableAgentProcessorConfig{HTTPConfig: Config{
		Store: store, Invoker: &fakeAgentInvoker{}, Events: &fakeEventSubscriber{}, Approvals: approval.NewMemoryStore(),
	}})
	if err == nil || !strings.Contains(err.Error(), "approval runtime is required") {
		t.Fatalf("error=%v", err)
	}
}

func TestDurableOutputReceiptRejectsModelAuthoredPseudoApproval(t *testing.T) {
	content := `{"a2ui_version":"1.0","actions":[{"type":"append_card","surface":"chat","card_id":"storyboard-details","card":{"root":"root","components":[{"id":"root","component":{"Card":{"children":["details"]}}},{"id":"details","component":{"Markdown":{"value":"故事板已生成，请回复「确认」开始生成素材。"}}}]}}]}`
	_, _, err := encodeDurableAgentOutput([]AgentEvent{{
		Event: a2ui.EventChatDelta, AssistantText: content, Message: schema.AssistantMessage(content, nil),
	}}, false, "")
	if !errors.Is(err, a2ui.ErrModelAuthoredApproval) {
		t.Fatalf("encodeDurableAgentOutput() error = %v", err)
	}
}

func TestDurableInterruptResumeRecordsReceiptAndResolves(t *testing.T) {
	store := newFakeSessionStore()
	store.sessions["s1"] = session.SessionRecord{ID: "s1", UserID: "u1", Status: "active"}
	checkpoints := &fakeTransitionCheckpointStore{fakeCheckpointStore: &fakeCheckpointStore{record: session.CheckpointMapping{
		ID: "mapping-1", SessionID: "s1", Scope: session.CheckpointScopeRunner,
		RunnerCheckpointID: "checkpoint-1", InterruptID: "interrupt-1",
		MappingEpoch: 1, Status: session.CheckpointStatusPending,
	}}}
	invoker := &fakeAgentInvoker{}
	broker := &fakeEventSubscriber{}
	processor, err := NewDurableAgentProcessor(DurableAgentProcessorConfig{HTTPConfig: Config{
		Store: store, Checkpoints: checkpoints, Invoker: invoker, Events: broker, Now: fixedNow,
	}})
	if err != nil {
		t.Fatal(err)
	}
	input := sessionruntime.NewInterruptResumeRequested("mapping-1", 1, "checkpoint-1", "interrupt-1", "确认", json.RawMessage(`{"approved":true}`), "")
	result, err := processor.Process(context.Background(), input,
		sessionruntime.SessionTurnRun{TurnID: "turn-1", RunnerRunID: "run-1"},
		sessionruntime.Fence{SessionID: "s1", OwnerID: "owner", FenceToken: 1})
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != sessionruntime.TurnOutcomeCommit || checkpoints.record.Status != session.CheckpointStatusResumed {
		t.Fatalf("result=%+v checkpoint=%+v", result, checkpoints.record)
	}
	if invoker.resumeCalls != 1 || invoker.seenResume.CheckpointID != "checkpoint-1" {
		t.Fatalf("resume calls=%d request=%+v", invoker.resumeCalls, invoker.seenResume)
	}
	target := invoker.seenResume.Targets["interrupt-1"].(map[string]any)
	if target["approved"] != true {
		t.Fatalf("resume target=%#v", target)
	}
	if eventOfType(broker.published, a2ui.EventInterruptResolved) == nil {
		t.Fatalf("resolved event missing: %#v", broker.published)
	}
}

func TestDurableInterruptResumeRecoversResumingCheckpointAfterPublishFailure(t *testing.T) {
	store := newFakeSessionStore()
	store.sessions["s1"] = session.SessionRecord{ID: "s1", UserID: "u1", Status: "active"}
	checkpoints := &fakeTransitionCheckpointStore{fakeCheckpointStore: &fakeCheckpointStore{record: session.CheckpointMapping{
		ID: "mapping-1", SessionID: "s1", Scope: session.CheckpointScopeRunner,
		RunnerCheckpointID: "checkpoint-1", InterruptID: "interrupt-1",
		MappingEpoch: 1, Status: session.CheckpointStatusPending,
	}}}
	invoker := &fakeAgentInvoker{events: []AgentEvent{{Event: a2ui.EventChatDelta, AssistantText: "继续", Payload: map[string]any{"text": "继续"}}}}
	broker := &failOnceEventBroker{fakeEventSubscriber: fakeEventSubscriber{}, err: errors.New("simulated publish failure")}
	receipts := &fakeTurnOutputReceiptStore{}
	processor, err := NewDurableAgentProcessor(DurableAgentProcessorConfig{HTTPConfig: Config{
		Store: store, Checkpoints: checkpoints, Invoker: invoker, Events: broker, Now: fixedNow,
	}, TurnOutputs: receipts})
	if err != nil {
		t.Fatal(err)
	}
	input := sessionruntime.NewInterruptResumeRequested("mapping-1", 1, "checkpoint-1", "interrupt-1", "确认", json.RawMessage(`{"approved":true}`), "")
	turn := sessionruntime.SessionTurnRun{TurnID: "turn-retry", RunnerRunID: "run-retry"}
	fence := sessionruntime.Fence{SessionID: "s1", OwnerID: "owner", FenceToken: 1}
	if _, err := processor.Process(context.Background(), input, turn, fence); err == nil {
		t.Fatal("first processing unexpectedly succeeded")
	}
	if checkpoints.record.Status != session.CheckpointStatusResuming || invoker.resumeCalls != 1 {
		t.Fatalf("checkpoint=%+v calls=%d", checkpoints.record, invoker.resumeCalls)
	}
	broker.err = nil
	result, err := processor.Process(context.Background(), input, receipts.saved, fence)
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != sessionruntime.TurnOutcomeCommit || checkpoints.record.Status != session.CheckpointStatusResumed || invoker.resumeCalls != 1 {
		t.Fatalf("result=%+v checkpoint=%+v calls=%d", result, checkpoints.record, invoker.resumeCalls)
	}
}

func TestDurableUserMessageReplaysFrozenOutputInsteadOfCallingModelAgain(t *testing.T) {
	store := newFakeSessionStore()
	store.sessions["s1"] = session.SessionRecord{ID: "s1", Status: "active"}
	firstAnswer := assistantCardEnvelope(t, "first-card", "first answer")
	differentAnswer := assistantCardEnvelope(t, "different-card", "different answer")
	invoker := &fakeAgentInvoker{events: []AgentEvent{{Event: a2ui.EventChatDelta, AssistantText: firstAnswer, Payload: map[string]any{"text": firstAnswer}}}}
	broker := &failOnceEventBroker{err: errors.New("simulated projection failure")}
	receipts := &fakeTurnOutputReceiptStore{}
	processor, err := NewDurableAgentProcessor(DurableAgentProcessorConfig{HTTPConfig: Config{Store: store, Invoker: invoker, Events: broker, Now: fixedNow}, TurnOutputs: receipts})
	if err != nil {
		t.Fatal(err)
	}
	input := sessionruntime.NewUserMessage("message-1", "event-1")
	turn := sessionruntime.SessionTurnRun{TurnID: "turn-message", RunnerRunID: "run-message"}
	fence := sessionruntime.Fence{SessionID: "s1", OwnerID: "owner", FenceToken: 1}
	if _, err := processor.Process(context.Background(), input, turn, fence); err == nil {
		t.Fatal("first processing unexpectedly succeeded")
	}
	if invoker.invokeCalls != 1 || len(receipts.saved.OutputPayload) == 0 {
		t.Fatalf("invoke_calls=%d receipt=%+v", invoker.invokeCalls, receipts.saved)
	}
	invoker.events = []AgentEvent{{Event: a2ui.EventChatDelta, AssistantText: differentAnswer, Payload: map[string]any{"text": differentAnswer}}}
	broker.err = nil
	if _, err := processor.Process(context.Background(), input, receipts.saved, fence); err != nil {
		t.Fatal(err)
	}
	if invoker.invokeCalls != 1 {
		t.Fatalf("model was called again: %d", invoker.invokeCalls)
	}
	messages := store.messages["s1"]
	if len(messages) == 0 || !strings.Contains(messages[len(messages)-1].Content, "first answer") || strings.Contains(messages[len(messages)-1].Content, "different answer") {
		t.Fatalf("messages=%+v receipt=%s events=%#v", messages, string(receipts.saved.OutputPayload), broker.published)
	}
}

func TestDurableUserMessageRetryKeepsOriginalHistoryBoundary(t *testing.T) {
	store := newFakeSessionStore()
	store.sessions["s1"] = session.SessionRecord{ID: "s1", Status: "active"}
	store.messages["s1"] = []session.MessageRecord{{
		ID: "message-a", SessionID: "s1", RunID: "run-a", Role: "user", Content: "A", Seq: 1,
	}}
	invoker := &fakeAgentInvoker{invokeErr: errors.New("model temporarily unavailable")}
	processor, err := NewDurableAgentProcessor(DurableAgentProcessorConfig{HTTPConfig: Config{
		Store: store, Invoker: invoker, Events: &fakeEventSubscriber{}, Now: fixedNow,
	}})
	if err != nil {
		t.Fatal(err)
	}
	input := sessionruntime.NewUserMessage("message-a", "event-a")
	bounded, err := sessionruntime.WithContextMessageSeq(input, 1)
	if err != nil {
		t.Fatal(err)
	}
	input = bounded.(sessionruntime.UserMessage)
	turn := sessionruntime.SessionTurnRun{
		TurnID: "turn-a", RunnerRunID: "run-a", ContextMessageSeq: 1,
	}
	fence := sessionruntime.Fence{SessionID: "s1", OwnerID: "owner", FenceToken: 1}
	if _, err := processor.Process(context.Background(), input, turn, fence); err == nil {
		t.Fatal("first model attempt unexpectedly succeeded")
	}

	// B is durably appended before A is retried. A's immutable turn boundary
	// must prevent the retry from reconstructing history through B.
	store.messages["s1"] = append(store.messages["s1"], session.MessageRecord{
		ID: "message-b", SessionID: "s1", RunID: "run-b", Role: "user", Content: "B", Seq: 2,
	})
	invoker.invokeErr = nil
	if _, err := processor.Process(context.Background(), input, turn, fence); err != nil {
		t.Fatal(err)
	}
	if invoker.invokeCalls != 2 || len(invoker.seenMessages) != 1 || invoker.seenMessages[0].Content != "A" {
		t.Fatalf("retry history crossed A boundary: calls=%d messages=%+v", invoker.invokeCalls, invoker.seenMessages)
	}
}

func TestQueuedUserTurnSeesPredecessorOutputsButNotLaterUser(t *testing.T) {
	store := newFakeSessionStore()
	store.sessions["s1"] = session.SessionRecord{ID: "s1", Status: "active"}
	store.messages["s1"] = []session.MessageRecord{
		{ID: "message-a", SessionID: "s1", RunID: "run-a", Role: "user", Content: "A", Seq: 1},
		// B was appended while A was still running.
		{ID: "message-b", SessionID: "s1", RunID: "run-b", Role: "user", Content: "B", Seq: 2},
		{ID: "answer-a", SessionID: "s1", RunID: "run-a", Role: "assistant", Content: "A answer", Seq: 3},
		{ID: "tool-a", SessionID: "s1", RunID: "run-a", Role: "tool", Content: "A tool result", ToolCallID: "call-a", ToolName: "tool-a", Seq: 4},
		// C arrived after B and must not leak into B's reconstruction.
		{ID: "message-c", SessionID: "s1", RunID: "run-c", Role: "user", Content: "C", Seq: 5},
	}
	invoker := &fakeAgentInvoker{}
	processor, err := NewDurableAgentProcessor(DurableAgentProcessorConfig{HTTPConfig: Config{
		Store: store, Invoker: invoker, Events: &fakeEventSubscriber{}, Now: fixedNow,
	}})
	if err != nil {
		t.Fatal(err)
	}
	input := sessionruntime.NewUserMessage("message-b", "event-b")
	bounded, err := sessionruntime.WithContextMessageSeq(input, 2)
	if err != nil {
		t.Fatal(err)
	}
	input = bounded.(sessionruntime.UserMessage)
	turn := sessionruntime.SessionTurnRun{
		TurnID: "turn-b", RunnerRunID: "run-b", ContextMessageSeq: 2, ContextSeqFrozen: true,
	}
	if _, err := processor.Process(context.Background(), input, turn, sessionruntime.Fence{SessionID: "s1"}); err != nil {
		t.Fatal(err)
	}
	got := invoker.seenMessages
	if len(got) != 4 || got[0].Content != "A" || got[1].Content != "A answer" || got[2].Content != "A tool result" || got[3].Content != "B" {
		t.Fatalf("logical transcript order/content = %+v", got)
	}
	for _, message := range got {
		if message.Content == "C" {
			t.Fatalf("later user leaked into B context: %+v", got)
		}
	}
}

func TestThirdQueuedUserTurnGetsMultiTurnCausalTranscript(t *testing.T) {
	store := newFakeSessionStore()
	store.sessions["s1"] = session.SessionRecord{ID: "s1", Status: "active"}
	store.messages["s1"] = []session.MessageRecord{
		{ID: "message-a", SessionID: "s1", RunID: "run-a", Role: "user", Content: "A", Seq: 1},
		{ID: "message-b", SessionID: "s1", RunID: "run-b", Role: "user", Content: "B", Seq: 2},
		{ID: "message-c", SessionID: "s1", RunID: "run-c", Role: "user", Content: "C", Seq: 3},
		{ID: "answer-a", SessionID: "s1", RunID: "run-a", Role: "assistant", Content: "A answer", Seq: 4},
		{ID: "answer-b", SessionID: "s1", RunID: "run-b", Role: "assistant", Content: "B answer", Seq: 5},
	}
	invoker := &fakeAgentInvoker{}
	processor, err := NewDurableAgentProcessor(DurableAgentProcessorConfig{HTTPConfig: Config{
		Store: store, Invoker: invoker, Events: &fakeEventSubscriber{}, Now: fixedNow,
	}})
	if err != nil {
		t.Fatal(err)
	}
	input := sessionruntime.NewUserMessage("message-c", "event-c")
	bounded, err := sessionruntime.WithContextMessageSeq(input, 3)
	if err != nil {
		t.Fatal(err)
	}
	input = bounded.(sessionruntime.UserMessage)
	turn := sessionruntime.SessionTurnRun{
		TurnID: "turn-c", RunnerRunID: "run-c", ContextMessageSeq: 3, ContextSeqFrozen: true,
	}
	if _, err := processor.Process(context.Background(), input, turn, sessionruntime.Fence{SessionID: "s1"}); err != nil {
		t.Fatal(err)
	}
	got := invoker.seenMessages
	want := []string{"A", "A answer", "B", "B answer", "C"}
	if len(got) != len(want) {
		t.Fatalf("causal transcript = %+v", got)
	}
	for index, content := range want {
		if got[index].Content != content {
			t.Fatalf("causal transcript[%d]=%q, want=%q; all=%+v", index, got[index].Content, content, got)
		}
	}
}

func TestDurableBatchExplanationUsesFrozenHistoryBoundary(t *testing.T) {
	store := newFakeSessionStore()
	store.sessions["s1"] = session.SessionRecord{ID: "s1", Status: "active"}
	store.messages["s1"] = []session.MessageRecord{
		{ID: "message-a", SessionID: "s1", RunID: "run-a", Role: "user", Content: "A", Seq: 1},
		{ID: "message-b", SessionID: "s1", RunID: "run-b", Role: "user", Content: "B", Seq: 2},
	}
	invoker := &fakeAgentInvoker{}
	processor, err := NewDurableAgentProcessor(DurableAgentProcessorConfig{HTTPConfig: Config{
		Store: store, Invoker: invoker, Events: &fakeEventSubscriber{}, Now: fixedNow,
	}})
	if err != nil {
		t.Fatal(err)
	}
	input := sessionruntime.NewBatchContinuationResult("batch-1", 1, "event-batch")
	input.OperationID, input.StageStatus, input.NeedsAgentExplanation = "operation-1", "completed", true
	input.Result = json.RawMessage(`{"status":"completed"}`)
	bounded, err := sessionruntime.WithContextMessageSeq(input, 1)
	if err != nil {
		t.Fatal(err)
	}
	input = bounded.(sessionruntime.BatchContinuationResult)
	turn := sessionruntime.SessionTurnRun{TurnID: "turn-batch", RunnerRunID: "run-batch", ContextMessageSeq: 1}
	if _, err := processor.Process(context.Background(), input, turn, sessionruntime.Fence{SessionID: "s1"}); err != nil {
		t.Fatal(err)
	}
	if len(invoker.seenMessages) != 2 || invoker.seenMessages[0].Content != "A" {
		t.Fatalf("batch explanation history crossed boundary: %+v", invoker.seenMessages)
	}
	if internal := invoker.seenMessages[1]; internal.Role != schema.System || !strings.Contains(internal.Content, "不是用户消息") {
		t.Fatalf("batch continuation must be a trusted system event: %+v", internal)
	}
}

func TestApprovalContinuationNextStageInstruction(t *testing.T) {
	const noForcedProgress = "本次审批未形成可推进的已应用状态，不强制推进到下一个 Capability；准确解释结果并按当前状态决定停止、等待用户输入或重新规划。"
	tests := []struct {
		name          string
		artifactType  string
		status        string
		commandKind   string
		commandResult json.RawMessage
		want          string
	}{
		{
			name:         "approved creation spec advances to storyboard planning",
			artifactType: "creation_spec_revision",
			status:       string(approval.StatusApproved),
			want:         "确定性下一阶段：必须调用 plan_storyboard；禁止再次调用 plan_creation_spec。",
		},
		{
			name:         "approved storyboard advances to all eligible media",
			artifactType: "storyboard_revision",
			status:       string(approval.StatusApproved),
			want:         "确定性下一阶段：禁止再次调用 plan_storyboard；必须调用 generate_media，参数固定为 {\"phase\":\"auto_next\",\"policy\":\"all_eligible\"}。",
		},
		{
			name:         "approved candidate continues media before assembly",
			artifactType: "candidate_asset",
			status:       string(approval.StatusApproved),
			want:         "确定性下一阶段：必须继续调用 generate_media，参数使用 {\"phase\":\"auto_next\",\"policy\":\"all_eligible\"}；仅当没有更多 eligible 媒体阶段且当前装配依赖已满足时，才调用 assemble_output。",
		},
		{name: "rejected does not force progress", artifactType: "creation_spec_revision", status: string(approval.StatusRejected), want: noForcedProgress},
		{name: "stale does not force progress", artifactType: "storyboard_revision", status: string(approval.StatusStale), want: noForcedProgress},
		{name: "cancelled does not force progress", artifactType: "candidate_asset", status: string(approval.StatusCancelled), want: noForcedProgress},
		{name: "superseded approved command does not progress", artifactType: "creation_spec_revision", status: string(approval.StatusApproved), commandKind: "SupersededApprovalNoop", commandResult: json.RawMessage(`{"status":"stale","superseded":true}`), want: noForcedProgress},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := approvalContinuationNextStageInstruction(sessionruntime.ApprovalContinuationResult{
				ArtifactType:    test.artifactType,
				EffectiveStatus: test.status,
				CommandKind:     test.commandKind, CommandResult: test.commandResult,
			})
			if got != test.want {
				t.Fatalf("instruction=%q, want %q", got, test.want)
			}
		})
	}
}

func TestApprovalContinuationNextCapabilityDirective(t *testing.T) {
	tests := []struct {
		name, artifactType, status, commandKind, commandResult, wantTool, wantArguments string
		artifactVersion                                                                 int
	}{
		{name: "spec v1 approval", artifactType: "creation_spec_revision", status: "approved", artifactVersion: 1, wantTool: capability.PlanStoryboardToolKey, wantArguments: `{"mode":"create"}`},
		{name: "spec revision approval", artifactType: "creation_spec_revision", status: "approved", artifactVersion: 2, wantTool: capability.PlanStoryboardToolKey, wantArguments: `{"mode":"replan","preserve_approved_assets":true}`},
		{name: "storyboard approval", artifactType: "storyboard_revision", status: "approved", wantTool: capability.GenerateMediaToolKey, wantArguments: `{"phase":"auto_next","policy":"all_eligible"}`},
		{name: "candidate approval", artifactType: "candidate_asset", status: "approved", wantTool: capability.GenerateMediaToolKey, wantArguments: `{"phase":"auto_next","policy":"all_eligible"}`},
		{name: "rejected", artifactType: "creation_spec_revision", status: "rejected"},
		{name: "unknown", artifactType: "unknown", status: "approved"},
		{name: "superseded noop", artifactType: "creation_spec_revision", status: "approved", commandKind: "SupersededApprovalNoop", commandResult: `{"status":"stale","superseded":true}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			encoded, err := approvalContinuationNextCapabilityDirective(sessionruntime.ApprovalContinuationResult{
				ApprovalID: "approval-1", DecisionVersion: 2, ExecutionEpoch: 3,
				ArtifactType: test.artifactType, ArtifactVersion: test.artifactVersion,
				EffectiveStatus: test.status, CommandKind: test.commandKind, CommandResult: json.RawMessage(test.commandResult),
			})
			if err != nil {
				t.Fatal(err)
			}
			if test.wantTool == "" {
				if encoded != "" {
					t.Fatalf("unexpected directive %q", encoded)
				}
				return
			}
			value, ok, err := agentcontrol.ParseNextCapabilityDirective(encoded)
			if err != nil || !ok {
				t.Fatalf("parsed=(%#v,%v,%v)", value, ok, err)
			}
			if value.Tool != test.wantTool || string(value.Arguments) != test.wantArguments || value.SourceID != "approval:approval-1:2:3" {
				t.Fatalf("directive=%#v", value)
			}
		})
	}
}

func TestFinalCandidateApprovalDeterministicallyStartsAssembly(t *testing.T) {
	complete := approvalAggregateForTest(false)
	value := sessionruntime.ApprovalContinuationResult{
		ApprovalID: "approval-final", DecisionVersion: 4, ExecutionEpoch: 2,
		ArtifactType: "candidate_asset", EffectiveStatus: string(approval.StatusApproved),
		CommandKind: "ActivateArtifactBinding", CommandResult: approvalAggregateResultForTest(t, complete),
	}
	first, err := approvalContinuationNextCapabilityDirective(value)
	if err != nil {
		t.Fatal(err)
	}
	second, err := approvalContinuationNextCapabilityDirective(value)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("directive replay changed: first=%q second=%q", first, second)
	}
	directive, ok, err := agentcontrol.ParseNextCapabilityDirective(first)
	if err != nil || !ok {
		t.Fatalf("directive=(%+v,%v,%v)", directive, ok, err)
	}
	if directive.Tool != capability.AssembleOutputToolKey || string(directive.Arguments) != `{"mode":"preview","output_type":"video"}` {
		t.Fatalf("final directive=%+v", directive)
	}
	if instruction := approvalContinuationNextStageInstruction(value); !strings.Contains(instruction, "全部生产槽位已激活") || !strings.Contains(instruction, "assemble_output") {
		t.Fatalf("final instruction=%q", instruction)
	}
}

func TestCandidateApprovalDoesNotAssembleBeforeEveryCandidateIsResolved(t *testing.T) {
	aggregate := approvalAggregateForTest(true)
	value := sessionruntime.ApprovalContinuationResult{
		ApprovalID: "approval-not-final", DecisionVersion: 1, ExecutionEpoch: 1,
		ArtifactType: "candidate_asset", EffectiveStatus: string(approval.StatusApproved),
		CommandKind: "ActivateArtifactBinding", CommandResult: approvalAggregateResultForTest(t, aggregate),
	}
	encoded, err := approvalContinuationNextCapabilityDirective(value)
	if err != nil {
		t.Fatal(err)
	}
	directive, ok, err := agentcontrol.ParseNextCapabilityDirective(encoded)
	if err != nil || !ok {
		t.Fatalf("directive=(%+v,%v,%v)", directive, ok, err)
	}
	if directive.Tool != capability.GenerateMediaToolKey || string(directive.Arguments) != `{"phase":"auto_next","policy":"all_eligible"}` {
		t.Fatalf("non-final directive=%+v", directive)
	}
}

func TestApprovalProductionCompletionFailsClosedOnMalformedAggregate(t *testing.T) {
	_, err := approvalContinuationNextCapabilityDirective(sessionruntime.ApprovalContinuationResult{
		ApprovalID: "approval-bad", DecisionVersion: 1, ExecutionEpoch: 1,
		ArtifactType: "candidate_asset", EffectiveStatus: string(approval.StatusApproved),
		CommandKind: "ActivateArtifactBinding", CommandResult: json.RawMessage(`{"aggregate":"corrupt"}`),
	})
	if err == nil || !strings.Contains(err.Error(), "storyboard aggregate") {
		t.Fatalf("malformed aggregate error=%v", err)
	}
}

func approvalAggregateForTest(withPendingCandidate bool) storyboard.StoryboardAggregate {
	bindings := []storyboard.ArtifactBinding{{
		ID: "binding-image", StoryboardID: "board", TargetID: "image", AssetSlot: "image", AssetID: "asset-image", State: storyboard.BindingStateActive,
	}}
	slots := []storyboard.AssetSlot{{Key: "image", MediaKind: "image", Required: true, Status: storyboard.AssetSlotStatusActive, ActiveBindingID: "binding-image"}}
	if withPendingCandidate {
		bindings = append(bindings, storyboard.ArtifactBinding{
			ID: "binding-video-candidate", StoryboardID: "board", TargetID: "video", AssetSlot: "video", AssetID: "asset-video", State: storyboard.BindingStateCandidate,
		})
		slots = append(slots, storyboard.AssetSlot{Key: "video", MediaKind: "video", Required: true, Status: storyboard.AssetSlotStatusCandidate, CandidateIDs: []string{"binding-video-candidate"}})
	} else {
		bindings = append(bindings, storyboard.ArtifactBinding{
			ID: "binding-video", StoryboardID: "board", TargetID: "video", AssetSlot: "video", AssetID: "asset-video", State: storyboard.BindingStateActive,
		})
		slots = append(slots, storyboard.AssetSlot{Key: "video", MediaKind: "video", Required: true, Status: storyboard.AssetSlotStatusActive, ActiveBindingID: "binding-video"})
	}
	elements := []storyboard.StoryboardElement{
		{ID: "image", AssetSlots: slots[:1]},
		{ID: "video", AssetSlots: slots[1:]},
	}
	return storyboard.StoryboardAggregate{
		ID: "board", SessionID: "session", Version: 7, ActiveRevisionID: "revision", Status: storyboard.AggregateStatusActive, Bindings: bindings,
		Revisions: []storyboard.StoryboardRevision{{ID: "revision", StoryboardID: "board", Status: storyboard.RevisionStatusActive, Modules: []storyboard.StoryboardModule{{ID: "module", Elements: elements}}}},
	}
}

func approvalAggregateResultForTest(t *testing.T, aggregate storyboard.StoryboardAggregate) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"aggregate": aggregate})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestDurableApprovalContinuationStartsTrustedInternalTurn(t *testing.T) {
	for _, test := range []struct {
		name            string
		requested       string
		status          string
		wantInstruction string
		wantDirective   bool
	}{
		{name: "approved continues deterministic capability", requested: "approve", status: "approved", wantInstruction: "必须调用 generate_media，参数固定为 {\"phase\":\"auto_next\",\"policy\":\"all_eligible\"}", wantDirective: true},
		{name: "rejected explains or replans", requested: "reject", status: "rejected", wantInstruction: "不强制推进到下一个 Capability"},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := newFakeSessionStore()
			store.sessions["s1"] = session.SessionRecord{ID: "s1", Status: "active"}
			store.messages["s1"] = []session.MessageRecord{{
				ID: "message-a", SessionID: "s1", RunID: "run-a", Role: "user", Content: "请制作短片", Seq: 1,
			}}
			invoker := &fakeAgentInvoker{}
			processor, err := NewDurableAgentProcessor(DurableAgentProcessorConfig{HTTPConfig: Config{
				Store: store, Invoker: invoker, Events: &fakeEventSubscriber{}, Now: fixedNow,
			}})
			if err != nil {
				t.Fatal(err)
			}
			input := sessionruntime.NewApprovalContinuationResult("approval-1", 1, 2, "")
			input.RequestedDecision = test.requested
			input.EffectiveStatus = test.status
			input.ArtifactType = "storyboard_revision"
			input.ArtifactID = "revision-1"
			input.StoryboardID = "board-1"
			input.CommandKind = "PromoteStoryboardRevision"
			input.CommandResult = json.RawMessage(`{"status":"active"}`)
			result, err := processor.Process(context.Background(), input,
				sessionruntime.SessionTurnRun{TurnID: "turn-approval", RunnerRunID: "run-approval"},
				sessionruntime.Fence{SessionID: "s1"})
			if err != nil || result.Outcome != sessionruntime.TurnOutcomeCommit {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			if invoker.invokeCalls != 1 || invoker.resumeCalls != 0 || len(invoker.seenMessages) != 2 {
				t.Fatalf("invoke=%d resume=%d messages=%+v", invoker.invokeCalls, invoker.resumeCalls, invoker.seenMessages)
			}
			if invoker.seenMessages[0].Role != schema.User || invoker.seenMessages[0].Content != "请制作短片" {
				t.Fatalf("historical user message changed: %+v", invoker.seenMessages)
			}
			internal := invoker.seenMessages[1]
			if internal.Role != schema.System || !strings.Contains(internal.Content, `"effective_status":"`+test.status+`"`) ||
				!strings.Contains(internal.Content, "不是用户消息") || !strings.Contains(internal.Content, test.wantInstruction) {
				t.Fatalf("trusted continuation message=%+v", internal)
			}
			_, hasDirective, directiveErr := agentcontrol.ParseNextCapabilityDirective(internal.Content)
			if directiveErr != nil || hasDirective != test.wantDirective {
				t.Fatalf("directive=(%v,%v), want=%v in %q", hasDirective, directiveErr, test.wantDirective, internal.Content)
			}
			for index, message := range invoker.seenMessages[1:] {
				if message.Role == schema.User {
					t.Fatalf("approval continuation forged user message at %d: %+v", index+1, message)
				}
			}
		})
	}
}

func TestDurableAgentProcessorPublishesToolProgressBeforeToolCompletes(t *testing.T) {
	store := newFakeSessionStore()
	store.sessions["s1"] = session.SessionRecord{ID: "s1", Status: "active"}
	stream := make(chan AgentEvent)
	invoker := &blockingAgentInvoker{stream: stream}
	broker := a2ui.NewMemoryBroker(16)
	live, unsubscribe := broker.Subscribe(context.Background(), "s1")
	defer unsubscribe()
	receipts := &fakeTurnOutputReceiptStore{}
	processor, err := NewDurableAgentProcessor(DurableAgentProcessorConfig{
		HTTPConfig: Config{Store: store, Invoker: invoker, Events: broker, Now: fixedNow}, TurnOutputs: receipts,
	})
	if err != nil {
		t.Fatal(err)
	}
	type processResult struct {
		result sessionruntime.TurnResult
		err    error
	}
	done := make(chan processResult, 1)
	go func() {
		result, processErr := processor.Process(context.Background(), sessionruntime.NewUserMessage("message-live", ""),
			sessionruntime.SessionTurnRun{TurnID: "turn-live", RunnerRunID: "run-live"}, sessionruntime.Fence{SessionID: "s1"})
		done <- processResult{result: result, err: processErr}
	}()
	stream <- messageToAgentEvent(schema.AssistantMessage("", []schema.ToolCall{{
		ID: "call-plan", Type: "function", Function: schema.FunctionCall{Name: "plan_storyboard", Arguments: `{}`},
	}}))
	select {
	case event := <-live:
		if event.Event != a2ui.EventAction {
			t.Fatalf("live event=%+v", event)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("tool progress was not visible while the Tool stream remained open")
	}
	select {
	case result := <-done:
		t.Fatalf("processor completed before Tool result: %+v", result)
	default:
	}
	stream <- messageToAgentEvent(&schema.Message{Role: schema.Tool, ToolCallID: "call-plan", ToolName: "plan_storyboard", Content: `{"status":"completed"}`})
	answer := assistantCardEnvelope(t, "live-answer", "故事板规划完成")
	stream <- AgentEvent{Event: a2ui.EventChatDelta, AssistantText: answer, Message: schema.AssistantMessage(answer, nil), Payload: map[string]any{"text": answer}}
	close(stream)
	processed := <-done
	if processed.err != nil || processed.result.Outcome != sessionruntime.TurnOutcomeCommit {
		t.Fatalf("result=%+v err=%v", processed.result, processed.err)
	}
	_, _, _, decodedErr := decodeDurableAgentOutput(receipts.saved.OutputPayload)
	if decodedErr != nil {
		t.Fatal(decodedErr)
	}
	var receipt durableAgentOutputReceipt
	if err := json.Unmarshal(receipts.saved.OutputPayload, &receipt); err != nil {
		t.Fatal(err)
	}
	if len(receipt.Events) < 1 || !receipt.Events[0].ProgressPublished {
		t.Fatalf("live progress receipt=%+v", receipt)
	}
}

func TestDurableAgentProcessorReplaysProgressAfterLivePublishFailure(t *testing.T) {
	store := newFakeSessionStore()
	store.sessions["s1"] = session.SessionRecord{ID: "s1", Status: "active"}
	toolCall := messageToAgentEvent(schema.AssistantMessage("", []schema.ToolCall{{
		ID: "call-analysis", Type: "function", Function: schema.FunctionCall{Name: "analyze_materials", Arguments: `{}`},
	}}))
	answer := assistantCardEnvelope(t, "analysis-answer", "素材分析完成")
	invoker := &fakeAgentInvoker{events: []AgentEvent{toolCall, {Event: a2ui.EventChatDelta, AssistantText: answer, Message: schema.AssistantMessage(answer, nil), Payload: map[string]any{"text": answer}}}}
	broker := &failFirstEventBroker{err: errors.New("transient live progress failure")}
	receipts := &fakeTurnOutputReceiptStore{}
	processor, err := NewDurableAgentProcessor(DurableAgentProcessorConfig{
		HTTPConfig: Config{Store: store, Invoker: invoker, Events: broker, Now: fixedNow}, TurnOutputs: receipts,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := processor.Process(context.Background(), sessionruntime.NewUserMessage("message-progress-retry", ""),
		sessionruntime.SessionTurnRun{TurnID: "turn-progress-retry", RunnerRunID: "run-progress-retry"}, sessionruntime.Fence{SessionID: "s1"})
	if err != nil || result.Outcome != sessionruntime.TurnOutcomeCommit {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if invoker.invokeCalls != 1 || broker.failures != 1 || eventOfType(broker.published, a2ui.EventAction) == nil {
		t.Fatalf("invoke_calls=%d failures=%d events=%#v", invoker.invokeCalls, broker.failures, broker.published)
	}
	var receipt durableAgentOutputReceipt
	if err := json.Unmarshal(receipts.saved.OutputPayload, &receipt); err != nil {
		t.Fatal(err)
	}
	if len(receipt.Events) < 1 || receipt.Events[0].ProgressPublished {
		t.Fatalf("failed live projection was incorrectly acknowledged: %+v", receipt)
	}
}

func TestDurableInterruptResumeAppliedReceiptSkipsRunner(t *testing.T) {
	store := newFakeSessionStore()
	store.sessions["s1"] = session.SessionRecord{ID: "s1", Status: "active"}
	checkpoints := &fakeTransitionCheckpointStore{fakeCheckpointStore: &fakeCheckpointStore{record: session.CheckpointMapping{
		ID: "mapping-1", SessionID: "s1", Scope: session.CheckpointScopeRunner,
		RunnerCheckpointID: "checkpoint-1", InterruptID: "interrupt-1",
		MappingEpoch: 1, Status: session.CheckpointStatusResumeApplied,
	}}}
	invoker := &fakeAgentInvoker{}
	broker := &fakeEventSubscriber{}
	processor, err := NewDurableAgentProcessor(DurableAgentProcessorConfig{HTTPConfig: Config{
		Store: store, Checkpoints: checkpoints, Invoker: invoker, Events: broker, Now: fixedNow,
	}})
	if err != nil {
		t.Fatal(err)
	}
	input := sessionruntime.NewInterruptResumeRequested("mapping-1", 1, "checkpoint-1", "interrupt-1", "确认", json.RawMessage(`true`), "")
	if _, err := processor.Process(context.Background(), input, sessionruntime.SessionTurnRun{TurnID: "turn-1", RunnerRunID: "run-1"}, sessionruntime.Fence{SessionID: "s1"}); err != nil {
		t.Fatal(err)
	}
	if invoker.resumeCalls != 0 || checkpoints.record.Status != session.CheckpointStatusResumed {
		t.Fatalf("calls=%d checkpoint=%+v", invoker.resumeCalls, checkpoints.record)
	}
}

func TestDurableApprovalResumeWaitsForActiveContinuationLease(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	approvals := approval.NewMemoryStoreWithClock(clock)
	created, err := approvals.Create(ctx, approval.Approval{
		ID: "approval-lease", IdempotencyKey: "create:approval-lease", SessionID: "s1",
		ArtifactType: "generic_review",
		Binding:      approval.VersionBinding{ArtifactID: "artifact-1", ArtifactVersion: 1},
		ReviewMode:   approval.ReviewModeInterrupt, ExecutionMode: approval.ExecutionModeInterrupt,
		ApproveCommand: approval.FrozenCommand{Kind: "approve_generic", IdempotencyKey: "approve:approval-lease", Payload: json.RawMessage(`{}`)},
		RejectCommand:  approval.FrozenCommand{Kind: "reject_generic", IdempotencyKey: "reject:approval-lease", Payload: json.RawMessage(`{}`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := approvals.BindInterruptMapping(ctx, approval.MappingCommand{
		ApprovalID: created.Approval.ID, ExpectedExecutionEpoch: 1,
		CheckpointMappingID: "mapping-lease", MappingEpoch: 1,
	}); err != nil {
		t.Fatal(err)
	}
	// A mismatched binding closes this synthetic approval as a deterministic
	// no-op, keeping the test focused on continuation lease recovery.
	decision, err := approvals.Decide(ctx, approval.DecideCommand{
		ApprovalID: created.Approval.ID, IdempotencyKey: "decision:approval-lease",
		Decision: approval.DecisionApprove, CurrentBinding: approval.VersionBinding{ArtifactID: "different", ArtifactVersion: 1}, Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	continuations := sessionruntime.NewMemoryStoreWithClock(clock)
	continuation, _, err := continuations.RequestContinuation(ctx, decision.Continuation)
	if err != nil {
		t.Fatal(err)
	}
	claim := sessionruntime.ContinuationClaim{
		ApprovalID: continuation.ApprovalID, DecisionVersion: continuation.DecisionVersion,
		Executor: continuation.Executor, ExecutionEpoch: continuation.ExecutionEpoch, LeaseOwner: "crashed-owner",
	}
	if _, err := continuations.ClaimContinuation(ctx, claim, 30*time.Second); err != nil {
		t.Fatal(err)
	}
	approvalRuntime, err := approvalruntime.New(approvalruntime.Config{
		Approvals: approvals, Continuations: continuations, OwnerID: "resume-worker", LeaseTTL: 30 * time.Second, Now: clock,
	})
	if err != nil {
		t.Fatal(err)
	}

	store := newFakeSessionStore()
	store.sessions["s1"] = session.SessionRecord{ID: "s1", UserID: "u1", Status: "active"}
	checkpoints := &fakeTransitionCheckpointStore{fakeCheckpointStore: &fakeCheckpointStore{record: session.CheckpointMapping{
		ID: "mapping-lease", SessionID: "s1", Scope: session.CheckpointScopeRunner,
		RunnerCheckpointID: "checkpoint-lease", InterruptID: "interrupt-lease", ApprovalID: created.Approval.ID,
		MappingEpoch: 1, DecisionVersion: 1, Status: session.CheckpointStatusResuming,
	}}}
	broker := &fakeEventSubscriber{}
	success := assistantCardEnvelope(t, "approval-success", "审批命令已执行")
	completed := messageToAgentEvent(&schema.Message{Role: schema.Tool, ToolCallID: "call-approval", ToolName: "plan_storyboard", Content: `{"status":"completed"}`})
	invoker := &fakeAgentInvoker{events: []AgentEvent{completed, {Event: a2ui.EventChatDelta, AssistantText: success, Payload: map[string]any{"text": success}}}}
	receipts := &fakeTurnOutputReceiptStore{}
	processor, err := NewDurableAgentProcessor(DurableAgentProcessorConfig{
		HTTPConfig:      Config{Store: store, Checkpoints: checkpoints, Invoker: invoker, Events: broker, Approvals: approvals, Now: clock},
		ApprovalRuntime: approvalRuntime,
		TurnOutputs:     receipts,
	})
	if err != nil {
		t.Fatal(err)
	}
	input := sessionruntime.NewResumeRequested(created.Approval.ID, 1, "")
	input.MappingID, input.MappingEpoch = "mapping-lease", 1
	turn := sessionruntime.SessionTurnRun{TurnID: "turn-lease", RunnerRunID: "run-lease"}
	fence := sessionruntime.Fence{SessionID: "s1"}

	result, err := processor.Process(ctx, input, turn, fence)
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != sessionruntime.TurnOutcomeRetry || !result.RetryAt.Equal(now.Add(30*time.Second+100*time.Millisecond)) {
		t.Fatalf("first result=%+v", result)
	}
	if checkpoints.record.Status != session.CheckpointStatusResuming || eventOfType(broker.published, a2ui.EventInterruptResolved) != nil {
		t.Fatalf("checkpoint=%+v events=%#v", checkpoints.record, broker.published)
	}
	if len(broker.published) != 0 || invoker.resumeCalls != 1 || len(receipts.saved.OutputPayload) == 0 {
		t.Fatalf("success projected before apply: events=%#v resume_calls=%d receipt=%+v", broker.published, invoker.resumeCalls, receipts.saved)
	}

	now = now.Add(31 * time.Second)
	result, err = processor.Process(ctx, input, receipts.saved, fence)
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != sessionruntime.TurnOutcomeCommit || checkpoints.record.Status != session.CheckpointStatusResumed {
		t.Fatalf("second result=%+v checkpoint=%+v", result, checkpoints.record)
	}
	if invoker.resumeCalls != 1 {
		t.Fatalf("Runner resumed again despite frozen output: %d", invoker.resumeCalls)
	}
	applied, err := continuations.GetContinuation(ctx, created.Approval.ID, 1)
	if err != nil || applied.Status != sessionruntime.ContinuationStatusApplied {
		t.Fatalf("continuation=%+v err=%v", applied, err)
	}
	if eventOfType(broker.published, a2ui.EventInterruptResolved) == nil {
		t.Fatalf("resolved event missing: %#v", broker.published)
	}
}

type failOnceEventBroker struct {
	fakeEventSubscriber
	err error
}

type blockingAgentInvoker struct {
	stream      <-chan AgentEvent
	invokeCalls int
}

func (i *blockingAgentInvoker) Invoke(context.Context, AgentInvokeRequest) (<-chan AgentEvent, error) {
	i.invokeCalls++
	return i.stream, nil
}

func (i *blockingAgentInvoker) Resume(context.Context, AgentResumeRequest) (<-chan AgentEvent, error) {
	return i.stream, nil
}

type failFirstEventBroker struct {
	fakeEventSubscriber
	err      error
	failures int
}

func (b *failFirstEventBroker) Publish(ctx context.Context, event a2ui.SSEEvent) error {
	if b.failures == 0 {
		b.failures++
		return b.err
	}
	return b.fakeEventSubscriber.Publish(ctx, event)
}

type fakeTurnOutputReceiptStore struct {
	saved sessionruntime.SessionTurnRun
}

func (s *fakeTurnOutputReceiptStore) SaveTurnOutput(_ context.Context, _ sessionruntime.Fence, turnID string, payload json.RawMessage, digest string) (sessionruntime.SessionTurnRun, error) {
	if len(s.saved.OutputPayload) > 0 {
		if string(s.saved.OutputPayload) != string(payload) || s.saved.OutputDigest != digest {
			return sessionruntime.SessionTurnRun{}, sessionruntime.ErrIdempotencyConflict
		}
		return s.saved, nil
	}
	s.saved.TurnID = turnID
	s.saved.RunnerRunID = "run-retry"
	s.saved.OutputPayload = append(json.RawMessage(nil), payload...)
	s.saved.OutputDigest = digest
	return s.saved, nil
}

func (b *failOnceEventBroker) Publish(ctx context.Context, event a2ui.SSEEvent) error {
	if b.err != nil {
		return b.err
	}
	return b.fakeEventSubscriber.Publish(ctx, event)
}
