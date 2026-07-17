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
	"github.com/FigoGoo/Dora-Agent/business/internal/creationspec"
	"github.com/FigoGoo/Dora-Agent/business/internal/storyboardpreview"
)

// planStoryboardPlanningStub 记录 Binder 交给 Business 权威领域服务的精确查询。
type planStoryboardPlanningStub struct {
	query  storyboardpreview.ContextQuery
	result storyboardpreview.PlanningContext
	err    error
	calls  int
}

// GetPlanningContext 返回冻结测试快照，模拟 Business PostgreSQL 领域边界。
func (stub *planStoryboardPlanningStub) GetPlanningContext(_ context.Context, query storyboardpreview.ContextQuery) (storyboardpreview.PlanningContext, error) {
	stub.calls++
	stub.query = query
	return stub.result, stub.err
}

// planStoryboardBinderContent 返回同时满足 CreationSpec Card 与 Business Draft Validator 的内容。
func planStoryboardBinderContent() creationspec.Content {
	return creationspec.Content{
		Title: "夏日品牌短片", Goal: "展示产品在夏日场景中的核心价值",
		DeliverableType: creationspec.DeliverableTypeVideo, Audience: "年轻消费者", Locale: "zh-CN",
		Phases: []creationspec.Phase{{
			Key: "phase_1", Title: "开场", Objective: "建立夏日氛围", Output: "十秒开场画面",
		}},
		Constraints: []string{}, AcceptanceCriteria: []string{"产品名称清晰可见"},
	}
}

// planStoryboardBinderCurrentRef 返回与测试内容一致的 Business 精确引用。
func planStoryboardBinderCurrentRef(t *testing.T) PlanStoryboardPreviewCreationSpecRef {
	t.Helper()
	digest, err := creationspec.ContentDigest(planStoryboardBinderContent())
	if err != nil {
		t.Fatalf("ContentDigest() error = %v", err)
	}
	return PlanStoryboardPreviewCreationSpecRef{
		ID: planStoryboardPreviewCreationSpecID, Version: creationspec.InitialDraftVersion, ContentDigest: digest.Hex(),
	}
}

// planStoryboardBinderPlanningContext 返回与当前 Workspace Card 完全一致的权威 Business 快照。
func planStoryboardBinderPlanningContext(t *testing.T) storyboardpreview.PlanningContext {
	t.Helper()
	ref := planStoryboardBinderCurrentRef(t)
	digest, err := storyboardpreview.ParseDigest(ref.ContentDigest)
	if err != nil {
		t.Fatalf("ParseDigest() error = %v", err)
	}
	return storyboardpreview.PlanningContext{
		ProjectID: agentProxyProjectID, ProjectVersion: 1, ProjectTitle: "测试项目",
		CreationSpec: storyboardpreview.CreationSpecSnapshot{
			ID: ref.ID, ProjectID: agentProxyProjectID, UserID: agentProxyUserID,
			Status: creationspec.DraftStatus, Version: ref.Version, SchemaVersion: creationspec.DraftSchemaVersion,
			Content: planStoryboardBinderContent(), ContentDigest: digest,
		},
	}
}

// planStoryboardBinderWorkspaceResponse 构造指定 Workspace 版本的 current CreationSpec Card。
func planStoryboardBinderWorkspaceResponse(t *testing.T, requestID string, card string, schemaVersion string) *http.Response {
	t.Helper()
	creationSpecField := `,"creation_spec_preview":` + card
	if card == "" {
		creationSpecField = ""
	}
	body := `{"schema_version":"` + schemaVersion + `","request_id":"` + requestID +
		`","session":{"id":"` + agentProxySessionID + `","project_id":"` + agentProxyProjectID +
		`","status":"active","version":1,"created_at":"2026-07-17T00:00:00Z","updated_at":"2026-07-17T00:00:00Z"},` +
		`"messages":[],"inputs":[]` + creationSpecField +
		`,"latest_turn_output":null,"analyze_materials_preview":null,"plan_storyboard_preview":null,` +
		`"write_prompts_preview":null,"media_previews":[],"event_high_watermark":1,"min_available_seq":1}`
	return &http.Response{
		StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json; charset=utf-8"}},
		Body: io.NopCloser(strings.NewReader(body)),
	}
}

// planStoryboardBinderCardJSON 返回由 Agent Workspace 决定、并可由 Business 内容逐值复核的 Card。
func planStoryboardBinderCardJSON(t *testing.T) string {
	t.Helper()
	ref := planStoryboardBinderCurrentRef(t)
	return `{"schema_version":"creation_spec.preview.card.v1","creation_spec_id":"` + ref.ID +
		`","project_id":"` + agentProxyProjectID + `","version":1,"status":"draft","content_digest":"` + ref.ContentDigest +
		`","title":"夏日品牌短片","goal":"展示产品在夏日场景中的核心价值","deliverable_type":"video",` +
		`"audience":"年轻消费者","locale":"zh-CN","phases":[{"key":"phase_1","title":"开场","objective":"建立夏日氛围","output":"十秒开场画面"}],` +
		`"constraints":[],"acceptance_criteria":["产品名称清晰可见"],"updated_at":"2026-07-17T00:00:00Z"}`
}

// planStoryboardBinderContext 放入正式 Binder 必须消费的私有 Business 会话事实。
func planStoryboardBinderContext() context.Context {
	resolved := auth.ResolvedSession{
		Principal: auth.Principal{ID: agentProxyUserID}, WebSessionID: agentProxyWebID,
		WebSessionVersion: 7, SessionExpiresAt: time.Now().Add(time.Hour),
	}
	return auth.ContextWithResolvedSession(context.Background(), resolved)
}

// newPlanStoryboardBinderForTest 用共享 Agent Client/Signer 配置创建双权威 Binder。
func newPlanStoryboardBinderForTest(
	t *testing.T,
	client AgentHTTPClient,
	planning PlanStoryboardPreviewPlanningContextService,
) (PlanStoryboardPreviewCreationSpecBinder, *agentProxySignerStub) {
	t.Helper()
	signer := &agentProxySignerStub{}
	base, err := NewAgentProxyHandler(&agentProxyAccessStub{}, signer, agentProxyIDs{}, client, config.AgentHTTPConfig{
		BaseURL: "http://agent.internal", RequestTimeout: time.Second, PreviewMaxRequestBodyBytes: 4096,
	})
	if err != nil {
		t.Fatalf("NewAgentProxyHandler() error = %v", err)
	}
	binder, err := NewPlanStoryboardPreviewCreationSpecBinder(base, planning)
	if err != nil {
		t.Fatalf("NewPlanStoryboardPreviewCreationSpecBinder() error = %v", err)
	}
	return binder, signer
}

// TestPlanStoryboardPreviewBinderDerivesCurrentRefFromWorkspace 验证 PresentedRef 不参与资源选择，当前 Card 还会由 Business 内容逐值复核。
func TestPlanStoryboardPreviewBinderDerivesCurrentRefFromWorkspace(t *testing.T) {
	planning := &planStoryboardPlanningStub{result: planStoryboardBinderPlanningContext(t)}
	var upstream *http.Request
	binder, signer := newPlanStoryboardBinderForTest(t, agentProxyClientFunc(func(request *http.Request) (*http.Response, error) {
		upstream = request
		if _, ok := request.Context().Deadline(); !ok {
			t.Fatal("Workspace authority lookup has no bounded deadline")
		}
		return planStoryboardBinderWorkspaceResponse(t, agentProxyRequestID, planStoryboardBinderCardJSON(t), planStoryboardWorkspaceSchema), nil
	}), planning)
	presented := PlanStoryboardPreviewCreationSpecRef{
		ID: agentProxyOtherID, Version: 1, ContentDigest: strings.Repeat("b", 64),
	}
	result, err := binder.BindCurrent(planStoryboardBinderContext(), PlanStoryboardPreviewCreationSpecBindingRequest{
		RequestID: agentProxyRequestID, UserID: agentProxyUserID, ProjectID: agentProxyProjectID,
		AgentSessionID: agentProxySessionID, PresentedRef: presented,
	})
	if err != nil || result != planStoryboardBinderCurrentRef(t) {
		t.Fatalf("BindCurrent() result=%+v error=%v", result, err)
	}
	if upstream == nil || upstream.Method != http.MethodGet ||
		upstream.URL.String() != "http://agent.internal/api/v1/agent/sessions/"+agentProxySessionID+"/workspace" ||
		upstream.Header.Get("Accept") != "application/json" || len(upstream.Header) != 4 {
		t.Fatalf("unsafe Workspace authority request=%v headers=%v", upstream, upstream.Header)
	}
	if signer.identity.Scope != agentidentity.ScopeWorkspaceRead || signer.identity.RequestID != agentProxyRequestID ||
		signer.identity.ProjectID != agentProxyProjectID || signer.identity.AgentSessionID != agentProxySessionID ||
		planning.calls != 1 || planning.query.UserID != agentProxyUserID || planning.query.ProjectID != agentProxyProjectID ||
		planning.query.CreationSpecRef.ID != result.ID || planning.query.CreationSpecRef.Version != result.Version ||
		planning.query.CreationSpecRef.ContentDigest.Hex() != result.ContentDigest {
		t.Fatalf("authority binding mismatch identity=%+v query=%+v", signer.identity, planning.query)
	}
}

// TestPlanStoryboardPreviewBinderRejectsMissingOrDriftedAuthority 验证缺卡、损坏 Card 与 Business 漂移都不会退回 PresentedRef。
func TestPlanStoryboardPreviewBinderRejectsMissingOrDriftedAuthority(t *testing.T) {
	tests := []struct {
		name      string
		card      string
		schema    string
		planErr   error
		wantError error
		wantCalls int
	}{
		{name: "legacy workspace v3", card: planStoryboardBinderCardJSON(t), schema: "session.workspace.v3", wantError: ErrPlanStoryboardPreviewCreationSpecUnavailable},
		{name: "missing card field is protocol drift", card: "", schema: planStoryboardWorkspaceSchema, wantError: ErrPlanStoryboardPreviewCreationSpecUnavailable},
		{name: "missing current card", card: "null", schema: planStoryboardWorkspaceSchema, wantError: ErrPlanStoryboardPreviewCreationSpecNotFound},
		{name: "card content digest drift", card: strings.Replace(planStoryboardBinderCardJSON(t), "产品名称清晰可见", "另一条标准", 1), schema: planStoryboardWorkspaceSchema, wantError: ErrPlanStoryboardPreviewCreationSpecUnavailable},
		{name: "business version drift", card: planStoryboardBinderCardJSON(t), schema: planStoryboardWorkspaceSchema, planErr: storyboardpreview.ErrCreationSpecVersionConflict, wantError: ErrPlanStoryboardPreviewCreationSpecConflict, wantCalls: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			planning := &planStoryboardPlanningStub{result: planStoryboardBinderPlanningContext(t), err: test.planErr}
			binder, _ := newPlanStoryboardBinderForTest(t, agentProxyClientFunc(func(*http.Request) (*http.Response, error) {
				return planStoryboardBinderWorkspaceResponse(t, agentProxyRequestID, test.card, test.schema), nil
			}), planning)
			_, err := binder.BindCurrent(planStoryboardBinderContext(), PlanStoryboardPreviewCreationSpecBindingRequest{
				RequestID: agentProxyRequestID, UserID: agentProxyUserID, ProjectID: agentProxyProjectID,
				AgentSessionID: agentProxySessionID, PresentedRef: planStoryboardBinderCurrentRef(t),
			})
			if !errors.Is(err, test.wantError) || planning.calls != test.wantCalls {
				t.Fatalf("BindCurrent() error=%v calls=%d, want error=%v calls=%d", err, planning.calls, test.wantError, test.wantCalls)
			}
		})
	}
}
