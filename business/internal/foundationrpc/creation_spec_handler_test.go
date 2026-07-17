package foundationrpc

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/business/internal/config"
	"github.com/FigoGoo/Dora-Agent/business/internal/creationspec"
	"github.com/FigoGoo/Dora-Agent/business/kitex_gen/foundationv1"
)

const (
	rpcPreviewRequestID  = "019f68e8-0010-7000-8000-000000000010"
	rpcPreviewUserID     = "019f68e8-0011-7000-8000-000000000011"
	rpcPreviewProjectID  = "019f68e8-0012-7000-8000-000000000012"
	rpcPreviewCommandID  = "019f68e8-0013-7000-8000-000000000013"
	rpcPreviewToolCallID = "019f68e8-0014-7000-8000-000000000014"
	rpcPreviewDraftID    = "019f68e8-0015-7000-8000-000000000015"
)

type creationSpecRPCServiceStub struct {
	contextResult creationspec.ProjectContext
	contextErr    error
	saveResult    creationspec.SaveResult
	saveErr       error
	queryResult   creationspec.QueryResult
	queryErr      error
	contextCalls  int
	saveCalls     int
	queryCalls    int
	saveCommand   creationspec.SaveCommand
}

func (stub *creationSpecRPCServiceStub) GetContext(_ context.Context, _ creationspec.ContextQuery) (creationspec.ProjectContext, error) {
	stub.contextCalls++
	return stub.contextResult, stub.contextErr
}

func (stub *creationSpecRPCServiceStub) SaveDraft(_ context.Context, command creationspec.SaveCommand) (creationspec.SaveResult, error) {
	stub.saveCalls++
	stub.saveCommand = command
	return stub.saveResult, stub.saveErr
}

func (stub *creationSpecRPCServiceStub) QueryCommand(_ context.Context, _, _, _, _ string) (creationspec.QueryResult, error) {
	stub.queryCalls++
	return stub.queryResult, stub.queryErr
}

func newCreationSpecRPCHandler(t *testing.T, service CreationSpecService) *Handler {
	t.Helper()
	handler, err := NewHandlerWithCreationSpecPreview(config.ServiceConfig{
		Name: "dora-business-service", Version: "test", Environment: "test", InstanceID: "business-test-1",
	}, fixedClock{now: time.Now()}, slog.New(slog.NewTextHandler(io.Discard, nil)), service, true)
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	return handler
}

// TestCreationSpecPreviewRPCMapsThreeMethods 验证 Context、Save、Query 的显式 DTO 映射与安全资源响应。
func TestCreationSpecPreviewRPCMapsThreeMethods(t *testing.T) {
	draft := rpcCreationSpecDraft(t)
	service := &creationSpecRPCServiceStub{
		contextResult: creationspec.ProjectContext{ProjectID: rpcPreviewProjectID, Version: 7, Title: "安全项目标题"},
		saveResult:    creationspec.SaveResult{Disposition: creationspec.CommandDispositionReplayed, Draft: draft},
		queryResult:   creationspec.QueryResult{Status: creationspec.QueryStatusCompleted, Draft: &draft},
	}
	handler := newCreationSpecRPCHandler(t, service)
	contextResponse, err := handler.GetCreationSpecContextPreviewV1(context.Background(), &foundationv1.GetCreationSpecContextPreviewRequestV1{
		SchemaVersion: foundationv1.CREATION_SPEC_PREVIEW_SCHEMA_VERSION, RequestId: rpcPreviewRequestID,
		UserId: rpcPreviewUserID, ProjectId: rpcPreviewProjectID,
	})
	if err != nil || contextResponse.ProjectVersion != 7 || contextResponse.ProjectTitle != "安全项目标题" {
		t.Fatalf("GetCreationSpecContextPreviewV1() response=%+v error=%v", contextResponse, err)
	}
	digest, err := creationspec.SaveRequestDigest(
		rpcPreviewUserID, rpcPreviewProjectID, 7, rpcPreviewToolCallID,
		"prompt.preview.v1", "validator.preview.v1", draft.Content,
	)
	if err != nil {
		t.Fatalf("SaveRequestDigest() error = %v", err)
	}
	saveResponse, err := handler.SaveCreationSpecDraftPreviewV1(context.Background(), &foundationv1.SaveCreationSpecDraftPreviewRequestV1{
		SchemaVersion: foundationv1.CREATION_SPEC_PREVIEW_SCHEMA_VERSION, RequestId: rpcPreviewRequestID,
		CommandId: rpcPreviewCommandID, RequestDigest: digest.Hex(), UserId: rpcPreviewUserID,
		ProjectId: rpcPreviewProjectID, ExpectedProjectVersion: 7, ToolCallId: rpcPreviewToolCallID,
		PromptVersion: "prompt.preview.v1", ValidatorVersion: "validator.preview.v1", Content: rpcCreationSpecContent(),
	})
	if err != nil || saveResponse.Disposition != foundationv1.CreationSpecPreviewCommandDispositionV1_REPLAYED ||
		saveResponse.Resource == nil || saveResponse.Resource.CreationSpecId != rpcPreviewDraftID ||
		saveResponse.Resource.Content.DeliverableType != foundationv1.CreationSpecPreviewDeliverableTypeV1_VIDEO ||
		service.saveCommand.Content.Title != "夏日短片" {
		t.Fatalf("SaveCreationSpecDraftPreviewV1() response=%+v command=%+v error=%v", saveResponse, service.saveCommand, err)
	}
	queryResponse, err := handler.QueryCreationSpecDraftCommandPreviewV1(context.Background(), &foundationv1.QueryCreationSpecDraftCommandPreviewRequestV1{
		SchemaVersion: foundationv1.CREATION_SPEC_PREVIEW_SCHEMA_VERSION, RequestId: rpcPreviewRequestID,
		CommandId: rpcPreviewCommandID, RequestDigest: digest.Hex(), UserId: rpcPreviewUserID, ProjectId: rpcPreviewProjectID,
	})
	if err != nil || queryResponse.Status != foundationv1.CreationSpecPreviewQueryStatusV1_COMPLETED || queryResponse.Resource == nil {
		t.Fatalf("QueryCreationSpecDraftCommandPreviewV1() response=%+v error=%v", queryResponse, err)
	}
	service.queryResult = creationspec.QueryResult{Status: creationspec.QueryStatusNotFound}
	queryResponse, err = handler.QueryCreationSpecDraftCommandPreviewV1(context.Background(), &foundationv1.QueryCreationSpecDraftCommandPreviewRequestV1{
		SchemaVersion: foundationv1.CREATION_SPEC_PREVIEW_SCHEMA_VERSION, RequestId: rpcPreviewRequestID,
		CommandId: rpcPreviewCommandID, RequestDigest: digest.Hex(), UserId: rpcPreviewUserID, ProjectId: rpcPreviewProjectID,
	})
	if err != nil || queryResponse.Status != foundationv1.CreationSpecPreviewQueryStatusV1_NOT_FOUND || queryResponse.Resource != nil {
		t.Fatalf("not-found Query response=%+v error=%v", queryResponse, err)
	}
}

// TestCreationSpecPreviewRPCPreservesRequiredEmptyConstraintList 锁定 Handler 双向映射合法空约束，不得在写前误报 INVALID_ARGUMENT。
func TestCreationSpecPreviewRPCPreservesRequiredEmptyConstraintList(t *testing.T) {
	content := creationspec.Content{
		Title: "视频创作规格", Goal: "Dora 基本功能一键验收 1784250000000",
		DeliverableType: creationspec.DeliverableTypeVideo, Audience: "本地 MVP 验收用户", Locale: "zh-CN",
		Phases:      []creationspec.Phase{{Key: "phase_1", Title: "创作规划", Objective: "冻结目标、结构与交付边界", Output: "可执行创作规格"}},
		Constraints: []string{}, AcceptanceCriteria: []string{"交付结果符合已冻结目标、类型和全部硬约束"},
	}
	digest, err := creationspec.ContentDigest(content)
	if err != nil {
		t.Fatalf("计算空约束 Content 摘要失败: %v", err)
	}
	draft := rpcCreationSpecDraft(t)
	draft.Content = content
	draft.ContentDigest = digest
	service := &creationSpecRPCServiceStub{saveResult: creationspec.SaveResult{
		Disposition: creationspec.CommandDispositionCreated, Draft: draft,
	}}
	handler := newCreationSpecRPCHandler(t, service)
	requestDigest, err := creationspec.SaveRequestDigest(
		rpcPreviewUserID, rpcPreviewProjectID, 2, rpcPreviewToolCallID,
		"prompt.preview.v1", "validator.preview.v1", content,
	)
	if err != nil {
		t.Fatalf("计算空约束 Save 摘要失败: %v", err)
	}
	rpcContent := rpcCreationSpecContent()
	rpcContent.Title = content.Title
	rpcContent.Goal = content.Goal
	rpcContent.Audience = content.Audience
	rpcContent.Phases = []*foundationv1.CreationSpecPreviewPhaseV1{{
		Key: "phase_1", Title: "创作规划", Objective: "冻结目标、结构与交付边界", Output: "可执行创作规格",
	}}
	rpcContent.Constraints = []string{}
	rpcContent.AcceptanceCriteria = append([]string{}, content.AcceptanceCriteria...)
	response, err := handler.SaveCreationSpecDraftPreviewV1(context.Background(), &foundationv1.SaveCreationSpecDraftPreviewRequestV1{
		SchemaVersion: foundationv1.CREATION_SPEC_PREVIEW_SCHEMA_VERSION, RequestId: rpcPreviewRequestID,
		CommandId: rpcPreviewCommandID, RequestDigest: requestDigest.Hex(), UserId: rpcPreviewUserID,
		ProjectId: rpcPreviewProjectID, ExpectedProjectVersion: 2, ToolCallId: rpcPreviewToolCallID,
		PromptVersion: "prompt.preview.v1", ValidatorVersion: "validator.preview.v1", Content: rpcContent,
	})
	if err != nil || service.saveCalls != 1 {
		t.Fatalf("合法空约束未进入 Business Service: response=%+v calls=%d err=%v", response, service.saveCalls, err)
	}
	if service.saveCommand.Content.Constraints == nil || len(service.saveCommand.Content.Constraints) != 0 {
		t.Fatalf("协议→领域 required constraints 未保留 []: %#v", service.saveCommand.Content.Constraints)
	}
	if response == nil || response.Resource == nil || response.Resource.Content == nil ||
		response.Resource.Content.Constraints == nil || len(response.Resource.Content.Constraints) != 0 {
		t.Fatalf("领域→协议 required constraints 未保留 []: %+v", response)
	}
}

// TestCreationSpecPreviewRPCDisabledNeverCallsService 验证关闭态三组 RPC 在 DTO 校验前失败关闭，尤其不会触发 Save。
func TestCreationSpecPreviewRPCDisabledNeverCallsService(t *testing.T) {
	service := &creationSpecRPCServiceStub{}
	handler, err := NewHandlerWithCreationSpecPreview(config.ServiceConfig{
		Name: "dora-business-service", Version: "test", Environment: "test", InstanceID: "business-test-1",
	}, fixedClock{now: time.Now()}, slog.New(slog.NewTextHandler(io.Discard, nil)), service, false)
	if err != nil {
		t.Fatalf("NewHandlerWithCreationSpecPreview() error = %v", err)
	}
	calls := []func() error{
		func() error {
			_, callErr := handler.GetCreationSpecContextPreviewV1(context.Background(), nil)
			return callErr
		},
		func() error {
			_, callErr := handler.SaveCreationSpecDraftPreviewV1(context.Background(), nil)
			return callErr
		},
		func() error {
			_, callErr := handler.QueryCreationSpecDraftCommandPreviewV1(context.Background(), nil)
			return callErr
		},
	}
	for index, call := range calls {
		err := call()
		serviceErr, ok := err.(*foundationv1.FoundationServiceExceptionV1)
		if !ok || serviceErr.Code != featureDisabledCode || serviceErr.Retryable {
			t.Fatalf("disabled call %d error=%T %v", index, err, err)
		}
	}
	if service.contextCalls != 0 || service.saveCalls != 0 || service.queryCalls != 0 {
		t.Fatalf("disabled RPC reached service: %+v", service)
	}

	probeOnly, err := NewHandler(config.ServiceConfig{
		Name: "dora-business-service", Version: "test", Environment: "test", InstanceID: "business-test-1",
	}, fixedClock{now: time.Now()}, slog.New(slog.NewTextHandler(io.Discard, nil)), service)
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	_, err = probeOnly.SaveCreationSpecDraftPreviewV1(context.Background(), nil)
	serviceErr, ok := err.(*foundationv1.FoundationServiceExceptionV1)
	if !ok || serviceErr.Code != featureDisabledCode || serviceErr.Retryable || service.saveCalls != 0 {
		t.Fatalf("Probe-only constructor enabled Preview: error=%T %v save_calls=%d", err, err, service.saveCalls)
	}
}

// TestCreationSpecPreviewRPCRejectsInvalidContentAndMapsConflict 验证 nil Phase 失败关闭及幂等冲突稳定错误码。
func TestCreationSpecPreviewRPCRejectsInvalidContentAndMapsConflict(t *testing.T) {
	service := &creationSpecRPCServiceStub{saveErr: creationspec.ErrIdempotencyConflict}
	handler := newCreationSpecRPCHandler(t, service)
	request := &foundationv1.SaveCreationSpecDraftPreviewRequestV1{
		SchemaVersion: foundationv1.CREATION_SPEC_PREVIEW_SCHEMA_VERSION, RequestId: rpcPreviewRequestID,
		Content: rpcCreationSpecContent(),
	}
	request.Content.Phases[0] = nil
	if _, err := handler.SaveCreationSpecDraftPreviewV1(context.Background(), request); err == nil || service.saveCalls != 0 {
		t.Fatalf("nil Phase reached service: calls=%d error=%v", service.saveCalls, err)
	}
	request.Content = rpcCreationSpecContent()
	_, err := handler.SaveCreationSpecDraftPreviewV1(context.Background(), request)
	serviceErr, ok := err.(*foundationv1.FoundationServiceExceptionV1)
	if !ok || serviceErr.Code != idempotencyConflictCode || serviceErr.Retryable {
		t.Fatalf("conflict error=%T %v", err, err)
	}
}

func rpcCreationSpecContent() *foundationv1.CreationSpecPreviewContentV1 {
	return &foundationv1.CreationSpecPreviewContentV1{
		Title: "夏日短片", Goal: "制作一支 30 秒新品短片",
		DeliverableType: foundationv1.CreationSpecPreviewDeliverableTypeV1_VIDEO,
		Audience:        "年轻消费者", Locale: "zh-CN",
		Phases:      []*foundationv1.CreationSpecPreviewPhaseV1{{Key: "phase_1", Title: "规划", Objective: "确定叙事", Output: "创意方案"}},
		Constraints: []string{"竖屏 9:16"}, AcceptanceCriteria: []string{"成片时长为 30 秒"},
	}
}

func rpcCreationSpecDraft(t *testing.T) creationspec.Draft {
	t.Helper()
	content, err := creationSpecContentFromRPC(rpcCreationSpecContent())
	if err != nil {
		t.Fatalf("creationSpecContentFromRPC() error = %v", err)
	}
	digest, err := creationspec.ContentDigest(content)
	if err != nil {
		t.Fatalf("ContentDigest() error = %v", err)
	}
	now := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	return creationspec.Draft{
		ID: rpcPreviewDraftID, ProjectID: rpcPreviewProjectID, UserID: rpcPreviewUserID,
		Status: creationspec.DraftStatus, Version: creationspec.InitialDraftVersion,
		SchemaVersion: creationspec.DraftSchemaVersion, Content: content, ContentDigest: digest,
		SourceToolCallID: rpcPreviewToolCallID, SourcePromptVersion: "prompt.preview.v1",
		SourceValidatorVersion: "validator.preview.v1", CreatedAt: now, UpdatedAt: now,
	}
}
