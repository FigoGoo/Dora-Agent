package analyzematerials

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/FigoGoo/Dora-Agent/agent/internal/turncontext"
)

const (
	toolTestUserID     = "019b78a0-11a2-7b01-8a11-111111111111"
	toolTestProjectID  = "019b78a0-11a2-7b02-8a22-222222222222"
	toolTestSessionID  = "019b78a0-11a2-7b03-8a33-333333333333"
	toolTestInputID    = "019b78a0-11a2-7b04-8a44-444444444444"
	toolTestTurnID     = "019b78a0-11a2-7b05-8a55-555555555555"
	toolTestRunID      = "019b78a0-11a2-7b06-8a66-666666666666"
	toolTestToolCallID = "019b78a0-11a2-7b07-8a77-777777777777"
	toolTestAssetID    = "019b78a0-11a2-7b08-8a88-888888888888"
	toolTestEvidenceID = "019b78a0-11a2-7b09-8a99-999999999999"
)

type toolGraphStub struct {
	outcome Outcome
	err     error
	calls   int
	input   GraphInput
}

func (stub *toolGraphStub) Invoke(_ context.Context, input GraphInput) (Outcome, error) {
	stub.calls++
	stub.input = input
	return stub.outcome, stub.err
}

func TestNewToolRejectsUncompiledGraph(t *testing.T) {
	t.Parallel()
	for _, graph := range []*CompiledGraph{nil, &CompiledGraph{}} {
		if tool, err := NewTool(graph); err == nil || tool != nil {
			t.Fatalf("NewTool(%v) tool=%v error=%v", graph, tool, err)
		}
	}
}

func TestToolMissingTrustedContextFailsClosed(t *testing.T) {
	t.Parallel()
	stub := &toolGraphStub{outcome: completedToolOutcome("completed")}
	tool := &Tool{graph: stub}

	output, err := tool.InvokableRun(context.Background(), validToolArguments())
	if err == nil || output != "" {
		t.Fatalf("InvokableRun() output=%q error=%v, want empty output and error", output, err)
	}
	if !strings.Contains(err.Error(), "trusted turn context is missing") || stub.calls != 0 {
		t.Fatalf("missing context error=%v graph calls=%d", err, stub.calls)
	}
}

func TestToolStrictInputFailureMapsSafeJSON(t *testing.T) {
	t.Parallel()
	stub := &toolGraphStub{outcome: completedToolOutcome("completed")}
	tool := &Tool{graph: stub}
	arguments := `{"schema_version":"analyze_materials.preview.intent.v1","asset_ids":["` + toolTestAssetID +
		`"],"analysis_goal":"do not echo this secret","focus_dimensions":["content"],"output_language":"zh-CN","expected_assets":[{"asset_id":"` +
		toolTestAssetID + `","asset_version":1,"user_id":"forged"}]}`

	output, err := tool.InvokableRun(toolTestContext(), arguments)
	if err != nil {
		t.Fatalf("InvokableRun() error = %v", err)
	}
	if stub.calls != 0 {
		t.Fatalf("strict input failure called graph %d times", stub.calls)
	}
	result := decodeToolResult(t, output)
	assertFailedToolResult(t, result, ResultCodeInvalidArgument)
	if strings.Contains(output, "do not echo this secret") || strings.Contains(output, "forged") ||
		strings.Contains(output, toolTestUserID) || strings.Contains(output, toolTestProjectID) {
		t.Fatalf("failed result leaked untrusted or trusted fields: %s", output)
	}
}

func TestToolInjectsTrustedContextOutsideIntentJSON(t *testing.T) {
	t.Parallel()
	stub := &toolGraphStub{outcome: completedToolOutcome("completed")}
	output, err := (&Tool{graph: stub}).InvokableRun(toolTestContext(), validToolArguments())
	if err != nil || output == "" {
		t.Fatalf("InvokableRun() output=%q error=%v", output, err)
	}
	trusted := stub.input.TrustedContext
	if trusted.UserID != toolTestUserID || trusted.ProjectID != toolTestProjectID ||
		trusted.SessionID != toolTestSessionID || trusted.InputID != toolTestInputID ||
		trusted.TurnID != toolTestTurnID || trusted.RunID != toolTestRunID ||
		trusted.ToolCallID != toolTestToolCallID || trusted.FenceToken != 1 ||
		trusted.PromptVersion != PromptVersion || trusted.ValidatorVersion != ValidatorVersion ||
		trusted.EvidencePolicyVersion != EvidencePolicyVersion {
		t.Fatalf("trusted graph input = %+v", trusted)
	}
	intentJSON := string(stub.input.IntentJSON)
	for _, forbidden := range []string{
		toolTestUserID, toolTestProjectID, toolTestSessionID, toolTestInputID, toolTestTurnID,
		toolTestRunID, toolTestToolCallID, "fence_token", "prompt_version", "validator_version",
	} {
		if strings.Contains(intentJSON, forbidden) {
			t.Fatalf("strict intent JSON exposed trusted value %q: %s", forbidden, intentJSON)
		}
	}
}

func TestToolResultJSONShapes(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name     string
		outcome  Outcome
		wantCode string
		wantKeys []string
	}{
		{
			name: "completed", outcome: completedToolOutcome("completed"), wantCode: ResultCodeCompleted,
			wantKeys: []string{"analysis", "coverage", "evidence_refs", "invocation_ref", "result_code", "schema_version", "status"},
		},
		{
			name: "partial", outcome: completedToolOutcome("partial"), wantCode: ResultCodePartial,
			wantKeys: []string{"analysis", "coverage", "evidence_refs", "invocation_ref", "result_code", "schema_version", "status"},
		},
		{
			name: "failed",
			outcome: Outcome{Result: Result{
				Status: "failed", ResultCode: ResultCodeDependencyNotReady,
				Summary: "loader secret must never cross the boundary",
			}},
			wantCode: ResultCodeDependencyNotReady,
			wantKeys: []string{"invocation_ref", "result_code", "retryable", "schema_version", "status", "summary"},
		},
	}
	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			stub := &toolGraphStub{outcome: testCase.outcome}
			tool := &Tool{graph: stub}
			output, err := tool.InvokableRun(toolTestContext(), validToolArguments())
			if err != nil {
				t.Fatalf("InvokableRun() error = %v", err)
			}
			if stub.calls != 1 {
				t.Fatalf("graph calls = %d, want 1", stub.calls)
			}
			result := decodeToolResult(t, output)
			if result.ResultCode != testCase.wantCode {
				t.Fatalf("result code = %q, want %q", result.ResultCode, testCase.wantCode)
			}
			var shape map[string]json.RawMessage
			if err := json.Unmarshal([]byte(output), &shape); err != nil {
				t.Fatalf("decode result shape: %v", err)
			}
			keys := make([]string, 0, len(shape))
			for key := range shape {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			if !reflect.DeepEqual(keys, testCase.wantKeys) {
				t.Fatalf("result keys = %v, want %v; result=%s", keys, testCase.wantKeys, output)
			}
			if strings.Contains(output, "loader secret") || strings.Contains(output, toolTestUserID) ||
				strings.Contains(output, toolTestProjectID) || strings.Contains(output, "Content") {
				t.Fatalf("result leaked sensitive fields: %s", output)
			}
		})
	}
}

func TestToolGraphErrorAndCancellationBoundary(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		name      string
		err       error
		wantError bool
		wantCode  string
		forbidden string
	}{
		{name: "unknown is safe internal", err: errors.New("provider secret payload"), wantCode: ResultCodeInternal, forbidden: "provider secret payload"},
		{name: "cancellation returns to runner", err: context.Canceled, wantError: true},
		{name: "deadline returns to runner", err: context.DeadlineExceeded, wantError: true},
	} {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			stub := &toolGraphStub{err: testCase.err}
			output, err := (&Tool{graph: stub}).InvokableRun(toolTestContext(), validToolArguments())
			if testCase.wantError {
				if !errors.Is(err, testCase.err) || output != "" {
					t.Fatalf("InvokableRun() output=%q error=%v", output, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("InvokableRun() error = %v", err)
			}
			result := decodeToolResult(t, output)
			assertFailedToolResult(t, result, testCase.wantCode)
			if testCase.forbidden != "" && strings.Contains(output, testCase.forbidden) {
				t.Fatalf("safe failure leaked internal error: %s", output)
			}
		})
	}
}

func validToolArguments() string {
	return `{"schema_version":"analyze_materials.preview.intent.v1","asset_ids":["` + toolTestAssetID +
		`"],"analysis_goal":"分析素材主题","focus_dimensions":["content"],"output_language":"zh-CN"}`
}

func toolTestContext() context.Context {
	return turncontext.WithMaterialAnalysisPreview(context.Background(), turncontext.MaterialAnalysisPreview{
		Owner: "analyze-materials-test", UserID: toolTestUserID, ProjectID: toolTestProjectID,
		SessionID: toolTestSessionID, InputID: toolTestInputID, TurnID: toolTestTurnID,
		RunID: toolTestRunID, ToolCallID: toolTestToolCallID, FenceToken: 1,
		PromptVersion: PromptVersion, ValidatorVersion: ValidatorVersion,
		EvidencePolicyVersion: EvidencePolicyVersion,
	})
}

func completedToolOutcome(status string) Outcome {
	focus := []string{"content"}
	if status == "partial" {
		focus = append(focus, "risk")
	}
	intent := graphTestIntent(focus...)
	assets, ready, missing, err := NormalizeEvidence(intent, graphTestSnapshot("ready"))
	if err != nil {
		panic(err)
	}
	included, missing, err := SelectPromptEvidence(intent, assets, ready, missing)
	if err != nil {
		panic(err)
	}
	coverage, err := EvaluateCoverage(intent, assets, included, missing)
	if err != nil {
		panic(err)
	}
	resultCode := ResultCodeCompleted
	if coverage.Status == "partial" {
		resultCode = ResultCodePartial
	}
	candidate := graphTestCandidate()
	return Outcome{Result: Result{
		SchemaVersion: ResultSchemaVersion, Status: coverage.Status, ResultCode: resultCode,
		Analysis: &candidate, Coverage: &coverage, EvidenceRefs: evidenceRefs(included),
		InvocationRef: InvocationRef{ToolCallID: toolTestToolCallID},
	}}
}

func decodeToolResult(t *testing.T, output string) Result {
	t.Helper()
	var result Result
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("decode result: %v; output=%s", err, output)
	}
	return result
}

func assertFailedToolResult(t *testing.T, result Result, wantCode string) {
	t.Helper()
	if result.SchemaVersion != ResultSchemaVersion || result.Status != "failed" || result.ResultCode != wantCode ||
		result.Analysis != nil || result.Coverage != nil || len(result.EvidenceRefs) != 0 ||
		result.InvocationRef.ToolCallID != toolTestToolCallID || result.Summary == "" ||
		result.Retryable == nil || *result.Retryable {
		t.Fatalf("failed result = %+v", result)
	}
}
