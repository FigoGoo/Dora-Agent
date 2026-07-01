package rpc

import (
	"context"
	"fmt"
	"time"

	"github.com/FigoGoo/Dora-Agent/kitex_gen/dora/api/businessagent"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/accountspace"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/admin"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/asset"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/notification"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/project"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/work"
	bizerrors "github.com/FigoGoo/Dora-Agent/services/business/internal/pkg/errors"
)

func (h *Handler) PreviewTransferOwner(ctx context.Context, req *businessagent.PreviewTransferOwnerRequest) (*businessagent.TransferOwnerPreviewDTO, error) {
	if h.Account == nil {
		return nil, bizerrors.NotImplemented("EnterpriseService.PreviewTransferOwner")
	}
	if req == nil || req.AuthContext == nil || req.RequestMeta == nil {
		return nil, bizerrors.New(bizerrors.CodeInvalidArgument, "auth_context and request_meta are required")
	}
	out, err := h.Account.PreviewTransferOwner(ctx, accountspace.TransferOwnerInput{
		Auth: authContextFromRPC(req.AuthContext), TargetMemberID: req.TargetMemberId,
		Reason: req.Reason, Meta: metaFromRPC(req.RequestMeta),
	})
	if err != nil {
		return nil, err
	}
	return &businessagent.TransferOwnerPreviewDTO{
		PreviewToken: out.PreviewToken, ImpactItems: out.ImpactItems, ExpiresAt: formatTime(out.ExpiresAt),
	}, nil
}

func (h *Handler) ConfirmTransferOwner(ctx context.Context, req *businessagent.ConfirmTransferOwnerRequest) (*businessagent.EnterpriseSummaryDTO, error) {
	if h.Account == nil {
		return nil, bizerrors.NotImplemented("EnterpriseService.ConfirmTransferOwner")
	}
	if req == nil || req.AuthContext == nil || req.RequestMeta == nil {
		return nil, bizerrors.New(bizerrors.CodeInvalidArgument, "auth_context and request_meta are required")
	}
	out, err := h.Account.ConfirmTransferOwner(ctx, accountspace.TransferOwnerInput{
		Auth: authContextFromRPC(req.AuthContext), TargetMemberID: req.TargetMemberId,
		Reason: req.Reason, PreviewToken: req.PreviewToken, Meta: metaFromRPC(req.RequestMeta),
	})
	if err != nil {
		return nil, err
	}
	return enterpriseSummaryToRPC(out), nil
}

func (h *Handler) CreateAdmin(ctx context.Context, req *businessagent.CreateAdminRequest) (*businessagent.PlatformAdminDTO, error) {
	if h.Admin == nil {
		return nil, bizerrors.NotImplemented("AdminService.CreateAdmin")
	}
	if req == nil || req.AuthContext == nil || req.RequestMeta == nil {
		return nil, bizerrors.New(bizerrors.CodeInvalidArgument, "auth_context and request_meta are required")
	}
	out, err := h.Admin.CreateAdmin(ctx, admin.CreateAdminInput{
		Auth: adminAuthFromRPC(req.AuthContext), Account: req.Account, InitialPassword: req.InitialPassword,
		Reason: req.Reason, Meta: metaFromRPC(req.RequestMeta),
	})
	if err != nil {
		return nil, err
	}
	return platformAdminToRPC(out), nil
}

func (h *Handler) DisableAdmin(ctx context.Context, req *businessagent.DisableAdminRequest) (*businessagent.PlatformAdminDTO, error) {
	if h.Admin == nil {
		return nil, bizerrors.NotImplemented("AdminService.DisableAdmin")
	}
	if req == nil || req.AuthContext == nil || req.RequestMeta == nil {
		return nil, bizerrors.New(bizerrors.CodeInvalidArgument, "auth_context and request_meta are required")
	}
	out, err := h.Admin.DisableAdmin(ctx, admin.DisableAdminInput{
		Auth: adminAuthFromRPC(req.AuthContext), AdminID: req.AdminId, Reason: req.Reason, Meta: metaFromRPC(req.RequestMeta),
	})
	if err != nil {
		return nil, err
	}
	return platformAdminToRPC(out), nil
}

func (h *Handler) PreviewSetUserStatus(ctx context.Context, req *businessagent.PreviewSetUserStatusRequest) (*businessagent.UserStatusPreviewDTO, error) {
	if h.Admin == nil {
		return nil, bizerrors.NotImplemented("UserAdminService.PreviewSetUserStatus")
	}
	if req == nil || req.AuthContext == nil || req.RequestMeta == nil {
		return nil, bizerrors.New(bizerrors.CodeInvalidArgument, "auth_context and request_meta are required")
	}
	out, err := h.Admin.PreviewSetUserStatus(ctx, admin.UserStatusInput{
		Auth: adminAuthFromRPC(req.AuthContext), UserID: req.TargetUserId, TargetStatus: req.TargetStatus,
		Reason: req.Reason, Meta: metaFromRPC(req.RequestMeta),
	})
	if err != nil {
		return nil, err
	}
	return &businessagent.UserStatusPreviewDTO{
		PreviewToken: out.PreviewToken, CurrentStatus: out.CurrentStatus, TargetStatus: out.TargetStatus,
		ImpactSummary: out.ImpactSummary, PublicContentRetained: out.PublicContentRetained,
		PrivateContentNotExposed: out.PrivateContentNotExposed, ExpiresAt: formatTime(out.ExpiresAt),
	}, nil
}

func (h *Handler) ConfirmSetUserStatus(ctx context.Context, req *businessagent.ConfirmSetUserStatusRequest) (*businessagent.AdminUserSummaryDTO, error) {
	if h.Admin == nil {
		return nil, bizerrors.NotImplemented("UserAdminService.ConfirmSetUserStatus")
	}
	if req == nil || req.AuthContext == nil || req.RequestMeta == nil {
		return nil, bizerrors.New(bizerrors.CodeInvalidArgument, "auth_context and request_meta are required")
	}
	out, err := h.Admin.ConfirmSetUserStatus(ctx, admin.UserStatusInput{
		Auth: adminAuthFromRPC(req.AuthContext), UserID: req.TargetUserId, TargetStatus: req.TargetStatus,
		Reason: req.Reason, PreviewToken: req.PreviewToken, Meta: metaFromRPC(req.RequestMeta),
	})
	if err != nil {
		return nil, err
	}
	return adminUserSummaryToRPC(out), nil
}

func (h *Handler) CreateProject(ctx context.Context, req *businessagent.CreateProjectRequest) (*businessagent.ProjectDetailDTO, error) {
	if h.Project == nil {
		return nil, bizerrors.NotImplemented("ProjectService.CreateProject")
	}
	if req == nil || req.AuthContext == nil || req.RequestMeta == nil {
		return nil, bizerrors.New(bizerrors.CodeInvalidArgument, "auth_context and request_meta are required")
	}
	out, err := h.Project.CreateProject(ctx, project.CreateInput{
		Auth: authContextFromRPC(req.AuthContext), Title: req.Title, InitialPromptDigest: req.GetInitialPromptDigest(),
		Source: req.GetSource(), SpaceID: req.GetSpaceId(), Meta: metaFromRPC(req.RequestMeta),
	})
	if err != nil {
		return nil, err
	}
	return projectDetailToRPC(out), nil
}

func (h *Handler) UpdateProjectTitle(ctx context.Context, req *businessagent.UpdateProjectTitleRequest) (*businessagent.ProjectDetailDTO, error) {
	if h.Project == nil {
		return nil, bizerrors.NotImplemented("ProjectService.UpdateProjectTitle")
	}
	if req == nil || req.AuthContext == nil || req.RequestMeta == nil {
		return nil, bizerrors.New(bizerrors.CodeInvalidArgument, "auth_context and request_meta are required")
	}
	title := req.Title
	out, err := h.Project.UpdateProject(ctx, project.UpdateInput{
		Auth: authContextFromRPC(req.AuthContext), ProjectID: req.ProjectId,
		Title: &title, BaseUpdatedAt: req.GetBaseUpdatedAt(), Meta: metaFromRPC(req.RequestMeta),
	})
	if err != nil {
		return nil, err
	}
	return projectDetailToRPC(out), nil
}

func (h *Handler) AttachAssetToProject(ctx context.Context, req *businessagent.AttachAssetToProjectRequest) (*businessagent.ProjectAssetDTO, error) {
	if h.Project == nil {
		return nil, bizerrors.NotImplemented("ProjectAssetService.AttachAssetToProject")
	}
	if req == nil || req.AuthContext == nil || req.RequestMeta == nil {
		return nil, bizerrors.New(bizerrors.CodeInvalidArgument, "auth_context and request_meta are required")
	}
	out, err := h.Project.AttachAssetToProject(ctx, project.AttachAssetInput{
		Auth: authContextFromRPC(req.AuthContext), ProjectID: req.ProjectId, AssetID: req.AssetId,
		AssetRole: req.GetAssetRole(), SourceSessionID: req.GetSourceSessionId(), SourceRunID: req.GetSourceRunId(),
		SourceArtifactID: req.GetSourceArtifactId(), SourceType: req.GetSourceType(), DisplayOrder: int(req.GetDisplayOrder()),
		Meta: metaFromRPC(req.RequestMeta),
	})
	if err != nil {
		return nil, err
	}
	return projectAssetToRPC(out), nil
}

func (h *Handler) CreateUploadIntent(ctx context.Context, req *businessagent.CreateUploadIntentRequest) (*businessagent.UploadIntentDTO, error) {
	if h.Asset == nil {
		return nil, bizerrors.NotImplemented("AssetService.CreateUploadIntent")
	}
	if req == nil || req.AuthContext == nil || req.RequestMeta == nil {
		return nil, bizerrors.New(bizerrors.CodeInvalidArgument, "auth_context and request_meta are required")
	}
	out, err := h.Asset.CreateUploadIntent(ctx, asset.CreateUploadIntentInput{
		Auth: authContextFromRPC(req.AuthContext), Meta: metaFromRPC(req.RequestMeta), ProjectID: req.ProjectId,
		AssetType: req.AssetType, Filename: req.Filename, ContentType: req.ContentType, SizeBytes: req.SizeBytes,
		Checksum: req.GetChecksum(), MetadataText: req.GetMetadataText(), SafetyEvidence: req.SafetyEvidence,
	})
	if err != nil {
		return nil, err
	}
	return uploadIntentToRPC(out), nil
}

func (h *Handler) ConfirmUploadedAsset(ctx context.Context, req *businessagent.ConfirmUploadedAssetRequest) (*businessagent.AssetDetailDTO, error) {
	if h.Asset == nil {
		return nil, bizerrors.NotImplemented("AssetService.ConfirmUploadedAsset")
	}
	if req == nil || req.AuthContext == nil || req.RequestMeta == nil {
		return nil, bizerrors.New(bizerrors.CodeInvalidArgument, "auth_context and request_meta are required")
	}
	out, err := h.Asset.ConfirmUploadIntent(ctx, asset.ConfirmUploadInput{
		Auth: authContextFromRPC(req.AuthContext), Meta: metaFromRPC(req.RequestMeta), UploadIntentID: req.UploadIntentId,
		Etag: req.Etag, SizeBytes: req.SizeBytes, ContentType: req.ContentType, Checksum: req.Checksum,
	})
	if err != nil {
		return nil, err
	}
	return assetDetailToRPC(out), nil
}

func (h *Handler) CreateWork(ctx context.Context, req *businessagent.CreateWorkRequest) (*businessagent.WorkDetailDTO, error) {
	if h.Work == nil {
		return nil, bizerrors.NotImplemented("WorkService.CreateWork")
	}
	if req == nil || req.AuthContext == nil || req.RequestMeta == nil {
		return nil, bizerrors.New(bizerrors.CodeInvalidArgument, "auth_context and request_meta are required")
	}
	out, err := h.Work.CreateWork(ctx, work.CreateWorkInput{
		Auth: authContextFromRPC(req.AuthContext), Meta: metaFromRPC(req.RequestMeta), ProjectID: req.ProjectId,
		Title: req.Title, Description: req.GetDescription(), AssetIDs: req.AssetIds, CoverAssetID: req.GetCoverAssetId(),
		Category: req.GetCategory(), Tags: req.GetTags(),
	})
	if err != nil {
		return nil, err
	}
	return workDetailToRPC(out), nil
}

func (h *Handler) PreviewShareWork(ctx context.Context, req *businessagent.PreviewShareWorkRequest) (*businessagent.ShareWorkPreviewDTO, error) {
	if h.Work == nil {
		return nil, bizerrors.NotImplemented("WorkShareService.PreviewShareWork")
	}
	if req == nil || req.AuthContext == nil || req.RequestMeta == nil {
		return nil, bizerrors.New(bizerrors.CodeInvalidArgument, "auth_context and request_meta are required")
	}
	out, err := h.Work.PreviewShareWork(ctx, work.PreviewShareWorkInput{
		Auth: authContextFromRPC(req.AuthContext), WorkID: req.WorkId, PublicTitle: req.PublicTitle,
		PublicDescription: req.GetPublicDescription(), Tags: req.GetTags(), SafetyEvidence: req.SafetyEvidence,
	})
	if err != nil {
		return nil, err
	}
	return sharePreviewToRPC(out), nil
}

func (h *Handler) ConfirmShareWork(ctx context.Context, req *businessagent.ConfirmShareWorkRequest) (*businessagent.WorkShareResultDTO, error) {
	if h.Work == nil {
		return nil, bizerrors.NotImplemented("WorkShareService.ConfirmShareWork")
	}
	if req == nil || req.AuthContext == nil || req.RequestMeta == nil {
		return nil, bizerrors.New(bizerrors.CodeInvalidArgument, "auth_context and request_meta are required")
	}
	out, err := h.Work.ConfirmShareWork(ctx, work.ConfirmShareWorkInput{
		Auth: authContextFromRPC(req.AuthContext), Meta: metaFromRPC(req.RequestMeta), WorkID: req.WorkId, PreviewToken: req.PreviewToken,
	})
	if err != nil {
		return nil, err
	}
	return &businessagent.WorkShareResultDTO{
		WorkId: out.WorkID, PublicWorkId: out.PublicWorkID, ShareUrl: out.ShareURL, ShareStatus: out.ShareStatus, SnapshotId: out.SnapshotID,
	}, nil
}

func (h *Handler) PreviewTakeDownWork(ctx context.Context, req *businessagent.PreviewTakeDownPublicWorkRequest) (*businessagent.TakeDownPublicWorkPreviewDTO, error) {
	if h.Work == nil {
		return nil, bizerrors.NotImplemented("FeaturedWorkAdminService.PreviewTakeDownWork")
	}
	if req == nil || req.AuthContext == nil || req.RequestMeta == nil {
		return nil, bizerrors.New(bizerrors.CodeInvalidArgument, "auth_context and request_meta are required")
	}
	out, err := h.Work.PreviewTakeDownWork(ctx, work.PreviewTakeDownWorkInput{
		Auth: adminAuthFromRPC(req.AuthContext), PublicWorkID: req.PublicWorkId, Reason: req.Reason, NotifyAuthor: req.NotifyAuthor,
	})
	if err != nil {
		return nil, err
	}
	return takeDownPreviewToRPC(out), nil
}

func (h *Handler) ConfirmTakeDownWork(ctx context.Context, req *businessagent.ConfirmTakeDownPublicWorkRequest) (*businessagent.AdminPublicWorkDTO, error) {
	if h.Work == nil {
		return nil, bizerrors.NotImplemented("FeaturedWorkAdminService.ConfirmTakeDownWork")
	}
	if req == nil || req.AuthContext == nil || req.RequestMeta == nil {
		return nil, bizerrors.New(bizerrors.CodeInvalidArgument, "auth_context and request_meta are required")
	}
	out, err := h.Work.ConfirmTakeDownWork(ctx, work.ConfirmTakeDownWorkInput{
		Auth: adminAuthFromRPC(req.AuthContext), Meta: metaFromRPC(req.RequestMeta), PublicWorkID: req.PublicWorkId,
		PreviewToken: req.PreviewToken, Reason: req.Reason, NotifyAuthor: req.NotifyAuthor,
	})
	if err != nil {
		return nil, err
	}
	return adminPublicWorkToRPC(out), nil
}

func (h *Handler) ListPublicWorks(ctx context.Context, req *businessagent.ListPublicWorksRequest) (*businessagent.ListPublicWorksResponse, error) {
	if h.Work == nil {
		return nil, bizerrors.NotImplemented("PublicContentService.ListPublicWorks")
	}
	if req == nil {
		req = businessagent.NewListPublicWorksRequest()
	}
	out, err := h.Work.ListPublicWorks(ctx, work.ListPublicWorksInput{
		Category: req.GetCategory(), Tag: req.GetTag(), ResourceType: req.GetResourceType(),
		Limit: int(req.GetPageSize()), Offset: int(req.GetOffset()),
	})
	if err != nil {
		return nil, err
	}
	items := make([]*businessagent.PublicWorkCardDTO, 0, len(out.Items))
	for _, item := range out.Items {
		items = append(items, publicWorkCardToRPC(item))
	}
	return &businessagent.ListPublicWorksResponse{Items: items, Limit: int32(out.Limit), Offset: int32(out.Offset), Total: out.Total}, nil
}

func (h *Handler) GetPublicWork(ctx context.Context, req *businessagent.GetPublicWorkRequest) (*businessagent.PublicWorkDetailDTO, error) {
	if h.Work == nil {
		return nil, bizerrors.NotImplemented("PublicContentService.GetPublicWork")
	}
	if req == nil {
		return nil, bizerrors.New(bizerrors.CodeInvalidArgument, "request is required")
	}
	out, err := h.Work.GetPublicWork(ctx, work.GetPublicWorkInput{PublicWorkID: req.PublicWorkId})
	if err != nil {
		return nil, err
	}
	return publicWorkDetailToRPC(out), nil
}

func (h *Handler) CreateNotification(ctx context.Context, req *businessagent.CreateNotificationRequest) (*businessagent.NotificationDTO, error) {
	if h.Notify == nil {
		return nil, bizerrors.NotImplemented("NotificationService.CreateNotification")
	}
	if req == nil || req.AuthContext == nil || req.RequestMeta == nil {
		return nil, bizerrors.New(bizerrors.CodeInvalidArgument, "auth_context and request_meta are required")
	}
	out, err := h.Notify.CreateNotification(ctx, notification.CreateNotificationInput{
		RecipientUserID: req.RecipientUserId, Type: req.Type, Title: req.Title, Summary: req.Summary, Body: req.GetBody(),
		RelatedResourceType: req.GetRelatedResourceType(), RelatedResourceID: req.GetRelatedResourceId(),
		NavigationHint: stringMapToAny(req.NavigationHint), IdempotencyKey: req.RequestMeta.GetIdempotencyKey(), TraceID: req.RequestMeta.TraceId,
	})
	if err != nil {
		return nil, err
	}
	return notificationToRPC(out), nil
}

func (h *Handler) ListNotifications(ctx context.Context, req *businessagent.ListNotificationsRequest) (*businessagent.ListNotificationsResponse, error) {
	if h.Notify == nil {
		return nil, bizerrors.NotImplemented("NotificationService.ListNotifications")
	}
	if req == nil || req.AuthContext == nil {
		return nil, bizerrors.New(bizerrors.CodeInvalidArgument, "auth_context is required")
	}
	out, err := h.Notify.ListNotifications(ctx, authContextFromRPC(req.AuthContext), notification.ListInput{
		Type: req.GetType(), ReadStatus: req.GetReadState(), Limit: int(req.GetPageSize()), Offset: int(req.GetOffset()),
	})
	if err != nil {
		return nil, err
	}
	items := make([]*businessagent.NotificationDTO, 0, len(out.Items))
	for _, item := range out.Items {
		items = append(items, notificationToRPC(item))
	}
	return &businessagent.ListNotificationsResponse{Items: items, Limit: int32(out.Limit), Offset: int32(out.Offset), Total: out.Total}, nil
}

func (h *Handler) GetUnreadCount(ctx context.Context, req *businessagent.GetUnreadCountRequest) (*businessagent.UnreadCountDTO, error) {
	if h.Notify == nil {
		return nil, bizerrors.NotImplemented("NotificationService.GetUnreadCount")
	}
	if req == nil || req.AuthContext == nil {
		return nil, bizerrors.New(bizerrors.CodeInvalidArgument, "auth_context is required")
	}
	out, err := h.Notify.GetUnreadCount(ctx, authContextFromRPC(req.AuthContext))
	if err != nil {
		return nil, err
	}
	return &businessagent.UnreadCountDTO{UnreadCount: out.UnreadCount}, nil
}

func (h *Handler) MarkNotificationRead(ctx context.Context, req *businessagent.MarkNotificationReadRequest) (*businessagent.NotificationDTO, error) {
	if h.Notify == nil {
		return nil, bizerrors.NotImplemented("NotificationService.MarkNotificationRead")
	}
	if req == nil || req.AuthContext == nil || req.RequestMeta == nil {
		return nil, bizerrors.New(bizerrors.CodeInvalidArgument, "auth_context and request_meta are required")
	}
	out, err := h.Notify.MarkNotificationRead(ctx, authContextFromRPC(req.AuthContext), metaFromRPC(req.RequestMeta), req.NotificationId)
	if err != nil {
		return nil, err
	}
	return notificationToRPC(out), nil
}

func (h *Handler) MarkAllNotificationsRead(ctx context.Context, req *businessagent.MarkAllNotificationsReadRequest) (*businessagent.UnreadCountDTO, error) {
	if h.Notify == nil {
		return nil, bizerrors.NotImplemented("NotificationService.MarkAllNotificationsRead")
	}
	if req == nil || req.AuthContext == nil || req.RequestMeta == nil {
		return nil, bizerrors.New(bizerrors.CodeInvalidArgument, "auth_context and request_meta are required")
	}
	out, err := h.Notify.MarkAllNotificationsRead(ctx, authContextFromRPC(req.AuthContext), metaFromRPC(req.RequestMeta), req.GetType())
	if err != nil {
		return nil, err
	}
	return &businessagent.UnreadCountDTO{UnreadCount: out.UnreadCount}, nil
}

func adminAuthFromRPC(in *businessagent.AuthContext) admin.AdminAuth {
	if in == nil || in.LoginIdentityType != businessagent.LoginIdentityType_ADMIN {
		return admin.AdminAuth{}
	}
	return admin.AdminAuth{AdminID: in.GetAdminId(), Account: in.ActorUserId}
}

func enterpriseSummaryToRPC(in accountspace.EnterpriseSummaryDTO) *businessagent.EnterpriseSummaryDTO {
	return &businessagent.EnterpriseSummaryDTO{
		EnterpriseId: in.EnterpriseID, SpaceId: in.SpaceID, Name: in.Name, OwnerUserId: in.OwnerUserID,
		CurrentUserRole: optionalString(in.CurrentUserRole), Status: in.Status, MemberCount: in.MemberCount,
	}
}

func platformAdminToRPC(in admin.PlatformAdminDTO) *businessagent.PlatformAdminDTO {
	return &businessagent.PlatformAdminDTO{
		AdminId: in.AdminID, Account: in.Account, Status: in.Status,
		MustRotatePassword: in.MustRotatePassword, CreatedAt: formatTime(in.CreatedAt),
	}
}

func adminUserSummaryToRPC(in admin.UserSummaryDTO) *businessagent.AdminUserSummaryDTO {
	return &businessagent.AdminUserSummaryDTO{
		UserId: in.UserID, Status: in.Status, PublicNickname: in.PublicNickname,
		EmailMasked: optionalString(in.EmailMasked), PhoneMasked: optionalString(in.PhoneMasked),
		PersonalSpaceId: optionalString(in.PersonalSpaceID), RegisteredAt: formatTime(in.RegisteredAt),
		LastLoginAt: optionalTime(in.LastLoginAt),
	}
}

func projectDetailToRPC(in project.ProjectDetailDTO) *businessagent.ProjectDetailDTO {
	return &businessagent.ProjectDetailDTO{
		ProjectId: in.ProjectID, Title: in.Title, Description: optionalString(in.Description),
		CoverAssetId: optionalString(in.CoverAssetID), Status: in.Status, CreativeAllowed: in.CreativeAllowed,
		AllowedActions: in.AllowedActions, AgentSessionQueryRef: optionalString(in.AgentSessionQueryRef), UpdatedAt: formatTime(in.UpdatedAt),
	}
}

func projectAssetToRPC(in project.ProjectAssetDTO) *businessagent.ProjectAssetDTO {
	return &businessagent.ProjectAssetDTO{
		AssetId: in.AssetID, SourceType: in.SourceType, SourceSessionId: optionalString(in.SourceSessionID),
		SourceRunId: optionalString(in.SourceRunID), CreatedAt: formatTime(in.CreatedAt),
	}
}

func uploadIntentToRPC(in asset.UploadIntentDTO) *businessagent.UploadIntentDTO {
	return &businessagent.UploadIntentDTO{
		UploadIntentId: in.UploadIntentID, AssetId: in.AssetID, Bucket: in.Bucket, ObjectKey: in.ObjectKey,
		UploadUrl: in.UploadURL, UploadHeaders: in.UploadHeaders, ExpiresAt: formatTime(in.ExpiresAt),
		MaxSizeBytes: in.MaxSizeBytes, ContentType: in.ContentType,
	}
}

func assetDetailToRPC(in asset.AssetDetailDTO) *businessagent.AssetDetailDTO {
	return &businessagent.AssetDetailDTO{
		AssetId: in.Asset.AssetID, AssetType: in.Asset.AssetType, Status: in.Asset.Status,
		ProjectId: optionalString(in.Asset.ProjectID), PreviewUrl: optionalString(in.Asset.PreviewURL), AccessActions: in.AccessActions,
	}
}

func workDetailToRPC(in work.WorkDetailDTO) *businessagent.WorkDetailDTO {
	assetIDs := make([]string, 0, len(in.Assets))
	for _, item := range in.Assets {
		assetIDs = append(assetIDs, item.AssetID)
	}
	return &businessagent.WorkDetailDTO{
		WorkId: in.Work.WorkID, ProjectId: in.Work.ProjectID, Title: in.Work.Title,
		Description: optionalString(in.Work.Description), ShareStatus: in.Work.ShareStatus,
		CoverAssetId: optionalString(in.Work.CoverAssetID), Category: optionalString(in.Work.Category),
		Tags: in.Work.Tags, AssetIds: assetIDs, AllowedActions: in.AllowedActions, UpdatedAt: formatTime(in.Work.UpdatedAt),
	}
}

func sharePreviewToRPC(in work.SharePreviewDTO) *businessagent.ShareWorkPreviewDTO {
	media := make([]string, 0, len(in.PublicMediaSummary))
	for _, item := range in.PublicMediaSummary {
		media = append(media, item.PublicMediaID)
	}
	return &businessagent.ShareWorkPreviewDTO{
		PreviewToken: in.PreviewToken, WorkId: in.WorkID, PublicTitle: in.PublicTitle,
		PublicDescriptionDigest: in.PublicDescriptionDigest, Tags: in.Tags,
		PrivacyRedactionSummary: in.PrivacyRedactionSummary, PublicMediaSummary: media, ExpiresAt: formatTime(in.ExpiresAt),
	}
}

func takeDownPreviewToRPC(in work.TakeDownPublicWorkPreviewDTO) *businessagent.TakeDownPublicWorkPreviewDTO {
	return &businessagent.TakeDownPublicWorkPreviewDTO{
		PreviewToken: in.PreviewToken, PublicWorkId: in.PublicWorkID, WorkId: in.WorkID, CurrentStatus: in.CurrentStatus,
		ImpactItems: in.ImpactItems, PublicLinkWillBeInaccessible: in.PublicLinkWillBeInaccessible,
		SourceAssetRetained: in.SourceAssetRetained, NotifyAuthor: in.NotifyAuthor, ExpiresAt: formatTime(in.ExpiresAt),
	}
}

func adminPublicWorkToRPC(in work.AdminPublicWorkDTO) *businessagent.AdminPublicWorkDTO {
	return &businessagent.AdminPublicWorkDTO{
		PublicWorkId: in.PublicWorkID, WorkId: in.WorkID, Title: in.Title, Status: in.Status,
		PublishedAt: formatTime(in.PublishedAt), TakenDownAt: optionalTime(in.TakenDownAt),
		NotificationStatus: optionalString(in.NotificationStatus),
	}
}

func publicWorkCardToRPC(in work.PublicWorkCardDTO) *businessagent.PublicWorkCardDTO {
	return &businessagent.PublicWorkCardDTO{
		PublicWorkId: in.PublicWorkID, Title: in.Title, CoverUrl: optionalString(in.CoverURL), ShareUrl: in.ShareURL,
		Category: optionalString(in.Category), Tags: in.Tags, ResourceType: optionalString(in.ResourceType),
		LikeCount: in.LikeCount, PublishedAt: formatTime(in.PublishedAt),
	}
}

func publicWorkDetailToRPC(in work.PublicWorkDetailDTO) *businessagent.PublicWorkDetailDTO {
	refs := make([]map[string]string, 0, len(in.PublicMediaRefs))
	for _, item := range in.PublicMediaRefs {
		refs = append(refs, map[string]string{
			"public_media_id":  item.PublicMediaID,
			"resource_type":    item.ResourceType,
			"variant":          item.Variant,
			"public_media_url": item.PublicMediaURL,
		})
	}
	return &businessagent.PublicWorkDetailDTO{
		PublicWorkId: in.PublicWorkID, Title: in.Title, Description: optionalString(in.Description),
		ShareUrl: in.ShareURL, PublicMediaRefs: refs, AuthorDisplayName: in.AuthorDisplayName,
		Category: optionalString(in.Category), Tags: in.Tags, LikeCount: in.LikeCount, LikedByCurrentUser: in.LikedByCurrentUser,
	}
}

func notificationToRPC(in notification.NotificationDTO) *businessagent.NotificationDTO {
	return &businessagent.NotificationDTO{
		NotificationId: in.NotificationID, Type: in.Type, Title: in.Title, Summary: in.Summary,
		Body: optionalString(in.Body), NavigationHint: anyMapToString(in.NavigationHint), ReadAt: optionalTime(in.ReadAt),
		CreatedAt: formatTime(in.CreatedAt), RelatedResourceType: optionalString(in.RelatedResourceType),
		RelatedResourceId: optionalString(in.RelatedResourceID),
	}
}

func anyMapToString(in map[string]any) map[string]string {
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = fmt.Sprint(value)
	}
	return out
}

func stringMapToAny(in map[string]string) map[string]any {
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func optionalTime(value *time.Time) *string {
	if value == nil || value.IsZero() {
		return nil
	}
	formatted := formatTime(*value)
	return &formatted
}
