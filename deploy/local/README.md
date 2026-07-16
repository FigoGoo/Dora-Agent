# Dora 本地基础设施

该 Compose 只启动三个 Runtime 的共享基础设施：PostgreSQL、Redis 和 etcd。三个 Go 服务始终从各自 Module 启动，不被包装成根目录生产 Runtime。

## 使用

```bash
cp .env.example .env.local
make local-up
make migrate-up
make run-business
make run-agent
make run-worker
```

健康接口分别位于 `http://127.0.0.1:18081/readyz`、`http://127.0.0.1:18082/readyz`、`http://127.0.0.1:18083/readyz`。

为避免与历史 Demo 或系统服务冲突，模板默认映射 PostgreSQL `15432`、Redis `16379`、etcd `12379`；可在 `.env.local` 中通过 `DORA_POSTGRES_PORT`、`DORA_REDIS_PORT`、`DORA_ETCD_PORT` 覆盖，并同步修改三个 Runtime 的连接配置。

`make local-down` 保留数据卷；需要验证空库 Migration 时执行 `make local-reset`。后者会删除 `dora-local` Compose 项目的本地数据卷，不得用于共享或生产环境。

本地每个数据库使用独立应用角色简化联调。生产环境必须由部署系统分别注入 DDL 与 Runtime 最小权限角色，不能复用这里的开发密码或 PostgreSQL 管理角色。

## W1 Skill 完整本地门禁

`w1-smoke` 与 `w1-browser-smoke` 执行同一套完整门禁；编排会启动基础设施、执行 Migration、从当前 worktree 重建 Runtime/前端，并通过 build-tagged 本地 Seeder 准备 Creator、Reviewer、Governor、Provisioner 四个独立账号，无需手工 SQL：

```bash
make GO=/Users/figo/sdk/go1.26.3/bin/go W0_ENV_FILE=.env.example w1-smoke
```

Seeder 仅能以 `localsmoke` tag 在 `DORA_ENV=local`、loopback 专用数据库和本地应用角色下编译执行，不得作为生产赋权入口。成功运行必须同时原子发布 Foundation canonical Evidence 与独立 Governance sidecar；只有其中任一缺失、未通过或脱敏扫描失败，整次门禁都失败关闭。
