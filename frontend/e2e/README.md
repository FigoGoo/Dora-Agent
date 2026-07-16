# W0 浏览器冒烟

`npm run test:e2e:w0` 使用真实 Chromium 依次验证：页面登录、Quick Create、进入正式
`/projects/:project_id/workspace`、Snapshot 与 SSE 就绪、首个 Prompt 可见、硬刷新后恢复同一
Session。canonical 编排会在活动 SSE 停留于 Cursor 2 时，用本地 PostgreSQL 原子故障注入追加并
保留 seq 3、删除旧事件并推进 `min_available_seq=3`；测试通过 Chromium CDP 逐帧验证无 `id` 的
`stream.reset(cursor_expired)`，观察页面保留最后可信投影、完整回源 Snapshot，再从 Cursor 3 收到
`stream.ready` 并恢复同一 Session。测试随后用 Chromium 原生离线控制命中
真实 SSE 断线，在保留已验证 Snapshot 的同时观察 `reconnecting`，恢复网络后回到同一 Session；
最后切换到第二个真实用户，验证首用户工作台在 Project 层返回 Owner-safe `404`，且不会继续请求
Agent Workspace/Events/Tools。全程还会审计浏览器请求，保证没有调用 `POST /api/aigc/sessions`、
其他历史 Demo API 或跨 Origin 直连 Agent。

完整用例应由仓库根目录的 canonical 编排启动；它负责当前 worktree 二进制、两个隔离账号、
`0700` Retention 握手目录、PostgreSQL 故障注入器、0600 结果文件和脱敏扫描：

```sh
make GO=/Users/figo/sdk/go1.26.3/bin/go W0_ENV_FILE=.env.example w05-browser-smoke
```

Browser Driver 仍只从进程环境读取账号、密码、0600 结果路径与 `0700`
`DORA_E2E_W05_RETENTION_CONTROL_DIR`，不把它们写入前端配置或报告。任一必需环境缺失时用例会
标记为 skipped；canonical `w05-browser-smoke` 还要求结果精确符合
`w05.workspace-browser.smoke.result.v2`，逐值校验 Retention Reset/完整回源、受控断线恢复、跨
Owner 404、Agent 请求阻断和资源事实不泄漏，因而 skipped 运行不能发布 passed Evidence。

可选配置：

- `DORA_E2E_BASE_URL`：前端地址，默认 `http://127.0.0.1:3200`。
- `DORA_E2E_BUSINESS_API_TARGET`：Vite `/api` 代理目标，默认 `http://127.0.0.1:18081`。
- `DORA_E2E_PROMPT`：本次创建使用的 Prompt；不设置时自动生成非敏感值。
- `DORA_E2E_EXTERNAL_SERVER=1`：不由 Playwright 启动 Vite，直接测试 `DORA_E2E_BASE_URL` 指向的现有前端。

首次安装依赖后若本机尚无 Playwright Chromium，可执行 `npx playwright install chromium`。

## W1 真实 Reviewer 发布链

`npm run test:e2e:w1-real-review` 只运行独立 `@w1-real-review` 用例。它在同一个 browser
context 中依次执行 Creator 创建并提交 sentinel A、保存 sentinel B 新草稿、Reviewer 从管理队列
批准发布，再由 Creator 选择已发布 Skill 发起 `project_quick_create.v2` 并进入正式 Workspace。
Creator 登录后还会先直达 Reviewer 管理路由，证明前端显示稳定的无权限状态且不会隐式请求审核
队列；随后复用同一 Chromium Cookie 显式请求正式审核 API，严格验证
`403 SKILL_REVIEW_CAPABILITY_REQUIRED`、`Cache-Control: no-store`、错误 Envelope 和无敏感
信息泄漏，并用同一 request ID 关联 Business 结构化拒绝审计。Reviewer 登录后也会直达 Creator
草稿编辑路由，并分别用正式 Owner GET 和带合法 CSRF、ETag、Definition 的 PUT 证明跨 Owner
读取/修改均返回防枚举 `404 SKILL_NOT_FOUND`，页面不渲染编辑表单且不泄漏当前草稿事实；Creator
随后用正式 Owner GET 证明草稿 ETag 与 Definition 均未变化。前一条
capability 负向链是
`SMK-001A` Reviewer capability isolation 子集，不代表完整管理员角色、数据范围、敏感字段或导出
RBAC 已完成；后一条只加强 W1-C1 Owner 资源跨用户隔离，也不表示 Reviewer 不能创建自己的 Skill。
测试不拦截请求，也不会输出凭据、Cookie、CSRF、ETag、Definition 或原始 Idempotency-Key。

命令在启动 Playwright 前会校验以下四项环境变量，任何一项缺失都会直接失败：

```sh
cd frontend
DORA_E2E_USER_EMAIL="$DORA_SMOKE_USER_EMAIL" \
DORA_E2E_USER_PASSWORD="$DORA_SMOKE_USER_PASSWORD" \
DORA_E2E_REVIEWER_EMAIL="$DORA_SMOKE_REVIEWER_EMAIL" \
DORA_E2E_REVIEWER_PASSWORD="$DORA_SMOKE_REVIEWER_PASSWORD" \
npm run test:e2e:w1-real-review
```
