package planstoryboard

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

const (
	nodeValidateIntent          = "validate_intent"
	nodeLoadCreationSpec        = "load_creation_spec"
	nodeBuildPrompt             = "build_prompt"
	nodeCallModel               = "call_model"
	nodeValidateCandidate       = "validate_candidate"
	nodeValidateDependencyGraph = "validate_dependency_graph"
	nodeSaveStoryboardDraft     = "save_storyboard_draft"
	nodeQuerySaveReceipt        = "query_save_receipt"
	nodeBuildSavedResult        = "build_saved_result"
	nodeBuildQueriedResult      = "build_queried_result"
	nodeEmitCandidateFailed     = "emit_candidate_failed"
	nodeEmitDependencyFailed    = "emit_dependency_failed"
	nodeDeferRecovery           = "defer_recovery"
)

const (
	branchCandidateValidation  = "route_candidate_validation"
	branchDependencyValidation = "route_dependency_validation"
	branchSaveOutcome          = "route_save_outcome"
	branchQueryOutcome         = "route_query_outcome"
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
	{nodeValidateIntent, nodeLoadCreationSpec},
	{nodeLoadCreationSpec, nodeBuildPrompt},
	{nodeBuildPrompt, nodeCallModel},
	{nodeCallModel, nodeValidateCandidate},
	{nodeBuildSavedResult, compose.END},
	{nodeBuildQueriedResult, compose.END},
	{nodeEmitCandidateFailed, compose.END},
	{nodeEmitDependencyFailed, compose.END},
	{nodeDeferRecovery, compose.END},
}

// NodeKeys 返回 AllPredecessor 安全拓扑冻结的 13 个 Node exact-set。
func NodeKeys() []string {
	return []string{
		nodeValidateIntent, nodeLoadCreationSpec, nodeBuildPrompt, nodeCallModel, nodeValidateCandidate,
		nodeValidateDependencyGraph, nodeSaveStoryboardDraft, nodeQuerySaveReceipt,
		nodeBuildSavedResult, nodeBuildQueriedResult, nodeEmitCandidateFailed, nodeEmitDependencyFailed, nodeDeferRecovery,
	}
}

// BranchKeys 返回批准设计冻结的四个 Branch exact-set。
func BranchKeys() []string {
	return []string{branchCandidateValidation, branchDependencyValidation, branchSaveOutcome, branchQueryOutcome}
}

// GraphEdges 返回直接 Edge exact-set 的副本。
func GraphEdges() []GraphEdge { return append([]GraphEdge(nil), graphEdges...) }

// CompiledGraph 是启动阶段编译后可并发复用的 plan_storyboard Preview DAG。
type CompiledGraph struct {
	runnable compose.Runnable[GraphInput, Outcome]
}

// Invoke 执行已编译 Graph；静态生产 Catalog 不会注册该 Core。
func (g *CompiledGraph) Invoke(ctx context.Context, input GraphInput) (Outcome, error) {
	if g == nil || g.runnable == nil {
		return Outcome{}, fmt.Errorf("invoke %s: graph is not compiled", GraphName)
	}
	return g.runnable.Invoke(ctx, input)
}

type graphBuilder struct {
	reader         BusinessContextReader
	store          BusinessDraftStore
	journal        CommandJournal
	clock          Clock
	promptTemplate primaryPromptTemplate
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
	builder := &graphBuilder{reader: reader, store: store, journal: journal, clock: clock, promptTemplate: newPrimaryPromptTemplate()}
	graph := compose.NewGraph[GraphInput, Outcome](compose.WithGenLocalState(func(context.Context) *State { return &State{} }))
	for _, node := range []struct {
		key    string
		lambda *compose.Lambda
	}{
		{nodeValidateIntent, compose.InvokableLambda(builder.validateIntent)},
		{nodeLoadCreationSpec, compose.InvokableLambda(builder.loadCreationSpec)},
		{nodeBuildPrompt, compose.InvokableLambda(builder.buildPrompt)},
		{nodeValidateCandidate, compose.InvokableLambda(builder.validateCandidate)},
		{nodeValidateDependencyGraph, compose.InvokableLambda(builder.validateDependencyGraph)},
		{nodeSaveStoryboardDraft, compose.InvokableLambda(builder.saveStoryboardDraft)},
		{nodeQuerySaveReceipt, compose.InvokableLambda(builder.querySaveReceipt)},
		{nodeBuildSavedResult, compose.InvokableLambda(builder.buildResult)},
		{nodeBuildQueriedResult, compose.InvokableLambda(builder.buildResult)},
		{nodeEmitCandidateFailed, compose.InvokableLambda(builder.emitFailed)},
		{nodeEmitDependencyFailed, compose.InvokableLambda(builder.emitFailed)},
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
	if err := graph.AddBranch(nodeValidateCandidate, compose.NewGraphBranch(builder.routeCandidate, map[string]bool{
		nodeValidateDependencyGraph: true, nodeEmitCandidateFailed: true,
	})); err != nil {
		return nil, fmt.Errorf("add %s branch: %w", branchCandidateValidation, err)
	}
	if err := graph.AddBranch(nodeValidateDependencyGraph, compose.NewGraphBranch(builder.routeDependency, map[string]bool{
		nodeSaveStoryboardDraft: true, nodeEmitDependencyFailed: true,
	})); err != nil {
		return nil, fmt.Errorf("add %s branch: %w", branchDependencyValidation, err)
	}
	if err := graph.AddBranch(nodeSaveStoryboardDraft, compose.NewGraphBranch(builder.routeSave, map[string]bool{
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
	return &CompiledGraph{runnable: runnable}, nil
}

type validatedInput struct {
	TrustedContext TrustedContext
	Intent         Intent
	IntentDigest   string
}

type planningInput struct {
	TrustedContext TrustedContext
	Intent         Intent
	IntentDigest   string
	Context        PlanningContext
}

type validationRoute struct {
	Route           string
	TrustedContext  TrustedContext
	Intent          Intent
	Context         PlanningContext
	Candidate       *Candidate
	Content         *Content
	CandidateDigest string
	FailureCode     string
}

func (b *graphBuilder) validateIntent(ctx context.Context, input GraphInput) (validatedInput, error) {
	if err := ValidateTrustedContext(input.TrustedContext); err != nil {
		return validatedInput{}, err
	}
	intent, err := DecodeIntent(input.IntentJSON)
	if err != nil {
		return validatedInput{}, err
	}
	digest, err := IntentDigest(intent)
	if err != nil {
		return validatedInput{}, err
	}
	result := validatedInput{TrustedContext: input.TrustedContext, Intent: intent, IntentDigest: digest}
	err = compose.ProcessState[*State](ctx, func(_ context.Context, state *State) error {
		state.TrustedContext = input.TrustedContext
		state.Intent = intent
		state.IntentDigest = digest
		state.CreationSpecRef = input.TrustedContext.CreationSpecRef
		return nil
	})
	return result, err
}

func (b *graphBuilder) loadCreationSpec(ctx context.Context, input validatedInput) (planningInput, error) {
	value, err := b.reader.GetStoryboardPlanningContext(ctx, input.TrustedContext)
	if err != nil {
		return planningInput{}, err
	}
	if err := ValidatePlanningContext(value, input.TrustedContext); err != nil {
		return planningInput{}, err
	}
	result := planningInput{TrustedContext: input.TrustedContext, Intent: input.Intent, IntentDigest: input.IntentDigest, Context: value}
	err = compose.ProcessState[*State](ctx, func(_ context.Context, state *State) error {
		state.CreationSpecContext = value
		return nil
	})
	return result, err
}

func (*graphBuilder) captureModelMessage(_ context.Context, message *schema.Message, state *State) (*schema.Message, error) {
	if message == nil || message.Role != schema.Assistant || strings.TrimSpace(message.Content) == "" ||
		len(message.ToolCalls) != 0 || message.ReasoningContent != "" || len(message.Extra) != 0 {
		return nil, fmt.Errorf("capture storyboard model message: invalid assistant response")
	}
	cloned := &schema.Message{Role: schema.Assistant, Content: message.Content}
	state.ModelMessage = cloned
	return &schema.Message{Role: cloned.Role, Content: cloned.Content}, nil
}

func (*graphBuilder) validateCandidate(ctx context.Context, message *schema.Message) (validationRoute, error) {
	if message == nil {
		return validationRoute{}, fmt.Errorf("validate storyboard candidate: model message is nil")
	}
	var snapshot State
	if err := compose.ProcessState[*State](ctx, func(_ context.Context, state *State) error {
		snapshot = *state
		return nil
	}); err != nil {
		return validationRoute{}, err
	}
	content, candidate, err := DecodeAndValidateCandidate([]byte(message.Content), snapshot.Intent, snapshot.CreationSpecContext)
	if err != nil {
		route := validationRoute{Route: routeInvalid, TrustedContext: snapshot.TrustedContext, Intent: snapshot.Intent,
			Context: snapshot.CreationSpecContext, FailureCode: ResultCodeCandidateInvalid}
		if stateErr := compose.ProcessState[*State](ctx, func(_ context.Context, state *State) error {
			state.ValidationReport = ValidationReport{CandidateValid: false, Code: ResultCodeCandidateInvalid}
			state.Error = ResultCodeCandidateInvalid
			return nil
		}); stateErr != nil {
			return validationRoute{}, stateErr
		}
		return route, nil
	}
	digest, err := CandidateDigest(candidate)
	if err != nil {
		return validationRoute{}, err
	}
	route := validationRoute{Route: routeValid, TrustedContext: snapshot.TrustedContext, Intent: snapshot.Intent,
		Context: snapshot.CreationSpecContext, Candidate: &candidate, Content: &content, CandidateDigest: digest}
	err = compose.ProcessState[*State](ctx, func(_ context.Context, state *State) error {
		candidateCopy := candidate
		state.Candidate = &candidateCopy
		state.CandidateDigest = digest
		state.ValidationReport = ValidationReport{CandidateValid: true}
		return nil
	})
	return route, err
}

func (*graphBuilder) validateDependencyGraph(ctx context.Context, input validationRoute) (validationRoute, error) {
	if input.Route != routeValid || input.Content == nil {
		return validationRoute{}, fmt.Errorf("validate storyboard dependency graph: invalid upstream route")
	}
	if err := ValidateDependencyGraph(*input.Content); err != nil {
		input.Route = routeInvalid
		input.FailureCode = ResultCodeDependencyInvalid
		if stateErr := compose.ProcessState[*State](ctx, func(_ context.Context, state *State) error {
			state.ValidationReport.DependencyValid = false
			state.ValidationReport.Code = ResultCodeDependencyInvalid
			state.Error = ResultCodeDependencyInvalid
			return nil
		}); stateErr != nil {
			return validationRoute{}, stateErr
		}
		return input, nil
	}
	input.Route = routeValid
	err := compose.ProcessState[*State](ctx, func(_ context.Context, state *State) error {
		state.ValidationReport.DependencyValid = true
		return nil
	})
	return input, err
}

func (b *graphBuilder) saveStoryboardDraft(ctx context.Context, input validationRoute) (SaveOutcome, error) {
	if input.Route != routeValid || input.Content == nil {
		return SaveOutcome{}, fmt.Errorf("save storyboard draft: invalid validator route")
	}
	command := DraftCommand{TrustedContext: input.TrustedContext, DomainContext: input.Context, Content: *input.Content}
	digest, err := SaveRequestDigest(command)
	if err != nil {
		return SaveOutcome{}, err
	}
	command.RequestDigest = digest
	if err := b.journal.PrepareCommand(ctx, command); err != nil {
		return SaveOutcome{}, err
	}
	disposition, resource, err := b.store.SaveStoryboardDraft(ctx, command)
	if err != nil {
		if errors.Is(err, ErrBusinessUnknownOutcome) {
			return SaveOutcome{Status: routeUnknown, Command: command}, nil
		}
		return SaveOutcome{}, err
	}
	if disposition != SaveDispositionCreated && disposition != SaveDispositionReplayed {
		return SaveOutcome{}, fmt.Errorf("save storyboard draft: invalid disposition")
	}
	if err := ValidateResourceForCommand(resource, command); err != nil {
		return SaveOutcome{}, err
	}
	return SaveOutcome{Status: routeSaved, Disposition: disposition, Resource: &resource, Command: command}, nil
}

func (b *graphBuilder) querySaveReceipt(ctx context.Context, input SaveOutcome) (SaveOutcome, error) {
	if input.Status != routeUnknown {
		return SaveOutcome{}, fmt.Errorf("query storyboard save receipt: invalid upstream route")
	}
	status, resource, err := b.store.QueryStoryboardDraftCommand(ctx, input.Command)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrBusinessConflict) {
			return SaveOutcome{}, err
		}
		return recoverySaveOutcome(input.Command)
	}
	switch status {
	case "completed":
		if resource == nil {
			return SaveOutcome{}, fmt.Errorf("query storyboard save receipt: completed resource is nil")
		}
		if err := ValidateResourceForCommand(*resource, input.Command); err != nil {
			return SaveOutcome{}, err
		}
		return SaveOutcome{Status: routeSaved, Disposition: SaveDispositionReplayed, Resource: resource, Command: input.Command}, nil
	case "not_found":
		return recoverySaveOutcome(input.Command)
	case "conflict":
		return SaveOutcome{}, ErrBusinessConflict
	default:
		return recoverySaveOutcome(input.Command)
	}
}

func recoverySaveOutcome(command DraftCommand) (SaveOutcome, error) {
	contentDigest, err := ContentDigest(command.Content)
	if err != nil {
		return SaveOutcome{}, err
	}
	recovery := &RecoveryDeferred{
		ToolCallID: command.TrustedContext.ToolCallID, BusinessCommandID: command.TrustedContext.BusinessCommandID,
		RequestDigest: command.RequestDigest, ContentDigest: contentDigest, Command: command, ResendLimit: 1,
	}
	return SaveOutcome{Status: routeRecoveryPending, Command: command, Recovery: recovery}, nil
}

func (b *graphBuilder) buildResult(ctx context.Context, input SaveOutcome) (Outcome, error) {
	if input.Status != routeSaved || input.Resource == nil {
		return Outcome{}, fmt.Errorf("build storyboard result: saved resource is missing")
	}
	resource := *input.Resource
	card := &Card{
		SchemaVersion: CardSchemaVersion, StoryboardPreviewID: resource.StoryboardPreviewID,
		ProjectID: resource.ProjectID, CreationSpecRef: resource.CreationSpecRef, Version: resource.Version,
		Status: resource.Status, ContentDigest: resource.ContentDigest, Title: resource.Content.Title,
		Summary: resource.Content.Summary, Sections: cloneSections(resource.Content.Sections),
		Elements: cloneElements(resource.Content.Elements), Slots: cloneSlots(resource.Content.Slots), UpdatedAt: b.clock.Now().UTC(),
	}
	result := &Result{
		SchemaVersion: ResultSchemaVersion, Status: "completed", ResultCode: ResultCodeCompleted,
		ResourceRef: &ResourceRef{StoryboardPreviewID: resource.StoryboardPreviewID, Version: resource.Version,
			Digest: resource.ContentDigest, Status: resource.Status, CreationSpecRef: resource.CreationSpecRef},
		InvocationRef: InvocationRef{ToolCallID: input.Command.TrustedContext.ToolCallID, BusinessCommandID: input.Command.TrustedContext.BusinessCommandID},
		Card:          card,
	}
	if err := ValidateTerminalResult(*result, input.Command.TrustedContext); err != nil {
		return Outcome{}, err
	}
	if err := compose.ProcessState[*State](ctx, func(_ context.Context, state *State) error { state.Result = result; return nil }); err != nil {
		return Outcome{}, err
	}
	return Outcome{Terminal: result}, nil
}

func (*graphBuilder) emitFailed(ctx context.Context, input validationRoute) (Outcome, error) {
	if input.Route != routeInvalid || input.FailureCode == "" {
		return Outcome{}, fmt.Errorf("emit storyboard failed: invalid failure route")
	}
	result := failedResult(input.TrustedContext, input.FailureCode)
	if err := ValidateTerminalResult(result, input.TrustedContext); err != nil {
		return Outcome{}, err
	}
	if err := compose.ProcessState[*State](ctx, func(_ context.Context, state *State) error { state.Result = &result; return nil }); err != nil {
		return Outcome{}, err
	}
	return Outcome{Terminal: &result}, nil
}

func (*graphBuilder) deferRecovery(_ context.Context, input SaveOutcome) (Outcome, error) {
	if input.Status != routeRecoveryPending || input.Recovery == nil {
		return Outcome{}, fmt.Errorf("defer storyboard recovery: recovery payload is missing")
	}
	return Outcome{Recovery: input.Recovery}, nil
}

func (*graphBuilder) routeCandidate(_ context.Context, input validationRoute) (string, error) {
	if input.Route == routeValid {
		return nodeValidateDependencyGraph, nil
	}
	if input.Route == routeInvalid {
		return nodeEmitCandidateFailed, nil
	}
	return "", fmt.Errorf("%s: unknown route", branchCandidateValidation)
}

func (*graphBuilder) routeDependency(_ context.Context, input validationRoute) (string, error) {
	if input.Route == routeValid {
		return nodeSaveStoryboardDraft, nil
	}
	if input.Route == routeInvalid {
		return nodeEmitDependencyFailed, nil
	}
	return "", fmt.Errorf("%s: unknown route", branchDependencyValidation)
}

func (*graphBuilder) routeSave(_ context.Context, input SaveOutcome) (string, error) {
	if input.Status == routeSaved {
		return nodeBuildSavedResult, nil
	}
	if input.Status == routeUnknown {
		return nodeQuerySaveReceipt, nil
	}
	return "", fmt.Errorf("%s: unknown route", branchSaveOutcome)
}

func (*graphBuilder) routeQuery(_ context.Context, input SaveOutcome) (string, error) {
	if input.Status == routeSaved {
		return nodeBuildQueriedResult, nil
	}
	if input.Status == routeRecoveryPending {
		return nodeDeferRecovery, nil
	}
	return "", fmt.Errorf("%s: unknown route", branchQueryOutcome)
}

func failedResult(trusted TrustedContext, code string) Result {
	summary := "无法生成 Storyboard 预览，请检查输入后重试。"
	retryable := false
	switch code {
	case ResultCodeCandidateInvalid:
		summary = "模型候选不符合 Storyboard 预览协议。"
	case ResultCodeDependencyInvalid:
		summary = "Storyboard 的引用或依赖关系无效。"
	case ResultCodeCreationSpecNotFound:
		summary = "CreationSpec 不存在或不可访问。"
	case ResultCodeCreationSpecConflict:
		summary = "CreationSpec 已发生变化，请刷新后重试。"
	case ResultCodeBusinessConflict:
		summary = "Storyboard 保存命令发生冲突，请刷新后重试。"
	case ResultCodeBusinessDisabled:
		summary = "Storyboard 预览当前未启用。"
	}
	return Result{
		SchemaVersion: ResultSchemaVersion, Status: "failed", ResultCode: code,
		InvocationRef: InvocationRef{ToolCallID: trusted.ToolCallID, BusinessCommandID: trusted.BusinessCommandID},
		Summary:       summary, Retryable: &retryable,
	}
}
