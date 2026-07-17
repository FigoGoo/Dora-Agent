package plancreationspec

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/prompt"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

const (
	nodeValidateIntent   = "validate_intent"
	nodeLoadContext      = "load_context"
	nodeRenderPrompt     = "render_prompt"
	nodeCallModel        = "call_model"
	nodeValidateProposal = "validate_proposal"
	nodeSaveDraft        = "save_draft"
	nodeQuerySaveReceipt = "query_save_receipt"
	nodeBuildResult      = "build_result"
	nodeDeferRecovery    = "defer_recovery"
)

var (
	// ErrBusinessUnknownOutcome 表示 Save RPC 可能已提交，调用方只能查询原 command_id 与摘要。
	ErrBusinessUnknownOutcome = errors.New("creation spec Business command outcome is unknown")
	// ErrBusinessConflict 表示 Business 发现命令摘要或 Project 版本冲突，禁止换键覆盖。
	ErrBusinessConflict = errors.New("creation spec Business command conflict")
	// ErrBusinessNotFound 表示 Project 不存在或不属于可信用户，统一折叠避免资源枚举。
	ErrBusinessNotFound = errors.New("creation spec Business project not found")
	// ErrBusinessTechnical 表示确定未提交副作用的可重试技术失败，不得冻结 terminal Result。
	ErrBusinessTechnical = errors.New("creation spec Business technical unavailable")
	// ErrBusinessDisabled 表示 Business Preview 能力确定关闭。
	ErrBusinessDisabled = errors.New("creation spec Business preview disabled")
)

// NodeKeys 返回启动编译与拓扑测试共用的稳定 Node exact-set。
func NodeKeys() []string {
	return []string{
		nodeValidateIntent, nodeLoadContext, nodeRenderPrompt, nodeCallModel, nodeValidateProposal,
		nodeSaveDraft, nodeQuerySaveReceipt, nodeBuildResult, nodeDeferRecovery,
	}
}

// Clock 为完成 Card 投影冻结一个可测试 UTC 时间。
type Clock interface {
	// Now 返回当前时间；零值会使 Graph 失败关闭。
	Now() time.Time
}

// CompiledGraph 是启动阶段 Compile 后可并发复用的 plan_creation_spec DAG。
type CompiledGraph struct {
	runnable compose.Runnable[GraphInput, Outcome]
	business BusinessClient
	journal  ResultStore
	clock    Clock
}

// Invoke 执行已经编译的 Graph；调用方必须通过 Runner 内的 Graph Tool 进入，HTTP 不得直接调用。
func (g *CompiledGraph) Invoke(ctx context.Context, input GraphInput) (Outcome, error) {
	if g == nil || g.runnable == nil {
		return Outcome{}, fmt.Errorf("invoke plan_creation_spec graph: graph is not compiled")
	}
	return g.runnable.Invoke(ctx, input)
}

// graphBuilder 持有启动时注入依赖；Node 方法保持单一职责并可独立测试。
type graphBuilder struct {
	business BusinessClient
	journal  ResultStore
	clock    Clock
}

// Compile 在启动阶段组装并以 AllPredecessor 编译无环 DAG；任何 Node/Edge/Branch 错误阻止 Readiness。
func Compile(ctx context.Context, chatModel model.BaseChatModel, business BusinessClient, journal ResultStore, clock Clock) (*CompiledGraph, error) {
	if chatModel == nil || business == nil || journal == nil || clock == nil {
		return nil, fmt.Errorf("compile plan_creation_spec graph: dependency is nil")
	}
	builder := &graphBuilder{business: business, journal: journal, clock: clock}
	graph := compose.NewGraph[GraphInput, Outcome](compose.WithGenLocalState(func(context.Context) *State {
		return &State{}
	}))

	if err := graph.AddLambdaNode(nodeValidateIntent, compose.InvokableLambda(builder.validateIntent)); err != nil {
		return nil, fmt.Errorf("add %s node: %w", nodeValidateIntent, err)
	}
	if err := graph.AddLambdaNode(nodeLoadContext, compose.InvokableLambda(builder.loadContext)); err != nil {
		return nil, fmt.Errorf("add %s node: %w", nodeLoadContext, err)
	}
	template := prompt.FromMessages(
		schema.FString,
		schema.SystemMessage("你是 CreationSpec 开发预览规划模型。只输出一个严格 JSON 对象，不输出 Markdown、reasoning、价格、权限或资源标识。"),
		schema.UserMessage("prompt_version={prompt_version}\nproject_title={project_title}\nintent_json={intent_json}\n输出 schema_version=creation_spec.preview.proposal.v1，并保留目标、交付类型、受众和全部硬约束。"),
	)
	if err := graph.AddChatTemplateNode(nodeRenderPrompt, template); err != nil {
		return nil, fmt.Errorf("add %s node: %w", nodeRenderPrompt, err)
	}
	if err := graph.AddChatModelNode(nodeCallModel, chatModel); err != nil {
		return nil, fmt.Errorf("add %s node: %w", nodeCallModel, err)
	}
	if err := graph.AddLambdaNode(nodeValidateProposal, compose.InvokableLambda(builder.validateProposal)); err != nil {
		return nil, fmt.Errorf("add %s node: %w", nodeValidateProposal, err)
	}
	if err := graph.AddLambdaNode(nodeSaveDraft, compose.InvokableLambda(builder.saveDraft)); err != nil {
		return nil, fmt.Errorf("add %s node: %w", nodeSaveDraft, err)
	}
	if err := graph.AddLambdaNode(nodeQuerySaveReceipt, compose.InvokableLambda(builder.querySaveReceipt)); err != nil {
		return nil, fmt.Errorf("add %s node: %w", nodeQuerySaveReceipt, err)
	}
	if err := graph.AddLambdaNode(nodeBuildResult, compose.InvokableLambda(builder.buildResult)); err != nil {
		return nil, fmt.Errorf("add %s node: %w", nodeBuildResult, err)
	}
	if err := graph.AddLambdaNode(nodeDeferRecovery, compose.InvokableLambda(builder.deferRecovery)); err != nil {
		return nil, fmt.Errorf("add %s node: %w", nodeDeferRecovery, err)
	}

	for _, edge := range [][2]string{
		{compose.START, nodeValidateIntent},
		{nodeValidateIntent, nodeLoadContext},
		{nodeLoadContext, nodeRenderPrompt},
		{nodeRenderPrompt, nodeCallModel},
		{nodeCallModel, nodeValidateProposal},
		{nodeValidateProposal, nodeSaveDraft},
		{nodeBuildResult, compose.END},
		{nodeDeferRecovery, compose.END},
	} {
		if err := graph.AddEdge(edge[0], edge[1]); err != nil {
			return nil, fmt.Errorf("add graph edge %s -> %s: %w", edge[0], edge[1], err)
		}
	}
	if err := graph.AddBranch(nodeSaveDraft, compose.NewGraphBranch(builder.routeSave, map[string]bool{
		nodeBuildResult: true, nodeQuerySaveReceipt: true,
	})); err != nil {
		return nil, fmt.Errorf("add save branch: %w", err)
	}
	if err := graph.AddBranch(nodeQuerySaveReceipt, compose.NewGraphBranch(builder.routeQuery, map[string]bool{
		nodeBuildResult: true, nodeDeferRecovery: true,
	})); err != nil {
		return nil, fmt.Errorf("add query branch: %w", err)
	}

	runnable, err := graph.Compile(ctx,
		compose.WithGraphName(GraphName),
		compose.WithNodeTriggerMode(compose.AllPredecessor),
	)
	if err != nil {
		return nil, fmt.Errorf("compile %s: %w", GraphName, err)
	}
	return &CompiledGraph{runnable: runnable, business: business, journal: journal, clock: clock}, nil
}

// validatedInput 是 validate_intent 到 load_context 的有类型输出。
type validatedInput struct {
	TrustedContext TrustedContext
	Intent         Intent
	IntentDigest   string
}

// validateIntent 严格解码 Tool 输入，并将模型不可控的可信上下文与 Intent 分开冻结到 Local State。
func (b *graphBuilder) validateIntent(ctx context.Context, input GraphInput) (validatedInput, error) {
	intent, err := DecodeIntent(input.IntentJSON)
	if err != nil {
		return validatedInput{}, err
	}
	if !validTrustedContext(input.TrustedContext) {
		return validatedInput{}, fmt.Errorf("validate preview trusted context: invalid identity or fence")
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
		return nil
	})
	return result, err
}

// loadContext 通过普通 Business RPC 校验 Owner/Project 版本，并只向 Prompt 边界输出固定变量 Map。
func (b *graphBuilder) loadContext(ctx context.Context, input validatedInput) (map[string]any, error) {
	domainContext, err := b.business.GetCreationSpecContext(
		ctx, input.TrustedContext.RequestID, input.TrustedContext.UserID, input.TrustedContext.ProjectID,
	)
	if err != nil {
		return nil, err
	}
	if domainContext.ProjectID != input.TrustedContext.ProjectID || domainContext.ProjectVersion < 1 ||
		!validText(domainContext.ProjectTitle, 1, 160, false) {
		return nil, fmt.Errorf("load creation spec context: invalid Business response")
	}
	intentJSON, err := jsonMarshalIntent(input.Intent)
	if err != nil {
		return nil, err
	}
	if err := compose.ProcessState[*State](ctx, func(_ context.Context, state *State) error {
		state.DomainContext = domainContext
		return nil
	}); err != nil {
		return nil, err
	}
	// map 仅存在于 Eino ChatTemplate 变量边界；Key exact-set 固定，不作为跨 Node 业务状态。
	return map[string]any{
		"prompt_version": input.TrustedContext.PromptVersion,
		"project_title":  domainContext.ProjectTitle,
		"intent_json":    string(intentJSON),
	}, nil
}

// validateProposal 在 ChatModel Node 之后独立严格解析候选，只有通过后才构造 Business Command。
func (b *graphBuilder) validateProposal(ctx context.Context, message *schema.Message) (DraftCommand, error) {
	if message == nil || message.Role != schema.Assistant || message.Content == "" || len(message.ToolCalls) != 0 {
		return DraftCommand{}, fmt.Errorf("validate creation spec proposal: model message is invalid")
	}
	var stateSnapshot State
	if err := compose.ProcessState[*State](ctx, func(_ context.Context, state *State) error {
		stateSnapshot = *state
		stateSnapshot.Intent.Constraints = cloneRequiredStrings(state.Intent.Constraints)
		return nil
	}); err != nil {
		return DraftCommand{}, err
	}
	content, proposal, err := DecodeAndValidateProposal([]byte(message.Content), stateSnapshot.Intent)
	if err != nil {
		return DraftCommand{}, err
	}
	command := DraftCommand{
		TrustedContext: stateSnapshot.TrustedContext,
		DomainContext:  stateSnapshot.DomainContext,
		Content:        content,
	}
	digest, err := SaveRequestDigest(command)
	if err != nil {
		return DraftCommand{}, err
	}
	command.RequestDigest = digest
	err = compose.ProcessState[*State](ctx, func(_ context.Context, state *State) error {
		state.ModelMessage = message
		state.Proposal = proposal
		state.ValidationReport = "valid"
		state.Draft = content
		return nil
	})
	return command, err
}

// saveDraft 调用唯一副作用 Business Command；Unknown Outcome 只路由到原命令查询，不在此重试或换键。
func (b *graphBuilder) saveDraft(ctx context.Context, command DraftCommand) (SaveOutcome, error) {
	if err := b.journal.PrepareCommand(ctx, command); err != nil {
		return SaveOutcome{}, err
	}
	disposition, resource, err := b.business.SaveCreationSpecDraft(ctx, command)
	if err != nil {
		if errors.Is(err, ErrBusinessNotFound) || errors.Is(err, ErrBusinessConflict) || errors.Is(err, ErrBusinessDisabled) {
			return SaveOutcome{}, err
		}
		// Prepare 已冻结且 Save 边界已进入；除权威确定拒绝外，所有错误都只能查询原命令。
		return SaveOutcome{Status: "unknown", Command: command}, nil
	}
	if disposition != SaveDispositionCreated && disposition != SaveDispositionReplayed {
		// Save 请求已返回，不可信响应不能被解释为未提交或确定失败。
		return SaveOutcome{Status: "unknown", Command: command}, nil
	}
	if err := ValidateResourceForCommand(resource, command); err != nil {
		return SaveOutcome{Status: "unknown", Command: command}, nil
	}
	resourceCopy := resource
	if err := compose.ProcessState[*State](ctx, func(_ context.Context, state *State) error {
		state.BusinessCommandReceipt = &resourceCopy
		return nil
	}); err != nil {
		return SaveOutcome{Status: "unknown", Command: command}, nil
	}
	return SaveOutcome{Status: "saved", Disposition: disposition, Resource: &resourceCopy, Command: command}, nil
}

// routeSave 只依据确定性 SaveOutcome 状态路由，未知值失败关闭。
func (b *graphBuilder) routeSave(_ context.Context, outcome SaveOutcome) (string, error) {
	switch outcome.Status {
	case "saved":
		return nodeBuildResult, nil
	case "unknown":
		return nodeQuerySaveReceipt, nil
	default:
		return "", fmt.Errorf("route save outcome: unknown status")
	}
}

// querySaveReceipt 先查询原命令；只有权威 not_found 才在 PostgreSQL 预算内重发完全相同的命令一次。
func (b *graphBuilder) querySaveReceipt(ctx context.Context, outcome SaveOutcome) (SaveOutcome, error) {
	if outcome.Status != "unknown" {
		return SaveOutcome{}, fmt.Errorf("query save receipt: invalid predecessor status")
	}
	recovery, err := recoveryForCommand(outcome.Command)
	if err != nil {
		return SaveOutcome{}, err
	}
	return resolveUnknownSave(ctx, b.business, b.journal, outcome.Command, recovery)
}

// resolveUnknownSave 每次调用最多执行一次有预算的同键重发；查询和重发都绝不切换 command_id 或摘要。
func resolveUnknownSave(
	ctx context.Context,
	business BusinessClient,
	journal ResultStore,
	command DraftCommand,
	recovery RecoveryDeferred,
) (SaveOutcome, error) {
	status, resource, queryErr := business.QueryCreationSpecDraftCommand(ctx, command)
	if errors.Is(queryErr, ErrBusinessConflict) || status == "conflict" {
		return SaveOutcome{}, ErrBusinessConflict
	}
	if queryErr == nil && status == "completed" && resource != nil && ValidateResourceForCommand(*resource, command) == nil {
		return SaveOutcome{Status: "saved", Disposition: SaveDispositionReplayed, Resource: resource, Command: command}, nil
	}
	if recovery.ResendExhausted {
		return unresolvedSaveOutcome(command, recovery), nil
	}
	if queryErr != nil || status != "not_found" {
		// 查询技术失败或未知响应不等于权威 not_found：即使重发预算已用尽也继续 Query-only，不能错误停机。
		return unresolvedSaveOutcome(command, recovery), nil
	}

	reservedRecovery, reserved, err := journal.ReserveCommandResend(ctx, command.TrustedContext, recovery)
	if err != nil {
		return SaveOutcome{}, err
	}
	if !reserved {
		return unresolvedSaveOutcome(command, reservedRecovery), nil
	}
	disposition, saved, saveErr := business.SaveCreationSpecDraft(ctx, command)
	if saveErr == nil && (disposition == SaveDispositionCreated || disposition == SaveDispositionReplayed) &&
		ValidateResourceForCommand(saved, command) == nil {
		resourceCopy := saved
		return SaveOutcome{
			Status: "saved", Disposition: disposition, Resource: &resourceCopy,
			Command: command, Recovery: &reservedRecovery,
		}, nil
	}
	if errors.Is(saveErr, ErrBusinessNotFound) || errors.Is(saveErr, ErrBusinessConflict) || errors.Is(saveErr, ErrBusinessDisabled) {
		return SaveOutcome{}, saveErr
	}

	// 重发边界可能已经提交；无论响应是否可信都再查询一次，仍未收敛时只保留同一 durable 命令。
	status, resource, queryErr = business.QueryCreationSpecDraftCommand(ctx, command)
	if errors.Is(queryErr, ErrBusinessConflict) || status == "conflict" {
		return SaveOutcome{}, ErrBusinessConflict
	}
	if queryErr == nil && status == "completed" && resource != nil && ValidateResourceForCommand(*resource, command) == nil {
		return SaveOutcome{Status: "saved", Disposition: SaveDispositionReplayed, Resource: resource, Command: command}, nil
	}
	if queryErr == nil && status == "not_found" && reservedRecovery.ResendLimit > 0 &&
		reservedRecovery.ResendAttempts >= reservedRecovery.ResendLimit {
		// 最后一次重发后的 Query 已执行；之后停在可观察 exhausted 阶段，不能形成自动循环或继续施压 Business。
		reservedRecovery.ResendExhausted = true
	}
	return unresolvedSaveOutcome(command, reservedRecovery), nil
}

// unresolvedSaveOutcome 保留完整恢复工件和预算快照，不构造伪终态。
func unresolvedSaveOutcome(command DraftCommand, recovery RecoveryDeferred) SaveOutcome {
	recovery.Command = command
	return SaveOutcome{Status: "unresolved", Command: command, Recovery: &recovery}
}

// routeQuery 只把权威 completed 导向成功，把仍未确定导向 recovery_pending。
func (b *graphBuilder) routeQuery(_ context.Context, outcome SaveOutcome) (string, error) {
	switch outcome.Status {
	case "saved":
		return nodeBuildResult, nil
	case "unresolved":
		return nodeDeferRecovery, nil
	default:
		return "", fmt.Errorf("route query outcome: unknown status")
	}
}

// buildResult 构造严格 completed Result 与完整 Card；这里只冻结安全投影，不直接写 EventLog。
func (b *graphBuilder) buildResult(ctx context.Context, outcome SaveOutcome) (Outcome, error) {
	if outcome.Status != "saved" || outcome.Resource == nil {
		return recoveryOutcome(outcome.Command)
	}
	now := b.clock.Now().UTC()
	if now.IsZero() {
		return recoveryOutcome(outcome.Command)
	}
	result, err := completedResult(outcome.Command.TrustedContext, *outcome.Resource, outcome.Command.RequestDigest, now)
	if err != nil {
		return recoveryOutcome(outcome.Command)
	}
	err = compose.ProcessState[*State](ctx, func(_ context.Context, state *State) error {
		state.Result = result
		return nil
	})
	if err != nil {
		return recoveryOutcome(outcome.Command)
	}
	return Outcome{Terminal: &result}, nil
}

// deferRecovery 只返回恢复标记；Unknown Outcome 不构造、不冻结 Tool Result，也不发布伪终态。
func (b *graphBuilder) deferRecovery(ctx context.Context, outcome SaveOutcome) (Outcome, error) {
	var recovery RecoveryDeferred
	if outcome.Recovery != nil {
		recovery = *outcome.Recovery
	} else {
		var err error
		recovery, err = recoveryForCommand(outcome.Command)
		if err != nil {
			return Outcome{}, err
		}
	}
	_ = compose.ProcessState[*State](ctx, func(_ context.Context, state *State) error {
		state.Error = "CREATION_SPEC_SAVE_OUTCOME_UNKNOWN"
		return nil
	})
	// Error 只是可观测本地 State；Prepare 后即使 State 写入失败，也必须保留原命令 Recovery。
	return Outcome{Recovery: &recovery}, nil
}

// Recover 从 AEAD 工件恢复完整命令；只查询并在持久化预算内同键重发，不得重跑 Context、模型或 Validator。
func (g *CompiledGraph) Recover(
	ctx context.Context,
	trusted TrustedContext,
	recovery RecoveryDeferred,
) (Outcome, error) {
	if g == nil || g.business == nil || g.journal == nil || g.clock == nil || !validTrustedContext(trusted) ||
		recovery.ToolCallID != trusted.ToolCallID || recovery.BusinessCommandID != trusted.BusinessCommandID ||
		!validLowerSHA256(recovery.RequestDigest) || !validLowerSHA256(recovery.ContentDigest) ||
		recovery.Command.TrustedContext != trusted || recovery.Command.RequestDigest != recovery.RequestDigest {
		return Outcome{}, fmt.Errorf("recover creation spec command: invalid durable receipt")
	}
	if digest, err := ContentDigest(recovery.Command.Content); err != nil || digest != recovery.ContentDigest {
		return Outcome{}, fmt.Errorf("recover creation spec command: content digest mismatch")
	}
	resolved, err := resolveUnknownSave(ctx, g.business, g.journal, recovery.Command, recovery)
	if err != nil {
		return Outcome{}, err
	}
	if resolved.Status != "saved" || resolved.Resource == nil {
		if resolved.Recovery == nil {
			return Outcome{}, fmt.Errorf("recover creation spec command: missing recovery state")
		}
		return Outcome{Recovery: resolved.Recovery}, nil
	}
	now := g.clock.Now().UTC()
	if now.IsZero() {
		return Outcome{Recovery: &recovery}, nil
	}
	result, err := completedResult(trusted, *resolved.Resource, recovery.RequestDigest, now)
	if err != nil {
		return Outcome{Recovery: &recovery}, nil
	}
	return Outcome{Terminal: &result}, nil
}

func recoveryForCommand(command DraftCommand) (RecoveryDeferred, error) {
	contentDigest, err := ContentDigest(command.Content)
	if err != nil || !validLowerSHA256(command.RequestDigest) {
		return RecoveryDeferred{}, fmt.Errorf("build creation spec recovery: invalid original command")
	}
	return RecoveryDeferred{
		ToolCallID: command.TrustedContext.ToolCallID, BusinessCommandID: command.TrustedContext.BusinessCommandID,
		RequestDigest: command.RequestDigest, ContentDigest: contentDigest, Command: command,
	}, nil
}

func recoveryOutcome(command DraftCommand) (Outcome, error) {
	recovery, err := recoveryForCommand(command)
	if err != nil {
		return Outcome{}, err
	}
	return Outcome{Recovery: &recovery}, nil
}

func completedResult(
	trusted TrustedContext,
	resource Resource,
	requestDigest string,
	now time.Time,
) (Result, error) {
	if ValidateResource(resource, trusted.ProjectID) != nil || !validLowerSHA256(requestDigest) || now.IsZero() {
		return Result{}, fmt.Errorf("build creation spec result: invalid resource or receipt")
	}
	card := Card{
		SchemaVersion: CardSchemaVersion, CreationSpecID: resource.ID, ProjectID: resource.ProjectID,
		Version: resource.Version, Status: resource.Status, ContentDigest: resource.ContentDigest,
		Title: resource.Content.Title, Goal: resource.Content.Goal, DeliverableType: resource.Content.DeliverableType,
		Audience: resource.Content.Audience, Locale: resource.Content.Locale, Phases: clonePhases(resource.Content.Phases),
		Constraints:        cloneRequiredStrings(resource.Content.Constraints),
		AcceptanceCriteria: cloneRequiredStrings(resource.Content.AcceptanceCriteria), UpdatedAt: now.UTC(),
	}
	result := Result{
		Status: "completed", ResultCode: ResultCodeCreated,
		ResourceRef: &ResourceRef{ID: resource.ID, Version: resource.Version, Digest: resource.ContentDigest, Status: resource.Status},
		ReceiptRef:  ReceiptRef{ToolCallID: trusted.ToolCallID, BusinessCommandID: trusted.BusinessCommandID},
		Card:        &card, BusinessRequestDigest: requestDigest,
	}
	if err := ValidateTerminalResult(result, trusted); err != nil {
		return Result{}, err
	}
	return result, nil
}

// validTrustedContext 关闭所有服务端身份、稳定 ID 与 Fence 边界。
func validTrustedContext(value TrustedContext) bool {
	return value.Owner != "" && canonicalUUIDv7(value.RequestID) && canonicalUUIDv7(value.UserID) && canonicalUUIDv7(value.ProjectID) &&
		canonicalUUIDv7(value.SessionID) && canonicalUUIDv7(value.InputID) && canonicalUUIDv7(value.TurnID) &&
		canonicalUUIDv7(value.RunID) && canonicalUUIDv7(value.ToolCallID) && canonicalUUIDv7(value.BusinessCommandID) &&
		value.FenceToken > 0 && value.PromptVersion == PromptVersion && value.ValidatorVersion == ValidatorVersion
}
