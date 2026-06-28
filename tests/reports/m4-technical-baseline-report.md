# M4 积分资产闭环技术基线报告

状态：已通过 `scripts/validate-m4.sh`
日期：2026-06-28
范围：业务 09/10/11、Agent 09/10/13、Agent 07 中 Credit / Asset / AssetCreditCommit / 资产写入权限 RPC 子集；不含前端、部署上线文档、M5 作品中心/精选作品、M6 通知与服务级总验收。

## 已执行验证

- `go test -count=1 ./...`：已执行通过，覆盖 Agent/Business 当前 Go 包和本地 Testcontainers 集成测试。
- `scripts/validate-m0.sh`：已由 `scripts/validate-m4.sh` 串行执行通过。
- `scripts/validate-m1.sh`：已由 `scripts/validate-m4.sh` 串行执行通过。
- `scripts/validate-m2.sh`：已由 `scripts/validate-m4.sh` 串行执行通过。
- `scripts/validate-m3.sh`：已由 `scripts/validate-m4.sh` 串行执行通过。
- `scripts/validate-m4.sh`：已执行通过，覆盖 Go toolchain、gofmt dry check、全量 Go 测试、SQL up/down 配对、OpenAPI 路由、上传安全证据契约、M4 RPC/HTTP/Agent 语义门禁、AG-UI fixture、RPC fixture、元素类型和无外键扫描。
- `services/business/internal/application/credit`：已执行通过，覆盖兑换码 `account_type`、`bind_target_type`、`credit_expires_at` 持久化、幂等 request hash 冲突和目标账户类型不匹配不消耗兑换码。
- `services/business/internal/application/assetcommit`：已执行通过，覆盖生成资产成功提交后 `assets`、`asset_storage_objects`、`project_assets`、`asset_commit_items`、积分 ledger、freeze/account 状态落库。
- `rg -n "FOREIGN KEY|REFERENCES" db/migrations api code-plan services internal`：已由 `scripts/validate-m4.sh` 执行，通过。

## 验收覆盖

- 业务 RPC：`EstimateGenerationCredits`、`EstimateToolCredits`、`FreezeCredits`、`ChargeToolUsageCredits`、`ReleaseFrozenCredits`、`BatchCheckAssetAccess`、`PrepareGeneratedAssetObjects`、`CommitGeneratedAssetAndCharge` 已接入业务 application，不再作为 M4 范围内的未实现能力。
- 业务 HTTP：`/api/credits/**`、`/api/enterprise/credits`、`/api/enterprise/usage`、`/api/admin/credits/**`、`/api/assets/**`、`/api/asset-element-types`、`/api/admin/asset-element-types` 按 OpenAPI 路由族接入。
- Agent 闭环：创建 run 前批量校验资产引用；安全证据 passed 后估算积分；积分不足写 `credits.insufficient` 并失败；确认 accepted 后冻结积分、本地 adapter 生成 artifact、准备业务对象槽、通过 streaming uploader 写入对象槽、提交资产并扣费，失败路径释放冻结。
- TOS/对象校验：Agent 侧使用流式上传，不再伪造 `local-*` etag；业务侧 `CommitGeneratedAssetAndCharge` 在保存和扣费前通过对象 verifier 校验 bucket/object key/content type/size/checksum/etag，生产配置接入 Volcengine TOS SDK `HeadObjectV2`。
- Commit 绑定：提交资产时强制校验 `estimate_item_id` 属于当前 freeze 对应 estimate，且 item 类型必须为生成类；多产物按 artifact/resource type 分配不同 estimate item，不复用首个 item。
- 安全与确认：上传意图要求完整 `SafetyEvidence`，不再接受 `safety_evidence_id` 伪造；积分估算和资产提交校验 safety digest、trace/session/run 绑定；人工确认校验 stored confirmation payload digest。
- 幂等重放：`ReleaseFrozenCredits` replay 返回原始释放结果，不再返回占位 `ReleasedPoints=0`。
- 兑换码：`CreateRedeemCodesRequest` 已按 code-plan 使用 `count`、`points`、`code_expires_at`、`credit_expires_at`、`account_type`、`bind_target_type`、`bind_target_id`、`channel`、`reason`；领域模型不再把 `account_type` 混入 `target_type`，兑换目标不匹配返回 `REDEEM_CODE_TARGET_MISMATCH` 且不消耗 code。
- AG-UI：运行时写入 `credits.estimated/frozen/charged/released/insufficient`、`generation.artifact.completed`、`asset.save.started/completed/failed`、`workspace.assets.updated`、`process.snapshot.saved`、`agent.run.completed/failed/cancelled` 等 schema canonical 事件。
- 边界：Agent 只保存 runtime、artifact ref 和 business asset ref；业务服务保存积分账户、资产、storage object、项目资产绑定、credit ledger、commit batch/item。
- 元素类型：seed 和 contract fixture 已对齐 14 个内置类型：`short_text`、`long_text`、`rich_text`、`structured_object`、`list`、`image_ref`、`audio_ref`、`video_ref`、`file_ref`、`prompt`、`storyboard`、`timeline`、`tag_group`、`parameter_group`。

## 未执行项

无。M4 范围外的前端、部署上线文档、M5 作品中心/精选作品、M6 通知与服务级总验收未纳入本报告。

## 阻塞问题

无。
