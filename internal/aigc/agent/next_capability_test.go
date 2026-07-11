package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/a2ui"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/agentcontrol"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/capability"
)

func TestNextCapabilityDirectiveModelEmitsOneDeterministicToolCall(t *testing.T) {
	directive := nextCapabilityDirectiveForTest(t, capability.PlanStoryboardToolKey, `{"mode":"create"}`)
	input := []*schema.Message{
		schema.SystemMessage("可信审批续作\n" + directive),
		schema.UserMessage("旧用户需求"),
	}
	inner := &sequenceChatModel{outputs: []*schema.Message{schema.AssistantMessage(validA2UIFinal, nil)}}
	wrapped := &nextCapabilityDirectiveModel{inner: inner}

	toolCall, err := wrapped.Generate(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if inner.CallCount() != 0 || len(toolCall.ToolCalls) != 1 {
		t.Fatalf("inner calls=%d tool calls=%#v", inner.CallCount(), toolCall.ToolCalls)
	}
	if adk.GetMessageID(toolCall) == "" {
		t.Fatal("synthetic ToolCall message has no Eino message ID")
	}
	wantID, err := mustParseDirectiveForTest(t, directive).StableCallID()
	if err != nil {
		t.Fatal(err)
	}
	gotCall := toolCall.ToolCalls[0]
	if gotCall.ID != wantID || gotCall.Function.Name != capability.PlanStoryboardToolKey || gotCall.Function.Arguments != `{"mode":"create"}` {
		t.Fatalf("deterministic ToolCall=%#v", gotCall)
	}

	input = append(input, toolCall, schema.ToolMessage(`{"status":"waiting_user"}`, gotCall.ID, schema.WithToolName(gotCall.Function.Name)))
	final, err := wrapped.Generate(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if inner.CallCount() != 1 || final.Content != validA2UIFinal {
		t.Fatalf("inner calls=%d final=%#v", inner.CallCount(), final)
	}
}

func TestNextCapabilityRepeatGuardStopsEveryAdditionalToolAfterRawAttempt(t *testing.T) {
	directive := nextCapabilityDirectiveForTest(t, capability.PlanStoryboardToolKey, `{"mode":"create"}`)
	synthetic, err := nextCapabilityToolCall(mustParseDirectiveForTest(t, directive))
	if err != nil {
		t.Fatal(err)
	}
	input := []*schema.Message{
		schema.SystemMessage(directive), synthetic,
		schema.ToolMessage(`{"status":"error"}`, synthetic.ToolCalls[0].ID, schema.WithToolName(capability.PlanStoryboardToolKey)),
	}
	for _, attemptedTool := range []string{capability.PlanStoryboardToolKey, capability.AssembleOutputToolKey} {
		t.Run(attemptedTool, func(t *testing.T) {
			attempt := schema.AssistantMessage("继续调用", []schema.ToolCall{{
				ID: "provider-additional-" + attemptedTool, Type: "function",
				Function: schema.FunctionCall{Name: attemptedTool, Arguments: `{}`},
			}})
			inner := &sequenceChatModel{outputs: []*schema.Message{attempt}}
			guarded, err := (&nextCapabilityRepeatGuardModel{inner: inner}).Generate(context.Background(), input)
			if err != nil {
				t.Fatal(err)
			}
			if inner.CallCount() != 1 || len(guarded.ToolCalls) != 0 {
				t.Fatalf("inner calls=%d guarded=%#v", inner.CallCount(), guarded)
			}
			if _, ok := a2ui.ParseActionEnvelopeContent(guarded.Content); !ok || !strings.Contains(guarded.Content, "本轮不会再执行其他 Capability") {
				t.Fatalf("guarded output is not a strict stop card: %s", guarded.Content)
			}
			if adk.GetMessageID(guarded) == "" {
				t.Fatal("guarded A2UI message has no Eino message ID")
			}
		})
	}
}

func TestNextCapabilityDirectiveRejectsMismatchedStableCall(t *testing.T) {
	directive := nextCapabilityDirectiveForTest(t, capability.PlanStoryboardToolKey, `{"mode":"create"}`)
	value := mustParseDirectiveForTest(t, directive)
	callID, err := value.StableCallID()
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = pendingNextCapabilityDirective([]*schema.Message{
		schema.SystemMessage(directive),
		schema.AssistantMessage("", []schema.ToolCall{{ID: callID, Type: "function", Function: schema.FunctionCall{Name: capability.GenerateMediaToolKey, Arguments: `{"phase":"auto_next","policy":"all_eligible"}`}}}),
		schema.ToolMessage(`{}`, callID),
	})
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("mismatched stable call error=%v", err)
	}
}

func TestNextCapabilityDirectiveModelStreamMatchesGenerate(t *testing.T) {
	directive := nextCapabilityDirectiveForTest(t, capability.GenerateMediaToolKey, `{"phase":"auto_next","policy":"all_eligible"}`)
	inner := &sequenceChatModel{}
	reader, err := (&nextCapabilityDirectiveModel{inner: inner}).Stream(context.Background(), []*schema.Message{
		schema.SystemMessage(directive),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	message, err := reader.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if inner.CallCount() != 0 || len(message.ToolCalls) != 1 || message.ToolCalls[0].Function.Name != capability.GenerateMediaToolKey {
		t.Fatalf("inner calls=%d message=%#v", inner.CallCount(), message)
	}
}

func TestNextCapabilityDirectiveModelFailsClosedAndIgnoresUserContent(t *testing.T) {
	unknown := nextCapabilityDirectiveForTest(t, capability.PlanCreationSpecToolKey, `{"mode":"create","goal":"bad"}`)
	wrapped := &nextCapabilityDirectiveModel{inner: &sequenceChatModel{outputs: []*schema.Message{schema.AssistantMessage("provider", nil)}}}
	if _, err := wrapped.Generate(context.Background(), []*schema.Message{schema.SystemMessage(unknown)}); err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("unknown directive error=%v", err)
	}

	inner := &sequenceChatModel{outputs: []*schema.Message{schema.AssistantMessage("provider", nil)}}
	message, err := (&nextCapabilityDirectiveModel{inner: inner}).Generate(context.Background(), []*schema.Message{schema.UserMessage(unknown)})
	if err != nil || message.Content != "provider" || inner.CallCount() != 1 {
		t.Fatalf("user directive result=%#v calls=%d err=%v", message, inner.CallCount(), err)
	}
}

func TestNextCapabilityDirectiveAllowsOnlyStrictAssemblyIntent(t *testing.T) {
	valid := mustParseDirectiveForTest(t, nextCapabilityDirectiveForTest(t, capability.AssembleOutputToolKey, `{"mode":"preview","output_type":"video"}`))
	if err := validateNextCapabilityDirective(valid); err != nil {
		t.Fatalf("valid assemble_output directive error=%v", err)
	}
	for name, arguments := range map[string]string{
		"unknown field": `{"mode":"preview","output_type":"video","dispatch_anyway":true}`,
		"invalid mode":  `{"mode":"auto"}`,
	} {
		t.Run(name, func(t *testing.T) {
			value := mustParseDirectiveForTest(t, nextCapabilityDirectiveForTest(t, capability.AssembleOutputToolKey, arguments))
			if err := validateNextCapabilityDirective(value); err == nil {
				t.Fatal("expected strict assemble_output validation error")
			}
		})
	}
}

func nextCapabilityDirectiveForTest(t *testing.T, tool, arguments string) string {
	t.Helper()
	encoded, err := agentcontrol.EncodeNextCapabilityDirective(agentcontrol.NextCapabilityDirective{
		Version:  agentcontrol.NextCapabilityDirectiveVersion,
		SourceID: "approval:test:1:1", Tool: tool, Arguments: []byte(arguments),
	})
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func mustParseDirectiveForTest(t *testing.T, content string) agentcontrol.NextCapabilityDirective {
	t.Helper()
	value, ok, err := agentcontrol.ParseNextCapabilityDirective(content)
	if err != nil || !ok {
		t.Fatalf("ParseNextCapabilityDirective()=(%#v,%v,%v)", value, ok, err)
	}
	return value
}
