package rpc

import (
	"context"
	"strings"

	"github.com/FigoGoo/Dora-Agent/internal/contracts/foundation"
	"github.com/FigoGoo/Dora-Agent/internal/contracts/skillmarket"
	marketplaceapi "github.com/FigoGoo/Dora-Agent/kitex_gen/dora/api/businessskillmarketplace"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/marketplace"
	bizerrors "github.com/FigoGoo/Dora-Agent/services/business/internal/pkg/errors"
)

func (h *Handler) ListMarketplaceSkills(ctx context.Context, req *marketplaceapi.ListMarketplaceSkillsRequest) (*marketplaceapi.ListMarketplaceSkillsResponse, error) {
	if h.Marketplace == nil {
		return nil, bizerrors.NotImplemented("BusinessSkillMarketplaceService.ListMarketplaceSkills")
	}
	if req == nil {
		return nil, bizerrors.New(bizerrors.CodeInvalidArgument, "request is required")
	}
	if err := validateMarketplaceRPCContext(req.GetAuthContext(), req.GetRequestMeta()); err != nil {
		return nil, err
	}
	out, err := h.Marketplace.ListMarketplaceSkills(ctx, marketplace.ListMarketplaceSkillsInput{
		Auth:   authContextFromRPC(req.GetAuthContext()),
		Query:  req.GetQuery(),
		Limit:  int(req.GetPageSize()),
		Cursor: req.GetCursor(),
	})
	if err != nil {
		return nil, err
	}
	listings := make([]*marketplaceapi.MarketplaceListingDTO, 0, len(out.Items))
	for _, item := range out.Items {
		listings = append(listings, marketplaceListingToRPC(item))
	}
	return &marketplaceapi.ListMarketplaceSkillsResponse{Listings: listings, NextCursor: optionalString(out.NextCursor)}, nil
}

func (h *Handler) GetMarketplaceSkill(ctx context.Context, req *marketplaceapi.GetMarketplaceSkillRequest) (*marketplaceapi.GetMarketplaceSkillResponse, error) {
	if h.Marketplace == nil {
		return nil, bizerrors.NotImplemented("BusinessSkillMarketplaceService.GetMarketplaceSkill")
	}
	if req == nil {
		return nil, bizerrors.New(bizerrors.CodeInvalidArgument, "request is required")
	}
	if err := validateMarketplaceRPCContext(req.GetAuthContext(), req.GetRequestMeta()); err != nil {
		return nil, err
	}
	out, err := h.Marketplace.GetMarketplaceSkill(ctx, authContextFromRPC(req.GetAuthContext()), req.GetListingId())
	if err != nil {
		return nil, err
	}
	return &marketplaceapi.GetMarketplaceSkillResponse{Listing: marketplaceListingToRPC(out.Listing)}, nil
}

func (h *Handler) InstallSkill(ctx context.Context, req *marketplaceapi.InstallSkillRequest) (*marketplaceapi.InstallSkillResponse, error) {
	if h.Marketplace == nil {
		return nil, bizerrors.NotImplemented("BusinessSkillMarketplaceService.InstallSkill")
	}
	if req == nil {
		return nil, bizerrors.New(bizerrors.CodeInvalidArgument, "request is required")
	}
	if err := validateMarketplaceRPCContext(req.GetAuthContext(), req.GetRequestMeta()); err != nil {
		return nil, err
	}
	out, err := h.Marketplace.InstallSkill(ctx, marketplace.InstallSkillInput{
		Auth:         authContextFromRPC(req.GetAuthContext()),
		Meta:         metaFromRPC(req.GetRequestMeta()),
		ListingID:    req.GetListingId(),
		TargetScope:  installationScopeFromRPC(req.GetTargetScope()),
		EnterpriseID: req.GetEnterpriseId(),
	})
	if err != nil {
		return nil, err
	}
	return &marketplaceapi.InstallSkillResponse{
		Installation:     skillInstallationToMarketplaceRPC(out.Installation),
		IdempotentReplay: out.IdempotentReplay,
	}, nil
}

func (h *Handler) UpgradeSkillInstallation(ctx context.Context, req *marketplaceapi.UpgradeSkillInstallationRequest) (*marketplaceapi.UpgradeSkillInstallationResponse, error) {
	if h.Marketplace == nil {
		return nil, bizerrors.NotImplemented("BusinessSkillMarketplaceService.UpgradeSkillInstallation")
	}
	if req == nil {
		return nil, bizerrors.New(bizerrors.CodeInvalidArgument, "request is required")
	}
	if err := validateMarketplaceRPCContext(req.GetAuthContext(), req.GetRequestMeta()); err != nil {
		return nil, err
	}
	out, err := h.Marketplace.UpgradeSkillInstallation(ctx, marketplace.UpgradeSkillInstallationInput{
		Auth:           authContextFromRPC(req.GetAuthContext()),
		Meta:           metaFromRPC(req.GetRequestMeta()),
		InstallationID: req.GetInstallationId(),
		TargetVersion:  req.GetTargetVersion(),
		Confirmed:      req.GetConfirmed(),
	})
	if err != nil {
		return nil, err
	}
	return &marketplaceapi.UpgradeSkillInstallationResponse{Installation: skillInstallationToMarketplaceRPC(out.Installation)}, nil
}

func (h *Handler) EstimateSkillUsageCredits(ctx context.Context, req *marketplaceapi.EstimateSkillUsageCreditsRequest) (*marketplaceapi.EstimateSkillUsageCreditsResponse, error) {
	if h.Marketplace == nil {
		return nil, bizerrors.NotImplemented("BusinessSkillMarketplaceService.EstimateSkillUsageCredits")
	}
	if req == nil {
		return nil, bizerrors.New(bizerrors.CodeInvalidArgument, "request is required")
	}
	if err := validateMarketplaceRPCContext(req.GetAuthContext(), req.GetRequestMeta()); err != nil {
		return nil, err
	}
	out, err := h.Marketplace.EstimateSkillUsageCredits(ctx, marketplace.EstimateSkillUsageCreditsInput{
		Auth:                authContextFromRPC(req.GetAuthContext()),
		Meta:                metaFromRPC(req.GetRequestMeta()),
		RunID:               req.GetRunId(),
		ListingID:           req.GetListingId(),
		PricingPolicyDigest: req.GetPricingPolicyDigest(),
	})
	if err != nil {
		return nil, err
	}
	return &marketplaceapi.EstimateSkillUsageCreditsResponse{
		EstimatedCredits:    int64(out.EstimatedCredits),
		PricingPolicyDigest: out.PricingPolicyDigest,
		SkillUsageDigest:    out.SkillUsageDigest,
		ExpiresAt:           formatTime(out.ExpiresAt),
	}, nil
}

func (h *Handler) CreateSkillUsageRecord(ctx context.Context, req *marketplaceapi.CreateSkillUsageRecordRequest) (*marketplaceapi.CreateSkillUsageRecordResponse, error) {
	if h.Marketplace == nil {
		return nil, bizerrors.NotImplemented("BusinessSkillMarketplaceService.CreateSkillUsageRecord")
	}
	if req == nil {
		return nil, bizerrors.New(bizerrors.CodeInvalidArgument, "request is required")
	}
	if err := validateMarketplaceRPCContext(req.GetAuthContext(), req.GetRequestMeta()); err != nil {
		return nil, err
	}
	out, err := h.Marketplace.CreateSkillUsageRecord(ctx, marketplace.CreateSkillUsageRecordInput{
		Auth:                authContextFromRPC(req.GetAuthContext()),
		Meta:                metaFromRPC(req.GetRequestMeta()),
		RunID:               req.GetRunId(),
		ListingID:           req.GetListingId(),
		SkillID:             req.GetSkillId(),
		SkillVersion:        req.GetSkillVersion(),
		PricingPolicyDigest: req.GetPricingPolicyDigest(),
		SkillUsageDigest:    req.GetSkillUsageDigest(),
		EstimatedCredits:    int(req.GetEstimatedCredits()),
	})
	if err != nil {
		return nil, err
	}
	return &marketplaceapi.CreateSkillUsageRecordResponse{Usage: skillUsageToMarketplaceRPC(out.Usage), IdempotentReplay: out.IdempotentReplay}, nil
}

func (h *Handler) FreezeSkillUsageCredits(ctx context.Context, req *marketplaceapi.FreezeSkillUsageCreditsRequest) (*marketplaceapi.FreezeSkillUsageCreditsResponse, error) {
	if h.Marketplace == nil {
		return nil, bizerrors.NotImplemented("BusinessSkillMarketplaceService.FreezeSkillUsageCredits")
	}
	if req == nil {
		return nil, bizerrors.New(bizerrors.CodeInvalidArgument, "request is required")
	}
	if err := validateMarketplaceRPCContext(req.GetAuthContext(), req.GetRequestMeta()); err != nil {
		return nil, err
	}
	out, err := h.Marketplace.FreezeSkillUsageCredits(ctx, marketplace.FreezeSkillUsageCreditsInput{
		Auth:             authContextFromRPC(req.GetAuthContext()),
		UsageID:          req.GetUsageId(),
		SkillUsageDigest: req.GetSkillUsageDigest(),
	})
	if err != nil {
		return nil, err
	}
	return &marketplaceapi.FreezeSkillUsageCreditsResponse{Usage: skillUsageToMarketplaceRPC(out.Usage)}, nil
}

func (h *Handler) CommitSkillUsageAndSettle(ctx context.Context, req *marketplaceapi.CommitSkillUsageAndSettleRequest) (*marketplaceapi.CommitSkillUsageAndSettleResponse, error) {
	if h.Marketplace == nil {
		return nil, bizerrors.NotImplemented("BusinessSkillMarketplaceService.CommitSkillUsageAndSettle")
	}
	if req == nil {
		return nil, bizerrors.New(bizerrors.CodeInvalidArgument, "request is required")
	}
	if err := validateMarketplaceRPCContext(req.GetAuthContext(), req.GetRequestMeta()); err != nil {
		return nil, err
	}
	if err := foundation.ValidateDigest(req.GetValueDeliveredDigest()); err != nil {
		return nil, bizerrors.New(bizerrors.CodeInvalidArgument, "value_delivered_digest is invalid")
	}
	out, err := h.Marketplace.CommitSkillUsageAndSettle(ctx, marketplace.CommitSkillUsageAndSettleInput{
		Auth:    authContextFromRPC(req.GetAuthContext()),
		UsageID: req.GetUsageId(),
	})
	if err != nil {
		return nil, err
	}
	return &marketplaceapi.CommitSkillUsageAndSettleResponse{Usage: skillUsageToMarketplaceRPC(out.Usage), SettlementId: out.Settlement.SettlementID}, nil
}

func (h *Handler) ReleaseSkillUsageFreeze(ctx context.Context, req *marketplaceapi.ReleaseSkillUsageFreezeRequest) (*marketplaceapi.ReleaseSkillUsageFreezeResponse, error) {
	if h.Marketplace == nil {
		return nil, bizerrors.NotImplemented("BusinessSkillMarketplaceService.ReleaseSkillUsageFreeze")
	}
	if req == nil {
		return nil, bizerrors.New(bizerrors.CodeInvalidArgument, "request is required")
	}
	if err := validateMarketplaceRPCContext(req.GetAuthContext(), req.GetRequestMeta()); err != nil {
		return nil, err
	}
	out, err := h.Marketplace.ReleaseSkillUsageFreeze(ctx, marketplace.ReleaseSkillUsageFreezeInput{
		Auth:          authContextFromRPC(req.GetAuthContext()),
		UsageID:       req.GetUsageId(),
		ReleaseReason: req.GetReleaseReason(),
	})
	if err != nil {
		return nil, err
	}
	return &marketplaceapi.ReleaseSkillUsageFreezeResponse{Usage: skillUsageToMarketplaceRPC(out.Usage)}, nil
}

func validateMarketplaceRPCContext(auth any, meta any) error {
	if auth == nil {
		return bizerrors.New(bizerrors.CodeInvalidArgument, "auth_context is required")
	}
	if meta == nil {
		return bizerrors.New(bizerrors.CodeInvalidArgument, "request_meta is required")
	}
	return nil
}

func marketplaceListingToRPC(in marketplace.MarketplaceListingDTO) *marketplaceapi.MarketplaceListingDTO {
	return &marketplaceapi.MarketplaceListingDTO{
		ListingId:           in.ListingID,
		SkillId:             in.SkillID,
		SkillVersionId:      in.SkillVersionID,
		Status:              marketplaceListingStatusToRPC(in.Status),
		PricingPolicyDigest: in.PricingPolicyDigest,
		CreatedAt:           formatTime(in.CreatedAt),
		UpdatedAt:           formatTime(in.UpdatedAt),
	}
}

func skillInstallationToMarketplaceRPC(in marketplace.SkillInstallationDTO) *marketplaceapi.SkillInstallationDTO {
	return &marketplaceapi.SkillInstallationDTO{
		InstallationId:   in.InstallationID,
		AccountId:        in.AccountID,
		AccountScope:     installationScopeToRPC(in.AccountScope),
		ListingId:        in.ListingID,
		SkillId:          in.SkillID,
		InstalledVersion: in.InstalledVersion,
		VersionStrategy:  in.VersionStrategy,
		Status:           in.Status,
		UpgradeStatus:    in.UpgradeStatus,
		CreatedAt:        formatTime(in.CreatedAt),
		UpdatedAt:        formatTime(in.UpdatedAt),
	}
}

func skillUsageToMarketplaceRPC(in marketplace.SkillUsageRecordDTO) *marketplaceapi.SkillUsageRecordDTO {
	return &marketplaceapi.SkillUsageRecordDTO{
		UsageId:             in.UsageID,
		RunId:               in.RunID,
		ListingId:           in.ListingID,
		SkillId:             in.SkillID,
		SkillVersion:        in.SkillVersion,
		PricingPolicyDigest: in.PricingPolicyDigest,
		SkillUsageDigest:    in.SkillUsageDigest,
		UsageStatus:         in.UsageStatus,
		ChargeStatus:        in.ChargeStatus,
		RefundStatus:        in.RefundStatus,
		SettlementStatus:    in.SettlementStatus,
		EstimatedCredits:    int64(in.EstimatedCredits),
		CreditHoldId:        in.CreditHoldID,
	}
}

func marketplaceListingStatusToRPC(status string) marketplaceapi.MarketplaceListingStatus {
	switch strings.TrimSpace(status) {
	case "pending_listing_review":
		return marketplaceapi.MarketplaceListingStatus_PENDING_LISTING_REVIEW
	case "listed":
		return marketplaceapi.MarketplaceListingStatus_LISTED
	case "unlisted":
		return marketplaceapi.MarketplaceListingStatus_UNLISTED
	case "suspended":
		return marketplaceapi.MarketplaceListingStatus_SUSPENDED
	case "removed":
		return marketplaceapi.MarketplaceListingStatus_REMOVED
	default:
		return marketplaceapi.MarketplaceListingStatus_DRAFT
	}
}

func installationScopeFromRPC(scope marketplaceapi.InstallationScope) string {
	switch scope {
	case marketplaceapi.InstallationScope_ENTERPRISE:
		return skillmarket.AccountScopeEnterprise
	default:
		return skillmarket.AccountScopePersonal
	}
}

func installationScopeToRPC(scope string) marketplaceapi.InstallationScope {
	if strings.TrimSpace(scope) == skillmarket.AccountScopeEnterprise {
		return marketplaceapi.InstallationScope_ENTERPRISE
	}
	return marketplaceapi.InstallationScope_PERSONAL
}
