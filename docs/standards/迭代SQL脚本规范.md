# 迭代 SQL 脚本规范

状态：active  
owner：主控 Codex 汇总维护  
适用范围：业务数据库和 Agent 领域数据库的迭代 SQL 脚本  
更新时间：2026-06-22  

## 当前策略

- 当前阶段先完成本地开发。
- 本地可以使用 golang-migrate 执行 SQL。
- 线上环境为 CentOS 8 单机，上线后由用户手动迁移。
- 每次迭代必须准备 SQL 脚本，但不自动执行线上迁移。

## 目录结构

建议：

```text
db/migrations/
  iterations/
    2026-06-22-iteration-name/
      agent/
        0001_feature_name.up.sql
        0001_feature_name.down.sql
      business/
        0001_feature_name.up.sql
        0001_feature_name.down.sql
      README.md
```

## 命名规则

```text
NNNN_<feature_slug>.up.sql
NNNN_<feature_slug>.down.sql
```

- `NNNN` 为迭代内顺序号。
- `feature_slug` 对应功能点。
- `up.sql` 是正向变更。
- `down.sql` 是回滚脚本；无法回滚时必须写明原因和人工回滚方式。

## 脚本要求

- SQL 必须可重复评审。
- 注释说明功能点、owner、影响表、风险。
- 业务库和 Agent 领域库分开。
- 大表索引、字段默认值、非空约束需要说明锁风险。
- 初始化数据必须可识别来源，避免污染生产业务事实。

## 提交要求

- 涉及数据库的功能点 PR 必须包含 SQL 脚本或说明无需 SQL。
- SQL 脚本必须跟随功能点提交。
- PR 中必须列出本地执行结果或未执行原因。

## 本地执行示例

```bash
migrate -path db/migrations/iterations/<iteration>/agent -database "$AGENT_DATABASE_URL" up
migrate -path db/migrations/iterations/<iteration>/business -database "$BUSINESS_DATABASE_URL" up
```

## 检查表

- [ ] 是否按功能点准备 SQL。
- [ ] 是否区分 Agent DB 和业务 DB。
- [ ] 是否有 up/down。
- [ ] 是否说明锁风险和回滚方式。
- [ ] 是否在 PR 中说明本地执行结果。
