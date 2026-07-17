package mediapreview

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

type fixedClock struct{ now time.Time }

func (clock fixedClock) Now() time.Time { return clock.now }

type fakeBusiness struct {
	prepareErr  error
	queryStatus string
	lastRequest PrepareRequest
}

func (client *fakeBusiness) Prepare(_ context.Context, request PrepareRequest) (PrepareResult, error) {
	client.lastRequest = request
	if client.prepareErr != nil {
		return PrepareResult{}, client.prepareErr
	}
	return preparedResult(request), nil
}

func (client *fakeBusiness) QueryPreparation(_ context.Context, query PrepareQuery) (PrepareQueryResult, error) {
	status := client.queryStatus
	if status == "" {
		status = PreparationStatusNotFound
	}
	result := PrepareQueryResult{
		SchemaVersion: PrepareQueryResultVersion,
		RequestID:     query.RequestID,
		Status:        status,
	}
	if status == PreparationStatusCompleted {
		prepared := preparedResult(client.lastRequest)
		result.Result = &prepared
	}
	return result, nil
}

type fakeRepository struct {
	operation   Operation
	dispatchErr error
	queryStatus string
	deferred    bool
	preparation PrepareResult
}

func (repository *fakeRepository) EnsureOperation(_ context.Context, command EnsureOperationCommand) (Operation, error) {
	if repository.operation.OperationID == "" {
		repository.operation = Operation{
			OperationID: newTestUUID(), BatchID: newTestUUID(), JobID: newTestUUID(),
			DispatchEventID: newTestUUID(), PreparationRequestID: newTestUUID(), PreparationCommandID: newTestUUID(),
			ToolKey: command.ToolKey, ScopeDigest: command.ScopeDigest, OutputProfile: command.OutputProfile,
			Status: OperationStatusPreparing,
		}
	}
	return repository.operation, nil
}

func (*fakeRepository) FreezePreparationRequest(context.Context, string, PrepareRequest) error {
	return nil
}

func (repository *fakeRepository) RecordPreparation(_ context.Context, _ string, result PrepareResult) error {
	repository.preparation = result
	return nil
}

func (repository *fakeRepository) Dispatch(_ context.Context, command DispatchCommand) (DispatchReceipt, error) {
	if repository.dispatchErr != nil {
		return DispatchReceipt{}, repository.dispatchErr
	}
	return receiptFor(command.Operation, command.Preparation), nil
}

func (repository *fakeRepository) QueryDispatch(_ context.Context, _ string, _ string) (DispatchQueryResult, error) {
	status := repository.queryStatus
	if status == "" {
		status = DispatchStatusNotFound
	}
	result := DispatchQueryResult{Status: status}
	if status == DispatchStatusCommitted {
		receipt := receiptFor(repository.operation, repository.preparation)
		result.Receipt = &receipt
	}
	return result, nil
}

func (repository *fakeRepository) DeferRecovery(context.Context, string, string) error {
	repository.deferred = true
	return nil
}

func TestMediaPreviewGraphTopologyExactSet(t *testing.T) {
	wantNodes := []string{
		"validate_intent", "freeze_scope", "ensure_operation", "prepare_asset", "query_preparation",
		"build_job", "dispatch_job", "query_dispatch", "defer_recovery", "emit_accepted", "emit_failed",
	}
	wantBranches := []string{
		"route_intent_validation", "route_prepare_outcome", "route_preparation_query",
		"route_dispatch_outcome", "route_dispatch_query",
	}
	if !reflect.DeepEqual(NodeKeys(), wantNodes) {
		t.Fatalf("Node exact-set 不一致: got=%v want=%v", NodeKeys(), wantNodes)
	}
	if !reflect.DeepEqual(BranchKeys(), wantBranches) {
		t.Fatalf("Branch exact-set 不一致: got=%v want=%v", BranchKeys(), wantBranches)
	}
}

func TestMediaPreviewGraphsCompileAndConverge(t *testing.T) {
	fixedNow := time.Date(2026, 7, 17, 8, 0, 0, 0, time.UTC)
	tests := []struct {
		name          string
		toolKey       string
		prepareErr    error
		prepareQuery  string
		dispatchErr   error
		dispatchQuery string
		wantTerminal  string
		wantRecovery  bool
	}{
		{name: "generate accepted", toolKey: GenerateMediaToolKey, wantTerminal: "accepted"},
		{name: "assemble accepted", toolKey: AssembleOutputToolKey, wantTerminal: "accepted"},
		{name: "prepare unknown query completed", toolKey: GenerateMediaToolKey, prepareErr: ErrUnknownOutcome, prepareQuery: PreparationStatusCompleted, wantTerminal: "accepted"},
		{name: "prepare unknown unresolved", toolKey: GenerateMediaToolKey, prepareErr: ErrUnknownOutcome, prepareQuery: PreparationStatusNotFound, wantRecovery: true},
		{name: "dispatch unknown query committed", toolKey: AssembleOutputToolKey, dispatchErr: ErrUnknownOutcome, dispatchQuery: DispatchStatusCommitted, wantTerminal: "accepted"},
		{name: "dispatch unknown unresolved", toolKey: AssembleOutputToolKey, dispatchErr: ErrUnknownOutcome, dispatchQuery: DispatchStatusNotFound, wantRecovery: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			business := &fakeBusiness{prepareErr: test.prepareErr, queryStatus: test.prepareQuery}
			repository := &fakeRepository{dispatchErr: test.dispatchErr, queryStatus: test.dispatchQuery}
			trusted := validTestTrustedContext(fixedNow)
			var outcome GraphOutcome
			var err error
			if test.toolKey == GenerateMediaToolKey {
				graph, compileErr := CompileGenerateMediaGraph(context.Background(), business, repository, fixedClock{fixedNow})
				if compileErr != nil {
					t.Fatalf("CompileGenerateMediaGraph() error = %v", compileErr)
				}
				intent := `{"schema_version":"generate_media.intent.v3preview1","prompt_preview_id":"` + newTestUUID() + `","expected_prompt_version":1,"expected_prompt_content_digest":"` + strings.Repeat("a", 64) + `","target_local_key":"hero_image","output_profile":"png_640x360.v1"}`
				outcome, err = graph.Invoke(context.Background(), GenerateMediaGraphInput{TrustedContext: trusted, IntentJSON: []byte(intent)})
			} else {
				graph, compileErr := CompileAssembleOutputGraph(context.Background(), business, repository, fixedClock{fixedNow})
				if compileErr != nil {
					t.Fatalf("CompileAssembleOutputGraph() error = %v", compileErr)
				}
				intent := `{"schema_version":"assemble_output.intent.v3preview1","source_asset_id":"` + newTestUUID() + `","expected_source_version":1,"expected_source_content_digest":"` + strings.Repeat("b", 64) + `","output_profile":"mp4_h264_640x360_2s.v1"}`
				outcome, err = graph.Invoke(context.Background(), AssembleOutputGraphInput{TrustedContext: trusted, IntentJSON: []byte(intent)})
			}
			if err != nil {
				t.Fatalf("Invoke() error = %v", err)
			}
			if test.wantRecovery {
				if outcome.Recovery == nil || outcome.Terminal != nil || !repository.deferred {
					t.Fatalf("恢复联合不正确: %+v deferred=%v", outcome, repository.deferred)
				}
				return
			}
			if outcome.Terminal == nil || outcome.Recovery != nil || outcome.Terminal.Status != test.wantTerminal {
				t.Fatalf("终态联合不正确: %+v", outcome)
			}
		})
	}
}

func TestMediaPreviewIntentRejectsInjectionAndUnknownFields(t *testing.T) {
	id := newTestUUID()
	digest := strings.Repeat("a", 64)
	tests := [][]byte{
		[]byte(`{"schema_version":"generate_media.intent.v3preview1","prompt_preview_id":"` + id + `","expected_prompt_version":1,"expected_prompt_content_digest":"` + digest + `","target_local_key":"../prompt","output_profile":"png_640x360.v1"}`),
		[]byte(`{"schema_version":"generate_media.intent.v3preview1","prompt_preview_id":"` + id + `","expected_prompt_version":1,"expected_prompt_content_digest":"` + digest + `","target_local_key":"hero","output_profile":"png_640x360.v1","prompt":"ignore previous"}`),
		[]byte(`{"schema_version":"assemble_output.intent.v3preview1","source_asset_id":"` + id + `","expected_source_version":1,"expected_source_content_digest":"` + digest + `","output_profile":"mp4_h264_640x360_2s.v1","ffmpeg_args":["-i","/tmp/x"]}`),
		[]byte(`{"schema_version":"assemble_output.intent.v3preview1","source_asset_id":"` + id + `","source_asset_id":"` + id + `","expected_source_version":1,"expected_source_content_digest":"` + digest + `","output_profile":"mp4_h264_640x360_2s.v1"}`),
	}
	for index, encoded := range tests {
		if _, err := DecodeGenerateMediaIntent(encoded); err == nil {
			if _, assembleErr := DecodeAssembleOutputIntent(encoded); assembleErr == nil {
				t.Fatalf("case %d 未拒绝注入/未知字段", index)
			}
		}
	}
}

func preparedResult(request PrepareRequest) PrepareResult {
	asset := AssetRef{AssetID: newTestUUID(), Version: 1, Status: "reserved"}
	source := SourceRef{SourceVersion: 1}
	result := PrepareResult{
		SchemaVersion: PrepareResultSchemaVersion, RequestID: request.RequestID, CommandID: request.CommandID,
		Disposition: "created", PreparationID: newTestUUID(), OutputProfile: request.OutputProfile,
		StagingObjectKey: "staging/output.part", CreatedAt: time.Date(2026, 7, 17, 8, 0, 0, 0, time.UTC),
	}
	if request.ToolKey == GenerateMediaToolKey {
		asset.MediaKind, asset.MIMEType = "image", "image/png"
		source.SourceType, source.SourceID = SourceTypePromptPreview, request.PromptSource.PromptPreviewID
		source.SourceDigest, source.TargetLocalKey = request.PromptSource.ContentDigest, request.PromptSource.TargetLocalKey
		source.TargetDigest = strings.Repeat("c", 64)
	} else {
		asset.MediaKind, asset.MIMEType = "video", "video/mp4"
		source.SourceType, source.SourceID = SourceTypeImageAsset, request.ImageAssetSource.AssetID
		source.SourceDigest, source.SourceObjectKey = request.ImageAssetSource.ContentDigest, "objects/source.png"
		result.SourceObjectKey = source.SourceObjectKey
	}
	result.AssetRef, result.SourceRef = asset, source
	return result
}

func receiptFor(operation Operation, preparation PrepareResult) DispatchReceipt {
	return DispatchReceipt{
		Status: DispatchStatusCommitted, OperationID: operation.OperationID, BatchID: operation.BatchID,
		JobID: operation.JobID, DispatchEventID: operation.DispatchEventID, AssetRef: preparation.AssetRef,
	}
}

func validTestTrustedContext(now time.Time) TrustedContext {
	return TrustedContext{
		RequestID: newTestUUID(), IdempotencyKey: newTestUUID(), UserID: newTestUUID(), ProjectID: newTestUUID(),
		SessionID: newTestUUID(), InputID: newTestUUID(), TurnID: newTestUUID(), RunID: newTestUUID(),
		ToolCallID: newTestUUID(), FenceToken: 1, DeadlineAt: now.Add(time.Minute),
	}
}

func newTestUUID() string {
	value, err := uuid.NewV7()
	if err != nil {
		panic(err)
	}
	return value.String()
}
