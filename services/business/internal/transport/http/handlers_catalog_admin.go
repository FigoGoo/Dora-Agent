package http

import (
	"strings"

	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/assetdict"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/modelconfig"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/skillcatalog"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/toolpolicy"
	bizerrors "github.com/FigoGoo/Dora-Agent/services/business/internal/pkg/errors"
	"github.com/gin-gonic/gin"
)

func registerM3Routes(router *gin.Engine, opts RouterOptions) {
	auth := m2Handler{account: opts.AccountSpace, admin: opts.Admin, project: opts.Project}
	h := m3Handler{model: opts.Model, tool: opts.Tool, skill: opts.Skill, dictionary: opts.Dictionary}

	router.GET("/api/models/generation", auth.userAuth(), h.listGenerationModels)
	router.GET("/api/tools/bindable", auth.userAuth(), h.listBindableTools)
	router.GET("/api/skills", auth.userAuth(), h.listSkills)
	router.POST("/api/skills", auth.userAuth(), requireIdempotency(), h.createSkill)
	router.GET("/api/skills/:skill_id", auth.userAuth(), h.getSkill)
	router.PATCH("/api/skills/:skill_id", auth.userAuth(), requireIdempotency(), h.updateSkill)
	router.POST("/api/skills/:skill_id/test", auth.userAuth(), requireIdempotency(), h.submitSkillTest)
	router.POST("/api/skills/:skill_id/submit-review", auth.userAuth(), requireIdempotency(), h.submitSkillReview)
	router.POST("/api/skills/:skill_id/rollback", auth.userAuth(), requireIdempotency(), h.rollbackSkill)
	router.GET("/api/asset-element-types", auth.userAuth(), h.listAssetElementTypes)

	router.GET("/api/admin/models/providers", auth.adminAuth(false), h.adminListProviders)
	router.POST("/api/admin/models/providers", auth.adminAuth(false), requireIdempotency(), h.adminSaveProvider)
	router.PATCH("/api/admin/models/providers/:provider_id", auth.adminAuth(false), requireIdempotency(), h.adminPatchProvider)
	router.POST("/api/admin/models/providers/:provider_id/connectivity-test", auth.adminAuth(false), requireIdempotency(), h.adminConnectivityTest)
	router.GET("/api/admin/models", auth.adminAuth(false), h.adminListModels)
	router.POST("/api/admin/models", auth.adminAuth(false), requireIdempotency(), h.adminSaveModel)
	router.PATCH("/api/admin/models/:model_id", auth.adminAuth(false), requireIdempotency(), h.adminPatchModel)
	router.POST("/api/admin/models/default", auth.adminAuth(false), requireIdempotency(), h.adminSetDefaultModel)
	router.POST("/api/admin/models/:model_id/status", auth.adminAuth(false), requireIdempotency(), h.adminSetModelStatus)

	router.GET("/api/admin/tools", auth.adminAuth(false), h.adminListTools)
	router.POST("/api/admin/tools", auth.adminAuth(false), requireIdempotency(), h.adminRegisterTool)
	router.POST("/api/admin/tools/:tool_key/impact-preview", auth.adminAuth(false), h.adminToolImpactPreview)
	router.PATCH("/api/admin/tools/:tool_key/policy", auth.adminAuth(false), requireIdempotency(), h.adminUpdateToolPolicy)
	router.PATCH("/api/admin/tools/:tool_key/pricing-policy", auth.adminAuth(false), requireIdempotency(), h.adminUpdateToolPricing)
	router.POST("/api/admin/tools/:tool_key/status", auth.adminAuth(false), requireIdempotency(), h.adminSetToolStatus)
	router.PUT("/api/admin/tools/:tool_key/whitelist", auth.adminAuth(false), requireIdempotency(), h.adminSaveToolWhitelist)

	router.GET("/api/admin/skills/system", auth.adminAuth(false), h.adminListSystemSkills)
	router.POST("/api/admin/skills/system", auth.adminAuth(false), requireIdempotency(), h.adminCreateSystemSkill)
	router.POST("/api/admin/skills/system/:skill_id/test", auth.adminAuth(false), requireIdempotency(), h.adminSkillTest)
	router.POST("/api/admin/skills/system/:skill_id/publish", auth.adminAuth(false), requireIdempotency(), h.adminPublishSkill)
	router.POST("/api/admin/skills/system/:skill_id/deprecate", auth.adminAuth(false), requireIdempotency(), h.adminDeprecateSkill)
	router.GET("/api/admin/skills/reviews", auth.adminAuth(false), h.adminListSkillReviews)
	router.POST("/api/admin/skills/reviews/:review_id/confirm", auth.adminAuth(false), requireIdempotency(), h.adminConfirmSkillReview)

	router.GET("/api/admin/asset-element-types", auth.adminAuth(false), h.adminListAssetElementTypes)
}

type m3Handler struct {
	model      *modelconfig.App
	tool       *toolpolicy.App
	skill      *skillcatalog.App
	dictionary *assetdict.App
}

func (h m3Handler) listGenerationModels(c *gin.Context) {
	if h.model == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	models, next, err := h.model.ListAvailableGenerationModels(c.Request.Context(), userAuth(c), c.Query("resource_type"), intQuery(c, "limit", 10), c.Query("cursor"))
	respond(c, gin.H{"models": models, "next_cursor": next}, err)
}

func (h m3Handler) listBindableTools(c *gin.Context) {
	if h.tool == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	out, err := h.tool.ListBindableTools(c.Request.Context(), userAuth(c), intQuery(c, "limit", 10), intQuery(c, "offset", 0))
	respond(c, out, err)
}

func (h m3Handler) listSkills(c *gin.Context) {
	if h.skill == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	out, err := h.skill.ListSkills(c.Request.Context(), userAuth(c), c.Query("status"), intQuery(c, "limit", 10), intQuery(c, "offset", 0))
	respond(c, out, err)
}

func (h m3Handler) createSkill(c *gin.Context) {
	h.saveSkill(c, "")
}

func (h m3Handler) updateSkill(c *gin.Context) {
	h.saveSkill(c, c.Param("skill_id"))
}

func (h m3Handler) saveSkill(c *gin.Context, skillID string) {
	if h.skill == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	var req struct {
		SkillKey               string             `json:"skill_key"`
		SkillName              string             `json:"skill_name"`
		SkillScope             string             `json:"skill_scope"`
		RouteHints             map[string]string  `json:"route_hints"`
		Version                string             `json:"version"`
		SkillSpecJSON          string             `json:"skill_spec_json"`
		InputSchemaJSON        string             `json:"input_schema_json"`
		OutputSchemaJSON       string             `json:"output_schema_json"`
		MemoryPolicyJSON       string             `json:"memory_policy_json"`
		ConfirmationPolicyJSON string             `json:"confirmation_policy_json"`
		OutputElements         []outputElementReq `json:"output_elements"`
	}
	if !bindJSON(c, &req) {
		return
	}
	out, err := h.skill.SaveSkill(c.Request.Context(), skillcatalog.SaveSkillInput{
		Auth: userAuth(c), SkillID: skillID, SkillKey: req.SkillKey, SkillName: req.SkillName, SkillScope: req.SkillScope,
		RouteHints: req.RouteHints, Version: req.Version, SkillSpecJSON: req.SkillSpecJSON, InputSchemaJSON: req.InputSchemaJSON,
		OutputSchemaJSON: req.OutputSchemaJSON, MemoryPolicyJSON: req.MemoryPolicyJSON, ConfirmationPolicyJSON: req.ConfirmationPolicyJSON,
		OutputElements: outputElementInputs(req.OutputElements),
	})
	respond(c, out, err)
}

func (h m3Handler) getSkill(c *gin.Context) {
	out, err := h.skill.GetSkill(c.Request.Context(), userAuth(c), c.Param("skill_id"))
	respond(c, out, err)
}

func (h m3Handler) submitSkillTest(c *gin.Context) {
	var req struct {
		VersionID          string `json:"version_id"`
		TestRunID          string `json:"test_run_id"`
		TestCaseID         string `json:"test_case_id"`
		Status             string `json:"status"`
		ActualElementsJSON string `json:"actual_elements_json"`
		ErrorCode          string `json:"error_code"`
		ErrorSummary       string `json:"error_summary"`
		SafetyEvidenceJSON string `json:"safety_evidence_json"`
		AgentTraceID       string `json:"agent_trace_id"`
	}
	if !bindJSON(c, &req) {
		return
	}
	out, err := h.skill.SaveSkillTestResult(c.Request.Context(), userAuth(c), c.Param("skill_id"), req.VersionID, req.TestRunID, req.TestCaseID, c.GetHeader("Idempotency-Key"), req.Status, req.ActualElementsJSON, req.ErrorCode, req.ErrorSummary, req.SafetyEvidenceJSON, req.AgentTraceID)
	respond(c, out, err)
}

func (h m3Handler) submitSkillReview(c *gin.Context) {
	out, err := h.skill.SubmitReview(c.Request.Context(), userAuth(c), c.Param("skill_id"))
	respond(c, out, err)
}

func (h m3Handler) rollbackSkill(c *gin.Context) {
	out, err := h.skill.Rollback(c.Request.Context(), userAuth(c), c.Param("skill_id"))
	respond(c, out, err)
}

func (h m3Handler) listAssetElementTypes(c *gin.Context) {
	out, version, err := h.dictionary.ListAssetElementTypes(c.Request.Context(), userAuth(c), intQuery(c, "limit", 50), c.Query("schema_version"))
	respond(c, gin.H{"element_types": out, "schema_version": version}, err)
}

func (h m3Handler) adminListProviders(c *gin.Context) {
	out, err := h.model.ListProviders(c.Request.Context(), adminAuth(c), c.Query("status"), adminPageLimit(c, 10), adminPageOffset(c))
	respond(c, out, err)
}

func (h m3Handler) adminSaveProvider(c *gin.Context) {
	h.adminSaveProviderWithID(c, "")
}

func (h m3Handler) adminPatchProvider(c *gin.Context) {
	h.adminSaveProviderWithID(c, c.Param("provider_id"))
}

func (h m3Handler) adminSaveProviderWithID(c *gin.Context, providerID string) {
	var req struct {
		ProviderCode string         `json:"provider_code"`
		ProviderName string         `json:"provider_name"`
		DisplayName  string         `json:"display_name"`
		ProviderType string         `json:"provider_type"`
		Status       string         `json:"status"`
		SecretKeyRef string         `json:"secret_key_ref"`
		BaseURL      string         `json:"base_url"`
		Config       map[string]any `json:"config"`
	}
	if !bindJSON(c, &req) {
		return
	}
	if req.DisplayName == "" {
		req.DisplayName = req.ProviderName
	}
	if req.ProviderCode == "" && providerID == "" {
		req.ProviderCode = stableCode(req.DisplayName)
	}
	if strings.TrimSpace(req.SecretKeyRef) != "" {
		if req.Config == nil {
			req.Config = map[string]any{}
		}
		req.Config["secret_key_ref"] = strings.TrimSpace(req.SecretKeyRef)
	}
	out, err := h.model.SaveProvider(c.Request.Context(), modelconfig.SaveProviderInput{Auth: adminAuth(c), ProviderID: providerID, ProviderCode: req.ProviderCode, DisplayName: req.DisplayName, ProviderType: req.ProviderType, Status: req.Status, BaseURL: req.BaseURL, Config: req.Config})
	respond(c, out, err)
}

func (h m3Handler) adminConnectivityTest(c *gin.Context) {
	var req struct {
		ModelID string `json:"model_id"`
	}
	_ = c.ShouldBindJSON(&req)
	out, err := h.model.RecordConnectivityTest(c.Request.Context(), adminAuth(c), c.Param("provider_id"), req.ModelID)
	respond(c, out, err)
}

func (h m3Handler) adminListModels(c *gin.Context) {
	out, err := h.model.ListModels(c.Request.Context(), adminAuth(c), c.Query("provider_id"), c.Query("resource_type"), c.Query("status"), adminPageLimit(c, 10), adminPageOffset(c))
	respond(c, out, err)
}

func (h m3Handler) adminSaveModel(c *gin.Context) {
	h.adminSaveModelWithID(c, "")
}

func (h m3Handler) adminPatchModel(c *gin.Context) {
	h.adminSaveModelWithID(c, c.Param("model_id"))
}

func (h m3Handler) adminSaveModelWithID(c *gin.Context, modelID string) {
	var req struct {
		ProviderID        string         `json:"provider_id"`
		ModelCode         string         `json:"model_code"`
		DisplayName       string         `json:"display_name"`
		ResourceType      string         `json:"resource_type"`
		PricingSnapshotID string         `json:"pricing_snapshot_id"`
		BillingUnit       string         `json:"billing_unit"`
		UnitPoints        float64        `json:"unit_points"`
		MinChargePoints   int64          `json:"min_charge_points"`
		Status            string         `json:"status"`
		CapabilityTags    []string       `json:"capability_tags"`
		RouteConfig       map[string]any `json:"route_config"`
		CredentialID      string         `json:"credential_id"`
	}
	if !bindJSON(c, &req) {
		return
	}
	if req.ModelCode == "" {
		req.ModelCode = stableCode(req.DisplayName)
	}
	out, err := h.model.SaveModel(c.Request.Context(), modelconfig.SaveModelInput{
		Auth: adminAuth(c), ModelID: modelID, ProviderID: req.ProviderID, ModelCode: req.ModelCode,
		DisplayName: req.DisplayName, ResourceType: req.ResourceType, PricingSnapshotID: req.PricingSnapshotID,
		BillingUnit: req.BillingUnit, UnitPoints: req.UnitPoints, MinChargePoints: req.MinChargePoints,
		Status: req.Status, CapabilityTags: req.CapabilityTags, RouteConfig: req.RouteConfig, CredentialID: req.CredentialID,
	})
	respond(c, out, err)
}

func (h m3Handler) adminSetDefaultModel(c *gin.Context) {
	var req struct {
		ResourceType      string `json:"resource_type"`
		ModelID           string `json:"model_id"`
		PricingSnapshotID string `json:"pricing_snapshot_id"`
	}
	if !bindJSON(c, &req) {
		return
	}
	out, err := h.model.SetDefaultModel(c.Request.Context(), adminAuth(c), req.ResourceType, req.ModelID, req.PricingSnapshotID)
	respond(c, out, err)
}

func (h m3Handler) adminSetModelStatus(c *gin.Context) {
	var req struct {
		Status string `json:"status"`
	}
	if !bindJSON(c, &req) {
		return
	}
	out, err := h.model.SetModelStatus(c.Request.Context(), adminAuth(c), c.Param("model_id"), req.Status)
	respond(c, out, err)
}

func (h m3Handler) adminListTools(c *gin.Context) {
	out, err := h.tool.ListAdminTools(c.Request.Context(), adminAuth(c), c.Query("status"), adminPageLimit(c, 10), adminPageOffset(c))
	respond(c, out, err)
}

func (h m3Handler) adminRegisterTool(c *gin.Context) {
	var req struct {
		ToolName             string            `json:"tool_name"`
		ToolType             string            `json:"tool_type"`
		DisplayName          string            `json:"display_name"`
		Description          string            `json:"description"`
		Status               string            `json:"status"`
		Version              string            `json:"version"`
		InputSchemaJSON      string            `json:"input_schema_json"`
		OutputSchemaJSON     string            `json:"output_schema_json"`
		Allowed              *bool             `json:"allowed"`
		RiskLevel            string            `json:"risk_level"`
		RequiresConfirmation bool              `json:"requires_confirmation"`
		TimeoutMS            int32             `json:"timeout_ms"`
		RetryPolicy          map[string]string `json:"retry_policy"`
		CancelPolicy         map[string]string `json:"cancel_policy"`
		ChargeMode           string            `json:"charge_mode"`
		BillingUnit          string            `json:"billing_unit"`
		UnitPoints           float64           `json:"unit_points"`
		FreeQuota            int               `json:"free_quota"`
		MinChargePoints      int64             `json:"min_charge_points"`
		Reason               string            `json:"reason"`
		RequestHash          string            `json:"request_hash"`
	}
	if !bindJSON(c, &req) {
		return
	}
	allowed := true
	if req.Allowed != nil {
		allowed = *req.Allowed
	}
	out, err := h.tool.RegisterTool(c.Request.Context(), toolpolicy.RegisterToolInput{
		Auth: adminAuth(c), ToolName: req.ToolName, ToolType: req.ToolType, DisplayName: req.DisplayName,
		Description: req.Description, Status: req.Status, Version: req.Version, InputSchemaJSON: req.InputSchemaJSON,
		OutputSchemaJSON: req.OutputSchemaJSON, Allowed: allowed, RiskLevel: req.RiskLevel, RequiresConfirmation: req.RequiresConfirmation,
		TimeoutMS: req.TimeoutMS, RetryPolicy: req.RetryPolicy, CancelPolicy: req.CancelPolicy, ChargeMode: req.ChargeMode,
		BillingUnit: req.BillingUnit, UnitPoints: req.UnitPoints, FreeQuota: req.FreeQuota, MinChargePoints: req.MinChargePoints,
		Reason: req.Reason,
	})
	respond(c, out, err)
}

func (h m3Handler) adminToolImpactPreview(c *gin.Context) {
	toolName, toolType := parseToolKey(c.Param("tool_key"), c.Query("tool_type"))
	out, err := h.tool.ImpactPreview(c.Request.Context(), adminAuth(c), toolName, toolType)
	respond(c, out, err)
}

func (h m3Handler) adminUpdateToolPolicy(c *gin.Context) {
	var req struct {
		ToolType             string            `json:"tool_type"`
		Allowed              *bool             `json:"allowed"`
		RiskLevel            string            `json:"risk_level"`
		RequiresConfirmation *bool             `json:"requires_confirmation"`
		TimeoutMS            int32             `json:"timeout_ms"`
		RetryPolicy          map[string]string `json:"retry_policy"`
		CancelPolicy         map[string]string `json:"cancel_policy"`
	}
	if !bindJSON(c, &req) {
		return
	}
	toolName, toolType := parseToolKey(c.Param("tool_key"), req.ToolType)
	out, err := h.tool.UpdatePolicy(c.Request.Context(), adminAuth(c), toolName, toolType, req.Allowed, req.RiskLevel, req.RequiresConfirmation, req.TimeoutMS, req.RetryPolicy, req.CancelPolicy)
	respond(c, out, err)
}

func (h m3Handler) adminUpdateToolPricing(c *gin.Context) {
	var req struct {
		ToolType        string  `json:"tool_type"`
		ChargeMode      string  `json:"charge_mode"`
		BillingUnit     string  `json:"billing_unit"`
		UnitPoints      float64 `json:"unit_points"`
		FreeQuota       int     `json:"free_quota"`
		MinChargePoints int64   `json:"min_charge_points"`
	}
	if !bindJSON(c, &req) {
		return
	}
	toolName, toolType := parseToolKey(c.Param("tool_key"), req.ToolType)
	out, err := h.tool.UpdatePricing(c.Request.Context(), adminAuth(c), toolName, toolType, req.ChargeMode, req.BillingUnit, req.UnitPoints, req.FreeQuota, req.MinChargePoints)
	respond(c, out, err)
}

func (h m3Handler) adminSetToolStatus(c *gin.Context) {
	var req struct {
		ToolType string `json:"tool_type"`
		Status   string `json:"status"`
	}
	if !bindJSON(c, &req) {
		return
	}
	toolName, toolType := parseToolKey(c.Param("tool_key"), req.ToolType)
	out, err := h.tool.SetToolStatus(c.Request.Context(), adminAuth(c), toolName, toolType, req.Status)
	respond(c, out, err)
}

func (h m3Handler) adminSaveToolWhitelist(c *gin.Context) {
	var req struct {
		ToolType  string `json:"tool_type"`
		ScopeType string `json:"scope_type"`
		ScopeID   string `json:"scope_id"`
		Allowed   bool   `json:"allowed"`
		Reason    string `json:"reason"`
	}
	if !bindJSON(c, &req) {
		return
	}
	toolName, toolType := parseToolKey(c.Param("tool_key"), req.ToolType)
	out, err := h.tool.SaveWhitelist(c.Request.Context(), adminAuth(c), toolName, toolType, req.ScopeType, req.ScopeID, req.Allowed, req.Reason)
	respond(c, out, err)
}

func (h m3Handler) adminListSystemSkills(c *gin.Context) {
	out, err := h.skill.ListSystemSkills(c.Request.Context(), adminAuth(c), c.Query("status"), adminPageLimit(c, 10), adminPageOffset(c))
	respond(c, out, err)
}

func (h m3Handler) adminCreateSystemSkill(c *gin.Context) {
	var req struct {
		SkillKey               string             `json:"skill_key"`
		SkillName              string             `json:"skill_name"`
		SkillTags              []string           `json:"skill_tags"`
		RouteHints             map[string]string  `json:"route_hints"`
		Version                string             `json:"version"`
		SkillMarkdown          string             `json:"skill_markdown"`
		SkillSpecJSON          string             `json:"skill_spec_json"`
		InputSchemaJSON        string             `json:"input_schema_json"`
		OutputSchemaJSON       string             `json:"output_schema_json"`
		ToolRefs               []string           `json:"tool_refs"`
		MemoryPolicyJSON       string             `json:"memory_policy_json"`
		ConfirmationPolicyJSON string             `json:"confirmation_policy_json"`
		OutputElements         []outputElementReq `json:"output_elements"`
	}
	if !bindJSON(c, &req) {
		return
	}
	auth := userAuth(c)
	auth.UserID = adminAuth(c).AdminID
	out, err := h.skill.SaveSkill(c.Request.Context(), skillcatalog.SaveSkillInput{
		Auth: auth, SkillKey: req.SkillKey, SkillName: req.SkillName, SkillScope: "public",
		SkillTags: req.SkillTags, RouteHints: req.RouteHints, Version: req.Version, SkillMarkdown: req.SkillMarkdown,
		SkillSpecJSON: req.SkillSpecJSON, InputSchemaJSON: req.InputSchemaJSON, OutputSchemaJSON: req.OutputSchemaJSON,
		ToolRefs: req.ToolRefs, MemoryPolicyJSON: req.MemoryPolicyJSON, ConfirmationPolicyJSON: req.ConfirmationPolicyJSON,
		OutputElements: outputElementInputs(req.OutputElements),
	})
	respond(c, out, err)
}

func (h m3Handler) adminSkillTest(c *gin.Context) {
	var req struct {
		VersionID          string `json:"version_id"`
		TestRunID          string `json:"test_run_id"`
		TestCaseID         string `json:"test_case_id"`
		Status             string `json:"status"`
		ActualElementsJSON string `json:"actual_elements_json"`
		SafetyEvidenceJSON string `json:"safety_evidence_json"`
	}
	if !bindJSON(c, &req) {
		return
	}
	auth := userAuth(c)
	auth.UserID = adminAuth(c).AdminID
	out, err := h.skill.SaveSkillTestResult(c.Request.Context(), auth, c.Param("skill_id"), req.VersionID, req.TestRunID, req.TestCaseID, c.GetHeader("Idempotency-Key"), req.Status, req.ActualElementsJSON, "", "", req.SafetyEvidenceJSON, loggerTrace(c))
	respond(c, out, err)
}

func (h m3Handler) adminPublishSkill(c *gin.Context) {
	var req struct {
		VersionID string `json:"version_id"`
	}
	if !bindJSON(c, &req) {
		return
	}
	out, err := h.skill.Publish(c.Request.Context(), adminAuth(c), c.Param("skill_id"), req.VersionID)
	respond(c, out, err)
}

func (h m3Handler) adminDeprecateSkill(c *gin.Context) {
	out, err := h.skill.Deprecate(c.Request.Context(), adminAuth(c), c.Param("skill_id"))
	respond(c, out, err)
}

func (h m3Handler) adminListSkillReviews(c *gin.Context) {
	out, err := h.skill.ListReviews(c.Request.Context(), adminAuth(c), adminPageLimit(c, 10), adminPageOffset(c))
	respond(c, out, err)
}

func (h m3Handler) adminConfirmSkillReview(c *gin.Context) {
	var req adminSkillReviewRequest
	if !bindJSON(c, &req) {
		return
	}
	out, err := h.skill.ConfirmReview(c.Request.Context(), adminAuth(c), c.Param("review_id"), req.normalizedAction(), req.normalizedComment())
	respond(c, out, err)
}

type adminSkillReviewRequest struct {
	Action   string `json:"action"`
	Comment  string `json:"comment"`
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
}

type outputElementReq struct {
	ElementType  string `json:"element_type"`
	ElementName  string `json:"element_name"`
	Required     bool   `json:"required"`
	UseDraft     bool   `json:"use_draft"`
	UseFinal     bool   `json:"use_final"`
	Editable     bool   `json:"editable"`
	Referable    bool   `json:"referable"`
	DisplayOrder int32  `json:"display_order"`
	DisplaySlot  string `json:"display_slot"`
	SchemaJSON   string `json:"schema_json"`
}

func outputElementInputs(items []outputElementReq) []skillcatalog.OutputElementInput {
	if len(items) == 0 {
		return nil
	}
	out := make([]skillcatalog.OutputElementInput, 0, len(items))
	for _, item := range items {
		out = append(out, skillcatalog.OutputElementInput{
			ElementType: item.ElementType, ElementName: item.ElementName, Required: item.Required,
			UseDraft: item.UseDraft, UseFinal: item.UseFinal, Editable: item.Editable, Referable: item.Referable,
			DisplayOrder: item.DisplayOrder, DisplaySlot: item.DisplaySlot, SchemaJSON: item.SchemaJSON,
		})
	}
	return out
}

func (r adminSkillReviewRequest) normalizedAction() string {
	if strings.TrimSpace(r.Action) != "" {
		return strings.TrimSpace(r.Action)
	}
	return strings.TrimSpace(r.Decision)
}

func (r adminSkillReviewRequest) normalizedComment() string {
	if strings.TrimSpace(r.Comment) != "" {
		return strings.TrimSpace(r.Comment)
	}
	return strings.TrimSpace(r.Reason)
}

func (h m3Handler) adminListAssetElementTypes(c *gin.Context) {
	out, err := h.dictionary.ListAdminElementTypes(c.Request.Context(), adminAuth(c), c.Query("status"), adminPageLimit(c, 20), adminPageOffset(c))
	respond(c, out, err)
}

func (h m3Handler) adminSaveAssetElementType(c *gin.Context) {
	h.adminSaveAssetElementTypeWithKey(c, "")
}

func (h m3Handler) adminPatchAssetElementType(c *gin.Context) {
	h.adminSaveAssetElementTypeWithKey(c, c.Param("element_type"))
}

func (h m3Handler) adminSaveAssetElementTypeWithKey(c *gin.Context, elementType string) {
	var req struct {
		ElementType   string `json:"element_type"`
		DisplayName   string `json:"display_name"`
		SchemaVersion string `json:"schema_version"`
		SchemaJSON    string `json:"schema_json"`
		Status        string `json:"status"`
	}
	if !bindJSON(c, &req) {
		return
	}
	if req.ElementType == "" {
		req.ElementType = elementType
	}
	out, err := h.dictionary.SaveElementType(c.Request.Context(), assetdict.SaveInput{Auth: adminAuth(c), ElementType: req.ElementType, DisplayName: req.DisplayName, SchemaVersion: req.SchemaVersion, SchemaJSON: req.SchemaJSON, Status: req.Status})
	respond(c, out, err)
}

func requireIdempotency() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.GetHeader("Idempotency-Key") == "" {
			_ = c.Error(bizerrors.New(bizerrors.CodeInvalidArgument, "Idempotency-Key is required"))
			c.Abort()
			return
		}
		c.Next()
	}
}

func bindJSON(c *gin.Context, out any) bool {
	if err := c.ShouldBindJSON(out); err != nil {
		_ = c.Error(bizerrors.New(bizerrors.CodeInvalidArgument, "invalid json request"))
		return false
	}
	return true
}

func parseToolKey(toolKey, fallbackType string) (string, string) {
	if strings.Contains(toolKey, ":") {
		parts := strings.SplitN(toolKey, ":", 2)
		return parts[0], parts[1]
	}
	return toolKey, fallbackType
}

func stableCode(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var builder strings.Builder
	for _, ch := range value {
		switch {
		case ch >= 'a' && ch <= 'z', ch >= '0' && ch <= '9':
			builder.WriteRune(ch)
		case ch == '-' || ch == '_' || ch == '.':
			builder.WriteRune(ch)
		case builder.Len() > 0:
			builder.WriteByte('_')
		}
	}
	code := strings.Trim(builder.String(), "_")
	if code == "" {
		return "default"
	}
	return code
}

func loggerTrace(c *gin.Context) string {
	traceID := c.GetHeader("X-Trace-Id")
	if traceID == "" {
		traceID = c.GetHeader("X-Request-Id")
	}
	return traceID
}
