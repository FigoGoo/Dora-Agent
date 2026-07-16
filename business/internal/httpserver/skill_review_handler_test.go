package httpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/FigoGoo/Dora-Agent/business/internal/auth"
	"github.com/FigoGoo/Dora-Agent/business/internal/skill"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type skillReviewHTTPService struct {
	listPrincipal   skill.ReviewerPrincipal
	listStatus      string
	listCursor      string
	listResult      skill.ReviewerQueueResult
	listErr         error
	detailPrincipal skill.ReviewerPrincipal
	detailReviewID  string
	detailResult    skill.ReviewerDetailDTO
	detailErr       error
	decisionCommand skill.ApproveAndPublishServiceCommand
	decisionResult  skill.ApproveAndPublishResult
	decisionErr     error
}

func (service *skillReviewHTTPService) ListReviewQueue(_ context.Context, principal skill.ReviewerPrincipal, status string, cursor string) (skill.ReviewerQueueResult, error) {
	service.listPrincipal, service.listStatus, service.listCursor = principal, status, cursor
	return service.listResult, service.listErr
}

func (service *skillReviewHTTPService) FindReviewDetail(_ context.Context, principal skill.ReviewerPrincipal, reviewID string) (skill.ReviewerDetailDTO, error) {
	service.detailPrincipal, service.detailReviewID = principal, reviewID
	return service.detailResult, service.detailErr
}

func (service *skillReviewHTTPService) ApproveAndPublish(_ context.Context, command skill.ApproveAndPublishServiceCommand) (skill.ApproveAndPublishResult, error) {
	service.decisionCommand = command
	return service.decisionResult, service.decisionErr
}

func newSkillReviewHandlerRouter(t *testing.T, service *skillReviewHTTPService, capabilities []string) (*gin.Engine, string, string, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	userID, _ := uuid.NewV7()
	reviewID, _ := uuid.NewV7()
	requestID, _ := uuid.NewV7()
	handler, err := NewSkillReviewHandler(service, projectRequestIDs{value: requestID.String()})
	if err != nil {
		t.Fatal(err)
	}
	principalMiddleware := func(c *gin.Context) {
		principal := auth.Principal{ID: userID.String(), Capabilities: append([]string(nil), capabilities...)}
		c.Request = c.Request.WithContext(auth.ContextWithPrincipal(c.Request.Context(), principal))
		c.Next()
	}
	router := gin.New()
	handler.Register(router, principalMiddleware, principalMiddleware)
	return router, reviewID.String(), userID.String(), requestID.String()
}

func TestSkillReviewHandlerListRequiresExactReviewingQuery(t *testing.T) {
	service := &skillReviewHTTPService{listResult: skill.ReviewerQueueResult{Items: []skill.ReviewerQueueItemDTO{}}}
	router, _, reviewerID, _ := newSkillReviewHandlerRouter(t, service, []string{skill.ReviewCapability})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/admin/skill-reviews?status=reviewing&cursor=opaque", nil))
	if recorder.Code != http.StatusOK || service.listPrincipal.UserID != reviewerID || service.listStatus != "reviewing" || service.listCursor != "opaque" ||
		!strings.Contains(recorder.Body.String(), `"items":[]`) || !strings.Contains(recorder.Body.String(), `"next_cursor":null`) {
		t.Fatalf("invalid list contract: status=%d principal=%+v body=%s", recorder.Code, service.listPrincipal, recorder.Body.String())
	}

	for _, query := range []string{"", "?status=approved", "?status=reviewing&status=reviewing", "?status=reviewing&unknown=1"} {
		service.listPrincipal = skill.ReviewerPrincipal{}
		recorder = httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/admin/skill-reviews"+query, nil))
		if recorder.Code != http.StatusBadRequest || service.listPrincipal.UserID != "" {
			t.Fatalf("invalid query reached service: query=%q status=%d", query, recorder.Code)
		}
	}
}

func TestSkillReviewHandlerDetailWritesMatchingStrongETagWithoutHTMLEscape(t *testing.T) {
	definition := validSkillHTTPDefinition()
	definition.Summary = "大量字符 <>& 保持 Canonical 输出"
	etag := `"sr1-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"`
	service := &skillReviewHTTPService{detailResult: skill.ReviewerDetailDTO{
		ReviewID: "review", SkillID: "skill", OwnerUserID: "owner", Status: skill.ReviewStatusReviewing,
		Definition: definition, CurrentPublished: nil, Comparison: skill.ReviewerComparisonDTO{}, ReviewETag: etag,
		AllowedActions: []string{skill.CommandTypeApproveAndPublish},
	}}
	router, reviewID, _, _ := newSkillReviewHandlerRouter(t, service, []string{skill.ReviewCapability})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/admin/skill-reviews/"+reviewID, nil))
	if recorder.Code != http.StatusOK || recorder.Header().Get("ETag") != etag || service.detailReviewID != reviewID ||
		!strings.Contains(recorder.Body.String(), "<>&") || strings.Contains(recorder.Body.String(), `\u003c`) ||
		!strings.Contains(recorder.Body.String(), `"current_published":null`) {
		t.Fatalf("detail encoding drifted: status=%d etag=%q body=%s", recorder.Code, recorder.Header().Get("ETag"), recorder.Body.String())
	}
}

func TestSkillReviewHandlerDecisionUsesTrustedPrincipalAndCurrentRequestID(t *testing.T) {
	etag := `"sr1-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"`
	service := &skillReviewHTTPService{decisionResult: skill.ApproveAndPublishResult{Review: skill.ReviewerDecisionDTO{
		ReviewID: "review", SkillID: "skill", Status: skill.ReviewStatusApproved,
		PublishedSnapshotID: "snapshot", DecidedAt: "2026-07-14T05:00:00Z", AllowedActions: []string{},
	}}}
	router, reviewID, reviewerID, requestID := newSkillReviewHandlerRouter(t, service, []string{skill.ReviewCapability})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/skill-reviews/"+reviewID+"/decisions", strings.NewReader(`{"decision":"approved"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("If-Match", etag)
	request.Header.Set("Idempotency-Key", "review-decision-1")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	command := service.decisionCommand
	if recorder.Code != http.StatusOK || command.Reviewer.UserID != reviewerID || command.ReviewID != reviewID ||
		command.RequestID != requestID || command.IfMatch != etag || command.Decision != "approved" ||
		!strings.Contains(recorder.Body.String(), `"request_id":"`+requestID+`"`) || !strings.Contains(recorder.Body.String(), `"allowed_actions":[]`) {
		t.Fatalf("decision contract drifted: status=%d command=%+v body=%s", recorder.Code, command, recorder.Body.String())
	}
}

func TestSkillReviewHandlerRejectsDuplicateIfMatchBeforeService(t *testing.T) {
	etag := `"sr1-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"`
	service := &skillReviewHTTPService{}
	router, reviewID, _, _ := newSkillReviewHandlerRouter(t, service, []string{skill.ReviewCapability})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/skill-reviews/"+reviewID+"/decisions", strings.NewReader(`{"decision":"approved"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Add("If-Match", etag)
	request.Header.Add("If-Match", etag)
	request.Header.Set("Idempotency-Key", "review-decision-2")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest || service.decisionCommand.ReviewID != "" || !strings.Contains(recorder.Body.String(), `"code":"INVALID_REQUEST"`) {
		t.Fatalf("duplicate If-Match reached service: status=%d command=%+v body=%s", recorder.Code, service.decisionCommand, recorder.Body.String())
	}
}

func TestSkillReviewHandlerRejectsDuplicateDecisionKeyBeforeService(t *testing.T) {
	etag := `"sr1-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"`
	service := &skillReviewHTTPService{}
	router, reviewID, _, _ := newSkillReviewHandlerRouter(t, service, []string{skill.ReviewCapability})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/skill-reviews/"+reviewID+"/decisions", strings.NewReader(`{"decision":"rejected","decision":"approved"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("If-Match", etag)
	request.Header.Set("Idempotency-Key", "review-decision-duplicate-json-key")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest || service.decisionCommand.ReviewID != "" || !strings.Contains(recorder.Body.String(), `"code":"INVALID_REQUEST"`) {
		t.Fatalf("duplicate decision key reached service: status=%d command=%+v body=%s", recorder.Code, service.decisionCommand, recorder.Body.String())
	}
}

func TestSkillReviewHandlerDeniesMissingCapabilityBeforeService(t *testing.T) {
	service := &skillReviewHTTPService{}
	router, _, _, _ := newSkillReviewHandlerRouter(t, service, nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/admin/skill-reviews?status=reviewing", nil))
	if recorder.Code != http.StatusForbidden || service.listStatus != "" || !strings.Contains(recorder.Body.String(), `"code":"SKILL_REVIEW_CAPABILITY_REQUIRED"`) {
		t.Fatalf("capability denial drifted: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestSkillReviewHandlerAuditsTransactionalRevocationDenialWithoutCommandSecrets(t *testing.T) {
	const sensitiveKey = "sensitive-review-idempotency-key"
	etag := `"sr1-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"`
	service := &skillReviewHTTPService{decisionErr: skill.ErrReviewCapabilityRequired}
	router, reviewID, reviewerID, requestID := newSkillReviewHandlerRouter(t, service, []string{skill.ReviewCapability})
	logs := captureHTTPTestLogs(t)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/skill-reviews/"+reviewID+"/decisions", strings.NewReader(`{"decision":"approved"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("If-Match", etag)
	request.Header.Set("Idempotency-Key", sensitiveKey)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusForbidden || !strings.Contains(logs.String(), `"event_type":"security.authorization.v1"`) ||
		!strings.Contains(logs.String(), `"route":"/api/v1/admin/skill-reviews/:review_id/decisions"`) ||
		!strings.Contains(logs.String(), `"action":"approve_and_publish"`) ||
		!strings.Contains(logs.String(), `"actor_id":"`+reviewerID+`"`) ||
		!strings.Contains(logs.String(), `"request_id":"`+requestID+`"`) ||
		!strings.Contains(logs.String(), `"error_code":"SKILL_REVIEW_CAPABILITY_REQUIRED"`) ||
		strings.Contains(logs.String(), sensitiveKey) || strings.Contains(logs.String(), etag) {
		t.Fatalf("transactional capability denial audit drifted or leaked command data: status=%d logs=%s", recorder.Code, logs.String())
	}
}
