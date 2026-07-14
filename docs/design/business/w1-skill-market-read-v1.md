# W1-E Skill Market 公开读取 v1

> 状态：Frozen / Approved for W1-E Read-only
>
> 设计日期：2026-07-14
>
> 冻结日期：2026-07-14
>
> 覆盖范围：匿名公开 Skill 最新列表、公开详情、安全字段投影、治理即时可见性与前后端只读纵切
>
> 依赖：[W1 Skill 与 Tool 入口基础契约 v1](../cross-module/w1-skill-tool-entry-contract-v1.md)、[W1-D Skill 治理能力与管理处置 v1](./w1-skill-governance-v1.md)、[Project Skill Binding 与 Session Snapshot Producer 契约 v1](../cross-module/project-skill-binding-contract-v1.md)

## 1. 结论与完成定义

W1-E 第一阶段注册独立的公开 Market Read API，让游客和登录用户读取同一份由当前不可变发布快照派生的安全投影。公开可见性的唯一业务条件是 Skill 已有 current published pointer 且当前治理状态为 `active`；列表和详情必须在一次 PostgreSQL 查询中同时消费这个条件，不能依赖前端、缓存、旧列表结果或调用方保存的 Skill ID。

完成必须同时满足：

1. `GET /api/v1/skill-market` 与 `GET /api/v1/skill-market/:skill_id` 无需 Session，登录状态不改变同一 Skill 的公开字段；
2. Draft、从未发布、`suspended`、`offline` Skill 均不公开；合法但不可见的详情统一返回 `404 SKILL_MARKET_NOT_FOUND`；
3. 公开 DTO 不返回草稿、运行指令、能力 guidance、内部发布修订、快照 ID、digest、治理状态/纪元、审核、审计、权限或管理 ETag；
4. 列表固定 newest-first keyset 分页，Repository 使用 `LIMIT 21` 和一次集合查询；详情使用一次集合查询，均无 N+1；
5. 本次实际读取的 `active + current pointer non-null` 公开候选中，Published pointer、publication revision、source revision、schema、Canonical Definition、digest、Publisher 逻辑引用任一损坏时返回 `503 SKILL_PERSISTENCE_UNAVAILABLE`，不能把损坏行静默隐藏或伪装为 404；
6. 所有成功和失败响应都使用 `Cache-Control: no-store`，治理暂停、恢复或下架提交后，下一次公开请求直接读取数据库当前态；
7. 前端 `/skills` 和 `/skills/:skill_id` 只消费严格公开 DTO，覆盖 loading、empty、error/retry、分页、404 与非法路径，不复用 Owner DTO 或生产 Mock；
8. 真实 PostgreSQL 与浏览器冒烟证明发布后可见、草稿变化不泄漏、暂停后移除、恢复后重现、下线后移除。

本设计只关闭公开发现的只读门禁，不表示完整 Skill Market 或完整 `SMK-005/006/031` 已完成。跨发布者使用必须在独立 Public Market Binding 契约中增加新的权限 basis 后才能开放。

## 2. 明确排除

本批不实现：

- 跨 Owner 的 Project Skill Binding、市场“立即使用”、安装记录或给已有 Project 追加 Skill；
- 收藏、最近使用、举报、申诉、Publisher Profile、关注或消费者社交关系；
- 搜索、分类/标签筛选、榜单、推荐、运营位、曝光和点击统计；
- 调用次数、成功率、费用范围、最大积分、收益或任何没有权威事实的零值占位；
- 封面 Asset 公共访问；W1 `cover_asset_id` 继续固定为 `null`；
- 非空公共 Tool 引用、权限说明、数据使用策略或 Tool 费用；
- 发布版本列表、历史快照、版本切换、版本比较或公开 ETag；
- 治理管理页面、Owner 治理原因、历史 Session Kill Switch 或在线治理中断。

“收藏”是消费者偏好，“启用/使用”是 Project Skill Binding，“最近使用”只能来自真实 Skill Invocation；查看详情或仅创建 Binding 均不能冒充已经使用。三者不能混成含义不明的 `skill_install`。当前 Owner-only QuickCreate 继续拒绝其他发布者的 Skill，前端不得把公开 Market Skill 直接提交给现有 Owner Picker。

## 3. 公开可见性与权威完整性

### 3.1 可见性谓词

列表与详情 Repository 直接使用：

```text
skill.current_published_snapshot_id IS NOT NULL
AND skill.governance_status = 'active'
```

并在同一查询和 Mapper 中验证：

```text
published.id = skill.current_published_snapshot_id
published.skill_id = skill.id
published.publication_revision = skill.publication_revision
published.definition_schema_version = 'skill_definition.v1'
canonical(published.definition_json).sha256 = published.content_digest
source_revision.id = published.source_content_revision_id
source_revision.skill_id = skill.id
source_revision.content_digest = published.content_digest
publisher.id = skill.owner_user_id
```

`published_by_user_id` 是批准发布的 Reviewer，不是 Publisher，禁止用于市场发布者投影。Publisher 展示名取 `user_account.display_name` 的当前安全值；它是当前公开身份投影，不属于已审核 Definition 快照。

本批明确接受现状：Publisher 账户后来变为 `disabled/cancelled` 时，只要该 Skill 仍为 current published + active，它仍继续公开；Market Repository 不偷偷追加账户状态条件。后续账户处置若要求批量暂停或下线 Skill，必须另行冻结账户命令、Skill 治理事务/Outbox、失败恢复和审计。本批真实 PostgreSQL 测试必须锁定这一现状，避免不同查询各自猜测。

### 3.2 不可见与损坏

- Draft-only、suspended、offline、不存在：先按可见性门禁排除，公开列表不返回、详情统一 404；即使这些不可见 Skill 同时存在内部损坏，也不通过公开接口暴露；
- current pointer 非空但发布快照缺失、逻辑关联错配或摘要损坏：503；
- Publisher 账户缺失或 ID 错配：503；
- 未知 Definition schema、非法 UUIDv7、非法时间或非法 Canonical 文本：503。

详情对 Published Snapshot、Source Content Revision 和 Publisher 使用 `LEFT JOIN`，让 active + current pointer 非空但逻辑关联损坏的单个目标进入 Mapper 并返回 503，不能用 `INNER JOIN` 把损坏事实静默过滤。

列表最初设计为一条平铺 `LEFT JOIN + OR + NULLS FIRST` 查询；真实 PostgreSQL 16 评审证明该形态会产生 Hash Join 与完整候选集 Sort，008 keyset 索引无法驱动分页，因此禁止实现。冻结实现仍使用一条 SQL，但拆成两个 `MATERIALIZED` 候选分支：

1. `market_dangling` 从 active + current pointer 非空的 Skill 出发，`LEFT JOIN` Published Snapshot，固定 `published.id IS NULL`、不应用 cursor、`LIMIT 1`；任一悬空 current pointer 都会作为 NULL-first 候选触发 Mapper 503，不能被后续页边界隐藏；
2. `market_valid` 从 `skill_published_snapshot` 的 008 索引出发，通过有界 `LATERAL` 关联 current active Skill，再 `LEFT JOIN` Source Revision 与 Publisher；只在该分支应用 keyset 边界，按 `published_at DESC, skill_id DESC` 读取 `LIMIT 21`；
3. 两个分支 `UNION ALL` 后只对最多 22 行执行 `NULLS FIRST` 排序并最终 `LIMIT 21`。Repository 必须先映射并校验实际返回的全部候选，再截取前 20 条。

其等价伪 SQL 为：

```sql
WITH market_dangling AS MATERIALIZED (
  SELECT ...
  FROM business.skill AS skill
  LEFT JOIN business.skill_published_snapshot AS published
    ON published.id = skill.current_published_snapshot_id
   AND published.skill_id = skill.id
  WHERE skill.governance_status = 'active'
    AND skill.current_published_snapshot_id IS NOT NULL
    AND published.id IS NULL
  LIMIT 1
),
market_valid AS MATERIALIZED (
  SELECT ...
  FROM business.skill_published_snapshot AS published
  JOIN LATERAL (
    SELECT candidate.*
    FROM business.skill AS candidate
    WHERE candidate.id = published.skill_id
      AND candidate.current_published_snapshot_id = published.id
      AND candidate.governance_status = 'active'
    OFFSET 0
  ) AS skill ON TRUE
  LEFT JOIN ...
  WHERE (published.published_at, published.skill_id) < (?, ?)
  ORDER BY published.published_at DESC, published.skill_id DESC
  LIMIT 21
)
SELECT * FROM market_dangling
UNION ALL
SELECT * FROM market_valid
ORDER BY published_at DESC NULLS FIRST, skill_id DESC
LIMIT 21
```

没有 cursor 时省略 `market_valid` 的 tuple 条件；`market_dangling` 始终不带 cursor。第 21 条有效候选损坏也必须令整页 503，不能因为它只用于判断 `has_more` 而跳过校验。除悬空 current pointer guard 外，列表的 503 范围只覆盖本次实际读取的 21 条有效候选；其他全库完整性由 Readiness/巡检负责，分页 API 不冒充全库校验。

## 4. 公开 HTTP v1

| 方法 | 路径 | 认证与语义 |
| --- | --- | --- |
| `GET` | `/api/v1/skill-market?cursor=...` | 匿名可读；current published + active 最新列表 |
| `GET` | `/api/v1/skill-market/:skill_id` | 匿名可读；安全公开详情 |

首批列表 Query 只允许可选 `cursor`，并且最多出现一次。未知 Query、重复 cursor、空 cursor、超过 1024 字节、非法 Base64URL、未知 JSON 字段、尾随 JSON、非法时间或非规范小写 UUIDv7 均返回 `400 INVALID_REQUEST`。

首批不宣称搜索、分类或标签筛选。后续若增加过滤，必须先冻结 Unicode/NFC、匹配规则、查询索引、`EXPLAIN` 门禁，并把规范化 filter 或 filter digest 纳入 cursor，禁止跨过滤条件复用游标。

### 4.1 列表 Envelope

```json
{
  "items": [{
    "skill_id": "019f0000-0000-7000-8000-000000000101",
    "name": "短片提示词助手",
    "summary": "帮助整理图片与视频提示词",
    "category": "视频",
    "tags": ["提示词", "视频"],
    "publisher": {
      "publisher_id": "019f0000-0000-7000-8000-000000000102",
      "display_name": "Dora Creator"
    },
    "published_at": "2026-07-14T10:00:00Z",
    "cover_asset": null,
    "declared_capability_keys": ["write_prompts"]
  }],
  "next_cursor": null,
  "request_id": "019f0000-0000-7000-8000-000000000103"
}
```

`declared_capability_keys` 按六能力产品固定顺序返回 `applicability=enabled` 的 key：

1. `plan_creation_spec`
2. `analyze_materials`
3. `plan_storyboard`
4. `generate_media`
5. `write_prompts`
6. `assemble_output`

该数组只表示当前发布 Definition 声明支持的领域，不证明 Graph 已审核、已注册或可执行。列表不返回 capability guidance 或 not-applicable reason。

### 4.2 详情 Envelope

```json
{
  "skill": {
    "skill_id": "019f0000-0000-7000-8000-000000000101",
    "name": "短片提示词助手",
    "summary": "帮助整理图片与视频提示词",
    "category": "视频",
    "tags": ["提示词", "视频"],
    "publisher": {
      "publisher_id": "019f0000-0000-7000-8000-000000000102",
      "display_name": "Dora Creator"
    },
    "published_at": "2026-07-14T10:00:00Z",
    "cover_asset": null,
    "declared_capability_keys": ["write_prompts"],
    "input_description": "输入创作主题与目标媒体",
    "output_description": "输出结构化提示词建议",
    "examples": [{"input": "城市夜景短片", "output": "镜头与提示词示例"}],
    "starter_prompts": ["帮我写一个城市夜景短片提示词"],
    "market_detail": "公开市场详情",
    "copyright_notice": "版权说明",
    "user_notice": "使用说明"
  },
  "request_id": "019f0000-0000-7000-8000-000000000103"
}
```

公开详情不直接返回完整 `SkillDefinitionV1`。特别禁止返回：

- `invocation_rules`；
- 六项 capability 的 `guidance` 和 `not_applicable_reason`；
- `public_tool_refs` 内部引用；当前非空引用本就不能发布；
- `schema_version`、内部 snapshot/publication/content revision 和 digest；
- `governance_status`、`governance_epoch`、治理 ETag、原因或审计；
- Draft、Review、Reviewer、Owner 邮箱、账户类型或账户状态。

## 5. 分页与一致性

固定页大小 20，Repository 读取 21 条，排序为：

```text
published_at DESC, skill_id DESC
```

opaque cursor schema 为 `skill_market_cursor.v1`，只包含：

- `schema_version`，固定为 `skill_market_cursor.v1`；
- `published_at_unix_nano`；
- `skill_id`，使用已经公开的 Skill UUIDv7，禁止把内部 Published Snapshot ID 放入可解码 cursor。

游标使用无填充 Base64URL、最大 1024 字节、严格 JSON 解码。下一页条件只应用于 `market_valid` 分支：

```text
(published_at, skill_id) < (?, ?)
```

`market_dangling` guard 在每一页都无条件运行并位于 outer `NULLS FIRST`，因此 cursor 不能跳过悬空 current pointer。

市场列表不是审计级快照。分页期间重新发布会使项目移动到前页，治理变化会使项目离开或重新进入集合；客户端跨页按 `skill_id` 去重，主动刷新时丢弃旧 cursor 从第一页重建。单个响应不得重复 ID、不得返回非 active 当前发布内容，也不得返回 offset/total 冒充一致快照。

## 6. Domain、Repository 与 DTO 边界

新增独立 `MarketRepository` 与 `MarketService`，不扩大 Owner/Reviewer/Governance HTTP DTO：

- `MarketRepository.ListPublished(ctx, boundary, limit)`：一次包含 dangling guard 与 index-driven valid 分支的集合查询；
- `MarketRepository.FindPublishedByID(ctx, skillID)`：一次集合查询；
- Repository 恢复内部 current Published State 并做逻辑关联、Canonical 与 digest 校验；
- Service 严格映射公开白名单 DTO、能力 key、RFC3339Nano 时间和 cursor；
- HTTP Handler 只处理 Query/Path、request ID、安全错误 Envelope 与 no-store。

`SkillRepository` 可以实现该小接口并复用治理 current snapshot 的完整性校验 helper，但 Market 不能复用含治理 epoch、状态、快照 ID 或完整 Definition 的管理 DTO。完整 Runtime 必须新增独立 `SkillMarketHandler`，由 `RouteHandlers` 作为必填依赖注入并直接 `Register(router)`；不得传 Session/CSRF Middleware。Server 测试必须证明带/不带 Cookie 的 Market 请求均不触发 Auth Resolve。

## 7. 缓存与治理联动

W1-E 所有响应统一：

```http
Cache-Control: no-store
```

不返回 public ETag，不在进程、Redis、CDN、Service Worker 或浏览器存储 Market DTO。治理事务提交后，后续公开请求直接读取 PostgreSQL 当前状态：

```text
active --suspend--> suspended：列表移除，详情 404
suspended --resume--> active：列表和详情重新可见
active/suspended --offline--> offline：永久移除，详情 404
```

未来若增加公开缓存，必须另行冻结 cache key、主动失效 Owner、最大陈旧 SLA、CDN/代理行为、恢复/下线顺序和失败 Evidence；不得把本批 no-store 行为默认为已有缓存协议。

## 8. 错误、安全与日志

| HTTP | Code | 语义 |
| --- | --- | --- |
| 400 | `INVALID_REQUEST` | Query、cursor 或路径 UUID 格式非法 |
| 404 | `SKILL_MARKET_NOT_FOUND` | Skill 不存在、未发布或当前不可公开 |
| 503 | `SKILL_PERSISTENCE_UNAVAILABLE` | 数据库、逻辑关联、Canonical 或摘要不可用 |

所有响应包含本次服务端 UUIDv7 `request_id`。Request ID 生成失败使用 Skill 保留 emergency ID 返回 503。404 不区分不存在、未发布、暂停或下线。

错误 Envelope 固定复用 Business v1：

```json
{
  "error": {
    "code": "SKILL_MARKET_NOT_FOUND",
    "message": "Skill 暂不可用",
    "request_id": "019f0000-0000-7000-8000-000000000103",
    "retryable": false,
    "details": {}
  }
}
```

Market Handler 在生成 request ID 后立即设置 `Cache-Control: no-store`，因此 Query/Path 400、详情 404、持久化 503 和成功响应均不能被缓存。只有持久化不可用错误的 `retryable=true`；格式错误和不可见 404 固定为 false。

安全日志只允许 method、route、skill ID、request ID、result 和稳定 error code；不得记录 Cookie、CSRF、邮箱、完整 Definition、运行指令、guidance、cursor 原文、SQL、DSN 或底层错误。

## 9. 前端路径与状态

- `/skills`：公开列表；
- `/skills/:skill_id`：公开详情，只接受规范小写 UUIDv7；
- `/skill`：继续规范跳转 `/skills`；
- `/my/skills` 与 Builder 继续要求登录并使用 Owner API。

前端新增独立 Market API 与 exact parser，不能复用 Owner parser。Parser 必须拒绝额外字段、重复 ID、未知 capability key、乱序 capability、非法 UUIDv7/时间、null 数组、非法 cover、非法 publisher 和跨字段错配。

列表覆盖 loading、empty、error/retry、page loading 和末页；加载下一页时按 `skill_id` 去重。详情覆盖 loading、error/retry 与 404。非法详情路径不发 API，并展示“Skill 详情路径无效”。

本批不显示可点击“立即使用”。页面固定标记“基础预览”，真实说明搜索、收藏、费用、指标和跨发布者使用尚未开放，同时保留已实现的“我的 Skill”和“创建 Skill”入口；不得把跳到首页 Owner Picker 伪装为可以使用任意 Market Skill。

## 10. Migration 与发布顺序

新增独立前向 Migration `20260714000800_create_skill_market_read`，不得修改 004～007。Migration 只增加公开 newest-first 游标所需索引：

```sql
CREATE INDEX idx_skill_published_snapshot__published_skill_id
    ON business.skill_published_snapshot (published_at DESC, skill_id DESC);
```

索引必须有中文 COMMENT，Down 只删除该索引且禁止 `CASCADE`。真实 PostgreSQL 16 使用至少 1000 条 representative active/published 数据执行 `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)`，验证 dangling guard、active/current LATERAL 条件和 latest keyset：计划必须实际出现 `idx_skill_published_snapshot__published_skill_id`，且任何 Sort 的直接输入不得超过两分支最多 22 条候选。如查询计划无法利用索引或重新出现对完整有效候选集的无界排序，则设计退回评审，不能以“已有索引应该够用”放行。首批仍不增加 JSONB 搜索、分类或标签索引。

发布顺序：

```text
Business Market Domain/Repository/HTTP 双路由上线
→ 匿名 API 与治理联动黑盒通过
→ 前端 Market 列表/详情上线
→ Chromium 匿名浏览与治理联动通过
→ 冻结 Public Market Binding v1
→ 才允许开放“立即使用”
```

回滚 Market Handler 和前端页面不会改变 Skill、Published Snapshot 或治理事实。现有 Owner/Reviewer/Governance/QuickCreate 行为必须完整回归。

## 11. 测试与 Evidence

### 11.1 Domain / Repository / HTTP

- cursor 空值、上限、非法 Base64URL、未知字段、尾随 JSON、时间和 UUIDv7；
- 0/1/20/21/100 条 keyset 分页，稳定同时间排序、无单页重复、SQL 数固定；
- 21 条最大 1 MiB Canonical Definition 的有界资源测试和查询计划检查；全部候选先校验再截断；
- draft-only 不可见，active published 可见；
- Owner 编辑 Draft 后仍返回旧 Published；再次批准发布后只返回新 current Published；
- suspend/offline 后列表移除且详情 404，resume 后重新可见；
- current pointer 悬空、revision/schema/digest/owner 错配均 503；
- Publisher 账户改为 disabled/cancelled 但 Skill 仍 active 时，公开投影继续可见；
- 匿名成功，带或不带登录 Cookie 的公开业务字段完全一致；比较时排除每次独立生成的 `request_id`；
- 未知/重复 Query、非法路径在 Service 前 400；
- Response exact fields，不含 draft、snapshot、digest、revision、governance、review、guidance、email；
- Owner、Reviewer、Governor 与 QuickCreate 鉴权和门禁完整回归。

### 11.2 前端

- list/detail exact parser 正反例和 capability 固定顺序；
- loading、empty、retry、pagination、404、非法路径；
- `/skills/:skill_id` 请求正确，非法 UUIDv7 请求数为零；
- 匿名可访问；登录状态变化不清空公开页或切换到 Owner DTO；
- 无生产 Mock、无 `/api/aigc/**`、无 LocalStorage 权威状态；
- 页面不出现可执行/可使用误导按钮。

### 11.3 真实冒烟

1. Creator 创建并提交 Skill，Reviewer 真实批准；
2. 未登录浏览器在列表和详情看到发布安全投影；
3. Creator 修改 Draft，市场仍显示旧 Published；
4. Governor 暂停后列表消失、详情 404；
5. Governor 恢复后列表和详情重现；
6. Governor 下线后列表消失、详情 404；
7. 真实 HTTP 使用 21 个同发布时间 fixture 跨两页验证 ID 倒序、opaque cursor、无重复与无遗漏，并以非法 cursor 证明 `400 INVALID_REQUEST/no-store`；
8. 由真实 HTTP Driver 让另一用户使用现有 QuickCreate 选择该跨 Owner Skill，断言精确 `PROJECT_SKILL_UNAVAILABLE`、非重试错误和 Project/Binding/Resolution/Outbox 零部分写入；
9. 响应和 Evidence 不含 Cookie、凭据、CSRF、幂等键、治理 ETag、运行指令、guidance、快照 ID 或 digest。

独立 sidecar Evidence 固定为六项闭集：

```text
skill_market_public_read=true
skill_market_safe_projection=true
skill_market_keyset_pagination=true
skill_market_governance_visibility=true
skill_market_cursor_fail_closed=true
skill_market_cross_owner_use_blocked=true
```

它不能修改现有 Foundation v3 的 47 assertions/42 booleans，也不能把 Governance v1 的五项结果复制为市场通过。

## 12. 下一门禁：Public Market Binding v1

只读 Market 通过后，跨发布者“立即使用”至少需要另行关闭：

1. Permission basis 从唯一 `owner_private` 扩展为闭集 `owner_private|public_market`；
2. `subject_user_id/project_owner_user_id` 是消费者，`skill_owner_user_id/publisher_user_id` 是创建者，禁止继续把 Project Owner 硬编码为 Publisher；
3. `policy_ref=project-skill-permission:public-market:v1` 的 Canonical、golden vector、摘要与审计语义；
4. Market 读取到 QuickCreate 之间的 suspend/offline TOCTOU 必须在同一创建事务重新验证并零部分写入；
5. 旧 Session 继续冻结，不能因市场治理或再次发布被静默替换；
6. Agent 继续把 permission digest 当 Business-owned opaque proof，若跨模块 wire 或消费语义变化则必须另行评审；
7. 前端 Market 预选如何进入显式 QuickCreate v2，登录恢复与幂等意图如何保持；
8. “使用”不是收藏或安装，不能创建语义含糊的新表。

在该契约 Frozen 前，本批只读页面不得注册可点击的跨发布者使用入口。

## 13. 评审清单

- [x] 列表和详情只消费 current published + active；
- [x] 公开 DTO 不直接暴露完整 SkillDefinitionV1；
- [x] Publisher 来自 Skill Owner，不是 Reviewer；
- [x] suspended/offline 与不存在对公开详情统一 404；
- [x] 损坏 current pointer 不会被 JOIN 静默隐藏；
- [x] cursor 严格、稳定且未宣称一致快照；
- [x] cursor 只包含公开 Skill ID，不泄露 Published Snapshot ID；
- [x] no-store 足以让本批治理变化在下一请求生效；
- [x] 前端非法详情路径不发请求且无生产 Mock；
- [x] 现有 Owner-only QuickCreate 继续明确阻止跨 Owner；
- [x] 文档没有把公开读取夸大为完整市场或跨发布者使用完成。
