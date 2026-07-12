package vocabulary

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

type GenerationDispatchRequest struct {
	SessionID      string
	UserID         string
	PlanRunID      string
	NodeID         string
	Attempt        int
	IdempotencyKey string
	Inputs         map[string]any
}

type GenerationDispatchResult struct {
	OperationID string
	BatchID     string
	JobIDs      []string
}

type GenerationDispatcher interface {
	Dispatch(ctx context.Context, request GenerationDispatchRequest) (GenerationDispatchResult, error)
}

type dispatchGenerationTool struct {
	dispatcher GenerationDispatcher
}

func NewDispatchGenerationTool(dispatcher GenerationDispatcher) Tool {
	return &dispatchGenerationTool{dispatcher: dispatcher}
}

func (t *dispatchGenerationTool) Descriptor() Descriptor {
	return Descriptor{
		Key:         "dispatch_generation",
		Name:        "生成派发",
		Description: "批量创建生成任务并预注册生成中资产",
		Category:    "data",
		Inputs: map[string]ParamSpec{
			"targets": {Type: "array", Desc: "非空的媒体生成目标列表", Required: true},
		},
		Outputs: map[string]ParamSpec{
			"operation_id": {Type: "string", Desc: "生成操作标识"},
			"batch_id":     {Type: "string", Desc: "生成批次标识"},
			"job_ids":      {Type: "array", Desc: "生成任务标识列表"},
		},
	}
}

func (t *dispatchGenerationTool) Run(ctx context.Context, call Call) (Result, error) {
	if ctx == nil {
		return Result{}, errors.New("dispatch_generation context is required")
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if isNilDependency(t.dispatcher) {
		return Result{}, errors.New("dispatch_generation dispatcher is required")
	}
	inputs, err := cloneJSONMap(call.Inputs)
	if err != nil {
		return Result{}, fmt.Errorf("dispatch_generation inputs: %w", err)
	}
	if !validDispatchContext(call) {
		return invalidDispatchRequest("session, user, plan run, node, attempt, and idempotency key are required"), nil
	}
	targets, ok := inputs["targets"].([]any)
	if !ok || len(targets) == 0 {
		return invalidDispatchRequest("targets must be a non-empty array"), nil
	}
	dispatched, err := t.dispatcher.Dispatch(ctx, GenerationDispatchRequest{
		SessionID: call.SessionID, UserID: call.UserID, PlanRunID: call.PlanRunID,
		NodeID: call.NodeID, Attempt: call.Attempt, IdempotencyKey: call.IdempotencyKey,
		Inputs: inputs,
	})
	if err != nil {
		return Result{}, fmt.Errorf("dispatch_generation: %w", err)
	}
	if strings.TrimSpace(dispatched.BatchID) == "" {
		return Result{}, errors.New("dispatch_generation: dispatcher returned an empty batch_id")
	}
	outputJobIDs := append([]string(nil), dispatched.JobIDs...)
	payloadJobIDs := append([]string(nil), dispatched.JobIDs...)
	return Result{
		Outputs: map[string]any{
			"operation_id": dispatched.OperationID,
			"batch_id":     dispatched.BatchID,
			"job_ids":      outputJobIDs,
		},
		Suspension: &Suspension{Reason: "waiting_jobs", Payload: map[string]any{
			"batch_id": dispatched.BatchID,
			"job_ids":  payloadJobIDs,
		}},
	}, nil
}

func validDispatchContext(call Call) bool {
	return strings.TrimSpace(call.SessionID) != "" && strings.TrimSpace(call.UserID) != "" &&
		strings.TrimSpace(call.PlanRunID) != "" && strings.TrimSpace(call.NodeID) != "" &&
		call.Attempt > 0 && strings.TrimSpace(call.IdempotencyKey) != ""
}

func invalidDispatchRequest(message string) Result {
	return Result{Fail: &Failure{Code: "invalid_request", Message: message}}
}
