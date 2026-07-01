package http

import (
	"github.com/FigoGoo/Dora-Agent/kitex_gen/dora/api/businessagent"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/marketplace"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/notification"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/work"
	bizerrors "github.com/FigoGoo/Dora-Agent/services/business/internal/pkg/errors"
	"github.com/gin-gonic/gin"
)

func registerWorkNotificationMarketplaceRoutes(router *gin.Engine, opts RouterOptions) {
	auth := accountProjectAdminHandler{account: opts.AccountSpace, admin: opts.Admin, project: opts.Project}
	h := workNotificationMarketplaceHandler{work: opts.Work, notification: opts.Notification, marketplace: opts.Marketplace, auth: auth}

	router.GET("/api/marketplace/skills", auth.userAuth(), h.listMarketplaceSkills)
	router.GET("/api/marketplace/skills/:listing_id", auth.userAuth(), h.getMarketplaceSkill)
	router.POST("/api/marketplace/installations", auth.userAuth(), requireIdempotency(), h.installMarketplaceSkill)
	router.POST("/api/marketplace/installations/:installation_id/upgrade", auth.userAuth(), requireIdempotency(), h.upgradeSkillInstallation)
	router.GET("/api/marketplace/my-skills", auth.userAuth(), h.listInstalledSkills)

	router.POST("/api/creator/skills", auth.userAuth(), requireIdempotency(), h.createCreatorSkillDraft)
	router.POST("/api/creator/skills/:skill_id/versions/:version/submit", auth.userAuth(), requireIdempotency(), h.submitCreatorSkillVersion)
	router.GET("/api/creator/listings", auth.userAuth(), h.listCreatorListings)
	router.GET("/api/creator/analytics/skill-usage", auth.userAuth(), h.creatorSkillUsageAnalytics)

	router.GET("/api/admin/marketplace/skill-reviews", auth.adminAuth(false), h.adminListSkillReviews)
	router.GET("/api/admin/marketplace/listings", auth.adminAuth(false), h.adminListMarketplaceListings)
	router.GET("/api/admin/marketplace/refund-cases", auth.adminAuth(false), h.adminListRefundCases)
	router.GET("/api/admin/marketplace/settlements", auth.adminAuth(false), h.adminListSettlements)
	router.POST("/api/admin/skill-reviews/:review_id/approve", auth.adminAuth(false), requireIdempotency(), h.adminApproveSkillReview)
	router.POST("/api/admin/listings/:listing_id/suspend", auth.adminAuth(false), requireIdempotency(), h.adminSuspendMarketplaceListing)
	router.POST("/api/admin/refund-cases/:refund_case_id/approve", auth.adminAuth(false), requireIdempotency(), h.adminApproveSkillUsageRefund)
	router.POST("/api/admin/settlements/:settlement_id/release-hold", auth.adminAuth(false), requireIdempotency(), h.adminReleaseSkillSettlementHold)
	router.POST("/api/admin/settlements/:settlement_id/confirm-payout", auth.adminAuth(false), requireIdempotency(), h.adminConfirmSkillSettlementPayout)

	router.GET("/api/public/home", h.publicHome)
	router.GET("/api/public/works", h.listPublicWorks)
	router.GET("/api/public/works/:public_work_id", h.getPublicWork)
	router.POST("/api/public/works/:public_work_id/like", auth.userAuth(), requireIdempotency(), h.likePublicWork)
	router.POST("/api/public/works/:public_work_id/unlike", auth.userAuth(), requireIdempotency(), h.unlikePublicWork)

	router.GET("/api/works", auth.userAuth(), h.listWorks)
	router.POST("/api/works", auth.userAuth(), requireIdempotency(), h.createWork)
	router.GET("/api/works/:work_id", auth.userAuth(), h.getWork)
	router.PATCH("/api/works/:work_id", auth.userAuth(), requireIdempotency(), h.updateWork)
	router.POST("/api/works/:work_id/share/preview", auth.userAuth(), h.previewShareWork)
	router.POST("/api/works/:work_id/share/confirm", auth.userAuth(), requireIdempotency(), h.confirmShareWork)
	router.POST("/api/works/:work_id/unshare", auth.userAuth(), requireIdempotency(), h.unshareWork)

	router.GET("/api/admin/works/public", auth.adminAuth(false), h.adminListPublicWorks)
	router.POST("/api/admin/works/public/:public_work_id/take-down/preview", auth.adminAuth(false), h.adminPreviewTakeDownWork)
	router.POST("/api/admin/works/public/:public_work_id/take-down/confirm", auth.adminAuth(false), requireIdempotency(), h.adminConfirmTakeDownWork)

	router.GET("/api/notifications", auth.userAuth(), h.listNotifications)
	router.GET("/api/notifications/unread-count", auth.userAuth(), h.unreadCount)
	router.POST("/api/notifications/:notification_id/read", auth.userAuth(), requireIdempotency(), h.markNotificationRead)
	router.POST("/api/notifications/read-all", auth.userAuth(), requireIdempotency(), h.markAllNotificationsRead)
	router.GET("/api/notifications/:notification_id/navigation", auth.userAuth(), h.notificationNavigation)
}

type workNotificationMarketplaceHandler struct {
	work         *work.App
	notification *notification.App
	marketplace  *marketplace.App
	auth         accountProjectAdminHandler
}

func (h workNotificationMarketplaceHandler) listMarketplaceSkills(c *gin.Context) {
	if h.marketplace == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	out, err := h.marketplace.ListMarketplaceSkills(c.Request.Context(), marketplace.ListMarketplaceSkillsInput{
		Auth: userAuth(c), Query: c.Query("query"), Limit: intQuery(c, "limit", intQuery(c, "page_size", 10)), Cursor: c.Query("cursor"),
	})
	respond(c, out, err)
}

func (h workNotificationMarketplaceHandler) getMarketplaceSkill(c *gin.Context) {
	if h.marketplace == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	out, err := h.marketplace.GetMarketplaceSkill(c.Request.Context(), userAuth(c), c.Param("listing_id"))
	respond(c, out, err)
}

func (h workNotificationMarketplaceHandler) installMarketplaceSkill(c *gin.Context) {
	if h.marketplace == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	var req struct {
		ListingID    string `json:"listing_id"`
		TargetScope  string `json:"target_scope"`
		EnterpriseID string `json:"enterprise_id"`
		RequestHash  string `json:"request_hash"`
	}
	if !h.auth.bind(c, &req) {
		return
	}
	meta := h.auth.meta(c, true)
	if req.RequestHash != "" {
		meta.RequestHash = req.RequestHash
	}
	out, err := h.marketplace.InstallSkill(c.Request.Context(), marketplace.InstallSkillInput{
		Auth: userAuth(c), Meta: meta, ListingID: req.ListingID, TargetScope: req.TargetScope, EnterpriseID: req.EnterpriseID,
	})
	respond(c, out, err)
}

func (h workNotificationMarketplaceHandler) upgradeSkillInstallation(c *gin.Context) {
	if h.marketplace == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	var req struct {
		TargetVersion string `json:"target_version"`
		Confirmed     bool   `json:"confirmed"`
		RequestHash   string `json:"request_hash"`
	}
	if !h.auth.bind(c, &req) {
		return
	}
	meta := h.auth.meta(c, true)
	if req.RequestHash != "" {
		meta.RequestHash = req.RequestHash
	}
	out, err := h.marketplace.UpgradeSkillInstallation(c.Request.Context(), marketplace.UpgradeSkillInstallationInput{
		Auth: userAuth(c), Meta: meta, InstallationID: c.Param("installation_id"), TargetVersion: req.TargetVersion, Confirmed: req.Confirmed,
	})
	respond(c, out, err)
}

func (h workNotificationMarketplaceHandler) listInstalledSkills(c *gin.Context) {
	if h.marketplace == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	out, err := h.marketplace.ListInstalledSkills(c.Request.Context(), marketplace.ListInstalledSkillsInput{
		Auth: userAuth(c), AccountScope: c.Query("account_scope"), Limit: intQuery(c, "limit", intQuery(c, "page_size", 10)), Offset: intQuery(c, "offset", 0),
	})
	respond(c, out, err)
}

func (h workNotificationMarketplaceHandler) createCreatorSkillDraft(c *gin.Context) {
	if h.marketplace == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		RequestHash string `json:"request_hash"`
	}
	if !h.auth.bind(c, &req) {
		return
	}
	meta := h.auth.meta(c, true)
	if req.RequestHash != "" {
		meta.RequestHash = req.RequestHash
	}
	out, err := h.marketplace.CreateCreatorSkillDraft(c.Request.Context(), marketplace.CreateCreatorSkillDraftInput{
		Auth: userAuth(c), Meta: meta, Name: req.Name, Description: req.Description,
	})
	respond(c, out, err)
}

func (h workNotificationMarketplaceHandler) submitCreatorSkillVersion(c *gin.Context) {
	if h.marketplace == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	var req struct {
		RequestHash string `json:"request_hash"`
	}
	if !h.auth.bind(c, &req) {
		return
	}
	meta := h.auth.meta(c, true)
	if req.RequestHash != "" {
		meta.RequestHash = req.RequestHash
	}
	out, err := h.marketplace.SubmitCreatorSkillVersion(c.Request.Context(), marketplace.SubmitCreatorSkillVersionInput{
		Auth: userAuth(c), Meta: meta, SkillID: c.Param("skill_id"), Version: c.Param("version"),
	})
	respond(c, out, err)
}

func (h workNotificationMarketplaceHandler) listCreatorListings(c *gin.Context) {
	if h.marketplace == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	out, err := h.marketplace.ListCreatorListings(c.Request.Context(), marketplace.ListCreatorListingsInput{
		Auth: userAuth(c), Limit: intQuery(c, "limit", intQuery(c, "page_size", 10)),
	})
	respond(c, out, err)
}

func (h workNotificationMarketplaceHandler) creatorSkillUsageAnalytics(c *gin.Context) {
	if h.marketplace == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	out, err := h.marketplace.GetCreatorSkillUsageAnalytics(c.Request.Context(), userAuth(c))
	respond(c, out, err)
}

func (h workNotificationMarketplaceHandler) adminListSkillReviews(c *gin.Context) {
	if h.marketplace == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	out, err := h.marketplace.ListAdminSkillReviews(c.Request.Context(), marketplace.ListAdminSkillReviewsInput{
		AdminID: adminAuth(c).AdminID,
		Status:  c.Query("status"),
		Keyword: c.Query("keyword"),
		Limit:   adminPageLimit(c, 10),
		Offset:  adminPageOffset(c),
	})
	respond(c, out, err)
}

func (h workNotificationMarketplaceHandler) adminApproveSkillReview(c *gin.Context) {
	if h.marketplace == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	var req struct {
		Reason      string `json:"reason"`
		RequestHash string `json:"request_hash"`
	}
	if !h.auth.bind(c, &req) {
		return
	}
	out, err := h.marketplace.ApproveSkillReview(c.Request.Context(), marketplace.ApproveSkillReviewInput{
		AdminID:  adminAuth(c).AdminID,
		ReviewID: c.Param("review_id"),
		Reason:   req.Reason,
	})
	respond(c, out, err)
}

func (h workNotificationMarketplaceHandler) adminListMarketplaceListings(c *gin.Context) {
	if h.marketplace == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	out, err := h.marketplace.ListAdminMarketplaceListings(c.Request.Context(), marketplace.ListAdminMarketplaceListingsInput{
		AdminID: adminAuth(c).AdminID,
		Status:  c.Query("status"),
		Keyword: c.Query("keyword"),
		Limit:   adminPageLimit(c, 10),
		Offset:  adminPageOffset(c),
	})
	respond(c, out, err)
}

func (h workNotificationMarketplaceHandler) adminSuspendMarketplaceListing(c *gin.Context) {
	if h.marketplace == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	var req struct {
		ReasonCode  string `json:"reason_code"`
		RequestHash string `json:"request_hash"`
	}
	if !h.auth.bind(c, &req) {
		return
	}
	out, err := h.marketplace.SuspendMarketplaceListing(c.Request.Context(), marketplace.SuspendMarketplaceListingInput{
		AdminID:    adminAuth(c).AdminID,
		ListingID:  c.Param("listing_id"),
		ReasonCode: req.ReasonCode,
	})
	respond(c, out, err)
}

func (h workNotificationMarketplaceHandler) adminListRefundCases(c *gin.Context) {
	if h.marketplace == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	out, err := h.marketplace.ListAdminRefundCases(c.Request.Context(), marketplace.ListAdminRefundCasesInput{
		AdminID: adminAuth(c).AdminID,
		Status:  c.Query("status"),
		Limit:   adminPageLimit(c, 10),
		Offset:  adminPageOffset(c),
	})
	respond(c, out, err)
}

func (h workNotificationMarketplaceHandler) adminApproveSkillUsageRefund(c *gin.Context) {
	if h.marketplace == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	var req struct {
		RequestHash string `json:"request_hash"`
	}
	if !h.auth.bind(c, &req) {
		return
	}
	out, err := h.marketplace.ApproveSkillUsageRefund(c.Request.Context(), marketplace.ApproveSkillUsageRefundInput{
		AdminID:      adminAuth(c).AdminID,
		RefundCaseID: c.Param("refund_case_id"),
	})
	respond(c, out, err)
}

func (h workNotificationMarketplaceHandler) adminListSettlements(c *gin.Context) {
	if h.marketplace == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	out, err := h.marketplace.ListAdminSettlements(c.Request.Context(), marketplace.ListAdminSettlementsInput{
		AdminID: adminAuth(c).AdminID,
		Status:  c.Query("status"),
		Limit:   adminPageLimit(c, 10),
		Offset:  adminPageOffset(c),
	})
	respond(c, out, err)
}

func (h workNotificationMarketplaceHandler) adminReleaseSkillSettlementHold(c *gin.Context) {
	if h.marketplace == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	var req struct {
		ReasonCode  string `json:"reason_code"`
		RequestHash string `json:"request_hash"`
	}
	if !h.auth.bind(c, &req) {
		return
	}
	meta := h.auth.meta(c, true)
	if req.RequestHash != "" {
		meta.RequestHash = req.RequestHash
	}
	out, err := h.marketplace.ReleaseSkillSettlementHold(c.Request.Context(), marketplace.ReleaseSkillSettlementHoldInput{
		AdminID:      adminAuth(c).AdminID,
		Meta:         meta,
		SettlementID: c.Param("settlement_id"),
		ReasonCode:   req.ReasonCode,
	})
	respond(c, out, err)
}

func (h workNotificationMarketplaceHandler) adminConfirmSkillSettlementPayout(c *gin.Context) {
	if h.marketplace == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	var req struct {
		PayoutReference string `json:"payout_reference"`
		ReasonCode      string `json:"reason_code"`
		RequestHash     string `json:"request_hash"`
	}
	if !h.auth.bind(c, &req) {
		return
	}
	meta := h.auth.meta(c, true)
	if req.RequestHash != "" {
		meta.RequestHash = req.RequestHash
	}
	out, err := h.marketplace.ConfirmSkillSettlementPayout(c.Request.Context(), marketplace.ConfirmSkillSettlementPayoutInput{
		AdminID:         adminAuth(c).AdminID,
		Meta:            meta,
		SettlementID:    c.Param("settlement_id"),
		PayoutReference: req.PayoutReference,
		ReasonCode:      req.ReasonCode,
	})
	respond(c, out, err)
}

func (h workNotificationMarketplaceHandler) publicHome(c *gin.Context) {
	if h.work == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	out, err := h.work.GetHomePublicContent(c.Request.Context())
	respond(c, out, err)
}

func (h workNotificationMarketplaceHandler) listPublicWorks(c *gin.Context) {
	if h.work == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	out, err := h.work.ListPublicWorks(c.Request.Context(), work.ListPublicWorksInput{
		Category: c.Query("category"), Tag: c.Query("tag"), ResourceType: c.Query("resource_type"),
		Limit: intQuery(c, "limit", intQuery(c, "page_size", 10)), Offset: intQuery(c, "offset", 0),
	})
	respond(c, out, err)
}

func (h workNotificationMarketplaceHandler) getPublicWork(c *gin.Context) {
	if h.work == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	out, err := h.work.GetPublicWork(c.Request.Context(), work.GetPublicWorkInput{PublicWorkID: c.Param("public_work_id")})
	respond(c, out, err)
}

func (h workNotificationMarketplaceHandler) likePublicWork(c *gin.Context) {
	if h.work == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	var req struct {
		RequestHash string `json:"request_hash"`
	}
	if !h.auth.bind(c, &req) {
		return
	}
	meta := h.auth.meta(c, true)
	if req.RequestHash != "" {
		meta.RequestHash = req.RequestHash
	}
	out, err := h.work.LikePublicWork(c.Request.Context(), work.LikePublicWorkInput{Auth: userAuth(c), Meta: meta, PublicWorkID: c.Param("public_work_id")})
	respond(c, out, err)
}

func (h workNotificationMarketplaceHandler) unlikePublicWork(c *gin.Context) {
	if h.work == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	var req struct {
		RequestHash string `json:"request_hash"`
	}
	if !h.auth.bind(c, &req) {
		return
	}
	meta := h.auth.meta(c, true)
	if req.RequestHash != "" {
		meta.RequestHash = req.RequestHash
	}
	out, err := h.work.UnlikePublicWork(c.Request.Context(), work.LikePublicWorkInput{Auth: userAuth(c), Meta: meta, PublicWorkID: c.Param("public_work_id")})
	respond(c, out, err)
}

func (h workNotificationMarketplaceHandler) listWorks(c *gin.Context) {
	if h.work == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	out, err := h.work.ListMyWorks(c.Request.Context(), work.ListWorksInput{
		Auth: userAuth(c), ProjectID: c.Query("project_id"), ShareStatus: c.Query("share_status"), Category: c.Query("category"),
		Limit: intQuery(c, "limit", intQuery(c, "page_size", 10)), Offset: intQuery(c, "offset", 0),
	})
	respond(c, out, err)
}

func (h workNotificationMarketplaceHandler) createWork(c *gin.Context) {
	if h.work == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	var req struct {
		ProjectID    string   `json:"project_id"`
		Title        string   `json:"title"`
		Description  string   `json:"description"`
		AssetIDs     []string `json:"asset_ids"`
		CoverAssetID string   `json:"cover_asset_id"`
		Category     string   `json:"category"`
		Tags         []string `json:"tags"`
		RequestHash  string   `json:"request_hash"`
	}
	if !h.auth.bind(c, &req) {
		return
	}
	meta := h.auth.meta(c, true)
	if req.RequestHash != "" {
		meta.RequestHash = req.RequestHash
	}
	out, err := h.work.CreateWork(c.Request.Context(), work.CreateWorkInput{
		Auth: userAuth(c), Meta: meta, ProjectID: req.ProjectID, Title: req.Title, Description: req.Description,
		AssetIDs: req.AssetIDs, CoverAssetID: req.CoverAssetID, Category: req.Category, Tags: req.Tags,
	})
	respond(c, out, err)
}

func (h workNotificationMarketplaceHandler) getWork(c *gin.Context) {
	if h.work == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	out, err := h.work.GetWorkDetail(c.Request.Context(), userAuth(c), c.Param("work_id"))
	respond(c, out, err)
}

func (h workNotificationMarketplaceHandler) updateWork(c *gin.Context) {
	if h.work == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	var req struct {
		Title         *string  `json:"title"`
		Description   *string  `json:"description"`
		AssetIDs      []string `json:"asset_ids"`
		CoverAssetID  *string  `json:"cover_asset_id"`
		Category      *string  `json:"category"`
		Tags          []string `json:"tags"`
		BaseUpdatedAt string   `json:"base_updated_at"`
		RequestHash   string   `json:"request_hash"`
	}
	if !h.auth.bind(c, &req) {
		return
	}
	meta := h.auth.meta(c, true)
	if req.RequestHash != "" {
		meta.RequestHash = req.RequestHash
	}
	out, err := h.work.UpdateWork(c.Request.Context(), work.UpdateWorkInput{
		Auth: userAuth(c), Meta: meta, WorkID: c.Param("work_id"), Title: req.Title, Description: req.Description,
		AssetIDs: req.AssetIDs, CoverAssetID: req.CoverAssetID, Category: req.Category, Tags: req.Tags, BaseUpdatedAt: req.BaseUpdatedAt,
	})
	respond(c, out, err)
}

func (h workNotificationMarketplaceHandler) previewShareWork(c *gin.Context) {
	if h.work == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	var req struct {
		PublicTitle       string                           `json:"public_title"`
		PublicDescription string                           `json:"public_description"`
		Tags              []string                         `json:"tags"`
		SafetyEvidence    *businessagent.SafetyEvidenceDTO `json:"safety_evidence"`
	}
	if !h.auth.bind(c, &req) {
		return
	}
	out, err := h.work.PreviewShareWork(c.Request.Context(), work.PreviewShareWorkInput{
		Auth: userAuth(c), WorkID: c.Param("work_id"), PublicTitle: req.PublicTitle,
		PublicDescription: req.PublicDescription, Tags: req.Tags, SafetyEvidence: req.SafetyEvidence,
	})
	respond(c, out, err)
}

func (h workNotificationMarketplaceHandler) confirmShareWork(c *gin.Context) {
	if h.work == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	var req struct {
		PreviewToken string `json:"preview_token"`
	}
	if !h.auth.bind(c, &req) {
		return
	}
	meta := h.auth.meta(c, true)
	out, err := h.work.ConfirmShareWork(c.Request.Context(), work.ConfirmShareWorkInput{Auth: userAuth(c), Meta: meta, WorkID: c.Param("work_id"), PreviewToken: req.PreviewToken})
	respond(c, out, err)
}

func (h workNotificationMarketplaceHandler) unshareWork(c *gin.Context) {
	if h.work == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	var req struct {
		Reason      string `json:"reason"`
		RequestHash string `json:"request_hash"`
	}
	if !h.auth.bind(c, &req) {
		return
	}
	meta := h.auth.meta(c, true)
	if req.RequestHash != "" {
		meta.RequestHash = req.RequestHash
	}
	out, err := h.work.UnshareWork(c.Request.Context(), work.UnshareWorkInput{Auth: userAuth(c), Meta: meta, WorkID: c.Param("work_id"), Reason: req.Reason})
	respond(c, out, err)
}

func (h workNotificationMarketplaceHandler) adminListPublicWorks(c *gin.Context) {
	if h.work == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	out, err := h.work.ListAdminPublicWorks(c.Request.Context(), work.ListAdminPublicWorksInput{
		Keyword: c.Query("keyword"), Status: c.Query("status"), Category: c.Query("category"), Tag: c.Query("tag"),
		ResourceType: c.Query("resource_type"), Limit: adminPageLimit(c, 10), Offset: adminPageOffset(c),
	})
	respond(c, out, err)
}

func (h workNotificationMarketplaceHandler) adminPreviewTakeDownWork(c *gin.Context) {
	if h.work == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	var req struct {
		Reason       string `json:"reason"`
		NotifyAuthor bool   `json:"notify_author"`
	}
	if !h.auth.bind(c, &req) {
		return
	}
	out, err := h.work.PreviewTakeDownWork(c.Request.Context(), work.PreviewTakeDownWorkInput{
		Auth: adminAuth(c), PublicWorkID: c.Param("public_work_id"), Reason: req.Reason, NotifyAuthor: req.NotifyAuthor,
	})
	respond(c, out, err)
}

func (h workNotificationMarketplaceHandler) adminConfirmTakeDownWork(c *gin.Context) {
	if h.work == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	var req struct {
		PreviewToken string `json:"preview_token"`
		Reason       string `json:"reason"`
		NotifyAuthor bool   `json:"notify_author"`
	}
	if !h.auth.bind(c, &req) {
		return
	}
	meta := h.auth.meta(c, true)
	out, err := h.work.ConfirmTakeDownWork(c.Request.Context(), work.ConfirmTakeDownWorkInput{
		Auth: adminAuth(c), Meta: meta, PublicWorkID: c.Param("public_work_id"),
		PreviewToken: req.PreviewToken, Reason: req.Reason, NotifyAuthor: req.NotifyAuthor,
	})
	respond(c, out, err)
}

func (h workNotificationMarketplaceHandler) listNotifications(c *gin.Context) {
	if h.notification == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	out, err := h.notification.ListNotifications(c.Request.Context(), userAuth(c), notification.ListInput{
		ReadStatus: c.Query("read_status"), Type: c.Query("type"), Limit: intQuery(c, "limit", intQuery(c, "page_size", 10)), Offset: intQuery(c, "offset", 0),
	})
	respond(c, out, err)
}

func (h workNotificationMarketplaceHandler) unreadCount(c *gin.Context) {
	if h.notification == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	out, err := h.notification.GetUnreadCount(c.Request.Context(), userAuth(c))
	respond(c, out, err)
}

func (h workNotificationMarketplaceHandler) markNotificationRead(c *gin.Context) {
	if h.notification == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	var req struct {
		RequestHash string `json:"request_hash"`
	}
	if !h.auth.bind(c, &req) {
		return
	}
	meta := h.auth.meta(c, true)
	if req.RequestHash != "" {
		meta.RequestHash = req.RequestHash
	}
	out, err := h.notification.MarkNotificationRead(c.Request.Context(), userAuth(c), meta, c.Param("notification_id"))
	respond(c, out, err)
}

func (h workNotificationMarketplaceHandler) markAllNotificationsRead(c *gin.Context) {
	if h.notification == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	var req struct {
		Type        string `json:"type"`
		RequestHash string `json:"request_hash"`
	}
	if !h.auth.bind(c, &req) {
		return
	}
	meta := h.auth.meta(c, true)
	if req.RequestHash != "" {
		meta.RequestHash = req.RequestHash
	}
	out, err := h.notification.MarkAllNotificationsRead(c.Request.Context(), userAuth(c), meta, req.Type)
	respond(c, out, err)
}

func (h workNotificationMarketplaceHandler) notificationNavigation(c *gin.Context) {
	if h.notification == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	out, err := h.notification.GetNotificationNavigation(c.Request.Context(), userAuth(c), c.Param("notification_id"))
	respond(c, out, err)
}
