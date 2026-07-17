package mediapreview

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/compose"
)

const (
	nodeValidateIntent     = "validate_intent"
	nodeFreezeScope        = "freeze_scope"
	nodeEnsureOperation    = "ensure_operation"
	nodePrepareAsset       = "prepare_asset"
	nodeQueryPreparation   = "query_preparation"
	nodeBuildJob           = "build_job"
	nodeDispatchJob        = "dispatch_job"
	nodeQueryDispatch      = "query_dispatch"
	nodeDeferRecovery      = "defer_recovery"
	nodeEmitAccepted       = "emit_accepted"
	nodeEmitFailed         = "emit_failed"
	branchValidateIntent   = "route_intent_validation"
	branchPrepareAsset     = "route_prepare_outcome"
	branchQueryPreparation = "route_preparation_query"
	branchDispatchJob      = "route_dispatch_outcome"
	branchQueryDispatch    = "route_dispatch_query"
)

// GraphEdge 是两个 Preview Graph 共享的稳定直接边；Branch 目标不重复计入。
type GraphEdge struct {
	// From 是 START 或上游 Node Key。
	From string
	// To 是下游 Node Key 或 END。
	To string
}

var mediaGraphEdges = []GraphEdge{
	{compose.START, nodeValidateIntent},
	{nodeFreezeScope, nodeEnsureOperation},
	{nodeEnsureOperation, nodePrepareAsset},
	{nodeBuildJob, nodeDispatchJob},
	{nodeDeferRecovery, compose.END},
	{nodeEmitAccepted, compose.END},
	{nodeEmitFailed, compose.END},
}

// NodeKeys 返回批准设计冻结的 11 个 Node exact-set。
func NodeKeys() []string {
	return []string{
		nodeValidateIntent, nodeFreezeScope, nodeEnsureOperation, nodePrepareAsset,
		nodeQueryPreparation, nodeBuildJob, nodeDispatchJob, nodeQueryDispatch,
		nodeDeferRecovery, nodeEmitAccepted, nodeEmitFailed,
	}
}

// BranchKeys 返回确定性 Unknown Outcome 路由使用的五个 Branch exact-set。
func BranchKeys() []string {
	return []string{
		branchValidateIntent, branchPrepareAsset, branchQueryPreparation,
		branchDispatchJob, branchQueryDispatch,
	}
}

// GraphEdges 返回直接 Edge exact-set 副本，调用方不能修改启动拓扑。
func GraphEdges() []GraphEdge { return append([]GraphEdge(nil), mediaGraphEdges...) }

// GenerateMediaGraph 是启动期编译后可并发复用的 generate_media Preview DAG。
type GenerateMediaGraph struct {
	runnable compose.Runnable[GenerateMediaGraphInput, GraphOutcome]
}

// Invoke 执行已编译 Graph；HTTP Handler 不得直接调用本方法。
func (g *GenerateMediaGraph) Invoke(ctx context.Context, input GenerateMediaGraphInput) (GraphOutcome, error) {
	if g == nil || g.runnable == nil {
		return GraphOutcome{}, fmt.Errorf("invoke %s: graph is not compiled", GenerateMediaGraphName)
	}
	return g.runnable.Invoke(ctx, input)
}

// AssembleOutputGraph 是启动期编译后可并发复用的 assemble_output Preview DAG。
type AssembleOutputGraph struct {
	runnable compose.Runnable[AssembleOutputGraphInput, GraphOutcome]
}

// Invoke 执行已编译 Graph；HTTP Handler 不得直接调用本方法。
func (g *AssembleOutputGraph) Invoke(ctx context.Context, input AssembleOutputGraphInput) (GraphOutcome, error) {
	if g == nil || g.runnable == nil {
		return GraphOutcome{}, fmt.Errorf("invoke %s: graph is not compiled", AssembleOutputGraphName)
	}
	return g.runnable.Invoke(ctx, input)
}

// CompileGenerateMediaGraph 装配 11 Node/5 Branch 并以 AllPredecessor 编译无环 DAG。
func CompileGenerateMediaGraph(
	ctx context.Context,
	business BusinessClient,
	repository Repository,
	clock Clock,
) (*GenerateMediaGraph, error) {
	runnable, err := compileMediaGraph(ctx, generateMediaDefinition(), business, repository, clock)
	if err != nil {
		return nil, err
	}
	return &GenerateMediaGraph{runnable: runnable}, nil
}

// CompileAssembleOutputGraph 装配相同 exact-set，并只替换严格 Intent/source/job pin。
func CompileAssembleOutputGraph(
	ctx context.Context,
	business BusinessClient,
	repository Repository,
	clock Clock,
) (*AssembleOutputGraph, error) {
	runnable, err := compileMediaGraph(ctx, assembleOutputDefinition(), business, repository, clock)
	if err != nil {
		return nil, err
	}
	return &AssembleOutputGraph{runnable: runnable}, nil
}

// compileMediaGraph 只负责拓扑装配；所有业务判断位于 nodes.go 的确定性 Node/Branch 方法。
func compileMediaGraph[Intent any](
	ctx context.Context,
	definition mediaGraphDefinition[Intent],
	business BusinessClient,
	repository Repository,
	clock Clock,
) (compose.Runnable[MediaGraphInput, GraphOutcome], error) {
	if business == nil || repository == nil || clock == nil {
		return nil, fmt.Errorf("compile %s: dependency is nil", definition.graphName)
	}
	builder := &mediaGraphBuilder[Intent]{
		definition: definition,
		business:   business,
		repository: repository,
		clock:      clock,
	}
	graph := compose.NewGraph[MediaGraphInput, GraphOutcome](compose.WithGenLocalState(func(context.Context) *mediaPreviewState[Intent] {
		return &mediaPreviewState[Intent]{}
	}))
	for _, node := range []struct {
		key    string
		lambda *compose.Lambda
	}{
		{nodeValidateIntent, compose.InvokableLambda(builder.validateIntent)},
		{nodeFreezeScope, compose.InvokableLambda(builder.freezeScope)},
		{nodeEnsureOperation, compose.InvokableLambda(builder.ensureOperation)},
		{nodePrepareAsset, compose.InvokableLambda(builder.prepareAsset)},
		{nodeQueryPreparation, compose.InvokableLambda(builder.queryPreparation)},
		{nodeBuildJob, compose.InvokableLambda(builder.buildJob)},
		{nodeDispatchJob, compose.InvokableLambda(builder.dispatchJob)},
		{nodeQueryDispatch, compose.InvokableLambda(builder.queryDispatch)},
		{nodeDeferRecovery, compose.InvokableLambda(builder.deferRecovery)},
		{nodeEmitAccepted, compose.InvokableLambda(builder.emitAccepted)},
		{nodeEmitFailed, compose.InvokableLambda(builder.emitFailed)},
	} {
		if err := graph.AddLambdaNode(node.key, node.lambda); err != nil {
			return nil, fmt.Errorf("add %s node to %s: %w", node.key, definition.graphName, err)
		}
	}
	for _, edge := range mediaGraphEdges {
		if err := graph.AddEdge(edge.From, edge.To); err != nil {
			return nil, fmt.Errorf("add graph edge %s -> %s to %s: %w", edge.From, edge.To, definition.graphName, err)
		}
	}
	if err := graph.AddBranch(nodeValidateIntent, compose.NewGraphBranch(builder.routeIntent, map[string]bool{
		nodeFreezeScope: true, nodeEmitFailed: true,
	})); err != nil {
		return nil, fmt.Errorf("add %s branch to %s: %w", branchValidateIntent, definition.graphName, err)
	}
	if err := graph.AddBranch(nodePrepareAsset, compose.NewGraphBranch(builder.routePrepare, map[string]bool{
		nodeBuildJob: true, nodeQueryPreparation: true, nodeEmitFailed: true,
	})); err != nil {
		return nil, fmt.Errorf("add %s branch to %s: %w", branchPrepareAsset, definition.graphName, err)
	}
	if err := graph.AddBranch(nodeQueryPreparation, compose.NewGraphBranch(builder.routePreparationQuery, map[string]bool{
		nodeBuildJob: true, nodeDeferRecovery: true, nodeEmitFailed: true,
	})); err != nil {
		return nil, fmt.Errorf("add %s branch to %s: %w", branchQueryPreparation, definition.graphName, err)
	}
	if err := graph.AddBranch(nodeDispatchJob, compose.NewGraphBranch(builder.routeDispatch, map[string]bool{
		nodeEmitAccepted: true, nodeQueryDispatch: true, nodeEmitFailed: true,
	})); err != nil {
		return nil, fmt.Errorf("add %s branch to %s: %w", branchDispatchJob, definition.graphName, err)
	}
	if err := graph.AddBranch(nodeQueryDispatch, compose.NewGraphBranch(builder.routeDispatchQuery, map[string]bool{
		nodeEmitAccepted: true, nodeDeferRecovery: true, nodeEmitFailed: true,
	})); err != nil {
		return nil, fmt.Errorf("add %s branch to %s: %w", branchQueryDispatch, definition.graphName, err)
	}
	runnable, err := graph.Compile(ctx,
		compose.WithGraphName(definition.graphName),
		compose.WithNodeTriggerMode(compose.AllPredecessor),
	)
	if err != nil {
		return nil, fmt.Errorf("compile %s: %w", definition.graphName, err)
	}
	return runnable, nil
}
