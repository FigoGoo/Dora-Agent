# M5 公开触达与通知技术基线报告

状态：已通过 `scripts/validate-work-marketplace-flow.sh`
日期：2026-06-28
范围：业务 12/13，Agent 06/10 中公开展示安全证据、快照/资产引用不泄密子集；不含前端、部署上线文档、推荐算法、榜单、评论、收藏、关注、二次创作。

## 已执行验证

- `go test -count=1 ./services/business/internal/application/accountspace ./services/business/internal/application/work ./services/business/internal/application/notification ./services/business/internal/transport/http`：已执行通过，覆盖企业 token 移除失效、M5 作品分享/取消分享/点赞/下架 replay、taken_down 作品空 PATCH/同值 PATCH 不可重置、真实编辑后重置 private、通知 read/read-all/navigation 和 HTTP 响应 schema 回归。
- `python3 tests/contract/validate_fixtures.py`：已执行通过，覆盖 M5 business-api fixture JSON 结构和全局 HTTP 状态场景。
- `scripts/validate-work-marketplace-flow.sh`：已执行通过，串行执行 `scripts/validate-tool-generation-flow.sh`、gofmt dry check、`go test -count=1 ./...`、SQL up/down 配对、M5 OpenAPI/Gin route parity、M5 语义扫描、AG-UI fixture、contract fixture 和无外键扫描。
- `scripts/validate-toolchain-contract-baseline.sh`、`scripts/validate-engineering-baseline.sh`、`scripts/validate-account-agent-http.sh`、`scripts/validate-catalog-skill-runtime.sh`、`scripts/validate-tool-generation-flow.sh`：已由 `scripts/validate-work-marketplace-flow.sh` 串行执行通过。
- `rg -n "FOREIGN KEY|REFERENCES" db/migrations api code-plan services`：已由 `scripts/validate-work-marketplace-flow.sh` 执行，通过。

## 验收覆盖

- OpenAPI：作品分享和后台下架已从旧单步接口切到 preview/confirm；`PreviewShareWorkRequest` 使用 `public_title/public_description/tags/safety_evidence`，`ConfirmShareWorkRequest` 只接收 `preview_token`；通知 DTO 对齐实现的 `title/summary/body/related_resource_*/navigation_hint/read_at` 和 `items/limit/offset/total` 分页结构；`NotificationNavigationDTO` 使用 `target_route/target_resource_id`，不暴露旧 `target_id`。
- Migration/seed：通过 0018 增量迁移对齐 `works`、`work_public_snapshots`、`work_likes`、`notifications`、`notification_create_failures` 等 M5 字段；无数据库级外键。
- 业务应用：新增 work/notification application，支持作品创建/编辑/列表/详情、分享 preview/confirm、取消分享、公开列表/详情/点赞/取消点赞、后台公开作品列表、下架 preview/confirm、通知列表/未读数/read/read-all/navigation；taken_down 作品只有标题、简介、资产、封面、分类或标签发生真实变化时才会重置为 private。
- 安全证据：分享只接受 `scene=work_share`、`target_type=work_share_text`、`result=passed` 且 digest 匹配公开标题/简介/标签摘要的证据。
- 通知：Skill 审核通过/拒绝、作品下架通知接入 `NotificationService`；通知失败不回滚下架事实，写入补偿记录；通知跳转前重新校验业务资源权限和企业 active member 状态。
- 幂等与审计：M5 持久化写操作接入幂等 guard 和审计记录；分享/下架 preview 不持久化业务事实，不要求 `Idempotency-Key` 或客户端 `request_hash`；confirm 保留 `Idempotency-Key`，由业务逻辑按 preview token、租户、space、actor 生成幂等冲突指纹。`UnshareWork`、`ConfirmTakeDownWork`、点赞/取消点赞、`MarkAllNotificationsRead` 已补 replay 分支。
- Agent 边界：Agent 未持久化公开作品状态、通知主数据、公开快照状态、私有 object key、长期 URL、Prompt 原文、积分或模型成本；M5 不新增 AG-UI 事件，继续复用 canonical 事件。
- Fixtures：business-api fixture 覆盖 M5 success、permission、business error、idempotency conflict、timeout、version compatibility 场景；`scripts/validate-work-marketplace-flow.sh` 已新增 fixture 字段语义门禁，防止 preview/confirm、通知 DTO 和 navigation 目标字段再次漂移。

## 未执行项

M5 范围内无未执行项。前端、部署上线文档、推荐/榜单/评论/收藏/关注/二次创作是本阶段范围外内容，未纳入本报告。

## 阻塞问题

无。
