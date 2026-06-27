# M5 公开触达与通知技术基线报告

状态：已通过 `scripts/validate-m5.sh`
日期：2026-06-28
范围：业务 12/13，Agent 06/10 中公开展示安全证据、快照/资产引用不泄密子集；不含前端、部署上线文档、推荐算法、榜单、评论、收藏、关注、二次创作。

## 已执行验证

- `go test ./services/business/internal/application/work ./services/business/internal/transport/http`：已执行通过，覆盖 M5 作品分享、下架通知状态和 HTTP 闭环回归。
- `python3 tests/contract/validate_fixtures.py`：已执行通过，覆盖 M5 business-api fixture JSON 结构和全局 HTTP 状态场景。
- `scripts/validate-m5.sh`：已执行通过，串行执行 `scripts/validate-m4.sh`、gofmt dry check、`go test -count=1 ./...`、SQL up/down 配对、M5 OpenAPI/Gin route parity、M5 语义扫描、AG-UI fixture、contract fixture 和无外键扫描。
- `scripts/validate-m0.sh`、`scripts/validate-m1.sh`、`scripts/validate-m2.sh`、`scripts/validate-m3.sh`、`scripts/validate-m4.sh`：已由 `scripts/validate-m5.sh` 串行执行通过。
- `rg -n "FOREIGN KEY|REFERENCES" db/migrations api code-plan services`：已由 `scripts/validate-m5.sh` 执行，通过。

## 验收覆盖

- OpenAPI：作品分享和后台下架已从旧单步接口切到 preview/confirm；补齐 `ShareWorkPreviewDTO`、`ConfirmShareWorkRequest`、`TakeDownPublicWorkPreviewDTO`、通知 `type/summary/body/navigation_hint/read_at` 等字段。
- Migration/seed：通过 0018 增量迁移对齐 `works`、`work_public_snapshots`、`work_likes`、`notifications`、`notification_create_failures` 等 M5 字段；无数据库级外键。
- 业务应用：新增 work/notification application，支持作品创建/编辑/列表/详情、分享 preview/confirm、取消分享、公开列表/详情/点赞/取消点赞、后台公开作品列表、下架 preview/confirm、通知列表/未读数/read/read-all/navigation。
- 安全证据：分享只接受 `scene=work_share`、`target_type=work_share_text`、`result=passed` 且 digest 匹配公开标题/简介/标签摘要的证据。
- 通知：Skill 审核通过/拒绝、作品下架通知接入 `NotificationService`；通知失败不回滚下架事实，写入补偿记录；通知跳转前重新校验业务资源权限。
- 幂等与审计：M5 POST/PATCH 写操作均由 OpenAPI 和 Gin 路由要求 `Idempotency-Key`，请求体要求 `request_hash`；业务写入路径接入幂等 guard 和审计记录。
- Agent 边界：Agent 未持久化公开作品状态、通知主数据、公开快照状态、私有 object key、长期 URL、Prompt 原文、积分或模型成本；M5 不新增 AG-UI 事件，继续复用 canonical 事件。
- Fixtures：business-api fixture 覆盖 M5 success、permission、business error、idempotency conflict、timeout、version compatibility 场景。

## 未执行项

M5 范围内无未执行项。前端、部署上线文档、推荐/榜单/评论/收藏/关注/二次创作是本阶段范围外内容，未纳入本报告。

## 阻塞问题

无。
