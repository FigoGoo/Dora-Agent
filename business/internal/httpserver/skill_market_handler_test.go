package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/FigoGoo/Dora-Agent/business/internal/skill"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type skillMarketHTTPService struct {
	listCursor  string
	listResult  skill.MarketListResult
	listErr     error
	listCalls   int
	detailID    string
	detail      skill.MarketDetailDTO
	detailErr   error
	detailCalls int
}

func (service *skillMarketHTTPService) ListPublished(_ context.Context, cursor string) (skill.MarketListResult, error) {
	service.listCalls++
	service.listCursor = cursor
	return service.listResult, service.listErr
}

func (service *skillMarketHTTPService) FindPublishedByID(_ context.Context, skillID string) (skill.MarketDetailDTO, error) {
	service.detailCalls++
	service.detailID = skillID
	return service.detail, service.detailErr
}

func mustSkillMarketHandlerForServerTest(t *testing.T) *SkillMarketHandler {
	t.Helper()
	handler, err := NewSkillMarketHandler(&skillMarketHTTPService{}, authHandlerTestIDs{})
	if err != nil {
		t.Fatalf("create server test Skill Market handler: %v", err)
	}
	return handler
}

func newSkillMarketRouter(t *testing.T, service *skillMarketHTTPService) (*gin.Engine, string, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	skillID, _ := uuid.NewV7()
	requestID, _ := uuid.NewV7()
	handler, err := NewSkillMarketHandler(service, projectRequestIDs{value: requestID.String()})
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	handler.Register(router)
	return router, skillID.String(), requestID.String()
}

func TestSkillMarketHandlerAnonymousListAndDetail(t *testing.T) {
	itemID, _ := uuid.NewV7()
	publisherID, _ := uuid.NewV7()
	listItem := skill.MarketListItemDTO{
		SkillID: itemID.String(), Name: "公开 Skill", Summary: "摘要", Category: "视频", Tags: []string{},
		Publisher:   skill.MarketPublisherDTO{PublisherID: publisherID.String(), DisplayName: "Creator"},
		PublishedAt: "2026-07-14T10:00:00Z", DeclaredCapabilityKeys: []string{"write_prompts"},
	}
	service := &skillMarketHTTPService{
		listResult: skill.MarketListResult{Items: []skill.MarketListItemDTO{listItem}, NextCursor: "next-page"},
		detail: skill.MarketDetailDTO{
			SkillID: itemID.String(), Name: listItem.Name, Summary: listItem.Summary, Category: listItem.Category,
			Tags: []string{}, Publisher: listItem.Publisher, PublishedAt: listItem.PublishedAt,
			DeclaredCapabilityKeys: []string{"write_prompts"}, Examples: []skill.MarketExampleDTO{}, StarterPrompts: []string{},
		},
	}
	router, _, requestID := newSkillMarketRouter(t, service)

	listRequest := httptest.NewRequest(http.MethodGet, "/api/v1/skill-market?cursor=page-one", nil)
	listRequest.Header.Set("Cookie", "dora_session=ignored")
	listRecorder := httptest.NewRecorder()
	router.ServeHTTP(listRecorder, listRequest)
	var listResponse SkillMarketListResponse
	if err := json.Unmarshal(listRecorder.Body.Bytes(), &listResponse); err != nil || listRecorder.Code != http.StatusOK ||
		listRecorder.Header().Get("Cache-Control") != "no-store" || service.listCalls != 1 || service.listCursor != "page-one" ||
		listResponse.RequestID != requestID || listResponse.NextCursor == nil || len(listResponse.Items) != 1 {
		t.Fatalf("Market list response=%+v service=%+v status=%d err=%v", listResponse, service, listRecorder.Code, err)
	}
	for _, forbidden := range []string{"snapshot", "digest", "governance", "guidance", "invocation_rules"} {
		if strings.Contains(listRecorder.Body.String(), forbidden) {
			t.Fatalf("Market list leaked %q: %s", forbidden, listRecorder.Body.String())
		}
	}

	detailRecorder := httptest.NewRecorder()
	router.ServeHTTP(detailRecorder, httptest.NewRequest(http.MethodGet, "/api/v1/skill-market/"+itemID.String(), nil))
	if detailRecorder.Code != http.StatusOK || service.detailCalls != 1 || service.detailID != itemID.String() ||
		detailRecorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Market detail service=%+v status=%d body=%s", service, detailRecorder.Code, detailRecorder.Body.String())
	}
}

func TestSkillMarketHandlerRejectsInvalidTransportBeforeService(t *testing.T) {
	service := &skillMarketHTTPService{}
	router, skillID, _ := newSkillMarketRouter(t, service)
	paths := []string{
		"/api/v1/skill-market?cursor=",
		"/api/v1/skill-market?cursor=a&cursor=b",
		"/api/v1/skill-market?cursor=a;b",
		"/api/v1/skill-market?status=active",
		"/api/v1/skill-market/not-a-uuid",
		"/api/v1/skill-market/" + skillID + "?cursor=x",
	}
	for _, path := range paths {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusBadRequest || recorder.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("invalid Market path %q status=%d body=%s", path, recorder.Code, recorder.Body.String())
		}
	}
	if service.listCalls != 0 || service.detailCalls != 0 {
		t.Fatalf("invalid Market transport reached service: %+v", service)
	}
}

func TestSkillMarketHandlerMapsNotFoundAndPersistence(t *testing.T) {
	service := &skillMarketHTTPService{detailErr: skill.ErrMarketNotFound, listErr: errors.New("database detail")}
	router, skillID, _ := newSkillMarketRouter(t, service)
	detailRecorder := httptest.NewRecorder()
	router.ServeHTTP(detailRecorder, httptest.NewRequest(http.MethodGet, "/api/v1/skill-market/"+skillID, nil))
	if detailRecorder.Code != http.StatusNotFound || detailRecorder.Header().Get("Cache-Control") != "no-store" ||
		!strings.Contains(detailRecorder.Body.String(), "SKILL_MARKET_NOT_FOUND") {
		t.Fatalf("Market not-found status=%d body=%s", detailRecorder.Code, detailRecorder.Body.String())
	}
	listRecorder := httptest.NewRecorder()
	router.ServeHTTP(listRecorder, httptest.NewRequest(http.MethodGet, "/api/v1/skill-market", nil))
	if listRecorder.Code != http.StatusServiceUnavailable || listRecorder.Header().Get("Cache-Control") != "no-store" ||
		!strings.Contains(listRecorder.Body.String(), "SKILL_PERSISTENCE_UNAVAILABLE") {
		t.Fatalf("Market persistence status=%d body=%s", listRecorder.Code, listRecorder.Body.String())
	}
}
