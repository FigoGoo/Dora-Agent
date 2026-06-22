# Database Migration Scripts

本目录保存每次迭代准备的 SQL 脚本。

当前策略：

- 本地开发可使用 golang-migrate 执行。
- 线上 CentOS 8 单机环境由用户手动迁移。
- 业务数据库和 Agent 领域数据库分开目录。
- 每个功能点按顺序准备 `up.sql` 和 `down.sql`。

建议结构：

```text
db/migrations/iterations/<YYYY-MM-DD-iteration>/
  agent/
    0001_<feature>.up.sql
    0001_<feature>.down.sql
  business/
    0001_<feature>.up.sql
    0001_<feature>.down.sql
  README.md
```

详细规范见 `docs/standards/迭代SQL脚本规范.md`。
