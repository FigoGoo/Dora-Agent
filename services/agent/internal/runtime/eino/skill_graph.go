package eino

import (
	"context"
	"errors"
	"fmt"

	"github.com/FigoGoo/Dora-Agent/services/agent/internal/runtime/skillgraph"
	"github.com/cloudwego/eino/compose"
)

const (
	SkillGraphRuntimeName = "skill_graph_runtime"

	skillGraphNodeCompile = "skill_graph_compile"
)

type SkillGraphRunner struct {
	runnable compose.Runnable[skillgraph.Input, skillgraph.Result]
}

func NewSkillGraphRunner(ctx context.Context, clock skillgraph.Clock) (*SkillGraphRunner, error) {
	runtime := skillgraph.New(clock)
	runnable, err := compileSkillGraph(ctx, runtime)
	if err != nil {
		return nil, err
	}
	return &SkillGraphRunner{runnable: runnable}, nil
}

func (r *SkillGraphRunner) Execute(ctx context.Context, input skillgraph.Input) (skillgraph.Result, error) {
	if r == nil || r.runnable == nil {
		return skillgraph.Result{}, errors.New("skill graph runner is not initialized")
	}
	return r.runnable.Invoke(ctx, input, compose.WithRuntimeMaxSteps(3))
}

func compileSkillGraph(ctx context.Context, runtime skillgraph.Runtime) (compose.Runnable[skillgraph.Input, skillgraph.Result], error) {
	graph := compose.NewGraph[skillgraph.Input, skillgraph.Result]()
	if err := graph.AddLambdaNode(skillGraphNodeCompile, compose.InvokableLambda(runtime.Execute, compose.WithLambdaType(skillGraphNodeCompile)), compose.WithNodeName(skillGraphNodeCompile)); err != nil {
		return nil, fmt.Errorf("add graph node %s: %w", skillGraphNodeCompile, err)
	}
	if err := graph.AddEdge(compose.START, skillGraphNodeCompile); err != nil {
		return nil, fmt.Errorf("add graph edge start -> %s: %w", skillGraphNodeCompile, err)
	}
	if err := graph.AddEdge(skillGraphNodeCompile, compose.END); err != nil {
		return nil, fmt.Errorf("add graph edge %s -> end: %w", skillGraphNodeCompile, err)
	}
	return graph.Compile(ctx, compose.WithGraphName(SkillGraphRuntimeName), compose.WithMaxRunSteps(3))
}
