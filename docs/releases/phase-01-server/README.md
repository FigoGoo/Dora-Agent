# 第一阶段服务端开发归档

状态：archived  
owner：文档与契约责任域  
更新时间：2026-06-28  
适用范围：第一阶段服务端开发完成后的历史设计入口和追溯说明

## 归档说明

第一阶段服务端开发已经完成。原 `code-plan/**` 中的开发批次、阶段推进、Done Gate 和阻断任务内容不再作为当前开发入口。

后续功能迭代、前端开发、发布上线或契约变更，必须先从 [`../../current/README.md`](../../current/README.md) 进入，并更新当前 active 文档。

## 历史入口

| 历史入口 | 用途 | 默认读取 |
| --- | --- | --- |
| [`../../../code-plan/README.md`](../../../code-plan/README.md) | 第一阶段服务端设计归档入口 | 否 |
| [`../../../code-plan/agent/README.md`](../../../code-plan/agent/README.md) | Agent 服务历史设计目录 | 否 |
| [`../../../code-plan/business/README.md`](../../../code-plan/business/README.md) | 业务服务历史设计目录 | 否 |
| [`../../../code-plan/tests/README.md`](../../../code-plan/tests/README.md) | 服务端测试历史设计目录 | 否 |

## 使用规则

- 仅在追溯第一阶段设计背景、字段来源、验收口径或历史取舍时读取。
- 不在 `code-plan/**` 中追加新迭代内容。
- 如果历史设计仍然有效，应迁移或摘录到 `docs/technical/**`、`docs/contracts/**`、`docs/standards/**` 或 `docs/test/**` 的 active 文档。
- 如果历史设计已经被替代，应在替代文档中说明，并把旧文档保持 `archived` 或 `deprecated`。

