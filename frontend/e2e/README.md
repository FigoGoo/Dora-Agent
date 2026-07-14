# W0 浏览器冒烟

`npm run test:e2e:w0` 使用真实 Chromium 依次验证：页面登录、Quick Create、进入正式
`/projects/:project_id/workspace`、Snapshot 与 SSE 就绪、首个 Prompt 可见、硬刷新后恢复同一
Session，以及规范但不存在的 Project 返回不可访问。测试还会审计浏览器请求，保证没有调用
`POST /api/aigc/sessions`、其他历史 Demo API 或跨 Origin 直连 Agent；最后退出登录并验证
受保护工作台由 401 会话门禁拦截，且不会继续请求 Agent 工作台接口。

运行前需保证 Business、Agent 和依赖基础设施已经可用，并准备一个由本地 Seeder 创建的测试账号。
账号与密码只从进程环境读取，不应写入前端文件、Playwright 配置或测试报告：

```sh
cd frontend
DORA_E2E_USER_EMAIL="$DORA_SMOKE_USER_EMAIL" \
DORA_E2E_USER_PASSWORD="$DORA_SMOKE_USER_PASSWORD" \
npm run test:e2e:w0
```

可选配置：

- `DORA_E2E_BASE_URL`：前端地址，默认 `http://127.0.0.1:3200`。
- `DORA_E2E_BUSINESS_API_TARGET`：Vite `/api` 代理目标，默认 `http://127.0.0.1:18081`。
- `DORA_E2E_PROMPT`：本次创建使用的 Prompt；不设置时自动生成非敏感值。
- `DORA_E2E_EXTERNAL_SERVER=1`：不由 Playwright 启动 Vite，直接测试 `DORA_E2E_BASE_URL` 指向的现有前端。

首次安装依赖后若本机尚无 Playwright Chromium，可执行 `npx playwright install chromium`。
