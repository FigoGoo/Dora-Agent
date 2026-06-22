# GitHub 仓库协作规范

状态：active  
owner：主控 Codex 汇总维护  
适用范围：所有代码、文档、配置、模板变更  
更新时间：2026-06-22  

## 目标

让每个功能点都能在 GitHub 中清晰追踪、评审、回滚和验证。

## 分支规范

按功能点创建分支：

```text
feature/<feature-slug>
fix/<bug-slug>
docs/<doc-slug>
chore/<infra-slug>
```

示例：

```text
feature/agent-session-runtime
fix/agui-resume-event
docs/product-done-flow
```

## 提交规范

每次提交只对应一个功能点、缺陷点或文档点。禁止把多个无关功能混在一个提交里。

提交信息建议：

```text
<type>(<scope>): <summary>
```

type 可选：

- `feat`：功能点。
- `fix`：缺陷修复。
- `docs`：文档。
- `test`：测试。
- `refactor`：不改变行为的重构。
- `chore`：工程配置或工具。

示例：

```text
feat(agent): add session run state model
docs(flow): define product done gate
fix(agui): handle duplicated event id
```

## PR 规范

- PR 必须围绕一个功能点或一个清晰主题。
- PR 描述必须使用 `.github/PULL_REQUEST_TEMPLATE.md`。
- PR 必须说明产品文档 Done 状态、需求映射、契约影响、SQL 脚本、验证结果和风险。
- 涉及 Thrift / Kitex 生成代码时，必须说明生成命令和源 IDL。
- 生成代码不得手动修改；如必须修改，需要在 PR 中解释原因。

## 评审规则

- 涉及产品行为变更：产品体验设计师确认产品映射。
- 涉及 Agent Runtime：Go Eino 智能体微服务架构工程师评审。
- 涉及业务规则、业务数据库、RPC server：业务微服务后端工程师评审。
- 涉及前端页面、AG-UI 消费：前端开发工程师评审。
- 涉及测试、缺陷验证、数据库证据：浏览器、RPC 与数据库测试工程师评审。

## 合并前检查

- 产品文档为 Done。
- 需求映射矩阵完整。
- 契约和文档已同步。
- 本地 env 配置不包含真实密钥。
- SQL 脚本已按迭代目录准备。
- 本地验证或替代验证已说明。
- 无无关格式化、无关重构和无关文件。

## 禁止事项

- 禁止一个提交混入多个功能点。
- 禁止直接提交真实 `.env`、密钥、Token、AK/SK。
- 禁止绕过产品 Done Gate。
- 禁止绕过契约直接改调用方或消费方。
- 禁止手改生成代码后不说明原因。
