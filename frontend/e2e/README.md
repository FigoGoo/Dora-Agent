# W0 浏览器冒烟

`npm run test:e2e:w0` 使用真实 Chromium 依次验证：页面登录、Quick Create、进入正式
`/projects/:project_id/workspace`、Snapshot 与 SSE 就绪、首个 Prompt 可见、硬刷新后恢复同一
Session，以及规范但不存在的 Project 返回不可访问。测试随后用 Chromium 原生离线控制命中
真实 SSE 断线，在保留已验证 Snapshot 的同时观察 `reconnecting`，恢复网络后回到同一 Session；
最后切换到第二个真实用户，验证首用户工作台在 Project 层返回 Owner-safe `404`，且不会继续请求
Agent Workspace/Events/Tools。全程还会审计浏览器请求，保证没有调用 `POST /api/aigc/sessions`、
其他历史 Demo API 或跨 Origin 直连 Agent。

运行前需保证 Business、Agent 和依赖基础设施已经可用，并准备两个相互隔离、由本地 Seeder 创建的
测试账号。账号、密码和 0600 结构化结果路径只从进程环境读取，不应写入前端文件、Playwright
配置或测试报告：

```sh
cd frontend
DORA_E2E_USER_EMAIL="$DORA_SMOKE_USER_EMAIL" \
DORA_E2E_USER_PASSWORD="$DORA_SMOKE_USER_PASSWORD" \
DORA_E2E_OWNER_B_EMAIL="$DORA_SMOKE_OWNER_B_EMAIL" \
DORA_E2E_OWNER_B_PASSWORD="$DORA_SMOKE_OWNER_B_PASSWORD" \
DORA_E2E_W05_RESULT_PATH="$(mktemp)" \
npm run test:e2e:w0
```

上述五项缺少任一项时用例会标记为 skipped；canonical `w05-browser-smoke` 还会要求结果文件存在、
精确符合 `w05.workspace-browser.smoke.result.v1`，并逐值校验断线命中、同 Session 恢复、跨 Owner
404、Agent 请求阻断和资源事实不泄漏，因而 skipped 运行不能发布 passed Evidence。

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
测试不拦截请求，也不会输出凭据、Cookie、CSRF 或原始 Idempotency-Key。

命令在启动 Playwright 前会校验以下四项环境变量，任何一项缺失都会直接失败：

```sh
cd frontend
DORA_E2E_USER_EMAIL="$DORA_SMOKE_USER_EMAIL" \
DORA_E2E_USER_PASSWORD="$DORA_SMOKE_USER_PASSWORD" \
DORA_E2E_REVIEWER_EMAIL="$DORA_SMOKE_REVIEWER_EMAIL" \
DORA_E2E_REVIEWER_PASSWORD="$DORA_SMOKE_REVIEWER_PASSWORD" \
npm run test:e2e:w1-real-review
```
