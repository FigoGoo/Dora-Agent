package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/business/internal/auth"
	"github.com/FigoGoo/Dora-Agent/business/internal/skill"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type skillGovernanceHTTPService struct {
	listPrincipal   skill.GovernancePrincipal
	listStatus      string
	listCursor      string
	listResult      skill.GovernanceQueueResult
	listErr         error
	listCalls       int
	detailPrincipal skill.GovernancePrincipal
	detailSkillID   string
	detailResult    skill.GovernanceDetailDTO
	detailErr       error
	detailCalls     int
	decisionCommand skill.GovernanceDecisionCommand
	decisionResult  skill.GovernanceDecisionResult
	decisionErr     error
	decisionCalls   int
}

func (service *skillGovernanceHTTPService) ListGovernance(
	_ context.Context,
	principal skill.GovernancePrincipal,
	status string,
	cursor string,
) (skill.GovernanceQueueResult, error) {
	service.listCalls++
	service.listPrincipal = principal
	service.listStatus = status
	service.listCursor = cursor
	return service.listResult, service.listErr
}

func (service *skillGovernanceHTTPService) FindGovernanceDetail(
	_ context.Context,
	principal skill.GovernancePrincipal,
	skillID string,
) (skill.GovernanceDetailDTO, error) {
	service.detailCalls++
	service.detailPrincipal = principal
	service.detailSkillID = skillID
	return service.detailResult, service.detailErr
}

func (service *skillGovernanceHTTPService) DecideGovernance(
	_ context.Context,
	command skill.GovernanceDecisionCommand,
) (skill.GovernanceDecisionResult, error) {
	service.decisionCalls++
	service.decisionCommand = command
	return service.decisionResult, service.decisionErr
}

func mustSkillGovernanceHandlerForServerTest(t *testing.T) *SkillGovernanceHandler {
	t.Helper()
	handler, err := NewSkillGovernanceHandler(&skillGovernanceHTTPService{}, authHandlerTestIDs{})
	if err != nil {
		t.Fatalf("create server test skill governance handler: %v", err)
	}
	return handler
}

func newSkillGovernanceHandlerRouter(
	t *testing.T,
	service *skillGovernanceHTTPService,
	capabilities []string,
) (*gin.Engine, string, string, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	userID, _ := uuid.NewV7()
	skillID, _ := uuid.NewV7()
	requestID, _ := uuid.NewV7()
	handler, err := NewSkillGovernanceHandler(service, projectRequestIDs{value: requestID.String()})
	if err != nil {
		t.Fatalf("create skill governance handler: %v", err)
	}
	principalMiddleware := func(c *gin.Context) {
		principal := auth.Principal{ID: userID.String(), Capabilities: append([]string(nil), capabilities...)}
		c.Request = c.Request.WithContext(auth.ContextWithPrincipal(c.Request.Context(), principal))
		c.Next()
	}
	router := gin.New()
	handler.Register(router, principalMiddleware, principalMiddleware)
	return router, skillID.String(), userID.String(), requestID.String()
}

func TestSkillGovernanceHandlerListAndDetailUseTrustedGovernor(t *testing.T) {
	skillID, _ := uuid.NewV7()
	snapshotID, _ := uuid.NewV7()
	etag, err := skill.GovernanceETag(skillID.String(), snapshotID.String(), skill.GovernanceStatusActive, 1)
	if err != nil {
		t.Fatalf("create governance etag: %v", err)
	}
	publishedAt := time.Date(2026, 7, 14, 9, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	service := &skillGovernanceHTTPService{
		listResult: skill.GovernanceQueueResult{Items: []skill.GovernanceQueueItemDTO{{
			SkillID: skillID.String(), Name: "视频策划助手", Summary: "摘要", Category: "video",
			PublishedAt: publishedAt, GovernanceStatus: skill.GovernanceStatusActive,
			GovernanceEpoch: 1, AllowedActions: []string{"suspend", "offline"},
		}}, NextCursor: "next-page"},
		detailResult: skill.GovernanceDetailDTO{
			SkillID: skillID.String(), Definition: validSkillHTTPDefinition(), PublishedAt: publishedAt,
			GovernanceStatus: skill.GovernanceStatusActive, GovernanceEpoch: 1, GovernanceETag: etag,
			AllowedActions: []string{"suspend", "offline"},
		},
	}
	router, _, governorID, requestID := newSkillGovernanceHandlerRouter(t, service, []string{skill.GovernanceCapability})

	listRecorder := httptest.NewRecorder()
	router.ServeHTTP(listRecorder, httptest.NewRequest(http.MethodGet, "/api/v1/admin/skill-governance?status=active&cursor=cursor-1", nil))
	if listRecorder.Code != http.StatusOK || listRecorder.Header().Get("Cache-Control") != "no-store" ||
		service.listCalls != 1 || service.listPrincipal.UserID != governorID || service.listStatus != "active" || service.listCursor != "cursor-1" {
		t.Fatalf("list governance contract drifted: status=%d service=%+v body=%s", listRecorder.Code, service, listRecorder.Body.String())
	}
	var listResponse SkillGovernanceQueueResponse
	if err := json.Unmarshal(listRecorder.Body.Bytes(), &listResponse); err != nil || listResponse.RequestID != requestID ||
		len(listResponse.Items) != 1 || listResponse.NextCursor == nil || *listResponse.NextCursor != "next-page" {
		t.Fatalf("list response = %+v, err=%v", listResponse, err)
	}

	detailRecorder := httptest.NewRecorder()
	router.ServeHTTP(detailRecorder, httptest.NewRequest(http.MethodGet, "/api/v1/admin/skill-governance/"+skillID.String(), nil))
	if detailRecorder.Code != http.StatusOK || detailRecorder.Header().Get("ETag") != etag ||
		service.detailCalls != 1 || service.detailPrincipal.UserID != governorID || service.detailSkillID != skillID.String() {
		t.Fatalf("detail governance contract drifted: status=%d service=%+v headers=%v body=%s", detailRecorder.Code, service, detailRecorder.Header(), detailRecorder.Body.String())
	}
}

func TestSkillGovernanceHandlerDecisionFreezesPeerAndHeaders(t *testing.T) {
	skillID, _ := uuid.NewV7()
	snapshotID, _ := uuid.NewV7()
	oldETag, _ := skill.GovernanceETag(skillID.String(), snapshotID.String(), skill.GovernanceStatusActive, 1)
	newETag, _ := skill.GovernanceETag(skillID.String(), snapshotID.String(), skill.GovernanceStatusSuspended, 2)
	service := &skillGovernanceHTTPService{decisionResult: skill.GovernanceDecisionResult{Skill: skill.GovernanceDecisionDTO{
		SkillID: skillID.String(), GovernanceStatus: skill.GovernanceStatusSuspended, GovernanceEpoch: 2,
		TransitionedAt: "2026-07-14T09:10:00Z", GovernanceETag: newETag,
		AllowedActions: []string{"resume", "offline"},
	}}}
	router, _, governorID, requestID := newSkillGovernanceHandlerRouter(t, service, []string{skill.GovernanceCapability})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/skill-governance/"+skillID.String()+"/decisions",
		strings.NewReader(`{"action":"suspend","reason_code":"content_safety","approval_reference":"TICKET-123"}`))
	request.RemoteAddr = "[::ffff:192.0.2.8]:4321"
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("If-Match", oldETag)
	request.Header.Set("Idempotency-Key", "governance-intent-1")
	request.Header.Set("X-Forwarded-For", "203.0.113.99")
	request.Header.Set("X-Real-IP", "203.0.113.100")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	command := service.decisionCommand
	if recorder.Code != http.StatusOK || recorder.Header().Get("ETag") != newETag || recorder.Header().Get("Cache-Control") != "no-store" ||
		service.decisionCalls != 1 || command.Governor.UserID != governorID || command.SkillID != skillID.String() ||
		command.Action != "suspend" || command.ReasonCode != "content_safety" || command.ApprovalReference != "TICKET-123" ||
		command.SourceAddress != "192.0.2.8" || command.IfMatch != oldETag || command.IdempotencyKey != "governance-intent-1" ||
		command.RequestID != requestID {
		t.Fatalf("decision governance contract drifted: status=%d command=%+v headers=%v body=%s", recorder.Code, command, recorder.Header(), recorder.Body.String())
	}
}

func TestSkillGovernanceHandlerRejectsMalformedDecisionBeforeService(t *testing.T) {
	skillID, _ := uuid.NewV7()
	snapshotID, _ := uuid.NewV7()
	etag, _ := skill.GovernanceETag(skillID.String(), snapshotID.String(), skill.GovernanceStatusActive, 1)
	tests := []struct {
		name       string
		body       string
		content    string
		ifMatch    string
		key        string
		remoteAddr string
		pathSuffix string
	}{
		{name: "unknown field", body: `{"action":"suspend","reason_code":"content_safety","approval_reference":"TICKET-123","status":"offline"}`, content: "application/json", ifMatch: etag, key: "key", remoteAddr: "192.0.2.1:1000"},
		{name: "duplicate field", body: `{"action":"suspend","action":"offline","reason_code":"content_safety","approval_reference":"TICKET-123"}`, content: "application/json", ifMatch: etag, key: "key", remoteAddr: "192.0.2.1:1000"},
		{name: "weak etag", body: `{"action":"suspend","reason_code":"content_safety","approval_reference":"TICKET-123"}`, content: "application/json", ifMatch: "W/" + etag, key: "key", remoteAddr: "192.0.2.1:1000"},
		{name: "missing key", body: `{"action":"suspend","reason_code":"content_safety","approval_reference":"TICKET-123"}`, content: "application/json", ifMatch: etag, remoteAddr: "192.0.2.1:1000"},
		{name: "invalid peer", body: `{"action":"suspend","reason_code":"content_safety","approval_reference":"TICKET-123"}`, content: "application/json", ifMatch: etag, key: "key", remoteAddr: "not-a-peer"},
		{name: "query injection", body: `{"action":"suspend","reason_code":"content_safety","approval_reference":"TICKET-123"}`, content: "application/json", ifMatch: etag, key: "key", remoteAddr: "192.0.2.1:1000", pathSuffix: "?owner_id=injected"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := &skillGovernanceHTTPService{}
			router, _, _, _ := newSkillGovernanceHandlerRouter(t, service, []string{skill.GovernanceCapability})
			request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/skill-governance/"+skillID.String()+"/decisions"+test.pathSuffix, strings.NewReader(test.body))
			request.RemoteAddr = test.remoteAddr
			if test.content != "" {
				request.Header.Set("Content-Type", test.content)
			}
			if test.ifMatch != "" {
				request.Header.Set("If-Match", test.ifMatch)
			}
			if test.key != "" {
				request.Header.Set("Idempotency-Key", test.key)
			}
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusBadRequest || service.decisionCalls != 0 || recorder.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("malformed decision reached service: status=%d calls=%d body=%s", recorder.Code, service.decisionCalls, recorder.Body.String())
			}
		})
	}
}

func TestSkillGovernanceHandlerRequiresExactCapabilityAndMapsErrors(t *testing.T) {
	service := &skillGovernanceHTTPService{}
	router, skillID, _, _ := newSkillGovernanceHandlerRouter(t, service, []string{skill.ReviewCapability})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/admin/skill-governance/"+skillID, nil))
	if recorder.Code != http.StatusForbidden || service.detailCalls != 0 || !strings.Contains(recorder.Body.String(), "SKILL_GOVERNANCE_CAPABILITY_REQUIRED") {
		t.Fatalf("review capability crossed governance boundary: status=%d calls=%d body=%s", recorder.Code, service.detailCalls, recorder.Body.String())
	}

	for _, test := range []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{name: "not found", err: skill.ErrGovernanceNotFound, status: http.StatusNotFound, code: "SKILL_GOVERNANCE_NOT_FOUND"},
		{name: "conflict", err: skill.ErrGovernanceConflict, status: http.StatusConflict, code: "SKILL_GOVERNANCE_CONFLICT"},
		{name: "persistence", err: errors.New("postgres password=secret"), status: http.StatusServiceUnavailable, code: "SKILL_PERSISTENCE_UNAVAILABLE"},
	} {
		t.Run(test.name, func(t *testing.T) {
			mappedService := &skillGovernanceHTTPService{detailErr: test.err}
			mappedRouter, mappedSkillID, _, _ := newSkillGovernanceHandlerRouter(t, mappedService, []string{skill.GovernanceCapability})
			mappedRecorder := httptest.NewRecorder()
			mappedRouter.ServeHTTP(mappedRecorder, httptest.NewRequest(http.MethodGet, "/api/v1/admin/skill-governance/"+mappedSkillID, nil))
			if mappedRecorder.Code != test.status || !strings.Contains(mappedRecorder.Body.String(), test.code) || strings.Contains(mappedRecorder.Body.String(), "password") {
				t.Fatalf("mapped error drifted: status=%d body=%s", mappedRecorder.Code, mappedRecorder.Body.String())
			}
		})
	}
}

func TestSkillGovernanceHandlerRejectsInvalidRequestIDBeforeService(t *testing.T) {
	service := &skillGovernanceHTTPService{}
	handler, err := NewSkillGovernanceHandler(service, projectRequestIDs{value: "not-a-uuid"})
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	handler.Register(router, func(c *gin.Context) {
		userID, _ := uuid.NewV7()
		principal := auth.Principal{ID: userID.String(), Capabilities: []string{skill.GovernanceCapability}}
		c.Request = c.Request.WithContext(auth.ContextWithPrincipal(c.Request.Context(), principal))
		c.Next()
	}, func(c *gin.Context) { c.Next() })

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/admin/skill-governance?status=active", nil))
	if recorder.Code != http.StatusServiceUnavailable || service.listCalls != 0 ||
		!strings.Contains(recorder.Body.String(), skillEmergencyRequestID) {
		t.Fatalf("invalid request ID crossed governance boundary: status=%d calls=%d body=%s", recorder.Code, service.listCalls, recorder.Body.String())
	}
}
