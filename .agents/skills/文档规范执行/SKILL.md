---
name: 文档规范执行
description: 用于 PRD、ADR、RPC、API、AG-UI、Agent 数据模型、测试报告和缺陷报告的规范化写作。
---

# 文档规范执行

## 目标

让项目文档拥有一致路径、状态、owner、更新时间、适用范围、背景、目标、约束、方案、风险和验收标准。

## 使用场景

- 编写或更新 PRD、ADR、RPC 契约、API 契约、AG-UI 协议、Agent 数据模型、测试报告、缺陷报告。
- 需要选择 docs/templates 下的模板。
- 需要判断文档 owner 或状态。

## 输入

- 文档类型和任务背景。
- `docs/standards/文档规范.md`。
- `docs/templates/` 下对应模板。
- 相关代码路径、契约和测试证据。

## 文档使用规则

- 开始任务先读 `AGENTS.md` 和 `docs/current/README.md`，再按任务读取 `docs/technical/README.md`、`docs/product/README.md`、`docs/contracts/README.md`、`docs/standards/README.md` 或 `docs/test/README.md`。
- 开发前若缺产品目标、技术设计、契约、数据模型、SQL 或测试口径，先补对应 `draft` 或 `active` 文档，不直接从历史归档派生实现。
- 开发前需要设计时，按 `docs/templates/` 下对应模板新建或更新 `docs/technical/**`、`docs/product/**`、`docs/contracts/**` 或 `docs/test/**`，状态先用 `draft` 或 `review`；达到当前事实源条件后再改为 `active`。
- 旧文档不再承接当前迭代、内容被字段级事实源替代、包含已完成阶段计划或与当前代码冲突时，标记为 `archived` 或 `deprecated`，移动到 `docs/archive/**` 或 `docs/releases/**`，并在当前入口写明替代文档。
- 开发中新增或改变 RPC、API、AG-UI、数据模型、SQL、配置、权限、错误码或测试夹具时，同步更新对应文档和目录 README。
- 开发完成后，在相关设计、契约或测试报告中记录实现状态、验证命令、证据路径、未执行原因、遗留风险和后续动作。
- `code-plan/**`、`docs/releases/**`、`docs/archive/**` 默认只用于历史追溯；需要复用其中结论时，先迁移到当前 active 文档。

## 执行流程

1. 文档类型判断：PRD、ADR、RPC、API、AG-UI、Agent 数据模型、测试报告或缺陷报告。
2. 模板选择：从 docs/templates 选择对应模板，不随意自造结构。
3. 填写 owner：按 AGENTS.md 责任域指定维护范围。
4. 填写状态：draft、review、active、archived 或 deprecated。
5. 填写更新时间：使用当前日期。
6. 填写适用范围：说明服务、模块、接口或页面范围。
7. 填写背景、目标、非目标和约束。
8. 填写方案：包含契约、数据、状态、流程或测试证据。
9. 填写风险和验收标准。
10. 关联相关代码路径和相关契约。

## 输出

- 文档类型。
- 使用模板。
- owner。
- 状态。
- 更新时间。
- 适用范围。
- 背景、目标、非目标、约束。
- 方案。
- 风险。
- 验收标准。

## 检查表

- [ ] 是否选择正确模板。
- [ ] 是否有 owner、状态、更新时间、适用范围。
- [ ] 是否写清背景、目标、非目标和约束。
- [ ] 是否关联代码路径和契约。
- [ ] 是否有风险和验收标准。
- [ ] 是否避免空壳模板。

## 注意事项

- 文档是跨责任域协作事实源，不写口头约定。
- 契约类文档优先于实现。
- archived 文档只用于历史追溯，不承接新迭代。
- deprecated 文档必须说明替代文档或废弃原因。
