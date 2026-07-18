# Dora Agent

Dora Agent 是面向桌面 Web 的 Skill 驱动 AIGC Agent 平台。仓库采用 Business、Agent、Worker 三个独立 Go Module，并以 `frontend/` 提供用户工作台；local Development Preview 基本链路与后续生产化能力使用不同验收边界。

## 三模块架构

| Module | 生产 Runtime | 核心职责 |
| --- | --- | --- |
| `business/` | `business/cmd/business-service` | 身份与权限、Project、Skill/治理、素材、创作草稿与 Asset 真源 |
| `agent/` | `agent/cmd/agent-service` | Session、Turn、六个 Graph Tool、Operation/Batch/Job、Workspace Card/Event 编排 |
| `worker/` | `worker/cmd/business-worker` | Claim/Lease/Heartbeat、确定性媒体执行、产物校验及 Business Finalize |

三个 Module 必须以 `GOWORK=off` 独立构建和测试；根 `go.work` 仅用于本地联调，不是生产 Runtime。

## 快速启动

前置环境：Go `1.26.3`、Node.js `24`、Docker Compose、`ffmpeg`/`ffprobe`，以及可用的 Chromium。首次运行还需要下载 Go 与 npm 依赖。

```bash
cp .env.example .env.local

cd frontend
npm ci
cd ..

make local-up
make migration-tools
make W0_ENV_FILE=.env.example trial-basic
```

`trial-basic` 会使用独立测试数据库启动三模块、Vite 与 Chromium，并验证登录、项目、文本素材、六个 Graph Tool、PNG/MP4、受保护内容读取和工作台刷新恢复。它是快速 MVP 主链验收，不等同于完整质量门禁或生产发布。完成后可执行 `make local-down` 停止本地中间件。

## 常用命令

| 命令 | 用途 |
| --- | --- |
| `make local-up` / `make local-down` | 启停本地 PostgreSQL、Redis、etcd |
| `make migrate-up` | 执行三个 Module 的本地 Migration |
| `make trial-basic` | 跑通统一 MVP 主链并生成本地 Evidence |
| `make test-document-single-source` | 校验文档单一事实源约束 |
| `make verify` | 校验三个 Module 的依赖完整性 |
| `make test` | 执行三个 Module 测试及静态 smoke 契约测试 |
| `make vet` / `make race` / `make build` | 执行服务端静态检查、竞态测试和独立构建 |
| `make check-frontend` | 执行前端单测和生产构建 |

## 文档与协作

- [统一文档索引](docs/README.md)
- [项目协作指引](AGENTS.md)
- [服务端开发规范](.agents/skills/dora-server-development/SKILL.md)
