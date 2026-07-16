# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 这个仓库是什么

Dora-Agent 是一个**AIGC(AI 内容创作)智能体**，负责规划并生成故事驱动型短视频。单个 Go HTTP 服务(`cmd/aigc-agent`)承载一个 Eino ADK `ChatModelAgent`(DeepSeek LLM)，驱动多轮创作流程：分析上传素材 → 编写 Final Video Spec → 设计故事板 → 生成媒体资产 → 装配。React/Vite 前端渲染左右分栏 UI(左：故事板，右：聊天)，数据来自基于 SSE 的 A2UI 事件协议。

权威设计文档是 `docs/aigc-chatmodelagent-demo-design.md` —— 做非平凡的后端改动前先读它。注意：该文档提议的包结构(`app/`、`api/`、`persistence/`、`jobs/`…)与实际落地的不一致；真实结构在下文描述的 `internal/aigc/*` 下。**文档与代码冲突时，以代码为准。**

## 常用命令

后端(Go 1.26)：
```bash
go build ./...                              # 编译全部
go test ./...                               # 全部测试
go test ./internal/aigc/server              # 单个包
go test ./internal/aigc/server -run TestName   # 单个测试
go vet ./...
go run ./cmd/aigc-agent                     # 启动 agent 服务(需要 Postgres+Redis+环境变量)
```

本地基础设施 + 运行：
```bash
docker compose -f docker-compose.local.yml up -d   # Postgres 16 (:5432) + Redis 7 (:6379)
# 配置 .env.local 或 .env(启动时经 godotenv 自动加载)：至少要有 DeepSeek key ——
# DORA_DEEPSEEK_API_KEY，或旧名 DEEPSEEK_API_KEY(main.go 会自动回退映射)。provider/TOS 密钥可选。
# 注意：.env.local 里若残留旧的 DB/Redis 变量(如指向 doraigc 用户)会覆盖 config 的 dora 默认值，需清理。
```

前端(`frontend/`，Vite + React 19 + Vitest)：
```bash
npm install
npm run dev        # dev 服务在 :3200，把 /api 代理到 http://localhost:19080
npm run build
npm run test       # vitest run
```

Kitex 代码生成(`kitex-all.sh`)只是脚手架 —— 它引用的 `api/thrift/business_agent_service.thrift` 尚不存在。真正运行的服务是 HTTP/Gin 的 AIGC agent，而非 Kitex RPC 服务。

## 测试约定

依赖 DB/Redis 的测试在本地基础设施不可用时会**跳过(skip)而非失败** —— 它们调用 `aigcstorage.OpenAgentPostgres`/`PingRedis`，连接出错就 `t.Skipf`。要真正跑到这些测试，先起 `docker-compose.local.yml`。项目没有用 testcontainers；测试直接连本地真实 Postgres/Redis，用的是 `config.LoadFromEnv()` 的同一套环境默认值(`dora`/`dora_local_password`)。

## 架构

### 分层
核心纪律(源自设计文档，并在代码中落实)：**Eino ADK 负责智能体决策与 interrupt/resume；业务层负责持久化与媒体编排。** 不要在业务层重新实现 runner/工具调用的行为。

- `ChatModelAgent` 决定*下一步做什么*(回复、调用工具、请求确认)。
- **Skill**(DB 存储，从 Skill.md 解析)提供*阶段语义与依赖关系* —— 它是 agent 的上下文，不是执行器。
- **Tool Registry** 提供*有哪些工具及其 schema*。
- **Compose Graph** 只承载有真实依赖的工具内部业务流程(媒体生成)，绝不承载对话路由。
- **A2UI** 纯粹是 UI 协议适配层(SSE JSONL) —— 它不是可调用的 tool。

### 装配
`cmd/aigc-agent/main.go` 是组装根(composition root)：打开 Postgres、对每个 store 执行 `AutoMigrate`、连接两个 Redis 客户端、构建 media graph + generation dispatcher/worker、装配带中间件链的 DeepSeek runner，并挂载 Gin router。一切通过接口做依赖注入(见 `server.Config`)，测试可自由替换成 fake。

### 关键包(`internal/aigc/`)
- `agent/` —— `NewDeepSeekRunner` 构建 `ChatModelAgent` + 中间件链(patchtoolcalls、tool-exception、skill、reduction、summarization、turn-context 注入)。`message_rebuilder.go` 每轮从持久化记录重建合法的 `[]*schema.Message` 链(tool-call 与 tool-result 的配对是关键不变量)。
- `tools/` —— 业务工具(`text_editor`、`storyboard_designer`、`media_generator`、`write_prompt`，以及 provider 工具 `image2`/`seedance`)、`Registry`，还有 `ToolInvocationEnvelope`/`ToolResultEnvelope` + `JSONPatchOp` 类型。provider 工具**绝不能**把 provider 原始负载(b64/data URL/长日志)返回给 agent —— 只返回业务摘要 + asset ID。
- `mediagraph/` —— `media_generator` 的 Compose Graph(资产注册 → 编写提示词 → 派发任务 → 同步故事板 → 参考图确认 interrupt)，编译为带 checkpoint store 的 DAG。
- `generation/` —— 异步媒体流水线：`Dispatcher` → Redis list `Queue` → `Worker`(并发 4)→ provider `handlers/`(image2、seedance、demo audio)。Worker 写入资产、patch 故事板并发布事件。
- `session/`、`spec/`、`storyboard/`、`skill/`、`asset/` —— 各自领域的 Postgres/GORM store。`storyboard/` 用**乐观锁**：patch 带 `base_version`，版本不一致返回 `ErrVersionConflict`(HTTP 409)。`skill/parser.go` 把 `<name>/<description>/<planner>` 形式的 Skill.md 解析为 `SkillPlan`。
- `server/` —— Gin router(所有路由在 `/api/aigc/*` 下)、SSE 流式输出，以及 agent invoker/wakeup 桥接。`router.go` 是最大的文件，也是主要 API 面。
- `a2ui/` —— SSE 事件 envelope + 内存 pub/sub broker。`turncontext/` —— 在每次模型调用前把当前 session 状态(spec/storyboard/asset 摘要)作为**瞬态(transient)**上下文注入(不持久化、不进入 summarization)。
- `storage/` —— Postgres/Redis 连接助手 + Redis 版 checkpoint store。
- `patch/` —— 用于故事板变更的 JSON Patch 应用。

### Interrupt / resume 与 checkpoint
确认卡点用 `compose.Interrupt`。存在两种 checkpoint 作用域 —— **runner**(普通工具 interrupt，通过 agent invoker 恢复)与 **media_graph**(graph 内部 interrupt，通过 `/media-graph/resume` 恢复)。`server/router.go` 把 `interrupt_id` 映射到 `CheckpointMapping`，并强制**幂等、一次性 resume**(已 resume 的 checkpoint 返回同一结果)。resume 负载携带 `spec_version`/`storyboard_version` 用于冲突检测。

### 异步 job 回流
媒体生成会超出单次 agent run 的生命周期。Worker 完成任务后发出 `JobWakeupEvent`，把结果推回 session，让 agent 在下一轮看到(`server/wakeup.go`)。job 回调按 `job_id + status_version` 做幂等。

### 存储切分
两个逻辑 Postgres 库：`dora_agent`(agent 运行时 —— sessions、messages、skills、specs、storyboards、assets、generation jobs、checkpoints)和 `dora_business`。两个 Redis 角色：**generation** 客户端(任务队列)与 **runtime** 客户端(checkpoint store + 缓存)，可独立配置。媒体字节存到火山引擎 **TOS** 对象存储；只有摘要/asset ID 进入 agent 上下文。

## 配置

所有配置都通过环境变量经 `internal/aigc/config` 加载(`LoadFromEnv().Normalize()`)。`DORA_DEEPSEEK_API_KEY` 是唯一硬性要求；provider 密钥(`DORA_IMAGE2_API_KEY`、`DORA_SEEDANCE_API_KEY`)可选，且决定对应 generation handler 是否注册。服务监听 `AGENT_HTTP_ADDR`(默认 `:18080`)。完整变量集见 `.env.example`(DeepSeek、providers、两个 DB URL、两套 Redis 配置、TOS)。`config.Sanitized()` 只返回凭据*状态*而不泄露密钥 —— 任何暴露配置的接口/日志都用它。

## 约定

- 错误用哨兵 `errors.New` 值并以 `errors.Is` 比较(如 `ErrVersionConflict`、`ErrToolNotFound`、`ErrCheckpointNotFound`)；router 把它们映射为 HTTP 状态码。
- store 在消费方定义为窄接口(`server.Config`、agent config)，由具体的 Postgres 实现满足 —— 新依赖优先接口化。
- `docs/superpowers/plans/` 下的计划按任务逐条以 TDD 执行(先写失败测试，再实现)；后端功能沿用这一节奏。
