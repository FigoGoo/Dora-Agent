package httpserver

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/business/internal/agentidentity"
	"github.com/FigoGoo/Dora-Agent/business/internal/auth"
	"github.com/FigoGoo/Dora-Agent/business/internal/config"
	"github.com/FigoGoo/Dora-Agent/business/internal/promptpreview"
	"github.com/FigoGoo/Dora-Agent/business/internal/storyboardpreview"
)

// writePromptsGenerationStub 记录 Binder 交给 Business 权威 Prompt 上下文服务的精确查询。
type writePromptsGenerationStub struct {
	query  promptpreview.ContextQuery
	result promptpreview.GenerationContext
	err    error
	calls  int
}

// GetGenerationContext 返回冻结测试快照，模拟 Business PostgreSQL 领域边界。
func (stub *writePromptsGenerationStub) GetGenerationContext(_ context.Context, query promptpreview.ContextQuery) (promptpreview.GenerationContext, error) {
	stub.calls++
	stub.query = query
	return stub.result, stub.err
}

// writePromptsBinderStoryboardContent 返回同时满足 Workspace Card 与 Business Draft Validator 的内容。
func writePromptsBinderStoryboardContent() storyboardpreview.Content {
	return storyboardpreview.Content{
		Title: "夏日产品短片", Summary: "用两个镜头展示产品价值",
		Sections: []storyboardpreview.Section{{Key: "section_1", Title: "开场", Objective: "建立产品认知"}},
		Elements: []storyboardpreview.Element{
			{Key: "element_1", SectionKey: "section_1", Order: 1, Type: storyboardpreview.ElementTypeScene, Title: "产品登场", NarrativePurpose: "建立视觉焦点", DurationSeconds: 5, SourcePhaseKey: "phase_1", DependencyKeys: []string{}},
			{Key: "element_2", SectionKey: "section_1", Order: 2, Type: storyboardpreview.ElementTypeShot, Title: "功能演示", NarrativePurpose: "展示核心卖点", DurationSeconds: 10, SourcePhaseKey: "phase_1", DependencyKeys: []string{"element_1"}},
		},
		Slots: []storyboardpreview.Slot{
			{Key: "slot_1", ElementKey: "element_1", Type: storyboardpreview.SlotTypeImage, Purpose: "产品主视觉", Required: true},
			{Key: "slot_2", ElementKey: "element_2", Type: storyboardpreview.SlotTypeVideo, Purpose: "功能演示画面", Required: true},
		},
	}
}

// writePromptsBinderCurrentRef 返回与测试 Storyboard 内容一致的精确引用。
func writePromptsBinderCurrentRef(t *testing.T) WritePromptsPreviewStoryboardRef {
	t.Helper()
	digest, err := storyboardpreview.ContentDigest(writePromptsBinderStoryboardContent())
	if err != nil {
		t.Fatalf("ContentDigest() error = %v", err)
	}
	return WritePromptsPreviewStoryboardRef{ID: planStoryboardPreviewCreationSpecID, Version: 1, ContentDigest: digest.Hex()}
}

// writePromptsBinderGenerationContext 返回与当前 Workspace Card 完全一致的权威 Business 快照。
func writePromptsBinderGenerationContext(t *testing.T) promptpreview.GenerationContext {
	t.Helper()
	ref := writePromptsBinderCurrentRef(t)
	digest, err := promptpreview.ParseDigest(ref.ContentDigest)
	if err != nil {
		t.Fatal(err)
	}
	return promptpreview.GenerationContext{
		ProjectID: agentProxyProjectID, ProjectVersion: 1, ProjectTitle: "测试项目",
		Storyboard: promptpreview.StoryboardSnapshot{
			ID: ref.ID, ProjectID: agentProxyProjectID, UserID: agentProxyUserID,
			Status: storyboardpreview.DraftStatus, Version: 1, SchemaVersion: storyboardpreview.DraftSchemaVersion,
			Content: writePromptsBinderStoryboardContent(), ContentDigest: digest,
		},
	}
}

// writePromptsBinderCardJSON 返回字段完整的 current Storyboard Workspace Card。
func writePromptsBinderCardJSON(t *testing.T) string {
	t.Helper()
	ref := writePromptsBinderCurrentRef(t)
	return `{"schema_version":"storyboard.preview.card.v1","input_id":"019f0000-0000-7000-8000-00000000000a",` +
		`"turn_id":"019f0000-0000-7000-8000-00000000000b","run_id":"019f0000-0000-7000-8000-00000000000c",` +
		`"tool_call_id":"019f0000-0000-7000-8000-00000000000d","status":"completed",` +
		`"result_code":"STORYBOARD_PREVIEW_DRAFT_CREATED","updated_at":"2026-07-17T00:00:00Z",` +
		`"storyboard_preview_id":"` + ref.ID + `","project_id":"` + agentProxyProjectID + `",` +
		`"creation_spec_ref":{"id":"019f0000-0000-7000-8000-00000000000e","version":1,"content_digest":"` + strings.Repeat("a", 64) + `"},` +
		`"version":1,"content_digest":"` + ref.ContentDigest + `","title":"夏日产品短片","summary":"用两个镜头展示产品价值",` +
		`"sections":[{"key":"section_1","title":"开场","objective":"建立产品认知"}],` +
		`"elements":[{"key":"element_1","section_key":"section_1","order":1,"element_type":"scene","title":"产品登场","narrative_purpose":"建立视觉焦点","duration_seconds":5,"source_phase_key":"phase_1","dependency_keys":[]},` +
		`{"key":"element_2","section_key":"section_1","order":2,"element_type":"shot","title":"功能演示","narrative_purpose":"展示核心卖点","duration_seconds":10,"source_phase_key":"phase_1","dependency_keys":["element_1"]}],` +
		`"slots":[{"key":"slot_1","element_key":"element_1","slot_type":"image","purpose":"产品主视觉","required":true},` +
		`{"key":"slot_2","element_key":"element_2","slot_type":"video","purpose":"功能演示画面","required":true}]}`
}

// writePromptsBinderWorkspaceResponse 构造指定 Workspace 版本的当前 Storyboard 权威投影。
func writePromptsBinderWorkspaceResponse(requestID string, card string, schemaVersion string) *http.Response {
	storyboardField := `,"plan_storyboard_preview":` + card
	if card == "" {
		storyboardField = ""
	}
	body := `{"schema_version":"` + schemaVersion + `","request_id":"` + requestID +
		`","session":{"id":"` + agentProxySessionID + `","project_id":"` + agentProxyProjectID +
		`","status":"active","version":1,"created_at":"2026-07-17T00:00:00Z","updated_at":"2026-07-17T00:00:00Z"},` +
		`"messages":[],"inputs":[],"creation_spec_preview":null,"latest_turn_output":null,"analyze_materials_preview":null` +
		storyboardField + `,"write_prompts_preview":null,"media_previews":[],"event_high_watermark":1,"min_available_seq":1}`
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
}

// writePromptsBinderContext 放入 Binder 必须消费的 Business 会话事实。
func writePromptsBinderContext() context.Context {
	resolved := auth.ResolvedSession{
		Principal: auth.Principal{ID: agentProxyUserID}, WebSessionID: agentProxyWebID,
		WebSessionVersion: 7, SessionExpiresAt: time.Now().Add(time.Hour),
	}
	return auth.ContextWithResolvedSession(context.Background(), resolved)
}

// newWritePromptsBinderForTest 用共享 Agent Client/Signer 配置创建双权威 Binder。
func newWritePromptsBinderForTest(t *testing.T, client AgentHTTPClient, service WritePromptsPreviewGenerationContextService) (WritePromptsPreviewStoryboardBinder, *agentProxySignerStub) {
	t.Helper()
	signer := &agentProxySignerStub{}
	base, err := NewAgentProxyHandler(&agentProxyAccessStub{}, signer, agentProxyIDs{}, client, config.AgentHTTPConfig{
		BaseURL: "http://agent.internal", RequestTimeout: time.Second, PreviewMaxRequestBodyBytes: 4096,
	})
	if err != nil {
		t.Fatal(err)
	}
	binder, err := NewWritePromptsPreviewStoryboardBinder(base, service)
	if err != nil {
		t.Fatal(err)
	}
	return binder, signer
}

// TestWritePromptsPreviewBinderDerivesCurrentStoryboardFromWorkspace 验证 PresentedRef 不参与资源选择，当前 Card 还会由 Business 内容逐值复核。
func TestWritePromptsPreviewBinderDerivesCurrentStoryboardFromWorkspace(t *testing.T) {
	service := &writePromptsGenerationStub{result: writePromptsBinderGenerationContext(t)}
	var upstream *http.Request
	binder, signer := newWritePromptsBinderForTest(t, agentProxyClientFunc(func(request *http.Request) (*http.Response, error) {
		upstream = request
		return writePromptsBinderWorkspaceResponse(agentProxyRequestID, writePromptsBinderCardJSON(t), writePromptsWorkspaceSchema), nil
	}), service)
	result, err := binder.BindCurrent(writePromptsBinderContext(), WritePromptsPreviewStoryboardBindingRequest{
		RequestID: agentProxyRequestID, UserID: agentProxyUserID, ProjectID: agentProxyProjectID,
		AgentSessionID: agentProxySessionID, PresentedRef: WritePromptsPreviewStoryboardRef{ID: agentProxyOtherID, Version: 1, ContentDigest: strings.Repeat("b", 64)},
	})
	if err != nil || result != writePromptsBinderCurrentRef(t) {
		t.Fatalf("BindCurrent() result=%+v error=%v", result, err)
	}
	if upstream == nil || upstream.Method != http.MethodGet || signer.identity.Scope != agentidentity.ScopeWorkspaceRead ||
		service.calls != 1 || service.query.UserID != agentProxyUserID || service.query.ProjectID != agentProxyProjectID ||
		service.query.StoryboardPreviewRef.ID != result.ID || service.query.StoryboardPreviewRef.ContentDigest != result.ContentDigest {
		t.Fatalf("authority binding mismatch upstream=%v identity=%+v query=%+v", upstream, signer.identity, service.query)
	}
}

// TestWritePromptsPreviewBinderRejectsMissingAndDriftedAuthority 验证缺卡、损坏 Card 与 Business 漂移都不会退回 PresentedRef。
func TestWritePromptsPreviewBinderRejectsMissingAndDriftedAuthority(t *testing.T) {
	tests := []struct {
		name       string
		card       string
		schema     string
		serviceErr error
		want       error
		calls      int
	}{
		{name: "legacy workspace v4", card: writePromptsBinderCardJSON(t), schema: "session.workspace.v4", want: ErrWritePromptsPreviewStoryboardUnavailable},
		{name: "missing field", card: "", schema: writePromptsWorkspaceSchema, want: ErrWritePromptsPreviewStoryboardUnavailable},
		{name: "missing card", card: "null", schema: writePromptsWorkspaceSchema, want: ErrWritePromptsPreviewStoryboardNotFound},
		{name: "card digest drift", card: strings.Replace(writePromptsBinderCardJSON(t), "产品主视觉", "其他用途", 1), schema: writePromptsWorkspaceSchema, want: ErrWritePromptsPreviewStoryboardUnavailable},
		{name: "business version drift", card: writePromptsBinderCardJSON(t), schema: writePromptsWorkspaceSchema, serviceErr: promptpreview.ErrStoryboardVersionConflict, want: ErrWritePromptsPreviewStoryboardConflict, calls: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := &writePromptsGenerationStub{result: writePromptsBinderGenerationContext(t), err: test.serviceErr}
			binder, _ := newWritePromptsBinderForTest(t, agentProxyClientFunc(func(*http.Request) (*http.Response, error) {
				return writePromptsBinderWorkspaceResponse(agentProxyRequestID, test.card, test.schema), nil
			}), service)
			_, err := binder.BindCurrent(writePromptsBinderContext(), WritePromptsPreviewStoryboardBindingRequest{
				RequestID: agentProxyRequestID, UserID: agentProxyUserID, ProjectID: agentProxyProjectID,
				AgentSessionID: agentProxySessionID, PresentedRef: writePromptsBinderCurrentRef(t),
			})
			if !errors.Is(err, test.want) || service.calls != test.calls {
				t.Fatalf("BindCurrent() error=%v calls=%d, want=%v/%d", err, service.calls, test.want, test.calls)
			}
		})
	}
}
