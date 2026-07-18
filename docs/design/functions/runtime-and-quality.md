# 运行、恢复与质量设计

> 状态：Current / 快速 MVP 与完整门禁分离
>
> 各门禁当前是否通过只见[交付阶段与当前状态](../../requirements/delivery-status.md)。

## 1. 目标

运行设计为开发者提供一条快速、可重复、不会冒充生产验收的反馈路径。所有执行状态以代码、Migration 和当次测试结果为准；文档不保存具体历史 Run ID 或文件 Hash。

## 2. 本地运行层级

| 层级 | 命令 | 证明什么 | 不证明什么 |
| --- | --- | --- | --- |
| 静态契约 | `make test-smoke-contracts` | 脚本、Profile、文档口径和 isolated smoke 契约 | 服务真实启动 |
| 快速主链 | `make trial-basic` | 三 Module、六 Tool、Worker、PNG/MP4、Workspace V5 happy path | 重启、Fence、故障注入、生产 Provider |
| Module 全量 | `make test` | 三个 Go Module 单元/集成/契约测试 | Race、前端、真实浏览器 |
| 静态与构建 | `make vet build` | 三 Module vet/build | 运行时行为 |
| 前端 | `make check-frontend` | 前端测试和生产构建 | 后端及浏览器联调 |
| 专项 | `make race`、isolated smoke | 并发或单能力恢复契约 | 完整生产发布 |

任何一层通过都不能改写成另一层通过。

## 3. `trial-basic`

`trial-basic` 使用专用测试数据库和 local-only 配置：

1. 校验脚本契约和工具依赖；
2. 重置 Business、Agent、Worker 专用 Schema；
3. 启动 Business、Agent、Worker、Vite；
4. 运行真实 Chromium；
5. 验证登录、Project、文本素材、六 Tool、媒体文件、Range 和刷新恢复；
6. 有界停止自身启动的进程；
7. 仅在全部成功后原子发布 `.local/smoke/trial-basic.json`，权限 0600。

Evidence 只保存安全 ID、摘要、计数和布尔断言，不保存 Cookie、密码、DSN、Secret、原始 Prompt、完整 Tool Payload 或媒体内容。

## 4. standalone Profile

以下命令继续作为单能力回归，不被 `trial-basic` 串行吸收：

- `plan-spec-preview-smoke`
- `user-message-runtime-smoke`
- `analyze-materials-runtime-smoke`
- `plan-storyboard-runtime-smoke`
- `write-prompts-runtime-smoke`

它们的 Feature Flag 互斥，用来验证各自的 Receipt、恢复和失败关闭规则。统一 Profile 证明组合主链，两类 Evidence 不能互相替代。

## 5. 启动与停止

服务启动遵循：配置校验 → PostgreSQL/Redis/etcd → Schema/Readiness → Graph Compile → Registry/Processor → HTTP/RPC → etcd 注册。

停止遵循：摘除 Readiness/停止 Intake → 停止 Claim → 在预算内 Drain 并保持 Heartbeat → 取消剩余 Context → 关闭传输和基础设施。

Coordinator 或 Worker 的通知可以丢失，但 PostgreSQL 轮询必须继续推进 backlog。

## 6. 错误与恢复

- 参数、身份、版本、摘要和未知枚举失败关闭。
- Model Candidate 非法时不进入 Command。
- RPC/HTTP Unknown Outcome 查询原 Receipt。
- Job Retry 只有 Worker 一个 Owner；Graph 和 Agent 不重复 Provider Retry。
- Lease/Fence 不匹配立即停止陈旧执行。
- 投影失败重放冻结输出，不重新调用模型或 Tool。
- 终态 Event 必须能从 Workspace Snapshot 恢复，不能只依赖 SSE 在线到达。

## 7. 数据与安全门禁

- `.env.example` 只提供 local 示例；真实 Secret 不进入 Git。
- 日志和 Trace 不保存完整 Prompt、Tool Payload、Checkpoint、Reasoning 或 Provider 原文。
- Migration 由 Owner Module 管理，Runtime 不执行 AutoMigrate。
- CI 检查无物理外键、中文 COMMENT、IDL/生成代码一致、无 N+1 和跨 Module internal import。
- 生产环境必须使用独立账号、TLS、最小权限、正式服务身份和私有对象存储。

## 8. 完整质量门禁

以下生产化门禁必须分别执行并记录 Evidence，不能用快速主链结果替代：

- 进程重启、Lease takeover、Fence 和 lost-wake；
- RPC/Finalize/Terminal 各阶段故障注入；
- Worker 崩溃时的执行/Finalize 去重；
- 真实模型/Provider 评测、费用上限和内容安全；
- 正式 Approval、计费、退款/对账策略；
- 压力、容量、监控、备份和灾难恢复。

## 9. 文档守卫

`make test-document-single-source` 必须验证：

- `docs/README.md` 是唯一设计入口；
- `docs/requirements/delivery-status.md` 是唯一阶段状态源；
- 六个 Graph Tool 设计路径和必需章节完整；
- 已删除的历史总览与里程碑文档不会回生；
- 快速主链与完整质量门禁始终分离；
- AGENTS、项目 Skill、README 和 Agent 规范不再引用旧文档路径。
