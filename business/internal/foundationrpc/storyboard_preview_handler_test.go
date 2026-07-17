package foundationrpc

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/FigoGoo/Dora-Agent/business/internal/creationspec"
	"github.com/FigoGoo/Dora-Agent/business/internal/storyboardpreview"
	"github.com/FigoGoo/Dora-Agent/business/kitex_gen/foundationv1"
)

const (
	storyboardHandlerRequestID      = "019f68e8-0001-7000-8000-000000000001"
	storyboardHandlerUserID         = "019f68e8-0002-7000-8000-000000000002"
	storyboardHandlerProjectID      = "019f68e8-0003-7000-8000-000000000003"
	storyboardHandlerCreationSpecID = "019f68e8-0004-7000-8000-000000000004"
	storyboardHandlerDraftID        = "019f68e8-0005-7000-8000-000000000005"
)

// storyboardHandlerServiceStub 记录三类 RPC 是否正确映射到领域 Service。
type storyboardHandlerServiceStub struct {
	contextResult storyboardpreview.PlanningContext
	saveResult    storyboardpreview.SaveResult
	queryResult   storyboardpreview.QueryResult
	contextCalls  int
	saveCalls     int
	queryCalls    int
}

// GetPlanningContext 返回测试冻结的联合上下文。
func (stub *storyboardHandlerServiceStub) GetPlanningContext(context.Context, storyboardpreview.ContextQuery) (storyboardpreview.PlanningContext, error) {
	stub.contextCalls++
	return stub.contextResult, nil
}

// SaveDraft 返回测试冻结的保存结果。
func (stub *storyboardHandlerServiceStub) SaveDraft(context.Context, storyboardpreview.SaveCommand) (storyboardpreview.SaveResult, error) {
	stub.saveCalls++
	return stub.saveResult, nil
}

// QueryCommand 返回测试冻结的命令查询结果。
func (stub *storyboardHandlerServiceStub) QueryCommand(context.Context, string, string, string, string) (storyboardpreview.QueryResult, error) {
	stub.queryCalls++
	return stub.queryResult, nil
}

// TestStoryboardPreviewRPCMapsContextSaveAndQuery 验证三类 Foundation RPC 的严格 DTO 映射与安全 Resource 输出。
func TestStoryboardPreviewRPCMapsContextSaveAndQuery(t *testing.T) {
	content := storyboardHandlerContent()
	contentDigest, err := storyboardpreview.ContentDigest(content)
	if err != nil {
		t.Fatalf("ContentDigest() error = %v", err)
	}
	creationSpecDigest, err := storyboardpreview.ParseDigest(strings.Repeat("1", 64))
	if err != nil {
		t.Fatalf("ParseDigest() error = %v", err)
	}
	reference := storyboardpreview.CreationSpecRef{ID: storyboardHandlerCreationSpecID, Version: 1, ContentDigest: creationSpecDigest}
	draft := storyboardpreview.Draft{
		ID: storyboardHandlerDraftID, ProjectID: storyboardHandlerProjectID, UserID: storyboardHandlerUserID,
		CreationSpecRef: reference, Status: storyboardpreview.DraftStatus, Version: 1,
		SchemaVersion: storyboardpreview.DraftSchemaVersion, Content: content, ContentDigest: contentDigest,
	}
	stub := &storyboardHandlerServiceStub{
		contextResult: storyboardpreview.PlanningContext{
			ProjectID: storyboardHandlerProjectID, ProjectVersion: 1, ProjectTitle: "测试项目",
			CreationSpec: storyboardpreview.CreationSpecSnapshot{
				ID: storyboardHandlerCreationSpecID, ProjectID: storyboardHandlerProjectID, UserID: storyboardHandlerUserID,
				Status: creationspec.DraftStatus, Version: 1, SchemaVersion: creationspec.DraftSchemaVersion,
				Content: storyboardHandlerCreationSpecContent(), ContentDigest: creationSpecDigest,
			},
		},
		saveResult:  storyboardpreview.SaveResult{Disposition: storyboardpreview.CommandDispositionCreated, Draft: draft},
		queryResult: storyboardpreview.QueryResult{Status: storyboardpreview.QueryStatusCompleted, Draft: &draft},
	}
	handler := &Handler{logger: slog.New(slog.NewTextHandler(io.Discard, nil)), storyboardPreview: stub, storyboardEnabled: true}

	refRPC := &foundationv1.StoryboardPreviewCreationSpecRefV1{
		CreationSpecId: reference.ID, Version: reference.Version, ContentDigest: reference.ContentDigest.Hex(),
	}
	contextResponse, err := handler.GetStoryboardPlanningContextPreviewV1(context.Background(), &foundationv1.GetStoryboardPlanningContextPreviewRequestV1{
		SchemaVersion: foundationv1.STORYBOARD_PREVIEW_RPC_SCHEMA_VERSION, RequestId: storyboardHandlerRequestID,
		UserId: storyboardHandlerUserID, ProjectId: storyboardHandlerProjectID, CreationSpecRef: refRPC,
	})
	if err != nil || contextResponse.CreationSpec == nil || contextResponse.CreationSpec.CreationSpecId != storyboardHandlerCreationSpecID {
		t.Fatalf("GetStoryboardPlanningContextPreviewV1() response=%+v error=%v", contextResponse, err)
	}

	saveResponse, err := handler.SaveStoryboardDraftPreviewV1(context.Background(), &foundationv1.SaveStoryboardDraftPreviewRequestV1{
		SchemaVersion: foundationv1.STORYBOARD_PREVIEW_RPC_SCHEMA_VERSION, RequestId: storyboardHandlerRequestID,
		CommandId: storyboardHandlerRequestID, RequestDigest: strings.Repeat("2", 64),
		UserId: storyboardHandlerUserID, ProjectId: storyboardHandlerProjectID, ExpectedProjectVersion: 1,
		CreationSpecRef: refRPC, ToolCallId: storyboardHandlerRequestID,
		PromptVersion: "prompt.v1", ValidatorVersion: "validator.v1", Content: storyboardHandlerContentRPC(),
	})
	if err != nil || saveResponse.Resource == nil || saveResponse.Resource.StoryboardPreviewId != storyboardHandlerDraftID {
		t.Fatalf("SaveStoryboardDraftPreviewV1() response=%+v error=%v", saveResponse, err)
	}

	queryResponse, err := handler.QueryStoryboardDraftCommandPreviewV1(context.Background(), &foundationv1.QueryStoryboardDraftCommandPreviewRequestV1{
		SchemaVersion: foundationv1.STORYBOARD_PREVIEW_RPC_SCHEMA_VERSION, RequestId: storyboardHandlerRequestID,
		CommandId: storyboardHandlerRequestID, RequestDigest: strings.Repeat("2", 64),
		UserId: storyboardHandlerUserID, ProjectId: storyboardHandlerProjectID,
	})
	if err != nil || queryResponse.Status != foundationv1.StoryboardPreviewQueryStatusV1_COMPLETED || queryResponse.Resource == nil {
		t.Fatalf("QueryStoryboardDraftCommandPreviewV1() response=%+v error=%v", queryResponse, err)
	}
	if stub.contextCalls != 1 || stub.saveCalls != 1 || stub.queryCalls != 1 {
		t.Fatalf("unexpected service calls: context=%d save=%d query=%d", stub.contextCalls, stub.saveCalls, stub.queryCalls)
	}
}

// TestStoryboardPreviewRPCDisabledFailsBeforeService 验证 M1 Bootstrap 默认关闭时三类 RPC 不接触领域 Service。
func TestStoryboardPreviewRPCDisabledFailsBeforeService(t *testing.T) {
	stub := &storyboardHandlerServiceStub{}
	handler := &Handler{storyboardPreview: stub, storyboardEnabled: false}
	_, err := handler.GetStoryboardPlanningContextPreviewV1(context.Background(), &foundationv1.GetStoryboardPlanningContextPreviewRequestV1{})
	var serviceError *foundationv1.FoundationServiceExceptionV1
	if !errors.As(err, &serviceError) || serviceError.Code != featureDisabledCode || stub.contextCalls != 0 {
		t.Fatalf("disabled RPC error=%v calls=%d", err, stub.contextCalls)
	}
}

// storyboardHandlerContent 返回 Handler 测试使用的严格 Storyboard 内容。
func storyboardHandlerContent() storyboardpreview.Content {
	return storyboardpreview.Content{
		Title: "测试故事板", Summary: "测试摘要",
		Sections: []storyboardpreview.Section{{Key: "section_1", Title: "开场", Objective: "建立背景"}},
		Elements: []storyboardpreview.Element{{
			Key: "element_1", SectionKey: "section_1", Order: 1, Type: storyboardpreview.ElementTypeScene,
			Title: "开场画面", NarrativePurpose: "建立叙事", DurationSeconds: 10,
			SourcePhaseKey: "phase_1", DependencyKeys: []string{},
		}},
		Slots: []storyboardpreview.Slot{},
	}
}

// storyboardHandlerContentRPC 返回与领域测试内容等价的 Thrift DTO。
func storyboardHandlerContentRPC() *foundationv1.StoryboardPreviewContentV1 {
	return storyboardContentToRPC(storyboardHandlerContent())
}

// storyboardHandlerCreationSpecContent 返回上下文映射测试使用的严格 CreationSpec 内容。
func storyboardHandlerCreationSpecContent() creationspec.Content {
	return creationspec.Content{
		Title: "创作规范", Goal: "制作测试短片", DeliverableType: creationspec.DeliverableTypeVideo,
		Audience: "测试用户", Locale: "zh-CN",
		Phases:      []creationspec.Phase{{Key: "phase_1", Title: "规划", Objective: "规划内容", Output: "故事板"}},
		Constraints: []string{}, AcceptanceCriteria: []string{"故事板结构完整"},
	}
}
