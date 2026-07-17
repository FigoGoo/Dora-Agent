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
	"github.com/FigoGoo/Dora-Agent/business/internal/promptpreview"
	"github.com/FigoGoo/Dora-Agent/business/internal/storyboardpreview"
)

const (
	// writePromptsWorkspaceSchema 是包含 Storyboard 与 Prompt Projection 的统一 Workspace Snapshot 版本。
	writePromptsWorkspaceSchema = "session.workspace.v5"
	// writePromptsStoryboardCardSchema 是当前 Workspace Storyboard Preview Card 版本。
	writePromptsStoryboardCardSchema = "storyboard.preview.card.v1"
	// writePromptsStoryboardCompletedCode 是可用作 Prompt Source 的 Storyboard Tool 成功结果码。
	writePromptsStoryboardCompletedCode = "STORYBOARD_PREVIEW_DRAFT_CREATED"
)

// WritePromptsPreviewGenerationContextService 定义 Binder 复核 Business 权威 Storyboard Draft 的最小领域端口。
type WritePromptsPreviewGenerationContextService interface {
	// GetGenerationContext 按可信 Owner、Project 和精确 Storyboard 引用返回权威联合快照。
	GetGenerationContext(context.Context, promptpreview.ContextQuery) (promptpreview.GenerationContext, error)
}

// writePromptsPreviewCurrentBinder 先读取 Session 当前 Workspace Storyboard Card，再由 Business Draft 事实复核完整内容。
// 浏览器 PresentedRef 从不参与权威资源选择，只用于检测用户界面是否过期。
type writePromptsPreviewCurrentBinder struct {
	base       *AgentProxyHandler
	generation WritePromptsPreviewGenerationContextService
}

var _ WritePromptsPreviewStoryboardBinder = (*writePromptsPreviewCurrentBinder)(nil)

// NewWritePromptsPreviewStoryboardBinder 创建当前 Workspace 与 Business Storyboard Draft 双权威绑定器。
func NewWritePromptsPreviewStoryboardBinder(base *AgentProxyHandler, generation WritePromptsPreviewGenerationContextService) (WritePromptsPreviewStoryboardBinder, error) {
	if base == nil || generation == nil {
		return nil, fmt.Errorf("create Write Prompts Preview Storyboard binder: invalid dependency")
	}
	return &writePromptsPreviewCurrentBinder{base: base, generation: generation}, nil
}

// BindCurrent 从受认证 Workspace 读取当前 Storyboard Card，复核 Business Draft 后返回权威引用。
func (binder *writePromptsPreviewCurrentBinder) BindCurrent(ctx context.Context, request WritePromptsPreviewStoryboardBindingRequest) (WritePromptsPreviewStoryboardRef, error) {
	if ctx == nil || !canonicalUUIDv7(request.RequestID) || !canonicalUUIDv7(request.UserID) ||
		!canonicalUUIDv7(request.ProjectID) || !canonicalUUIDv7(request.AgentSessionID) ||
		!validWritePromptsPreviewStoryboardRef(request.PresentedRef) {
		return WritePromptsPreviewStoryboardRef{}, ErrWritePromptsPreviewStoryboardUnavailable
	}
	resolved, ok := auth.ResolvedSessionFromContext(ctx)
	if !ok || resolved.Principal.ID != request.UserID || !canonicalUUIDv7(resolved.WebSessionID) || resolved.WebSessionVersion < 1 {
		return WritePromptsPreviewStoryboardRef{}, ErrWritePromptsPreviewStoryboardUnavailable
	}
	workspaceRef, workspaceContent, err := binder.loadCurrentWorkspaceStoryboard(ctx, request, resolved)
	if err != nil {
		return WritePromptsPreviewStoryboardRef{}, err
	}
	generationContext, err := binder.generation.GetGenerationContext(ctx, promptpreview.ContextQuery{
		UserID: request.UserID, ProjectID: request.ProjectID,
		StoryboardPreviewRef: promptpreview.StoryboardPreviewRef{
			ID: workspaceRef.ID, Version: workspaceRef.Version, ContentDigest: workspaceRef.ContentDigest,
		},
	})
	if err != nil {
		switch {
		case errors.Is(err, promptpreview.ErrNotFound):
			return WritePromptsPreviewStoryboardRef{}, ErrWritePromptsPreviewStoryboardNotFound
		case errors.Is(err, promptpreview.ErrStoryboardVersionConflict):
			return WritePromptsPreviewStoryboardRef{}, ErrWritePromptsPreviewStoryboardConflict
		default:
			return WritePromptsPreviewStoryboardRef{}, ErrWritePromptsPreviewStoryboardUnavailable
		}
	}
	authoritative := WritePromptsPreviewStoryboardRef{
		ID: generationContext.Storyboard.ID, Version: generationContext.Storyboard.Version,
		ContentDigest: generationContext.Storyboard.ContentDigest.Hex(),
	}
	if promptpreview.ValidateGenerationContext(generationContext) != nil || generationContext.Storyboard.UserID != request.UserID {
		return WritePromptsPreviewStoryboardRef{}, ErrWritePromptsPreviewStoryboardUnavailable
	}
	// Agent 投影必须完整等于 Business 权威 Draft；只比引用会让损坏或陈旧 Card 被误认为当前 Source。
	if authoritative != workspaceRef || generationContext.ProjectID != request.ProjectID ||
		generationContext.Storyboard.ProjectID != request.ProjectID ||
		!equalStoryboardPreviewContent(generationContext.Storyboard.Content, workspaceContent) {
		return WritePromptsPreviewStoryboardRef{}, ErrWritePromptsPreviewStoryboardConflict
	}
	return authoritative, nil
}

// writePromptsWorkspaceSnapshotWire 是 Binder 消费的 Workspace 安全子集。
type writePromptsWorkspaceSnapshotWire struct {
	// SchemaVersion 必须是统一的 session.workspace.v5。
	SchemaVersion string `json:"schema_version"`
	// RequestID 必须等于本次 workspace.read 断言标识。
	RequestID string `json:"request_id"`
	// Session 绑定内部断言的 Agent Session 与 Business Project。
	Session planStoryboardWorkspaceSessionWire `json:"session"`
	// PlanStoryboardPreview 是当前 Session 最新 Storyboard terminal Card。
	PlanStoryboardPreview json.RawMessage `json:"plan_storyboard_preview"`
	// WritePromptsPreview 是 v4 必须显式存在的 nullable Prompt Card，Binder 不用它选择 Source。
	WritePromptsPreview json.RawMessage `json:"write_prompts_preview"`
}

// writePromptsStoryboardCardWire 是 current Storyboard Card 的完整业务内容与精确引用。
type writePromptsStoryboardCardWire struct {
	// SchemaVersion 固定为 storyboard.preview.card.v1。
	SchemaVersion string `json:"schema_version"`
	// InputID 是产生 Card 的 typed Input UUIDv7。
	InputID string `json:"input_id"`
	// TurnID 是产生 Card 的 Turn UUIDv7。
	TurnID string `json:"turn_id"`
	// RunID 是产生 Card 的 Run UUIDv7。
	RunID string `json:"run_id"`
	// ToolCallID 是产生 Card 的 plan_storyboard ToolCall UUIDv7。
	ToolCallID string `json:"tool_call_id"`
	// Status 必须为 completed，failed Card 不可作 Prompt Source。
	Status string `json:"status"`
	// ResultCode 必须是冻结的 Storyboard Draft 成功码。
	ResultCode string `json:"result_code"`
	// UpdatedAt 是 Agent 投影冻结的 UTC 时间。
	UpdatedAt time.Time `json:"updated_at"`
	// StoryboardPreviewID 是 Business Storyboard Preview Draft UUIDv7。
	StoryboardPreviewID string `json:"storyboard_preview_id"`
	// ProjectID 是 Card 所属 Business Project UUIDv7。
	ProjectID string `json:"project_id"`
	// CreationSpecRef 是 Storyboard 生成时冻结的上游 Draft 引用。
	CreationSpecRef PlanStoryboardPreviewCreationSpecRef `json:"creation_spec_ref"`
	// Version 本 Preview 固定为一。
	Version int64 `json:"version"`
	// ContentDigest 是 Storyboard canonical 内容的小写 SHA-256。
	ContentDigest string `json:"content_digest"`
	// Title 是 Storyboard 标题。
	Title string `json:"title"`
	// Summary 是 Storyboard 摘要。
	Summary string `json:"summary"`
	// Sections 是冻结顺序的章节集合。
	Sections []storyboardpreview.Section `json:"sections"`
	// Elements 是冻结顺序的元素集合。
	Elements []storyboardpreview.Element `json:"elements"`
	// Slots 是 Prompt Graph 必须全部消费的媒体槽集合。
	Slots []storyboardpreview.Slot `json:"slots"`
}

// loadCurrentWorkspaceStoryboard 用专用 workspace.read 断言读取当前 Session Card，并拒绝无界、重复或漂移协议。
func (binder *writePromptsPreviewCurrentBinder) loadCurrentWorkspaceStoryboard(
	ctx context.Context,
	request WritePromptsPreviewStoryboardBindingRequest,
	resolved auth.ResolvedSession,
) (WritePromptsPreviewStoryboardRef, storyboardpreview.Content, error) {
	target := "/api/v1/agent/sessions/" + request.AgentSessionID + "/workspace"
	assertion, err := binder.base.signer.Sign(agentidentity.Identity{
		RequestID: request.RequestID, CanonicalTarget: target, Method: http.MethodGet,
		PrincipalUserID: request.UserID, WebSessionID: resolved.WebSessionID,
		WebSessionVersion: resolved.WebSessionVersion, ProjectID: request.ProjectID,
		AgentSessionID: request.AgentSessionID, Scope: agentidentity.ScopeWorkspaceRead,
	})
	if err != nil {
		return WritePromptsPreviewStoryboardRef{}, storyboardpreview.Content{}, ErrWritePromptsPreviewStoryboardUnavailable
	}
	upstream, err := http.NewRequestWithContext(ctx, http.MethodGet, binder.base.baseURL.String()+target, nil)
	if err != nil {
		return WritePromptsPreviewStoryboardRef{}, storyboardpreview.Content{}, ErrWritePromptsPreviewStoryboardUnavailable
	}
	upstream.Header.Set("Accept", "application/json")
	upstream.Header.Set(agentidentity.HeaderAssertion, assertion.EncodedCanonical)
	upstream.Header.Set(agentidentity.HeaderKeyVersion, assertion.KeyVersion)
	upstream.Header.Set(agentidentity.HeaderSignature, assertion.Signature)
	requestContext, cancel := context.WithTimeout(ctx, binder.base.requestTimeout)
	defer cancel()
	response, err := binder.base.client.Do(upstream.WithContext(requestContext))
	if err != nil || response == nil || response.Body == nil {
		return WritePromptsPreviewStoryboardRef{}, storyboardpreview.Content{}, ErrWritePromptsPreviewStoryboardUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return WritePromptsPreviewStoryboardRef{}, storyboardpreview.Content{}, ErrWritePromptsPreviewStoryboardUnavailable
	}
	contentTypes := response.Header.Values("Content-Type")
	if len(contentTypes) != 1 {
		return WritePromptsPreviewStoryboardRef{}, storyboardpreview.Content{}, ErrWritePromptsPreviewStoryboardUnavailable
	}
	mediaType, _, err := mime.ParseMediaType(contentTypes[0])
	if err != nil || mediaType != "application/json" {
		return WritePromptsPreviewStoryboardRef{}, storyboardpreview.Content{}, ErrWritePromptsPreviewStoryboardUnavailable
	}
	body, err := readBoundedBody(response.Body, maximumWorkspaceResponseBytes)
	if err != nil || !json.Valid(body) {
		return WritePromptsPreviewStoryboardRef{}, storyboardpreview.Content{}, ErrWritePromptsPreviewStoryboardUnavailable
	}
	duplicate, err := hasDuplicateJSONKey(body)
	if err != nil || duplicate {
		return WritePromptsPreviewStoryboardRef{}, storyboardpreview.Content{}, ErrWritePromptsPreviewStoryboardUnavailable
	}
	var snapshot writePromptsWorkspaceSnapshotWire
	if err := json.Unmarshal(body, &snapshot); err != nil || snapshot.SchemaVersion != writePromptsWorkspaceSchema ||
		snapshot.RequestID != request.RequestID || snapshot.Session.ID != request.AgentSessionID ||
		snapshot.Session.ProjectID != request.ProjectID || snapshot.Session.Status != "active" || snapshot.Session.Version < 1 ||
		len(snapshot.PlanStoryboardPreview) == 0 || len(snapshot.WritePromptsPreview) == 0 {
		return WritePromptsPreviewStoryboardRef{}, storyboardpreview.Content{}, ErrWritePromptsPreviewStoryboardUnavailable
	}
	if bytes.Equal(bytes.TrimSpace(snapshot.PlanStoryboardPreview), []byte("null")) {
		return WritePromptsPreviewStoryboardRef{}, storyboardpreview.Content{}, ErrWritePromptsPreviewStoryboardNotFound
	}
	var card writePromptsStoryboardCardWire
	decoder := json.NewDecoder(bytes.NewReader(snapshot.PlanStoryboardPreview))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&card); err != nil || ensureJSONEOF(decoder) != nil ||
		card.SchemaVersion != writePromptsStoryboardCardSchema || card.Status != "completed" ||
		card.ResultCode != writePromptsStoryboardCompletedCode || card.ProjectID != request.ProjectID ||
		card.Version != storyboardpreview.InitialDraftVersion || card.UpdatedAt.IsZero() || card.UpdatedAt.Location() != time.UTC ||
		!canonicalUUIDv7(card.InputID) || !canonicalUUIDv7(card.TurnID) || !canonicalUUIDv7(card.RunID) ||
		!canonicalUUIDv7(card.ToolCallID) || !validPlanStoryboardPreviewCreationSpecRef(card.CreationSpecRef) {
		return WritePromptsPreviewStoryboardRef{}, storyboardpreview.Content{}, ErrWritePromptsPreviewStoryboardUnavailable
	}
	ref := WritePromptsPreviewStoryboardRef{
		ID: card.StoryboardPreviewID, Version: card.Version, ContentDigest: card.ContentDigest,
	}
	content := storyboardpreview.Content{
		Title: card.Title, Summary: card.Summary, Sections: card.Sections, Elements: card.Elements, Slots: card.Slots,
	}
	digest, digestErr := storyboardpreview.ContentDigest(content)
	if !validWritePromptsPreviewStoryboardRef(ref) || digestErr != nil || digest.Hex() != ref.ContentDigest {
		return WritePromptsPreviewStoryboardRef{}, storyboardpreview.Content{}, ErrWritePromptsPreviewStoryboardUnavailable
	}
	return ref, content, nil
}

// equalStoryboardPreviewContent 用冻结 Canonical JSON 比较 Agent Card 与 Business Draft，避免 slice nil 表示差异。
func equalStoryboardPreviewContent(left storyboardpreview.Content, right storyboardpreview.Content) bool {
	leftJSON, leftErr := left.CanonicalJSON()
	rightJSON, rightErr := right.CanonicalJSON()
	return leftErr == nil && rightErr == nil && bytes.Equal(leftJSON, rightJSON)
}
