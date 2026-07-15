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

## W1 真实 Reviewer / Governor 链

`npm run test:e2e:w1-real-review` 只运行独立 `@w1-real-review` 用例。它在同一个 browser
context 中依次执行 Creator 创建并提交 sentinel A、保存 sentinel B 新草稿、Reviewer 从管理队列
批准发布，再由 Creator 选择已发布 Skill 发起 `project_quick_create.v2` 并进入正式 Workspace。随后
独立 Governor 从正式治理列表和详情找到该 Published Skill，验证 Definition 只读与 current published
指针仍指向 sentinel A，再携带真实 CSRF、Strong `If-Match` 和内存 Idempotency-Key 依次执行暂停、
恢复、永久下架，逐次校验响应 Body/Header ETag、`governance_status`、epoch、`allowed_actions` 和
offline 终态按钮。

Creator 和纯 Reviewer 都会直达治理路由并显式请求治理 API，证明页面失败关闭、隐式 API 增量为零、
正式 API 返回严格 `403 SKILL_GOVERNANCE_CAPABILITY_REQUIRED`，且三个拒绝 request ID 都能关联
Business 结构化授权审计。Governor 只拥有 `skill.govern`，不会获得 Reviewer 导航或权限；shell
coordinator 在 `0600` checkpoint 后通过正式 Role Admin CLI 撤销 Governor 分配，浏览器复用同一
Cookie 再次 Resolve 后必须得到空角色/capability，并在治理详情 API 上收到严格 403。整条浏览器链
不访问 `/api/aigc/**`，也不拦截业务请求。

Creator 的 Reviewer capability 负向链仍只是 `SMK-001A` 子集；Reviewer 对 Creator 草稿的 Owner
GET/PUT 防枚举 404 只加强 W1-C1 跨用户隔离。Governor 页面和 Chromium 子切片完成不代表在线
角色管理、完整管理员数据范围、申诉、Tool/Graph Tool 治理或完整 `SMK-031` 已完成。

Driver 启动前会校验 Creator、Reviewer、Governor 共六项邮箱/密码环境变量，任一缺失都直接失败。
完整用例还依赖 canonical shell 创建的 `0700` Governance 控制目录、原子 `0600` checkpoint/ACK
和结果路径；不要脱离 coordinator 单独运行 npm 命令。仓库根目录的标准入口是：

```sh
make GO=/Users/figo/sdk/go1.26.3/bin/go W0_ENV_FILE=.env.example w1-smoke
```

canonical 会选择隔离的 loopback Vite 端口并从当前 worktree 启动前端；只有显式设置
`DORA_E2E_REUSE_EXISTING_SERVER=1` 才允许复用已有服务。Browser Result 固定为
`w1.real-review-result.v6`，只保存安全资源 ID、拒绝 request ID 和布尔结论；Evidence 与 Playwright
产物不得包含邮箱、密码、Cookie、CSRF、原始 ETag、Definition、reason、approval reference 或原始
Idempotency-Key。
