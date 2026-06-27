# 当前文档入口

状态：active  
owner：文档与契约责任域  
更新时间：2026-06-28  
适用范围：Codex 和人工协作时的当前事实源读取入口

## 目标

本目录只维护当前有效文档入口，不复制各领域正文。Codex 执行任务时先从这里判断应该读取哪些文档，避免历史阶段计划、草图或过期讨论污染当前实现。

## 读取原则

1. 先读仓库级规则：`AGENTS.md`。
2. 再读本文档，确认当前事实源。
3. 按任务类型读取对应目录索引和具体 active 文档。
4. `code-plan/**`、`docs/releases/**`、`docs/archive/**` 默认不读取；只有追溯第一阶段服务端设计或历史决策时才按需读取。
5. 如果当前代码、契约和文档不一致，先判断事实源归属，再同步修正文档或实现。

## 当前事实源

| 领域 | 当前入口 | 必读时机 | 变更记录位置 |
| --- | --- | --- | --- |
| 技术设计 | [`../technical/README.md`](../technical/README.md) | 架构、后端、前端、主题、CI/CD 或迭代设计任务 | 对应技术设计文档、[`../technical/iterations/README.md`](../technical/iterations/README.md) 和目录 README |
| 产品设计 | [`../product/README.md`](../product/README.md) | 产品目标、用户流程、页面范围、验收标准变更 | PRD、产品设计文档和 [`../product/iterations/README.md`](../product/iterations/README.md) |
| 契约 | [`../contracts/README.md`](../contracts/README.md)、[`../contracts/契约成熟度复核.md`](../contracts/契约成熟度复核.md)、[`../contracts/字段级契约索引.md`](../contracts/字段级契约索引.md) | RPC、API、AG-UI、Agent 数据模型、SQL 变更 | 对应契约文档、字段级事实源、成熟度缺口和 contract fixture |
| 规范 | [`../standards/README.md`](../standards/README.md) | 编码、测试、安全、文档、发布、数据库规则变更 | 对应规范文档 |
| 测试 | [`../test/README.md`](../test/README.md) | 测试用例、测试报告、缺陷复核和验收 | 测试用例、测试报告、缺陷报告 |
| 阶段交付 | [`../releases/README.md`](../releases/README.md) | 查看已完成阶段范围、结论、遗留风险 | 对应 release 目录 |
| 历史归档 | [`../archive/README.md`](../archive/README.md) | 追溯废弃方案、早期草案或过期讨论 | 对应 archive 目录 |

## 文档状态

- `draft`：草案，不能作为开发最终依据。
- `review`：评审中，可用于讨论和补充，不进入正式代码开发。
- `active`：当前有效事实源。
- `archived`：历史阶段归档，只能追溯，不再承接新迭代。
- `deprecated`：已废弃，必须指向替代文档或说明废弃原因。

## 开发前文档要求

- 新功能开发前必须有产品目标、技术设计、契约或测试口径中的至少一个明确入口。
- 涉及 RPC、API、AG-UI、数据模型、SQL、配置、权限或测试夹具时，先更新对应文档，再改实现。
- 如果只有历史设计，没有当前 active 文档，先把历史结论迁移成新的 current/technical/product/contract 文档，再进入开发。

## 开发完成记录

开发完成后按变更类型记录：

- 契约变更：更新 `docs/contracts/**`、fixture 和对应 README 状态。
- 技术实现变更：更新 `docs/technical/**`、`docs/technical/iterations/**` 或相关规范。
- 产品口径变更：更新 `docs/product/**`、`docs/product/iterations/**` 或 PRD。
- 测试结果：更新 `docs/test/**`、测试报告或缺陷报告。
- 阶段总结：写入 `docs/releases/**`，不要追加到当前开发计划里。
