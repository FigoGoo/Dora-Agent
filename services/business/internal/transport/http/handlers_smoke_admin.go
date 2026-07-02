package http

import (
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/smoke"
	bizerrors "github.com/FigoGoo/Dora-Agent/services/business/internal/pkg/errors"
	"github.com/gin-gonic/gin"
)

func registerSmokeAdminRoutes(router *gin.Engine, opts RouterOptions) {
	auth := accountProjectAdminHandler{account: opts.AccountSpace, admin: opts.Admin, project: opts.Project}
	h := smokeAdminHandler{smoke: opts.Smoke, auth: auth}

	router.GET("/api/admin/feature-flags", auth.adminAuth(false), h.listFeatureFlags)
	router.POST("/api/admin/feature-flags/:flag_key", auth.adminAuth(false), requireIdempotency(), h.updateFeatureFlag)
	router.GET("/api/admin/fake-provider/tasks", auth.adminAuth(false), h.listFakeProviderTasks)
	router.POST("/api/admin/smoke/seed", auth.adminAuth(false), requireIdempotency(), h.runSmokeSeed)
	router.POST("/api/admin/smoke/runs", auth.adminAuth(false), requireIdempotency(), h.runSmokeSuite)
	router.GET("/api/admin/smoke/runs/:run_id", auth.adminAuth(false), h.getSmokeRunResult)
}

type smokeAdminHandler struct {
	smoke *smoke.App
	auth  accountProjectAdminHandler
}

func (h smokeAdminHandler) listFeatureFlags(c *gin.Context) {
	if h.smoke == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	out, err := h.smoke.ListFeatureFlags(c.Request.Context(), adminAuth(c), adminPageLimit(c, 10), adminPageOffset(c))
	respond(c, out, err)
}

func (h smokeAdminHandler) updateFeatureFlag(c *gin.Context) {
	if h.smoke == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	var req struct {
		Enabled     bool   `json:"enabled"`
		Description string `json:"description"`
	}
	if !h.auth.bind(c, &req) {
		return
	}
	meta := h.auth.meta(c, true)
	out, err := h.smoke.UpdateFeatureFlag(c.Request.Context(), smoke.UpdateFeatureFlagInput{
		Auth: adminAuth(c), Meta: meta, FlagKey: c.Param("flag_key"), Enabled: req.Enabled, Description: req.Description,
	})
	respond(c, out, err)
}

func (h smokeAdminHandler) listFakeProviderTasks(c *gin.Context) {
	if h.smoke == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	out, err := h.smoke.ListFakeProviderTasks(c.Request.Context(), adminAuth(c), c.Query("provider_key"), c.Query("scenario"), adminPageLimit(c, 10), adminPageOffset(c))
	respond(c, out, err)
}

func (h smokeAdminHandler) runSmokeSeed(c *gin.Context) {
	if h.smoke == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	var req struct {
	}
	if !h.auth.bind(c, &req) {
		return
	}
	meta := h.auth.meta(c, true)
	out, err := h.smoke.RunSmokeSeed(c.Request.Context(), smoke.SmokeRunInput{Auth: adminAuth(c), Meta: meta})
	respond(c, out, err)
}

func (h smokeAdminHandler) runSmokeSuite(c *gin.Context) {
	if h.smoke == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	var req struct {
		SuiteKey string `json:"suite_key"`
	}
	if !h.auth.bind(c, &req) {
		return
	}
	meta := h.auth.meta(c, true)
	out, err := h.smoke.RunSmokeSuite(c.Request.Context(), smoke.SmokeRunInput{Auth: adminAuth(c), Meta: meta, SuiteKey: req.SuiteKey})
	respond(c, out, err)
}

func (h smokeAdminHandler) getSmokeRunResult(c *gin.Context) {
	if h.smoke == nil {
		_ = c.Error(bizerrors.NotImplemented(c.FullPath()))
		return
	}
	out, err := h.smoke.GetSmokeRunResult(c.Request.Context(), adminAuth(c), c.Param("run_id"))
	respond(c, out, err)
}
