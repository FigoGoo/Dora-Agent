package http

import (
	"strings"
	"time"

	"github.com/FigoGoo/Dora-Agent/kitex_gen/dora/api/businessagent"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/asset"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/credit"
	bizerrors "github.com/FigoGoo/Dora-Agent/services/business/internal/pkg/errors"
	"github.com/gin-gonic/gin"
)

func registerM4Routes(router *gin.Engine, opts RouterOptions) {
	auth := m2Handler{account: opts.AccountSpace, admin: opts.Admin, project: opts.Project}
	h := m4Handler{credit: opts.Credit, asset: opts.Asset, auth: auth}

	router.GET("/api/credits/summary", auth.userAuth(), h.creditSummary)
	router.GET("/api/credits/ledger", auth.userAuth(), h.creditLedger)
	router.POST("/api/credits/redeem", auth.userAuth(), requireIdempotency(), h.redeemCode)
	router.GET("/api/enterprise/credits", auth.userAuth(), h.enterpriseCredits)
	router.GET("/api/enterprise/usage", auth.userAuth(), h.enterpriseUsage)

	router.GET("/api/assets", auth.userAuth(), h.listAssets)
	router.GET("/api/assets/:asset_id", auth.userAuth(), h.getAsset)
	router.POST("/api/assets/upload-intents", auth.userAuth(), requireIdempotency(), h.createUploadIntent)
	router.POST("/api/assets/upload-intents/:upload_intent_id/confirm", auth.userAuth(), requireIdempotency(), h.confirmUploadIntent)
	router.POST("/api/assets/upload-intents/:upload_intent_id/abort", auth.userAuth(), requireIdempotency(), h.abortUploadIntent)
	router.GET("/api/assets/:asset_id/access", auth.userAuth(), h.getAssetAccess)

	router.GET("/api/admin/credits/grants/targets", auth.adminAuth(false), h.searchCreditTargets)
	router.POST("/api/admin/credits/grants", auth.adminAuth(false), requireIdempotency(), h.adminGrantCredits)
	router.GET("/api/admin/credits/codes", auth.adminAuth(false), h.listRedeemCodes)
	router.POST("/api/admin/credits/codes", auth.adminAuth(false), requireIdempotency(), h.createRedeemCodes)
	router.POST("/api/admin/credits/codes/:batch_id/disable", auth.adminAuth(false), requireIdempotency(), h.disableRedeemCodeBatch)
	router.POST("/api/admin/credits/codes/:batch_id/export", auth.adminAuth(false), requireIdempotency(), h.exportRedeemCodes)
}

type m4Handler struct {
	credit *credit.App
	asset  *asset.App
	auth   m2Handler
}

func (h m4Handler) creditSummary(c *gin.Context) {
	if h.credit == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	out, err := h.credit.GetSummary(c.Request.Context(), userAuth(c))
	respond(c, out, err)
}

func (h m4Handler) creditLedger(c *gin.Context) {
	if h.credit == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	out, err := h.credit.ListLedger(c.Request.Context(), userAuth(c), intQuery(c, "limit", 10), intQuery(c, "offset", 0))
	respond(c, out, err)
}

func (h m4Handler) redeemCode(c *gin.Context) {
	if h.credit == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	var req struct {
		RedeemCode  string `json:"redeem_code"`
		RequestHash string `json:"request_hash"`
	}
	if !h.auth.bind(c, &req) {
		return
	}
	meta := h.auth.meta(c, true)
	if req.RequestHash != "" {
		meta.RequestHash = req.RequestHash
	}
	out, err := h.credit.RedeemCode(c.Request.Context(), credit.RedeemInput{Auth: userAuth(c), Meta: meta, Code: req.RedeemCode})
	respond(c, out, err)
}

func (h m4Handler) enterpriseCredits(c *gin.Context) {
	if h.credit == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	out, err := h.credit.GetEnterpriseSummary(c.Request.Context(), userAuth(c))
	respond(c, out, err)
}

func (h m4Handler) enterpriseUsage(c *gin.Context) {
	if h.credit == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	out, err := h.credit.ListEnterpriseUsage(c.Request.Context(), userAuth(c), intQuery(c, "limit", 10), intQuery(c, "offset", 0))
	respond(c, out, err)
}

func (h m4Handler) listAssets(c *gin.Context) {
	if h.asset == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	out, err := h.asset.ListAssets(c.Request.Context(), userAuth(c), c.Query("project_id"), c.Query("asset_type"), c.Query("status"), intQuery(c, "limit", 10), intQuery(c, "offset", 0))
	respond(c, out, err)
}

func (h m4Handler) getAsset(c *gin.Context) {
	if h.asset == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	out, err := h.asset.GetAsset(c.Request.Context(), userAuth(c), c.Param("asset_id"))
	respond(c, out, err)
}

func (h m4Handler) createUploadIntent(c *gin.Context) {
	if h.asset == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	var req struct {
		Filename       string                           `json:"filename"`
		ContentType    string                           `json:"content_type"`
		SizeBytes      int64                            `json:"size_bytes"`
		Checksum       string                           `json:"checksum"`
		ProjectID      string                           `json:"project_id"`
		AssetType      string                           `json:"asset_type"`
		MetadataText   string                           `json:"metadata_text"`
		SafetyEvidence *businessagent.SafetyEvidenceDTO `json:"safety_evidence"`
		RequestHash    string                           `json:"request_hash"`
	}
	if !h.auth.bind(c, &req) {
		return
	}
	meta := h.auth.meta(c, true)
	if req.RequestHash != "" {
		meta.RequestHash = req.RequestHash
	}
	out, err := h.asset.CreateUploadIntent(c.Request.Context(), asset.CreateUploadIntentInput{
		Auth: userAuth(c), Meta: meta, ProjectID: req.ProjectID, AssetType: assetType(req.AssetType, req.ContentType),
		Filename: req.Filename, ContentType: req.ContentType, SizeBytes: req.SizeBytes, Checksum: req.Checksum,
		MetadataText: req.MetadataText, SafetyEvidence: req.SafetyEvidence,
	})
	respond(c, out, err)
}

func (h m4Handler) confirmUploadIntent(c *gin.Context) {
	if h.asset == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	var req struct {
		ObjectKey   string `json:"object_key"`
		Checksum    string `json:"checksum"`
		Etag        string `json:"etag"`
		SizeBytes   int64  `json:"size_bytes"`
		ContentType string `json:"content_type"`
		RequestHash string `json:"request_hash"`
	}
	if !h.auth.bind(c, &req) {
		return
	}
	meta := h.auth.meta(c, true)
	if req.RequestHash != "" {
		meta.RequestHash = req.RequestHash
	}
	out, err := h.asset.ConfirmUploadIntent(c.Request.Context(), asset.ConfirmUploadInput{
		Auth: userAuth(c), Meta: meta, UploadIntentID: c.Param("upload_intent_id"),
		Etag: req.Etag, SizeBytes: req.SizeBytes, ContentType: req.ContentType, Checksum: req.Checksum,
	})
	respond(c, out, err)
}

func (h m4Handler) abortUploadIntent(c *gin.Context) {
	if h.asset == nil {
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
	out, err := h.asset.AbortUploadIntent(c.Request.Context(), userAuth(c), meta, c.Param("upload_intent_id"))
	respond(c, out, err)
}

func (h m4Handler) getAssetAccess(c *gin.Context) {
	if h.asset == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	out, err := h.asset.GetAssetAccess(c.Request.Context(), userAuth(c), c.Param("asset_id"), c.Query("access_type"))
	respond(c, out, err)
}

func (h m4Handler) searchCreditTargets(c *gin.Context) {
	if h.credit == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	out, err := h.credit.SearchCreditTargets(c.Request.Context(), adminAuth(c), c.Query("keyword"), c.Query("target_type"), intQuery(c, "limit", 10), intQuery(c, "offset", 0))
	respond(c, out, err)
}

func (h m4Handler) adminGrantCredits(c *gin.Context) {
	if h.credit == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	var req struct {
		TargetType  string `json:"target_type"`
		TargetID    string `json:"target_id"`
		Points      int64  `json:"points"`
		Reason      string `json:"reason"`
		ExpiresAt   string `json:"expires_at"`
		RequestHash string `json:"request_hash"`
	}
	if !h.auth.bind(c, &req) {
		return
	}
	expiresAt, err := parseFutureTime(req.ExpiresAt, 365*24*time.Hour)
	if err != nil {
		_ = c.Error(err)
		return
	}
	meta := h.auth.meta(c, true)
	if req.RequestHash != "" {
		meta.RequestHash = req.RequestHash
	}
	out, err := h.credit.AdminGrantCredits(c.Request.Context(), credit.AdminGrantInput{
		Auth: adminAuth(c), Meta: meta, TargetType: req.TargetType, TargetID: req.TargetID,
		Points: req.Points, ExpiresAt: expiresAt, Reason: req.Reason,
	})
	respond(c, out, err)
}

func (h m4Handler) listRedeemCodes(c *gin.Context) {
	if h.credit == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	out, err := h.credit.ListRedeemCodes(c.Request.Context(), adminAuth(c), intQuery(c, "limit", 10), intQuery(c, "offset", 0))
	respond(c, out, err)
}

func (h m4Handler) createRedeemCodes(c *gin.Context) {
	if h.credit == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	var req struct {
		PointsPerCode  int64  `json:"points_per_code"`
		CodeCount      int    `json:"code_count"`
		BindTargetType string `json:"bind_target_type"`
		BindTargetID   string `json:"bind_target_id"`
		RedeemChannel  string `json:"redeem_channel"`
		ExpiresAt      string `json:"expires_at"`
		RequestHash    string `json:"request_hash"`
	}
	if !h.auth.bind(c, &req) {
		return
	}
	expiresAt, err := parseRequiredTime(req.ExpiresAt)
	if err != nil {
		_ = c.Error(err)
		return
	}
	meta := h.auth.meta(c, true)
	if req.RequestHash != "" {
		meta.RequestHash = req.RequestHash
	}
	out, err := h.credit.CreateRedeemCodes(c.Request.Context(), credit.CreateCodesInput{
		Auth: adminAuth(c), Meta: meta, Count: req.CodeCount, Points: req.PointsPerCode,
		CodeExpiresAt: expiresAt, CreditExpiresAt: expiresAt, AccountType: "personal",
		BindTargetType: req.BindTargetType, BindTargetID: req.BindTargetID, Channel: req.RedeemChannel,
	})
	respond(c, out, err)
}

func (h m4Handler) disableRedeemCodeBatch(c *gin.Context) {
	if h.credit == nil {
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
	out, err := h.credit.DisableRedeemCodeBatch(c.Request.Context(), adminAuth(c), c.Param("batch_id"))
	respond(c, out, err)
}

func (h m4Handler) exportRedeemCodes(c *gin.Context) {
	if h.credit == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	var req struct {
		ExportReason string `json:"export_reason"`
		RequestHash  string `json:"request_hash"`
	}
	if !h.auth.bind(c, &req) {
		return
	}
	out, err := h.credit.ExportRedeemCodes(c.Request.Context(), adminAuth(c), c.Param("batch_id"))
	respond(c, out, err)
}

func assetType(explicit, contentType string) string {
	if strings.TrimSpace(explicit) != "" {
		return explicit
	}
	switch {
	case strings.HasPrefix(contentType, "image/"):
		return "image"
	case strings.HasPrefix(contentType, "audio/"):
		return "music"
	case strings.HasPrefix(contentType, "video/"):
		return "video"
	default:
		return "file"
	}
}

func parseRequiredTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
	if err != nil {
		return time.Time{}, bizerrors.New(bizerrors.CodeInvalidArgument, "invalid RFC3339 time")
	}
	return parsed.UTC(), nil
}

func parseFutureTime(value string, fallback time.Duration) (time.Time, error) {
	if strings.TrimSpace(value) == "" {
		return time.Now().UTC().Add(fallback), nil
	}
	return parseRequiredTime(value)
}
