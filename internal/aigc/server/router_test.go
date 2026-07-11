package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/gin-gonic/gin"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/a2ui"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/asset"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/generation"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/session"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/skill"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/storyboard"
	aigctools "github.com/FigoGoo/Dora-Agent/internal/aigc/tools"
)

func assistantCardEnvelope(t testing.TB, cardID string, text string) string {
	t.Helper()
	raw, err := json.Marshal(a2ui.ActionEnvelope{
		Version: a2ui.Version1,
		Actions: []a2ui.Action{{
			Type:    a2ui.ActionAppendCard,
			Surface: "chat",
			CardID:  cardID,
			Card: &a2ui.Card{
				Root:  "root",
				Title: "Agent",
				Components: []a2ui.Component{
					a2ui.CardContainer("root", []string{"content"}),
					a2ui.Text("content", text, "", ""),
				},
			},
		}},
	})
	if err != nil {
		t.Fatalf("marshal assistant card envelope: %v", err)
	}
	return string(raw)
}

func requirePublishedEvent(t testing.TB, broker *fakeEventSubscriber, eventName string) a2ui.SSEEvent {
	t.Helper()
	for _, event := range broker.published {
		if event.Event == eventName {
			return event
		}
	}
	t.Fatalf("missing published event %q, events = %#v", eventName, broker.published)
	return a2ui.SSEEvent{}
}

func eventOfType(events []a2ui.SSEEvent, eventName string) *a2ui.SSEEvent {
	for index := range events {
		if events[index].Event == eventName {
			return &events[index]
		}
	}
	return nil
}

func publishedPayloadString(t testing.TB, event a2ui.SSEEvent) string {
	t.Helper()
	raw, err := json.Marshal(event.Payload)
	if err != nil {
		t.Fatalf("marshal event payload: %v", err)
	}
	return string(raw)
}

func TestCreateSession(t *testing.T) {
	gin.SetMode(gin.TestMode)

	store := newFakeSessionStore()
	router := NewRouter(Config{
		Store:   store,
		Invoker: &fakeAgentInvoker{},
		NewID:   sequentialIDs("session-1"),
		Now:     fixedNow,
	})

	body := bytes.NewBufferString(`{"user_id":"u1","skill_id":"skill-video","title":"武侠短片"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/aigc/sessions", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var got session.SessionRecord
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if got.ID != "session-1" {
		t.Fatalf("session id = %q", got.ID)
	}
	if got.Status != "active" {
		t.Fatalf("status = %q", got.Status)
	}

	saved, ok := store.sessions["session-1"]
	if !ok {
		t.Fatal("session was not saved")
	}
	if saved.UserID != "u1" || saved.SkillID != "skill-video" {
		t.Fatalf("saved session = %#v", saved)
	}
}

func TestManualCompensationRequiresAdminTokenAndReturnsPublicJob(t *testing.T) {
	gin.SetMode(gin.TestMode)
	finalizer := &fakeCompensationFinalizer{job: generation.GenerationJob{
		ID: "job-1", UserID: "secret-user", Status: generation.StatusFailed,
		BillingTransactionID: "charge-secret", ErrorCode: "compensation_failed", ErrorMessage: "provider secret body",
	}}
	router := NewRouter(Config{Compensation: finalizer, AdminToken: "admin-secret"})
	body := `{"refunded_points":0}`
	unauthorized := httptest.NewRecorder()
	router.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodPost, "/api/aigc/admin/generation/jobs/job-1/compensation/finalize", strings.NewReader(body)))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", unauthorized.Code)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/aigc/admin/generation/jobs/job-1/compensation/finalize", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer admin-secret")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "secret-user") || strings.Contains(response.Body.String(), "charge-secret") || strings.Contains(response.Body.String(), "provider secret body") {
		t.Fatalf("response leaked internal job fields: %s", response.Body.String())
	}
}

func TestBindSkillToSession(t *testing.T) {
	gin.SetMode(gin.TestMode)

	store := newFakeSessionStore()
	store.sessions["s1"] = session.SessionRecord{ID: "s1", UserID: "u1", Title: "武侠短片", Status: "active", CreatedAt: fixedNow()}
	skills := &fakeSkillStore{
		records: map[string]skill.SkillRecord{
			"skill-video": {ID: "skill-video", Name: "武侠短片", Enabled: true},
		},
	}
	router := NewRouter(Config{
		Store:   store,
		Skills:  skills,
		Invoker: &fakeAgentInvoker{},
		Now:     fixedNow,
	})

	body := bytes.NewBufferString(`{"skill_id":"skill-video"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/aigc/sessions/s1/skill", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got session.SessionRecord
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if got.ID != "s1" || got.SkillID != "skill-video" || got.UserID != "u1" {
		t.Fatalf("response session = %#v", got)
	}
	if saved := store.sessions["s1"]; saved.SkillID != "skill-video" || saved.Title != "武侠短片" {
		t.Fatalf("saved session = %#v", saved)
	}
}

func TestUploadAssetStoresMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)

	store := newFakeSessionStore()
	store.sessions["s1"] = session.SessionRecord{ID: "s1", UserID: "u1", Status: "active"}
	assets := newFakeAssetStore()
	uploader := &fakeAssetUploader{
		result: asset.UploadResult{
			Provider:  asset.StorageProviderTOS,
			Bucket:    "dora-public",
			ObjectKey: "aigc/sessions/s1/assets/asset-1/suji.png",
			URL:       "https://tos.doraigc.com/aigc/sessions/s1/assets/asset-1/suji.png",
			SizeBytes: 8,
		},
	}
	router := NewRouter(Config{
		Store:         store,
		Assets:        assets,
		AssetUploader: uploader,
		Invoker:       &fakeAgentInvoker{},
		NewID:         sequentialIDs("asset-1"),
		Now:           fixedNow,
	})

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("session_id", "s1"); err != nil {
		t.Fatalf("WriteField(session_id): %v", err)
	}
	if err := writer.WriteField("kind", asset.KindReference); err != nil {
		t.Fatalf("WriteField(kind): %v", err)
	}
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", `form-data; name="file"; filename="suji.png"`)
	header.Set("Content-Type", "image/png")
	part, err := writer.CreatePart(header)
	if err != nil {
		t.Fatalf("CreatePart() error = %v", err)
	}
	if _, err := part.Write([]byte("pngbytes")); err != nil {
		t.Fatalf("Write file part: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close multipart writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/aigc/assets", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got asset.Asset
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if got.ID != "asset-1" || got.SessionID != "s1" || got.UserID != "u1" {
		t.Fatalf("asset response = %#v", got)
	}
	if got.Kind != asset.KindReference || got.Source != asset.SourceUpload || got.URL != uploader.result.URL {
		t.Fatalf("asset response metadata = %#v", got)
	}
	if uploader.seen.ObjectKey != "aigc/sessions/s1/assets/asset-1/suji.png" || uploader.seen.MIMEType != "image/png" {
		t.Fatalf("upload input = %#v", uploader.seen)
	}
	if string(uploader.body) != "pngbytes" {
		t.Fatalf("uploaded body = %q", string(uploader.body))
	}
	if saved := assets.records["asset-1"]; saved.URL != uploader.result.URL || saved.Bucket != "dora-public" {
		t.Fatalf("saved asset = %#v", saved)
	}
}

func TestGetAsset(t *testing.T) {
	gin.SetMode(gin.TestMode)

	assets := newFakeAssetStore()
	assets.records["asset-1"] = asset.Asset{
		ID:           "asset-1",
		SessionID:    "s1",
		Availability: asset.AvailabilityAvailable,
		Kind:         asset.KindImage,
		Source:       asset.SourceGenerated,
		URL:          "https://tos.doraigc.com/aigc/sessions/s1/assets/asset-1/keyframe.png",
	}
	store := newFakeSessionStore()
	store.sessions["s1"] = session.SessionRecord{ID: "s1", Status: "active"}
	router := NewRouter(Config{
		Store:   store,
		Assets:  assets,
		Invoker: &fakeAgentInvoker{},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/aigc/assets/asset-1?session_id=s1", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got asset.Asset
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if got.ID != "asset-1" || got.URL == "" {
		t.Fatalf("asset response = %#v", got)
	}
	assets.records["asset-1"] = asset.Asset{ID: "asset-1", SessionID: "s1", Availability: asset.AvailabilityPendingBilling}
	pendingReq := httptest.NewRequest(http.MethodGet, "/api/aigc/assets/asset-1?session_id=s1", nil)
	pendingRec := httptest.NewRecorder()
	router.ServeHTTP(pendingRec, pendingReq)
	if pendingRec.Code != http.StatusNotFound {
		t.Fatalf("pending asset status = %d, body = %s", pendingRec.Code, pendingRec.Body.String())
	}
}

func TestListSessionAssets(t *testing.T) {
	gin.SetMode(gin.TestMode)

	store := newFakeSessionStore()
	store.sessions["s1"] = session.SessionRecord{ID: "s1", Status: "active"}
	assets := newFakeAssetStore()
	assets.records["asset-1"] = asset.Asset{ID: "asset-1", SessionID: "s1", Kind: asset.KindImage, Availability: asset.AvailabilityAvailable}
	assets.records["asset-2"] = asset.Asset{ID: "asset-2", SessionID: "s2", Kind: asset.KindVideo}
	router := NewRouter(Config{
		Store:   store,
		Assets:  assets,
		Invoker: &fakeAgentInvoker{},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/aigc/sessions/s1/assets", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Assets []asset.Asset `json:"assets"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(got.Assets) != 1 || got.Assets[0].ID != "asset-1" {
		t.Fatalf("assets response = %#v", got)
	}
}

func TestListSessionGenerationJobs(t *testing.T) {
	gin.SetMode(gin.TestMode)

	store := newFakeSessionStore()
	store.sessions["s1"] = session.SessionRecord{ID: "s1", Status: "active"}
	jobs := &fakeGenerationJobStore{
		bySession: map[string][]generation.GenerationJob{
			"s1": {{
				ID:             "job-1",
				SessionID:      "s1",
				StoryboardID:   "storyboard-1",
				IdempotencyKey: "s1:storyboard-1:shot-1:image",
				Provider:       generation.ProviderImage2,
				TargetType:     generation.TargetShot,
				TargetID:       "shot-1",
				Status:         generation.StatusRunning,
				StatusVersion:  2,
				Payload:        map[string]any{"prompt": "private prompt"},
				Result:         map[string]any{"temporary_url": "https://provider.invalid/result"},
				ProviderTaskID: "provider-task-1",
			}},
		},
	}
	router := NewRouter(Config{
		Store:          store,
		GenerationJobs: jobs,
		Invoker:        &fakeAgentInvoker{},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/aigc/sessions/s1/jobs", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Jobs []generation.GenerationJob `json:"jobs"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(got.Jobs) != 1 || got.Jobs[0].ID != "job-1" || got.Jobs[0].Status != generation.StatusRunning {
		t.Fatalf("jobs response = %#v", got)
	}
	if got.Jobs[0].IdempotencyKey != "" || got.Jobs[0].Payload != nil || got.Jobs[0].Result != nil || got.Jobs[0].ProviderTaskID != "" {
		t.Fatalf("jobs response leaked internal provider state = %#v", got.Jobs[0])
	}
	if jobs.seenSessionID != "s1" {
		t.Fatalf("seen session id = %q", jobs.seenSessionID)
	}
}

func TestListSessionMessages(t *testing.T) {
	gin.SetMode(gin.TestMode)

	store := newFakeSessionStore()
	store.sessions["s1"] = session.SessionRecord{ID: "s1", Status: "active"}
	store.messages["s1"] = []session.MessageRecord{
		{ID: "m1", SessionID: "s1", Role: "user", Content: "第一轮", Seq: 1},
		{ID: "m2", SessionID: "s1", Role: "assistant", Content: "已规划。", Seq: 2},
	}
	router := NewRouter(Config{
		Store:   store,
		Invoker: &fakeAgentInvoker{},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/aigc/sessions/s1/messages", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Messages []session.MessageRecord `json:"messages"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(got.Messages) != 2 || got.Messages[1].Content != "已规划。" {
		t.Fatalf("messages = %#v", got.Messages)
	}
}

func TestStreamSessionEvents(t *testing.T) {
	gin.SetMode(gin.TestMode)

	store := newFakeSessionStore()
	store.sessions["s1"] = session.SessionRecord{ID: "s1", Status: "active"}
	events := make(chan a2ui.SSEEvent, 1)
	events <- a2ui.SSEEvent{
		ID:        "evt-1",
		SessionID: "s1",
		Event:     a2ui.EventAction,
		Payload: a2ui.ActionEnvelope{
			Version: a2ui.Version1,
			Actions: []a2ui.Action{{
				Type:    a2ui.ActionUpdateCard,
				Surface: "tool_runs",
				Target:  &a2ui.ActionTarget{Surface: "tool_runs", CardID: "tool_run:media_generator"},
				Payload: map[string]any{"tool_run": map[string]any{"job_id": "job-1"}},
			}},
		},
		CreatedAt: fixedNow(),
	}
	subscriber := &fakeEventSubscriber{events: events}
	router := NewRouter(Config{
		Store:   store,
		Events:  subscriber,
		Invoker: &fakeAgentInvoker{},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/aigc/sessions/s1/events/stream?once=1", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("content-type = %q", ct)
	}
	out := rec.Body.String()
	if !strings.Contains(out, "event: a2ui.action") || !strings.Contains(out, `"type":"update_card"`) || !strings.Contains(out, "tool_run") || !strings.Contains(out, "job-1") {
		t.Fatalf("missing a2ui tool run update in %q", out)
	}
	if subscriber.seenSessionID != "s1" || !subscriber.unsubscribed {
		t.Fatalf("subscriber = %#v", subscriber)
	}
}

func TestStreamSessionEventsDropsRawWorkerEvents(t *testing.T) {
	gin.SetMode(gin.TestMode)

	store := newFakeSessionStore()
	store.sessions["s1"] = session.SessionRecord{ID: "s1", Status: "active"}
	events := make(chan a2ui.SSEEvent, 1)
	events <- a2ui.SSEEvent{
		ID:        "evt-legacy",
		SessionID: "s1",
		Event:     generation.EventJobStatus,
		Payload:   generation.JobStatusPayload{JobID: "job-1", SessionID: "s1", Status: generation.StatusSucceeded},
		CreatedAt: fixedNow(),
	}
	close(events)
	subscriber := &fakeEventSubscriber{events: events}
	router := NewRouter(Config{
		Store:   store,
		Events:  subscriber,
		Invoker: &fakeAgentInvoker{},
		NewID:   sequentialIDs("evt-ready-1"),
		Now:     fixedNow,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/aigc/sessions/s1/events/stream?once=1", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	out := rec.Body.String()
	if !strings.Contains(out, "event: a2ui.ready") {
		t.Fatalf("missing ready event in %q", out)
	}
	if strings.Contains(out, "event: job.status") || strings.Contains(out, "job-1") || strings.Contains(out, "update_card") {
		t.Fatalf("raw worker event leaked to SSE: %q", out)
	}
}

func TestStreamSessionEventsWritesReadyEventOnSubscribe(t *testing.T) {
	gin.SetMode(gin.TestMode)

	store := newFakeSessionStore()
	store.sessions["s1"] = session.SessionRecord{ID: "s1", Status: "active"}
	events := make(chan a2ui.SSEEvent)
	close(events)
	subscriber := &fakeEventSubscriber{events: events}
	router := NewRouter(Config{
		Store:   store,
		Events:  subscriber,
		Invoker: &fakeAgentInvoker{},
		NewID:   sequentialIDs("evt-ready-1"),
		Now:     fixedNow,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/aigc/sessions/s1/events/stream?once=1", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	out := rec.Body.String()
	if !strings.Contains(out, "event: a2ui.ready") || !strings.Contains(out, `"session_id":"s1"`) {
		t.Fatalf("missing ready event in %q", out)
	}
	if subscriber.seenSessionID != "s1" || !subscriber.unsubscribed {
		t.Fatalf("subscriber = %#v", subscriber)
	}
}

func TestBindAssetToStoryboardKeyElement(t *testing.T) {
	gin.SetMode(gin.TestMode)

	store := newFakeSessionStore()
	store.sessions["s1"] = session.SessionRecord{ID: "s1", Status: "active"}
	assets := newFakeAssetStore()
	assets.records["asset-1"] = asset.Asset{ID: "asset-1", SessionID: "s1", Kind: asset.KindImage, Availability: asset.AvailabilityAvailable}
	storyboards := &fakeStoryboardStore{
		byID: map[string]storyboard.Storyboard{
			"storyboard-1": {
				ID:        "storyboard-1",
				SessionID: "s1",
				Version:   3,
				KeyElements: []storyboard.KeyElement{
					{Key: "suji", Name: "苏寂"},
				},
			},
		},
		patched: storyboard.Storyboard{
			ID:        "storyboard-1",
			SessionID: "s1",
			Version:   4,
		},
		event: storyboard.EventRecord{
			ID:           "patch-event-1",
			SessionID:    "s1",
			StoryboardID: "storyboard-1",
			BaseVersion:  3,
			NextVersion:  4,
			Source:       "user",
			Ops: []aigctools.JSONPatchOp{{
				Op:    "add",
				Path:  "/key_elements/0/asset_ids",
				Value: []string{"asset-1"},
			}},
		},
	}
	router := NewRouter(Config{
		Store:       store,
		Assets:      assets,
		Storyboards: storyboards,
		Invoker:     &fakeAgentInvoker{},
		NewID:       sequentialIDs("patch-event-1"),
	})

	body := bytes.NewBufferString(`{"base_version":3,"target_type":"key_element","target_id":"suji"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/aigc/sessions/s1/storyboards/storyboard-1/assets/asset-1/bind", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if storyboards.seenPatch.EventID != "patch-event-1" || storyboards.seenPatch.BaseVersion != 3 {
		t.Fatalf("patch request = %#v", storyboards.seenPatch)
	}
	if len(storyboards.seenPatch.Ops) != 2 ||
		storyboards.seenPatch.Ops[0].Path != "/key_elements/0/asset_ids" ||
		storyboards.seenPatch.Ops[1].Path != "/key_elements/0/status" ||
		storyboards.seenPatch.Ops[1].Value != storyboard.StatusReady {
		t.Fatalf("patch ops = %#v", storyboards.seenPatch.Ops)
	}
}

func TestCreateSkillParsesAndStoresSkillMarkdown(t *testing.T) {
	gin.SetMode(gin.TestMode)

	skills := &fakeSkillStore{}
	router := NewRouter(Config{
		Store:   newFakeSessionStore(),
		Skills:  skills,
		Invoker: &fakeAgentInvoker{},
		NewID:   sequentialIDs("skill-1"),
		Now:     fixedNow,
	})

	body := bytes.NewBufferString(`{"content":"<name>\n武侠短片创作\n</name>\n<description>\n生成武侠短片。\n</description>\n<planner>\n1. 编写 Final_Video_Spec.md。 -> ** text_editor **\n   depends_on: []\n   pause_after: true\n2. 生成故事板。 -> ** storyboard_designer **\n   depends_on: [1]\n   pause_after: true\n</planner>"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/aigc/skills", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got createSkillResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if got.Skill.ID != "skill-1" || got.Skill.Name != "武侠短片创作" || !got.Skill.Enabled {
		t.Fatalf("skill response = %#v", got.Skill)
	}
	if len(got.Plan.Stages) != 2 || got.Plan.Stages[0].ToolKeys[0] != aigctools.TextEditorToolKey {
		t.Fatalf("plan response = %#v", got.Plan)
	}
	if saved := skills.saved["skill-1"]; saved.Content == "" || saved.Description != "生成武侠短片。" {
		t.Fatalf("saved skill = %#v", saved)
	}
}

func TestListSkills(t *testing.T) {
	gin.SetMode(gin.TestMode)

	skills := &fakeSkillStore{enabled: []skill.SkillRecord{
		{ID: "skill-video", Name: "武侠短片创作", Description: "生成武侠短片。", Enabled: true},
	}}
	router := NewRouter(Config{
		Store:   newFakeSessionStore(),
		Skills:  skills,
		Invoker: &fakeAgentInvoker{},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/aigc/skills", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got listSkillsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(got.Skills) != 1 || got.Skills[0].ID != "skill-video" {
		t.Fatalf("skills response = %#v", got)
	}
}

func TestGetSkillPlan(t *testing.T) {
	gin.SetMode(gin.TestMode)

	content := `<name>
武侠短片创作
</name>
<description>
生成武侠短片。
</description>
<planner>
1. 编写 Final_Video_Spec.md。 -> ** text_editor **
   depends_on: []
   pause_after: true
</planner>`
	skills := &fakeSkillStore{records: map[string]skill.SkillRecord{
		"skill-video": {ID: "skill-video", Content: content, Enabled: true},
	}}
	router := NewRouter(Config{
		Store:   newFakeSessionStore(),
		Skills:  skills,
		Invoker: &fakeAgentInvoker{},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/aigc/skills/skill-video/plan", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got skill.SkillPlan
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if got.SkillID != "skill-video" || got.Stages[0].ToolKeys[0] != aigctools.TextEditorToolKey {
		t.Fatalf("plan response = %#v", got)
	}
}

func TestCreateMessageInvokesAgentAndPersistsMessages(t *testing.T) {
	gin.SetMode(gin.TestMode)

	store := newFakeSessionStore()
	store.sessions["s1"] = session.SessionRecord{ID: "s1", Status: "active"}
	store.messages["s1"] = []session.MessageRecord{
		{ID: "m0", SessionID: "s1", Role: "assistant", Content: "上一轮", Seq: 1},
	}
	assistantText := "好的，开始规划故事板。"
	assistantEnvelope := assistantCardEnvelope(t, "storyboard-plan", assistantText)
	invoker := &fakeAgentInvoker{
		events: []AgentEvent{{
			Event:         a2ui.EventChatDelta,
			Payload:       map[string]any{"text": assistantEnvelope},
			AssistantText: assistantEnvelope,
		}},
	}
	broker := &fakeEventSubscriber{}
	router := NewRouter(Config{
		Store:         store,
		Events:        broker,
		Invoker:       invoker,
		NewID:         sequentialIDs("run-1", "msg-user-1", "evt-1", "evt-2", "msg-assistant-1"),
		Now:           fixedNow,
		MessageWindow: session.MessageWindow{Limit: 20},
	})

	body := bytes.NewBufferString(`{"content":"帮我做一个武侠短片"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/aigc/sessions/s1/messages", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("content-type = %q", ct)
	}
	action := requirePublishedEvent(t, broker, a2ui.EventAction)
	out := publishedPayloadString(t, action)
	if action.RunID != "run-1" || action.Seq != 1 {
		t.Fatalf("published action envelope = %#v", action)
	}
	if strings.Contains(out, "chat.delta") || strings.Contains(out, "a2ui.surface_update") || strings.Contains(out, "a2ui.data_model_update") {
		t.Fatalf("assistant output should use a2ui protocol, got %q", out)
	}
	if !strings.Contains(out, `"type":"append_card"`) {
		t.Fatalf("missing a2ui action card event in %q", out)
	}
	if !strings.Contains(out, assistantText) {
		t.Fatalf("missing assistant delta in %q", out)
	}

	appended := store.messages["s1"]
	if len(appended) != 3 {
		t.Fatalf("message count = %d, records = %#v", len(appended), appended)
	}
	if appended[1].Role != "user" || appended[1].Content != "帮我做一个武侠短片" {
		t.Fatalf("user message = %#v", appended[1])
	}
	if appended[2].Role != string(schema.Assistant) || !strings.Contains(appended[2].Content, `"card_id":"storyboard-plan:evt-1"`) {
		t.Fatalf("assistant a2ui message = %#v", appended[2])
	}

	if len(invoker.seenMessages) != 2 {
		t.Fatalf("invoker message count = %d", len(invoker.seenMessages))
	}
	if invoker.seenMessages[0].Role != schema.Assistant || invoker.seenMessages[0].Content != "上一轮" {
		t.Fatalf("history message = %#v", invoker.seenMessages[0])
	}
	if invoker.seenMessages[1].Role != schema.User || invoker.seenMessages[1].Content != "帮我做一个武侠短片" {
		t.Fatalf("current message = %#v", invoker.seenMessages[1])
	}
}

func TestCreateMessageRendersAssistantMessageThroughA2UIProtocol(t *testing.T) {
	gin.SetMode(gin.TestMode)

	store := newFakeSessionStore()
	store.sessions["s1"] = session.SessionRecord{ID: "s1", Status: "active"}
	assistantText := "故事板草案已生成，请确认。"
	assistantEnvelope := assistantCardEnvelope(t, "storyboard-review", assistantText)
	invoker := &fakeAgentInvoker{
		events: []AgentEvent{{
			Event:         a2ui.EventChatDelta,
			Payload:       map[string]any{"text": assistantEnvelope},
			AssistantText: assistantEnvelope,
			Message:       schema.AssistantMessage(assistantEnvelope, nil),
		}},
	}
	broker := &fakeEventSubscriber{}
	router := NewRouter(Config{
		Store:   store,
		Events:  broker,
		Invoker: invoker,
		NewID:   sequentialIDs("run-1", "msg-user-1", "evt-surface", "msg-assistant-1"),
		Now:     fixedNow,
	})

	body := bytes.NewBufferString(`{"content":"开始吧"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/aigc/sessions/s1/messages", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	out := publishedPayloadString(t, requirePublishedEvent(t, broker, a2ui.EventAction))
	if strings.Contains(out, "chat.delta") || strings.Contains(out, "a2ui.surface_update") || strings.Contains(out, "a2ui.data_model_update") {
		t.Fatalf("assistant output should be rendered through a2ui protocol, got %q", out)
	}
	if !strings.Contains(out, `"type":"append_card"`) {
		t.Fatalf("missing a2ui action card in %q", out)
	}
	if !strings.Contains(out, assistantText) {
		t.Fatalf("missing assistant content in %q", out)
	}
	appended := store.messages["s1"]
	if len(appended) != 2 || appended[0].Role != string(schema.User) || appended[1].Role != string(schema.Assistant) {
		t.Fatalf("persisted messages = %#v", appended)
	}
}

func TestCreateMessagePassesA2UIActionEnvelopeFromAssistant(t *testing.T) {
	gin.SetMode(gin.TestMode)

	store := newFakeSessionStore()
	store.sessions["s1"] = session.SessionRecord{ID: "s1", Status: "active"}
	assistantEnvelope := `{
		"a2ui_version":"1.0",
		"actions":[{
			"type":"append_card",
			"surface":"chat",
			"card_id":"brief-card",
			"card":{
				"card_type":"info_collection",
				"title":"补充产品信息",
				"submit_label":"提交",
				"root":"root",
				"components":[
					{"id":"root","component":{"Card":{"children":["product"]}}},
					{"id":"product","component":{"TextInput":{"key":"product_name","label":"产品名称/品类","required":true}}}
				]
			}
		}]
	}`
	invoker := &fakeAgentInvoker{
		events: []AgentEvent{{
			Event:         a2ui.EventChatDelta,
			Payload:       map[string]any{"text": assistantEnvelope},
			AssistantText: assistantEnvelope,
			Message:       schema.AssistantMessage(assistantEnvelope, nil),
		}},
	}
	broker := &fakeEventSubscriber{}
	router := NewRouter(Config{
		Store:   store,
		Events:  broker,
		Invoker: invoker,
		NewID:   sequentialIDs("run-1", "msg-user-1", "card-instance-1", "evt-action", "msg-assistant-1"),
		Now:     fixedNow,
	})

	body := bytes.NewBufferString(`{"content":"开始吧"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/aigc/sessions/s1/messages", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	out := publishedPayloadString(t, requirePublishedEvent(t, broker, a2ui.EventAction))
	if !strings.Contains(out, `"type":"append_card"`) || !strings.Contains(out, `"card_id":"brief-card:card-instance-1"`) {
		t.Fatalf("missing a2ui action event in %q", out)
	}
	if strings.Contains(out, "a2ui.surface_update") || strings.Contains(out, "a2ui.data_model_update") {
		t.Fatalf("new action envelope should not be downgraded to legacy a2ui events: %q", out)
	}
	appended := store.messages["s1"]
	if len(appended) != 2 || appended[1].Role != string(schema.Assistant) || !strings.Contains(appended[1].Content, `"card_id":"brief-card:card-instance-1"`) {
		t.Fatalf("pure a2ui action envelope should be persisted as structured assistant history, messages = %#v", appended)
	}
}

func TestCreateMessageDoesNotInferProductBriefFormFromAssistantText(t *testing.T) {
	gin.SetMode(gin.TestMode)

	store := newFakeSessionStore()
	store.sessions["s1"] = session.SessionRecord{ID: "s1", Status: "active"}
	assistantText := `请补充产品信息：产品名称、核心卖点、目标平台、视觉风格。`
	invoker := &fakeAgentInvoker{
		events: []AgentEvent{{
			Event:         a2ui.EventChatDelta,
			Payload:       map[string]any{"text": assistantText},
			AssistantText: assistantText,
			Message:       schema.AssistantMessage(assistantText, nil),
		}},
	}
	broker := &fakeEventSubscriber{}
	router := NewRouter(Config{
		Store:   store,
		Events:  broker,
		Invoker: invoker,
		NewID:   sequentialIDs("run-1", "msg-user-1", "evt-begin", "evt-action", "msg-assistant-1"),
		Now:     fixedNow,
	})

	body := bytes.NewBufferString(`{"content":"做个商品宣传片"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/aigc/sessions/s1/messages", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	out := publishedPayloadString(t, requirePublishedEvent(t, broker, a2ui.EventError))
	if strings.Contains(out, "brief-intake") || strings.Contains(out, "TextInput") || strings.Contains(out, "a2ui.surface_update") {
		t.Fatalf("assistant text should not trigger legacy product form inference: %q", out)
	}
	if !strings.Contains(out, "invalid_a2ui_action_envelope") {
		t.Fatalf("non-protocol assistant text should be rejected, got %q", out)
	}
}

func TestCreateMessageUsesLatestCompleteAssistantMessage(t *testing.T) {
	gin.SetMode(gin.TestMode)

	store := newFakeSessionStore()
	store.sessions["s1"] = session.SessionRecord{ID: "s1", Status: "active"}
	historyEnvelope := assistantCardEnvelope(t, "history-reply", "历史回复")
	latestEnvelope := assistantCardEnvelope(t, "latest-reply", "最新回复")
	invoker := &fakeAgentInvoker{
		events: []AgentEvent{
			{
				Event:         a2ui.EventChatDelta,
				Payload:       map[string]any{"text": historyEnvelope},
				AssistantText: historyEnvelope,
				Message:       schema.AssistantMessage(historyEnvelope, nil),
			},
			{
				Event:         a2ui.EventChatDelta,
				Payload:       map[string]any{"text": latestEnvelope},
				AssistantText: latestEnvelope,
				Message:       schema.AssistantMessage(latestEnvelope, nil),
			},
		},
	}
	broker := &fakeEventSubscriber{}
	router := NewRouter(Config{
		Store:   store,
		Events:  broker,
		Invoker: invoker,
		NewID:   sequentialIDs("run-1", "msg-user-1", "evt-begin", "evt-surface", "evt-data", "msg-assistant-1"),
		Now:     fixedNow,
	})

	body := bytes.NewBufferString(`{"content":"继续"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/aigc/sessions/s1/messages", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	out := publishedPayloadString(t, requirePublishedEvent(t, broker, a2ui.EventAction))
	if strings.Contains(out, "历史回复") {
		t.Fatalf("rendered stale assistant history: %q", out)
	}
	if !strings.Contains(out, "最新回复") {
		t.Fatalf("missing latest assistant reply: %q", out)
	}
	appended := store.messages["s1"]
	if len(appended) != 2 {
		t.Fatalf("message count = %d, records = %#v", len(appended), appended)
	}
	if appended[0].Role != string(schema.User) {
		t.Fatalf("user message = %#v", appended[0])
	}
	if appended[1].Role != string(schema.Assistant) || !strings.Contains(appended[1].Content, "最新回复") {
		t.Fatalf("assistant message = %#v", appended[1])
	}
}

func TestCreateMessagePassesSessionValuesToInvoker(t *testing.T) {
	gin.SetMode(gin.TestMode)

	store := newFakeSessionStore()
	store.sessions["s1"] = session.SessionRecord{ID: "s1", SkillID: "skill-video", Title: "武侠短片", Status: "active"}
	assistantEnvelope := assistantCardEnvelope(t, "ack", "收到。")
	invoker := &fakeAgentInvoker{
		events: []AgentEvent{{
			Event:         a2ui.EventChatDelta,
			Payload:       map[string]any{"text": assistantEnvelope},
			AssistantText: assistantEnvelope,
		}},
	}
	broker := &fakeEventSubscriber{}
	router := NewRouter(Config{
		Store:   store,
		Events:  broker,
		Invoker: invoker,
		SessionValues: func(record session.SessionRecord) map[string]any {
			return map[string]any{
				"session_id": record.ID,
				"skill_id":   record.SkillID,
				"title":      record.Title,
			}
		},
		NewID: sequentialIDs("run-1", "msg-user-1", "evt-1", "msg-assistant-1"),
		Now:   fixedNow,
	})

	body := bytes.NewBufferString(`{"content":"把第一镜改成雨夜"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/aigc/sessions/s1/messages", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if len(invoker.seenMessages) != 1 {
		t.Fatalf("invoker message count = %d", len(invoker.seenMessages))
	}
	if invoker.seenMessages[0].Role != schema.User || invoker.seenMessages[0].Content != "把第一镜改成雨夜" {
		t.Fatalf("user message = %#v", invoker.seenMessages[0])
	}
	if invoker.seenSessionValues["session_id"] != "s1" || invoker.seenSessionValues["skill_id"] != "skill-video" {
		t.Fatalf("session values = %#v", invoker.seenSessionValues)
	}
}

func TestCreateMessagePersistsUISourceWithoutPassingItToAgent(t *testing.T) {
	gin.SetMode(gin.TestMode)

	store := newFakeSessionStore()
	store.sessions["s1"] = session.SessionRecord{ID: "s1", Status: "active"}
	assistantEnvelope := assistantCardEnvelope(t, "ack", "收到。")
	invoker := &fakeAgentInvoker{
		events: []AgentEvent{{
			Event:         a2ui.EventChatDelta,
			Payload:       map[string]any{"text": assistantEnvelope},
			AssistantText: assistantEnvelope,
		}},
	}
	broker := &fakeEventSubscriber{}
	router := NewRouter(Config{
		Store:   store,
		Events:  broker,
		Invoker: invoker,
		NewID:   sequentialIDs("run-1", "msg-user-1", "card-instance-2", "evt-1", "msg-assistant-1"),
		Now:     fixedNow,
	})

	body := bytes.NewBufferString(`{"content":"商品宣传短片_v2","ui_source":{"type":"a2ui_submit","card_id":"skill-selection:card-instance-1"}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/aigc/sessions/s1/messages", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	appended := store.messages["s1"]
	if len(appended) < 1 {
		t.Fatalf("messages = %#v", appended)
	}
	metadata := appended[0].Metadata
	uiSource, ok := metadata["ui_source"].(map[string]any)
	if !ok || uiSource["type"] != "a2ui_submit" || uiSource["card_id"] != "skill-selection:card-instance-1" {
		t.Fatalf("ui_source metadata = %#v", metadata)
	}
	if len(invoker.seenMessages) == 0 {
		t.Fatalf("invoker messages = %#v", invoker.seenMessages)
	}
	last := invoker.seenMessages[len(invoker.seenMessages)-1]
	if last.Content != "商品宣传短片_v2" {
		t.Fatalf("agent should receive plain content only, got %#v", last)
	}
	rawExtra, _ := json.Marshal(last.Extra)
	if strings.Contains(last.Content, "ui_source") || strings.Contains(string(rawExtra), "ui_source") {
		t.Fatalf("ui_source leaked to agent message: %#v", last)
	}
}

func TestCreateMessagePersistsToolCallAndToolResult(t *testing.T) {
	gin.SetMode(gin.TestMode)

	store := newFakeSessionStore()
	store.sessions["s1"] = session.SessionRecord{ID: "s1", Status: "active"}
	assistantEnvelope := assistantCardEnvelope(t, "spec-updated", "规范已更新。")
	invoker := &fakeAgentInvoker{
		events: []AgentEvent{
			{
				Event:   a2ui.EventToolProgress,
				Payload: map[string]any{"role": "assistant"},
				Message: schema.AssistantMessage("", []schema.ToolCall{{
					ID:   "call-1",
					Type: "function",
					Function: schema.FunctionCall{
						Name:      aigctools.TextEditorToolKey,
						Arguments: `{"document_type":"final_video_spec"}`,
					},
				}}),
			},
			{
				Event:   a2ui.EventToolProgress,
				Payload: map[string]any{"role": "tool", "content": `{"status":"ok"}`},
				Message: schema.ToolMessage(`{"status":"ok"}`, "call-1", schema.WithToolName(aigctools.TextEditorToolKey)),
			},
			{
				Event:         a2ui.EventChatDelta,
				Payload:       map[string]any{"text": assistantEnvelope},
				AssistantText: assistantEnvelope,
			},
		},
	}
	broker := &fakeEventSubscriber{}
	router := NewRouter(Config{
		Store:   store,
		Events:  broker,
		Invoker: invoker,
		NewID: sequentialIDs(
			"run-1",
			"msg-user-1",
			"evt-tool-call",
			"msg-tool-call",
			"evt-tool-result",
			"msg-tool-result",
			"evt-assistant",
			"msg-assistant-1",
		),
		Now: fixedNow,
	})

	body := bytes.NewBufferString(`{"content":"先写规范"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/aigc/sessions/s1/messages", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	appended := store.messages["s1"]
	if len(appended) != 4 {
		t.Fatalf("message count = %d, records = %#v", len(appended), appended)
	}
	if appended[1].Role != string(schema.Assistant) || len(appended[1].ToolCalls) == 0 {
		t.Fatalf("assistant tool call record = %#v", appended[1])
	}
	if appended[2].Role != string(schema.Tool) || appended[2].ToolCallID != "call-1" || appended[2].ToolName != aigctools.TextEditorToolKey {
		t.Fatalf("tool result record = %#v", appended[2])
	}
	if appended[3].Role != string(schema.Assistant) || !strings.Contains(appended[3].Content, `"card_id":"spec-updated:evt-tool-result"`) {
		t.Fatalf("assistant a2ui result record = %#v", appended[3])
	}

	rebuilt := recordsToSchemaMessages(appended)
	if len(rebuilt[1].ToolCalls) != 1 || rebuilt[1].ToolCalls[0].ID != "call-1" {
		t.Fatalf("rebuilt assistant tool calls = %#v", rebuilt[1])
	}
	if rebuilt[2].Role != schema.Tool || rebuilt[2].ToolCallID != "call-1" {
		t.Fatalf("rebuilt tool result = %#v", rebuilt[2])
	}
}

func TestCreateMessagePersistsInterruptCheckpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)

	store := newFakeSessionStore()
	store.sessions["s1"] = session.SessionRecord{ID: "s1", Status: "active"}
	checkpoints := &fakeCheckpointStore{}
	invoker := &fakeAgentInvoker{
		events: []AgentEvent{{
			Event: a2ui.EventInterruptRequest,
			Payload: map[string]any{
				"scope":              "runner",
				"checkpoint_id":      "s1",
				"interrupt_id":       "interrupt-1",
				"storyboard_version": float64(7),
				"message":            "请确认故事板",
			},
		}},
	}
	broker := &fakeEventSubscriber{}
	router := NewRouter(Config{
		Store:       store,
		Events:      broker,
		Checkpoints: checkpoints,
		Invoker:     invoker,
		NewID:       sequentialIDs("run-1", "msg-user-1", "evt-interrupt"),
		Now:         fixedNow,
	})

	body := bytes.NewBufferString(`{"content":"确认前暂停"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/aigc/sessions/s1/messages", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if checkpoints.saved.SessionID != "s1" || checkpoints.saved.RunID != "run-1" {
		t.Fatalf("checkpoint mapping = %#v", checkpoints.saved)
	}
	if checkpoints.saved.Scope != session.CheckpointScopeRunner || checkpoints.saved.RunnerCheckpointID != "s1" {
		t.Fatalf("checkpoint mapping = %#v", checkpoints.saved)
	}
	if checkpoints.saved.InterruptID != "interrupt-1" || checkpoints.saved.Status != session.CheckpointStatusPending {
		t.Fatalf("checkpoint mapping = %#v", checkpoints.saved)
	}
	if checkpoints.saved.StoryboardVersion != 7 {
		t.Fatalf("checkpoint mapping = %#v", checkpoints.saved)
	}
	if event := requirePublishedEvent(t, broker, a2ui.EventInterruptRequest); event.RunID != "run-1" {
		t.Fatalf("interrupt event = %#v", event)
	}
}

func TestResumeAgentMarksCheckpointResumed(t *testing.T) {
	gin.SetMode(gin.TestMode)

	store := newFakeSessionStore()
	store.sessions["s1"] = session.SessionRecord{ID: "s1", Status: "active"}
	checkpoints := &fakeCheckpointStore{
		record: session.CheckpointMapping{
			ID:                 "checkpoint-map-1",
			SessionID:          "s1",
			Scope:              session.CheckpointScopeRunner,
			RunnerCheckpointID: "s1",
			InterruptID:        "interrupt-1",
			Status:             session.CheckpointStatusPending,
		},
	}
	invoker := &fakeAgentInvoker{
		events: []AgentEvent{{
			Event:         a2ui.EventChatDelta,
			Payload:       map[string]any{"text": "继续。"},
			AssistantText: "继续。",
		}},
	}
	broker := &fakeEventSubscriber{}
	router := NewRouter(Config{
		Store:       store,
		Events:      broker,
		Checkpoints: checkpoints,
		Invoker:     invoker,
		NewID:       sequentialIDs("run-1", "msg-user-1", "evt-1", "msg-assistant-1"),
		Now:         fixedNow,
	})

	body := bytes.NewBufferString(`{"checkpoint_id":"s1","interrupt_id":"interrupt-1","content":"确认","data":{"approved":true}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/aigc/sessions/s1/messages/resume", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if checkpoints.resumedID != "checkpoint-map-1" {
		t.Fatalf("resumed checkpoint id = %q", checkpoints.resumedID)
	}
}

func TestResumeAgentRecoversAfterCompletionReceiptFailureWithoutRunningTwice(t *testing.T) {
	gin.SetMode(gin.TestMode)

	store := newFakeSessionStore()
	store.sessions["s1"] = session.SessionRecord{ID: "s1", Status: "active"}
	checkpoints := &fakeTransitionCheckpointStore{
		fakeCheckpointStore: &fakeCheckpointStore{record: session.CheckpointMapping{
			ID:                 "checkpoint-map-1",
			SessionID:          "s1",
			RunID:              "origin-run",
			Scope:              session.CheckpointScopeRunner,
			RunnerCheckpointID: "s1",
			InterruptID:        "interrupt-1",
			MappingEpoch:       1,
			Status:             session.CheckpointStatusPending,
		}},
		failCompleteOnce: true,
	}
	invoker := &fakeAgentInvoker{}
	broker := &fakeEventSubscriber{}
	router := NewRouter(Config{Store: store, Events: broker, Checkpoints: checkpoints, Invoker: invoker, Now: fixedNow})
	body := `{"checkpoint_id":"s1","interrupt_id":"interrupt-1","data":{"approved":true}}`

	first := httptest.NewRecorder()
	firstReq := httptest.NewRequest(http.MethodPost, "/api/aigc/sessions/s1/messages/resume", bytes.NewBufferString(body))
	firstReq.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(first, firstReq)
	if first.Code != http.StatusInternalServerError {
		t.Fatalf("first status = %d, body = %s", first.Code, first.Body.String())
	}
	if checkpoints.record.Status != session.CheckpointStatusResumeApplied || invoker.resumeCalls != 1 {
		t.Fatalf("checkpoint=%#v resume_calls=%d", checkpoints.record, invoker.resumeCalls)
	}
	if eventOfType(broker.published, a2ui.EventInterruptResolved) != nil {
		t.Fatalf("resolved was published before completion: %#v", broker.published)
	}

	second := httptest.NewRecorder()
	secondReq := httptest.NewRequest(http.MethodPost, "/api/aigc/sessions/s1/messages/resume", bytes.NewBufferString(body))
	secondReq.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(second, secondReq)
	if second.Code != http.StatusOK {
		t.Fatalf("second status = %d, body = %s", second.Code, second.Body.String())
	}
	if checkpoints.record.Status != session.CheckpointStatusResumed || invoker.resumeCalls != 1 {
		t.Fatalf("checkpoint=%#v resume_calls=%d", checkpoints.record, invoker.resumeCalls)
	}
	if eventOfType(broker.published, a2ui.EventInterruptResolved) == nil {
		t.Fatalf("resolved event was not published: %#v", broker.published)
	}
}

func TestResumeAgentAlreadyResumedDoesNotInvokeRunner(t *testing.T) {
	gin.SetMode(gin.TestMode)

	store := newFakeSessionStore()
	store.sessions["s1"] = session.SessionRecord{ID: "s1", Status: "active"}
	checkpoints := &fakeCheckpointStore{
		record: session.CheckpointMapping{
			ID:                 "checkpoint-map-1",
			SessionID:          "s1",
			Scope:              session.CheckpointScopeRunner,
			RunnerCheckpointID: "s1",
			InterruptID:        "interrupt-1",
			Status:             session.CheckpointStatusResumed,
		},
	}
	invoker := &fakeAgentInvoker{
		events: []AgentEvent{{
			Event:         a2ui.EventChatDelta,
			Payload:       map[string]any{"text": "不应被调用"},
			AssistantText: "不应被调用",
		}},
	}
	broker := &fakeEventSubscriber{}
	router := NewRouter(Config{
		Store:       store,
		Events:      broker,
		Checkpoints: checkpoints,
		Invoker:     invoker,
		NewID:       sequentialIDs("run-already", "evt-already"),
		Now:         fixedNow,
	})

	body := bytes.NewBufferString(`{"checkpoint_id":"s1","interrupt_id":"interrupt-1","content":"确认","data":{"approved":true}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/aigc/sessions/s1/messages/resume", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if invoker.seenResume.CheckpointID != "" {
		t.Fatalf("runner was invoked: %#v", invoker.seenResume)
	}
	if !strings.Contains(rec.Body.String(), `"status":"already_resumed"`) {
		t.Fatalf("resume response = %q", rec.Body.String())
	}
	if event := requirePublishedEvent(t, broker, a2ui.EventAction); event.RunID != "run-already" {
		t.Fatalf("already resumed event = %#v", event)
	}
}

func TestGetSessionStoryboard(t *testing.T) {
	gin.SetMode(gin.TestMode)

	store := newFakeSessionStore()
	store.sessions["s1"] = session.SessionRecord{ID: "s1", Status: "active"}
	storyboards := &fakeStoryboardStore{
		latest: storyboard.Storyboard{
			ID:        "storyboard-1",
			SessionID: "s1",
			Version:   3,
			Status:    storyboard.StatusReviewing,
			Shots:     []storyboard.Shot{{ShotID: "shot-1", Index: 1, SceneDescription: "竹林归隐"}},
		},
	}
	router := NewRouter(Config{
		Store:       store,
		Storyboards: storyboards,
		Invoker:     &fakeAgentInvoker{},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/aigc/sessions/s1/storyboard", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got storyboard.Storyboard
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if got.ID != "storyboard-1" || got.Version != 3 || got.Shots[0].ShotID != "shot-1" {
		t.Fatalf("storyboard response = %#v", got)
	}
	if storyboards.seenSessionID != "s1" {
		t.Fatalf("storyboard session id = %q", storyboards.seenSessionID)
	}
}

func TestGetSessionStoryboardReturnsNoContentWhenStoryboardNotCreated(t *testing.T) {
	gin.SetMode(gin.TestMode)

	store := newFakeSessionStore()
	store.sessions["s1"] = session.SessionRecord{ID: "s1", Status: "active"}
	storyboards := &fakeStoryboardStore{getErr: storyboard.ErrNotFound}
	router := NewRouter(Config{
		Store:       store,
		Storyboards: storyboards,
		Invoker:     &fakeAgentInvoker{},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/aigc/sessions/s1/storyboard", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if strings.TrimSpace(rec.Body.String()) != "" {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestPatchStoryboardReturnsPatchEvent(t *testing.T) {
	gin.SetMode(gin.TestMode)

	store := newFakeSessionStore()
	store.sessions["s1"] = session.SessionRecord{ID: "s1", Status: "active"}
	storyboards := &fakeStoryboardStore{
		byID: map[string]storyboard.Storyboard{
			"storyboard-1": {ID: "storyboard-1", SessionID: "s1", Version: 3},
		},
		patched: storyboard.Storyboard{
			ID:        "storyboard-1",
			SessionID: "s1",
			Version:   4,
			Status:    storyboard.StatusReviewing,
		},
		event: storyboard.EventRecord{
			ID:           "patch-event-1",
			SessionID:    "s1",
			StoryboardID: "storyboard-1",
			BaseVersion:  3,
			NextVersion:  4,
			Source:       "user",
			Ops:          []aigctools.JSONPatchOp{{Op: "replace", Path: "/key_elements/0/status", Value: storyboard.StatusReviewing}},
		},
	}
	router := NewRouter(Config{
		Store:       store,
		Storyboards: storyboards,
		Invoker:     &fakeAgentInvoker{},
		NewID:       sequentialIDs("patch-event-1"),
	})

	body := bytes.NewBufferString(`{"base_version":3,"source":"user","ops":[{"op":"replace","path":"/key_elements/0/status","value":"reviewing"}]}`)
	req := httptest.NewRequest(http.MethodPatch, "/api/aigc/sessions/s1/storyboards/storyboard-1", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got patchStoryboardResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if got.Storyboard.Version != 4 || got.Patch.NextVersion != 4 || got.Patch.Source != "user" {
		t.Fatalf("patch response = %#v", got)
	}
	if storyboards.seenPatch.BaseVersion != 3 || storyboards.seenPatch.EventID != "patch-event-1" {
		t.Fatalf("patch request = %#v", storyboards.seenPatch)
	}
}

func TestMediaGraphCompatibilityRouteIsRemoved(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := NewRouter(Config{})
	req := httptest.NewRequest(http.MethodPost, "/api/aigc/sessions/s1/media-graph/resume", bytes.NewBufferString(`{}`))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestLocalAssetRouteServesDemoArtifacts(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "aigc", "sessions", "s1", "assets", "a1")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "demo.txt"), []byte("local-demo"), 0o644); err != nil {
		t.Fatal(err)
	}
	router := NewRouter(Config{LocalAssetDir: root})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/aigc/local-assets/aigc/sessions/s1/assets/a1/demo.txt", nil)
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || recorder.Body.String() != "local-demo" {
		t.Fatalf("local asset response status=%d body=%q", recorder.Code, recorder.Body.String())
	}
}

func TestResumeAgentPublishesAndPersistsDecision(t *testing.T) {
	gin.SetMode(gin.TestMode)

	store := newFakeSessionStore()
	store.sessions["s1"] = session.SessionRecord{ID: "s1", Status: "active"}
	assistantEnvelope := assistantCardEnvelope(t, "resume-next", "继续生成素材。")
	invoker := &fakeAgentInvoker{
		events: []AgentEvent{{
			Event:         a2ui.EventChatDelta,
			Payload:       map[string]any{"text": assistantEnvelope},
			AssistantText: assistantEnvelope,
		}},
	}
	broker := &fakeEventSubscriber{}
	checkpoints := &fakeCheckpointStore{record: session.CheckpointMapping{
		ID: "runner-map-1", SessionID: "s1", Scope: session.CheckpointScopeRunner,
		RunnerCheckpointID: "s1", InterruptID: "agent:A;tool:confirm", Status: session.CheckpointStatusPending,
	}}
	router := NewRouter(Config{
		Store:       store,
		Events:      broker,
		Invoker:     invoker,
		Checkpoints: checkpoints,
		NewID:       sequentialIDs("run-1", "msg-user-1", "evt-1", "msg-assistant-1"),
		Now:         fixedNow,
	})

	body := bytes.NewBufferString(`{"checkpoint_id":"s1","interrupt_id":"agent:A;tool:confirm","content":"确认参考图，可以继续","data":{"approved":true}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/aigc/sessions/s1/messages/resume", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if invoker.seenResume.CheckpointID != "s1" {
		t.Fatalf("checkpoint id = %q", invoker.seenResume.CheckpointID)
	}
	if _, ok := invoker.seenResume.Targets["agent:A;tool:confirm"]; !ok {
		t.Fatalf("resume targets = %#v", invoker.seenResume.Targets)
	}
	out := publishedPayloadString(t, requirePublishedEvent(t, broker, a2ui.EventAction))
	if !strings.Contains(out, `"card_id":"resume-next:evt-1"`) {
		t.Fatalf("missing resume a2ui action event: %q", out)
	}
	appended := store.messages["s1"]
	if len(appended) != 2 {
		t.Fatalf("message count = %d, records = %#v", len(appended), appended)
	}
	if appended[0].Role != string(schema.User) || appended[0].Content != "确认参考图，可以继续" {
		t.Fatalf("resume user message = %#v", appended[0])
	}
	if appended[1].Role != string(schema.Assistant) || !strings.Contains(appended[1].Content, `"card_id":"resume-next:evt-1"`) {
		t.Fatalf("resume assistant message = %#v", appended[1])
	}
}

func fixedNow() time.Time {
	return time.Date(2026, 7, 5, 10, 0, 0, 0, time.UTC)
}

func sequentialIDs(ids ...string) func() string {
	var mu sync.Mutex
	i := 0
	return func() string {
		mu.Lock()
		defer mu.Unlock()
		if i >= len(ids) {
			return ids[len(ids)-1]
		}
		id := ids[i]
		i++
		return id
	}
}

type fakeSessionStore struct {
	sessions map[string]session.SessionRecord
	messages map[string][]session.MessageRecord
}

func newFakeSessionStore() *fakeSessionStore {
	return &fakeSessionStore{
		sessions: map[string]session.SessionRecord{},
		messages: map[string][]session.MessageRecord{},
	}
}

type fakeSkillStore struct {
	saved   map[string]skill.SkillRecord
	records map[string]skill.SkillRecord
	enabled []skill.SkillRecord
}

func (s *fakeSkillStore) Save(_ context.Context, record skill.SkillRecord) error {
	if s.saved == nil {
		s.saved = map[string]skill.SkillRecord{}
	}
	if s.records == nil {
		s.records = map[string]skill.SkillRecord{}
	}
	s.saved[record.ID] = record
	s.records[record.ID] = record
	return nil
}

func (s *fakeSkillStore) Get(_ context.Context, skillID string) (skill.SkillRecord, error) {
	if record, ok := s.records[skillID]; ok {
		return record, nil
	}
	return skill.SkillRecord{}, skill.ErrSkillNotFound
}

func (s *fakeSkillStore) ListEnabled(_ context.Context) ([]skill.SkillRecord, error) {
	if s.enabled != nil {
		return append([]skill.SkillRecord(nil), s.enabled...), nil
	}
	out := make([]skill.SkillRecord, 0, len(s.records))
	for _, record := range s.records {
		if record.Enabled {
			out = append(out, record)
		}
	}
	return out, nil
}

func (s *fakeSessionStore) SaveSession(_ context.Context, record session.SessionRecord) error {
	s.sessions[record.ID] = record
	return nil
}

func (s *fakeSessionStore) GetSession(_ context.Context, sessionID string) (session.SessionRecord, error) {
	record, ok := s.sessions[sessionID]
	if !ok {
		return session.SessionRecord{}, ErrNotFound
	}
	return record, nil
}

func (s *fakeSessionStore) AppendMessage(_ context.Context, record session.MessageRecord) (session.MessageRecord, error) {
	if record.Seq == 0 {
		record.Seq = int64(len(s.messages[record.SessionID]) + 1)
	}
	s.messages[record.SessionID] = append(s.messages[record.SessionID], record)
	return record, nil
}

func (s *fakeSessionStore) ListMessages(_ context.Context, sessionID string, window session.MessageWindow) ([]session.MessageRecord, error) {
	records := append([]session.MessageRecord(nil), s.messages[sessionID]...)
	return session.ApplyMessageWindow(records, window), nil
}

type fakeAgentInvoker struct {
	events            []AgentEvent
	invokeErr         error
	invokeCalls       int
	seenMessages      []*schema.Message
	seenSessionValues map[string]any
	seenResume        AgentResumeRequest
	resumeCalls       int
}

func (i *fakeAgentInvoker) Invoke(_ context.Context, req AgentInvokeRequest) (<-chan AgentEvent, error) {
	i.invokeCalls++
	i.seenMessages = append([]*schema.Message(nil), req.Messages...)
	i.seenSessionValues = req.SessionValues
	if i.invokeErr != nil {
		return nil, i.invokeErr
	}
	ch := make(chan AgentEvent, len(i.events))
	for _, event := range i.events {
		ch <- event
	}
	close(ch)
	return ch, nil
}

func (i *fakeAgentInvoker) Resume(_ context.Context, req AgentResumeRequest) (<-chan AgentEvent, error) {
	i.seenResume = req
	i.resumeCalls++
	ch := make(chan AgentEvent, len(i.events))
	for _, event := range i.events {
		ch <- event
	}
	close(ch)
	return ch, nil
}

type fakeStoryboardStore struct {
	latest        storyboard.Storyboard
	byID          map[string]storyboard.Storyboard
	patched       storyboard.Storyboard
	event         storyboard.EventRecord
	getErr        error
	patchErr      error
	seenSessionID string
	seenPatch     storyboard.PatchRequest
}

func (s *fakeStoryboardStore) Get(_ context.Context, storyboardID string) (storyboard.Storyboard, error) {
	if s.getErr != nil {
		return storyboard.Storyboard{}, s.getErr
	}
	if s.byID != nil {
		if board, ok := s.byID[storyboardID]; ok {
			return board, nil
		}
	}
	if s.latest.ID == storyboardID {
		return s.latest, nil
	}
	return storyboard.Storyboard{}, storyboard.ErrNotFound
}

func (s *fakeStoryboardStore) GetLatestBySession(_ context.Context, sessionID string) (storyboard.Storyboard, error) {
	s.seenSessionID = sessionID
	if s.getErr != nil {
		return storyboard.Storyboard{}, s.getErr
	}
	return s.latest, nil
}

func (s *fakeStoryboardStore) ApplyPatch(_ context.Context, req storyboard.PatchRequest) (storyboard.Storyboard, storyboard.EventRecord, error) {
	s.seenPatch = req
	if s.patchErr != nil {
		return storyboard.Storyboard{}, storyboard.EventRecord{}, s.patchErr
	}
	return s.patched, s.event, nil
}

type fakeAssetStore struct {
	records map[string]asset.Asset
}

func newFakeAssetStore() *fakeAssetStore {
	return &fakeAssetStore{records: map[string]asset.Asset{}}
}

func (s *fakeAssetStore) Save(_ context.Context, record asset.Asset) (asset.Asset, error) {
	s.records[record.ID] = record
	return record, nil
}

func (s *fakeAssetStore) Get(_ context.Context, assetID string) (asset.Asset, error) {
	record, ok := s.records[assetID]
	if !ok {
		return asset.Asset{}, asset.ErrNotFound
	}
	return record, nil
}

func (s *fakeAssetStore) ListBySession(_ context.Context, sessionID string) ([]asset.Asset, error) {
	var out []asset.Asset
	for _, record := range s.records {
		if record.SessionID == sessionID {
			out = append(out, record)
		}
	}
	return out, nil
}

type fakeGenerationJobStore struct {
	bySession     map[string][]generation.GenerationJob
	seenSessionID string
	err           error
}

func (s *fakeGenerationJobStore) ListBySession(_ context.Context, sessionID string) ([]generation.GenerationJob, error) {
	s.seenSessionID = sessionID
	if s.err != nil {
		return nil, s.err
	}
	return append([]generation.GenerationJob(nil), s.bySession[sessionID]...), nil
}

type fakeEventSubscriber struct {
	events        <-chan a2ui.SSEEvent
	seenSessionID string
	unsubscribed  bool
	published     []a2ui.SSEEvent
}

func (s *fakeEventSubscriber) Publish(_ context.Context, event a2ui.SSEEvent) error {
	s.published = append(s.published, event)
	return nil
}

func (s *fakeEventSubscriber) Subscribe(_ context.Context, sessionID string) (<-chan a2ui.SSEEvent, func()) {
	s.seenSessionID = sessionID
	return s.events, func() {
		s.unsubscribed = true
	}
}

type fakeCheckpointStore struct {
	saved     session.CheckpointMapping
	record    session.CheckpointMapping
	resumedID string
	err       error
}

type fakeTransitionCheckpointStore struct {
	*fakeCheckpointStore
	failCompleteOnce bool
}

func (s *fakeTransitionCheckpointStore) TransitionCheckpointMapping(_ context.Context, id string, expectedStatus string, expectedEpoch int64, nextStatus string, decisionVersion int) (session.CheckpointMapping, error) {
	if s.err != nil {
		return session.CheckpointMapping{}, s.err
	}
	if s.record.ID != id || s.record.Status != expectedStatus || s.record.MappingEpoch != expectedEpoch {
		return session.CheckpointMapping{}, session.ErrCheckpointNotFound
	}
	if nextStatus == session.CheckpointStatusResumed && s.failCompleteOnce {
		s.failCompleteOnce = false
		return session.CheckpointMapping{}, errors.New("simulated completion receipt failure")
	}
	s.record.Status = nextStatus
	s.record.DecisionVersion = decisionVersion
	return s.record, nil
}

func (s *fakeCheckpointStore) SaveCheckpointMapping(_ context.Context, record session.CheckpointMapping) (session.CheckpointMapping, error) {
	s.saved = record
	if s.err != nil {
		return session.CheckpointMapping{}, s.err
	}
	if s.record.ID == "" {
		s.record = record
	}
	return record, nil
}

func (s *fakeCheckpointStore) GetCheckpointMapping(_ context.Context, sessionID string, interruptID string) (session.CheckpointMapping, error) {
	if s.err != nil {
		return session.CheckpointMapping{}, s.err
	}
	if s.record.SessionID == sessionID && s.record.InterruptID == interruptID {
		return s.record, nil
	}
	return session.CheckpointMapping{}, session.ErrCheckpointNotFound
}

func (s *fakeCheckpointStore) GetCheckpointMappingByApproval(_ context.Context, approvalID string) (session.CheckpointMapping, error) {
	if s.err != nil {
		return session.CheckpointMapping{}, s.err
	}
	if s.record.ApprovalID == approvalID {
		return s.record, nil
	}
	return session.CheckpointMapping{}, session.ErrCheckpointNotFound
}

func (s *fakeCheckpointStore) MarkCheckpointResumed(_ context.Context, id string) (session.CheckpointMapping, error) {
	s.resumedID = id
	if s.err != nil {
		return session.CheckpointMapping{}, s.err
	}
	s.record.Status = session.CheckpointStatusResumed
	return s.record, nil
}

type fakeAssetUploader struct {
	result asset.UploadResult
	seen   asset.UploadInput
	body   []byte
	err    error
}

func (u *fakeAssetUploader) Upload(_ context.Context, input asset.UploadInput) (asset.UploadResult, error) {
	u.seen = input
	u.body, _ = io.ReadAll(input.Content)
	if u.err != nil {
		return asset.UploadResult{}, u.err
	}
	return u.result, nil
}

type fakeCompensationFinalizer struct {
	job generation.GenerationJob
	err error
}

func (f *fakeCompensationFinalizer) ManualFinalize(context.Context, string, int64, string) (generation.GenerationJob, error) {
	return f.job, f.err
}
