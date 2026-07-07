package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/gin-gonic/gin"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/a2ui"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/asset"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/generation"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/mediagraph"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/session"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/skill"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/storyboard"
	aigctools "github.com/FigoGoo/Dora-Agent/internal/aigc/tools"
)

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
		ID:        "asset-1",
		SessionID: "s1",
		Kind:      asset.KindImage,
		Source:    asset.SourceGenerated,
		URL:       "https://tos.doraigc.com/aigc/sessions/s1/assets/asset-1/keyframe.png",
	}
	router := NewRouter(Config{
		Store:   newFakeSessionStore(),
		Assets:  assets,
		Invoker: &fakeAgentInvoker{},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/aigc/assets/asset-1", nil)
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
}

func TestListSessionAssets(t *testing.T) {
	gin.SetMode(gin.TestMode)

	store := newFakeSessionStore()
	store.sessions["s1"] = session.SessionRecord{ID: "s1", Status: "active"}
	assets := newFakeAssetStore()
	assets.records["asset-1"] = asset.Asset{ID: "asset-1", SessionID: "s1", Kind: asset.KindImage}
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
		Event:     a2ui.EventJobStatus,
		Payload:   generation.JobStatusPayload{JobID: "job-1", SessionID: "s1", Status: generation.StatusSucceeded},
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
	if !strings.Contains(out, "event: job.status") || !strings.Contains(out, "job-1") {
		t.Fatalf("missing job status event in %q", out)
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
	assets.records["asset-1"] = asset.Asset{ID: "asset-1", SessionID: "s1", Kind: asset.KindImage}
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

func TestStreamMessageInvokesAgentAndPersistsMessages(t *testing.T) {
	gin.SetMode(gin.TestMode)

	store := newFakeSessionStore()
	store.sessions["s1"] = session.SessionRecord{ID: "s1", Status: "active"}
	store.messages["s1"] = []session.MessageRecord{
		{ID: "m0", SessionID: "s1", Role: "assistant", Content: "上一轮", Seq: 1},
	}
	invoker := &fakeAgentInvoker{
		events: []AgentEvent{
			{
				Event:         a2ui.EventChatDelta,
				Payload:       map[string]any{"text": "好的，"},
				AssistantText: "好的，",
			},
			{
				Event:         a2ui.EventChatDelta,
				Payload:       map[string]any{"text": "开始规划故事板。"},
				AssistantText: "开始规划故事板。",
			},
		},
	}
	router := NewRouter(Config{
		Store:         store,
		Invoker:       invoker,
		NewID:         sequentialIDs("run-1", "msg-user-1", "evt-1", "evt-2", "msg-assistant-1"),
		Now:           fixedNow,
		MessageWindow: session.MessageWindow{Limit: 20},
	})

	body := bytes.NewBufferString(`{"content":"帮我做一个武侠短片"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/aigc/sessions/s1/messages/stream", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("content-type = %q", ct)
	}
	out := rec.Body.String()
	if strings.Contains(out, "event: chat.delta") {
		t.Fatalf("assistant output should use a2ui protocol, got %q", out)
	}
	if !strings.Contains(out, "event: a2ui.surface_update") || !strings.Contains(out, "event: a2ui.data_model_update") {
		t.Fatalf("missing a2ui text card events in %q", out)
	}
	if !strings.Contains(out, "开始规划故事板") {
		t.Fatalf("missing assistant delta in %q", out)
	}

	appended := store.messages["s1"]
	if len(appended) != 3 {
		t.Fatalf("message count = %d, records = %#v", len(appended), appended)
	}
	if appended[1].Role != "user" || appended[1].Content != "帮我做一个武侠短片" {
		t.Fatalf("user message = %#v", appended[1])
	}
	if appended[2].Role != "assistant" || appended[2].Content != "好的，开始规划故事板。" {
		t.Fatalf("assistant message = %#v", appended[2])
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

func TestStreamMessageRendersAssistantMessageThroughA2UIProtocol(t *testing.T) {
	gin.SetMode(gin.TestMode)

	store := newFakeSessionStore()
	store.sessions["s1"] = session.SessionRecord{ID: "s1", Status: "active"}
	assistantText := "故事板草案已生成，请确认。"
	invoker := &fakeAgentInvoker{
		events: []AgentEvent{{
			Event:         a2ui.EventChatDelta,
			Payload:       map[string]any{"text": assistantText},
			AssistantText: assistantText,
			Message:       schema.AssistantMessage(assistantText, nil),
		}},
	}
	router := NewRouter(Config{
		Store:   store,
		Invoker: invoker,
		NewID:   sequentialIDs("run-1", "msg-user-1", "evt-surface", "msg-assistant-1"),
		Now:     fixedNow,
	})

	body := bytes.NewBufferString(`{"content":"开始吧"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/aigc/sessions/s1/messages/stream", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	out := rec.Body.String()
	if strings.Contains(out, "event: chat.delta") {
		t.Fatalf("assistant output should be rendered through a2ui protocol, got %q", out)
	}
	if !strings.Contains(out, "event: a2ui.begin_rendering") {
		t.Fatalf("missing a2ui begin event in %q", out)
	}
	if !strings.Contains(out, "event: a2ui.surface_update") {
		t.Fatalf("missing a2ui surface event in %q", out)
	}
	if !strings.Contains(out, "event: a2ui.data_model_update") {
		t.Fatalf("missing a2ui data update event in %q", out)
	}
	if !strings.Contains(out, assistantText) {
		t.Fatalf("missing assistant content in %q", out)
	}
	if appended := store.messages["s1"]; len(appended) != 2 || appended[1].Role != "assistant" || appended[1].Content != assistantText {
		t.Fatalf("persisted messages = %#v", appended)
	}
}

func TestStreamMessagePassesExplicitA2UIEnvelopeFromAssistant(t *testing.T) {
	gin.SetMode(gin.TestMode)

	store := newFakeSessionStore()
	store.sessions["s1"] = session.SessionRecord{ID: "s1", Status: "active"}
	assistantEnvelope := `{"a2ui_events":[{"event":"a2ui.surface_update","surface_id":"brief-intake","data_model_key":"brief","payload":{"root":"root","title":"补充产品信息","submit_label":"提交信息","components":[{"id":"root","component":{"Card":{"children":["product","style","platforms","steps"]}}},{"id":"product","component":{"TextInput":{"key":"product_name","label":"产品名称/品类","required":true}}},{"id":"style","component":{"SingleChoice":{"key":"visual_style","label":"视觉风格","options":[{"value":"tech","label":"高级科技感"}]}}},{"id":"platforms","component":{"MultiChoice":{"key":"platforms","label":"投放平台","options":[{"value":"douyin","label":"抖音"}]}}},{"id":"steps","component":{"VerticalSteps":{"steps":[{"title":"Agent 分析","status":"running"}]}}}]}}]}`
	invoker := &fakeAgentInvoker{
		events: []AgentEvent{{
			Event:         a2ui.EventChatDelta,
			Payload:       map[string]any{"text": assistantEnvelope},
			AssistantText: assistantEnvelope,
			Message:       schema.AssistantMessage(assistantEnvelope, nil),
		}},
	}
	router := NewRouter(Config{
		Store:   store,
		Invoker: invoker,
		NewID:   sequentialIDs("run-1", "msg-user-1", "evt-begin", "evt-surface", "msg-assistant-1"),
		Now:     fixedNow,
	})

	body := bytes.NewBufferString(`{"content":"开始吧"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/aigc/sessions/s1/messages/stream", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	out := rec.Body.String()
	if strings.Contains(out, "event: chat.delta") {
		t.Fatalf("explicit a2ui envelope should not be emitted as chat.delta, got %q", out)
	}
	if !strings.Contains(out, "event: a2ui.surface_update") || !strings.Contains(out, "SingleChoice") || !strings.Contains(out, "VerticalSteps") {
		t.Fatalf("missing explicit a2ui components in %q", out)
	}
}

func TestStreamMessageExtractsEmbeddedA2UIEnvelopeFromAssistant(t *testing.T) {
	gin.SetMode(gin.TestMode)

	store := newFakeSessionStore()
	store.sessions["s1"] = session.SessionRecord{ID: "s1", Status: "active"}
	assistantEnvelope := `{"a2ui_events":[{"event":"a2ui.surface_update","surface_id":"brief-intake","data_model_key":"brief","payload":{"root":"root","title":"补充产品信息","submit_label":"提交信息","components":[{"id":"root","component":{"Card":{"children":["title","product","style","steps"]}}},{"id":"title","component":{"Text":{"value":"请补充商品宣传短片信息","usageHint":"title"}}},{"id":"product","component":{"TextInput":{"key":"product_name","label":"产品名称/品类","required":true}}},{"id":"style","component":{"SingleChoice":{"key":"visual_style","label":"视觉风格","options":[{"value":"tech","label":"高级科技感"}]}}},{"id":"steps","component":{"VerticalSteps":{"steps":[{"title":"Agent 分析","status":"running"}]}}}]}}]}`
	assistantText := "好的！开始 Stage 1。请先补充资料：" + assistantEnvelope + "请填写以上信息，我收到后进入产品分析阶段。"
	invoker := &fakeAgentInvoker{
		events: []AgentEvent{{
			Event:         a2ui.EventChatDelta,
			Payload:       map[string]any{"text": assistantText},
			AssistantText: assistantText,
			Message:       schema.AssistantMessage(assistantText, nil),
		}},
	}
	router := NewRouter(Config{
		Store:   store,
		Invoker: invoker,
		NewID: sequentialIDs(
			"run-1",
			"msg-user-1",
			"evt-begin",
			"evt-text-surface",
			"evt-text-data",
			"evt-form-surface",
			"msg-assistant-1",
		),
		Now: fixedNow,
	})

	body := bytes.NewBufferString(`{"content":"开始吧"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/aigc/sessions/s1/messages/stream", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	out := rec.Body.String()
	if strings.Contains(out, "event: chat.delta") {
		t.Fatalf("embedded a2ui envelope should not be emitted as chat.delta, got %q", out)
	}
	if !strings.Contains(out, "event: a2ui.surface_update") || !strings.Contains(out, "TextInput") || !strings.Contains(out, "VerticalSteps") {
		t.Fatalf("missing extracted a2ui components in %q", out)
	}
	if strings.Contains(out, `\"a2ui_events\"`) {
		t.Fatalf("raw a2ui envelope leaked into SSE payload: %q", out)
	}
	appended := store.messages["s1"]
	if len(appended) != 2 {
		t.Fatalf("message count = %d, records = %#v", len(appended), appended)
	}
	if strings.Contains(appended[1].Content, "a2ui_events") || !strings.Contains(appended[1].Content, "Stage 1") {
		t.Fatalf("assistant message should persist display text only, got %#v", appended[1])
	}
}

func TestStreamMessageRendersProductBriefRequestAsA2UIForm(t *testing.T) {
	gin.SetMode(gin.TestMode)

	store := newFakeSessionStore()
	store.sessions["s1"] = session.SessionRecord{ID: "s1", Status: "active"}
	assistantText := `好的，收到您的“开始”指令！不过我现在还没有获取到您的产品信息详情。让我换个方式，您可以直接用文字告诉我：
--- **请简单告诉我以下信息：**
1. **产品是什么？**
2. **品牌名称？**
3. **核心卖点？**
4. **目标平台和视频时长？**
5. **想要的视觉风格？**
--- 您不用填表格，直接打字回复我就行！`
	invoker := &fakeAgentInvoker{
		events: []AgentEvent{{
			Event:         a2ui.EventChatDelta,
			Payload:       map[string]any{"text": assistantText},
			AssistantText: assistantText,
			Message:       schema.AssistantMessage(assistantText, nil),
		}},
	}
	router := NewRouter(Config{
		Store:   store,
		Invoker: invoker,
		NewID:   sequentialIDs("run-1", "msg-user-1", "evt-begin", "evt-brief-form", "msg-assistant-1"),
		Now:     fixedNow,
	})

	body := bytes.NewBufferString(`{"content":"开始"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/aigc/sessions/s1/messages/stream", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	out := rec.Body.String()
	if !strings.Contains(out, "event: a2ui.surface_update") {
		t.Fatalf("missing a2ui surface update in %q", out)
	}
	for _, want := range []string{"TextInput", "SingleChoice", "MultiChoice", "VerticalSteps", "产品名称/品类", "核心卖点"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in a2ui form: %q", want, out)
		}
	}
	if strings.Contains(out, "您不用填表格") {
		t.Fatalf("raw text prompt should be replaced by a2ui form, got %q", out)
	}
}

func TestStreamMessagePassesSessionValuesToInvoker(t *testing.T) {
	gin.SetMode(gin.TestMode)

	store := newFakeSessionStore()
	store.sessions["s1"] = session.SessionRecord{ID: "s1", SkillID: "skill-video", Title: "武侠短片", Status: "active"}
	invoker := &fakeAgentInvoker{
		events: []AgentEvent{{
			Event:         a2ui.EventChatDelta,
			Payload:       map[string]any{"text": "收到。"},
			AssistantText: "收到。",
		}},
	}
	router := NewRouter(Config{
		Store:   store,
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
	req := httptest.NewRequest(http.MethodPost, "/api/aigc/sessions/s1/messages/stream", body)
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

func TestStreamMessagePersistsToolCallAndToolResult(t *testing.T) {
	gin.SetMode(gin.TestMode)

	store := newFakeSessionStore()
	store.sessions["s1"] = session.SessionRecord{ID: "s1", Status: "active"}
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
				Payload:       map[string]any{"text": "规范已更新。"},
				AssistantText: "规范已更新。",
			},
		},
	}
	router := NewRouter(Config{
		Store:   store,
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
	req := httptest.NewRequest(http.MethodPost, "/api/aigc/sessions/s1/messages/stream", body)
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
	if appended[3].Role != string(schema.Assistant) || appended[3].Content != "规范已更新。" {
		t.Fatalf("assistant text record = %#v", appended[3])
	}

	rebuilt := recordsToSchemaMessages(appended)
	if len(rebuilt[1].ToolCalls) != 1 || rebuilt[1].ToolCalls[0].ID != "call-1" {
		t.Fatalf("rebuilt assistant tool calls = %#v", rebuilt[1])
	}
	if rebuilt[2].Role != schema.Tool || rebuilt[2].ToolCallID != "call-1" {
		t.Fatalf("rebuilt tool result = %#v", rebuilt[2])
	}
}

func TestStreamMessageRendersToolProgressWithoutRawToolPayloads(t *testing.T) {
	gin.SetMode(gin.TestMode)

	store := newFakeSessionStore()
	store.sessions["s1"] = session.SessionRecord{ID: "s1", Status: "active"}
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
						Arguments: `{"document_type":"final_video_spec","markdown":"# Final_Video_Spec.md"}`,
					},
				}}),
			},
			{
				Event:   a2ui.EventToolProgress,
				Payload: map[string]any{"role": "tool"},
				Message: schema.ToolMessage(`{"status":"error","error":{"tool_key":"text_editor","code":"validation_error","user_message":"工具参数不完整或格式不正确，需要重新组织参数。","technical_message":"session_id is required","trace_id":"call-1"}}`, "call-1", schema.WithToolName(aigctools.TextEditorToolKey)),
			},
			{
				Event:   a2ui.EventToolProgress,
				Payload: map[string]any{"role": "tool"},
				Message: schema.ToolMessage(`{"status":"ok","data":{"document_type":"final_video_spec","spec":{"id":"final_video_spec:s1","markdown":"# Final_Video_Spec.md"}}}`, "call-2", schema.WithToolName(aigctools.TextEditorToolKey)),
			},
		},
	}
	router := NewRouter(Config{
		Store:   store,
		Invoker: invoker,
		NewID: sequentialIDs(
			"run-1",
			"msg-user-1",
			"evt-begin",
			"evt-tool-call",
			"msg-tool-call",
			"evt-tool-error",
			"msg-tool-error",
			"evt-tool-ok",
			"msg-tool-ok",
		),
		Now: fixedNow,
	})

	body := bytes.NewBufferString(`{"content":"先写规范"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/aigc/sessions/s1/messages/stream", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	out := rec.Body.String()
	for _, want := range []string{"event: a2ui.surface_update", "VerticalSteps", "text_editor", "工具参数不完整或格式不正确"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in progress surface: %q", want, out)
		}
	}
	for _, leaked := range []string{"document_type", "Final_Video_Spec.md", "session_id is required", "trace_id", `"status\":\"ok\"`} {
		if strings.Contains(out, leaked) {
			t.Fatalf("raw tool payload leaked %q in %q", leaked, out)
		}
	}
}

func TestStreamMessageRendersStageConfirmationAsA2UISurface(t *testing.T) {
	gin.SetMode(gin.TestMode)

	store := newFakeSessionStore()
	store.sessions["s1"] = session.SessionRecord{ID: "s1", Status: "active"}
	assistantText := `**Final_Video_Spec.md** 已创建完毕！以下是视频规格概要，请您确认——如果有任何需要调整的地方，请告诉我：
--- ## 视频规格草案 | 项目 | 内容 |
| **标题** | 蓝牙音箱品牌宣传短片 |
| **类型** | 商品宣传短片 |
| **比例** | 9:16 竖屏 |
⚠️ **注意：** 由于您还没有提供具体的品牌名称和产品图片，目前规格基于“蓝牙音箱”做了通用设定。请您确认：
1. 以上规格是否符合您的预期？
2. 是否有具体的品牌名称要补充？`
	invoker := &fakeAgentInvoker{
		events: []AgentEvent{{
			Event:         a2ui.EventChatDelta,
			Payload:       map[string]any{"text": assistantText},
			AssistantText: assistantText,
			Message:       schema.AssistantMessage(assistantText, nil),
		}},
	}
	router := NewRouter(Config{
		Store:   store,
		Invoker: invoker,
		NewID:   sequentialIDs("run-1", "msg-user-1", "evt-begin", "evt-confirm-surface", "evt-confirm-data", "msg-assistant-1"),
		Now:     fixedNow,
	})

	body := bytes.NewBufferString(`{"content":"开始"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/aigc/sessions/s1/messages/stream", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	out := rec.Body.String()
	for _, want := range []string{"event: a2ui.surface_update", "stage-confirmation", "SingleChoice", "TextInput", "VerticalSteps", "提交确认"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in confirmation surface: %q", want, out)
		}
	}
}

func TestStreamMessageEmitsRenderEventsFromToolResult(t *testing.T) {
	gin.SetMode(gin.TestMode)

	store := newFakeSessionStore()
	store.sessions["s1"] = session.SessionRecord{ID: "s1", Status: "active"}
	toolResult := `{
		"status":"ok",
		"data":{
			"asset_ids":["asset-1"],
			"render_events":[{
				"event":"storyboard.patch",
				"surface_id":"storyboard",
				"data_model_key":"storyboard",
				"payload":{"updates":[{"target_type":"shot","target_id":"shot-1","field":"keyframe_asset_id","asset_ids":["asset-1"],"status":"generated"}]}
			}]
		}
	}`
	invoker := &fakeAgentInvoker{
		events: []AgentEvent{
			{
				Event:   a2ui.EventToolProgress,
				Payload: map[string]any{"role": "tool"},
				Message: schema.ToolMessage(toolResult, "call-1", schema.WithToolName(aigctools.Image2GenerateToolKey)),
			},
		},
	}
	router := NewRouter(Config{
		Store:   store,
		Invoker: invoker,
		NewID:   sequentialIDs("run-1", "msg-user-1", "evt-tool", "evt-render", "msg-tool"),
		Now:     fixedNow,
	})

	body := bytes.NewBufferString(`{"content":"生成第一镜关键帧"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/aigc/sessions/s1/messages/stream", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	out := rec.Body.String()
	if !strings.Contains(out, "event: storyboard.patch") {
		t.Fatalf("missing storyboard.patch event in %q", out)
	}
	if !strings.Contains(out, "keyframe_asset_id") {
		t.Fatalf("missing render payload in %q", out)
	}
}

func TestStreamMessageMaterializesStoryboardUpdatesFromToolResult(t *testing.T) {
	gin.SetMode(gin.TestMode)

	store := newFakeSessionStore()
	store.sessions["s1"] = session.SessionRecord{ID: "s1", Status: "active"}
	storyboards := &fakeStoryboardStore{
		latest: storyboard.Storyboard{
			ID:        "storyboard-1",
			SessionID: "s1",
			Version:   3,
			Shots: []storyboard.Shot{{
				ShotID:           "shot-1",
				Index:            1,
				SceneDescription: "竹林归隐",
				Status:           storyboard.StatusGenerating,
			}},
		},
		patched: storyboard.Storyboard{ID: "storyboard-1", SessionID: "s1", Version: 4},
		event: storyboard.EventRecord{
			ID:           "patch-event-1",
			SessionID:    "s1",
			StoryboardID: "storyboard-1",
			BaseVersion:  3,
			NextVersion:  4,
			Source:       "tool",
			Ops: []aigctools.JSONPatchOp{
				{Op: "add", Path: "/shots/0/keyframe_asset_id", Value: "asset-1"},
				{Op: "replace", Path: "/shots/0/status", Value: storyboard.StatusReady},
			},
		},
	}
	toolResult := `{
		"status":"ok",
		"data":{
			"render_events":[{
				"event":"storyboard.patch",
				"surface_id":"storyboard",
				"data_model_key":"storyboard",
				"payload":{"updates":[{"target_type":"shot","target_id":"shot-1","field":"keyframe_asset_id","asset_kind":"image","asset_ids":["asset-1"],"status":"generated"}]}
			}]
		}
	}`
	invoker := &fakeAgentInvoker{
		events: []AgentEvent{{
			Event:   a2ui.EventToolProgress,
			Payload: map[string]any{"role": "tool"},
			Message: schema.ToolMessage(toolResult, "call-1", schema.WithToolName(aigctools.Image2GenerateToolKey)),
		}},
	}
	router := NewRouter(Config{
		Store:       store,
		Storyboards: storyboards,
		Invoker:     invoker,
		NewID:       sequentialIDs("run-1", "msg-user-1", "evt-tool", "patch-event-1", "evt-render", "msg-tool"),
		Now:         fixedNow,
	})

	body := bytes.NewBufferString(`{"content":"生成第一镜关键帧"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/aigc/sessions/s1/messages/stream", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if storyboards.seenPatch.StoryboardID != "storyboard-1" || storyboards.seenPatch.BaseVersion != 3 {
		t.Fatalf("storyboard patch request = %#v", storyboards.seenPatch)
	}
	if len(storyboards.seenPatch.Ops) != 2 ||
		storyboards.seenPatch.Ops[0].Path != "/shots/0/keyframe_asset_id" ||
		storyboards.seenPatch.Ops[0].Value != "asset-1" {
		t.Fatalf("storyboard patch ops = %#v", storyboards.seenPatch.Ops)
	}
	out := rec.Body.String()
	if !strings.Contains(out, `"ops"`) || strings.Contains(out, `"updates"`) {
		t.Fatalf("render event should emit materialized JSON patch only, got %q", out)
	}
}

func TestStreamMessageEmitsInterruptFromMediaGeneratorToolResult(t *testing.T) {
	gin.SetMode(gin.TestMode)

	store := newFakeSessionStore()
	store.sessions["s1"] = session.SessionRecord{ID: "s1", Status: "active"}
	toolResult := `{
		"status":"queued",
		"next_confirmation_id":"interrupt-1",
		"data":{
			"interrupted":true,
			"interrupt":{
				"event":"a2ui.interrupt_request",
				"payload":{
					"checkpoint_id":"media-cp-1",
					"interrupt_id":"interrupt-1",
					"storyboard_version":7,
					"title":"确认参考图",
					"message":"请确认参考图",
					"actions":[{"key":"confirm_reference_image","label":"确认参考图"}]
				}
			}
		}
	}`
	invoker := &fakeAgentInvoker{
		events: []AgentEvent{{
			Event:   a2ui.EventToolProgress,
			Payload: map[string]any{"role": "tool"},
			Message: schema.ToolMessage(toolResult, "call-1", schema.WithToolName(aigctools.MediaGeneratorToolKey)),
		}},
	}
	router := NewRouter(Config{
		Store:   store,
		Invoker: invoker,
		NewID:   sequentialIDs("run-1", "msg-user-1", "evt-tool", "evt-interrupt", "msg-tool"),
		Now:     fixedNow,
	})

	body := bytes.NewBufferString(`{"content":"生成参考图"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/aigc/sessions/s1/messages/stream", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	out := rec.Body.String()
	if !strings.Contains(out, "event: a2ui.interrupt_request") {
		t.Fatalf("missing interrupt event in %q", out)
	}
	if !strings.Contains(out, `"scope":"media_graph"`) || !strings.Contains(out, "media-cp-1") {
		t.Fatalf("missing media graph interrupt payload in %q", out)
	}
}

func TestStreamMessagePersistsInterruptCheckpoint(t *testing.T) {
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
	router := NewRouter(Config{
		Store:       store,
		Checkpoints: checkpoints,
		Invoker:     invoker,
		NewID:       sequentialIDs("run-1", "msg-user-1", "evt-interrupt"),
		Now:         fixedNow,
	})

	body := bytes.NewBufferString(`{"content":"确认前暂停"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/aigc/sessions/s1/messages/stream", body)
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
	router := NewRouter(Config{
		Store:       store,
		Checkpoints: checkpoints,
		Invoker:     invoker,
		NewID:       sequentialIDs("run-1", "msg-user-1", "evt-1", "msg-assistant-1"),
		Now:         fixedNow,
	})

	body := bytes.NewBufferString(`{"checkpoint_id":"s1","interrupt_id":"interrupt-1","content":"确认","data":{"approved":true}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/aigc/sessions/s1/messages/resume/stream", body)
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

func TestResumeAgentAlreadyResumedDoesNotInvokeRunner(t *testing.T) {
	gin.SetMode(gin.TestMode)

	store := newFakeSessionStore()
	store.sessions["s1"] = session.SessionRecord{ID: "s1", Status: "active"}
	checkpoints := &fakeCheckpointStore{
		record: session.CheckpointMapping{
			ID:          "checkpoint-map-1",
			SessionID:   "s1",
			Scope:       session.CheckpointScopeRunner,
			InterruptID: "interrupt-1",
			Status:      session.CheckpointStatusResumed,
		},
	}
	invoker := &fakeAgentInvoker{
		events: []AgentEvent{{
			Event:         a2ui.EventChatDelta,
			Payload:       map[string]any{"text": "不应被调用"},
			AssistantText: "不应被调用",
		}},
	}
	router := NewRouter(Config{
		Store:       store,
		Checkpoints: checkpoints,
		Invoker:     invoker,
		NewID:       sequentialIDs("evt-already"),
		Now:         fixedNow,
	})

	body := bytes.NewBufferString(`{"checkpoint_id":"s1","interrupt_id":"interrupt-1","content":"确认","data":{"approved":true}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/aigc/sessions/s1/messages/resume/stream", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if invoker.seenResume.CheckpointID != "" {
		t.Fatalf("runner was invoked: %#v", invoker.seenResume)
	}
	if !strings.Contains(rec.Body.String(), `"status":"resumed"`) {
		t.Fatalf("resume response = %q", rec.Body.String())
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
			Ops:          []aigctools.JSONPatchOp{{Op: "replace", Path: "/status", Value: storyboard.StatusReviewing}},
		},
	}
	router := NewRouter(Config{
		Store:       store,
		Storyboards: storyboards,
		Invoker:     &fakeAgentInvoker{},
		NewID:       sequentialIDs("patch-event-1"),
	})

	body := bytes.NewBufferString(`{"base_version":3,"source":"user","ops":[{"op":"replace","path":"/status","value":"reviewing"}]}`)
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

func TestResumeMediaGraph(t *testing.T) {
	gin.SetMode(gin.TestMode)

	store := newFakeSessionStore()
	store.sessions["s1"] = session.SessionRecord{ID: "s1", Status: "active"}
	resumer := &fakeMediaGraphResumer{
		result: mediagraph.RunResult{
			Output: mediagraph.MediaGeneratorOutput{
				StoryboardID:      "storyboard-1",
				StoryboardVersion: 5,
				Status:            mediagraph.StatusReferenceConfirmed,
				JobIDs:            []string{"job:storyboard-1:6"},
			},
		},
	}
	router := NewRouter(Config{
		Store:      store,
		MediaGraph: resumer,
		Invoker:    &fakeAgentInvoker{},
	})

	body := bytes.NewBufferString(`{"checkpoint_id":"media-cp-1","interrupt_id":"interrupt-1","approved":true,"note":"可以继续"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/aigc/sessions/s1/media-graph/resume", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got resumeMediaGraphResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if got.Status != "completed" || got.Output.Status != mediagraph.StatusReferenceConfirmed {
		t.Fatalf("resume response = %#v", got)
	}
	if resumer.checkpointID != "media-cp-1" || resumer.interruptID != "interrupt-1" {
		t.Fatalf("resume ids = %q %q", resumer.checkpointID, resumer.interruptID)
	}
	if !resumer.decision.Approved || resumer.decision.Note != "可以继续" {
		t.Fatalf("decision = %#v", resumer.decision)
	}
}

func TestResumeAgentStreamsAndPersistsDecision(t *testing.T) {
	gin.SetMode(gin.TestMode)

	store := newFakeSessionStore()
	store.sessions["s1"] = session.SessionRecord{ID: "s1", Status: "active"}
	invoker := &fakeAgentInvoker{
		events: []AgentEvent{{
			Event:         a2ui.EventChatDelta,
			Payload:       map[string]any{"text": "继续生成素材。"},
			AssistantText: "继续生成素材。",
		}},
	}
	router := NewRouter(Config{
		Store:   store,
		Invoker: invoker,
		NewID:   sequentialIDs("run-1", "msg-user-1", "evt-1", "msg-assistant-1"),
		Now:     fixedNow,
	})

	body := bytes.NewBufferString(`{"checkpoint_id":"s1","interrupt_id":"agent:A;tool:confirm","content":"确认参考图，可以继续","data":{"approved":true}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/aigc/sessions/s1/messages/resume/stream", body)
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
	appended := store.messages["s1"]
	if len(appended) != 2 {
		t.Fatalf("message count = %d, records = %#v", len(appended), appended)
	}
	if appended[0].Role != string(schema.User) || appended[0].Content != "确认参考图，可以继续" {
		t.Fatalf("resume user message = %#v", appended[0])
	}
	if appended[1].Role != string(schema.Assistant) || appended[1].Content != "继续生成素材。" {
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
	if window.Limit > 0 && len(records) > window.Limit {
		records = records[len(records)-window.Limit:]
	}
	return records, nil
}

type fakeAgentInvoker struct {
	events            []AgentEvent
	seenMessages      []*schema.Message
	seenSessionValues map[string]any
	seenResume        AgentResumeRequest
	invokeCalls       int
	lastInvoke        AgentInvokeRequest
}

func (i *fakeAgentInvoker) Invoke(_ context.Context, req AgentInvokeRequest) (<-chan AgentEvent, error) {
	i.invokeCalls++
	i.lastInvoke = req
	i.seenMessages = append([]*schema.Message(nil), req.Messages...)
	i.seenSessionValues = req.SessionValues
	ch := make(chan AgentEvent, len(i.events))
	for _, event := range i.events {
		ch <- event
	}
	close(ch)
	return ch, nil
}

func (i *fakeAgentInvoker) Resume(_ context.Context, req AgentResumeRequest) (<-chan AgentEvent, error) {
	i.seenResume = req
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

type fakeMediaGraphResumer struct {
	result       mediagraph.RunResult
	err          error
	checkpointID string
	interruptID  string
	decision     mediagraph.ReferenceConfirmDecision
}

func (r *fakeMediaGraphResumer) Resume(_ context.Context, checkpointID string, interruptID string, decision mediagraph.ReferenceConfirmDecision) (mediagraph.RunResult, error) {
	r.checkpointID = checkpointID
	r.interruptID = interruptID
	r.decision = decision
	if r.err != nil {
		return mediagraph.RunResult{}, r.err
	}
	return r.result, nil
}
