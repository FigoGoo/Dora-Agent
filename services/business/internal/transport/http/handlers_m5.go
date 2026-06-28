package http

import (
	"github.com/FigoGoo/Dora-Agent/kitex_gen/dora/api/businessagent"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/notification"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/work"
	bizerrors "github.com/FigoGoo/Dora-Agent/services/business/internal/pkg/errors"
	"github.com/gin-gonic/gin"
)

func registerM5Routes(router *gin.Engine, opts RouterOptions) {
	auth := m2Handler{account: opts.AccountSpace, admin: opts.Admin, project: opts.Project}
	h := m5Handler{work: opts.Work, notification: opts.Notification, auth: auth}

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

type m5Handler struct {
	work         *work.App
	notification *notification.App
	auth         m2Handler
}

func (h m5Handler) publicHome(c *gin.Context) {
	if h.work == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	out, err := h.work.GetHomePublicContent(c.Request.Context())
	respond(c, out, err)
}

func (h m5Handler) listPublicWorks(c *gin.Context) {
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

func (h m5Handler) getPublicWork(c *gin.Context) {
	if h.work == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	out, err := h.work.GetPublicWork(c.Request.Context(), work.GetPublicWorkInput{PublicWorkID: c.Param("public_work_id")})
	respond(c, out, err)
}

func (h m5Handler) likePublicWork(c *gin.Context) {
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

func (h m5Handler) unlikePublicWork(c *gin.Context) {
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

func (h m5Handler) listWorks(c *gin.Context) {
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

func (h m5Handler) createWork(c *gin.Context) {
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

func (h m5Handler) getWork(c *gin.Context) {
	if h.work == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	out, err := h.work.GetWorkDetail(c.Request.Context(), userAuth(c), c.Param("work_id"))
	respond(c, out, err)
}

func (h m5Handler) updateWork(c *gin.Context) {
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

func (h m5Handler) previewShareWork(c *gin.Context) {
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

func (h m5Handler) confirmShareWork(c *gin.Context) {
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

func (h m5Handler) unshareWork(c *gin.Context) {
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

func (h m5Handler) adminListPublicWorks(c *gin.Context) {
	if h.work == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	out, err := h.work.ListAdminPublicWorks(c.Request.Context(), intQuery(c, "limit", intQuery(c, "page_size", 10)), intQuery(c, "offset", 0))
	respond(c, out, err)
}

func (h m5Handler) adminPreviewTakeDownWork(c *gin.Context) {
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

func (h m5Handler) adminConfirmTakeDownWork(c *gin.Context) {
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

func (h m5Handler) listNotifications(c *gin.Context) {
	if h.notification == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	out, err := h.notification.ListNotifications(c.Request.Context(), userAuth(c), notification.ListInput{
		ReadStatus: c.Query("read_status"), Type: c.Query("type"), Limit: intQuery(c, "limit", intQuery(c, "page_size", 10)), Offset: intQuery(c, "offset", 0),
	})
	respond(c, out, err)
}

func (h m5Handler) unreadCount(c *gin.Context) {
	if h.notification == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	out, err := h.notification.GetUnreadCount(c.Request.Context(), userAuth(c))
	respond(c, out, err)
}

func (h m5Handler) markNotificationRead(c *gin.Context) {
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

func (h m5Handler) markAllNotificationsRead(c *gin.Context) {
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

func (h m5Handler) notificationNavigation(c *gin.Context) {
	if h.notification == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	out, err := h.notification.GetNotificationNavigation(c.Request.Context(), userAuth(c), c.Param("notification_id"))
	respond(c, out, err)
}
