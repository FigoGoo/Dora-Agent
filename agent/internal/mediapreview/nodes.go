package mediapreview

import (
	"context"
	"errors"
	"fmt"

	"github.com/cloudwego/eino/compose"
)

const (
	flowValid      = "valid"
	flowFailed     = "failed"
	flowReserved   = "reserved"
	flowUnknown    = "unknown"
	flowUnresolved = "unresolved"
	flowCommitted  = "committed"
)

// mediaGraphDefinition 冻结两个 Graph 之间唯一允许变化的 Tool/Intent/Job 映射。
type mediaGraphDefinition[Intent any] struct {
	graphName         string
	toolKey           string
	definitionVersion string
	outputProfile     string
	jobType           string
	decodeIntent      func([]byte) (Intent, error)
	scopeDigest       func(TrustedContext, Intent) (string, error)
	prepareRequest    func(TrustedContext, Intent, Operation, string) (PrepareRequest, error)
}

// mediaGraphBuilder 只持有启动期不可变依赖；请求级状态全部位于 Eino Local State。
type mediaGraphBuilder[Intent any] struct {
	definition mediaGraphDefinition[Intent]
	business   BusinessClient
	repository Repository
	clock      Clock
}

// generateMediaDefinition 返回 generate_media 的严格 Prompt Ref、PNG 与 job_type pin。
func generateMediaDefinition() mediaGraphDefinition[GenerateMediaIntent] {
	return mediaGraphDefinition[GenerateMediaIntent]{
		graphName: GenerateMediaGraphName, toolKey: GenerateMediaToolKey,
		definitionVersion: GenerateMediaDefinitionVersion, outputProfile: GenerateOutputProfile,
		jobType: JobTypeGeneratePNG, decodeIntent: DecodeGenerateMediaIntent,
		scopeDigest:    generateMediaScopeDigest,
		prepareRequest: generateMediaPrepareRequest,
	}
}

// assembleOutputDefinition 返回 assemble_output 的严格 ready PNG、MP4 与 job_type pin。
func assembleOutputDefinition() mediaGraphDefinition[AssembleOutputIntent] {
	return mediaGraphDefinition[AssembleOutputIntent]{
		graphName: AssembleOutputGraphName, toolKey: AssembleOutputToolKey,
		definitionVersion: AssembleOutputDefinitionVersion, outputProfile: AssembleOutputProfile,
		jobType: JobTypeAssembleMP4, decodeIntent: DecodeAssembleOutputIntent,
		scopeDigest:    assembleOutputScopeDigest,
		prepareRequest: assembleOutputPrepareRequest,
	}
}

// validateIntent 严格解码 Intent，并在首节点冻结不可覆盖的 Runtime 身份。
func (b *mediaGraphBuilder[Intent]) validateIntent(ctx context.Context, input MediaGraphInput) (graphFlow[Intent], error) {
	flow := graphFlow[Intent]{TrustedContext: input.TrustedContext, Status: flowFailed, ErrorCode: ResultCodeInvalidArgument}
	intent, err := b.definition.decodeIntent(input.IntentJSON)
	if err == nil && ValidateTrustedContext(input.TrustedContext) == nil {
		flow.Intent = intent
		flow.Status = flowValid
		flow.ErrorCode = ""
	}
	if err := compose.ProcessState[*mediaPreviewState[Intent]](ctx, func(_ context.Context, state *mediaPreviewState[Intent]) error {
		state.TrustedContext = input.TrustedContext
		state.Intent = flow.Intent
		state.ErrorCode = flow.ErrorCode
		return nil
	}); err != nil {
		return graphFlow[Intent]{}, err
	}
	return flow, nil
}

// routeIntent 只读取确定性校验状态，未知值失败关闭。
func (b *mediaGraphBuilder[Intent]) routeIntent(_ context.Context, flow graphFlow[Intent]) (string, error) {
	switch flow.Status {
	case flowValid:
		return nodeFreezeScope, nil
	case flowFailed:
		return nodeEmitFailed, nil
	default:
		return "", fmt.Errorf("route %s intent: unknown status", b.definition.toolKey)
	}
}

// freezeScope 对 Tool Pin、可信 Owner 范围和资源 exact ref 做 canonical SHA-256，不读取内容正文。
func (b *mediaGraphBuilder[Intent]) freezeScope(ctx context.Context, flow graphFlow[Intent]) (graphFlow[Intent], error) {
	digest, err := b.definition.scopeDigest(flow.TrustedContext, flow.Intent)
	if err != nil || !ValidDigest(digest) {
		return graphFlow[Intent]{}, fmt.Errorf("freeze %s scope: %w", b.definition.toolKey, ErrInvalidArgument)
	}
	flow.ScopeDigest = digest
	if err := compose.ProcessState[*mediaPreviewState[Intent]](ctx, func(_ context.Context, state *mediaPreviewState[Intent]) error {
		state.ScopeDigest = digest
		return nil
	}); err != nil {
		return graphFlow[Intent]{}, err
	}
	return flow, nil
}

// ensureOperation first-write-wins 创建或恢复稳定 Operation/Batch/Job/Outbox 身份。
func (b *mediaGraphBuilder[Intent]) ensureOperation(ctx context.Context, flow graphFlow[Intent]) (graphFlow[Intent], error) {
	operation, err := b.repository.EnsureOperation(ctx, EnsureOperationCommand{
		TrustedContext: flow.TrustedContext,
		ToolKey:        b.definition.toolKey,
		ScopeDigest:    flow.ScopeDigest,
		OutputProfile:  b.definition.outputProfile,
	})
	if err != nil {
		flow.Status = flowFailed
		flow.ErrorCode = stableMediaErrorCode(err)
		return flow, nil
	}
	flow.Operation = operation
	if err := compose.ProcessState[*mediaPreviewState[Intent]](ctx, func(_ context.Context, state *mediaPreviewState[Intent]) error {
		state.Operation = operation
		return nil
	}); err != nil {
		return graphFlow[Intent]{}, err
	}
	return flow, nil
}

// prepareAsset 先冻结原 Prepare 命令再调用 Business；任何不确定响应都只能进入 Query。
func (b *mediaGraphBuilder[Intent]) prepareAsset(ctx context.Context, flow graphFlow[Intent]) (graphFlow[Intent], error) {
	if flow.Status == flowFailed {
		return flow, nil
	}
	request, err := b.definition.prepareRequest(flow.TrustedContext, flow.Intent, flow.Operation, flow.ScopeDigest)
	if err != nil {
		flow.Status, flow.ErrorCode = flowFailed, ResultCodeInvalidArgument
		return flow, nil
	}
	flow.PreparationRequest = request
	if err := b.repository.FreezePreparationRequest(ctx, flow.Operation.OperationID, request); err != nil {
		flow.Status, flow.ErrorCode = flowFailed, stableMediaErrorCode(err)
		return flow, nil
	}
	result, err := b.business.Prepare(ctx, request)
	if err != nil {
		if ctx.Err() != nil {
			return graphFlow[Intent]{}, ctx.Err()
		}
		if errors.Is(err, ErrBusinessConflict) || errors.Is(err, ErrBusinessPermanent) {
			flow.Status, flow.ErrorCode = flowFailed, stableMediaErrorCode(err)
		} else {
			flow.Status = flowUnknown
		}
		return flow, nil
	}
	// Prepare 已越过副作用边界；响应不可信不能解释为确定未提交，必须按原 command 查询。
	if ValidatePrepareResult(result, request) != nil {
		flow.Status = flowUnknown
		return flow, nil
	}
	if err := b.repository.RecordPreparation(ctx, flow.Operation.OperationID, result); err != nil {
		if errors.Is(err, ErrIdempotencyConflict) || errors.Is(err, ErrVersionConflict) {
			flow.Status, flow.ErrorCode = flowFailed, stableMediaErrorCode(err)
		} else {
			flow.Status = flowUnknown
		}
		return flow, nil
	}
	flow.Preparation = result
	flow.Status = flowReserved
	if err := b.capturePreparation(ctx, flow); err != nil {
		return graphFlow[Intent]{}, err
	}
	return flow, nil
}

// routePrepare 将 unknown 精确送入 Query，禁止直接重发 Prepare 或创建第二个 command。
func (b *mediaGraphBuilder[Intent]) routePrepare(_ context.Context, flow graphFlow[Intent]) (string, error) {
	switch flow.Status {
	case flowReserved:
		return nodeBuildJob, nil
	case flowUnknown:
		return nodeQueryPreparation, nil
	case flowFailed:
		return nodeEmitFailed, nil
	default:
		return "", fmt.Errorf("route %s prepare: unknown status", b.definition.toolKey)
	}
}

// queryPreparation 只按原 command/request digest 查询 Business 权威事实；not_found 仍保持恢复待定。
func (b *mediaGraphBuilder[Intent]) queryPreparation(ctx context.Context, flow graphFlow[Intent]) (graphFlow[Intent], error) {
	request := flow.PreparationRequest
	query := PrepareQuery{
		SchemaVersion: PrepareQuerySchemaVersion,
		RequestID:     request.RequestID,
		CommandID:     request.CommandID,
		RequestDigest: request.RequestDigest,
		UserID:        request.UserID,
		ProjectID:     request.ProjectID,
	}
	result, err := b.business.QueryPreparation(ctx, query)
	if err != nil {
		if ctx.Err() != nil {
			return graphFlow[Intent]{}, ctx.Err()
		}
		flow.Status = flowUnresolved
		return flow, nil
	}
	if ValidatePrepareQueryResult(result, query) != nil {
		flow.Status = flowUnresolved
		return flow, nil
	}
	switch result.Status {
	case PreparationStatusCompleted:
		if result.Result == nil || ValidatePrepareResult(*result.Result, request) != nil {
			flow.Status = flowUnresolved
			return flow, nil
		}
		if err := b.repository.RecordPreparation(ctx, flow.Operation.OperationID, *result.Result); err != nil {
			flow.Status, flow.ErrorCode = flowFailed, stableMediaErrorCode(err)
			return flow, nil
		}
		flow.Preparation = *result.Result
		flow.Status = flowReserved
		if err := b.capturePreparation(ctx, flow); err != nil {
			return graphFlow[Intent]{}, err
		}
	case PreparationStatusConflict:
		flow.Status, flow.ErrorCode = flowFailed, ResultCodeIdempotencyConflict
	case PreparationStatusNotFound:
		flow.Status = flowUnresolved
	default:
		return graphFlow[Intent]{}, fmt.Errorf("query %s preparation: unreachable status", b.definition.toolKey)
	}
	return flow, nil
}

// routePreparationQuery 将权威 completed、conflict 与 unresolved 三类结果分离。
func (b *mediaGraphBuilder[Intent]) routePreparationQuery(_ context.Context, flow graphFlow[Intent]) (string, error) {
	switch flow.Status {
	case flowReserved:
		return nodeBuildJob, nil
	case flowFailed:
		return nodeEmitFailed, nil
	case flowUnresolved:
		return nodeDeferRecovery, nil
	default:
		return "", fmt.Errorf("route %s preparation query: unknown status", b.definition.toolKey)
	}
}

// buildJob 从 Business 权威 source/target 构造唯一单 Job Envelope；不携带 Prompt 或动态媒体参数。
func (b *mediaGraphBuilder[Intent]) buildJob(ctx context.Context, flow graphFlow[Intent]) (graphFlow[Intent], error) {
	now := b.clock.Now().UTC()
	if now.IsZero() || !flow.TrustedContext.DeadlineAt.After(now) {
		flow.Status, flow.ErrorCode = flowFailed, ResultCodeInvalidArgument
		return flow, nil
	}
	job := JobSpec{
		JobID:             flow.Operation.JobID,
		BatchID:           flow.Operation.BatchID,
		OperationID:       flow.Operation.OperationID,
		SessionID:         flow.TrustedContext.SessionID,
		UserID:            flow.TrustedContext.UserID,
		ProjectID:         flow.TrustedContext.ProjectID,
		JobType:           b.definition.jobType,
		DefinitionVersion: b.definition.definitionVersion,
		ScopeDigest:       flow.ScopeDigest,
		OutputProfile:     b.definition.outputProfile,
		SourceRef:         flow.Preparation.SourceRef,
		Target: Target{
			AssetID: flow.Preparation.AssetRef.AssetID, AssetVersion: flow.Preparation.AssetRef.Version,
			PreparationID: flow.Preparation.PreparationID, StagingObjectKey: flow.Preparation.StagingObjectKey,
		},
		CreatedAt: now, DeadlineAt: flow.TrustedContext.DeadlineAt.UTC(),
	}
	artifactDigest, err := DigestJSON(struct {
		SchemaVersion string    `json:"schema_version"`
		JobID         string    `json:"job_id"`
		JobType       string    `json:"job_type"`
		ScopeDigest   string    `json:"scope_digest"`
		OutputProfile string    `json:"output_profile"`
		SourceRef     SourceRef `json:"source_ref"`
		Target        Target    `json:"target"`
	}{ArtifactRequestSchemaVersion, job.JobID, job.JobType, job.ScopeDigest, job.OutputProfile, job.SourceRef, job.Target})
	if err != nil {
		return graphFlow[Intent]{}, fmt.Errorf("build %s artifact digest: %w", b.definition.toolKey, err)
	}
	job.ArtifactRequestDigest = artifactDigest
	if ValidateJobSpec(job) != nil {
		flow.Status, flow.ErrorCode = flowFailed, ResultCodeInvalidArgument
		return flow, nil
	}
	flow.Job = job
	if err := compose.ProcessState[*mediaPreviewState[Intent]](ctx, func(_ context.Context, state *mediaPreviewState[Intent]) error {
		state.JobSpec = job
		return nil
	}); err != nil {
		return graphFlow[Intent]{}, err
	}
	return flow, nil
}

// dispatchJob 原子写 Operation/Batch/Job/Outbox；只有事务提交或权威查询确认后才能 accepted。
func (b *mediaGraphBuilder[Intent]) dispatchJob(ctx context.Context, flow graphFlow[Intent]) (graphFlow[Intent], error) {
	if flow.Status == flowFailed {
		return flow, nil
	}
	dispatchDigest, err := DigestJSON(struct {
		SchemaVersion  string `json:"schema_version"`
		OperationID    string `json:"operation_id"`
		BatchID        string `json:"batch_id"`
		JobID          string `json:"job_id"`
		PreparationID  string `json:"preparation_id"`
		ScopeDigest    string `json:"scope_digest"`
		ArtifactDigest string `json:"artifact_request_digest"`
	}{
		DispatchCommandSchemaVersion, flow.Operation.OperationID, flow.Operation.BatchID,
		flow.Operation.JobID, flow.Preparation.PreparationID, flow.ScopeDigest, flow.Job.ArtifactRequestDigest,
	})
	if err != nil {
		return graphFlow[Intent]{}, fmt.Errorf("build %s dispatch digest: %w", b.definition.toolKey, err)
	}
	receipt, err := b.repository.Dispatch(ctx, DispatchCommand{
		Operation: flow.Operation, Preparation: flow.Preparation, Job: flow.Job, DispatchDigest: dispatchDigest,
	})
	if err != nil {
		if errors.Is(err, ErrIdempotencyConflict) || errors.Is(err, ErrVersionConflict) || errors.Is(err, ErrInvalidArgument) {
			flow.Status, flow.ErrorCode = flowFailed, stableMediaErrorCode(err)
		} else {
			flow.Status = flowUnknown
		}
		return flow, nil
	}
	if !validDispatchReceipt(receipt, flow) {
		flow.Status = flowUnknown
		return flow, nil
	}
	flow.DispatchReceipt = receipt
	flow.Status = flowCommitted
	if err := b.captureDispatch(ctx, flow); err != nil {
		return graphFlow[Intent]{}, err
	}
	return flow, nil
}

// routeDispatch 保证 unknown 只能查询原 Operation，绝不创建第二个 Job。
func (b *mediaGraphBuilder[Intent]) routeDispatch(_ context.Context, flow graphFlow[Intent]) (string, error) {
	switch flow.Status {
	case flowCommitted:
		return nodeEmitAccepted, nil
	case flowUnknown:
		return nodeQueryDispatch, nil
	case flowFailed:
		return nodeEmitFailed, nil
	default:
		return "", fmt.Errorf("route %s dispatch: unknown status", b.definition.toolKey)
	}
}

// queryDispatch 查询 Agent PostgreSQL 原 Operation/scope 的派发事实。
func (b *mediaGraphBuilder[Intent]) queryDispatch(ctx context.Context, flow graphFlow[Intent]) (graphFlow[Intent], error) {
	result, err := b.repository.QueryDispatch(ctx, flow.Operation.OperationID, flow.ScopeDigest)
	if err != nil {
		if ctx.Err() != nil {
			return graphFlow[Intent]{}, ctx.Err()
		}
		flow.Status = flowUnresolved
		return flow, nil
	}
	switch result.Status {
	case DispatchStatusCommitted:
		if result.Receipt == nil || !validDispatchReceipt(*result.Receipt, flow) {
			flow.Status = flowUnresolved
			return flow, nil
		}
		flow.DispatchReceipt = *result.Receipt
		flow.Status = flowCommitted
		if err := b.captureDispatch(ctx, flow); err != nil {
			return graphFlow[Intent]{}, err
		}
	case DispatchStatusConflict:
		flow.Status, flow.ErrorCode = flowFailed, ResultCodeIdempotencyConflict
	case DispatchStatusNotFound:
		flow.Status = flowUnresolved
	default:
		flow.Status = flowUnresolved
	}
	return flow, nil
}

// routeDispatchQuery 将未收敛状态交给 Scanner，避免错误冻结 failed Tool Result。
func (b *mediaGraphBuilder[Intent]) routeDispatchQuery(_ context.Context, flow graphFlow[Intent]) (string, error) {
	switch flow.Status {
	case flowCommitted:
		return nodeEmitAccepted, nil
	case flowFailed:
		return nodeEmitFailed, nil
	case flowUnresolved:
		return nodeDeferRecovery, nil
	default:
		return "", fmt.Errorf("route %s dispatch query: unknown status", b.definition.toolKey)
	}
}

// deferRecovery 把 Operation 保持为 recovery_pending；原 Input 继续阻塞 HOL，由 Scanner 按原键核对。
func (b *mediaGraphBuilder[Intent]) deferRecovery(ctx context.Context, flow graphFlow[Intent]) (GraphOutcome, error) {
	if !ValidUUIDv7(flow.Operation.OperationID) {
		return GraphOutcome{}, fmt.Errorf("defer %s recovery: operation is missing", b.definition.toolKey)
	}
	if err := b.repository.DeferRecovery(ctx, flow.Operation.OperationID, ResultCodeUnknownOutcome); err != nil {
		return GraphOutcome{}, fmt.Errorf("defer %s recovery: %w", b.definition.toolKey, err)
	}
	return GraphOutcome{Recovery: &RecoveryDeferred{
		OperationID: flow.Operation.OperationID,
		ReasonCode:  ResultCodeUnknownOutcome,
	}}, nil
}

// emitAccepted 冻结异步受理回执；Job ID 与 Object Key 不进入 Tool Result。
func (b *mediaGraphBuilder[Intent]) emitAccepted(ctx context.Context, flow graphFlow[Intent]) (GraphOutcome, error) {
	now := b.clock.Now().UTC()
	if now.IsZero() || !validDispatchReceipt(flow.DispatchReceipt, flow) {
		return GraphOutcome{}, fmt.Errorf("emit %s accepted: invalid receipt or clock", b.definition.toolKey)
	}
	result := GraphToolResult{
		SchemaVersion: ToolResultSchemaVersion,
		ToolKey:       b.definition.toolKey,
		Status:        "accepted",
		ResultCode:    ResultCodeAccepted,
		OperationID:   flow.DispatchReceipt.OperationID,
		BatchID:       flow.DispatchReceipt.BatchID,
		AssetID:       flow.DispatchReceipt.AssetRef.AssetID,
		ReceiptID:     flow.DispatchReceipt.DispatchEventID,
		UpdatedAt:     now,
	}
	if err := compose.ProcessState[*mediaPreviewState[Intent]](ctx, func(_ context.Context, state *mediaPreviewState[Intent]) error {
		state.Result = &result
		return nil
	}); err != nil {
		return GraphOutcome{}, err
	}
	return GraphOutcome{Terminal: &result}, nil
}

// emitFailed 只返回白名单错误码，不泄漏 SQL、内部地址、Object Key 或依赖原文。
func (b *mediaGraphBuilder[Intent]) emitFailed(ctx context.Context, flow graphFlow[Intent]) (GraphOutcome, error) {
	now := b.clock.Now().UTC()
	if now.IsZero() {
		return GraphOutcome{}, fmt.Errorf("emit %s failed: clock returned zero", b.definition.toolKey)
	}
	code := flow.ErrorCode
	if code == "" {
		code = ResultCodeInternal
	}
	result := GraphToolResult{
		SchemaVersion: ToolResultSchemaVersion,
		ToolKey:       b.definition.toolKey,
		Status:        "failed",
		ResultCode:    code,
		ErrorCode:     code,
		UpdatedAt:     now,
	}
	if err := compose.ProcessState[*mediaPreviewState[Intent]](ctx, func(_ context.Context, state *mediaPreviewState[Intent]) error {
		state.ErrorCode = code
		state.Result = &result
		return nil
	}); err != nil {
		return GraphOutcome{}, err
	}
	return GraphOutcome{Terminal: &result}, nil
}

// capturePreparation 在单写节点同步 Local State，便于 Trace/测试核对但不替代 PostgreSQL。
func (b *mediaGraphBuilder[Intent]) capturePreparation(ctx context.Context, flow graphFlow[Intent]) error {
	return compose.ProcessState[*mediaPreviewState[Intent]](ctx, func(_ context.Context, state *mediaPreviewState[Intent]) error {
		state.PreparationRequest = flow.PreparationRequest
		state.Preparation = flow.Preparation
		return nil
	})
}

// captureDispatch 冻结当前 Graph 调用看到的权威 Dispatch Receipt。
func (b *mediaGraphBuilder[Intent]) captureDispatch(ctx context.Context, flow graphFlow[Intent]) error {
	return compose.ProcessState[*mediaPreviewState[Intent]](ctx, func(_ context.Context, state *mediaPreviewState[Intent]) error {
		state.DispatchReceipt = flow.DispatchReceipt
		return nil
	})
}

// validDispatchReceipt 复核 Repository 回执只能指向当前预分配的一 Operation/Batch/Job/Asset。
func validDispatchReceipt[Intent any](receipt DispatchReceipt, flow graphFlow[Intent]) bool {
	return receipt.Status == DispatchStatusCommitted &&
		receipt.OperationID == flow.Operation.OperationID &&
		receipt.BatchID == flow.Operation.BatchID &&
		receipt.JobID == flow.Operation.JobID &&
		receipt.DispatchEventID == flow.Operation.DispatchEventID &&
		receipt.AssetRef.AssetID == flow.Preparation.AssetRef.AssetID &&
		receipt.AssetRef.Version == flow.Preparation.AssetRef.Version &&
		receipt.AssetRef.Status == flow.Preparation.AssetRef.Status &&
		receipt.AssetRef.MediaKind == flow.Preparation.AssetRef.MediaKind &&
		receipt.AssetRef.MIMEType == flow.Preparation.AssetRef.MIMEType
}

// generateMediaScopeDigest 冻结 Prompt Preview exact ref、目标与 PNG pin，不包含 Prompt 正文。
func generateMediaScopeDigest(trusted TrustedContext, intent GenerateMediaIntent) (string, error) {
	return DigestJSON(struct {
		SchemaVersion     string `json:"schema_version"`
		Profile           string `json:"profile"`
		ToolKey           string `json:"tool_key"`
		DefinitionVersion string `json:"definition_version"`
		UserID            string `json:"user_id"`
		ProjectID         string `json:"project_id"`
		SessionID         string `json:"session_id"`
		PromptPreviewID   string `json:"prompt_preview_id"`
		PromptVersion     int64  `json:"prompt_version"`
		PromptDigest      string `json:"prompt_digest"`
		TargetLocalKey    string `json:"target_local_key"`
		OutputProfile     string `json:"output_profile"`
	}{
		ScopeSchemaVersion, Profile, GenerateMediaToolKey, GenerateMediaDefinitionVersion,
		trusted.UserID, trusted.ProjectID, trusted.SessionID, intent.PromptPreviewID,
		intent.ExpectedPromptVersion, intent.ExpectedPromptContentDigest, intent.TargetLocalKey, intent.OutputProfile,
	})
}

// assembleOutputScopeDigest 冻结 ready PNG exact ref 与固定 MP4 pin，不接受 Timeline 或 ffmpeg 参数。
func assembleOutputScopeDigest(trusted TrustedContext, intent AssembleOutputIntent) (string, error) {
	return DigestJSON(struct {
		SchemaVersion     string `json:"schema_version"`
		Profile           string `json:"profile"`
		ToolKey           string `json:"tool_key"`
		DefinitionVersion string `json:"definition_version"`
		UserID            string `json:"user_id"`
		ProjectID         string `json:"project_id"`
		SessionID         string `json:"session_id"`
		SourceAssetID     string `json:"source_asset_id"`
		SourceVersion     int64  `json:"source_version"`
		SourceDigest      string `json:"source_digest"`
		OutputProfile     string `json:"output_profile"`
	}{
		ScopeSchemaVersion, Profile, AssembleOutputToolKey, AssembleOutputDefinitionVersion,
		trusted.UserID, trusted.ProjectID, trusted.SessionID, intent.SourceAssetID,
		intent.ExpectedSourceVersion, intent.ExpectedSourceContentDigest, intent.OutputProfile,
	})
}

// generateMediaPrepareRequest 构造 Prompt source 严格联合并计算不自引用的 request digest。
func generateMediaPrepareRequest(
	trusted TrustedContext,
	intent GenerateMediaIntent,
	operation Operation,
	scopeDigest string,
) (PrepareRequest, error) {
	request := PrepareRequest{
		SchemaVersion: PrepareRequestSchemaVersion, RequestID: operation.PreparationRequestID,
		CommandID: operation.PreparationCommandID, OperationID: operation.OperationID,
		UserID: trusted.UserID, ProjectID: trusted.ProjectID, ToolKey: GenerateMediaToolKey,
		ScopeDigest: scopeDigest, OutputProfile: GenerateOutputProfile,
		PromptSource: &PromptSource{
			PromptPreviewID: intent.PromptPreviewID, Version: intent.ExpectedPromptVersion,
			ContentDigest: intent.ExpectedPromptContentDigest, TargetLocalKey: intent.TargetLocalKey,
		},
	}
	return freezePrepareRequestDigest(request)
}

// assembleOutputPrepareRequest 构造 Image Asset source 严格联合并计算不自引用的 request digest。
func assembleOutputPrepareRequest(
	trusted TrustedContext,
	intent AssembleOutputIntent,
	operation Operation,
	scopeDigest string,
) (PrepareRequest, error) {
	request := PrepareRequest{
		SchemaVersion: PrepareRequestSchemaVersion, RequestID: operation.PreparationRequestID,
		CommandID: operation.PreparationCommandID, OperationID: operation.OperationID,
		UserID: trusted.UserID, ProjectID: trusted.ProjectID, ToolKey: AssembleOutputToolKey,
		ScopeDigest: scopeDigest, OutputProfile: AssembleOutputProfile,
		ImageAssetSource: &ImageAssetSource{
			AssetID: intent.SourceAssetID, Version: intent.ExpectedSourceVersion,
			ContentDigest: intent.ExpectedSourceContentDigest,
		},
	}
	return freezePrepareRequestDigest(request)
}

// freezePrepareRequestDigest 以 request_digest 为空的 exact DTO 计算摘要，避免递归自引用。
func freezePrepareRequestDigest(request PrepareRequest) (PrepareRequest, error) {
	request.RequestDigest = ""
	digest, err := DigestJSON(struct {
		SchemaVersion    string            `json:"schema_version"`
		RequestID        string            `json:"request_id"`
		CommandID        string            `json:"command_id"`
		OperationID      string            `json:"operation_id"`
		UserID           string            `json:"user_id"`
		ProjectID        string            `json:"project_id"`
		ToolKey          string            `json:"tool_key"`
		ScopeDigest      string            `json:"scope_digest"`
		OutputProfile    string            `json:"output_profile"`
		PromptSource     *PromptSource     `json:"prompt_source,omitempty"`
		ImageAssetSource *ImageAssetSource `json:"image_asset_source,omitempty"`
	}{
		request.SchemaVersion, request.RequestID, request.CommandID, request.OperationID,
		request.UserID, request.ProjectID, request.ToolKey, request.ScopeDigest, request.OutputProfile,
		request.PromptSource, request.ImageAssetSource,
	})
	if err != nil {
		return PrepareRequest{}, err
	}
	request.RequestDigest = digest
	if ValidatePrepareRequest(request) != nil {
		return PrepareRequest{}, ErrInvalidArgument
	}
	return request, nil
}

// stableMediaErrorCode 把内部错误折叠为冻结契约允许的安全白名单。
func stableMediaErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrInvalidArgument):
		return ResultCodeInvalidArgument
	case errors.Is(err, ErrIdempotencyConflict):
		return ResultCodeIdempotencyConflict
	case errors.Is(err, ErrVersionConflict), errors.Is(err, ErrBusinessConflict):
		return ResultCodeVersionConflict
	case errors.Is(err, ErrDependencyNotReady):
		return ResultCodeDependencyNotReady
	case errors.Is(err, ErrBusinessPermanent):
		return ResultCodeNotFound
	case errors.Is(err, ErrUnknownOutcome):
		return ResultCodeUnknownOutcome
	default:
		return ResultCodeInternal
	}
}
