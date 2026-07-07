package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	aigca2ui "github.com/FigoGoo/Dora-Agent/internal/aigc/a2ui"
	aigcsession "github.com/FigoGoo/Dora-Agent/internal/aigc/session"
	aigcskill "github.com/FigoGoo/Dora-Agent/internal/aigc/skill"
)

// fakeSkillSelector 记录调用并返回预设选择。
type fakeSkillSelector struct {
	calls     int
	selection aigcskill.SkillSelection
	err       error
}

func (f *fakeSkillSelector) Select(_ context.Context, _ string, options []aigcskill.SkillOption) (aigcskill.SkillSelection, error) {
	f.calls++
	if f.err != nil {
		return aigcskill.SkillSelection{}, f.err
	}
	return f.selection, nil
}

// recordingPublisher 记录 Publish 的事件。
type recordingPublisher struct {
	events []aigca2ui.SSEEvent
}

func (p *recordingPublisher) Publish(_ context.Context, e aigca2ui.SSEEvent) error {
	p.events = append(p.events, e)
	return nil
}

// 两份候选 skill（带可解析 Skill.md 的最小 Content）。
func seedTwoSkills(skills *fakeSkillStore) {
	skills.records["sk_product"] = aigcskill.SkillRecord{
		ID: "sk_product", Name: "商品宣传短片", Enabled: true,
		Content: "<name>商品宣传短片</name>\n<description>电商带货</description>\n<planner></planner>",
	}
	skills.records["sk_travel"] = aigcskill.SkillRecord{
		ID: "sk_travel", Name: "人文纪录短片", Enabled: true,
		Content: "<name>人文纪录短片</name>\n<description>文旅纪实</description>\n<planner></planner>",
	}
}

func newRouterConfig(store *fakeSessionStore, skills *fakeSkillStore) Config {
	return Config{
		Store:         store,
		Skills:        skills,
		Invoker:       &fakeAgentInvoker{},
		SessionValues: func(r aigcsession.SessionRecord) map[string]any { return map[string]any{"skill_id": r.SkillID} },
		NewID:         func() string { return "run-1" },
		Now:           func() time.Time { return time.Unix(0, 0).UTC() },
	}
}

func postStream(t *testing.T, router http.Handler, sessionID, content string) *httptest.ResponseRecorder {
	t.Helper()
	body := strings.NewReader(`{"content":"` + content + `"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/aigc/sessions/"+sessionID+"/messages/stream", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestRouterAutoSelectsSkillWhenUnbound(t *testing.T) {
	store := newFakeSessionStore()
	store.sessions["s1"] = aigcsession.SessionRecord{ID: "s1"} // SkillID == ""
	skills := &fakeSkillStore{records: map[string]aigcskill.SkillRecord{}}
	seedTwoSkills(skills)
	selector := &fakeSkillSelector{selection: aigcskill.SkillSelection{SkillID: "sk_travel", Reason: "文旅题材"}}
	pub := &recordingPublisher{}
	invoker := &fakeAgentInvoker{}

	cfg := newRouterConfig(store, skills)
	cfg.Invoker = invoker
	cfg.SkillSelector = selector
	cfg.Publisher = pub
	router := NewRouter(cfg)

	rec := postStream(t, router, "s1", "帮我做北京平谷文旅片")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if selector.calls != 1 {
		t.Fatalf("selector 应被调用 1 次, 实际 %d", selector.calls)
	}
	if got := store.sessions["s1"].SkillID; got != "sk_travel" {
		t.Fatalf("session SkillID = %q, 期望 sk_travel", got)
	}
	if len(pub.events) != 1 || pub.events[0].Event != aigca2ui.EventSkillSelected {
		t.Fatalf("应发出 1 条 skill.selected 事件, 实际 %+v", pub.events)
	}
	// 自动选中的 Skill 当轮即生效：本轮 invoke 未被吞掉，且 SessionValues 已带上新 skill_id。
	if invoker.invokeCalls != 1 {
		t.Fatalf("invoke 应被调用 1 次, 实际 %d", invoker.invokeCalls)
	}
	if got := invoker.lastInvoke.SessionValues["skill_id"]; got != "sk_travel" {
		t.Fatalf("本轮 SessionValues[skill_id] = %v, 期望 sk_travel", got)
	}
}

func TestRouterSkipsWhenAlreadyBound(t *testing.T) {
	store := newFakeSessionStore()
	store.sessions["s1"] = aigcsession.SessionRecord{ID: "s1", SkillID: "sk_product"}
	skills := &fakeSkillStore{records: map[string]aigcskill.SkillRecord{}}
	seedTwoSkills(skills)
	selector := &fakeSkillSelector{}
	cfg := newRouterConfig(store, skills)
	cfg.SkillSelector = selector
	cfg.Publisher = &recordingPublisher{}
	router := NewRouter(cfg)

	rec := postStream(t, router, "s1", "任意")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if selector.calls != 0 {
		t.Fatalf("已绑 Skill 时不应调用 selector, 实际 %d", selector.calls)
	}
}

func TestRouterSingleCandidateSkipsLLM(t *testing.T) {
	store := newFakeSessionStore()
	store.sessions["s1"] = aigcsession.SessionRecord{ID: "s1"}
	skills := &fakeSkillStore{records: map[string]aigcskill.SkillRecord{
		"sk_only": {ID: "sk_only", Name: "唯一", Enabled: true,
			Content: "<name>唯一</name>\n<description>d</description>\n<planner></planner>"},
	}}
	selector := &fakeSkillSelector{}
	pub := &recordingPublisher{}
	cfg := newRouterConfig(store, skills)
	cfg.SkillSelector = selector
	cfg.Publisher = pub
	router := NewRouter(cfg)

	rec := postStream(t, router, "s1", "任意")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if selector.calls != 0 {
		t.Fatalf("候选=1 不应调用 selector, 实际 %d", selector.calls)
	}
	if store.sessions["s1"].SkillID != "sk_only" {
		t.Fatalf("应直选唯一候选")
	}
	var payload aigca2ui.SkillSelectedPayload
	if len(pub.events) != 1 {
		t.Fatalf("应发 1 条事件")
	}
	_ = json.Unmarshal(mustJSON(t, pub.events[0].Payload), &payload)
	if payload.Reason != "库中唯一 Skill" {
		t.Fatalf("Reason = %q", payload.Reason)
	}
}

func TestRouterZeroCandidatesSkips(t *testing.T) {
	store := newFakeSessionStore()
	store.sessions["s1"] = aigcsession.SessionRecord{ID: "s1"}
	skills := &fakeSkillStore{records: map[string]aigcskill.SkillRecord{}} // 无 Enabled
	selector := &fakeSkillSelector{}
	pub := &recordingPublisher{}
	cfg := newRouterConfig(store, skills)
	cfg.SkillSelector = selector
	cfg.Publisher = pub
	router := NewRouter(cfg)

	rec := postStream(t, router, "s1", "任意")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if selector.calls != 0 || len(pub.events) != 0 {
		t.Fatalf("候选=0 应跳过 Router")
	}
	if store.sessions["s1"].SkillID != "" {
		t.Fatalf("候选=0 不应绑定 Skill")
	}
}

func TestRouterFallbackOnSelectorError(t *testing.T) {
	store := newFakeSessionStore()
	store.sessions["s1"] = aigcsession.SessionRecord{ID: "s1"}
	skills := &fakeSkillStore{records: map[string]aigcskill.SkillRecord{}}
	seedTwoSkills(skills)
	selector := &fakeSkillSelector{err: errContext()}
	pub := &recordingPublisher{}
	cfg := newRouterConfig(store, skills)
	cfg.SkillSelector = selector
	cfg.DefaultSkillID = "sk_product"
	cfg.Publisher = pub
	router := NewRouter(cfg)

	rec := postStream(t, router, "s1", "任意")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if store.sessions["s1"].SkillID != "sk_product" {
		t.Fatalf("出错应兜底默认 SkillID, 实际 %q", store.sessions["s1"].SkillID)
	}
	var payload aigca2ui.SkillSelectedPayload
	_ = json.Unmarshal(mustJSON(t, pub.events[0].Payload), &payload)
	if !payload.Fallback {
		t.Fatalf("兜底应置 Fallback=true")
	}
}

// helpers
func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return b
}

func errContext() error { return context.DeadlineExceeded }
