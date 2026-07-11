package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/capability"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/modelreceipt"
)

func TestModelReceiptMiddlewareReplaysFrozenToolCallAndOrdinals(t *testing.T) {
	store := modelreceipt.NewMemoryStore()
	var toolACalls, toolBCalls int
	toolA, err := utils.InferTool("tool_a", "tool A", func(context.Context, *modelReceiptToolArgs) (string, error) {
		toolACalls++
		return "A", nil
	})
	if err != nil {
		t.Fatalf("InferTool(A) error = %v", err)
	}
	toolB, err := utils.InferTool("tool_b", "tool B", func(context.Context, *modelReceiptToolArgs) (string, error) {
		toolBCalls++
		return "B", nil
	})
	if err != nil {
		t.Fatalf("InferTool(B) error = %v", err)
	}

	firstModel := &sequenceChatModel{outputs: []*schema.Message{
		toolCallMessage("call-a", "tool_a"),
		schema.AssistantMessage("finished-with-a", nil),
	}}
	firstRunner := newReceiptTestRunner(t, firstModel, store, []tool.BaseTool{toolA, toolB}, false)
	ctx := capability.WithCommandContext(context.Background(), capability.CommandContext{RequestID: "turn-freeze-tools"})
	first := runReceiptTestTurn(t, firstRunner, ctx)
	if first.Content != "finished-with-a" {
		t.Fatalf("first final content = %q", first.Content)
	}
	if firstModel.CallCount() != 2 {
		t.Fatalf("first underlying model calls = %d, want 2", firstModel.CallCount())
	}

	// A fresh underlying model would choose Tool B. Replaying the same durable
	// turn must return both authoritative slots (Tool A, then final response)
	// without invoking this model at all.
	secondModel := &sequenceChatModel{outputs: []*schema.Message{
		toolCallMessage("call-b", "tool_b"),
		schema.AssistantMessage("finished-with-b", nil),
	}}
	secondRunner := newReceiptTestRunner(t, secondModel, store, []tool.BaseTool{toolA, toolB}, false)
	replayed := runReceiptTestTurn(t, secondRunner, ctx)
	if replayed.Content != "finished-with-a" {
		t.Fatalf("replayed final content = %q, want frozen A result", replayed.Content)
	}
	if secondModel.CallCount() != 0 {
		t.Fatalf("replay underlying model calls = %d, want 0", secondModel.CallCount())
	}
	if toolACalls != 2 || toolBCalls != 0 {
		t.Fatalf("tool calls A=%d B=%d, want A=2 B=0", toolACalls, toolBCalls)
	}

	firstSlot, err := store.Get(context.Background(), "turn-freeze-tools", 1)
	if err != nil {
		t.Fatalf("Get(slot 1) error = %v", err)
	}
	secondSlot, err := store.Get(context.Background(), "turn-freeze-tools", 2)
	if err != nil {
		t.Fatalf("Get(slot 2) error = %v", err)
	}
	if firstSlot.OutputDigest == secondSlot.OutputDigest {
		t.Fatalf("different model ordinals froze the same output: %s", firstSlot.OutputDigest)
	}
	var frozenTool *schema.Message
	if err := jsonRoundTrip(firstSlot.OutputJSON, &frozenTool); err != nil {
		t.Fatalf("decode frozen tool output: %v", err)
	}
	if len(frozenTool.ToolCalls) != 1 || frozenTool.ToolCalls[0].Function.Name != "tool_a" {
		t.Fatalf("slot 1 tool call = %#v, want tool_a", frozenTool.ToolCalls)
	}
}

func TestModelReceiptMiddlewareFreezesAndReplaysRejectedA2UIRetry(t *testing.T) {
	store := modelreceipt.NewMemoryStore()
	invalid := `{"a2ui_version":"1.0","actions":[{"type":"append_card","card":{"root":"root","components":[{"id":"root","component":{"Card":{"children":[]}}}]}}}]}`
	valid := `{"a2ui_version":"1.0","actions":[{"type":"append_card","surface":"chat","card_id":"result","card":{"root":"root","components":[{"id":"root","component":{"Card":{"children":["message"]}}},{"id":"message","component":{"Text":{"value":"已完成"}}}]}}]}`
	firstModel := &sequenceChatModel{outputs: []*schema.Message{
		schema.AssistantMessage(invalid, nil),
		schema.AssistantMessage(valid, nil),
	}}
	firstRunner := newA2UIRetryReceiptTestRunner(t, firstModel, store)
	ctx := capability.WithCommandContext(context.Background(), capability.CommandContext{RequestID: "turn-a2ui-retry"})

	first := runReceiptTestTurn(t, firstRunner, ctx)
	if first.Content != valid || firstModel.CallCount() != 2 {
		t.Fatalf("first output=%q calls=%d", first.Content, firstModel.CallCount())
	}
	for ordinal, want := range []string{invalid, valid} {
		receipt, err := store.Get(context.Background(), "turn-a2ui-retry", ordinal+1)
		if err != nil {
			t.Fatalf("Get(slot %d) error = %v", ordinal+1, err)
		}
		var message *schema.Message
		if err := jsonRoundTrip(receipt.OutputJSON, &message); err != nil {
			t.Fatalf("decode slot %d: %v", ordinal+1, err)
		}
		if message.Content != want {
			t.Fatalf("slot %d content = %q, want %q", ordinal+1, message.Content, want)
		}
	}

	replayModel := &sequenceChatModel{outputs: []*schema.Message{schema.AssistantMessage("must-not-run", nil)}}
	replayed := runReceiptTestTurn(t, newA2UIRetryReceiptTestRunner(t, replayModel, store), ctx)
	if replayed.Content != valid || replayModel.CallCount() != 0 {
		t.Fatalf("replayed output=%q calls=%d", replayed.Content, replayModel.CallCount())
	}
}

func TestModelReceiptMiddlewareNoCommandContextPassesThrough(t *testing.T) {
	store := modelreceipt.NewMemoryStore()
	inner := &sequenceChatModel{outputs: []*schema.Message{
		schema.AssistantMessage("first", nil),
		schema.AssistantMessage("second", nil),
	}}
	runner := newReceiptTestRunner(t, inner, store, nil, false)
	first := runReceiptTestTurn(t, runner, context.Background())
	second := runReceiptTestTurn(t, runner, context.Background())
	if first.Content != "first" || second.Content != "second" {
		t.Fatalf("passthrough outputs = %q, %q", first.Content, second.Content)
	}
	if inner.CallCount() != 2 {
		t.Fatalf("passthrough model calls = %d, want 2", inner.CallCount())
	}
}

func TestModelReceiptMiddlewareFreezesCompleteStream(t *testing.T) {
	store := modelreceipt.NewMemoryStore()
	firstModel := &sequenceChatModel{streams: [][]*schema.Message{{
		{Role: schema.Assistant, Content: "stream-"},
		{Role: schema.Assistant, Content: "one"},
	}}}
	ctx := capability.WithCommandContext(context.Background(), capability.CommandContext{RequestID: "turn-stream"})
	first := runReceiptTestTurn(t, newReceiptTestRunner(t, firstModel, store, nil, true), ctx)
	if first.Content != "stream-one" {
		t.Fatalf("first stream content = %q", first.Content)
	}

	secondModel := &sequenceChatModel{streams: [][]*schema.Message{{
		{Role: schema.Assistant, Content: "stream-two"},
	}}}
	replayed := runReceiptTestTurn(t, newReceiptTestRunner(t, secondModel, store, nil, true), ctx)
	if replayed.Content != "stream-one" {
		t.Fatalf("replayed stream content = %q", replayed.Content)
	}
	if secondModel.CallCount() != 0 {
		t.Fatalf("replayed stream invoked underlying model %d times", secondModel.CallCount())
	}
}

func TestModelReceiptMiddlewareInterruptResumeRestoresOrdinalAndFreezesRetry(t *testing.T) {
	receipts := modelreceipt.NewMemoryStore()
	checkpoints := newReceiptCheckpointStore()
	captures := &toolCommandCaptures{}
	var effectMu sync.Mutex
	effectKeys := map[string]struct{}{}
	sideEffects := 0
	approvalTool, err := utils.InferTool("approval_tool", "interrupt for approval", func(ctx context.Context, _ *modelReceiptToolArgs) (string, error) {
		command, ok := capability.CommandContextFrom(ctx)
		captures.Add(toolCommandCapture{Command: command, HasCommand: ok})
		effectKey := command.IdempotencyKey + "\x00" + command.ToolCallID
		effectMu.Lock()
		if _, exists := effectKeys[effectKey]; !exists {
			effectKeys[effectKey] = struct{}{}
			sideEffects++
		}
		effectMu.Unlock()
		isResume, hasData, approved := compose.GetResumeContext[bool](ctx)
		if isResume && hasData {
			if approved {
				return "approved", nil
			}
			return "rejected", nil
		}
		return "", compose.Interrupt(ctx, "approve generation")
	})
	if err != nil {
		t.Fatalf("InferTool(approval) error = %v", err)
	}

	inner := &sequenceChatModel{outputs: []*schema.Message{
		toolCallMessage("stable-approval-call", "approval_tool"),
		schema.AssistantMessage("resume-frozen-a", nil),
		schema.AssistantMessage("resume-divergent-b", nil),
	}}
	runner := newReceiptTestRunnerWithCheckpoints(t, inner, receipts, []tool.BaseTool{approvalTool}, false, checkpoints)
	initialCtx := capability.WithCommandContext(context.Background(), capability.CommandContext{RequestID: "initial-durable-turn", IdempotencyKey: "message:initial"})
	const checkpointID = "model-receipt-interrupt-checkpoint"
	iter := runner.Query(initialCtx, "create", adk.WithCheckPointID(checkpointID))
	interruptID := consumeReceiptInterrupt(t, iter)
	if interruptID == "" {
		t.Fatalf("initial run produced no root tool interrupt ID")
	}
	if _, existed, err := checkpoints.Get(context.Background(), checkpointID); err != nil || !existed {
		t.Fatalf("checkpoint persisted = %v, error = %v", existed, err)
	}
	if _, err := receipts.Get(context.Background(), "initial-durable-turn", 1); err != nil {
		t.Fatalf("initial model slot 1 was not frozen: %v", err)
	}
	if inner.CallCount() != 1 {
		t.Fatalf("model calls before resume = %d, want 1", inner.CallCount())
	}

	// Resume is a new durable server turn. The checkpoint restores ordinal N=1,
	// so the first post-approval Agent model call must use the new TurnID at N+1.
	resumeCtx := capability.WithCommandContext(context.Background(), capability.CommandContext{RequestID: "resume-durable-turn", IdempotencyKey: "checkpoint:mapping:resume:1"})
	firstResume, err := runner.ResumeWithParams(resumeCtx, checkpointID, &adk.ResumeParams{
		Targets: map[string]any{interruptID: true},
	})
	if err != nil {
		t.Fatalf("ResumeWithParams(first) error = %v", err)
	}
	firstFinal := consumeReceiptFinal(t, firstResume)
	if firstFinal.Content != "resume-frozen-a" {
		t.Fatalf("first resume final content = %q", firstFinal.Content)
	}
	if _, err := receipts.Get(context.Background(), "resume-durable-turn", 1); !errors.Is(err, modelreceipt.ErrNotFound) {
		t.Fatalf("resume slot 1 error = %v, want ErrNotFound because checkpoint restored N=1", err)
	}
	resumeSlot, err := receipts.Get(context.Background(), "resume-durable-turn", 2)
	if err != nil {
		t.Fatalf("resume model slot 2 was not frozen: %v", err)
	}
	if inner.CallCount() != 2 {
		t.Fatalf("model calls after first resume = %d, want 2", inner.CallCount())
	}

	// Simulate a crash after the model receipt committed but before the server
	// recorded turn completion. The durable checkpoint still represents the
	// original interrupt. Reusing it with the same new TurnID must hit slot 2,
	// even though the next underlying model output would diverge.
	secondResume, err := runner.ResumeWithParams(resumeCtx, checkpointID, &adk.ResumeParams{
		Targets: map[string]any{interruptID: true},
	})
	if err != nil {
		t.Fatalf("ResumeWithParams(retry) error = %v", err)
	}
	retriedFinal := consumeReceiptFinal(t, secondResume)
	if retriedFinal.Content != "resume-frozen-a" {
		t.Fatalf("retried resume content = %q, want frozen output", retriedFinal.Content)
	}
	if inner.CallCount() != 2 {
		t.Fatalf("crash retry invoked underlying model: calls=%d", inner.CallCount())
	}
	replayedSlot, err := receipts.Get(context.Background(), "resume-durable-turn", 2)
	if err != nil || replayedSlot.OutputDigest != resumeSlot.OutputDigest {
		t.Fatalf("replayed slot = %#v, error = %v; want digest %s", replayedSlot, err, resumeSlot.OutputDigest)
	}

	toolCalls := captures.All()
	if len(toolCalls) != 3 {
		t.Fatalf("approval tool captures = %d, want initial + two resumes", len(toolCalls))
	}
	if !toolCalls[0].HasCommand || toolCalls[0].Command.RequestID != "initial-durable-turn" {
		t.Fatalf("initial tool command context = %#v", toolCalls[0])
	}
	for index, capture := range toolCalls[1:] {
		if !capture.HasCommand || capture.Command.RequestID != "resume-durable-turn" {
			t.Fatalf("resume tool command context %d = %#v", index+1, capture)
		}
	}
	for index, capture := range toolCalls {
		if capture.Command.ToolCallID != "stable-approval-call" {
			t.Fatalf("tool call ID %d = %q, want stable frozen ID", index, capture.Command.ToolCallID)
		}
		if capture.Command.IdempotencyKey != "message:initial" {
			t.Fatalf("logical Tool idempotency base %d = %q, want initial turn base", index, capture.Command.IdempotencyKey)
		}
	}
	effectMu.Lock()
	defer effectMu.Unlock()
	if sideEffects != 1 || len(effectKeys) != 1 {
		t.Fatalf("logical Tool side effects=%d keys=%v, want one across interrupt/resume retries", sideEffects, effectKeys)
	}
}

func TestModelReceiptMiddlewarePropagatesFrozenToolCallID(t *testing.T) {
	middleware, err := NewModelReceiptMiddleware(ModelReceiptMiddlewareConfig{Store: modelreceipt.NewMemoryStore()})
	if err != nil {
		t.Fatalf("NewModelReceiptMiddleware() error = %v", err)
	}
	var captured capability.CommandContext
	endpoint, err := middleware.WrapInvokableToolCall(context.Background(), func(ctx context.Context, _ string, _ ...tool.Option) (string, error) {
		captured, _ = capability.CommandContextFrom(ctx)
		return `{}`, nil
	}, &adk.ToolContext{Name: "tool_a", CallID: "frozen-call-a"})
	if err != nil {
		t.Fatalf("WrapInvokableToolCall() error = %v", err)
	}
	ctx := capability.WithCommandContext(context.Background(), capability.CommandContext{RequestID: "turn-tools"})
	if _, err := endpoint(ctx, `{}`); err != nil {
		t.Fatalf("wrapped endpoint error = %v", err)
	}
	if captured.RequestID != "turn-tools" || captured.ToolCallID != "frozen-call-a" {
		t.Fatalf("captured command context = %#v", captured)
	}
}

func TestModelReceiptMiddlewareStoreErrorFailsClosed(t *testing.T) {
	inner := &sequenceChatModel{outputs: []*schema.Message{schema.AssistantMessage("must-not-run", nil)}}
	runner := newReceiptTestRunner(t, inner, failingModelReceiptStore{}, nil, false)
	ctx := capability.WithCommandContext(context.Background(), capability.CommandContext{RequestID: "turn-store-error"})
	iter := runner.Query(ctx, "create")
	var gotErr error
	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			gotErr = event.Err
		}
	}
	if gotErr == nil || !errors.Is(gotErr, errReceiptStoreUnavailable) {
		t.Fatalf("runner error = %v, want store failure", gotErr)
	}
	if inner.CallCount() != 0 {
		t.Fatalf("model ran despite receipt read failure: %d", inner.CallCount())
	}
}

type modelReceiptToolArgs struct{}

func toolCallMessage(id, name string) *schema.Message {
	return schema.AssistantMessage("", []schema.ToolCall{{
		ID: id, Type: "function", Function: schema.FunctionCall{Name: name, Arguments: `{}`},
	}})
}

func newReceiptTestRunner(t *testing.T, chatModel model.BaseChatModel, store modelreceipt.Store, tools []tool.BaseTool, streaming bool) *adk.Runner {
	return newReceiptTestRunnerWithCheckpoints(t, chatModel, store, tools, streaming, nil)
}

func newA2UIRetryReceiptTestRunner(t *testing.T, chatModel model.BaseChatModel, store modelreceipt.Store) *adk.Runner {
	t.Helper()
	middleware, err := NewModelReceiptMiddleware(ModelReceiptMiddlewareConfig{Store: store})
	if err != nil {
		t.Fatalf("NewModelReceiptMiddleware() error = %v", err)
	}
	agent, err := adk.NewChatModelAgent(context.Background(), &adk.ChatModelAgentConfig{
		Name: "a2ui-retry-receipt-test", Description: "test", Model: chatModel,
		Handlers:         []adk.ChatModelAgentMiddleware{middleware},
		ModelRetryConfig: newA2UIModelRetryConfig(),
	})
	if err != nil {
		t.Fatalf("NewChatModelAgent() error = %v", err)
	}
	return adk.NewRunner(context.Background(), adk.RunnerConfig{Agent: agent})
}

func newReceiptTestRunnerWithCheckpoints(t *testing.T, chatModel model.BaseChatModel, store modelreceipt.Store, tools []tool.BaseTool, streaming bool, checkpoints adk.CheckPointStore) *adk.Runner {
	t.Helper()
	middleware, err := NewModelReceiptMiddleware(ModelReceiptMiddlewareConfig{Store: store})
	if err != nil {
		t.Fatalf("NewModelReceiptMiddleware() error = %v", err)
	}
	agent, err := adk.NewChatModelAgent(context.Background(), &adk.ChatModelAgentConfig{
		Name: "model-receipt-test", Description: "test", Model: chatModel,
		ToolsConfig: adk.ToolsConfig{ToolsNodeConfig: compose.ToolsNodeConfig{Tools: tools}},
		Handlers:    []adk.ChatModelAgentMiddleware{middleware},
	})
	if err != nil {
		t.Fatalf("NewChatModelAgent() error = %v", err)
	}
	return adk.NewRunner(context.Background(), adk.RunnerConfig{Agent: agent, EnableStreaming: streaming, CheckPointStore: checkpoints})
}

func runReceiptTestTurn(t *testing.T, runner *adk.Runner, ctx context.Context) *schema.Message {
	t.Helper()
	iter := runner.Query(ctx, "create")
	var final *schema.Message
	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			t.Fatalf("Agent event error = %v", event.Err)
		}
		if event.Output == nil || event.Output.MessageOutput == nil || event.Output.MessageOutput.Role != schema.Assistant {
			continue
		}
		message, err := event.Output.MessageOutput.GetMessage()
		if err != nil {
			t.Fatalf("GetMessage() error = %v", err)
		}
		if len(message.ToolCalls) == 0 {
			final = message
		}
	}
	if final == nil {
		t.Fatalf("Agent produced no final assistant message")
	}
	return final
}

func consumeReceiptInterrupt(t *testing.T, iter *adk.AsyncIterator[*adk.AgentEvent]) string {
	t.Helper()
	var interruptID string
	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			t.Fatalf("Agent event error before interrupt = %v", event.Err)
		}
		if event.Action == nil || event.Action.Interrupted == nil {
			continue
		}
		for _, interruptContext := range event.Action.Interrupted.InterruptContexts {
			if interruptContext != nil && interruptContext.IsRootCause {
				interruptID = interruptContext.ID
				break
			}
		}
	}
	return interruptID
}

func consumeReceiptFinal(t *testing.T, iter *adk.AsyncIterator[*adk.AgentEvent]) *schema.Message {
	t.Helper()
	var final *schema.Message
	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			t.Fatalf("Agent resume event error = %v", event.Err)
		}
		if event.Action != nil && event.Action.Interrupted != nil {
			t.Fatalf("Agent unexpectedly interrupted again: %#v", event.Action.Interrupted)
		}
		if event.Output == nil || event.Output.MessageOutput == nil || event.Output.MessageOutput.Role != schema.Assistant {
			continue
		}
		message, err := event.Output.MessageOutput.GetMessage()
		if err != nil {
			t.Fatalf("GetMessage() error = %v", err)
		}
		if len(message.ToolCalls) == 0 {
			final = message
		}
	}
	if final == nil {
		t.Fatalf("Agent resume produced no final assistant message")
	}
	return final
}

type sequenceChatModel struct {
	mu      sync.Mutex
	outputs []*schema.Message
	streams [][]*schema.Message
	calls   int
}

type toolCommandCapture struct {
	Command    capability.CommandContext
	HasCommand bool
}

type toolCommandCaptures struct {
	mu     sync.Mutex
	values []toolCommandCapture
}

func (c *toolCommandCaptures) Add(value toolCommandCapture) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.values = append(c.values, value)
}

func (c *toolCommandCaptures) All() []toolCommandCapture {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]toolCommandCapture(nil), c.values...)
}

type receiptCheckpointStore struct {
	mu     sync.Mutex
	values map[string][]byte
}

func newReceiptCheckpointStore() *receiptCheckpointStore {
	return &receiptCheckpointStore{values: make(map[string][]byte)}
}

func (s *receiptCheckpointStore) Get(_ context.Context, key string) ([]byte, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.values[key]
	return append([]byte(nil), value...), ok, nil
}

func (s *receiptCheckpointStore) Set(_ context.Context, key string, value []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values[key] = append([]byte(nil), value...)
	return nil
}

func (m *sequenceChatModel) Generate(_ context.Context, _ []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	index := m.calls
	m.calls++
	if index >= len(m.outputs) {
		return nil, fmt.Errorf("unexpected model Generate call %d", index+1)
	}
	return m.outputs[index], nil
}

func (m *sequenceChatModel) Stream(_ context.Context, _ []*schema.Message, _ ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	index := m.calls
	m.calls++
	if index >= len(m.streams) {
		return nil, fmt.Errorf("unexpected model Stream call %d", index+1)
	}
	return schema.StreamReaderFromArray(m.streams[index]), nil
}

func (m *sequenceChatModel) CallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

var errReceiptStoreUnavailable = errors.New("receipt store unavailable")

type failingModelReceiptStore struct{}

func (failingModelReceiptStore) Get(context.Context, string, int) (modelreceipt.Receipt, error) {
	return modelreceipt.Receipt{}, errReceiptStoreUnavailable
}

func (failingModelReceiptStore) PutOnce(context.Context, modelreceipt.Receipt) (modelreceipt.Receipt, error) {
	return modelreceipt.Receipt{}, errReceiptStoreUnavailable
}

// jsonRoundTrip keeps this test explicit about the schema.Message JSON shape
// used by production receipts.
func jsonRoundTrip(raw []byte, output any) error {
	return json.Unmarshal(raw, output)
}
