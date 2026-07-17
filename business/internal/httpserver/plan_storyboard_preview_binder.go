package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"time"

	"github.com/FigoGoo/Dora-Agent/business/internal/agentidentity"
	"github.com/FigoGoo/Dora-Agent/business/internal/auth"
	"github.com/FigoGoo/Dora-Agent/business/internal/creationspec"
	"github.com/FigoGoo/Dora-Agent/business/internal/storyboardpreview"
)

const (
	// planStoryboardWorkspaceSchema 是当前 CreationSpec Card 所在的统一 Workspace Snapshot 版本。
	planStoryboardWorkspaceSchema = "session.workspace.v5"
	// planStoryboardCreationSpecCardSchema 是当前 Workspace CreationSpec Card 的冻结版本。
	planStoryboardCreationSpecCardSchema = "creation_spec.preview.card.v1"
)

// PlanStoryboardPreviewPlanningContextService 定义 Binder 复核 Business 权威 CreationSpec Draft 的最小领域端口。
type PlanStoryboardPreviewPlanningContextService interface {
	// GetPlanningContext 按可信 Owner、Project 和精确 Draft 引用返回权威联合快照。
	GetPlanningContext(ctx context.Context, query storyboardpreview.ContextQuery) (storyboardpreview.PlanningContext, error)
}

// planStoryboardPreviewCurrentBinder 先读取 Session 当前 Workspace Card，再由 Business Draft 事实复核完整内容。
// 浏览器 PresentedRef 从不参与权威资源选择，只用于检测用户界面是否已经过期。
type planStoryboardPreviewCurrentBinder struct {
	base     *AgentProxyHandler
	planning PlanStoryboardPreviewPlanningContextService
}

var _ PlanStoryboardPreviewCreationSpecBinder = (*planStoryboardPreviewCurrentBinder)(nil)

// NewPlanStoryboardPreviewCreationSpecBinder 创建当前 Workspace 与 Business Draft 双权威绑定器。
// Agent Snapshot 决定当前 Session Card，Business PostgreSQL 决定 Draft 内容、版本与摘要是否合法。
func NewPlanStoryboardPreviewCreationSpecBinder(base *AgentProxyHandler, planning PlanStoryboardPreviewPlanningContextService) (PlanStoryboardPreviewCreationSpecBinder, error) {
	if base == nil || planning == nil {
		return nil, fmt.Errorf("create Plan Storyboard Preview CreationSpec binder: invalid dependency")
	}
	return &planStoryboardPreviewCurrentBinder{base: base, planning: planning}, nil
}

// BindCurrent 从受认证 Workspace 读取当前 Card，复核 Business Draft 后返回权威引用。
// Workspace 缺卡或资源不可见返回 NotFound；Card 与 Business 漂移返回 Conflict；协议或依赖异常失败关闭。
func (binder *planStoryboardPreviewCurrentBinder) BindCurrent(ctx context.Context, request PlanStoryboardPreviewCreationSpecBindingRequest) (PlanStoryboardPreviewCreationSpecRef, error) {
	if ctx == nil || !canonicalUUIDv7(request.RequestID) || !canonicalUUIDv7(request.UserID) ||
		!canonicalUUIDv7(request.ProjectID) || !canonicalUUIDv7(request.AgentSessionID) ||
		!validPlanStoryboardPreviewCreationSpecRef(request.PresentedRef) {
		return PlanStoryboardPreviewCreationSpecRef{}, ErrPlanStoryboardPreviewCreationSpecUnavailable
	}
	resolved, ok := auth.ResolvedSessionFromContext(ctx)
	if !ok || resolved.Principal.ID != request.UserID || !canonicalUUIDv7(resolved.WebSessionID) || resolved.WebSessionVersion < 1 {
		return PlanStoryboardPreviewCreationSpecRef{}, ErrPlanStoryboardPreviewCreationSpecUnavailable
	}
	workspaceRef, workspaceContent, err := binder.loadCurrentWorkspaceRef(ctx, request, resolved)
	if err != nil {
		return PlanStoryboardPreviewCreationSpecRef{}, err
	}
	digest, err := storyboardpreview.ParseDigest(workspaceRef.ContentDigest)
	if err != nil {
		return PlanStoryboardPreviewCreationSpecRef{}, ErrPlanStoryboardPreviewCreationSpecUnavailable
	}
	planningContext, err := binder.planning.GetPlanningContext(ctx, storyboardpreview.ContextQuery{
		UserID: request.UserID, ProjectID: request.ProjectID,
		CreationSpecRef: storyboardpreview.CreationSpecRef{
			ID: workspaceRef.ID, Version: workspaceRef.Version, ContentDigest: digest,
		},
	})
	if err != nil {
		switch {
		case errors.Is(err, storyboardpreview.ErrNotFound):
			return PlanStoryboardPreviewCreationSpecRef{}, ErrPlanStoryboardPreviewCreationSpecNotFound
		case errors.Is(err, storyboardpreview.ErrCreationSpecVersionConflict):
			return PlanStoryboardPreviewCreationSpecRef{}, ErrPlanStoryboardPreviewCreationSpecConflict
		default:
			return PlanStoryboardPreviewCreationSpecRef{}, ErrPlanStoryboardPreviewCreationSpecUnavailable
		}
	}
	authoritative := PlanStoryboardPreviewCreationSpecRef{
		ID: planningContext.CreationSpec.ID, Version: planningContext.CreationSpec.Version,
		ContentDigest: planningContext.CreationSpec.ContentDigest.Hex(),
	}
	if storyboardpreview.ValidatePlanningContext(planningContext) != nil ||
		planningContext.CreationSpec.UserID != request.UserID {
		return PlanStoryboardPreviewCreationSpecRef{}, ErrPlanStoryboardPreviewCreationSpecUnavailable
	}
	// Agent 投影必须完整等于 Business 权威 Draft；只比 ref 会让损坏或陈旧 Card 被误认作当前入口。
	if authoritative != workspaceRef || planningContext.ProjectID != request.ProjectID ||
		planningContext.CreationSpec.ProjectID != request.ProjectID ||
		!equalCreationSpecContent(planningContext.CreationSpec.Content, workspaceContent) {
		return PlanStoryboardPreviewCreationSpecRef{}, ErrPlanStoryboardPreviewCreationSpecConflict
	}
	return authoritative, nil
}

// planStoryboardWorkspaceSnapshotWire 是 Binder 消费的 Workspace 安全子集。
// 顶层其他投影字段不影响当前 CreationSpec 选择；schema、Session 与 Card 本身仍逐值严格验证。
type planStoryboardWorkspaceSnapshotWire struct {
	// SchemaVersion 在本 Runtime 固定为统一的 session.workspace.v5。
	SchemaVersion string `json:"schema_version"`
	// RequestID 必须等于本次 workspace.read 断言的请求标识。
	RequestID string `json:"request_id"`
	// Session 绑定内部断言的 Agent Session 与 Business Project。
	Session planStoryboardWorkspaceSessionWire `json:"session"`
	// CreationSpecPreview 是每个 Session 至多一条的当前 CreationSpec Card。
	CreationSpecPreview json.RawMessage `json:"creation_spec_preview"`
}

// planStoryboardWorkspaceSessionWire 是 Binder 读取的 Workspace Session 身份子集。
type planStoryboardWorkspaceSessionWire struct {
	// ID 是 Agent Session UUIDv7。
	ID string `json:"id"`
	// ProjectID 是绑定的 Business Project UUIDv7。
	ProjectID string `json:"project_id"`
	// Status 必须为可操作的 active。
	Status string `json:"status"`
	// Version 是正数 Session 版本。
	Version int64 `json:"version"`
}

// planStoryboardCreationSpecCardWire 是 current Card 的完整业务内容与精确引用。
type planStoryboardCreationSpecCardWire struct {
	// SchemaVersion 固定为 creation_spec.preview.card.v1。
	SchemaVersion string `json:"schema_version"`
	// CreationSpecID 是 Business Draft UUIDv7。
	CreationSpecID string `json:"creation_spec_id"`
	// ProjectID 是 Business Project UUIDv7。
	ProjectID string `json:"project_id"`
	// Version 在本 Preview 固定为一。
	Version int64 `json:"version"`
	// Status 在本 Preview 固定为 draft。
	Status string `json:"status"`
	// ContentDigest 是 Business canonical CreationSpec 内容摘要。
	ContentDigest string `json:"content_digest"`
	// Title 是 CreationSpec 标题。
	Title string `json:"title"`
	// Goal 是 CreationSpec 创作目标。
	Goal string `json:"goal"`
	// DeliverableType 是稳定交付物枚举。
	DeliverableType creationspec.DeliverableType `json:"deliverable_type"`
	// Audience 是可为空的目标受众。
	Audience string `json:"audience"`
	// Locale 是受支持的 locale。
	Locale string `json:"locale"`
	// Phases 是一至六个冻结阶段。
	Phases []creationspec.Phase `json:"phases"`
	// Constraints 是零至八条冻结约束。
	Constraints []string `json:"constraints"`
	// AcceptanceCriteria 是一至八条验收标准。
	AcceptanceCriteria []string `json:"acceptance_criteria"`
	// UpdatedAt 是 Agent 投影冻结的 UTC 时间。
	UpdatedAt time.Time `json:"updated_at"`
}

// loadCurrentWorkspaceRef 用专用 workspace.read 断言读取当前 Session Card，并拒绝无界、重复或漂移协议。
func (binder *planStoryboardPreviewCurrentBinder) loadCurrentWorkspaceRef(
	ctx context.Context,
	request PlanStoryboardPreviewCreationSpecBindingRequest,
	resolved auth.ResolvedSession,
) (PlanStoryboardPreviewCreationSpecRef, creationspec.Content, error) {
	target := "/api/v1/agent/sessions/" + request.AgentSessionID + "/workspace"
	assertion, err := binder.base.signer.Sign(agentidentity.Identity{
		RequestID: request.RequestID, CanonicalTarget: target, Method: http.MethodGet,
		PrincipalUserID: request.UserID, WebSessionID: resolved.WebSessionID,
		WebSessionVersion: resolved.WebSessionVersion, ProjectID: request.ProjectID,
		AgentSessionID: request.AgentSessionID, Scope: agentidentity.ScopeWorkspaceRead,
	})
	if err != nil {
		return PlanStoryboardPreviewCreationSpecRef{}, creationspec.Content{}, ErrPlanStoryboardPreviewCreationSpecUnavailable
	}
	upstream, err := http.NewRequestWithContext(ctx, http.MethodGet, binder.base.baseURL.String()+target, nil)
	if err != nil {
		return PlanStoryboardPreviewCreationSpecRef{}, creationspec.Content{}, ErrPlanStoryboardPreviewCreationSpecUnavailable
	}
	upstream.Header.Set("Accept", "application/json")
	upstream.Header.Set(agentidentity.HeaderAssertion, assertion.EncodedCanonical)
	upstream.Header.Set(agentidentity.HeaderKeyVersion, assertion.KeyVersion)
	upstream.Header.Set(agentidentity.HeaderSignature, assertion.Signature)
	requestContext, cancel := context.WithTimeout(ctx, binder.base.requestTimeout)
	defer cancel()
	response, err := binder.base.client.Do(upstream.WithContext(requestContext))
	if err != nil || response == nil || response.Body == nil {
		return PlanStoryboardPreviewCreationSpecRef{}, creationspec.Content{}, ErrPlanStoryboardPreviewCreationSpecUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return PlanStoryboardPreviewCreationSpecRef{}, creationspec.Content{}, ErrPlanStoryboardPreviewCreationSpecUnavailable
	}
	contentTypes := response.Header.Values("Content-Type")
	if len(contentTypes) != 1 {
		return PlanStoryboardPreviewCreationSpecRef{}, creationspec.Content{}, ErrPlanStoryboardPreviewCreationSpecUnavailable
	}
	mediaType, _, err := mime.ParseMediaType(contentTypes[0])
	if err != nil || mediaType != "application/json" {
		return PlanStoryboardPreviewCreationSpecRef{}, creationspec.Content{}, ErrPlanStoryboardPreviewCreationSpecUnavailable
	}
	body, err := readBoundedBody(response.Body, maximumWorkspaceResponseBytes)
	if err != nil || !json.Valid(body) {
		return PlanStoryboardPreviewCreationSpecRef{}, creationspec.Content{}, ErrPlanStoryboardPreviewCreationSpecUnavailable
	}
	duplicate, err := hasDuplicateJSONKey(body)
	if err != nil || duplicate {
		return PlanStoryboardPreviewCreationSpecRef{}, creationspec.Content{}, ErrPlanStoryboardPreviewCreationSpecUnavailable
	}
	var snapshot planStoryboardWorkspaceSnapshotWire
	if err := json.Unmarshal(body, &snapshot); err != nil || snapshot.SchemaVersion != planStoryboardWorkspaceSchema ||
		snapshot.RequestID != request.RequestID || snapshot.Session.ID != request.AgentSessionID ||
		snapshot.Session.ProjectID != request.ProjectID || snapshot.Session.Status != "active" || snapshot.Session.Version < 1 {
		return PlanStoryboardPreviewCreationSpecRef{}, creationspec.Content{}, ErrPlanStoryboardPreviewCreationSpecUnavailable
	}
	if len(snapshot.CreationSpecPreview) == 0 {
		return PlanStoryboardPreviewCreationSpecRef{}, creationspec.Content{}, ErrPlanStoryboardPreviewCreationSpecUnavailable
	}
	if bytes.Equal(bytes.TrimSpace(snapshot.CreationSpecPreview), []byte("null")) {
		return PlanStoryboardPreviewCreationSpecRef{}, creationspec.Content{}, ErrPlanStoryboardPreviewCreationSpecNotFound
	}
	var card planStoryboardCreationSpecCardWire
	decoder := json.NewDecoder(bytes.NewReader(snapshot.CreationSpecPreview))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&card); err != nil || ensureJSONEOF(decoder) != nil ||
		card.SchemaVersion != planStoryboardCreationSpecCardSchema || card.ProjectID != request.ProjectID ||
		card.Version != creationspec.InitialDraftVersion || card.Status != creationspec.DraftStatus ||
		card.UpdatedAt.IsZero() || card.UpdatedAt.Location() != time.UTC {
		return PlanStoryboardPreviewCreationSpecRef{}, creationspec.Content{}, ErrPlanStoryboardPreviewCreationSpecUnavailable
	}
	ref := PlanStoryboardPreviewCreationSpecRef{
		ID: card.CreationSpecID, Version: card.Version, ContentDigest: card.ContentDigest,
	}
	content := creationspec.Content{
		Title: card.Title, Goal: card.Goal, DeliverableType: card.DeliverableType,
		Audience: card.Audience, Locale: card.Locale, Phases: card.Phases,
		Constraints: card.Constraints, AcceptanceCriteria: card.AcceptanceCriteria,
	}
	digest, digestErr := creationspec.ContentDigest(content)
	if !validPlanStoryboardPreviewCreationSpecRef(ref) || digestErr != nil || digest.Hex() != ref.ContentDigest {
		return PlanStoryboardPreviewCreationSpecRef{}, creationspec.Content{}, ErrPlanStoryboardPreviewCreationSpecUnavailable
	}
	return ref, content, nil
}

// equalCreationSpecContent 用冻结 Canonical JSON 比较 Agent Card 与 Business Draft，避免 slice 指针或 nil 表示差异。
func equalCreationSpecContent(left creationspec.Content, right creationspec.Content) bool {
	leftJSON, leftErr := left.CanonicalJSON()
	rightJSON, rightErr := right.CanonicalJSON()
	return leftErr == nil && rightErr == nil && bytes.Equal(leftJSON, rightJSON)
}
