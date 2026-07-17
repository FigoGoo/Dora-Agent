package foundationrpc

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/business/internal/promptpreview"
	"github.com/FigoGoo/Dora-Agent/business/internal/storyboardpreview"
	"github.com/FigoGoo/Dora-Agent/business/kitex_gen/foundationv1"
)

const (
	promptHandlerRequestID    = "019f68e8-0101-7000-8000-000000000101"
	promptHandlerUserID       = "019f68e8-0102-7000-8000-000000000102"
	promptHandlerProjectID    = "019f68e8-0103-7000-8000-000000000103"
	promptHandlerStoryboardID = "019f68e8-0104-7000-8000-000000000104"
	promptHandlerDraftID      = "019f68e8-0105-7000-8000-000000000105"
	promptHandlerCommandID    = "019f68e8-0106-7000-8000-000000000106"
	promptHandlerToolCallID   = "019f68e8-0107-7000-8000-000000000107"
)

// promptHandlerServiceStub 记录三类 RPC 到领域 Service 的显式映射。
type promptHandlerServiceStub struct {
	contextResult promptpreview.GenerationContext
	contextErr    error
	saveResult    promptpreview.SaveResult
	saveErr       error
	queryResult   promptpreview.QueryResult
	queryErr      error
	contextCalls  int
	saveCalls     int
	queryCalls    int
	contextQuery  promptpreview.ContextQuery
	saveCommand   promptpreview.SaveCommand
	queryValues   [4]string
}

// GetGenerationContext 返回测试冻结的联合上下文。
func (stub *promptHandlerServiceStub) GetGenerationContext(_ context.Context, query promptpreview.ContextQuery) (promptpreview.GenerationContext, error) {
	stub.contextCalls++
	stub.contextQuery = query
	return stub.contextResult, stub.contextErr
}

// SaveDraft 返回测试冻结的保存结果。
func (stub *promptHandlerServiceStub) SaveDraft(_ context.Context, command promptpreview.SaveCommand) (promptpreview.SaveResult, error) {
	stub.saveCalls++
	stub.saveCommand = command
	return stub.saveResult, stub.saveErr
}

// QueryCommand 返回测试冻结的命令查询结果。
func (stub *promptHandlerServiceStub) QueryCommand(_ context.Context, commandID string, requestDigest string, userID string, projectID string) (promptpreview.QueryResult, error) {
	stub.queryCalls++
	stub.queryValues = [4]string{commandID, requestDigest, userID, projectID}
	return stub.queryResult, stub.queryErr
}

// TestPromptPreviewRPCMapsContextSaveAndQuery 验证三类 RPC 的严格 DTO 映射、枚举和安全 Resource 输出。
func TestPromptPreviewRPCMapsContextSaveAndQuery(t *testing.T) {
	contextResult, draft := promptHandlerFixtures(t)
	stub := &promptHandlerServiceStub{
		contextResult: contextResult,
		saveResult: promptpreview.SaveResult{
			Disposition: promptpreview.CommandDispositionReplayed, Draft: draft,
		},
		queryResult: promptpreview.QueryResult{Status: promptpreview.QueryStatusCompleted, Draft: &draft},
	}
	handler := newPromptPreviewRPCHandler(stub, true)
	reference := promptHandlerReferenceRPC(draft.StoryboardPreviewRef)

	contextResponse, err := handler.GetPromptGenerationContextPreviewV1(context.Background(), &foundationv1.GetPromptGenerationContextPreviewRequestV1{
		SchemaVersion: foundationv1.PROMPT_PREVIEW_RPC_SCHEMA_VERSION, RequestId: promptHandlerRequestID,
		UserId: promptHandlerUserID, ProjectId: promptHandlerProjectID, StoryboardPreviewRef: reference,
	})
	if err != nil || contextResponse.StoryboardPreview == nil ||
		contextResponse.StoryboardPreview.StoryboardPreviewId != promptHandlerStoryboardID ||
		len(contextResponse.StoryboardPreview.Content.Elements) != 1 ||
		contextResponse.StoryboardPreview.Content.Slots[0].SlotType != foundationv1.StoryboardPreviewSlotTypeV1_IMAGE {
		t.Fatalf("GetPromptGenerationContextPreviewV1() response=%+v error=%v", contextResponse, err)
	}
	if stub.contextQuery.UserID != promptHandlerUserID || stub.contextQuery.StoryboardPreviewRef != draft.StoryboardPreviewRef {
		t.Fatalf("context query mismatch: %+v", stub.contextQuery)
	}

	requestDigest := strings.Repeat("3", 64)
	saveResponse, err := handler.SavePromptDraftPreviewV1(context.Background(), &foundationv1.SavePromptDraftPreviewRequestV1{
		SchemaVersion: foundationv1.PROMPT_PREVIEW_RPC_SCHEMA_VERSION, RequestId: promptHandlerRequestID,
		CommandId: promptHandlerCommandID, RequestDigest: requestDigest,
		UserId: promptHandlerUserID, ProjectId: promptHandlerProjectID, ExpectedProjectVersion: 7,
		StoryboardPreviewRef: reference, ToolCallId: promptHandlerToolCallID,
		PromptVersion: draft.SourcePromptVersion, ValidatorVersion: draft.SourceValidatorVersion,
		ExactSetValidatorVersion: draft.SourceExactSetValidatorVersion,
		ExactTargetSetDigest:     draft.ExactTargetSetDigest.Hex(), Content: promptHandlerContentRPC(reference),
	})
	if err != nil || saveResponse.Disposition != foundationv1.PromptPreviewCommandDispositionV1_REPLAYED ||
		saveResponse.Resource == nil || saveResponse.Resource.PromptPreviewId != promptHandlerDraftID ||
		saveResponse.Resource.ExactTargetSetDigest != draft.ExactTargetSetDigest.Hex() ||
		saveResponse.Resource.Content.Prompts[0].MediaKind != foundationv1.PromptPreviewMediaKindV1_IMAGE ||
		saveResponse.Resource.Content.Prompts[0].NegativeConstraints == nil {
		t.Fatalf("SavePromptDraftPreviewV1() response=%+v error=%v", saveResponse, err)
	}
	if stub.saveCommand.ExactSetValidatorVersion != draft.SourceExactSetValidatorVersion ||
		stub.saveCommand.ExactTargetSetDigestHex != draft.ExactTargetSetDigest.Hex() ||
		stub.saveCommand.Content.Prompts[0].NegativeConstraints == nil ||
		len(stub.saveCommand.Content.Prompts[0].NegativeConstraints) != 0 {
		t.Fatalf("save command mismatch: %+v", stub.saveCommand)
	}

	queryResponse, err := handler.QueryPromptDraftCommandPreviewV1(context.Background(), promptHandlerQueryRequest(requestDigest))
	if err != nil || queryResponse.Status != foundationv1.PromptPreviewQueryStatusV1_COMPLETED ||
		queryResponse.Resource == nil || queryResponse.Resource.PromptPreviewId != promptHandlerDraftID {
		t.Fatalf("QueryPromptDraftCommandPreviewV1() response=%+v error=%v", queryResponse, err)
	}
	if stub.queryValues != [4]string{promptHandlerCommandID, requestDigest, promptHandlerUserID, promptHandlerProjectID} {
		t.Fatalf("query values mismatch: %+v", stub.queryValues)
	}
	if stub.contextCalls != 1 || stub.saveCalls != 1 || stub.queryCalls != 1 {
		t.Fatalf("unexpected service calls: context=%d save=%d query=%d", stub.contextCalls, stub.saveCalls, stub.queryCalls)
	}
}

// TestPromptPreviewRPCDisabledFailsBeforeValidation 验证关闭态三类 RPC 在 DTO 校验前失败且绝不触发领域层。
func TestPromptPreviewRPCDisabledFailsBeforeValidation(t *testing.T) {
	stub := &promptHandlerServiceStub{}
	handler := newPromptPreviewRPCHandler(stub, false)
	calls := []func() error{
		func() error {
			_, err := handler.GetPromptGenerationContextPreviewV1(context.Background(), nil)
			return err
		},
		func() error {
			_, err := handler.SavePromptDraftPreviewV1(context.Background(), nil)
			return err
		},
		func() error {
			_, err := handler.QueryPromptDraftCommandPreviewV1(context.Background(), nil)
			return err
		},
	}
	for index, call := range calls {
		err := call()
		serviceError, ok := err.(*foundationv1.FoundationServiceExceptionV1)
		if !ok || serviceError.Code != featureDisabledCode || serviceError.Retryable {
			t.Fatalf("disabled call %d error=%T %v", index, err, err)
		}
	}
	if stub.contextCalls != 0 || stub.saveCalls != 0 || stub.queryCalls != 0 {
		t.Fatalf("disabled RPC reached service: %+v", stub)
	}

	probeOnly := &Handler{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	_, err := probeOnly.SavePromptDraftPreviewV1(context.Background(), nil)
	serviceError, ok := err.(*foundationv1.FoundationServiceExceptionV1)
	if !ok || serviceError.Code != featureDisabledCode || serviceError.Retryable {
		t.Fatalf("default Handler opened Prompt Preview: error=%T %v", err, err)
	}
}

// TestPromptPreviewRPCRejectsInvalidContentBeforeService 验证 nil 列表和未知枚举不会越过 RPC 边界。
func TestPromptPreviewRPCRejectsInvalidContentBeforeService(t *testing.T) {
	_, draft := promptHandlerFixtures(t)
	stub := &promptHandlerServiceStub{}
	handler := newPromptPreviewRPCHandler(stub, true)
	reference := promptHandlerReferenceRPC(draft.StoryboardPreviewRef)
	request := &foundationv1.SavePromptDraftPreviewRequestV1{
		SchemaVersion: foundationv1.PROMPT_PREVIEW_RPC_SCHEMA_VERSION, RequestId: promptHandlerRequestID,
		StoryboardPreviewRef: reference, Content: promptHandlerContentRPC(reference),
	}
	request.Content.Prompts[0].NegativeConstraints = nil
	if _, err := handler.SavePromptDraftPreviewV1(context.Background(), request); err == nil || stub.saveCalls != 0 {
		t.Fatalf("nil negative constraints reached service: calls=%d error=%v", stub.saveCalls, err)
	}
	request.Content = promptHandlerContentRPC(reference)
	request.Content.Prompts[0].MediaKind = 0
	if _, err := handler.SavePromptDraftPreviewV1(context.Background(), request); err == nil || stub.saveCalls != 0 {
		t.Fatalf("unknown media kind reached service: calls=%d error=%v", stub.saveCalls, err)
	}
}

// TestPromptPreviewQueryEnforcesUnionShape 验证查询状态与可选 Resource 形成严格联合类型。
func TestPromptPreviewQueryEnforcesUnionShape(t *testing.T) {
	_, draft := promptHandlerFixtures(t)
	stub := &promptHandlerServiceStub{}
	handler := newPromptPreviewRPCHandler(stub, true)
	request := promptHandlerQueryRequest(strings.Repeat("3", 64))

	for _, testCase := range []struct {
		name       string
		result     promptpreview.QueryResult
		wantStatus foundationv1.PromptPreviewQueryStatusV1
	}{
		{name: "not found", result: promptpreview.QueryResult{Status: promptpreview.QueryStatusNotFound}, wantStatus: foundationv1.PromptPreviewQueryStatusV1_NOT_FOUND},
		{name: "conflict", result: promptpreview.QueryResult{Status: promptpreview.QueryStatusConflict}, wantStatus: foundationv1.PromptPreviewQueryStatusV1_CONFLICT},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			stub.queryResult = testCase.result
			response, err := handler.QueryPromptDraftCommandPreviewV1(context.Background(), request)
			if err != nil || response.Status != testCase.wantStatus || response.Resource != nil {
				t.Fatalf("response=%+v error=%v", response, err)
			}
		})
	}

	invalidResults := []promptpreview.QueryResult{
		{Status: promptpreview.QueryStatusCompleted},
		{Status: promptpreview.QueryStatusNotFound, Draft: &draft},
		{Status: "unexpected"},
	}
	for index, result := range invalidResults {
		stub.queryResult = result
		_, err := handler.QueryPromptDraftCommandPreviewV1(context.Background(), request)
		assertPromptHandlerServiceError(t, err, persistenceCode, true, index)
	}
}

// TestPromptPreviewErrorMapping 验证领域错误不会泄漏内部实现且版本冲突口径保持冻结。
func TestPromptPreviewErrorMapping(t *testing.T) {
	for index, testCase := range []struct {
		err       error
		code      string
		retryable bool
	}{
		{err: promptpreview.ErrInvalidInput, code: invalidArgumentCode},
		{err: promptpreview.ErrNotFound, code: notFoundCode},
		{err: promptpreview.ErrProjectVersionConflict, code: versionConflictCode},
		{err: promptpreview.ErrStoryboardVersionConflict, code: promptStoryboardVersionConflictCode},
		{err: promptpreview.ErrIdempotencyConflict, code: idempotencyConflictCode},
		{err: context.Canceled, code: persistenceCode, retryable: true},
		{err: context.DeadlineExceeded, code: persistenceCode, retryable: true},
		{err: promptpreview.ErrPersistence, code: persistenceCode, retryable: true},
		{err: errors.New("unknown"), code: persistenceCode, retryable: true},
	} {
		assertPromptHandlerServiceError(t, mapPromptPreviewServiceError(testCase.err), testCase.code, testCase.retryable, index)
	}
}

// newPromptPreviewRPCHandler 创建无网络 Handler 并显式控制 Prompt Preview 门禁。
func newPromptPreviewRPCHandler(service PromptPreviewService, enabled bool) *Handler {
	return &Handler{
		logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		promptPreview: service, promptPreviewEnabled: enabled,
	}
}

// promptHandlerFixtures 返回合法生成上下文和不可变 Prompt Draft。
func promptHandlerFixtures(t *testing.T) (promptpreview.GenerationContext, promptpreview.Draft) {
	t.Helper()
	storyboardContent := storyboardpreview.Content{
		Title: "夏日品牌短片故事板", Summary: "以海边场景建立产品情绪。",
		Sections: []storyboardpreview.Section{{Key: "section_1", Title: "开场", Objective: "建立夏日氛围"}},
		Elements: []storyboardpreview.Element{{
			Key: "element_1", SectionKey: "section_1", Order: 1, Type: storyboardpreview.ElementTypeScene,
			Title: "海边开场", NarrativePurpose: "建立情绪", DurationSeconds: 10,
			SourcePhaseKey: "phase_1", DependencyKeys: []string{},
		}},
		Slots: []storyboardpreview.Slot{{
			Key: "slot_1", ElementKey: "element_1", Type: storyboardpreview.SlotTypeImage,
			Purpose: "海边环境图", Required: true,
		}},
	}
	storyboardDigest, err := storyboardpreview.ContentDigest(storyboardContent)
	if err != nil {
		t.Fatalf("storyboard ContentDigest() error = %v", err)
	}
	sourceDigest, err := promptpreview.DigestFromBytes(storyboardDigest.Bytes())
	if err != nil {
		t.Fatalf("DigestFromBytes() error = %v", err)
	}
	reference := promptpreview.StoryboardPreviewRef{
		ID: promptHandlerStoryboardID, Version: storyboardpreview.InitialDraftVersion, ContentDigest: sourceDigest.Hex(),
	}
	content := promptpreview.Content{
		SchemaVersion: promptpreview.DraftSchemaVersion, Mode: promptpreview.DraftMode,
		SourceStoryboardPreviewRef: reference,
		Prompts: []promptpreview.PromptEntry{{
			TargetLocalKey: "slot_1", ElementLocalKey: "element_1", SlotType: "image", MediaKind: "image",
			Purpose: "海边环境图", Required: true, PositivePrompt: "明亮夏日海边，品牌色调自然融入",
			NegativeConstraints: []string{}, OutputLanguage: "zh-CN",
		}},
	}
	contentDigest, err := promptpreview.ContentDigest(content)
	if err != nil {
		t.Fatalf("prompt ContentDigest() error = %v", err)
	}
	exactDigest, err := promptpreview.ParseDigest(strings.Repeat("2", 64))
	if err != nil {
		t.Fatalf("ParseDigest() error = %v", err)
	}
	now := time.Date(2026, 7, 17, 9, 0, 0, 0, time.UTC)
	draft := promptpreview.Draft{
		ID: promptHandlerDraftID, ProjectID: promptHandlerProjectID, UserID: promptHandlerUserID,
		StoryboardPreviewRef: reference, Status: promptpreview.DraftStatus, Version: promptpreview.InitialDraftVersion,
		SchemaVersion: promptpreview.DraftSchemaVersion, Content: content, ContentDigest: contentDigest,
		ExactTargetSetDigest: exactDigest, SourceToolCallID: promptHandlerToolCallID,
		SourcePromptVersion:            "graph_tool.write_prompts.preview.v1",
		SourceValidatorVersion:         "write_prompts.preview.validator.v1",
		SourceExactSetValidatorVersion: "write_prompts.preview.exact-set-validator.v1",
		CreatedAt:                      now, UpdatedAt: now,
	}
	contextResult := promptpreview.GenerationContext{
		ProjectID: promptHandlerProjectID, ProjectVersion: 7, ProjectTitle: "夏日品牌短片",
		Storyboard: promptpreview.StoryboardSnapshot{
			ID: promptHandlerStoryboardID, ProjectID: promptHandlerProjectID, UserID: promptHandlerUserID,
			Status: storyboardpreview.DraftStatus, Version: storyboardpreview.InitialDraftVersion,
			SchemaVersion: storyboardpreview.DraftSchemaVersion, Content: storyboardContent, ContentDigest: sourceDigest,
		},
	}
	return contextResult, draft
}

// promptHandlerReferenceRPC 映射测试使用的 Storyboard Preview 精确引用。
func promptHandlerReferenceRPC(reference promptpreview.StoryboardPreviewRef) *foundationv1.PromptPreviewStoryboardRefV1 {
	return &foundationv1.PromptPreviewStoryboardRefV1{
		StoryboardPreviewId: reference.ID, Version: reference.Version, ContentDigest: reference.ContentDigest,
	}
}

// promptHandlerContentRPC 返回手工构造的 Prompt Content，用于覆盖 RPC 枚举和空列表转换。
func promptHandlerContentRPC(reference *foundationv1.PromptPreviewStoryboardRefV1) *foundationv1.PromptPreviewDraftContentV1 {
	return &foundationv1.PromptPreviewDraftContentV1{
		SchemaVersion: promptpreview.DraftSchemaVersion, Mode: promptpreview.DraftMode,
		SourceStoryboardPreviewRef: reference,
		Prompts: []*foundationv1.PromptPreviewDraftEntryV1{{
			TargetLocalKey: "slot_1", ElementLocalKey: "element_1",
			SlotType:  foundationv1.StoryboardPreviewSlotTypeV1_IMAGE,
			MediaKind: foundationv1.PromptPreviewMediaKindV1_IMAGE,
			Purpose:   "海边环境图", Required: true, PositivePrompt: "明亮夏日海边，品牌色调自然融入",
			NegativeConstraints: []string{}, OutputLanguage: "zh-CN",
		}},
	}
}

// promptHandlerQueryRequest 返回严格命令查询请求。
func promptHandlerQueryRequest(requestDigest string) *foundationv1.QueryPromptDraftCommandPreviewRequestV1 {
	return &foundationv1.QueryPromptDraftCommandPreviewRequestV1{
		SchemaVersion: foundationv1.PROMPT_PREVIEW_RPC_SCHEMA_VERSION, RequestId: promptHandlerRequestID,
		CommandId: promptHandlerCommandID, RequestDigest: requestDigest,
		UserId: promptHandlerUserID, ProjectId: promptHandlerProjectID,
	}
}

// assertPromptHandlerServiceError 断言稳定 Thrift 错误码和重试语义。
func assertPromptHandlerServiceError(t *testing.T, err error, code string, retryable bool, index int) {
	t.Helper()
	serviceError, ok := err.(*foundationv1.FoundationServiceExceptionV1)
	if !ok || serviceError.Code != code || serviceError.Retryable != retryable {
		t.Fatalf("case %d error=%T %v, want code=%s retryable=%t", index, err, err, code, retryable)
	}
}
