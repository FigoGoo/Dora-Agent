package businessrpc

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/agent/internal/config"
	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/plancreationspec"
	"github.com/FigoGoo/Dora-Agent/agent/kitex_gen/foundationv1"
	"github.com/cloudwego/kitex/client/callopt"
)

const (
	previewRPCTestRequestID   = "019f68e8-0010-7000-8000-000000000010"
	previewRPCTestUserID      = "019f68e8-0011-7000-8000-000000000011"
	previewRPCTestProjectID   = "019f68e8-0012-7000-8000-000000000012"
	previewRPCTestToolCallID  = "019f68e8-0013-7000-8000-000000000013"
	previewRPCTestCommandID   = "019f68e8-0014-7000-8000-000000000014"
	previewRPCTestResourceID  = "019f68e8-0015-7000-8000-000000000015"
	previewRPCTestDigest      = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	previewRPCTestContentHash = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

type previewProtocolStub struct {
	getErr   error
	saveErr  error
	queryErr error

	getResponse   *foundationv1.GetCreationSpecContextPreviewResponseV1
	saveResponse  *foundationv1.SaveCreationSpecDraftPreviewResponseV1
	queryResponse *foundationv1.QueryCreationSpecDraftCommandPreviewResponseV1

	getRequest   *foundationv1.GetCreationSpecContextPreviewRequestV1
	saveRequest  *foundationv1.SaveCreationSpecDraftPreviewRequestV1
	queryRequest *foundationv1.QueryCreationSpecDraftCommandPreviewRequestV1
}

func (stub *previewProtocolStub) GetCreationSpecContextPreviewV1(
	_ context.Context,
	request *foundationv1.GetCreationSpecContextPreviewRequestV1,
	_ ...callopt.Option,
) (*foundationv1.GetCreationSpecContextPreviewResponseV1, error) {
	stub.getRequest = request
	return stub.getResponse, stub.getErr
}

func (stub *previewProtocolStub) SaveCreationSpecDraftPreviewV1(
	_ context.Context,
	request *foundationv1.SaveCreationSpecDraftPreviewRequestV1,
	_ ...callopt.Option,
) (*foundationv1.SaveCreationSpecDraftPreviewResponseV1, error) {
	stub.saveRequest = request
	return stub.saveResponse, stub.saveErr
}

func (stub *previewProtocolStub) QueryCreationSpecDraftCommandPreviewV1(
	_ context.Context,
	request *foundationv1.QueryCreationSpecDraftCommandPreviewRequestV1,
	_ ...callopt.Option,
) (*foundationv1.QueryCreationSpecDraftCommandPreviewResponseV1, error) {
	stub.queryRequest = request
	return stub.queryResponse, stub.queryErr
}

// TestPreviewRPCErrorCodesUseOperationSpecificTaxonomy 钉住 Business exact code，不允许 Retryable 布尔值改写 Unknown Outcome 所有权。
func TestPreviewRPCErrorCodesUseOperationSpecificTaxonomy(t *testing.T) {
	testCases := []struct {
		code      string
		readWant  error
		saveWant  error
		queryWant error
	}{
		{code: "NOT_FOUND", readWant: plancreationspec.ErrBusinessNotFound, saveWant: plancreationspec.ErrBusinessNotFound, queryWant: plancreationspec.ErrBusinessNotFound},
		{code: "PROJECT_VERSION_CONFLICT", readWant: plancreationspec.ErrBusinessConflict, saveWant: plancreationspec.ErrBusinessConflict, queryWant: plancreationspec.ErrBusinessConflict},
		{code: "IDEMPOTENCY_CONFLICT", readWant: plancreationspec.ErrBusinessConflict, saveWant: plancreationspec.ErrBusinessConflict, queryWant: plancreationspec.ErrBusinessConflict},
		{code: "INVALID_ARGUMENT", readWant: plancreationspec.ErrBusinessConflict, saveWant: plancreationspec.ErrBusinessConflict, queryWant: plancreationspec.ErrBusinessConflict},
		{code: "FEATURE_DISABLED", readWant: plancreationspec.ErrBusinessDisabled, saveWant: plancreationspec.ErrBusinessDisabled, queryWant: plancreationspec.ErrBusinessUnknownOutcome},
		{code: "PREVIEW_UNAVAILABLE", readWant: plancreationspec.ErrBusinessTechnical, saveWant: plancreationspec.ErrBusinessTechnical, queryWant: plancreationspec.ErrBusinessUnknownOutcome},
		{code: "PERSISTENCE_UNAVAILABLE", readWant: plancreationspec.ErrBusinessTechnical, saveWant: plancreationspec.ErrBusinessUnknownOutcome, queryWant: plancreationspec.ErrBusinessUnknownOutcome},
		{code: "UNRECOGNIZED_CODE", readWant: plancreationspec.ErrBusinessTechnical, saveWant: plancreationspec.ErrBusinessUnknownOutcome, queryWant: plancreationspec.ErrBusinessUnknownOutcome},
	}
	for _, fixture := range testCases {
		for _, retryable := range []bool{false, true} {
			name := fmt.Sprintf("%s/retryable=%t", fixture.code, retryable)
			t.Run(name, func(t *testing.T) {
				serviceErr := &foundationv1.FoundationServiceExceptionV1{
					Code: fixture.code, Message: "不可穿透的 Business 详情", Retryable: retryable,
				}
				for _, operation := range []struct {
					name string
					want error
				}{
					{name: "read", want: fixture.readWant},
					{name: "save", want: fixture.saveWant},
					{name: "query", want: fixture.queryWant},
				} {
					t.Run(operation.name, func(t *testing.T) {
						got := invokePreviewRPCWithError(t, operation.name, serviceErr)
						if !errors.Is(got, operation.want) {
							t.Fatalf("错误=%v want=%v", got, operation.want)
						}
						if got == serviceErr {
							t.Fatal("Business ServiceException 原文穿透 Agent 边界")
						}
					})
				}
			})
		}
	}
}

// TestPreviewRPCTransportErrorsPreserveReadVersusCommandSemantics 验证只读可重试失败与 Save/Query 未知结果不被压平。
func TestPreviewRPCTransportErrorsPreserveReadVersusCommandSemantics(t *testing.T) {
	transportFailure := errors.New("transport reset")
	for _, fixture := range []struct {
		name      string
		err       error
		readWant  error
		saveWant  error
		queryWant error
	}{
		{name: "context canceled", err: fmt.Errorf("rpc canceled: %w", context.Canceled), readWant: context.Canceled, saveWant: plancreationspec.ErrBusinessUnknownOutcome, queryWant: plancreationspec.ErrBusinessUnknownOutcome},
		{name: "deadline exceeded", err: fmt.Errorf("rpc deadline: %w", context.DeadlineExceeded), readWant: context.DeadlineExceeded, saveWant: plancreationspec.ErrBusinessUnknownOutcome, queryWant: plancreationspec.ErrBusinessUnknownOutcome},
		{name: "transport failure", err: transportFailure, readWant: plancreationspec.ErrBusinessTechnical, saveWant: plancreationspec.ErrBusinessUnknownOutcome, queryWant: plancreationspec.ErrBusinessUnknownOutcome},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			for _, operation := range []struct {
				name string
				want error
			}{
				{name: "read", want: fixture.readWant},
				{name: "save", want: fixture.saveWant},
				{name: "query", want: fixture.queryWant},
			} {
				t.Run(operation.name, func(t *testing.T) {
					got := invokePreviewRPCWithError(t, operation.name, fixture.err)
					if !errors.Is(got, operation.want) {
						t.Fatalf("错误=%v want=%v", got, operation.want)
					}
				})
			}
		})
	}
}

// TestPreviewRPCMethodsMapExactProtocolDTOs 验证三方法只在 Kitex 边界显式映射冻结 DTO 和 enum。
func TestPreviewRPCMethodsMapExactProtocolDTOs(t *testing.T) {
	command := previewRPCCommand()
	resource := previewProtocolResource(command.Content)
	stub := &previewProtocolStub{
		getResponse: &foundationv1.GetCreationSpecContextPreviewResponseV1{
			SchemaVersion: foundationv1.CREATION_SPEC_PREVIEW_SCHEMA_VERSION,
			RequestId:     previewRPCTestRequestID, ProjectId: previewRPCTestProjectID,
			ProjectVersion: 3, ProjectTitle: "预览项目",
		},
		saveResponse: &foundationv1.SaveCreationSpecDraftPreviewResponseV1{
			SchemaVersion: foundationv1.CREATION_SPEC_PREVIEW_SCHEMA_VERSION,
			RequestId:     previewRPCTestRequestID, CommandId: previewRPCTestCommandID,
			Disposition: foundationv1.CreationSpecPreviewCommandDispositionV1_CREATED, Resource: resource,
		},
		queryResponse: &foundationv1.QueryCreationSpecDraftCommandPreviewResponseV1{
			SchemaVersion: foundationv1.CREATION_SPEC_PREVIEW_SCHEMA_VERSION,
			RequestId:     previewRPCTestRequestID, CommandId: previewRPCTestCommandID,
			Status: foundationv1.CreationSpecPreviewQueryStatusV1_COMPLETED, Resource: resource,
		},
	}
	client := previewRPCClient(stub)

	contextResult, err := client.GetCreationSpecContext(context.Background(), previewRPCTestRequestID, previewRPCTestUserID, previewRPCTestProjectID)
	if err != nil || contextResult.ProjectID != previewRPCTestProjectID || contextResult.ProjectVersion != 3 || contextResult.ProjectTitle != "预览项目" {
		t.Fatalf("Get 映射结果=%+v err=%v", contextResult, err)
	}
	if stub.getRequest == nil || stub.getRequest.SchemaVersion != foundationv1.CREATION_SPEC_PREVIEW_SCHEMA_VERSION ||
		stub.getRequest.RequestId != previewRPCTestRequestID || stub.getRequest.UserId != previewRPCTestUserID ||
		stub.getRequest.ProjectId != previewRPCTestProjectID {
		t.Fatalf("Get request 映射错误: %+v", stub.getRequest)
	}

	disposition, saved, err := client.SaveCreationSpecDraft(context.Background(), command)
	if err != nil || disposition != plancreationspec.SaveDispositionCreated || saved.ID != previewRPCTestResourceID ||
		saved.Content.DeliverableType != "video" {
		t.Fatalf("Save 映射结果 disposition=%q resource=%+v err=%v", disposition, saved, err)
	}
	if stub.saveRequest == nil || stub.saveRequest.RequestId != previewRPCTestRequestID ||
		stub.saveRequest.CommandId != previewRPCTestCommandID || stub.saveRequest.RequestDigest != previewRPCTestDigest ||
		stub.saveRequest.UserId != previewRPCTestUserID || stub.saveRequest.ProjectId != previewRPCTestProjectID ||
		stub.saveRequest.ExpectedProjectVersion != 3 || stub.saveRequest.ToolCallId != previewRPCTestToolCallID ||
		stub.saveRequest.PromptVersion != plancreationspec.PromptVersion ||
		stub.saveRequest.ValidatorVersion != plancreationspec.ValidatorVersion || stub.saveRequest.Content == nil ||
		stub.saveRequest.Content.DeliverableType != foundationv1.CreationSpecPreviewDeliverableTypeV1_VIDEO {
		t.Fatalf("Save request 映射错误: %+v", stub.saveRequest)
	}

	status, queried, err := client.QueryCreationSpecDraftCommand(context.Background(), command)
	if err != nil || status != "completed" || queried == nil || queried.ID != previewRPCTestResourceID {
		t.Fatalf("Query 映射结果 status=%q resource=%+v err=%v", status, queried, err)
	}
	if stub.queryRequest == nil || stub.queryRequest.RequestId != previewRPCTestRequestID ||
		stub.queryRequest.CommandId != previewRPCTestCommandID || stub.queryRequest.RequestDigest != previewRPCTestDigest ||
		stub.queryRequest.UserId != previewRPCTestUserID || stub.queryRequest.ProjectId != previewRPCTestProjectID {
		t.Fatalf("Query request 映射错误: %+v", stub.queryRequest)
	}
}

// TestPreviewRPCPreservesRequiredEmptyConstraintList 锁定 Agent→Business 请求及 Business→Agent 响应都保留 required []，不得退化为 nil。
func TestPreviewRPCPreservesRequiredEmptyConstraintList(t *testing.T) {
	command := previewRPCCommand()
	command.Content.Constraints = []string{}
	resource := previewProtocolResource(command.Content)
	stub := &previewProtocolStub{saveResponse: &foundationv1.SaveCreationSpecDraftPreviewResponseV1{
		SchemaVersion: foundationv1.CREATION_SPEC_PREVIEW_SCHEMA_VERSION,
		RequestId:     previewRPCTestRequestID,
		CommandId:     previewRPCTestCommandID,
		Disposition:   foundationv1.CreationSpecPreviewCommandDispositionV1_CREATED,
		Resource:      resource,
	}}

	disposition, saved, err := previewRPCClient(stub).SaveCreationSpecDraft(context.Background(), command)
	if err != nil || disposition != plancreationspec.SaveDispositionCreated {
		t.Fatalf("空约束 RPC 映射失败: disposition=%q resource=%+v err=%v", disposition, saved, err)
	}
	if stub.saveRequest == nil || stub.saveRequest.Content == nil || stub.saveRequest.Content.Constraints == nil ||
		len(stub.saveRequest.Content.Constraints) != 0 {
		t.Fatalf("Agent→Business required constraints 未保留 []: %+v", stub.saveRequest)
	}
	if saved.Content.Constraints == nil || len(saved.Content.Constraints) != 0 {
		t.Fatalf("Business→Agent required constraints 未保留 []: %#v", saved.Content.Constraints)
	}
}

func invokePreviewRPCWithError(t *testing.T, operation string, injected error) error {
	t.Helper()
	stub := &previewProtocolStub{}
	switch operation {
	case "read":
		stub.getErr = injected
	case "save":
		stub.saveErr = injected
	case "query":
		stub.queryErr = injected
	default:
		t.Fatalf("未知测试 operation=%q", operation)
	}
	client := previewRPCClient(stub)
	switch operation {
	case "read":
		_, err := client.GetCreationSpecContext(context.Background(), previewRPCTestRequestID, previewRPCTestUserID, previewRPCTestProjectID)
		return err
	case "save":
		_, _, err := client.SaveCreationSpecDraft(context.Background(), previewRPCCommand())
		return err
	case "query":
		_, _, err := client.QueryCreationSpecDraftCommand(context.Background(), previewRPCCommand())
		return err
	}
	return nil
}

func previewRPCClient(stub *previewProtocolStub) *Client {
	return &Client{preview: stub, config: config.BusinessRPCConfig{RequestTimeout: time.Second}}
}

func previewRPCCommand() plancreationspec.DraftCommand {
	return plancreationspec.DraftCommand{
		TrustedContext: plancreationspec.TrustedContext{
			RequestID: previewRPCTestRequestID, UserID: previewRPCTestUserID,
			ProjectID: previewRPCTestProjectID, ToolCallID: previewRPCTestToolCallID,
			BusinessCommandID: previewRPCTestCommandID, PromptVersion: plancreationspec.PromptVersion,
			ValidatorVersion: plancreationspec.ValidatorVersion,
		},
		DomainContext: plancreationspec.DomainContext{ProjectID: previewRPCTestProjectID, ProjectVersion: 3},
		Content: plancreationspec.Content{
			Title: "预览规划", Goal: "生成一支品牌短片", DeliverableType: "video",
			Audience: "年轻用户", Locale: "zh-CN",
			Phases:      []plancreationspec.Phase{{Key: "phase_1", Title: "规划", Objective: "确定叙事", Output: "创意方案"}},
			Constraints: []string{"竖屏"}, AcceptanceCriteria: []string{"输出可执行方案"},
		},
		RequestDigest: previewRPCTestDigest,
	}
}

func previewProtocolResource(content plancreationspec.Content) *foundationv1.CreationSpecDraftPreviewResourceV1 {
	return &foundationv1.CreationSpecDraftPreviewResourceV1{
		CreationSpecId: previewRPCTestResourceID, ProjectId: previewRPCTestProjectID,
		Version: 1, Status: "draft", ContentDigest: previewRPCTestContentHash,
		Content: mapContentToProtocol(content),
	}
}

var _ creationSpecPreviewProtocolClient = (*previewProtocolStub)(nil)
