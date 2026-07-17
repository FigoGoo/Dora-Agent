package businessrpc

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/agent/internal/config"
	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/writeprompts"
	"github.com/FigoGoo/Dora-Agent/agent/kitex_gen/foundationv1"
	"github.com/cloudwego/kitex/client/callopt"
)

const (
	promptRPCRequestID        = "019f68e8-4010-7000-8000-000000000010"
	promptRPCUserID           = "019f68e8-4011-7000-8000-000000000011"
	promptRPCProjectID        = "019f68e8-4012-7000-8000-000000000012"
	promptRPCSessionID        = "019f68e8-4013-7000-8000-000000000013"
	promptRPCInputID          = "019f68e8-4014-7000-8000-000000000014"
	promptRPCTurnID           = "019f68e8-4015-7000-8000-000000000015"
	promptRPCRunID            = "019f68e8-4016-7000-8000-000000000016"
	promptRPCToolCallID       = "019f68e8-4017-7000-8000-000000000017"
	promptRPCCommandID        = "019f68e8-4018-7000-8000-000000000018"
	promptRPCStoryboardID     = "019f68e8-4019-7000-8000-000000000019"
	promptRPCPromptPreviewID  = "019f68e8-4020-7000-8000-000000000020"
	promptRPCStoryboardDigest = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
)

// promptPreviewProtocolStub 记录三条 RPC 的精确请求，并允许测试注入确定响应或错误。
type promptPreviewProtocolStub struct {
	getResponse   *foundationv1.GetPromptGenerationContextPreviewResponseV1
	saveResponse  *foundationv1.SavePromptDraftPreviewResponseV1
	queryResponse *foundationv1.QueryPromptDraftCommandPreviewResponseV1
	getErr        error
	saveErr       error
	queryErr      error
	getRequest    *foundationv1.GetPromptGenerationContextPreviewRequestV1
	saveRequest   *foundationv1.SavePromptDraftPreviewRequestV1
	queryRequest  *foundationv1.QueryPromptDraftCommandPreviewRequestV1
	getCalls      int
	saveCalls     int
	queryCalls    int
}

// GetPromptGenerationContextPreviewV1 记录只读请求并返回预设结果。
func (stub *promptPreviewProtocolStub) GetPromptGenerationContextPreviewV1(
	_ context.Context,
	request *foundationv1.GetPromptGenerationContextPreviewRequestV1,
	_ ...callopt.Option,
) (*foundationv1.GetPromptGenerationContextPreviewResponseV1, error) {
	stub.getCalls++
	stub.getRequest = request
	return stub.getResponse, stub.getErr
}

// SavePromptDraftPreviewV1 记录唯一写请求并返回预设结果。
func (stub *promptPreviewProtocolStub) SavePromptDraftPreviewV1(
	_ context.Context,
	request *foundationv1.SavePromptDraftPreviewRequestV1,
	_ ...callopt.Option,
) (*foundationv1.SavePromptDraftPreviewResponseV1, error) {
	stub.saveCalls++
	stub.saveRequest = request
	return stub.saveResponse, stub.saveErr
}

// QueryPromptDraftCommandPreviewV1 记录原命令查询并返回预设结果。
func (stub *promptPreviewProtocolStub) QueryPromptDraftCommandPreviewV1(
	_ context.Context,
	request *foundationv1.QueryPromptDraftCommandPreviewRequestV1,
	_ ...callopt.Option,
) (*foundationv1.QueryPromptDraftCommandPreviewResponseV1, error) {
	stub.queryCalls++
	stub.queryRequest = request
	return stub.queryResponse, stub.queryErr
}

// TestPromptPreviewRPCMapsFrozenDTOsAndPreservesEmptyLists 钉住三条 RPC 的全部冻结字段与 required 空列表语义。
func TestPromptPreviewRPCMapsFrozenDTOsAndPreservesEmptyLists(t *testing.T) {
	command := promptRPCCommand(t)
	resource := promptProtocolResource(t, command)
	stub := &promptPreviewProtocolStub{
		getResponse: &foundationv1.GetPromptGenerationContextPreviewResponseV1{
			SchemaVersion: foundationv1.PROMPT_PREVIEW_RPC_SCHEMA_VERSION,
			RequestId:     promptRPCRequestID, ProjectId: promptRPCProjectID,
			ProjectVersion: command.DomainContext.ProjectVersion, ProjectTitle: command.DomainContext.ProjectTitle,
			StoryboardPreview: promptProtocolStoryboard(command.DomainContext.Storyboard),
		},
		saveResponse: &foundationv1.SavePromptDraftPreviewResponseV1{
			SchemaVersion: foundationv1.PROMPT_PREVIEW_RPC_SCHEMA_VERSION,
			RequestId:     promptRPCRequestID, CommandId: promptRPCCommandID,
			Disposition: foundationv1.PromptPreviewCommandDispositionV1_CREATED, Resource: resource,
		},
		queryResponse: &foundationv1.QueryPromptDraftCommandPreviewResponseV1{
			SchemaVersion: foundationv1.PROMPT_PREVIEW_RPC_SCHEMA_VERSION,
			RequestId:     promptRPCRequestID, CommandId: promptRPCCommandID,
			Status: foundationv1.PromptPreviewQueryStatusV1_COMPLETED, Resource: resource,
		},
	}
	client := promptRPCClient(stub)

	generation, err := client.GetPromptGenerationContext(context.Background(), command.TrustedContext)
	if err != nil || generation.ProjectID != promptRPCProjectID || generation.Storyboard.ID != promptRPCStoryboardID ||
		len(generation.Storyboard.Content.Elements) != 1 || len(generation.Storyboard.Content.Slots) != 1 {
		t.Fatalf("Get 映射结果=%+v err=%v", generation, err)
	}
	if stub.getRequest == nil || stub.getRequest.SchemaVersion != foundationv1.PROMPT_PREVIEW_RPC_SCHEMA_VERSION ||
		stub.getRequest.RequestId != promptRPCRequestID || stub.getRequest.UserId != promptRPCUserID ||
		stub.getRequest.ProjectId != promptRPCProjectID || stub.getRequest.StoryboardPreviewRef == nil ||
		stub.getRequest.StoryboardPreviewRef.StoryboardPreviewId != promptRPCStoryboardID ||
		stub.getRequest.StoryboardPreviewRef.Version != 1 ||
		stub.getRequest.StoryboardPreviewRef.ContentDigest != promptRPCStoryboardDigest {
		t.Fatalf("Get request 映射错误: %+v", stub.getRequest)
	}

	disposition, saved, err := client.SavePromptPreviewDraft(context.Background(), command)
	if err != nil || disposition != writeprompts.SaveDispositionCreated || saved.PromptPreviewID != promptRPCPromptPreviewID {
		t.Fatalf("Save 映射 disposition=%q resource=%+v err=%v", disposition, saved, err)
	}
	request := stub.saveRequest
	if request == nil || request.RequestId != promptRPCRequestID || request.CommandId != promptRPCCommandID ||
		request.RequestDigest != command.RequestDigest || request.UserId != promptRPCUserID || request.ProjectId != promptRPCProjectID ||
		request.ExpectedProjectVersion != command.DomainContext.ProjectVersion || request.StoryboardPreviewRef == nil ||
		request.ToolCallId != promptRPCToolCallID || request.PromptVersion != writeprompts.PromptVersion ||
		request.ValidatorVersion != writeprompts.ValidatorVersion ||
		request.ExactSetValidatorVersion != writeprompts.ExactSetValidatorVersion ||
		request.ExactTargetSetDigest != command.ExactTargetSetDigest || request.Content == nil || len(request.Content.Prompts) != 1 {
		t.Fatalf("Save request 丢失冻结字段: %+v", request)
	}
	if request.Content.Prompts[0].NegativeConstraints == nil || len(request.Content.Prompts[0].NegativeConstraints) != 0 ||
		saved.Content.Prompts[0].NegativeConstraints == nil || len(saved.Content.Prompts[0].NegativeConstraints) != 0 {
		t.Fatalf("required 空 negative_constraints 被改写: request=%#v saved=%#v",
			request.Content.Prompts[0].NegativeConstraints, saved.Content.Prompts[0].NegativeConstraints)
	}

	status, queried, err := client.QueryPromptPreviewCommand(context.Background(), command)
	if err != nil || status != "completed" || queried == nil || queried.PromptPreviewID != promptRPCPromptPreviewID {
		t.Fatalf("Query 映射 status=%q resource=%+v err=%v", status, queried, err)
	}
	if stub.queryRequest == nil || stub.queryRequest.RequestId != promptRPCRequestID ||
		stub.queryRequest.CommandId != promptRPCCommandID || stub.queryRequest.RequestDigest != command.RequestDigest ||
		stub.queryRequest.UserId != promptRPCUserID || stub.queryRequest.ProjectId != promptRPCProjectID {
		t.Fatalf("Query request 映射错误: %+v", stub.queryRequest)
	}
	if stub.getCalls != 1 || stub.saveCalls != 1 || stub.queryCalls != 1 {
		t.Fatalf("Adapter 发生隐式重试: get=%d save=%d query=%d", stub.getCalls, stub.saveCalls, stub.queryCalls)
	}
}

// TestPromptPreviewRPCMapsStableSaveAndQueryEnums 钉住同义重放与两个无资源查询终态的稳定字符串。
func TestPromptPreviewRPCMapsStableSaveAndQueryEnums(t *testing.T) {
	command := promptRPCCommand(t)
	resource := promptProtocolResource(t, command)
	stub := &promptPreviewProtocolStub{saveResponse: &foundationv1.SavePromptDraftPreviewResponseV1{
		SchemaVersion: foundationv1.PROMPT_PREVIEW_RPC_SCHEMA_VERSION,
		RequestId:     promptRPCRequestID, CommandId: promptRPCCommandID,
		Disposition: foundationv1.PromptPreviewCommandDispositionV1_REPLAYED, Resource: resource,
	}}
	disposition, _, err := promptRPCClient(stub).SavePromptPreviewDraft(context.Background(), command)
	if err != nil || disposition != writeprompts.SaveDispositionReplayed {
		t.Fatalf("REPLAYED 映射错误: disposition=%q err=%v", disposition, err)
	}

	for _, fixture := range []struct {
		name   string
		status foundationv1.PromptPreviewQueryStatusV1
		want   string
	}{
		{name: "not found", status: foundationv1.PromptPreviewQueryStatusV1_NOT_FOUND, want: "not_found"},
		{name: "conflict", status: foundationv1.PromptPreviewQueryStatusV1_CONFLICT, want: "conflict"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			stub := &promptPreviewProtocolStub{queryResponse: &foundationv1.QueryPromptDraftCommandPreviewResponseV1{
				SchemaVersion: foundationv1.PROMPT_PREVIEW_RPC_SCHEMA_VERSION,
				RequestId:     promptRPCRequestID, CommandId: promptRPCCommandID, Status: fixture.status,
			}}
			status, resource, err := promptRPCClient(stub).QueryPromptPreviewCommand(context.Background(), command)
			if err != nil || status != fixture.want || resource != nil {
				t.Fatalf("Query enum 映射 status=%q resource=%+v err=%v", status, resource, err)
			}
		})
	}
}

// TestPromptPreviewRPCErrorOwnership 钉住只读、写入与权威查询的错误所有权和安全分类。
func TestPromptPreviewRPCErrorOwnership(t *testing.T) {
	testCases := []struct {
		name      string
		err       error
		operation promptPreviewOperation
		want      error
	}{
		{name: "read canceled", err: context.Canceled, operation: promptPreviewRead, want: context.Canceled},
		{name: "save deadline", err: context.DeadlineExceeded, operation: promptPreviewSave, want: writeprompts.ErrBusinessUnknownOutcome},
		{name: "read not found", err: promptServiceError("NOT_FOUND"), operation: promptPreviewRead, want: writeprompts.ErrBusinessNotFound},
		{name: "storyboard changed", err: promptServiceError("VERSION_CONFLICT"), operation: promptPreviewSave, want: writeprompts.ErrBusinessStoryboardConflict},
		{name: "project changed", err: promptServiceError("PROJECT_VERSION_CONFLICT"), operation: promptPreviewSave, want: writeprompts.ErrBusinessConflict},
		{name: "idempotency conflict", err: promptServiceError("IDEMPOTENCY_CONFLICT"), operation: promptPreviewSave, want: writeprompts.ErrBusinessConflict},
		{name: "save disabled", err: promptServiceError("FEATURE_DISABLED"), operation: promptPreviewSave, want: writeprompts.ErrBusinessDisabled},
		{name: "query disabled", err: promptServiceError("FEATURE_DISABLED"), operation: promptPreviewQuery, want: writeprompts.ErrBusinessUnknownOutcome},
		{name: "read persistence", err: promptServiceError("PERSISTENCE_UNAVAILABLE"), operation: promptPreviewRead, want: writeprompts.ErrBusinessTechnical},
		{name: "save persistence", err: promptServiceError("PERSISTENCE_UNAVAILABLE"), operation: promptPreviewSave, want: writeprompts.ErrBusinessUnknownOutcome},
		{name: "save preview unavailable", err: promptServiceError("PREVIEW_UNAVAILABLE"), operation: promptPreviewSave, want: writeprompts.ErrBusinessTechnical},
		{name: "query preview unavailable", err: promptServiceError("PREVIEW_UNAVAILABLE"), operation: promptPreviewQuery, want: writeprompts.ErrBusinessUnknownOutcome},
		{name: "read transport", err: errors.New("transport unavailable"), operation: promptPreviewRead, want: writeprompts.ErrBusinessTechnical},
		{name: "save transport", err: errors.New("transport unavailable"), operation: promptPreviewSave, want: writeprompts.ErrBusinessUnknownOutcome},
	}
	for _, fixture := range testCases {
		t.Run(fixture.name, func(t *testing.T) {
			if got := mapPromptPreviewRPCError(fixture.err, fixture.operation); !errors.Is(got, fixture.want) {
				t.Fatalf("错误=%v want=%v", got, fixture.want)
			}
		})
	}
}

// TestPromptPreviewRPCFailsClosedOnMalformedResponses 验证协议扩展、摘要漂移和非法联合不会越过 Adapter。
func TestPromptPreviewRPCFailsClosedOnMalformedResponses(t *testing.T) {
	t.Run("read unknown slot enum", func(t *testing.T) {
		command := promptRPCCommand(t)
		storyboard := promptProtocolStoryboard(command.DomainContext.Storyboard)
		storyboard.Content.Slots[0].SlotType = foundationv1.StoryboardPreviewSlotTypeV1(99)
		stub := &promptPreviewProtocolStub{getResponse: &foundationv1.GetPromptGenerationContextPreviewResponseV1{
			SchemaVersion: foundationv1.PROMPT_PREVIEW_RPC_SCHEMA_VERSION,
			RequestId:     promptRPCRequestID, ProjectId: promptRPCProjectID,
			ProjectVersion: command.DomainContext.ProjectVersion, ProjectTitle: command.DomainContext.ProjectTitle,
			StoryboardPreview: storyboard,
		}}
		_, err := promptRPCClient(stub).GetPromptGenerationContext(context.Background(), command.TrustedContext)
		if !errors.Is(err, writeprompts.ErrBusinessTechnical) {
			t.Fatalf("未知 Source enum 未失败关闭: %v", err)
		}
	})

	t.Run("save unknown disposition", func(t *testing.T) {
		command := promptRPCCommand(t)
		stub := &promptPreviewProtocolStub{saveResponse: &foundationv1.SavePromptDraftPreviewResponseV1{
			SchemaVersion: foundationv1.PROMPT_PREVIEW_RPC_SCHEMA_VERSION,
			RequestId:     promptRPCRequestID, CommandId: promptRPCCommandID,
			Disposition: foundationv1.PromptPreviewCommandDispositionV1(99), Resource: promptProtocolResource(t, command),
		}}
		_, _, err := promptRPCClient(stub).SavePromptPreviewDraft(context.Background(), command)
		if !errors.Is(err, writeprompts.ErrBusinessUnknownOutcome) {
			t.Fatalf("未知 disposition 未保持 Unknown Outcome: %v", err)
		}
	})

	t.Run("save resource digest drift", func(t *testing.T) {
		command := promptRPCCommand(t)
		resource := promptProtocolResource(t, command)
		resource.ContentDigest = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		stub := &promptPreviewProtocolStub{saveResponse: &foundationv1.SavePromptDraftPreviewResponseV1{
			SchemaVersion: foundationv1.PROMPT_PREVIEW_RPC_SCHEMA_VERSION,
			RequestId:     promptRPCRequestID, CommandId: promptRPCCommandID,
			Disposition: foundationv1.PromptPreviewCommandDispositionV1_CREATED, Resource: resource,
		}}
		_, _, err := promptRPCClient(stub).SavePromptPreviewDraft(context.Background(), command)
		if !errors.Is(err, writeprompts.ErrBusinessUnknownOutcome) {
			t.Fatalf("资源摘要漂移未保持 Unknown Outcome: %v", err)
		}
	})

	t.Run("save unknown content enum", func(t *testing.T) {
		command := promptRPCCommand(t)
		resource := promptProtocolResource(t, command)
		resource.Content.Prompts[0].MediaKind = foundationv1.PromptPreviewMediaKindV1(99)
		stub := &promptPreviewProtocolStub{saveResponse: &foundationv1.SavePromptDraftPreviewResponseV1{
			SchemaVersion: foundationv1.PROMPT_PREVIEW_RPC_SCHEMA_VERSION,
			RequestId:     promptRPCRequestID, CommandId: promptRPCCommandID,
			Disposition: foundationv1.PromptPreviewCommandDispositionV1_CREATED, Resource: resource,
		}}
		_, _, err := promptRPCClient(stub).SavePromptPreviewDraft(context.Background(), command)
		if !errors.Is(err, writeprompts.ErrBusinessUnknownOutcome) {
			t.Fatalf("未知 Content enum 未保持 Unknown Outcome: %v", err)
		}
	})

	t.Run("query invalid unions", func(t *testing.T) {
		command := promptRPCCommand(t)
		resource := promptProtocolResource(t, command)
		fixtures := []struct {
			name     string
			status   foundationv1.PromptPreviewQueryStatusV1
			resource *foundationv1.PromptPreviewDraftResourceV1
		}{
			{name: "not found with resource", status: foundationv1.PromptPreviewQueryStatusV1_NOT_FOUND, resource: resource},
			{name: "completed without resource", status: foundationv1.PromptPreviewQueryStatusV1_COMPLETED},
			{name: "unknown status", status: foundationv1.PromptPreviewQueryStatusV1(99)},
		}
		for _, fixture := range fixtures {
			t.Run(fixture.name, func(t *testing.T) {
				stub := &promptPreviewProtocolStub{queryResponse: &foundationv1.QueryPromptDraftCommandPreviewResponseV1{
					SchemaVersion: foundationv1.PROMPT_PREVIEW_RPC_SCHEMA_VERSION,
					RequestId:     promptRPCRequestID, CommandId: promptRPCCommandID,
					Status: fixture.status, Resource: fixture.resource,
				}}
				_, _, err := promptRPCClient(stub).QueryPromptPreviewCommand(context.Background(), command)
				if !errors.Is(err, writeprompts.ErrBusinessUnknownOutcome) {
					t.Fatalf("非法 Query union 未保持 Unknown Outcome: %v", err)
				}
			})
		}
	})
}

// TestPromptPreviewRPCLocallyRejectsInvalidUUIDAndDigest 验证非 canonical UUIDv7 与摘要在发起 RPC 前失败关闭。
func TestPromptPreviewRPCLocallyRejectsInvalidUUIDAndDigest(t *testing.T) {
	t.Run("uppercase source digest", func(t *testing.T) {
		command := promptRPCCommand(t)
		command.TrustedContext.StoryboardPreviewRef.ContentDigest = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
		stub := &promptPreviewProtocolStub{}
		_, err := promptRPCClient(stub).GetPromptGenerationContext(context.Background(), command.TrustedContext)
		if !errors.Is(err, writeprompts.ErrBusinessTechnical) || stub.getCalls != 0 {
			t.Fatalf("非法摘要未在 RPC 前拒绝: err=%v calls=%d", err, stub.getCalls)
		}
	})

	t.Run("uuid v4", func(t *testing.T) {
		command := promptRPCCommand(t)
		command.TrustedContext.RequestID = "550e8400-e29b-41d4-a716-446655440000"
		stub := &promptPreviewProtocolStub{}
		_, err := promptRPCClient(stub).GetPromptGenerationContext(context.Background(), command.TrustedContext)
		if !errors.Is(err, writeprompts.ErrBusinessTechnical) || stub.getCalls != 0 {
			t.Fatalf("UUIDv4 未在 RPC 前拒绝: err=%v calls=%d", err, stub.getCalls)
		}
	})

	t.Run("request digest mismatch", func(t *testing.T) {
		command := promptRPCCommand(t)
		command.RequestDigest = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		stub := &promptPreviewProtocolStub{}
		_, _, err := promptRPCClient(stub).SavePromptPreviewDraft(context.Background(), command)
		if !errors.Is(err, writeprompts.ErrBusinessTechnical) || stub.saveCalls != 0 {
			t.Fatalf("异义 request digest 未在 RPC 前拒绝: err=%v calls=%d", err, stub.saveCalls)
		}
	})
}

// promptRPCClient 创建只启用测试协议桩的 Agent Adapter。
func promptRPCClient(stub *promptPreviewProtocolStub) *Client {
	return &Client{promptPreview: stub, config: config.BusinessRPCConfig{RequestTimeout: time.Second}}
}

// promptRPCCommand 构造通过 Core 全部摘要和 exact-set 校验的冻结命令。
func promptRPCCommand(t *testing.T) writeprompts.DraftCommand {
	t.Helper()
	trusted := writeprompts.TrustedContext{
		Owner: "prompt-preview/worker-1", RequestID: promptRPCRequestID, UserID: promptRPCUserID,
		ProjectID: promptRPCProjectID, SessionID: promptRPCSessionID, InputID: promptRPCInputID,
		TurnID: promptRPCTurnID, RunID: promptRPCRunID, ToolCallID: promptRPCToolCallID,
		BusinessCommandID: promptRPCCommandID, FenceToken: 1,
		StoryboardPreviewRef: writeprompts.StoryboardPreviewRef{
			ID: promptRPCStoryboardID, Version: 1, ContentDigest: promptRPCStoryboardDigest,
		},
		PromptVersion: writeprompts.PromptVersion, ValidatorVersion: writeprompts.ValidatorVersion,
		ExactSetValidatorVersion: writeprompts.ExactSetValidatorVersion,
		Policy: writeprompts.Policy{
			Version: writeprompts.RuntimePolicyVersion, MaxTargets: 96,
			DefaultOutputLanguage: "zh-CN", MaxCommandResends: 1,
		},
	}
	generation := writeprompts.GenerationContext{
		ProjectID: promptRPCProjectID, ProjectVersion: 3, ProjectTitle: "Prompt 预览项目",
		Storyboard: writeprompts.StoryboardResource{
			ID: promptRPCStoryboardID, ProjectID: promptRPCProjectID, Version: 1, Status: "draft",
			ContentDigest: promptRPCStoryboardDigest,
			Content: writeprompts.StoryboardContent{
				SchemaVersion: "storyboard.preview.draft.v1", Title: "夏日品牌短片", Summary: "一段完整的夏日品牌短片分镜",
				Elements: []writeprompts.StoryboardElement{{
					Key: "element_1", Order: 1, Title: "海边开场", NarrativePurpose: "建立夏日品牌氛围",
				}},
				Slots: []writeprompts.StoryboardSlot{{
					Key: "slot_1", ElementKey: "element_1", SlotType: "image", Purpose: "生成海边开场主视觉", Required: true,
				}},
			},
		},
	}
	intent := writeprompts.Intent{
		SchemaVersion: writeprompts.IntentSchemaVersion, WritingInstruction: "为全部目标编写清晰提示词", OutputLanguage: "zh-CN",
	}
	intentDigest, err := writeprompts.IntentDigest(intent)
	if err != nil {
		t.Fatalf("计算 Intent 摘要失败: %v", err)
	}
	targets, exactDigest, outputLanguage, err := writeprompts.ResolveExactTargets(generation, intent, intentDigest, trusted)
	if err != nil {
		t.Fatalf("冻结 exact target set 失败: %v", err)
	}
	content, err := writeprompts.ValidateExactTargetSet(writeprompts.Candidate{
		SchemaVersion: writeprompts.CandidateSchemaVersion,
		Prompts: []writeprompts.CandidatePrompt{{
			TargetLocalKey: "slot_1", PositivePrompt: "阳光海岸与品牌产品的清晰主视觉", NegativeConstraints: []string{},
		}},
	}, targets, outputLanguage, trusted.StoryboardPreviewRef)
	if err != nil {
		t.Fatalf("构造 Prompt Content 失败: %v", err)
	}
	command := writeprompts.DraftCommand{
		TrustedContext: trusted, DomainContext: generation, Targets: targets,
		ExactTargetSetDigest: exactDigest, Content: content, ResendLimit: trusted.Policy.MaxCommandResends,
	}
	command.RequestDigest, err = writeprompts.SaveRequestDigest(command)
	if err != nil {
		t.Fatalf("计算保存请求摘要失败: %v", err)
	}
	return command
}

// promptProtocolStoryboard 把合法 Core Source fixture 映射成协议最小投影。
func promptProtocolStoryboard(value writeprompts.StoryboardResource) *foundationv1.PromptGenerationStoryboardResourcePreviewV1 {
	elements := make([]*foundationv1.PromptGenerationStoryboardElementPreviewV1, len(value.Content.Elements))
	for index, element := range value.Content.Elements {
		elements[index] = &foundationv1.PromptGenerationStoryboardElementPreviewV1{
			ElementLocalKey: element.Key, Order: int32(element.Order), Title: element.Title, NarrativePurpose: element.NarrativePurpose,
		}
	}
	slots := make([]*foundationv1.PromptGenerationStoryboardSlotPreviewV1, len(value.Content.Slots))
	for index, slot := range value.Content.Slots {
		slotType, ok := mapStoryboardSlotTypeToProtocol(slot.SlotType)
		if !ok {
			panic(fmt.Sprintf("测试 fixture 包含非法 slot type: %q", slot.SlotType))
		}
		slots[index] = &foundationv1.PromptGenerationStoryboardSlotPreviewV1{
			TargetLocalKey: slot.Key, ElementLocalKey: slot.ElementKey, SlotType: slotType,
			Purpose: slot.Purpose, Required: slot.Required,
		}
	}
	return &foundationv1.PromptGenerationStoryboardResourcePreviewV1{
		StoryboardPreviewId: value.ID, ProjectId: value.ProjectID, Version: value.Version,
		Status: value.Status, SchemaVersion: value.Content.SchemaVersion, ContentDigest: value.ContentDigest,
		Content: &foundationv1.PromptGenerationStoryboardContentPreviewV1{
			Title: value.Content.Title, Summary: value.Content.Summary, Elements: elements, Slots: slots,
		},
	}
}

// promptProtocolResource 构造与原冻结命令完全一致的 Business Prompt Draft 响应。
func promptProtocolResource(t *testing.T, command writeprompts.DraftCommand) *foundationv1.PromptPreviewDraftResourceV1 {
	t.Helper()
	content, err := mapPromptContentToProtocol(command.Content)
	if err != nil {
		t.Fatalf("映射测试 Prompt Content 失败: %v", err)
	}
	digest, err := writeprompts.ContentDigest(command.Content)
	if err != nil {
		t.Fatalf("计算测试 Prompt Content 摘要失败: %v", err)
	}
	return &foundationv1.PromptPreviewDraftResourceV1{
		PromptPreviewId: promptRPCPromptPreviewID, ProjectId: promptRPCProjectID,
		StoryboardPreviewRef: mapPromptStoryboardRefToProtocol(command.TrustedContext.StoryboardPreviewRef),
		Version:              1, Status: "draft", ContentDigest: digest,
		ExactTargetSetDigest: command.ExactTargetSetDigest, Content: content,
	}
}

// promptServiceError 构造不允许 Adapter 透传 message 的稳定 Business 异常。
func promptServiceError(code string) error {
	return &foundationv1.FoundationServiceExceptionV1{Code: code, Message: "不可穿透的内部详情", Retryable: true}
}

var _ promptPreviewProtocolClient = (*promptPreviewProtocolStub)(nil)
