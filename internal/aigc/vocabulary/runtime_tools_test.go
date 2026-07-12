package vocabulary

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

type fakePromptWriter struct {
	prompt  string
	err     error
	inputs  map[string]any
	invoked int
}

func (f *fakePromptWriter) WritePrompt(_ context.Context, inputs map[string]any) (string, error) {
	f.invoked++
	f.inputs = inputs
	return f.prompt, f.err
}

type fakeGenerationDispatcher struct {
	result   GenerationDispatchResult
	err      error
	requests []GenerationDispatchRequest
	seen     map[string]GenerationDispatchResult
	effects  int
	mutate   bool
}

type scriptedGenerationDispatcher struct {
	results []GenerationDispatchResult
	calls   int
}

func (f *scriptedGenerationDispatcher) Dispatch(context.Context, GenerationDispatchRequest) (GenerationDispatchResult, error) {
	result := f.results[f.calls]
	f.calls++
	return result, nil
}

func (f *fakeGenerationDispatcher) Dispatch(_ context.Context, request GenerationDispatchRequest) (GenerationDispatchResult, error) {
	clonedInputs, cloneErr := cloneJSONMap(request.Inputs)
	if cloneErr != nil {
		return GenerationDispatchResult{}, cloneErr
	}
	recorded := request
	recorded.Inputs = clonedInputs
	f.requests = append(f.requests, recorded)
	if f.err != nil {
		return GenerationDispatchResult{}, f.err
	}
	if f.seen == nil {
		f.seen = make(map[string]GenerationDispatchResult)
	}
	if prior, ok := f.seen[request.IdempotencyKey]; ok {
		return prior, nil
	}
	f.effects++
	f.seen[request.IdempotencyKey] = f.result
	if f.mutate {
		request.Inputs["targets"].([]any)[0].(map[string]any)["prompt"] = "dispatcher mutation"
	}
	return f.result, nil
}

func TestRuntimeToolsExposeStableContracts(t *testing.T) {
	prompt := NewWriteMediaPromptTool(&fakePromptWriter{prompt: "cinematic rain"})
	confirm := NewRequestConfirmationTool()
	dispatch := NewDispatchGenerationTool(&fakeGenerationDispatcher{result: GenerationDispatchResult{
		OperationID: "operation-1", BatchID: "batch-1", JobIDs: []string{"job-1"},
	}})

	tools := []struct {
		tool     Tool
		key      string
		category string
		inputs   []string
		outputs  []string
	}{
		{prompt, "write_media_prompt", "cognition", []string{"target_desc"}, []string{"prompt"}},
		{confirm, "request_confirmation", "interaction", []string{"options", "question"}, nil},
		{dispatch, "dispatch_generation", "data", []string{"targets"}, []string{"batch_id", "job_ids", "operation_id"}},
	}
	registry := NewRegistry()
	for _, want := range tools {
		descriptor := want.tool.Descriptor()
		if descriptor.Key != want.key || descriptor.Category != want.category {
			t.Fatalf("descriptor=%+v, want key=%q category=%q", descriptor, want.key, want.category)
		}
		if strings.TrimSpace(descriptor.Name) == "" || strings.TrimSpace(descriptor.Description) == "" {
			t.Fatalf("descriptor three-elements must be non-empty: %+v", descriptor)
		}
		for _, name := range want.inputs {
			if _, ok := descriptor.Inputs[name]; !ok {
				t.Fatalf("%s missing input %q", want.key, name)
			}
		}
		for _, name := range want.outputs {
			if _, ok := descriptor.Outputs[name]; !ok {
				t.Fatalf("%s missing output %q", want.key, name)
			}
		}
		if err := registry.Register(want.tool); err != nil {
			t.Fatalf("register %s: %v", want.key, err)
		}
	}
	catalog := registry.CatalogText()
	for _, token := range []string{
		"write_media_prompt", "入参:target_desc*", "出参:prompt",
		"request_confirmation", "options", "question*",
		"dispatch_generation", "targets*", "batch_id", "job_ids", "operation_id",
	} {
		if !strings.Contains(catalog, token) {
			t.Fatalf("catalog missing %q: %s", token, catalog)
		}
	}

	confirmation, err := confirm.Run(context.Background(), Call{Inputs: map[string]any{"question": "continue?"}})
	if err != nil || confirmation.Suspension == nil || confirmation.Suspension.Reason != "waiting_user" {
		t.Fatalf("confirmation=%+v err=%v", confirmation, err)
	}
	dispatched, err := dispatch.Run(context.Background(), validDispatchCall())
	if err != nil || dispatched.Suspension == nil || dispatched.Suspension.Reason != "waiting_jobs" || dispatched.Outputs["batch_id"] != "batch-1" {
		t.Fatalf("dispatch=%+v err=%v", dispatched, err)
	}
}

func validDispatchCall() Call {
	return Call{
		SessionID: "session-1", UserID: "user-1", PlanRunID: "run-1", NodeID: "node-1",
		Attempt: 1, IdempotencyKey: "run-1:node-1:1",
		Inputs: map[string]any{"targets": []any{map[string]any{"prompt": "cinematic rain"}}},
	}
}

func TestRuntimeToolsWriteMediaPromptBoundaries(t *testing.T) {
	t.Run("valid input is cloned and preserves large numbers", func(t *testing.T) {
		writer := &fakePromptWriter{prompt: "cinematic rain"}
		tool := NewWriteMediaPromptTool(writer)
		callerNested := map[string]any{"seed": int64(9007199254740993)}
		call := Call{Inputs: map[string]any{
			"target_desc": "rainy city", "unchanged": callerNested,
		}}
		result, err := tool.Run(context.Background(), call)
		if err != nil || result.Outputs["prompt"] != "cinematic rain" {
			t.Fatalf("result=%+v err=%v", result, err)
		}
		gotSeed := writer.inputs["unchanged"].(map[string]any)["seed"]
		if gotSeed != json.Number("9007199254740993") {
			t.Fatalf("large number=%T(%v)", gotSeed, gotSeed)
		}
		writer.inputs["unchanged"].(map[string]any)["seed"] = "mutated"
		if callerNested["seed"] != int64(9007199254740993) {
			t.Fatal("writer input aliases caller input")
		}
	})

	for name, inputs := range map[string]map[string]any{
		"missing":    nil,
		"wrong type": {"target_desc": 7},
		"blank":      {"target_desc": " \t"},
	} {
		t.Run("invalid target_desc "+name, func(t *testing.T) {
			writer := &fakePromptWriter{prompt: "unused"}
			result, err := NewWriteMediaPromptTool(writer).Run(context.Background(), Call{Inputs: inputs})
			if err != nil || result.Fail == nil || result.Fail.Code != "invalid_request" || writer.invoked != 0 {
				t.Fatalf("result=%+v err=%v invoked=%d", result, err, writer.invoked)
			}
		})
	}

	t.Run("empty prompt is retryable business failure", func(t *testing.T) {
		result, err := NewWriteMediaPromptTool(&fakePromptWriter{prompt: " \n"}).Run(
			context.Background(), Call{Inputs: map[string]any{"target_desc": "rain"}})
		if err != nil || result.Fail == nil || result.Fail.Code != "empty_prompt" || !result.Fail.Retryable {
			t.Fatalf("result=%+v err=%v", result, err)
		}
	})

	t.Run("writer error remains a Go error", func(t *testing.T) {
		cause := errors.New("model unavailable")
		result, err := NewWriteMediaPromptTool(&fakePromptWriter{err: cause}).Run(
			context.Background(), Call{Inputs: map[string]any{"target_desc": "rain"}})
		if !errors.Is(err, cause) || !reflect.DeepEqual(result, Result{}) {
			t.Fatalf("result=%+v err=%v", result, err)
		}
	})

	t.Run("nil dependency returns an error", func(t *testing.T) {
		var typedNil *fakePromptWriter
		for _, tool := range []Tool{NewWriteMediaPromptTool(nil), NewWriteMediaPromptTool(typedNil)} {
			if _, err := tool.Run(context.Background(), Call{Inputs: map[string]any{"target_desc": "rain"}}); err == nil {
				t.Fatal("nil prompt writer must return an error")
			}
		}
	})

	t.Run("unserializable input returns an error", func(t *testing.T) {
		result, err := NewWriteMediaPromptTool(&fakePromptWriter{prompt: "unused"}).Run(context.Background(), Call{
			Inputs: map[string]any{"target_desc": "rain", "bad": make(chan int)},
		})
		if err == nil || !reflect.DeepEqual(result, Result{}) {
			t.Fatalf("result=%+v err=%v", result, err)
		}
	})

	t.Run("cancelled context is not invoked", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		writer := &fakePromptWriter{prompt: "unused"}
		_, err := NewWriteMediaPromptTool(writer).Run(ctx, Call{Inputs: map[string]any{"target_desc": "rain"}})
		if !errors.Is(err, context.Canceled) || writer.invoked != 0 {
			t.Fatalf("err=%v invoked=%d", err, writer.invoked)
		}
	})
}

func TestRuntimeToolsRequestConfirmationBoundaries(t *testing.T) {
	t.Run("valid payload is isolated and JSON-compatible", func(t *testing.T) {
		callerOption := map[string]any{"label": "yes", "value": int64(9007199254740993)}
		call := Call{Inputs: map[string]any{
			"question": "continue?", "options": []any{callerOption}, "ignored": "not payload",
		}}
		result, err := NewRequestConfirmationTool().Run(context.Background(), call)
		if err != nil || result.Suspension == nil || result.Suspension.Reason != "waiting_user" || result.Fail != nil || result.Outputs != nil {
			t.Fatalf("result=%+v err=%v", result, err)
		}
		if len(result.Suspension.Payload) != 2 || result.Suspension.Payload["question"] != "continue?" {
			t.Fatalf("payload=%+v", result.Suspension.Payload)
		}
		payloadOption := result.Suspension.Payload["options"].([]any)[0].(map[string]any)
		if payloadOption["value"] != json.Number("9007199254740993") {
			t.Fatalf("large number=%T(%v)", payloadOption["value"], payloadOption["value"])
		}
		payloadOption["label"] = "mutated"
		if callerOption["label"] != "yes" {
			t.Fatal("confirmation payload aliases caller input")
		}
	})

	for name, inputs := range map[string]map[string]any{
		"missing question": nil,
		"blank question":   {"question": "  "},
		"question type":    {"question": true},
		"options type":     {"question": "continue?", "options": "yes"},
	} {
		t.Run(name, func(t *testing.T) {
			result, err := NewRequestConfirmationTool().Run(context.Background(), Call{Inputs: inputs})
			if err != nil || result.Fail == nil || result.Fail.Code != "invalid_request" || result.Suspension != nil || result.Outputs != nil {
				t.Fatalf("result=%+v err=%v", result, err)
			}
		})
	}

	t.Run("unserializable input returns an error", func(t *testing.T) {
		result, err := NewRequestConfirmationTool().Run(context.Background(), Call{Inputs: map[string]any{
			"question": "continue?", "bad": func() {},
		}})
		if err == nil || !reflect.DeepEqual(result, Result{}) {
			t.Fatalf("result=%+v err=%v", result, err)
		}
	})

	t.Run("cancelled context returns cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := NewRequestConfirmationTool().Run(ctx, Call{Inputs: map[string]any{"question": "continue?"}})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err=%v", err)
		}
	})
}

func TestRuntimeToolsDispatchGenerationBoundaries(t *testing.T) {
	t.Run("request is frozen, cloned, and idempotent replay is delegated", func(t *testing.T) {
		jobIDs := []string{"job-1", "job-2"}
		dispatcher := &fakeGenerationDispatcher{
			result: GenerationDispatchResult{OperationID: "operation-1", BatchID: "batch-1", JobIDs: jobIDs},
			mutate: true,
		}
		tool := NewDispatchGenerationTool(dispatcher)
		callerTarget := map[string]any{"prompt": "cinematic rain", "seed": int64(9007199254740993)}
		call := validDispatchCall()
		call.Inputs = map[string]any{
			"targets": []any{callerTarget}, "provider_policy": map[string]any{"mode": "auto"},
		}

		first, err := tool.Run(context.Background(), call)
		if err != nil {
			t.Fatalf("first dispatch: %v", err)
		}
		second, err := tool.Run(context.Background(), call)
		if err != nil {
			t.Fatalf("replayed dispatch: %v", err)
		}
		if first.Outputs["batch_id"] != "batch-1" || second.Outputs["batch_id"] != "batch-1" || dispatcher.effects != 1 {
			t.Fatalf("first=%+v second=%+v effects=%d", first, second, dispatcher.effects)
		}
		if len(dispatcher.requests) != 2 {
			t.Fatalf("dispatcher calls=%d, want 2", len(dispatcher.requests))
		}
		for _, request := range dispatcher.requests {
			if request.SessionID != call.SessionID || request.UserID != call.UserID ||
				request.PlanRunID != call.PlanRunID || request.NodeID != call.NodeID ||
				request.Attempt != call.Attempt || request.IdempotencyKey != call.IdempotencyKey {
				t.Fatalf("request context=%+v", request)
			}
			seed := request.Inputs["targets"].([]any)[0].(map[string]any)["seed"]
			if seed != json.Number("9007199254740993") {
				t.Fatalf("request seed=%T(%v)", seed, seed)
			}
		}
		if callerTarget["prompt"] != "cinematic rain" {
			t.Fatal("dispatcher request aliases caller input")
		}
		if first.Suspension == nil || first.Suspension.Reason != "waiting_jobs" || first.Suspension.Payload["batch_id"] != "batch-1" {
			t.Fatalf("suspension=%+v", first.Suspension)
		}
		jobIDs[0] = "fake mutation"
		if got := first.Outputs["job_ids"].([]string)[0]; got != "job-1" {
			t.Fatalf("output aliases dispatcher result: %q", got)
		}
		first.Outputs["job_ids"].([]string)[0] = "output mutation"
		if got := first.Suspension.Payload["job_ids"].([]string)[0]; got != "job-1" {
			t.Fatalf("payload aliases outputs: %q", got)
		}
	})

	invalidCalls := map[string]func(*Call){
		"session":         func(call *Call) { call.SessionID = " " },
		"user":            func(call *Call) { call.UserID = "" },
		"plan run":        func(call *Call) { call.PlanRunID = "" },
		"node":            func(call *Call) { call.NodeID = "" },
		"attempt":         func(call *Call) { call.Attempt = 0 },
		"idempotency key": func(call *Call) { call.IdempotencyKey = "" },
		"targets missing": func(call *Call) { call.Inputs = map[string]any{} },
		"targets type":    func(call *Call) { call.Inputs = map[string]any{"targets": "one"} },
		"targets empty":   func(call *Call) { call.Inputs = map[string]any{"targets": []any{}} },
	}
	for name, invalidate := range invalidCalls {
		t.Run("invalid "+name, func(t *testing.T) {
			call := validDispatchCall()
			invalidate(&call)
			dispatcher := &fakeGenerationDispatcher{result: GenerationDispatchResult{BatchID: "unused"}}
			result, err := NewDispatchGenerationTool(dispatcher).Run(context.Background(), call)
			if err != nil || result.Fail == nil || result.Fail.Code != "invalid_request" || len(dispatcher.requests) != 0 {
				t.Fatalf("result=%+v err=%v dispatcher calls=%d", result, err, len(dispatcher.requests))
			}
		})
	}

	t.Run("dispatcher error remains a Go error", func(t *testing.T) {
		cause := errors.New("queue unavailable")
		result, err := NewDispatchGenerationTool(&fakeGenerationDispatcher{err: cause}).Run(context.Background(), validDispatchCall())
		if !errors.Is(err, cause) || !reflect.DeepEqual(result, Result{}) {
			t.Fatalf("result=%+v err=%v", result, err)
		}
	})

	t.Run("empty batch is an invalid infrastructure receipt", func(t *testing.T) {
		result, err := NewDispatchGenerationTool(&fakeGenerationDispatcher{result: GenerationDispatchResult{OperationID: "operation-1"}}).
			Run(context.Background(), validDispatchCall())
		if err == nil || !strings.Contains(err.Error(), "dispatch_generation") || !strings.Contains(err.Error(), "batch_id") || !reflect.DeepEqual(result, Result{}) {
			t.Fatalf("result=%+v err=%v", result, err)
		}
	})

	t.Run("invalid receipt does not cache failure for the same idempotency key", func(t *testing.T) {
		dispatcher := &scriptedGenerationDispatcher{results: []GenerationDispatchResult{
			{OperationID: "operation-1", BatchID: " "},
			{OperationID: "operation-1", BatchID: "batch-recovered", JobIDs: []string{"job-1"}},
		}}
		tool := NewDispatchGenerationTool(dispatcher)
		call := validDispatchCall()
		first, firstErr := tool.Run(context.Background(), call)
		second, secondErr := tool.Run(context.Background(), call)
		if firstErr == nil || !reflect.DeepEqual(first, Result{}) {
			t.Fatalf("first=%+v err=%v", first, firstErr)
		}
		if secondErr != nil || second.Outputs["batch_id"] != "batch-recovered" || dispatcher.calls != 2 {
			t.Fatalf("second=%+v err=%v calls=%d", second, secondErr, dispatcher.calls)
		}
	})

	t.Run("nil dependency returns an error", func(t *testing.T) {
		var typedNil *fakeGenerationDispatcher
		for _, tool := range []Tool{NewDispatchGenerationTool(nil), NewDispatchGenerationTool(typedNil)} {
			if _, err := tool.Run(context.Background(), validDispatchCall()); err == nil {
				t.Fatal("nil dispatcher must return an error")
			}
		}
	})

	t.Run("unserializable input returns an error", func(t *testing.T) {
		call := validDispatchCall()
		call.Inputs["bad"] = make(chan int)
		result, err := NewDispatchGenerationTool(&fakeGenerationDispatcher{result: GenerationDispatchResult{BatchID: "unused"}}).
			Run(context.Background(), call)
		if err == nil || !reflect.DeepEqual(result, Result{}) {
			t.Fatalf("result=%+v err=%v", result, err)
		}
	})

	t.Run("cancelled context is not dispatched", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		dispatcher := &fakeGenerationDispatcher{result: GenerationDispatchResult{BatchID: "unused"}}
		_, err := NewDispatchGenerationTool(dispatcher).Run(ctx, validDispatchCall())
		if !errors.Is(err, context.Canceled) || len(dispatcher.requests) != 0 {
			t.Fatalf("err=%v dispatcher calls=%d", err, len(dispatcher.requests))
		}
	})
}
