package http

import (
	"bytes"
	"io"
	nethttp "net/http"
	"strconv"
	"time"

	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/accountspace"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/admin"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/project"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/idempotency"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/logger"
	bizerrors "github.com/FigoGoo/Dora-Agent/services/business/internal/pkg/errors"
	"github.com/gin-gonic/gin"
)

func registerM2Routes(router *gin.Engine, opts RouterOptions) {
	h := m2Handler{account: opts.AccountSpace, admin: opts.Admin, project: opts.Project}
	router.POST("/api/auth/register", h.register)
	router.POST("/api/auth/login", h.login)
	router.POST("/api/auth/logout", h.userAuth(), h.logout)
	router.GET("/api/account/current-space", h.userAuth(), h.currentSpace)
	router.POST("/api/account/switch-identity", h.userAuth(), h.switchIdentity)

	router.POST("/api/enterprise/register", h.userAuth(), h.createEnterprise)
	router.GET("/api/enterprise/summary", h.userAuth(), h.enterpriseSummary)
	router.GET("/api/enterprise/members", h.userAuth(), h.enterpriseMembers)
	router.POST("/api/enterprise/invites", h.userAuth(), h.enterpriseInvite)
	router.POST("/api/enterprise/members/:member_id/remove/preview", h.userAuth(), h.previewRemoveMember)
	router.POST("/api/enterprise/members/:member_id/remove/confirm", h.userAuth(), h.confirmRemoveMember)
	router.POST("/api/enterprise/owner-transfer/preview", h.userAuth(), h.previewTransferOwner)
	router.POST("/api/enterprise/owner-transfer/confirm", h.userAuth(), h.confirmTransferOwner)

	router.GET("/api/projects", h.userAuth(), h.listProjects)
	router.POST("/api/projects", h.userAuth(), h.createProject)
	router.GET("/api/projects/:project_id", h.userAuth(), h.getProject)
	router.PATCH("/api/projects/:project_id", h.userAuth(), h.updateProject)
	router.POST("/api/projects/:project_id/archive", h.userAuth(), h.archiveProject)
	router.POST("/api/projects/:project_id/restore", h.userAuth(), h.restoreProject)
	router.GET("/api/projects/:project_id/assets", h.userAuth(), h.projectAssets)
	router.GET("/api/projects/:project_id/works", h.userAuth(), h.projectWorks)

	router.POST("/api/admin/auth/login", h.adminLogin)
	router.POST("/api/admin/auth/logout", h.adminAuth(false), h.adminLogout)
	router.POST("/api/admin/auth/rotate-password", h.adminAuth(true), h.rotateAdminPassword)
	router.GET("/api/admin/dashboard", h.adminAuth(false), h.adminDashboard)
	router.GET("/api/admin/admins", h.adminAuth(false), h.listAdmins)
	router.POST("/api/admin/admins", h.adminAuth(false), h.createAdmin)
	router.POST("/api/admin/admins/:admin_id/disable", h.adminAuth(false), h.disableAdmin)
	router.GET("/api/admin/users", h.adminAuth(false), h.adminUsers)
	router.GET("/api/admin/users/:user_id", h.adminAuth(false), h.adminUserDetail)
	router.POST("/api/admin/users/:user_id/status/preview", h.adminAuth(false), h.previewUserStatus)
	router.POST("/api/admin/users/:user_id/status/confirm", h.adminAuth(false), h.confirmUserStatus)
	router.GET("/api/admin/audit-logs", h.adminAuth(false), h.auditLogs)
}

type m2Handler struct {
	account *accountspace.App
	admin   *admin.App
	project *project.App
}

func (h m2Handler) register(c *gin.Context) {
	var req struct {
		Email       string `json:"email"`
		Phone       string `json:"phone"`
		Password    string `json:"password"`
		DisplayName string `json:"display_name"`
	}
	if !h.bind(c, &req) {
		return
	}
	out, err := h.account.RegisterPersonalAccount(c.Request.Context(), accountspace.RegisterInput{
		Email: req.Email, Phone: req.Phone, Password: req.Password, DisplayName: req.DisplayName, Meta: h.meta(c, true),
	})
	respond(c, out, err)
}

func (h m2Handler) login(c *gin.Context) {
	var req struct {
		LoginType    string `json:"login_type"`
		Account      string `json:"account"`
		Password     string `json:"password"`
		EnterpriseID string `json:"enterprise_id"`
	}
	if !h.bind(c, &req) {
		return
	}
	out, err := h.account.Login(c.Request.Context(), accountspace.LoginInput{
		LoginType: req.LoginType, Account: req.Account, Password: req.Password, EnterpriseID: req.EnterpriseID, Meta: h.meta(c, false),
	})
	respond(c, out, err)
}

func (h m2Handler) logout(c *gin.Context) {
	err := h.account.Logout(c.Request.Context(), userAuth(c), h.meta(c, true))
	respond(c, gin.H{"logged_out": err == nil}, err)
}

func (h m2Handler) currentSpace(c *gin.Context) {
	out, err := h.account.CurrentSpaceFromSession(c.Request.Context(), userAuth(c))
	respond(c, out, err)
}

func (h m2Handler) switchIdentity(c *gin.Context) {
	var req struct {
		TargetIdentityType string `json:"target_identity_type"`
		TargetEnterpriseID string `json:"target_enterprise_id"`
		EnterpriseID       string `json:"enterprise_id"`
	}
	if !h.bind(c, &req) {
		return
	}
	if req.TargetEnterpriseID == "" {
		req.TargetEnterpriseID = req.EnterpriseID
	}
	out, err := h.account.SwitchIdentity(c.Request.Context(), accountspace.SwitchIdentityInput{
		Auth: userAuth(c), TargetIdentityType: req.TargetIdentityType, TargetEnterpriseID: req.TargetEnterpriseID, Meta: h.meta(c, true),
	})
	respond(c, out, err)
}

func (h m2Handler) createEnterprise(c *gin.Context) {
	var req struct {
		EnterpriseName   string `json:"enterprise_name"`
		Name             string `json:"name"`
		OwnerDisplayName string `json:"owner_display_name"`
		ContactEmail     string `json:"contact_email"`
	}
	if !h.bind(c, &req) {
		return
	}
	if req.EnterpriseName == "" {
		req.EnterpriseName = req.Name
	}
	out, err := h.account.CreateEnterprise(c.Request.Context(), accountspace.CreateEnterpriseInput{
		Auth: userAuth(c), EnterpriseName: req.EnterpriseName, OwnerDisplayName: req.OwnerDisplayName, ContactEmail: req.ContactEmail, Meta: h.meta(c, true),
	})
	respond(c, out, err)
}

func (h m2Handler) enterpriseSummary(c *gin.Context) {
	out, err := h.account.GetEnterpriseSummary(c.Request.Context(), userAuth(c))
	respond(c, out, err)
}

func (h m2Handler) enterpriseMembers(c *gin.Context) {
	out, err := h.account.ListEnterpriseMembers(c.Request.Context(), userAuth(c), page(c))
	respond(c, out, err)
}

func (h m2Handler) enterpriseInvite(c *gin.Context) {
	var req struct {
		Email         string `json:"email"`
		Phone         string `json:"phone"`
		InviteMessage string `json:"invite_message"`
		ExpiresInDays int    `json:"expires_in_days"`
	}
	if !h.bind(c, &req) {
		return
	}
	out, err := h.account.CreateMemberInvite(c.Request.Context(), accountspace.InviteInput{
		Auth: userAuth(c), Email: req.Email, Phone: req.Phone, InviteMessage: req.InviteMessage, ExpiresInDays: req.ExpiresInDays, Meta: h.meta(c, true),
	})
	respond(c, out, err)
}

func (h m2Handler) previewRemoveMember(c *gin.Context) {
	var req struct {
		Reason string `json:"reason"`
	}
	_ = c.ShouldBindJSON(&req)
	out, err := h.account.PreviewRemoveMember(c.Request.Context(), accountspace.RemoveMemberInput{Auth: userAuth(c), MemberID: c.Param("member_id"), Reason: req.Reason})
	respond(c, out, err)
}

func (h m2Handler) confirmRemoveMember(c *gin.Context) {
	var req struct {
		Reason       string `json:"reason"`
		PreviewToken string `json:"preview_token"`
	}
	if !h.bind(c, &req) {
		return
	}
	out, err := h.account.ConfirmRemoveMember(c.Request.Context(), accountspace.RemoveMemberInput{
		Auth: userAuth(c), MemberID: c.Param("member_id"), Reason: req.Reason, PreviewToken: req.PreviewToken, Meta: h.meta(c, true),
	})
	respond(c, out, err)
}

func (h m2Handler) previewTransferOwner(c *gin.Context) {
	var req struct {
		TargetMemberID string `json:"target_member_id"`
	}
	if !h.bind(c, &req) {
		return
	}
	out, err := h.account.PreviewTransferOwner(c.Request.Context(), accountspace.TransferOwnerInput{Auth: userAuth(c), TargetMemberID: req.TargetMemberID})
	respond(c, out, err)
}

func (h m2Handler) confirmTransferOwner(c *gin.Context) {
	var req struct {
		TargetMemberID string `json:"target_member_id"`
		PreviewToken   string `json:"preview_token"`
		Reason         string `json:"reason"`
	}
	if !h.bind(c, &req) {
		return
	}
	out, err := h.account.ConfirmTransferOwner(c.Request.Context(), accountspace.TransferOwnerInput{
		Auth: userAuth(c), TargetMemberID: req.TargetMemberID, PreviewToken: req.PreviewToken, Reason: req.Reason, Meta: h.meta(c, true),
	})
	respond(c, out, err)
}

func (h m2Handler) listProjects(c *gin.Context) {
	out, err := h.project.ListProjects(c.Request.Context(), userAuth(c), project.PageRequest{Status: c.Query("status"), Limit: intQuery(c, "limit", 10), Offset: intQuery(c, "offset", 0)})
	respond(c, out, err)
}

func (h m2Handler) createProject(c *gin.Context) {
	var req struct {
		Title               string `json:"title"`
		InitialPromptDigest string `json:"initial_prompt_digest"`
		Source              string `json:"source"`
		SpaceID             string `json:"space_id"`
	}
	if !h.bind(c, &req) {
		return
	}
	out, err := h.project.CreateProject(c.Request.Context(), project.CreateInput{Auth: userAuth(c), Title: req.Title, InitialPromptDigest: req.InitialPromptDigest, Source: req.Source, SpaceID: req.SpaceID, Meta: h.meta(c, true)})
	respond(c, out, err)
}

func (h m2Handler) getProject(c *gin.Context) {
	out, err := h.project.GetProject(c.Request.Context(), userAuth(c), c.Param("project_id"))
	respond(c, out, err)
}

func (h m2Handler) updateProject(c *gin.Context) {
	var req struct {
		Title         *string `json:"title"`
		Description   *string `json:"description"`
		CoverAssetID  *string `json:"cover_asset_id"`
		BaseUpdatedAt string  `json:"base_updated_at"`
	}
	if !h.bind(c, &req) {
		return
	}
	out, err := h.project.UpdateProject(c.Request.Context(), project.UpdateInput{Auth: userAuth(c), ProjectID: c.Param("project_id"), Title: req.Title, Description: req.Description, CoverAssetID: req.CoverAssetID, BaseUpdatedAt: req.BaseUpdatedAt, Meta: h.meta(c, true)})
	respond(c, out, err)
}

func (h m2Handler) archiveProject(c *gin.Context) {
	var req struct {
		Reason string `json:"reason"`
	}
	_ = c.ShouldBindJSON(&req)
	out, err := h.project.ArchiveProject(c.Request.Context(), project.ArchiveInput{Auth: userAuth(c), ProjectID: c.Param("project_id"), Reason: req.Reason, Meta: h.meta(c, true)})
	respond(c, out, err)
}

func (h m2Handler) restoreProject(c *gin.Context) {
	var req struct {
		Reason string `json:"reason"`
	}
	_ = c.ShouldBindJSON(&req)
	out, err := h.project.RestoreProject(c.Request.Context(), project.ArchiveInput{Auth: userAuth(c), ProjectID: c.Param("project_id"), Reason: req.Reason, Meta: h.meta(c, true)})
	respond(c, out, err)
}

func (h m2Handler) projectAssets(c *gin.Context) {
	out, err := h.project.ListProjectAssets(c.Request.Context(), userAuth(c), c.Param("project_id"), project.PageRequest{Limit: intQuery(c, "limit", 10), Offset: intQuery(c, "offset", 0)})
	respond(c, out, err)
}

func (h m2Handler) projectWorks(c *gin.Context) {
	out, err := h.project.ListProjectWorks(c.Request.Context(), userAuth(c), c.Param("project_id"), project.PageRequest{Limit: intQuery(c, "limit", 10), Offset: intQuery(c, "offset", 0)})
	respond(c, out, err)
}

func (h m2Handler) adminLogin(c *gin.Context) {
	var req struct {
		Account  string `json:"account"`
		Password string `json:"password"`
	}
	if !h.bind(c, &req) {
		return
	}
	out, err := h.admin.Login(c.Request.Context(), admin.AdminLoginInput{Account: req.Account, Password: req.Password, Meta: h.meta(c, false)})
	respond(c, out, err)
}

func (h m2Handler) adminLogout(c *gin.Context) {
	err := h.admin.Logout(c.Request.Context(), adminAuth(c), h.meta(c, true))
	respond(c, gin.H{"logged_out": err == nil}, err)
}

func (h m2Handler) rotateAdminPassword(c *gin.Context) {
	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
		Reason          string `json:"reason"`
	}
	if !h.bind(c, &req) {
		return
	}
	out, err := h.admin.RotatePassword(c.Request.Context(), admin.RotatePasswordInput{Auth: adminAuth(c), CurrentPassword: req.CurrentPassword, NewPassword: req.NewPassword, Reason: req.Reason, Meta: h.meta(c, true)})
	respond(c, out, err)
}

func (h m2Handler) adminDashboard(c *gin.Context) {
	out, err := h.admin.Dashboard(c.Request.Context(), adminAuth(c))
	respond(c, out, err)
}

func (h m2Handler) listAdmins(c *gin.Context) {
	out, err := h.admin.ListAdmins(c.Request.Context(), adminAuth(c), adminPageLimit(c, 10), adminPageOffset(c))
	respond(c, out, err)
}

func (h m2Handler) createAdmin(c *gin.Context) {
	var req struct {
		Account         string `json:"account"`
		InitialPassword string `json:"initial_password"`
		Reason          string `json:"reason"`
	}
	if !h.bind(c, &req) {
		return
	}
	out, err := h.admin.CreateAdmin(c.Request.Context(), admin.CreateAdminInput{Auth: adminAuth(c), Account: req.Account, InitialPassword: req.InitialPassword, Reason: req.Reason, Meta: h.meta(c, true)})
	respond(c, out, err)
}

func (h m2Handler) disableAdmin(c *gin.Context) {
	var req struct {
		Reason string `json:"reason"`
	}
	if !h.bind(c, &req) {
		return
	}
	out, err := h.admin.DisableAdmin(c.Request.Context(), admin.DisableAdminInput{Auth: adminAuth(c), AdminID: c.Param("admin_id"), Reason: req.Reason, Meta: h.meta(c, true)})
	respond(c, out, err)
}

func (h m2Handler) adminUsers(c *gin.Context) {
	out, err := h.admin.ListUsers(c.Request.Context(), admin.ListUsersInput{Auth: adminAuth(c), Status: c.Query("status"), Limit: adminPageLimit(c, 10), Offset: adminPageOffset(c)})
	respond(c, out, err)
}

func (h m2Handler) adminUserDetail(c *gin.Context) {
	out, err := h.admin.GetUserSummary(c.Request.Context(), adminAuth(c), c.Param("user_id"))
	respond(c, out, err)
}

func (h m2Handler) previewUserStatus(c *gin.Context) {
	var req struct {
		TargetStatus string `json:"target_status"`
		Reason       string `json:"reason"`
	}
	if !h.bind(c, &req) {
		return
	}
	out, err := h.admin.PreviewSetUserStatus(c.Request.Context(), admin.UserStatusInput{Auth: adminAuth(c), UserID: c.Param("user_id"), TargetStatus: req.TargetStatus, Reason: req.Reason})
	respond(c, out, err)
}

func (h m2Handler) confirmUserStatus(c *gin.Context) {
	var req struct {
		TargetStatus string `json:"target_status"`
		PreviewToken string `json:"preview_token"`
		Reason       string `json:"reason"`
	}
	if !h.bind(c, &req) {
		return
	}
	out, err := h.admin.ConfirmSetUserStatus(c.Request.Context(), admin.UserStatusInput{Auth: adminAuth(c), UserID: c.Param("user_id"), TargetStatus: req.TargetStatus, PreviewToken: req.PreviewToken, Reason: req.Reason, Meta: h.meta(c, true)})
	respond(c, out, err)
}

func (h m2Handler) auditLogs(c *gin.Context) {
	out, err := h.admin.ListAuditLogs(c.Request.Context(), admin.AuditQueryInput{Auth: adminAuth(c), BusinessAction: c.Query("business_action"), TraceID: c.Query("trace_id"), Limit: adminPageLimit(c, 10), Offset: adminPageOffset(c)})
	respond(c, out, err)
}

func (h m2Handler) userAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		if h.account == nil {
			_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
			c.Abort()
			return
		}
		auth, err := h.account.AuthenticateToken(c.Request.Context(), c.GetHeader("Authorization"))
		if err != nil {
			_ = c.Error(err)
			c.Abort()
			return
		}
		c.Set("user_auth", auth)
		c.Next()
	}
}

func (h m2Handler) adminAuth(allowRotate bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		if h.admin == nil {
			_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
			c.Abort()
			return
		}
		auth, err := h.admin.AuthenticateToken(c.Request.Context(), c.GetHeader("Authorization"))
		if err != nil {
			_ = c.Error(err)
			c.Abort()
			return
		}
		if !auth.ExpiresAt.IsZero() {
			c.Header("X-Admin-Session-Expires-At", auth.ExpiresAt.Format(time.RFC3339Nano))
		}
		_ = allowRotate
		c.Set("admin_auth", auth)
		c.Next()
	}
}

func (h m2Handler) bind(c *gin.Context, out any) bool {
	body := readBody(c)
	c.Set("raw_body", body)
	if err := c.ShouldBindJSON(out); err != nil {
		_ = c.Error(bizerrors.New(bizerrors.CodeInvalidArgument, "invalid json request"))
		return false
	}
	return true
}

func (h m2Handler) meta(c *gin.Context, requireIdempotency bool) accountspace.RequestMeta {
	body, _ := c.Get("raw_body")
	rawBody, _ := body.([]byte)
	key := c.GetHeader("Idempotency-Key")
	if requireIdempotency && key == "" {
		_ = c.Error(bizerrors.New(bizerrors.CodeInvalidArgument, "Idempotency-Key is required"))
	}
	hash, _ := idempotency.HashRequest(idempotency.RequestHashInput{
		TenantID:    tenantID(c),
		SpaceID:     spaceID(c),
		ActorUserID: actorID(c),
		AdminID:     adminID(c),
		Body:        rawBody,
	})
	return accountspace.RequestMeta{
		TraceID: logger.TraceID(c.Request.Context()), RequestID: c.GetString("request_id"), IdempotencyKey: key,
		Source: "business_http", RequestHash: hash,
	}
}

func readBody(c *gin.Context) []byte {
	if c.Request.Body == nil {
		return nil
	}
	body, _ := io.ReadAll(c.Request.Body)
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	return body
}

func respond(c *gin.Context, data any, err error) {
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(nethttp.StatusOK, gin.H{"code": "OK", "message": "ok", "data": data, "trace_id": logger.TraceID(c.Request.Context())})
}

func userAuth(c *gin.Context) accountspace.AuthContext {
	value, _ := c.Get("user_auth")
	auth, _ := value.(accountspace.AuthContext)
	return auth
}

func adminAuth(c *gin.Context) admin.AdminAuth {
	value, _ := c.Get("admin_auth")
	auth, _ := value.(admin.AdminAuth)
	return auth
}

func page(c *gin.Context) accountspace.PageRequest {
	return accountspace.PageRequest{Limit: intQuery(c, "limit", 10), Offset: intQuery(c, "offset", 0)}
}

func intQuery(c *gin.Context, key string, fallback int) int {
	value := c.Query(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func adminPageLimit(c *gin.Context, fallback int) int {
	return intQuery(c, "limit", intQuery(c, "page_size", fallback))
}

func adminPageOffset(c *gin.Context) int {
	if c.Query("offset") != "" {
		return intQuery(c, "offset", 0)
	}
	return intQuery(c, "page_token", 0)
}

func tenantID(c *gin.Context) string {
	if auth := userAuth(c); auth.SpaceID != "" {
		return "space:" + auth.SpaceID
	}
	if auth := adminAuth(c); auth.AdminID != "" {
		return "admin:" + auth.AdminID
	}
	return "anonymous"
}

func spaceID(c *gin.Context) string {
	return userAuth(c).SpaceID
}

func actorID(c *gin.Context) string {
	return userAuth(c).UserID
}

func adminID(c *gin.Context) string {
	return adminAuth(c).AdminID
}
