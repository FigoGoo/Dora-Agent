package writeprompts

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/compose"
)

const (
	nodeValidateIntent        = "validate_intent"
	nodeLoadStoryboardPreview = "load_storyboard_preview"
	nodeResolveExactTargets   = "resolve_exact_targets"
	nodeBuildPrompt           = "build_prompt"
	nodeCallModel             = "call_model"
	nodeValidateCandidate     = "validate_prompt_candidate"
	nodeValidateExactSet      = "validate_exact_target_set"
	nodeSavePromptDraft       = "save_prompt_preview_draft"
	nodeQuerySaveReceipt      = "query_save_receipt"
	nodeBuildSavedResult      = "build_saved_result"
	nodeBuildQueriedResult    = "build_queried_result"
	nodeEmitScopeFailed       = "emit_scope_failed"
	nodeEmitCandidateFailed   = "emit_candidate_failed"
	nodeEmitExactSetFailed    = "emit_exact_set_failed"
	nodeDeferRecovery         = "defer_recovery"
)

const (
	branchScopeValidation     = "route_scope_validation"
	branchCandidateValidation = "route_candidate_validation"
	branchExactSetValidation  = "route_exact_set_validation"
	branchSaveOutcome         = "route_save_outcome"
	branchQueryOutcome        = "route_query_outcome"
)

const (
	routeValid           = "valid"
	routeInvalid         = "invalid"
	routeSaved           = "saved"
	routeUnknown         = "unknown"
	routeRecoveryPending = "recovery_pending"
)

// GraphEdge 是设计与拓扑测试共同使用的稳定直接边；Branch 目标不重复计入。
type GraphEdge struct {
	// From 是上游 Node 或 START。
	From string
	// To 是下游 Node 或 END。
	To string
}

var graphEdges = []GraphEdge{
	{compose.START, nodeValidateIntent},
	{nodeValidateIntent, nodeLoadStoryboardPreview},
	{nodeLoadStoryboardPreview, nodeResolveExactTargets},
	{nodeBuildPrompt, nodeCallModel},
	{nodeCallModel, nodeValidateCandidate},
	{nodeBuildSavedResult, compose.END},
	{nodeBuildQueriedResult, compose.END},
	{nodeEmitScopeFailed, compose.END},
	{nodeEmitCandidateFailed, compose.END},
	{nodeEmitExactSetFailed, compose.END},
	{nodeDeferRecovery, compose.END},
}

// NodeKeys 返回 AllPredecessor 安全拓扑冻结的 15 个 Node exact-set。
func NodeKeys() []string {
	return []string{
		nodeValidateIntent, nodeLoadStoryboardPreview, nodeResolveExactTargets, nodeBuildPrompt, nodeCallModel,
		nodeValidateCandidate, nodeValidateExactSet, nodeSavePromptDraft, nodeQuerySaveReceipt,
		nodeBuildSavedResult, nodeBuildQueriedResult, nodeEmitScopeFailed, nodeEmitCandidateFailed,
		nodeEmitExactSetFailed, nodeDeferRecovery,
	}
}

// BranchKeys 返回批准设计冻结的五个 Branch exact-set。
func BranchKeys() []string {
	return []string{branchScopeValidation, branchCandidateValidation, branchExactSetValidation, branchSaveOutcome, branchQueryOutcome}
}

// GraphEdges 返回直接 Edge exact-set 的副本。
func GraphEdges() []GraphEdge { return append([]GraphEdge(nil), graphEdges...) }

// CompiledGraph 是启动阶段编译后可并发复用的 write_prompts Preview DAG。
type CompiledGraph struct {
	// runnable 是启动期完成拓扑与类型校验、运行期只读复用的 Eino 可执行对象。
	runnable compose.Runnable[GraphInput, Outcome]
	// clock 与 Graph 使用同一不可变时间源，供 Tool 层确定性失败 Card 使用。
	clock Clock
}

// Invoke 执行已编译 Graph；静态生产 Catalog 不会注册该 Core。
func (g *CompiledGraph) Invoke(ctx context.Context, input GraphInput) (Outcome, error) {
	if g == nil || g.runnable == nil {
		return Outcome{}, fmt.Errorf("invoke %s: graph is not compiled", GraphName)
	}
	return g.runnable.Invoke(ctx, input)
}

// graphBuilder 只在 Compile 阶段绑定不可变端口，并作为 Node/Branch 方法接收者；它不保存请求级状态。
type graphBuilder struct {
	// reader 执行一次 Owner 与 Source exact-match 的 Business 只读查询。
	reader BusinessContextReader
	// store 保存或查询原 Business Command，禁止节点自行更换幂等键。
	store BusinessDraftStore
	// journal 在首次 Save 前冻结完整命令与重发预算。
	journal CommandJournal
	// clock 为 Tool Result/Card 提供可测试 UTC 时间。
	clock Clock
}

// Compile 只装配批准的 Node、Branch、经典 Message 和无环 AllPredecessor DAG。
func Compile(
	ctx context.Context,
	chatModel model.BaseChatModel,
	reader BusinessContextReader,
	store BusinessDraftStore,
	journal CommandJournal,
	clock Clock,
) (*CompiledGraph, error) {
	if chatModel == nil || reader == nil || store == nil || journal == nil || clock == nil {
		return nil, fmt.Errorf("compile %s: dependency is nil", GraphName)
	}
	builder := &graphBuilder{reader: reader, store: store, journal: journal, clock: clock}
	graph := compose.NewGraph[GraphInput, Outcome](compose.WithGenLocalState(func(context.Context) *State { return &State{} }))
	for _, node := range []struct {
		key    string
		lambda *compose.Lambda
	}{
		{nodeValidateIntent, compose.InvokableLambda(builder.validateIntent)},
		{nodeLoadStoryboardPreview, compose.InvokableLambda(builder.loadStoryboardPreview)},
		{nodeResolveExactTargets, compose.InvokableLambda(builder.resolveExactTargets)},
		{nodeBuildPrompt, compose.InvokableLambda(builder.buildPrompt)},
		{nodeValidateCandidate, compose.InvokableLambda(builder.validateCandidate)},
		{nodeValidateExactSet, compose.InvokableLambda(builder.validateExactSet)},
		{nodeSavePromptDraft, compose.InvokableLambda(builder.savePromptDraft)},
		{nodeQuerySaveReceipt, compose.InvokableLambda(builder.querySaveReceipt)},
		{nodeBuildSavedResult, compose.InvokableLambda(builder.buildResult)},
		{nodeBuildQueriedResult, compose.InvokableLambda(builder.buildResult)},
		{nodeEmitScopeFailed, compose.InvokableLambda(builder.emitScopeFailed)},
		{nodeEmitCandidateFailed, compose.InvokableLambda(builder.emitCandidateFailed)},
		{nodeEmitExactSetFailed, compose.InvokableLambda(builder.emitExactSetFailed)},
		{nodeDeferRecovery, compose.InvokableLambda(builder.deferRecovery)},
	} {
		if err := graph.AddLambdaNode(node.key, node.lambda); err != nil {
			return nil, fmt.Errorf("add %s node: %w", node.key, err)
		}
	}
	if err := graph.AddChatModelNode(nodeCallModel, chatModel, compose.WithStatePostHandler(builder.captureModelMessage)); err != nil {
		return nil, fmt.Errorf("add %s node: %w", nodeCallModel, err)
	}
	for _, edge := range graphEdges {
		if err := graph.AddEdge(edge.From, edge.To); err != nil {
			return nil, fmt.Errorf("add graph edge %s -> %s: %w", edge.From, edge.To, err)
		}
	}
	if err := graph.AddBranch(nodeResolveExactTargets, compose.NewGraphBranch(builder.routeScope, map[string]bool{
		nodeBuildPrompt: true, nodeEmitScopeFailed: true,
	})); err != nil {
		return nil, fmt.Errorf("add %s branch: %w", branchScopeValidation, err)
	}
	if err := graph.AddBranch(nodeValidateCandidate, compose.NewGraphBranch(builder.routeCandidate, map[string]bool{
		nodeValidateExactSet: true, nodeEmitCandidateFailed: true,
	})); err != nil {
		return nil, fmt.Errorf("add %s branch: %w", branchCandidateValidation, err)
	}
	if err := graph.AddBranch(nodeValidateExactSet, compose.NewGraphBranch(builder.routeExactSet, map[string]bool{
		nodeSavePromptDraft: true, nodeEmitExactSetFailed: true,
	})); err != nil {
		return nil, fmt.Errorf("add %s branch: %w", branchExactSetValidation, err)
	}
	if err := graph.AddBranch(nodeSavePromptDraft, compose.NewGraphBranch(builder.routeSave, map[string]bool{
		nodeBuildSavedResult: true, nodeQuerySaveReceipt: true,
	})); err != nil {
		return nil, fmt.Errorf("add %s branch: %w", branchSaveOutcome, err)
	}
	if err := graph.AddBranch(nodeQuerySaveReceipt, compose.NewGraphBranch(builder.routeQuery, map[string]bool{
		nodeBuildQueriedResult: true, nodeDeferRecovery: true,
	})); err != nil {
		return nil, fmt.Errorf("add %s branch: %w", branchQueryOutcome, err)
	}
	runnable, err := graph.Compile(ctx, compose.WithGraphName(GraphName), compose.WithNodeTriggerMode(compose.AllPredecessor))
	if err != nil {
		return nil, fmt.Errorf("compile %s: %w", GraphName, err)
	}
	return &CompiledGraph{runnable: runnable, clock: clock}, nil
}
