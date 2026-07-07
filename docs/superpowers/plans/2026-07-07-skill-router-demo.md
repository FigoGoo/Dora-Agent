# Skill Router + 文档展示 Demo Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在真 App 里补上两条能力：① 用户未手动选 Skill 时由模型自动选一个（Skill Router），② 一个只读「文档」tab 展示项目的 `Final_Video_Spec.md` 与绑定的 `skill.md`。

**Architecture:** Slice ① 是纯写路径的最小侵入——在 `streamMessage` 拿到 `sessionRecord` 后、构造 `AgentInvokeRequest` 前，若 `SkillID == ""` 就用一个隔离可单测的 `SkillSelector`（LLM 分类）选中一个 Skill、写回 session、经 a2ui 广播 `skill.selected` 事件；Agent 层不改（`SessionValues` 自然带上新 `SkillID`，`adkskill` 中间件照常注入）。Slice ② 是两条纯只读 GET 端点复用现成 store 方法，加一个前端视图切换。两个 slice 正交。

**Tech Stack:** Go 1.26 / Gin、Eino ADK（`einomodel.BaseChatModel`）、a2ui SSE pub/sub（`MemoryBroker`）、GORM Postgres store、React 19 + Vite + Vitest。

**权威依据：** 设计规格 `docs/superpowers/specs/2026-07-07-skill-router-demo-design.md`。所有生成/视频/旁白/音乐**仅用现有默认**（image2 / seedance / demo audio），不新增 handler。

---

## 关键既有事实（实现前必读，均已核对代码）

- `server.Config`（`internal/aigc/server/router.go:102`）当前字段：`Store SessionStore`、`Skills SkillStore`、`Storyboards`、`Assets`、`GenerationJobs`、`AssetUploader`、`Events a2ui.EventSubscriber`、`Checkpoints`、`Invoker`、`MediaGraph`、`SessionValues func(session.SessionRecord) map[string]any`、`MessageWindow`、`NewID func() string`、`Now func() time.Time`。**没有 `Specs`、没有 `SkillSelector`、没有 `Publisher`——本计划新增这三个。**
- `streamMessage`（`internal/aigc/server/router.go:977`）：`GetSession` → bind JSON 得 `content` → `appendSchemaMessage` → `ListMessages` → 构造 `invokeReq := AgentInvokeRequest{Messages, CheckpointID: sessionID}` → `if cfg.SessionValues != nil { invokeReq.SessionValues = cfg.SessionValues(sessionRecord) }` → `cfg.Invoker.Invoke` → `cfg.streamAgentEvents`。**接线点：在 `invokeReq` 构造之前（router.go:1026 前）。**
- `SkillStore` 接口（router.go:56）：`Save`、`Get(ctx, skillID) (skill.SkillRecord, error)`、`ListEnabled(ctx) ([]skill.SkillRecord, error)`。均已存在。
- `skill.SkillRecord`（`internal/aigc/skill/postgres_store.go:15`）字段：`ID, Name, Description, Version, Content, Enabled, CreatedAt, UpdatedAt`。`Content` = skill.md 原文。
- `skill.ParseSkill(content) (SkillPlan, error)`；`SkillPlan` 有 `Name`、`Description`（`internal/aigc/skill/parser.go`）。
- `session.SessionRecord`（`internal/aigc/session/*.go`）有 `SkillID string`。`SessionStore.SaveSession(ctx, record) error` 已存在。
- a2ui：`type EventPublisher interface { Publish(ctx, SSEEvent) error }`、`type EventSubscriber interface { Subscribe(...) }`（`internal/aigc/a2ui/broker.go:10,14`）。`MemoryBroker` 同时满足两者。`main.go:115` 建了 `eventBroker := aigca2ui.NewMemoryBroker(64)`，已作为 `Events` 传入 Config，也作为 worker 的 Publisher。
- `a2ui.SSEEvent` 字段：`ID, SessionID, RunID, Seq, Event, SurfaceID, DataModelKey, Payload, CreatedAt`。
- `spec.PostgresStore.GetLatestBySession(ctx, sessionID) (spec.FinalVideoSpec, error)`（`internal/aigc/spec/postgres_store.go:108`），无记录返回包 `spec.ErrNotFound`（postgres_store.go:14）。`FinalVideoSpec` 有 `Markdown` 字段。
- DeepSeek 模型构造：`agent.NewDeepSeekChatModel(ctx, cfg) (einomodel.ToolCallingChatModel, error)`（`internal/aigc/agent/deepseek.go:43`）。`ToolCallingChatModel` 内嵌 `einomodel.BaseChatModel`，后者有 `Generate(ctx, []*schema.Message, ...einomodel.Option) (*schema.Message, error)`。
- 测试 fakes 在 `internal/aigc/server/router_test.go`：`fakeSessionStore`（含 `records map`、`GetSession`/`SaveSession`）、`fakeSkillStore`（含 `Get`/`ListEnabled`）、`fakeAgentInvoker`（`Invoke` 返回 chan）、`newFakeSessionStore()`。测试用 `NewRouter(Config{...})` + `httptest`。
- 前端 `frontend/src/features/aigc/AigcWorkspacePage.jsx`：`EVENT_NAMES` 数组（约 line 20-34，含 `'storyboard.patch'`、`'job.status'`、`'error'`）；EventSource 订阅 `/api/aigc/sessions/${sessionID}/events/stream` 并对每个 `EVENT_NAMES` 注册 listener（line 311-314）；`handleA2UIEvent` 里按 `protocolName === 'storyboard.patch'` 等分支处理（line 216-224）；故事板面板 `<section className="aigc-storyboard-pane">`（line 513）。

---

## 文件结构

**新建**
- `internal/aigc/skill/selector.go` — `SkillOption`/`SkillSelection`/`SkillSelector` 类型 + LLM 实现 `llmSkillSelector`。
- `internal/aigc/skill/selector_test.go` — 纯 LLM 分类单测（fake chat model）。
- `internal/aigc/server/skill_router.go` — 调用方兜底：`listEnabledSkillOptions`、`resolveSkillSelection`、`emitSkillSelected`。
- `internal/aigc/server/skill_router_test.go` — 兜底 + 接线集成测。
- `internal/aigc/server/documents.go` — `GET /spec`、`GET /skill` 两个只读 handler + `FinalVideoSpecReader` 接口。
- `internal/aigc/server/documents_test.go` — 端点集成测。

**修改**
- `internal/aigc/a2ui/events.go` — 加 `EventSkillSelected` 常量 + `SkillSelectedPayload`。
- `internal/aigc/server/router.go` — `Config` 加 `SkillSelector`/`DefaultSkillID`/`Publisher`/`Specs` 四字段；`streamMessage` 接线；注册两条新路由。
- `cmd/aigc-agent/main.go` — 装配真实 `llmSkillSelector`、`Publisher=eventBroker`、`Specs=specStore`。
- `frontend/src/features/aigc/AigcWorkspacePage.jsx` — `skill.selected` 提示条 + 「文档」视图切换。

---

## Slice ① · Skill Router（后端）

### Task 1: SkillSelector 接口 + LLM 实现

**Files:**
- Create: `internal/aigc/skill/selector.go`
- Test: `internal/aigc/skill/selector_test.go`

- [ ] **Step 1: 写失败测试**

`internal/aigc/skill/selector_test.go`:

```go
package skill

import (
	"context"
	"errors"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// fakeChatModel 用固定文本回应 Generate，模拟 LLM 分类输出。
type fakeChatModel struct {
	reply string
	err   error
	calls int
}

func (f *fakeChatModel) Generate(_ context.Context, _ []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return schema.AssistantMessage(f.reply, nil), nil
}

func (f *fakeChatModel) Stream(_ context.Context, _ []*schema.Message, _ ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, errors.New("not implemented")
}

func testOptions() []SkillOption {
	return []SkillOption{
		{ID: "sk_product", Name: "商品宣传短片", Description: "电商带货、卖点驱动的短片"},
		{ID: "sk_travel", Name: "人文纪录短片", Description: "城市/文旅/纪实风格"},
	}
}

func TestLLMSkillSelectorPicksCandidate(t *testing.T) {
	fm := &fakeChatModel{reply: `{"skill_id":"sk_travel","reason":"文旅题材"}`}
	sel := NewLLMSkillSelector(fm)

	got, err := sel.Select(context.Background(), "帮我做北京平谷文旅宣传片", testOptions())
	if err != nil {
		t.Fatalf("Select 出错: %v", err)
	}
	if got.SkillID != "sk_travel" {
		t.Fatalf("SkillID = %q, 期望 sk_travel", got.SkillID)
	}
	if got.Reason != "文旅题材" {
		t.Fatalf("Reason = %q", got.Reason)
	}
	if got.Fallback {
		t.Fatalf("正常命中不应 Fallback")
	}
	if fm.calls != 1 {
		t.Fatalf("应调用 LLM 一次, 实际 %d", fm.calls)
	}
}

func TestLLMSkillSelectorRejectsUnknownID(t *testing.T) {
	fm := &fakeChatModel{reply: `{"skill_id":"sk_nope","reason":"x"}`}
	sel := NewLLMSkillSelector(fm)

	if _, err := sel.Select(context.Background(), "brief", testOptions()); err == nil {
		t.Fatalf("越界 skill_id 应返回 error")
	}
}

func TestLLMSkillSelectorRejectsBadJSON(t *testing.T) {
	fm := &fakeChatModel{reply: "这不是 JSON"}
	sel := NewLLMSkillSelector(fm)

	if _, err := sel.Select(context.Background(), "brief", testOptions()); err == nil {
		t.Fatalf("非法 JSON 应返回 error")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/aigc/skill -run TestLLMSkillSelector -v`
Expected: 编译失败 —— `undefined: SkillOption` / `undefined: NewLLMSkillSelector`。

- [ ] **Step 3: 写最小实现**

`internal/aigc/skill/selector.go`:

```go
package skill

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// SkillOption 是一个候选 Skill 的最小描述（供选择用）。
type SkillOption struct {
	ID          string
	Name        string
	Description string
}

// SkillSelection 是一次选择的结果。
type SkillSelection struct {
	SkillID  string
	Reason   string // 为什么选它（透传给前端）
	Fallback bool   // 是否走了兜底（本接口不置位，由调用方兜底逻辑设置）
}

// SkillSelector 只负责“从候选里选哪个”，不做绑定、不发事件、不碰 session。
type SkillSelector interface {
	Select(ctx context.Context, brief string, options []SkillOption) (SkillSelection, error)
}

// chatModel 是 selector 需要的最小聊天模型能力，einomodel.BaseChatModel 天然满足。
type chatModel interface {
	Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error)
}

type llmSkillSelector struct {
	model chatModel
}

// NewLLMSkillSelector 用一个聊天模型构造 LLM 版选择器。
func NewLLMSkillSelector(m chatModel) SkillSelector {
	return &llmSkillSelector{model: m}
}

func (s *llmSkillSelector) Select(ctx context.Context, brief string, options []SkillOption) (SkillSelection, error) {
	if len(options) == 0 {
		return SkillSelection{}, fmt.Errorf("no skill options")
	}
	var b strings.Builder
	b.WriteString("你是一个短视频创作 Skill 路由器。根据用户诉求，从候选 Skill 中选出最合适的一个。\n")
	b.WriteString("只能从下列候选的 id 中选择，必须返回严格 JSON：{\"skill_id\":\"<候选id>\",\"reason\":\"<不超过30字理由>\"}，不要输出任何其它内容。\n\n")
	b.WriteString("用户诉求：\n")
	b.WriteString(brief)
	b.WriteString("\n\n候选 Skill：\n")
	for _, o := range options {
		fmt.Fprintf(&b, "- id=%s | 名称=%s | 说明=%s\n", o.ID, o.Name, o.Description)
	}

	msg, err := s.model.Generate(ctx, []*schema.Message{schema.UserMessage(b.String())})
	if err != nil {
		return SkillSelection{}, fmt.Errorf("skill selector generate: %w", err)
	}

	raw := extractJSONObject(msg.Content)
	var parsed struct {
		SkillID string `json:"skill_id"`
		Reason  string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return SkillSelection{}, fmt.Errorf("skill selector parse %q: %w", msg.Content, err)
	}
	for _, o := range options {
		if o.ID == parsed.SkillID {
			return SkillSelection{SkillID: parsed.SkillID, Reason: strings.TrimSpace(parsed.Reason)}, nil
		}
	}
	return SkillSelection{}, fmt.Errorf("skill selector returned unknown id %q", parsed.SkillID)
}

// extractJSONObject 从可能含前后缀文本的模型输出里截取第一个 {...} 段。
func extractJSONObject(s string) string {
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start < 0 || end < 0 || end < start {
		return s
	}
	return s[start : end+1]
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/aigc/skill -run TestLLMSkillSelector -v`
Expected: PASS（3 个用例）。

- [ ] **Step 5: 提交**

```bash
git add internal/aigc/skill/selector.go internal/aigc/skill/selector_test.go
git commit -m "feat(skill): add SkillSelector interface + LLM implementation

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 2: skill.selected 事件常量与 payload

**Files:**
- Modify: `internal/aigc/a2ui/events.go`
- Test: `internal/aigc/a2ui/events_test.go`（若不存在则创建）

- [ ] **Step 1: 写失败测试**

在 `internal/aigc/a2ui/events_test.go` 追加（无此文件则创建，`package a2ui`）：

```go
package a2ui

import (
	"encoding/json"
	"testing"
)

func TestSkillSelectedPayloadJSON(t *testing.T) {
	if EventSkillSelected != "skill.selected" {
		t.Fatalf("EventSkillSelected = %q", EventSkillSelected)
	}
	b, err := json.Marshal(SkillSelectedPayload{
		SkillID:   "sk_travel",
		SkillName: "人文纪录短片",
		Reason:    "文旅题材",
		Fallback:  false,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(b)
	want := `{"skill_id":"sk_travel","skill_name":"人文纪录短片","reason":"文旅题材"}`
	if got != want {
		t.Fatalf("json = %s, want %s", got, want)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/aigc/a2ui -run TestSkillSelectedPayloadJSON -v`
Expected: 编译失败 —— `undefined: EventSkillSelected` / `undefined: SkillSelectedPayload`。

- [ ] **Step 3: 写最小实现**

在 `internal/aigc/a2ui/events.go` 的事件常量区追加常量，并在类型区追加 payload：

```go
// EventSkillSelected 表示 Router 在用户未手动选 Skill 时自动选中了一个。
const EventSkillSelected = "skill.selected"

// SkillSelectedPayload 随 skill.selected 事件发给前端。
type SkillSelectedPayload struct {
	SkillID   string `json:"skill_id"`
	SkillName string `json:"skill_name"`
	Reason    string `json:"reason"`
	Fallback  bool   `json:"fallback,omitempty"`
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/aigc/a2ui -run TestSkillSelectedPayloadJSON -v`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add internal/aigc/a2ui/events.go internal/aigc/a2ui/events_test.go
git commit -m "feat(a2ui): add skill.selected event and payload

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 3: Config 依赖 + 兜底 + streamMessage 接线

**Files:**
- Modify: `internal/aigc/server/router.go`（`Config` 加 3 字段；`streamMessage` 接线；导入 `skill`——已导入）
- Create: `internal/aigc/server/skill_router.go`
- Test: `internal/aigc/server/skill_router_test.go`

- [ ] **Step 1: 写失败测试**

`internal/aigc/server/skill_router_test.go`:

```go
package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/schema"

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

	cfg := newRouterConfig(store, skills)
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

var _ = schema.UserMessage // 保留 schema 依赖占位（若未直接用可删）
```

> 注：若 `fakeSessionStore` 的字段名不是 `records`、`fakeSkillStore` 的不是 `records`，实现时以 `router_test.go` 里真实定义为准同步改这些测试引用（`newFakeSessionStore()` 已存在）。模块路径 `github.com/FigoGoo/Dora-Agent` 以 `go.mod` 第一行为准替换。

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/aigc/server -run TestRouter -v`
Expected: 编译失败 —— `Config` 无 `SkillSelector`/`Publisher`/`DefaultSkillID` 字段。

- [ ] **Step 3a: 给 Config 加字段**

在 `internal/aigc/server/router.go` 的 `type Config struct { ... }`（router.go:102）内追加：

```go
	// Skill Router：未手动绑 Skill 时自动选一个。均可为 nil（则不启用 Router）。
	SkillSelector  skill.SkillSelector
	DefaultSkillID string
	Publisher      a2ui.EventPublisher
```

（`skill` 与 `a2ui` 包在 router.go 已导入。）

- [ ] **Step 3b: 写兜底与接线辅助**

`internal/aigc/server/skill_router.go`:

```go
package server

import (
	"context"
	"log"

	"github.com/cloudwego/eino/schema"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/a2ui"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/skill"
)

var _ = schema.UserMessage // 保持与其它 server 文件一致的 schema 依赖（无实际用途可删）

// listEnabledSkillOptions 取所有 Enabled 的 Skill，解析出名称/描述组成候选。
// 解析失败的记录跳过（用其原始 Name/Description 兜底）。
func (cfg Config) listEnabledSkillOptions(ctx context.Context) []skill.SkillOption {
	records, err := cfg.Skills.ListEnabled(ctx)
	if err != nil {
		log.Printf("skill router: list enabled skills: %v", err)
		return nil
	}
	options := make([]skill.SkillOption, 0, len(records))
	for _, r := range records {
		name, desc := r.Name, r.Description
		if plan, err := skill.ParseSkill(r.Content); err == nil {
			if plan.Name != "" {
				name = plan.Name
			}
			if plan.Description != "" {
				desc = plan.Description
			}
		}
		options = append(options, skill.SkillOption{ID: r.ID, Name: name, Description: desc})
	}
	return options
}

// resolveSkillSelection 拥有 0/1 候选与出错的全部兜底策略。调用前应保证 len(options) > 0。
func (cfg Config) resolveSkillSelection(ctx context.Context, brief string, options []skill.SkillOption) skill.SkillSelection {
	if len(options) == 1 {
		return skill.SkillSelection{SkillID: options[0].ID, Reason: "库中唯一 Skill"}
	}
	sel, err := cfg.SkillSelector.Select(ctx, brief, options)
	if err != nil {
		log.Printf("skill router: selector error, fallback to default: %v", err)
		fallbackID := cfg.DefaultSkillID
		if fallbackID == "" {
			fallbackID = options[0].ID
		}
		return skill.SkillSelection{SkillID: fallbackID, Reason: "未能匹配，回落默认", Fallback: true}
	}
	return sel
}

// emitSkillSelected 通过 a2ui broker 广播 skill.selected（Publisher 为 nil 时静默跳过）。
func (cfg Config) emitSkillSelected(ctx context.Context, sessionID, runID string, sel skill.SkillSelection, options []skill.SkillOption) {
	if cfg.Publisher == nil {
		return
	}
	name := sel.SkillID
	for _, o := range options {
		if o.ID == sel.SkillID {
			name = o.Name
			break
		}
	}
	event := a2ui.SSEEvent{
		ID:        cfg.NewID(),
		SessionID: sessionID,
		RunID:     runID,
		Event:     a2ui.EventSkillSelected,
		Payload: a2ui.SkillSelectedPayload{
			SkillID:   sel.SkillID,
			SkillName: name,
			Reason:    sel.Reason,
			Fallback:  sel.Fallback,
		},
		CreatedAt: cfg.Now(),
	}
	if err := cfg.Publisher.Publish(ctx, event); err != nil {
		log.Printf("skill router: publish skill.selected: %v", err)
	}
}

// maybeRouteSkill 在会话未绑 Skill 且 Router 已配置时选中并绑定一个 Skill，返回更新后的 record。
func (cfg Config) maybeRouteSkill(ctx context.Context, sessionRecord interface {
	// 占位，见下方实际调用（用具体 session.SessionRecord，不用接口）
}) {
}
```

> `maybeRouteSkill` 占位块删除——接线直接内联在 `streamMessage`（下一步），避免为 `session.SessionRecord` 造多余抽象（YAGNI）。保留 `listEnabledSkillOptions`/`resolveSkillSelection`/`emitSkillSelected` 三个方法即可。实现时删掉 `maybeRouteSkill` 与顶部无用的 `schema` 占位行。

- [ ] **Step 3c: 在 streamMessage 接线**

在 `internal/aigc/server/router.go` 的 `streamMessage` 里，`invokeReq := AgentInvokeRequest{...}`（router.go:1026）**之前**插入：

```go
	if sessionRecord.SkillID == "" && cfg.Skills != nil && cfg.SkillSelector != nil {
		options := cfg.listEnabledSkillOptions(c.Request.Context())
		if len(options) > 0 {
			sel := cfg.resolveSkillSelection(c.Request.Context(), content, options)
			sessionRecord.SkillID = sel.SkillID
			sessionRecord.UpdatedAt = cfg.Now()
			if err := cfg.Store.SaveSession(c.Request.Context(), sessionRecord); err != nil {
				log.Printf("skill router: save session binding: %v", err) // 尽力而为，不中断本轮
			}
			cfg.emitSkillSelected(c.Request.Context(), sessionID, runID, sel, options)
		}
	}
```

（若 router.go 未导入 `"log"`，在实现时补上导入。）

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/aigc/server -run TestRouter -v`
Expected: PASS（5 个用例）。再跑 `go build ./...` 确认全仓库编译通过。

- [ ] **Step 5: 提交**

```bash
git add internal/aigc/server/router.go internal/aigc/server/skill_router.go internal/aigc/server/skill_router_test.go
git commit -m "feat(server): auto-route skill on unbound session in streamMessage

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 4: main.go 装配真实 Router 依赖

**Files:**
- Modify: `cmd/aigc-agent/main.go`（`NewRouter(aigcserver.Config{...})` 处，main.go:167 附近）

- [ ] **Step 1: 加装配代码**

在构造 `aigcserver.Config{...}` 前，已有 DeepSeek 模型可复用。若装配根尚无独立 chat model 变量，用 `agent.NewDeepSeekChatModel(ctx, cfg)` 造一个给 selector（它返回的 `ToolCallingChatModel` 满足 `chatModel`）。在 `Config{...}` 字面量里补三字段：

```go
	// selectorModel 复用 DeepSeek，仅用于 Skill 分类（无工具）。
	selectorModel, err := agent.NewDeepSeekChatModel(ctx, cfg)
	if err != nil {
		log.Fatalf("build skill selector model: %v", err)
	}
```

然后在 `aigcserver.Config{ ... }` 中加：

```go
		SkillSelector:  aigcskill.NewLLMSkillSelector(selectorModel),
		DefaultSkillID: "", // 未配则 resolveSkillSelection 回落到候选第一个
		Publisher:      eventBroker, // 与 Events 同一 broker，前端经 /events/stream 收到
```

（确认 main.go 已导入 `agent`（`internal/aigc/agent`）与 `aigcskill`（`internal/aigc/skill`）；如缺则补。实际包别名以文件现有 import 为准。）

- [ ] **Step 2: 编译验证**

Run: `go build ./...`
Expected: 无错误。

- [ ] **Step 3: 提交**

```bash
git add cmd/aigc-agent/main.go
git commit -m "chore(main): wire llmSkillSelector and skill.selected publisher

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Slice ② · 文档展示（后端）

### Task 5: GET /spec 与 GET /skill 两个只读端点

**Files:**
- Create: `internal/aigc/server/documents.go`
- Modify: `internal/aigc/server/router.go`（`Config` 加 `Specs`；注册两条路由）
- Test: `internal/aigc/server/documents_test.go`

- [ ] **Step 1: 写失败测试**

`internal/aigc/server/documents_test.go`:

```go
package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	aigcsession "github.com/FigoGoo/Dora-Agent/internal/aigc/session"
	aigcskill "github.com/FigoGoo/Dora-Agent/internal/aigc/skill"
	aigcspec "github.com/FigoGoo/Dora-Agent/internal/aigc/spec"
)

type fakeSpecReader struct {
	spec aigcspec.FinalVideoSpec
	err  error
}

func (f *fakeSpecReader) GetLatestBySession(_ context.Context, _ string) (aigcspec.FinalVideoSpec, error) {
	return f.spec, f.err
}

func docRouter(store *fakeSessionStore, skills *fakeSkillStore, specs FinalVideoSpecReader) http.Handler {
	return NewRouter(Config{
		Store:   store,
		Skills:  skills,
		Specs:   specs,
		Invoker: &fakeAgentInvoker{},
		NewID:   func() string { return "id" },
		Now:     func() time.Time { return time.Unix(0, 0).UTC() },
	})
}

func TestGetSessionSpecReturnsMarkdown(t *testing.T) {
	store := newFakeSessionStore()
	store.sessions["s1"] = aigcsession.SessionRecord{ID: "s1"}
	specs := &fakeSpecReader{spec: aigcspec.FinalVideoSpec{SessionID: "s1", Markdown: "# Final Video Spec\n内容"}}
	router := docRouter(store, &fakeSkillStore{records: map[string]aigcskill.SkillRecord{}}, specs)

	req := httptest.NewRequest(http.MethodGet, "/api/aigc/sessions/s1/spec", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var got aigcspec.FinalVideoSpec
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Markdown == "" {
		t.Fatalf("Markdown 应非空")
	}
}

func TestGetSessionSpecNotFound(t *testing.T) {
	store := newFakeSessionStore()
	store.sessions["s1"] = aigcsession.SessionRecord{ID: "s1"}
	specs := &fakeSpecReader{err: aigcspec.ErrNotFound}
	router := docRouter(store, &fakeSkillStore{records: map[string]aigcskill.SkillRecord{}}, specs)

	req := httptest.NewRequest(http.MethodGet, "/api/aigc/sessions/s1/spec", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, 期望 404", rec.Code)
	}
}

func TestGetSessionSkillBound(t *testing.T) {
	store := newFakeSessionStore()
	store.sessions["s1"] = aigcsession.SessionRecord{ID: "s1", SkillID: "sk_travel"}
	skills := &fakeSkillStore{records: map[string]aigcskill.SkillRecord{
		"sk_travel": {ID: "sk_travel", Name: "人文纪录短片", Content: "<name>人文纪录短片</name>"},
	}}
	router := docRouter(store, skills, &fakeSpecReader{err: aigcspec.ErrNotFound})

	req := httptest.NewRequest(http.MethodGet, "/api/aigc/sessions/s1/skill", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var got struct {
		Bound   bool   `json:"bound"`
		ID      string `json:"id"`
		Name    string `json:"name"`
		Content string `json:"content"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if !got.Bound || got.ID != "sk_travel" || got.Content == "" {
		t.Fatalf("got = %+v", got)
	}
}

func TestGetSessionSkillUnbound(t *testing.T) {
	store := newFakeSessionStore()
	store.sessions["s1"] = aigcsession.SessionRecord{ID: "s1"} // SkillID == ""
	router := docRouter(store, &fakeSkillStore{records: map[string]aigcskill.SkillRecord{}}, &fakeSpecReader{err: aigcspec.ErrNotFound})

	req := httptest.NewRequest(http.MethodGet, "/api/aigc/sessions/s1/skill", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var got struct {
		Bound bool `json:"bound"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Bound {
		t.Fatalf("未绑应 bound=false")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/aigc/server -run TestGetSession -v`
Expected: 编译失败 —— `Config` 无 `Specs` 字段、`FinalVideoSpecReader` 未定义、路由未注册。

- [ ] **Step 3a: Config 加 Specs + 定义读接口**

在 `internal/aigc/server/documents.go` 顶部定义窄读接口，并在 `Config`（router.go:102）加字段：

router.go 内 `Config` 追加：

```go
	Specs FinalVideoSpecReader
```

`internal/aigc/server/documents.go`:

```go
package server

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/skill"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/spec"
)

// FinalVideoSpecReader 是文档展示所需的最小只读能力（spec.PostgresStore 满足）。
type FinalVideoSpecReader interface {
	GetLatestBySession(ctx context.Context, sessionID string) (spec.FinalVideoSpec, error)
}

// getSessionSpec 返回本会话最新 Final Video Spec（含 Markdown 原文）。
func (cfg Config) getSessionSpec(c *gin.Context) {
	if cfg.Specs == nil {
		writeJSONError(c, http.StatusInternalServerError, "spec store is not configured")
		return
	}
	sessionID := strings.TrimSpace(c.Param("session_id"))
	if sessionID == "" {
		writeJSONError(c, http.StatusBadRequest, "session id is required")
		return
	}
	if !cfg.ensureSession(c, sessionID) {
		return
	}
	got, err := cfg.Specs.GetLatestBySession(c.Request.Context(), sessionID)
	if err != nil {
		if errors.Is(err, spec.ErrNotFound) {
			writeJSONError(c, http.StatusNotFound, "spec not found")
			return
		}
		writeJSONError(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, got)
}

// getSessionSkill 返回本会话当前绑定的 Skill 原文（skill.md）；未绑返回 {bound:false}。
func (cfg Config) getSessionSkill(c *gin.Context) {
	if cfg.Store == nil {
		writeJSONError(c, http.StatusInternalServerError, "session store is not configured")
		return
	}
	sessionID := strings.TrimSpace(c.Param("session_id"))
	if sessionID == "" {
		writeJSONError(c, http.StatusBadRequest, "session id is required")
		return
	}
	record, err := cfg.Store.GetSession(c.Request.Context(), sessionID)
	if err != nil {
		writeJSONError(c, http.StatusNotFound, "session not found")
		return
	}
	if record.SkillID == "" || cfg.Skills == nil {
		c.JSON(http.StatusOK, gin.H{"bound": false})
		return
	}
	skillRecord, err := cfg.Skills.Get(c.Request.Context(), record.SkillID)
	if err != nil {
		if errors.Is(err, skill.ErrSkillNotFound) {
			c.JSON(http.StatusOK, gin.H{"bound": false})
			return
		}
		writeJSONError(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"bound":   true,
		"id":      skillRecord.ID,
		"name":    skillRecord.Name,
		"content": skillRecord.Content,
	})
}
```

> `cfg.ensureSession`、`writeJSONError`、`skill.ErrSkillNotFound` 均已存在（见 router.go 的 `getSessionStoryboard`/`bindSkillToSession`）。

- [ ] **Step 3b: 注册路由**

在 `internal/aigc/server/router.go` 的 `NewRouter` 路由块（router.go:147 附近，`getSessionStoryboard` 注册旁）追加：

```go
	router.GET("/api/aigc/sessions/:session_id/spec", cfg.getSessionSpec)
	router.GET("/api/aigc/sessions/:session_id/skill", cfg.getSessionSkill)
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/aigc/server -run TestGetSession -v`
Expected: PASS（4 个用例）。再 `go build ./...`。

- [ ] **Step 5: 提交**

```bash
git add internal/aigc/server/documents.go internal/aigc/server/router.go internal/aigc/server/documents_test.go
git commit -m "feat(server): add read-only GET /spec and /skill document endpoints

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 6: main.go 注入 Specs

**Files:**
- Modify: `cmd/aigc-agent/main.go`（`aigcserver.Config{...}`）

- [ ] **Step 1: 注入 Specs**

装配根已构造了 spec store（text_editor / write_prompt 用的同一实例，`*aigcspec.PostgresStore`）。在 `aigcserver.Config{...}` 里加：

```go
		Specs: specStore, // 复用已构造的 *spec.PostgresStore（满足 FinalVideoSpecReader）
```

（变量名以 main.go 里 spec store 的真实变量为准；若尚未提升为独立变量，则在装配处引用其构造结果。）

- [ ] **Step 2: 编译验证**

Run: `go build ./...`
Expected: 无错误。

- [ ] **Step 3: 提交**

```bash
git add cmd/aigc-agent/main.go
git commit -m "chore(main): wire spec store into document endpoints

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## 前端

### Task 7: skill.selected 自动选择提示条

**Files:**
- Modify: `frontend/src/features/aigc/AigcWorkspacePage.jsx`

- [ ] **Step 1: 订阅事件名**

在 `EVENT_NAMES` 数组（约 line 25-34，`'job.status'` 之后、`'error'` 之前）加入：

```js
  'skill.selected',
```

- [ ] **Step 2: 处理事件并存状态**

在组件状态区（其它 `useState` 旁）加：

```js
  const [autoSkill, setAutoSkill] = useState(null); // {name, reason, fallback}
```

在 `handleA2UIEvent` 里，与 `protocolName === 'storyboard.patch'` / `'job.status'` 同一 `if` 链（约 line 216-224）追加分支：

```js
        if (protocolName === 'skill.selected') {
          const data = event?.payload || event?.data || {};
          setAutoSkill({
            name: data.skill_name || data.skill_id || '',
            reason: data.reason || '',
            fallback: Boolean(data.fallback)
          });
          return;
        }
```

> `protocolName` 与 `event.payload` 的取法沿用该函数内既有写法；若既有分支用的是 `event.data` 或已解构的字段，按同款访问方式对齐。

- [ ] **Step 3: 渲染提示条**

在对话区顶部（消息列表容器之上）渲染：

```jsx
      {autoSkill && (
        <div className="aigc-auto-skill-notice" role="status">
          🧭 已为你自动选择 Skill：<strong>{autoSkill.name}</strong>
          {autoSkill.reason ? `——${autoSkill.reason}` : ''}
          {autoSkill.fallback ? '（回落默认）' : ''}
        </div>
      )}
```

- [ ] **Step 4: 手动验证（真 App）**

Run 前端 `npm run dev`（`frontend/`），后端已起。新建会话、**不选 Skill**、输入"帮我做北京平谷文旅短片"→ 对话区顶部应出现"🧭 已为你自动选择 Skill：〈名称〉——〈理由〉"。手动点某 Skill 再发 → **不**出现提示条。
Expected: 自动路径出现提示；手动路径不出现。

- [ ] **Step 5: 提交**

```bash
git add frontend/src/features/aigc/AigcWorkspacePage.jsx
git commit -m "feat(frontend): show auto-selected skill notice on skill.selected

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 8: 「文档」视图切换（只读展示 spec.md / skill.md）

**Files:**
- Modify: `frontend/src/features/aigc/AigcWorkspacePage.jsx`

- [ ] **Step 1: 视图状态 + 拉取函数**

在状态区加：

```js
  const [leftView, setLeftView] = useState('storyboard'); // 'storyboard' | 'docs'
  const [docSpec, setDocSpec] = useState(null);   // FinalVideoSpec | null
  const [docSkill, setDocSkill] = useState(null);  // {bound, name, content} | null
  const [activeDoc, setActiveDoc] = useState('spec'); // 'spec' | 'skill'
```

加拉取函数（放在其它 fetch helper 旁）：

```js
  const loadDocuments = useCallback(async () => {
    if (!sessionID) return;
    try {
      const specRes = await fetch(`/api/aigc/sessions/${sessionID}/spec`);
      setDocSpec(specRes.ok ? await specRes.json() : null);
    } catch {
      setDocSpec(null);
    }
    try {
      const skillRes = await fetch(`/api/aigc/sessions/${sessionID}/skill`);
      setDocSkill(skillRes.ok ? await skillRes.json() : null);
    } catch {
      setDocSkill(null);
    }
  }, [sessionID]);
```

- [ ] **Step 2: 切到文档 tab 时拉取**

在切换按钮点击时触发 `loadDocuments()`（见下步），并在收到 `final_video_spec` 相关事件或 resume 后可重拉（可选：在既有刷新逻辑里追加 `void loadDocuments()`）。

- [ ] **Step 3: 面板切换 + 只读渲染**

在 `<section className="aigc-storyboard-pane" ...>`（line 513）内最上方加视图切换头，并按 `leftView` 条件渲染。故事板原内容包进 `leftView === 'storyboard'` 分支，新增 `leftView === 'docs'` 分支：

```jsx
        <div className="aigc-left-tabs" role="tablist">
          <button type="button" role="tab" aria-selected={leftView === 'storyboard'}
            onClick={() => setLeftView('storyboard')}>故事板</button>
          <button type="button" role="tab" aria-selected={leftView === 'docs'}
            onClick={() => { setLeftView('docs'); void loadDocuments(); }}>文档</button>
        </div>

        {leftView === 'docs' && (
          <div className="aigc-docs-pane">
            <div className="aigc-docs-tabs" role="tablist">
              <button type="button" role="tab" aria-selected={activeDoc === 'spec'}
                onClick={() => setActiveDoc('spec')}>Final_Video_Spec.md</button>
              <button type="button" role="tab" aria-selected={activeDoc === 'skill'}
                onClick={() => setActiveDoc('skill')}>skill.md</button>
            </div>
            <pre className="aigc-doc-content">
              {activeDoc === 'spec'
                ? (docSpec?.Markdown || docSpec?.markdown || '尚未生成 Final Video Spec。')
                : (docSkill?.bound ? (docSkill.content || '') : '当前会话未绑定 Skill。')}
            </pre>
          </div>
        )}
```

> 保持只读：用 `<pre>` 直出原文即可，无需引入 markdown 渲染库（YAGNI；本 demo 只读）。原故事板 JSX 用 `{leftView === 'storyboard' && ( ... )}` 包住。`FinalVideoSpec` 的字段在 Go 侧 JSON tag 为 `markdown`（小写），故优先取 `docSpec?.markdown`，`Markdown` 作兜底。

- [ ] **Step 4: 手动验证（真 App）**

Run 前端 dev + 后端。跑完一轮生成（spec 已由 text_editor 写入）后：点「文档」→ 看到两个子 tab；`Final_Video_Spec.md` 显示 spec 原文，`skill.md` 显示当前绑定 Skill 原文（Router 自动选中的即展示被选中那份）；未绑会话显示"当前会话未绑定 Skill"。
Expected: 两份文档只读可见、随绑定联动。

- [ ] **Step 5: 提交**

```bash
git add frontend/src/features/aigc/AigcWorkspacePage.jsx
git commit -m "feat(frontend): add read-only document view for spec.md and skill.md

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## 全量回归

- [ ] **Step 1: 后端全测 + 编译**

Run: `go build ./... && go test ./...`
Expected: 全绿（依赖 DB/Redis 的测试在本地无基础设施时 skip，非 fail）。

- [ ] **Step 2: 前端构建**

Run: `cd frontend && npm run build`
Expected: 构建成功。

---

## Self-Review（对照 spec 核查）

- **Slice ① SkillSelector（spec §2.1）** → Task 1（接口/类型/LLM 实现/JSON 校验/越界与坏 JSON 报错）。✅
- **兜底策略（spec §2.2：0 跳过 / 1 直选 / 出错默认）** → Task 3 `resolveSkillSelection` + 5 个集成测覆盖 0/1/已绑/自动/出错。✅
- **streamMessage 接线（spec §2.3）** → Task 3 Step 3c，插入点在 `AgentInvokeRequest` 构造前；`listEnabledSkillOptions` 用 `ListEnabled`+`ParseSkill`。✅
- **skill.selected 事件（spec §2.4）** → Task 2 常量/payload + Task 3 `emitSkillSelected` 经 broker 广播。✅
- **Config 注入（spec §2.5）** → Task 3（字段）+ Task 4（main.go 真实装配）。✅
- **前端提示（spec §2.6）** → Task 7。✅
- **文档端点（spec §2B.1：GET /spec、GET /skill，复用现成 store）** → Task 5 + Task 6。✅
- **文档 tab 只读（spec §2B.2）** → Task 8。✅
- **非目标**：未新增生成 handler；文档纯只读无编辑；未改 `bindSkillToSession`。✅
- **类型一致性**：`SkillOption{ID,Name,Description}`、`SkillSelection{SkillID,Reason,Fallback}`、`SkillSelectedPayload{SkillID,SkillName,Reason,Fallback}`、`FinalVideoSpecReader.GetLatestBySession` 跨任务一致。✅
- **实现提醒**：模块导入路径以 `go.mod` 首行为准替换示例中的 `github.com/FigoGoo/Dora-Agent`；`fake*` 字段名以 `router_test.go` 真实定义为准；Task 3 Step 3b 的 `maybeRouteSkill` 占位块须删除。
