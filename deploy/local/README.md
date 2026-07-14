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
