# 第 0 步：绑定层 target-中立化 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让生成/落库/审批底座支持两态交付目标——`storyboard_slot`（现状，全链不变）与 `session_deliverable`（无故事板的会话交付物：无版本冲突检测、无候选审批、直接交付），为轻流程（快速出图等）铺地基。

**Architecture:** 沿用代码中已有的 `assembly:` 前缀分派先例，把分派依据升级为 `BindingToken.TargetKind` 显式字段（空值=storyboard_slot，兼容全部旧数据）。改动收敛在三个分派点：`BindingToken.Validate/Equal`、`BindingGuard.Check`、`Committer.commit/IsCommitted`。**Barrier、candidate 审批链、DeliveryPolicy、workflow store 均不动**（取证结论：Barrier 只看 job 级字段；`auto_approve+active` 是 DeliveryPolicy 已合法的组合，不触发审批；`ExpectedStoryboardVersion` 仅在 candidate 批次里被强校验而轻流程不走那条链）。

**Tech Stack:** Go 1.26，现有测试基建（generation 包 memory workflow store、adapters_test fake）。

**评审要求：本计划改动持久底座保护面（BindingToken/Finalization），执行前后须 Figo 评审。**

**背景证据（写计划时逐处核实）：**
- `BindingToken.Validate` 硬要求 StoryboardID 非空：`internal/aigc/generation/models.go:166`
- `assembly:` 前缀分派先例：`internal/aigc/generationruntime/adapters.go:110,289,310,340,216`
- 资产置可用的既有写法：`adapters.go:330-337`（`Availability = asset.AvailabilityAvailable` + `Assets.Save`）
- `IsCommitted` 的 assembly 短路：`adapters.go:216-218`
- candidate 审批链只在 `review_required` 时创建：`adapters.go` `needsApproval` + `generation/models.go:144`
- `NewFinalizationEngine` 测试构造惯例：`internal/aigc/generation/workflow_test.go:324,484`

---

### Task 1: BindingToken 增加 TargetKind（两态 + 归一化 + 校验分支）

**Files:**
- Modify: `internal/aigc/generation/models.go`（BindingToken 结构体与 Validate/Equal，约 :153-190）
- Test: `internal/aigc/generation/binding_token_test.go`（新建）

- [ ] **Step 1: 写失败测试**

```go
package generation

import "testing"

func TestBindingTokenTargetKindValidation(t *testing.T) {
	base := BindingToken{TargetID: "deliverable:img-1", AssetSlot: "primary", InputFingerprint: "fp"}

	deliverable := base
	deliverable.TargetKind = TargetKindSessionDeliverable
	if err := deliverable.Validate(); err != nil {
		t.Fatalf("session deliverable token must not require storyboard id: %v", err)
	}

	legacyEmptyKind := base // TargetKind 为空 = 默认 storyboard_slot，仍要求 StoryboardID
	if err := legacyEmptyKind.Validate(); err == nil {
		t.Fatal("legacy empty-kind token must still require storyboard id")
	}

	slot := base
	slot.TargetKind = TargetKindStoryboardSlot
	if err := slot.Validate(); err == nil {
		t.Fatal("storyboard_slot token must require storyboard id")
	}

	unknown := base
	unknown.TargetKind = "weird"
	if err := unknown.Validate(); err == nil {
		t.Fatal("unknown target kind must be rejected")
	}
}

func TestBindingTokenEqualNormalizesKind(t *testing.T) {
	a := BindingToken{StoryboardID: "sb", TargetID: "e1", AssetSlot: "s", InputFingerprint: "fp"}
	b := a
	b.TargetKind = TargetKindStoryboardSlot
	if !a.Equal(b) {
		t.Fatal("empty kind and explicit storyboard_slot must compare equal")
	}
	c := a
	c.TargetKind = TargetKindSessionDeliverable
	if a.Equal(c) {
		t.Fatal("different target kinds must not compare equal")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/aigc/generation -run TestBindingToken -v`
Expected: FAIL（`TargetKindSessionDeliverable` undefined）

- [ ] **Step 3: 最小实现**

在 `models.go` 的 BindingToken 附近加：

```go
const (
	TargetKindStoryboardSlot     = "storyboard_slot"
	TargetKindSessionDeliverable = "session_deliverable"
)
```

BindingToken 结构体加字段（放在 StoryboardID 前）：

```go
	TargetKind       string `json:"target_kind,omitempty"`
```

加归一化方法：

```go
// NormalizedKind treats an empty TargetKind as storyboard_slot so that every
// token persisted before this field existed keeps its original semantics.
func (t BindingToken) NormalizedKind() string {
	kind := strings.TrimSpace(t.TargetKind)
	if kind == "" {
		return TargetKindStoryboardSlot
	}
	return kind
}
```

Validate 改为（保留原 revisions/fingerprint 检查不动）：

```go
func (t BindingToken) Validate() error {
	kind := t.NormalizedKind()
	if kind != TargetKindStoryboardSlot && kind != TargetKindSessionDeliverable {
		return fmt.Errorf("invalid binding target kind %q", t.TargetKind)
	}
	if strings.TrimSpace(t.TargetID) == "" || strings.TrimSpace(t.AssetSlot) == "" {
		return fmt.Errorf("binding token target and asset slot are required")
	}
	if kind == TargetKindStoryboardSlot && strings.TrimSpace(t.StoryboardID) == "" {
		return fmt.Errorf("binding token storyboard id is required for storyboard slot targets")
	}
	if t.TargetRevision < 0 || t.PromptRevision < 0 || t.GenerationEpoch < 0 || t.SpecVersion < 0 || t.AggregateVersion < 0 {
		return fmt.Errorf("binding token revisions cannot be negative")
	}
	if strings.TrimSpace(t.InputFingerprint) == "" {
		return fmt.Errorf("binding token input fingerprint is required")
	}
	return nil
}
```

Equal 的第一行加 kind 比较：

```go
	return t.NormalizedKind() == other.NormalizedKind() &&
		strings.TrimSpace(t.StoryboardID) == strings.TrimSpace(other.StoryboardID) &&
		... // 其余保持原样
```

- [ ] **Step 4: 跑测试确认通过 + 包内回归**

Run: `go test ./internal/aigc/generation -run TestBindingToken -v && go test ./internal/aigc/generation`
Expected: 新测试 PASS，包内全部既有测试 PASS（空 kind 归一化保证零行为变化）

- [ ] **Step 5: Commit**

```bash
git add internal/aigc/generation/models.go internal/aigc/generation/binding_token_test.go
git commit -m "feat(generation): BindingToken 支持 target_kind 两态（storyboard_slot 默认/session_deliverable）"
```

---

### Task 2: BindingGuard.Check 的 session_deliverable 短路

**Files:**
- Modify: `internal/aigc/generationruntime/adapters.go`（`StoryboardBindingAdapter.Check` :109 起；先读 :427-490 确认 `BindingGuard.Check`/`CheckWithFingerprint` 的委托关系，若外层有 fingerprint 重算逻辑，短路加在最外入口）
- Test: `internal/aigc/generationruntime/adapters_test.go`

- [ ] **Step 1: 写失败测试**

```go
func TestBindingCheckSessionDeliverableSkipsStoryboard(t *testing.T) {
	// Repository 故意为 nil：deliverable 路径若碰 storyboard 会直接报
	// "storyboard repository is required"，这是本测试的物理隔离证明。
	guard := BindingGuard{StoryboardBindingAdapter{}}
	token := generation.BindingToken{
		TargetKind:       generation.TargetKindSessionDeliverable,
		TargetID:         "deliverable:img-1",
		AssetSlot:        "primary",
		InputFingerprint: "fp",
	}
	check, err := guard.Check(context.Background(), token)
	if err != nil {
		t.Fatalf("deliverable check must not touch storyboard store: %v", err)
	}
	if !check.TargetExists || !check.Matches {
		t.Fatalf("deliverable target must always exist and match, got %+v", check)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/aigc/generationruntime -run TestBindingCheckSessionDeliverable -v`
Expected: FAIL（error "storyboard repository is required"）

- [ ] **Step 3: 最小实现**

`StoryboardBindingAdapter.Check` 函数体第一行加（在 assembly 特判之前）：

```go
	if token.NormalizedKind() == generation.TargetKindSessionDeliverable {
		// Session deliverables have no storyboard target that can drift; the
		// input fingerprint inside the token is the only identity that matters.
		return generation.BindingCheck{TargetExists: true, Matches: true, Current: token}, nil
	}
```

若 Step 1 读码发现 `BindingGuard.Check`（:433）在委托前有额外逻辑，把同样的短路放到该入口最前。

- [ ] **Step 4: 跑测试确认通过 + 包内回归**

Run: `go test ./internal/aigc/generationruntime`
Expected: 全部 PASS

- [ ] **Step 5: Commit**

```bash
git add internal/aigc/generationruntime/adapters.go internal/aigc/generationruntime/adapters_test.go
git commit -m "feat(generationruntime): 绑定校验对 session_deliverable 目标短路（无可漂移目标）"
```

---

### Task 3: commit / IsCommitted 的 session_deliverable 分支

**Files:**
- Modify: `internal/aigc/generationruntime/adapters.go`（`commit` :279 起、`IsCommitted` :197 起；外层 `Commit` :153 的 Postgres 事务包装不动——deliverable 分支在事务内不会触碰 storyboard/approval 表，包装无害）
- Test: `internal/aigc/generationruntime/adapters_test.go`

- [ ] **Step 1: 先读 adapters_test.go 既有测试的 Assets fake 构造方式（约 50 行内即可找到），复用同一构造；然后写失败测试**

```go
func TestCommitSessionDeliverableOnlyPublishesAssets(t *testing.T) {
	assets := /* 复用本文件既有 Assets fake/memory 构造 */
	seeded, _ := assets.Save(context.Background(), asset.Record{ // 字段名以 asset 包实际模型为准
		ID: "asset-1", SessionID: "sess-1", Availability: asset.AvailabilityPendingBilling,
	})
	_ = seeded

	// Repository/Commands/Approvals 全部为 nil：deliverable commit 碰任何一个都会报错。
	adapter := StoryboardBindingAdapter{Assets: assets}
	job := generation.GenerationJob{
		ID: "job-1", SessionID: "sess-1",
		DeliveryPolicy: generation.DeliveryPolicy{
			BindingMode:    generation.BindingModeActive,
			ApprovalPolicy: generation.ApprovalAutoApprove,
			ChargePolicy:   generation.ChargePostpaidNoReservation,
		},
		BindingToken: generation.BindingToken{
			TargetKind:       generation.TargetKindSessionDeliverable,
			TargetID:         "deliverable:img-1",
			AssetSlot:        "primary",
			InputFingerprint: "fp",
		},
	}

	err := adapter.Commit(context.Background(), generation.FinalizationCommit{
		Job: job, AssetIDs: []string{"asset-1"},
		BindingToken: job.BindingToken,
		BindingMode:  generation.BindingModeActive,
		ApprovalPolicy: generation.ApprovalAutoApprove,
	})
	if err != nil {
		t.Fatalf("deliverable commit must succeed without storyboard/approval stores: %v", err)
	}

	stored, _ := assets.Get(context.Background(), "asset-1")
	if stored.Availability != asset.AvailabilityAvailable {
		t.Fatalf("asset must be available after commit, got %s", stored.Availability)
	}

	committed, err := adapter.IsCommitted(context.Background(), job, []string{"asset-1"})
	if err != nil || !committed {
		t.Fatalf("IsCommitted must be true after deliverable commit: %v %v", committed, err)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/aigc/generationruntime -run TestCommitSessionDeliverable -v`
Expected: FAIL（"storyboard binding services are required" —— commit 的 storyboard 校验段被触发）

- [ ] **Step 3: 最小实现（三处 diff）**

`commit`（:279）函数体开头定义：

```go
	isDeliverable := input.BindingToken.NormalizedKind() == generation.TargetKindSessionDeliverable
```

三处条件改造：

```go
	// ① assembly 的 pre-commit Check 保持原样（不含 deliverable）
	// ② storyboard 校验段：
	if !strings.HasPrefix(input.BindingToken.TargetID, "assembly:") && !isDeliverable {
		... // 原 storyboard 读取与 token 比对逻辑不动
	}
	// ③ 资产 available 循环之后的短路 return：
	if strings.HasPrefix(input.BindingToken.TargetID, "assembly:") || isDeliverable {
		return nil
	}
```

`IsCommitted`（:216 短路处）：

```go
	if strings.HasPrefix(job.BindingToken.TargetID, "assembly:") ||
		job.BindingToken.NormalizedKind() == generation.TargetKindSessionDeliverable {
		return true, nil
	}
```

- [ ] **Step 4: 跑测试确认通过 + 包内回归**

Run: `go test ./internal/aigc/generationruntime`
Expected: 全部 PASS

- [ ] **Step 5: Commit**

```bash
git add internal/aigc/generationruntime/adapters.go internal/aigc/generationruntime/adapters_test.go
git commit -m "feat(generationruntime): session_deliverable 落库分支——只发布资产，不做故事板绑定与审批"
```

---

### Task 4: FinalizationEngine 级端到端（物理隔离证明）

**Files:**
- Test: `internal/aigc/generationruntime/adapters_test.go`（新增一个端到端测试；FinalizationEngine 构造沿用 `internal/aigc/generation/workflow_test.go:484` 的形状——`NewFinalizationEngine(FinalizationEngineConfig{Store: ..., Bindings: ..., Committer: ...})`；workflow memory store 构造沿用 workflow_test.go:49 附近测试所用的同一 helper，跨包需要其为导出符号，若未导出则本测试放入 `internal/aigc/generation` 包并注入本包 adapter 的接口值）

- [ ] **Step 1: 写失败测试（骨架，store/job 构造按上述既有 helper 落实）**

```go
func TestFinalizeSessionDeliverableEndToEnd(t *testing.T) {
	// 1) memory workflow store 里放入一个 finalizing 前状态的 deliverable job：
	//    TargetKind=session_deliverable、StoryboardID=""、auto_approve+active、
	//    provider 结果已就绪（沿用既有测试的 job/receipt 构造方式）
	// 2) engine := generation.NewFinalizationEngine(generation.FinalizationEngineConfig{
	//        Store: store,
	//        Bindings: BindingGuard{StoryboardBindingAdapter{Assets: assets}},   // Repository nil
	//        Committer: StoryboardBindingAdapter{Assets: assets},                 // Approvals nil
	//    })
	// 3) job, err := engine.Finalize(ctx, jobID)
	// 断言：
	//   err == nil && job.Status == generation.StatusSucceeded
	//   资产 Availability == available
	//   （Repository/Approvals 均为 nil 而全链无 panic/无错 = 物理证明不碰 storyboard/审批）
}
```

- [ ] **Step 2: 跑测试确认失败（在 Task 3 未合入的分支上应报 storyboard services required；合入后首跑若因 job 构造缺字段失败，按报错补齐构造）**

Run: `go test ./internal/aigc/generationruntime -run TestFinalizeSessionDeliverableEndToEnd -v`

- [ ] **Step 3: 无实现改动预期——本任务是集成验证；若暴露 Finalize 主流程里其他 storyboard 硬依赖（如 Inspector/Discarder 路径），逐一按 Task 3 的分派模式补分支，每处补一个对应断言**

- [ ] **Step 4: 全链绿**

Run: `go test ./internal/aigc/generationruntime ./internal/aigc/generation`
Expected: 全部 PASS

- [ ] **Step 5: Commit**

```bash
git add internal/aigc/generationruntime/adapters_test.go
git commit -m "test(generationruntime): session_deliverable 全链 finalize 端到端（storyboard/审批物理隔离证明）"
```

---

### Task 5: 全量回归与收尾

**Files:** 无新改动（验证任务）

- [ ] **Step 1: 全仓测试**

Run: `go test ./... 2>&1 | tail -20 && go vet ./...`
Expected: 全部 PASS（依赖 DB/Redis 的测试按仓库惯例在基础设施缺失时 skip 而非 fail）

- [ ] **Step 2: 有本地基础设施则起 docker compose 跑 DB 相关测试**

Run: `docker compose -f docker-compose.local.yml up -d && go test ./internal/aigc/generation ./internal/aigc/generationruntime ./internal/aigc/approvalruntime -count=1`
Expected: 全部 PASS（含 Postgres 路径）

- [ ] **Step 3: 提交收尾说明**

```bash
git add -A && git commit -m "chore: 第0步绑定层target-中立化完成——两态交付目标全链绿" --allow-empty
```

- [ ] **Step 4: 请 Figo 评审本计划全部 diff（底座保护面变更），评审通过后才进入第 1 步（推进映射数据化）计划**

---

## 计划自审记录

- 覆盖检查：终版 §5 交付物模型两态 ✓（Task 1）、无版本冲突检测 ✓（Task 2 恒 match）、无候选审批 ✓（Task 3 auto_approve+active 路径 + Repository/Approvals nil 物理证明）、Barrier/candidate 链不动 ✓（取证依据在 header）。
- 边界外（后续计划）：generate_media 入口的目标类型分派（第 2 步轻模板计划）、前端轻交付物视图（第 2 步）、推进映射数据化（第 1 步）。
- 已知不确定点（诚实标注）：Task 3/4 测试中 Assets fake 与 workflow memory store 的构造符号名未逐字核实，执行时以对应测试文件既有构造为准——意图与断言已完整给出。
