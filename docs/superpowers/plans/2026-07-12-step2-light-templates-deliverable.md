# 第 2 步：轻模板×4 + generate_media 目标分派 + 前端轻交付物视图 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 落地终版 §6.1 第 2 步——快速出图/浅视频/音乐/音频四个轻模板（节点=能力+参数，目标=session_deliverable）；`generate_media` 前置校验按目标类型分派（storyboard 走旧四闸门，deliverable 免故事板）；前端渲染无 storyboard 的交付物。覆盖需求矩阵高频格子。

**Architecture:** `GenerateMediaIntent` 增加 `target` 分派字段（空=storyboard 默认，行为零变化）；`capabilityruntime` 新增 `generateDeliverableMedia` 路径（复用 replay 幂等/preflight/CreateWorkflow，DeliveryPolicy=active+auto_approve 走 Step 0 铺好的 deliverable 直通道）；`workflow_store` 对 deliverable token 补齐校验；job 完成后 `PublishFinalized` 在 Step 0 短路处发 `deliverables` surface 增量事件；轻模板作为 orchestration 数据 + system prompt 目录注入；前端新增 deliverables 状态分支与面板。

**Tech Stack:** Go 1.26 后端 + React 19/Vitest 前端。

**取证依据（2026-07-12 核实）：**
- `generateMedia` 四闸门与 job 构造：`capabilityruntime/runtime.go:789-899`（闸门 :800-817；BindingToken 硬编码 StoryboardID :872；Batch candidate+review_required+WakeOnFailure :884）。
- `GenerateMediaIntent{Phase,Policy}` + Validate：`capability/intents.go:69-72,117-125`；LLM params schema：`capability/tools.go:188-192`（phase Required）；description :19。
- `providerFor`：`runtime.go:1661-1672`——video→seedance、audio/**music**/voice→audio、image→image2，四方向 provider 全有落点。
- replay 幂等 `replayGenerateMedia`：`runtime.go:1098-1118`（按 operation Kind="generate_media"+IdempotencyKey，deliverable 可复用）；preflight `runGenerationPreflight`：`runtime.go:1297`。
- WakePolicy 词汇：`generation/models.go:38-39`（`WakeOnTerminal`/`WakeOnFailure`）。deliverable 无审批链推进，完成需回 Agent 解释 → WakeOnTerminal。
- **workflow_store.go:259-263 校验漏洞**：`BindingToken.Validate()` 被 `job.StoryboardID != ""` 门控——deliverable job（StoryboardID 空）会跳过 token 校验，须补严。
- UI 事件通道：outbox relay `generationruntime/outbox.go:96`（job.succeeded → `PublishFinalized`）；Step 0 在 `adapters.go publishChanges` 对 deliverable 短路 return nil——本步在该分支发事件。`publishJob`（:136）的工具卡进度事件已覆盖 chat surface，无需动。
- 资产列表端点已有：`GET /api/aigc/sessions/:session_id/assets`（router.go:210）。
- 前端（Explore 核实）：SSE 入口 `AigcWorkspacePage.jsx:396-433`；surface 路由 `updateA2UICard:2548-2602`（'storyboard'→setStoryboard、'tool_runs'→setToolRuns、其余→chat）；资产渲染单字段 `asset.url`（StoryboardPanel.jsx:679-684）；**deliverable 渲染不存在**（src/ 无该 token）；测试模式 `App.test.jsx` MockEventSource + `updateCardEvent(surface, cardID, payload)`(:2892)。
- system prompt：`agent/deepseek.go:93-96`（五能力深流程叙事，轻直出指引的注入点）。
- directive 白名单兼容：`agent/next_capability.go:216-221` 用 DisallowUnknownFields 解 GenerateMediaIntent——Intent 加新字段后，旧 directive JSON（只含 phase/policy）仍解入新结构，兼容。

**设计定案：**
- 目标词汇：`target` ∈ `""`(=storyboard 默认) | `session_deliverable`；`media_kind` ∈ image|video|music|audio（四模态方向，平台中性）；`count` 1..4 默认 1；`aspect_ratio` 可选透传。deliverable 模式忽略 phase/policy（不校验不使用）。
- deliverable job：TargetID=`deliverable:<newID>`、AssetSlot=`primary`、TargetKind=session_deliverable、无 StoryboardID；DeliveryPolicy=active+auto_approve+postpaid（轻直出免候选审，Step 0 已证合法）；InputFingerprint=sha256(prompt|kind|ratio|index)。
- deliverables UI 事件：增量（每 job succeeded 发该 job 的资产视图），CardID 固定 `deliverables`，前端按 asset.id merge；断线兜底靠既有全量 `refreshSessionData`。deliverable 资产在 commit 时打 `metadata.target_kind=session_deliverable` 供初始化过滤。
- 轻模板：orchestration 数据四条（image/video/music/audio_creation），Node=generate_media deliverable 参数骨架（prompt 为槽）；v1 消费方=system prompt 目录注入（M2 v1 形态"目录整体注入上下文"）。

---

### Task 1: GenerateMediaIntent 目标分派扩展（capability 包）

**Files:**
- Modify: `internal/aigc/capability/intents.go`（:69 Intent 结构、:117 Validate）
- Modify: `internal/aigc/capability/tools.go`（:19 description、:188 params）
- Test: `internal/aigc/capability/intents_deliverable_test.go`（新建）

- [x] **Step 1: 写失败测试**

```go
package capability

import "testing"

func TestGenerateMediaIntentTargetDispatch(t *testing.T) {
	legacy := GenerateMediaIntent{Phase: "auto_next", Policy: "all_eligible"}
	if err := legacy.Validate(); err != nil {
		t.Fatalf("legacy storyboard intent must stay valid: %v", err)
	}
	if legacy.NormalizedTarget() != MediaTargetStoryboard {
		t.Fatalf("empty target must normalize to storyboard, got %q", legacy.NormalizedTarget())
	}

	deliverable := GenerateMediaIntent{Target: MediaTargetSessionDeliverable, MediaKind: "image", Prompt: "一只在雨里撑伞的柴犬"}
	if err := deliverable.Validate(); err != nil {
		t.Fatalf("deliverable intent must not require phase/policy: %v", err)
	}
	if deliverable.NormalizedCount() != 1 {
		t.Fatalf("count defaults to 1, got %d", deliverable.NormalizedCount())
	}

	for _, kind := range []string{"image", "video", "music", "audio"} {
		intent := GenerateMediaIntent{Target: MediaTargetSessionDeliverable, MediaKind: kind, Prompt: "p"}
		if err := intent.Validate(); err != nil {
			t.Fatalf("media_kind %s must be valid: %v", kind, err)
		}
	}

	badKind := GenerateMediaIntent{Target: MediaTargetSessionDeliverable, MediaKind: "hologram", Prompt: "p"}
	if err := badKind.Validate(); err == nil {
		t.Fatal("unknown media_kind must be rejected")
	}
	noPrompt := GenerateMediaIntent{Target: MediaTargetSessionDeliverable, MediaKind: "image"}
	if err := noPrompt.Validate(); err == nil {
		t.Fatal("deliverable intent requires prompt")
	}
	tooMany := GenerateMediaIntent{Target: MediaTargetSessionDeliverable, MediaKind: "image", Prompt: "p", Count: 5}
	if err := tooMany.Validate(); err == nil {
		t.Fatal("count above 4 must be rejected")
	}
	unknownTarget := GenerateMediaIntent{Target: "weird", MediaKind: "image", Prompt: "p"}
	if err := unknownTarget.Validate(); err == nil {
		t.Fatal("unknown target must be rejected")
	}
}
```

- [x] **Step 2: 跑测试确认失败**

Run: `go test ./internal/aigc/capability -run TestGenerateMediaIntentTargetDispatch -v`
Expected: FAIL（未定义 MediaTargetStoryboard 等）

- [x] **Step 3: 实现——intents.go**

Intent 结构替换为：

```go
const (
	MediaTargetStoryboard         = "storyboard"
	MediaTargetSessionDeliverable = "session_deliverable"
)

type GenerateMediaIntent struct {
	Phase  string `json:"phase,omitempty"`
	Policy string `json:"policy,omitempty"`
	// 轻直出（session_deliverable 目标）字段；target 为空 = storyboard，
	// 行为与旧版完全一致。
	Target      string `json:"target,omitempty"`
	MediaKind   string `json:"media_kind,omitempty"`
	Prompt      string `json:"prompt,omitempty"`
	Count       int    `json:"count,omitempty"`
	AspectRatio string `json:"aspect_ratio,omitempty"`
}

func (intent GenerateMediaIntent) NormalizedTarget() string {
	target := strings.TrimSpace(intent.Target)
	if target == "" {
		return MediaTargetStoryboard
	}
	return target
}

func (intent GenerateMediaIntent) NormalizedCount() int {
	if intent.Count == 0 {
		return 1
	}
	return intent.Count
}
```

Validate 替换为：

```go
func (intent GenerateMediaIntent) Validate() error {
	switch intent.NormalizedTarget() {
	case MediaTargetStoryboard:
		if !slices.Contains([]string{"auto_next", "element_images", "keyframes", "videos", "audio"}, strings.TrimSpace(intent.Phase)) {
			return fmt.Errorf("generate_media phase is invalid")
		}
		if !slices.Contains([]string{"single_next", "all_eligible"}, strings.TrimSpace(intent.Policy)) {
			return fmt.Errorf("generate_media policy is invalid")
		}
		return nil
	case MediaTargetSessionDeliverable:
		if !slices.Contains([]string{"image", "video", "music", "audio"}, strings.TrimSpace(intent.MediaKind)) {
			return fmt.Errorf("generate_media media_kind must be image|video|music|audio for session_deliverable")
		}
		if strings.TrimSpace(intent.Prompt) == "" {
			return fmt.Errorf("generate_media prompt is required for session_deliverable")
		}
		if count := intent.NormalizedCount(); count < 1 || count > 4 {
			return fmt.Errorf("generate_media count must be between 1 and 4")
		}
		return nil
	default:
		return fmt.Errorf("generate_media target %q is not supported", intent.Target)
	}
}
```

- [x] **Step 4: 实现——tools.go（description + params；phase 的 Required 取消，desc 说明分派）**

```go
	generateMediaDescription = "Generate media. Two targets: (1) storyboard (default) — advance normal production for the active storyboard, deterministically selecting the next eligible stage; requires phase+policy. (2) session_deliverable — lightweight direct generation without a storyboard for quick single-intent requests (one image / short video / music / narration); requires media_kind+prompt. Do not use for targeted UI regeneration."
```

```go
func generateMediaParams() map[string]*schema.ParameterInfo {
	return map[string]*schema.ParameterInfo{
		"phase":        {Type: schema.String, Desc: "Storyboard target only: production phase selected at a high level; the graph selects concrete targets.", Enum: []string{"auto_next", "element_images", "keyframes", "videos", "audio"}},
		"policy":       {Type: schema.String, Desc: "Storyboard target only: how many eligible targets to dispatch.", Enum: []string{"single_next", "all_eligible"}},
		"target":       {Type: schema.String, Desc: "Generation target. Omit for storyboard production; use session_deliverable for lightweight direct generation without a storyboard.", Enum: []string{"storyboard", "session_deliverable"}},
		"media_kind":   {Type: schema.String, Desc: "session_deliverable only: modality direction.", Enum: []string{"image", "video", "music", "audio"}},
		"prompt":       {Type: schema.String, Desc: "session_deliverable only: complete generation prompt."},
		"count":        {Type: schema.Integer, Desc: "session_deliverable only: number of variants (1-4, default 1)."},
		"aspect_ratio": {Type: schema.String, Desc: "session_deliverable only: optional aspect ratio, e.g. 16:9."},
	}
}
```

（保留原 policy 参数行若与上不同则以上为准；phase/policy 均不再 Required——storyboard 模式的必填由 Validate 兜底，行为不变。）

- [x] **Step 5: 跑测试 + 包回归 + 白名单兼容确认**

Run: `go test ./internal/aigc/capability ./internal/aigc/agent`
Expected: 全部 PASS（agent 包含 directive 白名单测试，旧 directive JSON 解新结构须兼容）

- [x] **Step 6: Commit**

```bash
git add internal/aigc/capability/
git commit -m "feat(capability): generate_media 目标分派——session_deliverable 轻直出入参（storyboard 默认零变化）"
```

---

### Task 2: capabilityruntime 轻直出路径 generateDeliverableMedia

**Files:**
- Modify: `internal/aigc/capabilityruntime/runtime.go`（`generateMedia:789` 顶部分派 + 新函数）
- Test: `internal/aigc/capabilityruntime/deliverable_media_test.go`（新建；fake 构造沿用本包既有 runtime 测试的形状——先读既有 generateMedia 测试找 fake store/NewID 构造，复用同一套）

- [x] **Step 1: 读既有 generateMedia 测试的 Runtime 构造（`grep -n "generateMedia\|NewRuntime" internal/aigc/capabilityruntime/*_test.go | head`），复用其 fake；写失败测试**

```go
// 意图与断言（构造按既有 fake 落实）：
func TestGenerateMediaSessionDeliverableSkipsStoryboardGates(t *testing.T) {
	// Runtime 构造：Storyboards/Specs 两个 store 显式传 nil 或未 seed 的 fake——
	// deliverable 路径触碰任何 storyboard/spec 闸门即报错，这是免闸门的物理证明。
	// GenerationCommands/GenerationJobs/Billing 用既有 fake。
	// request: GenerateMediaIntent{Target: session_deliverable, MediaKind: "image", Prompt: "雨中柴犬", Count: 2, AspectRatio: "1:1"}
	// 断言：
	//   result.Status == capability.StatusAccepted；JobCount == 2
	//   每个 job：Provider == generation.ProviderImage2；MediaKind == "image"
	//     BindingToken.TargetKind == generation.TargetKindSessionDeliverable
	//     BindingToken.StoryboardID == "" 且 TargetID 前缀 "deliverable:"，AssetSlot == "primary"
	//     DeliveryPolicy == {active, auto_approve, postpaid_no_reservation}
	//     Payload["prompt"] == "雨中柴犬"，Payload["ratio"] == "1:1"
	//   Batch.WakePolicy == generation.WakeOnTerminal；Batch.DeliveryPolicy 同 job
	//   两个 job 的 InputFingerprint 不同（index 参与）
	// 再以同 IdempotencyKey 重调：replay 命中，OperationID/BatchID 与首次一致（幂等）。
}

func TestGenerateMediaDeliverableRejectsUnknownProviderKind(t *testing.T) {
	// media_kind 合法但 provider 未注册的场景由 providerFor 空串暴露：
	// 构造上 Validate 已挡未知 kind，此测试锁 providerFor 对四方向全部非空。
	for _, kind := range []string{"image", "video", "music", "audio"} {
		if providerFor(kind) == "" {
			t.Fatalf("providerFor(%s) must resolve", kind)
		}
	}
}
```

- [x] **Step 2: 跑测试确认失败**

Run: `go test ./internal/aigc/capabilityruntime -run TestGenerateMediaSessionDeliverable -v`
Expected: FAIL（旧路径撞 storyboard 闸门报错）

- [x] **Step 3: 实现——generateMedia 顶部分派 + 新函数**

`generateMedia` 在两个 replay 检查**之后**、`GetAggregateBySession` 之前插入分派（replay 对两种目标通用）：

```go
	if request.Intent.NormalizedTarget() == capability.MediaTargetSessionDeliverable {
		return r.generateDeliverableMedia(ctx, request)
	}
```

新函数（放在 generateMedia 之后）：

```go
// generateDeliverableMedia 是轻直出路径：无 storyboard 闸门，目标为
// session 级交付物（终版 §5 两态之二、§6.1 第 2 步）。审批链不介入
// （active+auto_approve），完成后由 batch 终结唤醒 Agent 解释结果。
func (r *Runtime) generateDeliverableMedia(ctx context.Context, request capability.Request[capability.GenerateMediaIntent]) (capability.CapabilityResult[capability.GenerateMediaData], error) {
	kind := strings.TrimSpace(request.Intent.MediaKind)
	provider := providerFor(kind)
	if provider == "" {
		return capability.CapabilityResult[capability.GenerateMediaData]{}, fmt.Errorf("no provider registered for media_kind %s", kind)
	}
	prompt := strings.TrimSpace(request.Intent.Prompt)
	ratio := strings.TrimSpace(request.Intent.AspectRatio)
	policy := generation.DeliveryPolicy{BindingMode: generation.BindingModeActive, ApprovalPolicy: generation.ApprovalAutoApprove, ChargePolicy: generation.ChargePostpaidNoReservation}
	count := request.Intent.NormalizedCount()
	operationID, batchID := r.cfg.NewID(), r.cfg.NewID()
	jobs := make([]generation.GenerationJob, 0, count)
	selected := make([]string, 0, count)
	for index := 0; index < count; index++ {
		targetID := "deliverable:" + r.cfg.NewID()
		digest := sha256.Sum256([]byte(strings.Join([]string{prompt, kind, ratio, strconv.Itoa(index)}, "\x00")))
		payload := map[string]any{"prompt": prompt, "media_kind": kind, "user_id": request.Command.UserID}
		if ratio != "" {
			payload["ratio"] = ratio
		}
		jobs = append(jobs, generation.GenerationJob{
			ID: r.cfg.NewID(), SessionID: request.Command.SessionID, UserID: request.Command.UserID,
			ToolCallID:     request.Command.ToolCallID,
			IdempotencyKey: request.Command.IdempotencyKey + ":" + targetID,
			Provider:       provider, MediaKind: kind,
			TargetType: "session_deliverable", TargetID: targetID, AssetSlot: "primary",
			Required: true,
			BindingToken: generation.BindingToken{
				TargetKind: generation.TargetKindSessionDeliverable,
				TargetID:   targetID, AssetSlot: "primary",
				TargetRevision: 1, InputFingerprint: hex.EncodeToString(digest[:]),
			},
			DeliveryPolicy: policy, MaxAttempts: 4,
			Payload: payload,
		})
		selected = append(selected, targetID+":primary")
	}
	estimatedPoints, err := r.runGenerationPreflight(ctx, request.Command.UserID, jobs)
	if err != nil {
		return capability.CapabilityResult[capability.GenerateMediaData]{}, err
	}
	workflow, _, err := r.cfg.GenerationCommands.Create(ctx, generation.CreateWorkflowCommand{
		Operation: generation.GenerationOperation{ID: operationID, SessionID: request.Command.SessionID, UserID: request.Command.UserID, WorkflowRunID: valueOr(request.Command.WorkflowID, request.Command.RunID), StageRunID: request.Command.StageRunID, ToolCallID: request.Command.ToolCallID, IdempotencyKey: request.Command.IdempotencyKey, Kind: "generate_media", Status: generation.OperationStatusAccepted, BatchID: batchID, Result: map[string]any{"estimated_points": estimatedPoints}},
		Batch:     generation.GenerationBatch{ID: batchID, SessionID: request.Command.SessionID, UserID: request.Command.UserID, WorkflowRunID: valueOr(request.Command.WorkflowID, request.Command.RunID), StageRunID: request.Command.StageRunID, ToolCallID: request.Command.ToolCallID, OperationID: operationID, Kind: "session_deliverable", CompletionPolicy: generation.CompletionAllowPartial, WakePolicy: generation.WakeOnTerminal, DeliveryPolicy: policy},
		Jobs:      jobs,
	})
	if err != nil {
		return capability.CapabilityResult[capability.GenerateMediaData]{}, err
	}
	result := capability.CapabilityResult[capability.GenerateMediaData]{
		Status: capability.StatusAccepted, OperationID: workflow.Operation.ID, BatchID: workflow.Batch.ID,
		Data: capability.GenerateMediaData{SelectedTargets: selected, JobCount: len(workflow.Jobs)},
	}
	if estimatedPoints > 0 {
		result.Cost = &capability.CostSummary{Currency: "points", EstimatedMinor: estimatedPoints}
	}
	return result, nil
}
```

（import 补 `crypto/sha256`、`encoding/hex`、`strconv`，若已有则跳过。）

- [x] **Step 4: 跑测试确认通过 + 包回归**

Run: `go test ./internal/aigc/capabilityruntime`
Expected: 全部 PASS

- [x] **Step 5: Commit**

```bash
git add internal/aigc/capabilityruntime/
git commit -m "feat(capabilityruntime): generate_media 轻直出路径——session_deliverable 免故事板闸门直派发"
```

---

### Task 3: workflow_store 对 deliverable token 补齐校验

**Files:**
- Modify: `internal/aigc/generation/workflow_store.go:259-263`
- Test: `internal/aigc/generation/workflow_deliverable_test.go`（新建）

- [x] **Step 1: 写失败测试**

```go
package generation

import (
	"context"
	"strings"
	"testing"
)

func TestCreateWorkflowValidatesDeliverableToken(t *testing.T) {
	store := NewMemoryStore()
	command := testWorkflowCommand("op-d1", "batch-d1", []GenerationJob{{
		ID: "job-d1", IdempotencyKey: "job-key-d1", Provider: "mock", Required: true,
		BindingToken: BindingToken{TargetKind: TargetKindSessionDeliverable, TargetID: "deliverable:x"},
		// AssetSlot 缺失 + InputFingerprint 缺失（AssetSlot 会被 normalize 回填吗？
		// 不会——job.AssetSlot 也为空。Validate 必须拒绝。）
	}})
	if _, _, err := store.CreateWorkflow(context.Background(), command); err == nil || !strings.Contains(err.Error(), "asset slot") {
		t.Fatalf("deliverable token without asset slot must be rejected at creation, got %v", err)
	}

	valid := testWorkflowCommand("op-d2", "batch-d2", []GenerationJob{{
		ID: "job-d2", IdempotencyKey: "job-key-d2", Provider: "mock", Required: true,
		BindingToken: BindingToken{TargetKind: TargetKindSessionDeliverable, TargetID: "deliverable:y", AssetSlot: "primary", InputFingerprint: "fp"},
	}})
	if _, _, err := store.CreateWorkflow(context.Background(), valid); err != nil {
		t.Fatalf("valid deliverable job must be accepted: %v", err)
	}
}
```

（错误信息断言以 `BindingToken.Validate` 实际文本为准，执行时先看 models.go 的报错串再定 Contains 关键词。）

- [x] **Step 2: 跑测试确认失败（第一段：目前被 StoryboardID 门控跳过校验，err==nil）**

Run: `go test ./internal/aigc/generation -run TestCreateWorkflowValidatesDeliverableToken -v`

- [x] **Step 3: 实现——把 :259 的门控从"有 StoryboardID 才校验"改为"storyboard 槽位或 deliverable 都校验"**

```go
		if strings.TrimSpace(job.StoryboardID) != "" || job.BindingToken.NormalizedKind() == TargetKindSessionDeliverable {
			if err := job.BindingToken.Validate(); err != nil {
				return WorkflowAggregate{}, false, fmt.Errorf("job %s: %w", job.ID, err)
			}
		}
```

（不改成无条件校验：既有测试里存在 StoryboardID 与 token 全空的最小 job 构造——如 `testWorkflowCommand` 的 job——无条件校验会把它们全打红，超出本步意图。）

- [x] **Step 4: 跑测试 + 包回归**

Run: `go test ./internal/aigc/generation`
Expected: 全部 PASS

- [x] **Step 5: Commit**

```bash
git add internal/aigc/generation/
git commit -m "fix(generation): CreateWorkflow 对 session_deliverable token 补齐校验（原被 StoryboardID 门控跳过）"
```

---

### Task 4: deliverable 资产打标 + PublishFinalized 发 deliverables surface 事件

**Files:**
- Modify: `internal/aigc/generationruntime/adapters.go`（`commit` 资产循环打标；`publishChanges` deliverable 分支发事件）
- Test: `internal/aigc/generationruntime/adapters_deliverable_test.go`（扩展既有两个测试）

- [x] **Step 1: 扩展测试（失败先行）**

在 `TestCommitSessionDeliverableOnlyPublishesAssets` 中，asset available 断言后追加：

```go
	if stored.Metadata["target_kind"] != generation.TargetKindSessionDeliverable {
		t.Fatalf("deliverable asset must be tagged, metadata=%v", stored.Metadata)
	}
```

新增事件测试（fake publisher 记录 envelope）：

```go
type recordingEventPublisher struct{ events []a2ui.SSEEvent }

func (p *recordingEventPublisher) Publish(_ context.Context, event a2ui.SSEEvent) error {
	p.events = append(p.events, event)
	return nil
}

func TestPublishFinalizedEmitsDeliverablesSurface(t *testing.T) {
	assets := &memoryAssets{values: map[string]asset.Asset{
		"asset-1": {ID: "asset-1", SessionID: "sess-1", Availability: asset.AvailabilityAvailable, URL: "https://cdn/x.png"},
	}}
	publisher := &recordingEventPublisher{}
	adapter := StoryboardBindingAdapter{Assets: assets, Events: publisher}
	job := generation.GenerationJob{
		ID: "job-1", SessionID: "sess-1", ResultAssetIDs: []string{"asset-1"},
		BindingToken: generation.BindingToken{TargetKind: generation.TargetKindSessionDeliverable, TargetID: "deliverable:img-1", AssetSlot: "primary", InputFingerprint: "fp"},
	}
	if err := adapter.PublishFinalized(context.Background(), job); err != nil {
		t.Fatalf("PublishFinalized: %v", err)
	}
	if len(publisher.events) != 1 {
		t.Fatalf("expected one deliverables event, got %d", len(publisher.events))
	}
	envelope, ok := publisher.events[0].Payload.(a2ui.ActionEnvelope)
	if !ok || len(envelope.Actions) != 1 {
		t.Fatalf("payload = %#v", publisher.events[0].Payload)
	}
	action := envelope.Actions[0]
	if action.Type != a2ui.ActionUpdateCard || action.Surface != "deliverables" || action.Target == nil || action.Target.CardID != "deliverables" {
		t.Fatalf("action = %#v", action)
	}
}
```

（asset.Asset 的 URL 字段名以 `internal/aigc/asset` 实际模型为准，执行时核实；Events 为 nil 时必须仍然静默成功——既有端到端测试用 failingEventPublisher 且断言 err==nil，改动后该测试会**变红**，需要把既有两处 `Events: failingEventPublisher{}` 的 deliverable 测试改为 `Events: nil` 或断言添加事件语义——以"deliverable 发布失败不得让 PublishFinalized 报错回滚 outbox"为准绳：**发布失败要传播吗？** storyboard 路径 publishChanges 对 Events.Publish 错误是传播的（outbox 重试），deliverable 保持同语义：传播。因此既有 failingEventPublisher 端到端测试的 PublishFinalized 断言改为允许错误或换 publisher——执行时按此语义修正该断言，并在 commit message 说明。）

- [x] **Step 2: 跑测试确认失败**

Run: `go test ./internal/aigc/generationruntime -run "TestCommitSessionDeliverable|TestPublishFinalizedEmitsDeliverables" -v`

- [x] **Step 3: 实现**

`commit` 的资产循环（deliverable 时打标）：

```go
	for _, assetID := range input.AssetIDs {
		stored, err := a.Assets.Get(ctx, assetID)
		if err != nil {
			return err
		}
		stored.Availability = asset.AvailabilityAvailable
		if isDeliverable {
			if stored.Metadata == nil {
				stored.Metadata = map[string]any{}
			}
			stored.Metadata["target_kind"] = generation.TargetKindSessionDeliverable
			stored.Metadata["deliverable_target_id"] = input.BindingToken.TargetID
		}
		if _, err := a.Assets.Save(ctx, stored); err != nil {
			return err
		}
	}
```

`publishChanges` 把 Step 0 的 deliverable 短路改为发增量事件：

```go
	if input.BindingToken.NormalizedKind() == generation.TargetKindSessionDeliverable {
		if a.Events == nil || a.Assets == nil {
			return nil
		}
		views := make([]map[string]any, 0, len(input.AssetIDs))
		for _, assetID := range input.AssetIDs {
			stored, err := a.Assets.Get(ctx, assetID)
			if err != nil {
				return err
			}
			views = append(views, deliverableAssetView(stored, input.BindingToken.TargetID))
		}
		envelope := a2ui.ActionEnvelope{Version: a2ui.Version1, Actions: []a2ui.Action{{
			Type: a2ui.ActionUpdateCard, Surface: "deliverables",
			Target:  &a2ui.ActionTarget{Surface: "deliverables", CardID: "deliverables"},
			Payload: map[string]any{"assets": views},
		}}}
		return a.Events.Publish(ctx, a2ui.SSEEvent{ID: "deliverable:" + input.Job.ID, SessionID: input.Job.SessionID, Event: a2ui.EventAction, Payload: envelope, CreatedAt: time.Now()})
	}
```

（`PublishFinalized` 传入的 FinalizationCommit.AssetIDs 已是 job.ResultAssetIDs ✓ :193-199。`deliverableAssetView` 输出 id/url/kind/mime_type/target_id 等前端渲染所需字段，字段名对齐 asset 模型与前端 `asset.url` 约定。SSEEvent 构造字段以 a2ui 包实际模型为准，含 CreatedAt 的时钟来源沿用包内既有做法。）

- [x] **Step 4: 跑测试 + 包回归**

Run: `go test ./internal/aigc/generationruntime`
Expected: 全部 PASS（含按 Step 1 括注修正的既有端到端断言）

- [x] **Step 5: Commit**

```bash
git add internal/aigc/generationruntime/
git commit -m "feat(generationruntime): deliverable 完成投影——资产打标 + deliverables surface 增量事件"
```

---

### Task 5: 轻模板×4 数据 + system prompt 目录注入

**Files:**
- Create: `internal/aigc/orchestration/light_templates.go`
- Test: `internal/aigc/orchestration/light_templates_test.go`
- Modify: `internal/aigc/agent/deepseek.go:93-96`（instruction 追加轻直出指引段）

- [x] **Step 1: 写失败测试**

```go
package orchestration

import (
	"strings"
	"testing"
)

func TestLightTemplatesAreValid(t *testing.T) {
	if len(LightTemplates) != 4 {
		t.Fatalf("v1 ships exactly 4 light templates, got %d", len(LightTemplates))
	}
	seen := map[string]struct{}{}
	for _, template := range LightTemplates {
		if strings.TrimSpace(template.Key) == "" || strings.TrimSpace(template.Name) == "" || strings.TrimSpace(template.Description) == "" {
			t.Fatalf("template three-elements are required: %+v", template)
		}
		if _, dup := seen[template.Key]; dup {
			t.Fatalf("duplicate template key %s", template.Key)
		}
		seen[template.Key] = struct{}{}
		if err := template.Node.Validate(); err != nil {
			t.Fatalf("template %s node: %v", template.Key, err)
		}
		if template.Node.ToolKey != "generate_media" {
			t.Fatalf("v1 light template node must be generate_media, got %s", template.Node.ToolKey)
		}
		if !strings.Contains(string(template.Node.Arguments), "session_deliverable") {
			t.Fatalf("template %s must target session_deliverable", template.Key)
		}
	}
	catalog := LightTemplateCatalogText()
	for _, key := range []string{"image_creation", "video_creation", "music_creation", "audio_creation"} {
		if !strings.Contains(catalog, key) {
			t.Fatalf("catalog text must mention %s", key)
		}
	}
}
```

- [x] **Step 2: 跑测试确认失败**

Run: `go test ./internal/aigc/orchestration -run TestLightTemplates -v`

- [x] **Step 3: 实现（模板三要素对齐终版 §2.2 初始库；参数骨架 prompt 为槽）**

```go
package orchestration

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/capability"
)

// LightTemplate 是编排库初始模板的轻直出子集（终版 §2.2/§6.1 第 2 步）：
// 单节点 generate_media(session_deliverable)。深编排模板（视频规划等）
// 仍由五能力流程承载，不在此表。
type LightTemplate struct {
	Key         string   `json:"key"`
	Name        string   `json:"name"`
	Description string   `json:"description"` // 命中判定依据
	Node        PlanNode `json:"node"`        // Arguments 中 <PROMPT> 为参数槽
}

var LightTemplates = []LightTemplate{
	{
		Key: "image_creation", Name: "图片创作",
		Description: "从想法/文本/参考快速生成图片；单张/多张皆为参数，无需故事板。",
		Node:        PlanNode{Kind: NodeKindCapability, ToolKey: capability.GenerateMediaToolKey, Arguments: json.RawMessage(`{"target":"session_deliverable","media_kind":"image","prompt":"<PROMPT>"}`)},
	},
	{
		Key: "video_creation", Name: "视频创作（浅）",
		Description: "从单条想法直出短视频；需要叙事结构/多镜头时改走五阶段深流程。",
		Node:        PlanNode{Kind: NodeKindCapability, ToolKey: capability.GenerateMediaToolKey, Arguments: json.RawMessage(`{"target":"session_deliverable","media_kind":"video","prompt":"<PROMPT>"}`)},
	},
	{
		Key: "music_creation", Name: "音乐创作",
		Description: "按风格/情绪/时长生成音乐。",
		Node:        PlanNode{Kind: NodeKindCapability, ToolKey: capability.GenerateMediaToolKey, Arguments: json.RawMessage(`{"target":"session_deliverable","media_kind":"music","prompt":"<PROMPT>"}`)},
	},
	{
		Key: "audio_creation", Name: "音频创作",
		Description: "文本变语音（旁白/播报）。",
		Node:        PlanNode{Kind: NodeKindCapability, ToolKey: capability.GenerateMediaToolKey, Arguments: json.RawMessage(`{"target":"session_deliverable","media_kind":"audio","prompt":"<PROMPT>"}`)},
	},
}

// LightTemplateCatalogText 生成注入 system prompt 的目录（M2 v1 形态：
// 目录整体注入上下文，Agent 直接判命中）。
func LightTemplateCatalogText() string {
	var builder strings.Builder
	builder.WriteString("轻直出模板目录（需求命中其一且无叙事结构要求时，直接调用 generate_media，参数按模板骨架补 prompt；深需求走五阶段）：\n")
	for _, template := range LightTemplates {
		builder.WriteString(fmt.Sprintf("- %s（%s）：%s 参数骨架 %s\n", template.Key, template.Name, template.Description, string(template.Node.Arguments)))
	}
	return builder.String()
}
```

- [x] **Step 4: deepseek.go instruction 追加（默认 instruction 字符串末尾并入目录；import orchestration）**

在 `instruction == ""` 分支的字符串末尾（`…只允许在 Graph/Worker 内部执行。` 之后、`异步生成返回 accepted 后…` 之前或之后均可，选句末追加）加：

```go
	instruction = strings.TrimSpace(instruction + "\n" + orchestration.LightTemplateCatalogText())
```

（放在 `if instruction == ""` 块内、赋值之后：仅默认 instruction 注入；调用方显式传 Instruction 时尊重其内容不强改。）

- [x] **Step 5: 跑测试 + agent 包回归**

Run: `go test ./internal/aigc/orchestration ./internal/aigc/agent`
Expected: 全部 PASS

- [x] **Step 6: Commit**

```bash
git add internal/aigc/orchestration/ internal/aigc/agent/deepseek.go
git commit -m "feat(orchestration): 轻直出模板×4 + 目录注入 system prompt（M2 v1 目录整体注入形态）"
```

---

### Task 6: 前端 deliverables surface 分支 + 轻交付物面板

**Files:**
- Modify: `frontend/src/features/aigc/AigcWorkspacePage.jsx`（updateA2UICard surface 分支 :2548；deliverables state；左栏渲染分支 :968）
- Create: `frontend/src/features/aigc/DeliverablesPanel.jsx`
- Test: `frontend/src/app/App.test.jsx`（沿用 MockEventSource + updateCardEvent 模式追加集成测试）

- [x] **Step 1: 写失败测试（App.test.jsx 追加；helper 沿用 :2892 updateCardEvent）**

```jsx
it('renders session deliverables when a deliverables surface update arrives', async () => {
  // 沿用既有测试的 mount + session 建立流程（复制邻近测试的 setup 块）
  // MockEventSource emit:
  // updateCardEvent('deliverables', 'deliverables', {
  //   assets: [{ id: 'asset-1', url: 'https://cdn/x.png', kind: 'image', mime_type: 'image/png', target_id: 'deliverable:img-1' }],
  // })
  // 断言：
  //   屏幕出现交付物面板（data-testid="deliverables-panel"）
  //   <img src="https://cdn/x.png"> 存在
  // 再 emit 同 id 资产的更新（url 变化）：面板内仍只有一个条目（按 id merge 去重）
});
```

- [x] **Step 2: 跑测试确认失败**

Run: `cd frontend && npx vitest run src/app/App.test.jsx -t deliverables`
Expected: FAIL

- [x] **Step 3: 实现**

`AigcWorkspacePage.jsx`：

1. state：`const [deliverables, setDeliverables] = useState([]);`（会话切换时清空，跟 storyboard 同处置；`refreshSessionData` 拉到 assets 后以 `asset.metadata?.target_kind === 'session_deliverable'` 过滤初始化——注意后端 GET assets 返回的 metadata 字段名以实际响应为准，执行时核实 listSessionAssets 的序列化）。
2. `updateA2UICard` 在 `'storyboard'` 分支之前/之后加：

```jsx
    if (surface === 'deliverables') {
      const incoming = Array.isArray(payload?.assets) ? payload.assets : [];
      if (incoming.length > 0) {
        setDeliverables((current) => {
          const byId = new Map(current.map((item) => [item.id, item]));
          incoming.forEach((item) => { if (item && item.id) byId.set(item.id, { ...byId.get(item.id), ...item }); });
          return Array.from(byId.values());
        });
      }
      return;
    }
```

3. 左栏渲染：storyboard 为空且 deliverables 非空时渲染 `<DeliverablesPanel assets={deliverables} />`；storyboard 存在时面板显示在故事板下方或以 tab 并存——**v1 取最简**：`<StoryboardPanel …/>` 之后无条件渲染 `{deliverables.length > 0 && <DeliverablesPanel assets={deliverables} />}`。

`DeliverablesPanel.jsx`（对齐 StoryboardPanel 的资产渲染方式，按 kind/mime 分流 img/video/audio）：

```jsx
export default function DeliverablesPanel({ assets }) {
  if (!assets || assets.length === 0) return null;
  return (
    <section className="deliverables-panel" data-testid="deliverables-panel">
      <h3>交付物</h3>
      <div className="deliverables-grid">
        {assets.map((item) => (
          <figure key={item.id} className="deliverable-item">
            <DeliverableMedia asset={item} />
            <figcaption>{item.target_id || item.id}</figcaption>
          </figure>
        ))}
      </div>
    </section>
  );
}

function DeliverableMedia({ asset }) {
  const mime = asset.mime_type || '';
  const kind = asset.kind || '';
  if (kind === 'video' || mime.startsWith('video/')) return <video src={asset.url} controls />;
  if (kind === 'audio' || kind === 'music' || mime.startsWith('audio/')) return <audio src={asset.url} controls />;
  return <img src={asset.url} alt={asset.target_id || asset.id} />;
}
```

（样式沿用工程既有 CSS 组织方式——找 StoryboardPanel 的样式所在文件按同模式加最小网格样式。）

- [x] **Step 4: 跑前端测试全量**

Run: `cd frontend && npm run test`
Expected: 全部 PASS

- [x] **Step 5: Commit**

```bash
git add frontend/src/
git commit -m "feat(frontend): 轻交付物面板——deliverables surface 增量合并渲染（无故事板会话可见产物）"
```

---

### Task 7: 全量回归与收尾

- [x] **Step 1: 后端全仓 + 前端全量**

Run: `go build ./... && go vet ./... && go test ./... 2>&1 | tail -5 && cd frontend && npm run test`
Expected: 全部 PASS

- [x] **Step 2: DB 包强制真跑**

Run: `go test ./internal/aigc/generation ./internal/aigc/generationruntime ./internal/aigc/capabilityruntime ./internal/aigc/server -count=1`
Expected: 全部 PASS

- [x] **Step 3: 勾选计划 + 执行记录 + 收尾提交**

```bash
git add docs/superpowers/plans/2026-07-12-step2-light-templates-deliverable.md
git commit -m "chore: 第2步轻模板+目标分派+轻交付物视图完成"
```

---

## 计划自审记录

- 覆盖检查：终版 §6.1 第 2 步三件套——轻模板×4 ✓（Task 5，§2.2 三要素对齐）、generate_media 按目标类型分派 ✓（Task 1 入参/Task 2 运行时）、前端无 storyboard 交付物展示 ✓（Task 4 事件 + Task 6 面板）；附带补严 workflow_store 校验漏洞 ✓（Task 3，取证时发现）。
- 边界外：深浅自动裁决与模板命中入场（第 3 步）；音乐/音频专用 provider（现走 demo audio handler）；deliverable 的重生成/删除交互；credits 金额化预算。
- 已知不确定点（诚实标注）：capabilityruntime 既有测试的 fake 构造符号名、asset.Asset 的 URL/Metadata 字段序列化名、a2ui.SSEEvent 构造细节、前端 refreshSessionData 的 metadata 字段名——执行时以对应文件既有代码为准，意图与断言已完整给出。Task 4 中 failingEventPublisher 既有断言将按"发布失败传播（outbox 重试语义）"修正。

## 执行记录（2026-07-12，全部完成）

**提交序列**（分支 `cc/core-version`，每 task 一提交 + 一次 gofmt 补充）：

- Task 1 — `capability`：Intent 分派字段/Validate/NormalizedTarget/NormalizedCount + tools.go description/params（phase/policy 取消 Required，storyboard 必填由 Validate 兜底）。agent 包 directive 白名单兼容全绿。
- Task 2 — `capabilityruntime.generateDeliverableMedia`：分派插在两个 replay 之后（replay 对两目标通用，幂等断言过）；空 storyboard/spec store 下派发成功 = 免闸门物理证明；job/batch 形态断言全过（provider 分派、TargetKind、active+auto_approve、WakeOnTerminal、per-variant fingerprint）。
- Task 3 — `workflow_store.go` 校验门控补严：`StoryboardID != "" || NormalizedKind()==session_deliverable`；不做无条件校验（既有最小 job 构造会被误伤，超本步意图）。
- Task 4 — deliverable commit 打标（metadata.target_kind/deliverable_target_id）+ `publishDeliverable`（deliverables surface、CardID=deliverables、增量 assets 视图）；发布失败传播（outbox 重试语义），既有端到端断言随语义更新。
- Task 5 — `orchestration.LightTemplates` ×4（§2.2 三要素；参数骨架 `<PROMPT>` 槽）+ `LightTemplateCatalogText` 注入 deepseek.go 默认 instruction（显式传 Instruction 时不强改）。
- Task 6 — 前端：`updateA2UICard` deliverables 分支按 id upsert 进 assets 单一事实源；`DeliverablesPanel`（img/video/audio 按 kind/mime 分流）；左栏 `aigc-left-pane` 容器承载故事板+交付物（CSS 迁移滚动/边框职责）；App.test.jsx 集成测试（SSE 增量渲染 + 刷新一致 + 同 id merge 去重）。
- Task 7 — 后端全仓 build/vet/test 绿（DB 包 -count=1 真跑）+ 前端 73/73 全绿。

**执行中发现并解决的计划外事实**：

- 测试首版把两条增量都放 messageEvents，被消息完成后的全量资产刷新（mock 返回空）覆盖——生产无此问题（DB 含 deliverable 资产），属 mock 不完整；修正为"刷新与事实源一致 + 刷新后手动 emit 增量锁 merge 语义"，同时避免了假绿（若 merge 分支缺失，第二段断言 y.png 覆盖必挂）。

**事实清单**：

- 已验证：四模态 provider 全落点（image2/seedance/audio，music 走 audio provider）；deliverable 全链（校验→派发→落库→打标→投影事件→前端渲染）各层单测/集成测试绿；storyboard 旧路径零行为变化（全部既有测试未动全绿）。
- 未验证：真实 provider 生成的端到端产物（需 DeepSeek/provider key + 手工会话，属验收范围）；音乐/音频当前共用 demo audio handler 的产物质量；深浅裁决由 LLM 按 system prompt 目录自行判断的命中质量（第 3 步范围）。
- 已知取舍：deliverable 无候选审批（active+auto_approve，终版 §5 两态定案）；phase/policy 在 LLM schema 不再 Required（Validate 兜底，directive 白名单回归绿）。
