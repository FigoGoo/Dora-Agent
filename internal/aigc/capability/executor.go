package capability

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/compose"
)

type Handler[I, D any] func(context.Context, Request[I]) (CapabilityResult[D], error)

// Executor is injectable so tests and production graphs can supply different
// node implementations without changing the Agent-facing Tool contract.
type Executor[I, D any] interface {
	Execute(context.Context, Request[I]) (CapabilityResult[D], error)
}

type ExecutorFunc[I, D any] func(context.Context, Request[I]) (CapabilityResult[D], error)

func (f ExecutorFunc[I, D]) Execute(ctx context.Context, request Request[I]) (CapabilityResult[D], error) {
	return f(ctx, request)
}

type boundedGraphExecutor[I, D any] struct {
	runnable compose.Runnable[Request[I], CapabilityResult[D]]
}

func (e *boundedGraphExecutor[I, D]) Execute(ctx context.Context, request Request[I]) (CapabilityResult[D], error) {
	if e == nil || e.runnable == nil {
		return CapabilityResult[D]{}, fmt.Errorf("capability graph executor is required")
	}
	return e.runnable.Invoke(ctx, request)
}

// CompileBoundedGraph compiles a finite, acyclic Eino graph at startup. The
// injected handler may itself call typed internal query, ChatModel, validation,
// command and dispatch nodes; those internal nodes are deliberately not Tools.
func CompileBoundedGraph[I, D any](ctx context.Context, name string, handler Handler[I, D]) (Executor[I, D], error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("capability graph name is required")
	}
	if handler == nil {
		return nil, fmt.Errorf("%s capability handler is required", name)
	}
	graph := compose.NewGraph[Request[I], CapabilityResult[D]]()
	validateRequest := func(_ context.Context, request Request[I]) (Request[I], error) {
		if strings.TrimSpace(request.Command.SessionID) == "" || strings.TrimSpace(request.Command.RequestID) == "" || strings.TrimSpace(request.Command.IdempotencyKey) == "" {
			return Request[I]{}, fmt.Errorf("%s capability requires trusted session, request and idempotency context", name)
		}
		return request, nil
	}
	execute := func(ctx context.Context, request Request[I]) (CapabilityResult[D], error) {
		return handler(ctx, request)
	}
	validateResult := func(_ context.Context, result CapabilityResult[D]) (CapabilityResult[D], error) {
		switch result.Status {
		case StatusCompleted, StatusAccepted, StatusWaitingUser, StatusPartial, StatusFailed, StatusCancelled:
			return result, nil
		default:
			return CapabilityResult[D]{}, fmt.Errorf("%s capability returned invalid result status %q", name, result.Status)
		}
	}
	if err := graph.AddLambdaNode("validate_request", compose.InvokableLambda(validateRequest)); err != nil {
		return nil, fmt.Errorf("add %s request validator node: %w", name, err)
	}
	if err := graph.AddLambdaNode("execute_capability", compose.InvokableLambda(execute)); err != nil {
		return nil, fmt.Errorf("add %s capability graph node: %w", name, err)
	}
	if err := graph.AddLambdaNode("validate_result", compose.InvokableLambda(validateResult)); err != nil {
		return nil, fmt.Errorf("add %s result validator node: %w", name, err)
	}
	if err := graph.AddEdge(compose.START, "validate_request"); err != nil {
		return nil, fmt.Errorf("connect %s graph start: %w", name, err)
	}
	if err := graph.AddEdge("validate_request", "execute_capability"); err != nil {
		return nil, fmt.Errorf("connect %s request validator: %w", name, err)
	}
	if err := graph.AddEdge("execute_capability", "validate_result"); err != nil {
		return nil, fmt.Errorf("connect %s result validator: %w", name, err)
	}
	if err := graph.AddEdge("validate_result", compose.END); err != nil {
		return nil, fmt.Errorf("connect %s graph end: %w", name, err)
	}
	runnable, err := graph.Compile(ctx,
		compose.WithGraphName(name),
		compose.WithNodeTriggerMode(compose.AllPredecessor),
	)
	if err != nil {
		return nil, fmt.Errorf("compile %s capability graph: %w", name, err)
	}
	return &boundedGraphExecutor[I, D]{runnable: runnable}, nil
}

type Handlers struct {
	AnalyzeMaterials Handler[AnalyzeMaterialsIntent, AnalyzeMaterialsData]
	PlanCreationSpec Handler[PlanCreationSpecIntent, PlanCreationSpecData]
	PlanStoryboard   Handler[PlanStoryboardIntent, PlanStoryboardData]
	GenerateMedia    Handler[GenerateMediaIntent, GenerateMediaData]
	AssembleOutput   Handler[AssembleOutputIntent, AssembleOutputData]
}

type Executors struct {
	AnalyzeMaterials Executor[AnalyzeMaterialsIntent, AnalyzeMaterialsData]
	PlanCreationSpec Executor[PlanCreationSpecIntent, PlanCreationSpecData]
	PlanStoryboard   Executor[PlanStoryboardIntent, PlanStoryboardData]
	GenerateMedia    Executor[GenerateMediaIntent, GenerateMediaData]
	AssembleOutput   Executor[AssembleOutputIntent, AssembleOutputData]
}

func CompileExecutors(ctx context.Context, handlers Handlers) (Executors, error) {
	analyze, err := CompileBoundedGraph(ctx, AnalyzeMaterialsToolKey, handlers.AnalyzeMaterials)
	if err != nil {
		return Executors{}, err
	}
	spec, err := CompileBoundedGraph(ctx, PlanCreationSpecToolKey, handlers.PlanCreationSpec)
	if err != nil {
		return Executors{}, err
	}
	board, err := CompileBoundedGraph(ctx, PlanStoryboardToolKey, handlers.PlanStoryboard)
	if err != nil {
		return Executors{}, err
	}
	media, err := CompileBoundedGraph(ctx, GenerateMediaToolKey, handlers.GenerateMedia)
	if err != nil {
		return Executors{}, err
	}
	assembly, err := CompileBoundedGraph(ctx, AssembleOutputToolKey, handlers.AssembleOutput)
	if err != nil {
		return Executors{}, err
	}
	return Executors{
		AnalyzeMaterials: analyze,
		PlanCreationSpec: spec,
		PlanStoryboard:   board,
		GenerateMedia:    media,
		AssembleOutput:   assembly,
	}, nil
}
