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

func registerCreditAssetRoutes(router *gin.Engine, opts RouterOptions) {
	auth := accountProjectAdminHandler{account: opts.AccountSpace, admin: opts.Admin, project: opts.Project}
	h := creditAssetHandler{credit: opts.Credit, asset: opts.Asset, auth: auth}

	router.GET("/api/credits/summary", auth.userAuth(), h.creditSummary)
	router.GET("/api/credits/lots", auth.userAuth(), h.creditLots)
	router.GET("/api/credits/expiring", auth.userAuth(), h.expiringCredits)
	router.GET("/api/credits/ledger", auth.userAuth(), h.creditLedger)
	router.POST("/api/credits/redeem", auth.userAuth(), requireIdempotency(), h.redeemCode)
	router.GET("/api/credits/recharge-packages", auth.userAuth(), h.rechargePackages)
	router.GET("/api/credits/recharge-orders", auth.userAuth(), h.rechargeOrders)
	router.POST("/api/credits/recharge-orders", auth.userAuth(), requireIdempotency(), h.createRechargeOrder)
	router.POST("/api/credits/recharge-orders/:order_id/mock-pay", auth.userAuth(), requireIdempotency(), h.mockPayRechargeOrder)
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
	router.GET("/api/admin/credits/accounts", auth.adminAuth(false), h.adminCreditAccounts)
	router.GET("/api/admin/credits/lots", auth.adminAuth(false), h.adminCreditLots)
	router.POST("/api/admin/credits/lots/expire", auth.adminAuth(false), requireIdempotency(), h.adminExpireCreditLots)
	router.POST("/api/admin/credits/refunds", auth.adminAuth(false), requireIdempotency(), h.adminRefundCredits)
	router.POST("/api/admin/credits/ledger/:entry_id/reverse", auth.adminAuth(false), requireIdempotency(), h.adminReverseCreditLedgerEntry)
	router.GET("/api/admin/credits/codes", auth.adminAuth(false), h.listRedeemCodes)
	router.POST("/api/admin/credits/codes", auth.adminAuth(false), requireIdempotency(), h.createRedeemCodes)
	router.POST("/api/admin/credits/codes/:batch_id/disable", auth.adminAuth(false), requireIdempotency(), h.disableRedeemCodeBatch)
	router.POST("/api/admin/credits/codes/:batch_id/export", auth.adminAuth(false), requireIdempotency(), h.exportRedeemCodes)
	router.GET("/api/admin/orders", auth.adminAuth(false), h.adminRechargeOrders)
	router.GET("/api/admin/billing/packages", auth.adminAuth(false), h.adminBillingPackages)
	router.POST("/api/admin/billing/packages", auth.adminAuth(false), requireIdempotency(), h.adminSaveBillingPackage)
	router.PATCH("/api/admin/billing/packages/:package_id", auth.adminAuth(false), requireIdempotency(), h.adminSaveBillingPackage)
	router.POST("/api/admin/billing/packages/:package_id/status", auth.adminAuth(false), requireIdempotency(), h.adminSetBillingPackageStatus)
	router.GET("/api/admin/billing/skus", auth.adminAuth(false), h.adminBillingSKUs)
	router.POST("/api/admin/billing/skus", auth.adminAuth(false), requireIdempotency(), h.adminCreateBillingSKU)
	router.GET("/api/admin/billing/orders", auth.adminAuth(false), h.adminRechargeOrders)
	router.GET("/api/admin/billing/redeem-codes", auth.adminAuth(false), h.listRedeemCodes)
	router.GET("/api/admin/billing/credit-lots", auth.adminAuth(false), h.adminCreditLots)
	router.POST("/api/admin/billing/credit-lots/expire", auth.adminAuth(false), requireIdempotency(), h.adminExpireCreditLots)
	router.POST("/api/admin/billing/refunds", auth.adminAuth(false), requireIdempotency(), h.adminRefundCredits)
	router.POST("/api/admin/billing/ledger/:entry_id/reverse", auth.adminAuth(false), requireIdempotency(), h.adminReverseCreditLedgerEntry)
	router.GET("/api/admin/billing/entitlements", auth.adminAuth(false), h.adminEntitlementSnapshots)
	router.GET("/api/admin/billing/enterprise-contracts", auth.adminAuth(false), h.adminEnterpriseContracts)
	router.GET("/api/admin/billing/invoices", auth.adminAuth(false), h.adminBillingInvoices)
	router.GET("/api/admin/billing/promotions", auth.adminAuth(false), h.adminBillingPromotions)
}

type creditAssetHandler struct {
	credit *credit.App
	asset  *asset.App
	auth   accountProjectAdminHandler
}

func (h creditAssetHandler) creditSummary(c *gin.Context) {
	if h.credit == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	out, err := h.credit.GetSummary(c.Request.Context(), userAuth(c))
	respond(c, out, err)
}

func (h creditAssetHandler) creditLedger(c *gin.Context) {
	if h.credit == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	out, err := h.credit.ListLedger(c.Request.Context(), userAuth(c), intQuery(c, "limit", 10), intQuery(c, "offset", 0))
	respond(c, out, err)
}

func (h creditAssetHandler) creditLots(c *gin.Context) {
	if h.credit == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	out, err := h.credit.ListCreditLots(c.Request.Context(), userAuth(c), c.Query("source_type"), c.Query("status"), intQuery(c, "limit", 10), intQuery(c, "offset", 0))
	respond(c, out, err)
}

func (h creditAssetHandler) expiringCredits(c *gin.Context) {
	if h.credit == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	out, err := h.credit.ListExpiringCredits(c.Request.Context(), userAuth(c), intQuery(c, "within_days", 30), intQuery(c, "limit", 10), intQuery(c, "offset", 0))
	respond(c, out, err)
}

func (h creditAssetHandler) redeemCode(c *gin.Context) {
	if h.credit == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	var req struct {
		RedeemCode        string `json:"redeem_code"`
		TargetAccountType string `json:"target_account_type"`
		RedeemChannel     string `json:"redeem_channel"`
		RequestHash       string `json:"request_hash"`
	}
	if !h.auth.bind(c, &req) {
		return
	}
	meta := h.auth.meta(c, true)
	if req.RequestHash != "" {
		meta.RequestHash = req.RequestHash
	}
	out, err := h.credit.RedeemCode(c.Request.Context(), credit.RedeemInput{
		Auth: userAuth(c), Meta: meta, Code: req.RedeemCode,
		TargetAccountType: req.TargetAccountType, RedeemChannel: req.RedeemChannel,
	})
	respond(c, out, err)
}

func (h creditAssetHandler) rechargePackages(c *gin.Context) {
	if h.credit == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	out, err := h.credit.ListRechargePackages(c.Request.Context())
	respond(c, out, err)
}

func (h creditAssetHandler) rechargeOrders(c *gin.Context) {
	if h.credit == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	out, err := h.credit.ListRechargeOrders(c.Request.Context(), userAuth(c), c.Query("payment_status"), intQuery(c, "limit", 10), intQuery(c, "offset", 0))
	respond(c, out, err)
}

func (h creditAssetHandler) createRechargeOrder(c *gin.Context) {
	if h.credit == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	var req struct {
		PackageID         string `json:"package_id"`
		SKUID             string `json:"sku_id"`
		TargetAccountType string `json:"target_account_type"`
		RequestHash       string `json:"request_hash"`
	}
	if !h.auth.bind(c, &req) {
		return
	}
	meta := h.auth.meta(c, true)
	if req.RequestHash != "" {
		meta.RequestHash = req.RequestHash
	}
	out, err := h.credit.CreateRechargeOrder(c.Request.Context(), credit.CreateRechargeOrderInput{Auth: userAuth(c), Meta: meta, PackageID: req.PackageID, SKUID: req.SKUID, TargetAccountType: req.TargetAccountType})
	respond(c, out, err)
}

func (h creditAssetHandler) mockPayRechargeOrder(c *gin.Context) {
	if h.credit == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	var req struct {
		PaymentResult         string `json:"payment_result"`
		ProviderTransactionID string `json:"provider_transaction_id"`
		RequestHash           string `json:"request_hash"`
	}
	if !h.auth.bind(c, &req) {
		return
	}
	meta := h.auth.meta(c, true)
	if req.RequestHash != "" {
		meta.RequestHash = req.RequestHash
	}
	out, err := h.credit.MockPayRechargeOrder(c.Request.Context(), credit.MockPayRechargeOrderInput{
		Auth: userAuth(c), Meta: meta, OrderID: c.Param("order_id"),
		PaymentResult: req.PaymentResult, ProviderTransactionID: req.ProviderTransactionID,
	})
	respond(c, out, err)
}

func (h creditAssetHandler) enterpriseCredits(c *gin.Context) {
	if h.credit == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	out, err := h.credit.GetEnterpriseSummary(c.Request.Context(), userAuth(c))
	respond(c, out, err)
}

func (h creditAssetHandler) enterpriseUsage(c *gin.Context) {
	if h.credit == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	out, err := h.credit.ListEnterpriseUsage(c.Request.Context(), userAuth(c), intQuery(c, "limit", 10), intQuery(c, "offset", 0))
	respond(c, out, err)
}

func (h creditAssetHandler) listAssets(c *gin.Context) {
	if h.asset == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	out, err := h.asset.ListAssets(c.Request.Context(), userAuth(c), c.Query("project_id"), c.Query("asset_type"), c.Query("status"), intQuery(c, "limit", 10), intQuery(c, "offset", 0))
	respond(c, out, err)
}

func (h creditAssetHandler) getAsset(c *gin.Context) {
	if h.asset == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	out, err := h.asset.GetAsset(c.Request.Context(), userAuth(c), c.Param("asset_id"))
	respond(c, out, err)
}

func (h creditAssetHandler) createUploadIntent(c *gin.Context) {
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

func (h creditAssetHandler) confirmUploadIntent(c *gin.Context) {
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

func (h creditAssetHandler) abortUploadIntent(c *gin.Context) {
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

func (h creditAssetHandler) getAssetAccess(c *gin.Context) {
	if h.asset == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	out, err := h.asset.GetAssetAccess(c.Request.Context(), userAuth(c), c.Param("asset_id"), c.Query("access_type"))
	respond(c, out, err)
}

func (h creditAssetHandler) searchCreditTargets(c *gin.Context) {
	if h.credit == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	out, err := h.credit.SearchCreditTargets(c.Request.Context(), adminAuth(c), c.Query("keyword"), c.Query("target_type"), adminPageLimit(c, 10), adminPageOffset(c))
	respond(c, out, err)
}

func (h creditAssetHandler) adminGrantCredits(c *gin.Context) {
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

func (h creditAssetHandler) adminCreditAccounts(c *gin.Context) {
	if h.credit == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	out, err := h.credit.AdminListCreditAccounts(c.Request.Context(), adminAuth(c), c.Query("account_type"), c.Query("status"), adminPageLimit(c, 10), adminPageOffset(c))
	respond(c, out, err)
}

func (h creditAssetHandler) adminCreditLots(c *gin.Context) {
	if h.credit == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	out, err := h.credit.AdminListCreditLots(c.Request.Context(), adminAuth(c), c.Query("account_id"), c.Query("source_type"), c.Query("status"), adminPageLimit(c, 10), adminPageOffset(c))
	respond(c, out, err)
}

func (h creditAssetHandler) adminExpireCreditLots(c *gin.Context) {
	if h.credit == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	var req struct {
		AccountID   string `json:"account_id"`
		LotID       string `json:"lot_id"`
		Limit       int    `json:"limit"`
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
	out, err := h.credit.AdminExpireCreditLots(c.Request.Context(), credit.ExpireCreditLotsInput{
		Auth: adminAuth(c), Meta: meta, AccountID: req.AccountID, LotID: req.LotID, Limit: req.Limit, Reason: req.Reason,
	})
	respond(c, out, err)
}

func (h creditAssetHandler) adminRefundCredits(c *gin.Context) {
	if h.credit == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	var req struct {
		AccountID             string `json:"account_id"`
		Points                int64  `json:"points"`
		OriginalLotID         string `json:"original_lot_id"`
		OriginalLedgerEntryID string `json:"original_ledger_entry_id"`
		GracePeriodDays       int    `json:"grace_period_days"`
		Reason                string `json:"reason"`
		RequestHash           string `json:"request_hash"`
	}
	if !h.auth.bind(c, &req) {
		return
	}
	meta := h.auth.meta(c, true)
	if req.RequestHash != "" {
		meta.RequestHash = req.RequestHash
	}
	out, err := h.credit.AdminRefundCredits(c.Request.Context(), credit.RefundCreditsInput{
		Auth: adminAuth(c), Meta: meta, AccountID: req.AccountID, Points: req.Points,
		OriginalLotID: req.OriginalLotID, OriginalLedgerEntryID: req.OriginalLedgerEntryID,
		GracePeriodDays: req.GracePeriodDays, Reason: req.Reason,
	})
	respond(c, out, err)
}

func (h creditAssetHandler) adminReverseCreditLedgerEntry(c *gin.Context) {
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
	meta := h.auth.meta(c, true)
	if req.RequestHash != "" {
		meta.RequestHash = req.RequestHash
	}
	out, err := h.credit.AdminReverseCreditLedgerEntry(c.Request.Context(), credit.ReverseCreditLedgerEntryInput{
		Auth: adminAuth(c), Meta: meta, LedgerEntryID: c.Param("entry_id"), Reason: req.Reason,
	})
	respond(c, out, err)
}

func (h creditAssetHandler) adminRechargeOrders(c *gin.Context) {
	if h.credit == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	out, err := h.credit.AdminListRechargeOrders(c.Request.Context(), adminAuth(c), c.Query("user_id"), c.Query("account_id"), c.Query("payment_status"), adminPageLimit(c, 10), adminPageOffset(c))
	respond(c, out, err)
}

func (h creditAssetHandler) adminBillingPackages(c *gin.Context) {
	if h.credit == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	out, err := h.credit.AdminListBillingPackages(c.Request.Context(), adminAuth(c), c.Query("target_scope"), c.Query("package_type"), c.Query("status"), adminPageLimit(c, 10), adminPageOffset(c))
	respond(c, out, err)
}

func (h creditAssetHandler) adminSaveBillingPackage(c *gin.Context) {
	if h.credit == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	var req struct {
		PackageID           string         `json:"package_id"`
		PackageType         string         `json:"package_type"`
		Name                string         `json:"name"`
		DisplayName         string         `json:"display_name"`
		TargetScope         string         `json:"target_scope"`
		BillingMode         string         `json:"billing_mode"`
		PriceAmount         int64          `json:"price_amount"`
		PriceCents          int64          `json:"price_cents"`
		Currency            string         `json:"currency"`
		GrantedPoints       int64          `json:"granted_points"`
		BonusPoints         int64          `json:"bonus_points"`
		Points              int64          `json:"points"`
		CreditExpiryPolicy  string         `json:"credit_expiry_policy"`
		CreditValidDuration string         `json:"credit_valid_duration"`
		SpendScope          []string       `json:"spend_scope"`
		SettlementEligible  bool           `json:"settlement_eligible"`
		EntitlementPolicy   map[string]any `json:"entitlement_policy"`
		RenewalPolicy       map[string]any `json:"renewal_policy"`
		RefundPolicy        map[string]any `json:"refund_policy"`
		VisibleScope        string         `json:"visible_scope"`
		Status              string         `json:"status"`
		Reason              string         `json:"reason"`
		RequestHash         string         `json:"request_hash"`
	}
	if !h.auth.bind(c, &req) {
		return
	}
	if req.PackageID == "" {
		req.PackageID = c.Param("package_id")
	}
	if req.Name == "" {
		req.Name = req.DisplayName
	}
	if req.PriceAmount == 0 {
		req.PriceAmount = req.PriceCents
	}
	if req.GrantedPoints == 0 && req.Points > 0 {
		req.GrantedPoints = req.Points
	}
	if req.CreditExpiryPolicy == "" {
		req.CreditExpiryPolicy = req.CreditValidDuration
	}
	meta := h.auth.meta(c, true)
	if req.RequestHash != "" {
		meta.RequestHash = req.RequestHash
	}
	out, err := h.credit.AdminSaveBillingPackage(c.Request.Context(), credit.SaveBillingPackageInput{
		Auth: adminAuth(c), Meta: meta, PackageID: req.PackageID, PackageType: req.PackageType, Name: req.Name,
		TargetScope: req.TargetScope, BillingMode: req.BillingMode, PriceAmount: req.PriceAmount, Currency: req.Currency,
		GrantedPoints: req.GrantedPoints, BonusPoints: req.BonusPoints, CreditExpiryPolicy: req.CreditExpiryPolicy,
		SpendScope: req.SpendScope, SettlementEligible: req.SettlementEligible, EntitlementPolicy: req.EntitlementPolicy,
		RenewalPolicy: req.RenewalPolicy, RefundPolicy: req.RefundPolicy, VisibleScope: req.VisibleScope,
		Status: req.Status, Reason: req.Reason,
	})
	respond(c, out, err)
}

func (h creditAssetHandler) adminSetBillingPackageStatus(c *gin.Context) {
	if h.credit == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	var req struct {
		Status      string `json:"status"`
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
	out, err := h.credit.AdminSetBillingPackageStatus(c.Request.Context(), credit.BillingPackageStatusInput{
		Auth: adminAuth(c), Meta: meta, PackageID: c.Param("package_id"), Status: req.Status, Reason: req.Reason,
	})
	respond(c, out, err)
}

func (h creditAssetHandler) adminBillingSKUs(c *gin.Context) {
	if h.credit == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	out, err := h.credit.AdminListBillingPackageSKUs(c.Request.Context(), adminAuth(c), c.Query("package_id"), c.Query("status"), adminPageLimit(c, 10), adminPageOffset(c))
	respond(c, out, err)
}

func (h creditAssetHandler) adminCreateBillingSKU(c *gin.Context) {
	if h.credit == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	var req struct {
		PackageID           string `json:"package_id"`
		SKUID               string `json:"sku_id"`
		ChannelCode         string `json:"channel_code"`
		PriceAmount         int64  `json:"price_amount"`
		Currency            string `json:"currency"`
		ActivityPriceAmount *int64 `json:"activity_price_amount"`
		EffectiveAt         string `json:"effective_at"`
		ExpiredAt           string `json:"expired_at"`
		Reason              string `json:"reason"`
		RequestHash         string `json:"request_hash"`
	}
	if !h.auth.bind(c, &req) {
		return
	}
	effectiveAt, err := parseOptionalTime(req.EffectiveAt)
	if err != nil {
		_ = c.Error(err)
		return
	}
	expiredAt, err := parseOptionalTimePtr(req.ExpiredAt)
	if err != nil {
		_ = c.Error(err)
		return
	}
	meta := h.auth.meta(c, true)
	if req.RequestHash != "" {
		meta.RequestHash = req.RequestHash
	}
	out, err := h.credit.AdminCreateBillingPackageSKU(c.Request.Context(), credit.CreateBillingSKUInput{
		Auth: adminAuth(c), Meta: meta, PackageID: req.PackageID, SKUID: req.SKUID, ChannelCode: req.ChannelCode,
		PriceAmount: req.PriceAmount, Currency: req.Currency, ActivityPriceAmount: req.ActivityPriceAmount,
		EffectiveAt: effectiveAt, ExpiredAt: expiredAt, Reason: req.Reason,
	})
	respond(c, out, err)
}

func (h creditAssetHandler) adminEntitlementSnapshots(c *gin.Context) {
	if h.credit == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	out, err := h.credit.AdminListEntitlementSnapshots(c.Request.Context(), adminAuth(c), c.Query("account_id"), c.Query("enterprise_id"), c.Query("status"), adminPageLimit(c, 10), adminPageOffset(c))
	respond(c, out, err)
}

func (h creditAssetHandler) adminEnterpriseContracts(c *gin.Context) {
	if h.credit == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	out, err := h.credit.AdminListEnterpriseContracts(c.Request.Context(), adminAuth(c), c.Query("enterprise_id"), c.Query("contract_status"), adminPageLimit(c, 10), adminPageOffset(c))
	respond(c, out, err)
}

func (h creditAssetHandler) adminBillingInvoices(c *gin.Context) {
	if h.credit == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	out, err := h.credit.AdminListBillingInvoices(c.Request.Context(), adminAuth(c), c.Query("enterprise_id"), c.Query("invoice_status"), adminPageLimit(c, 10), adminPageOffset(c))
	respond(c, out, err)
}

func (h creditAssetHandler) adminBillingPromotions(c *gin.Context) {
	if h.credit == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	out, err := h.credit.AdminListBillingPromotions(c.Request.Context(), adminAuth(c), c.Query("package_id"), c.Query("status"), adminPageLimit(c, 10), adminPageOffset(c))
	respond(c, out, err)
}

func (h creditAssetHandler) listRedeemCodes(c *gin.Context) {
	if h.credit == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	out, err := h.credit.ListRedeemCodes(c.Request.Context(), adminAuth(c), adminPageLimit(c, 10), adminPageOffset(c))
	respond(c, out, err)
}

func (h creditAssetHandler) createRedeemCodes(c *gin.Context) {
	if h.credit == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	var req struct {
		Points          int64  `json:"points"`
		Count           int    `json:"count"`
		PointsPerCode   int64  `json:"points_per_code"`
		CodeCount       int    `json:"code_count"`
		CodeExpiresAt   string `json:"code_expires_at"`
		CreditExpiresAt string `json:"credit_expires_at"`
		ExpiresAt       string `json:"expires_at"`
		AccountType     string `json:"account_type"`
		BindTargetType  string `json:"bind_target_type"`
		BindTargetID    string `json:"bind_target_id"`
		Channel         string `json:"channel"`
		RedeemChannel   string `json:"redeem_channel"`
		Reason          string `json:"reason"`
		RequestHash     string `json:"request_hash"`
	}
	if !h.auth.bind(c, &req) {
		return
	}
	if req.Count == 0 {
		req.Count = req.CodeCount
	}
	if req.Points == 0 {
		req.Points = req.PointsPerCode
	}
	if req.Channel == "" {
		req.Channel = req.RedeemChannel
	}
	codeExpiresAtRaw := req.CodeExpiresAt
	if codeExpiresAtRaw == "" {
		codeExpiresAtRaw = req.ExpiresAt
	}
	creditExpiresAtRaw := req.CreditExpiresAt
	if creditExpiresAtRaw == "" {
		creditExpiresAtRaw = codeExpiresAtRaw
	}
	codeExpiresAt, err := parseRequiredTime(codeExpiresAtRaw)
	if err != nil {
		_ = c.Error(err)
		return
	}
	creditExpiresAt, err := parseRequiredTime(creditExpiresAtRaw)
	if err != nil {
		_ = c.Error(err)
		return
	}
	meta := h.auth.meta(c, true)
	if req.RequestHash != "" {
		meta.RequestHash = req.RequestHash
	}
	out, err := h.credit.CreateRedeemCodes(c.Request.Context(), credit.CreateCodesInput{
		Auth: adminAuth(c), Meta: meta, Count: req.Count, Points: req.Points,
		CodeExpiresAt: codeExpiresAt, CreditExpiresAt: creditExpiresAt, AccountType: req.AccountType,
		BindTargetType: req.BindTargetType, BindTargetID: req.BindTargetID, Channel: req.Channel, Reason: req.Reason,
	})
	respond(c, out, err)
}

func (h creditAssetHandler) disableRedeemCodeBatch(c *gin.Context) {
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

func (h creditAssetHandler) exportRedeemCodes(c *gin.Context) {
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

func parseOptionalTime(value string) (time.Time, error) {
	if strings.TrimSpace(value) == "" {
		return time.Time{}, nil
	}
	return parseRequiredTime(value)
}

func parseOptionalTimePtr(value string) (*time.Time, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	parsed, err := parseRequiredTime(value)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func parseFutureTime(value string, fallback time.Duration) (time.Time, error) {
	if strings.TrimSpace(value) == "" {
		return time.Now().UTC().Add(fallback), nil
	}
	return parseRequiredTime(value)
}
