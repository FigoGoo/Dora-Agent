package businessrpc

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/agent/internal/config"
	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/planstoryboard"
	"github.com/FigoGoo/Dora-Agent/agent/kitex_gen/foundationv1"
	"github.com/cloudwego/kitex/client/callopt"
)

const (
	storyboardRPCRequestID      = "019f68e8-3010-7000-8000-000000000010"
	storyboardRPCUserID         = "019f68e8-3011-7000-8000-000000000011"
	storyboardRPCProjectID      = "019f68e8-3012-7000-8000-000000000012"
	storyboardRPCToolCallID     = "019f68e8-3013-7000-8000-000000000013"
	storyboardRPCCommandID      = "019f68e8-3014-7000-8000-000000000014"
	storyboardRPCCreationSpecID = "019f68e8-3015-7000-8000-000000000015"
	storyboardRPCResourceID     = "019f68e8-3016-7000-8000-000000000016"
	storyboardRPCCreationDigest = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	storyboardRPCRequestDigest  = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	storyboardRPCContentDigest  = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
)

type storyboardProtocolStub struct {
	getResponse   *foundationv1.GetStoryboardPlanningContextPreviewResponseV1
	saveResponse  *foundationv1.SaveStoryboardDraftPreviewResponseV1
	queryResponse *foundationv1.QueryStoryboardDraftCommandPreviewResponseV1
	getErr        error
	saveErr       error
	queryErr      error
	getRequest    *foundationv1.GetStoryboardPlanningContextPreviewRequestV1
	saveRequest   *foundationv1.SaveStoryboardDraftPreviewRequestV1
	queryRequest  *foundationv1.QueryStoryboardDraftCommandPreviewRequestV1
}

func (stub *storyboardProtocolStub) GetStoryboardPlanningContextPreviewV1(
	_ context.Context,
	request *foundationv1.GetStoryboardPlanningContextPreviewRequestV1,
	_ ...callopt.Option,
) (*foundationv1.GetStoryboardPlanningContextPreviewResponseV1, error) {
	stub.getRequest = request
	return stub.getResponse, stub.getErr
}

func (stub *storyboardProtocolStub) SaveStoryboardDraftPreviewV1(
	_ context.Context,
	request *foundationv1.SaveStoryboardDraftPreviewRequestV1,
	_ ...callopt.Option,
) (*foundationv1.SaveStoryboardDraftPreviewResponseV1, error) {
	stub.saveRequest = request
	return stub.saveResponse, stub.saveErr
}

func (stub *storyboardProtocolStub) QueryStoryboardDraftCommandPreviewV1(
	_ context.Context,
	request *foundationv1.QueryStoryboardDraftCommandPreviewRequestV1,
	_ ...callopt.Option,
) (*foundationv1.QueryStoryboardDraftCommandPreviewResponseV1, error) {
	stub.queryRequest = request
	return stub.queryResponse, stub.queryErr
}

// TestStoryboardPreviewRPCMapsFrozenDTOsAndPreservesEmptyLists 钉住三条 RPC 的显式映射及 required 空列表语义。
func TestStoryboardPreviewRPCMapsFrozenDTOsAndPreservesEmptyLists(t *testing.T) {
	command := storyboardRPCCommand()
	resource := storyboardProtocolResource()
	stub := &storyboardProtocolStub{
		getResponse: &foundationv1.GetStoryboardPlanningContextPreviewResponseV1{
			SchemaVersion: foundationv1.STORYBOARD_PREVIEW_RPC_SCHEMA_VERSION,
			RequestId:     storyboardRPCRequestID, ProjectId: storyboardRPCProjectID,
			ProjectVersion: 3, ProjectTitle: "分镜项目", CreationSpec: storyboardProtocolCreationSpec(),
		},
		saveResponse: &foundationv1.SaveStoryboardDraftPreviewResponseV1{
			SchemaVersion: foundationv1.STORYBOARD_PREVIEW_RPC_SCHEMA_VERSION,
			RequestId:     storyboardRPCRequestID, CommandId: storyboardRPCCommandID,
			Disposition: foundationv1.StoryboardPreviewCommandDispositionV1_CREATED, Resource: resource,
		},
		queryResponse: &foundationv1.QueryStoryboardDraftCommandPreviewResponseV1{
			SchemaVersion: foundationv1.STORYBOARD_PREVIEW_RPC_SCHEMA_VERSION,
			RequestId:     storyboardRPCRequestID, CommandId: storyboardRPCCommandID,
			Status: foundationv1.StoryboardPreviewQueryStatusV1_COMPLETED, Resource: resource,
		},
	}
	client := storyboardRPCClient(stub)

	planning, err := client.GetStoryboardPlanningContext(context.Background(), command.TrustedContext)
	if err != nil || planning.ProjectVersion != 3 || planning.CreationSpec.ID != storyboardRPCCreationSpecID {
		t.Fatalf("Get 映射结果=%+v err=%v", planning, err)
	}
	if stub.getRequest == nil || stub.getRequest.SchemaVersion != foundationv1.STORYBOARD_PREVIEW_RPC_SCHEMA_VERSION ||
		stub.getRequest.UserId != storyboardRPCUserID || stub.getRequest.ProjectId != storyboardRPCProjectID ||
		stub.getRequest.CreationSpecRef == nil || stub.getRequest.CreationSpecRef.ContentDigest != storyboardRPCCreationDigest {
		t.Fatalf("Get request 映射错误: %+v", stub.getRequest)
	}
	if planning.CreationSpec.Content.Constraints == nil || len(planning.CreationSpec.Content.Constraints) != 0 {
		t.Fatalf("required 空 constraints 被改写: %#v", planning.CreationSpec.Content.Constraints)
	}

	disposition, saved, err := client.SaveStoryboardDraft(context.Background(), command)
	if err != nil || disposition != planstoryboard.SaveDispositionCreated || saved.StoryboardPreviewID != storyboardRPCResourceID {
		t.Fatalf("Save 映射 disposition=%q resource=%+v err=%v", disposition, saved, err)
	}
	if stub.saveRequest == nil || stub.saveRequest.RequestDigest != storyboardRPCRequestDigest ||
		stub.saveRequest.ExpectedProjectVersion != 3 || stub.saveRequest.CreationSpecRef == nil ||
		stub.saveRequest.Content == nil || stub.saveRequest.Content.Elements[0].DependencyKeys == nil ||
		stub.saveRequest.Content.Slots == nil {
		t.Fatalf("Save request 丢失冻结字段或 required 空列表: %+v", stub.saveRequest)
	}
	if saved.Content.Elements[0].DependencyKeys == nil || saved.Content.Slots == nil {
		t.Fatalf("Save response 丢失 required 空列表: %+v", saved.Content)
	}

	status, queried, err := client.QueryStoryboardDraftCommand(context.Background(), command)
	if err != nil || status != "completed" || queried == nil || queried.StoryboardPreviewID != storyboardRPCResourceID {
		t.Fatalf("Query 映射 status=%q resource=%+v err=%v", status, queried, err)
	}
	if stub.queryRequest == nil || stub.queryRequest.CommandId != storyboardRPCCommandID ||
		stub.queryRequest.RequestDigest != storyboardRPCRequestDigest || stub.queryRequest.UserId != storyboardRPCUserID {
		t.Fatalf("Query request 映射错误: %+v", stub.queryRequest)
	}
}

// TestStoryboardPreviewRPCErrorOwnership 钉住只读失败与写入 Unknown Outcome 的所有权边界。
func TestStoryboardPreviewRPCErrorOwnership(t *testing.T) {
	testCases := []struct {
		name      string
		err       error
		operation storyboardPreviewOperation
		want      error
	}{
		{name: "read not found", err: storyboardServiceError("NOT_FOUND"), operation: storyboardPreviewRead, want: planstoryboard.ErrBusinessNotFound},
		{name: "read unavailable", err: storyboardServiceError("PERSISTENCE_UNAVAILABLE"), operation: storyboardPreviewRead, want: planstoryboard.ErrBusinessTechnical},
		{name: "creation spec changed", err: storyboardServiceError("CREATION_SPEC_VERSION_CONFLICT"), operation: storyboardPreviewSave, want: planstoryboard.ErrBusinessCreationSpecConflict},
		{name: "save unavailable", err: storyboardServiceError("PERSISTENCE_UNAVAILABLE"), operation: storyboardPreviewSave, want: planstoryboard.ErrBusinessUnknownOutcome},
		{name: "query disabled", err: storyboardServiceError("FEATURE_DISABLED"), operation: storyboardPreviewQuery, want: planstoryboard.ErrBusinessUnknownOutcome},
		{name: "save deadline", err: context.DeadlineExceeded, operation: storyboardPreviewSave, want: planstoryboard.ErrBusinessUnknownOutcome},
	}
	for _, fixture := range testCases {
		t.Run(fixture.name, func(t *testing.T) {
			if got := mapStoryboardPreviewRPCError(fixture.err, fixture.operation); !errors.Is(got, fixture.want) {
				t.Fatalf("错误=%v want=%v", got, fixture.want)
			}
		})
	}
}

// TestStoryboardPreviewRPCRejectsUnknownEnums 验证协议枚举扩展不会被静默接纳。
func TestStoryboardPreviewRPCRejectsUnknownEnums(t *testing.T) {
	resource := storyboardProtocolResource()
	resource.Content.Elements[0].ElementType = foundationv1.StoryboardPreviewElementTypeV1(99)
	stub := &storyboardProtocolStub{saveResponse: &foundationv1.SaveStoryboardDraftPreviewResponseV1{
		SchemaVersion: foundationv1.STORYBOARD_PREVIEW_RPC_SCHEMA_VERSION,
		RequestId:     storyboardRPCRequestID, CommandId: storyboardRPCCommandID,
		Disposition: foundationv1.StoryboardPreviewCommandDispositionV1_CREATED, Resource: resource,
	}}
	_, _, err := storyboardRPCClient(stub).SaveStoryboardDraft(context.Background(), storyboardRPCCommand())
	if !errors.Is(err, planstoryboard.ErrBusinessUnknownOutcome) {
		t.Fatalf("未知枚举未失败关闭: %v", err)
	}
}

func storyboardRPCClient(stub *storyboardProtocolStub) *Client {
	return &Client{storyboardPreview: stub, config: config.BusinessRPCConfig{RequestTimeout: time.Second}}
}

func storyboardRPCCommand() planstoryboard.DraftCommand {
	return planstoryboard.DraftCommand{
		TrustedContext: planstoryboard.TrustedContext{
			RequestID: storyboardRPCRequestID, UserID: storyboardRPCUserID, ProjectID: storyboardRPCProjectID,
			ToolCallID: storyboardRPCToolCallID, BusinessCommandID: storyboardRPCCommandID,
			CreationSpecRef: planstoryboard.CreationSpecRef{ID: storyboardRPCCreationSpecID, Version: 1, ContentDigest: storyboardRPCCreationDigest},
			PromptVersion:   planstoryboard.PromptVersion, ValidatorVersion: planstoryboard.ValidatorVersion,
		},
		DomainContext: planstoryboard.PlanningContext{ProjectID: storyboardRPCProjectID, ProjectVersion: 3},
		Content: planstoryboard.Content{
			Title: "分镜预览", Summary: "一段可执行的预览分镜",
			Sections: []planstoryboard.Section{{Key: "section_1", Title: "开场", Objective: "建立语境"}},
			Elements: []planstoryboard.Element{{
				Key: "element_1", SectionKey: "section_1", Order: 1, ElementType: "scene",
				Title: "开场场景", NarrativePurpose: "建立品牌语境", DurationSeconds: 10,
				SourcePhaseKey: "phase_1", DependencyKeys: []string{},
			}},
			Slots: []planstoryboard.Slot{},
		},
		RequestDigest: storyboardRPCRequestDigest,
	}
}

func storyboardProtocolCreationSpec() *foundationv1.CreationSpecDraftPreviewResourceV1 {
	return &foundationv1.CreationSpecDraftPreviewResourceV1{
		CreationSpecId: storyboardRPCCreationSpecID, ProjectId: storyboardRPCProjectID,
		Version: 1, Status: "draft", ContentDigest: storyboardRPCCreationDigest,
		Content: &foundationv1.CreationSpecPreviewContentV1{
			Title: "短片方案", Goal: "生成品牌短片", DeliverableType: foundationv1.CreationSpecPreviewDeliverableTypeV1_VIDEO,
			Audience: "新用户", Locale: "zh-CN",
			Phases:      []*foundationv1.CreationSpecPreviewPhaseV1{{Key: "phase_1", Title: "规划", Objective: "确定叙事", Output: "创意方案"}},
			Constraints: []string{}, AcceptanceCriteria: []string{"分镜可执行"},
		},
	}
}

func storyboardProtocolResource() *foundationv1.StoryboardDraftPreviewResourceV1 {
	content := storyboardRPCCommand().Content
	protocolContent, _ := mapStoryboardContentToProtocol(content)
	return &foundationv1.StoryboardDraftPreviewResourceV1{
		StoryboardPreviewId: storyboardRPCResourceID, ProjectId: storyboardRPCProjectID,
		CreationSpecRef: &foundationv1.StoryboardPreviewCreationSpecRefV1{
			CreationSpecId: storyboardRPCCreationSpecID, Version: 1, ContentDigest: storyboardRPCCreationDigest,
		},
		Version: 1, Status: "draft", ContentDigest: storyboardRPCContentDigest, Content: protocolContent,
	}
}

func storyboardServiceError(code string) error {
	return &foundationv1.FoundationServiceExceptionV1{Code: code, Message: "不可穿透详情", Retryable: true}
}

var _ storyboardPreviewProtocolClient = (*storyboardProtocolStub)(nil)
