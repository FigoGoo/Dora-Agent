# 历史归档入口

状态：active  
owner：文档与契约责任域  
更新时间：2026-07-01  
适用范围：历史设计副本清理记录和当前归档规则

## 当前清理结论

本次 AIGC Agent 重构 review 已完成并通过。为避免旧设计继续被搜索命中并污染 active 拆分，以下本地历史设计副本已删除：

```text
docs/archive/aigc-agent-refactor/2026-06-30/
docs/archive/pre-agent-core-refactor-2026-06-30/
```

如需追溯历史设计，只能通过 git 历史查询；不得重新作为 active 拆分依据。

## 当前有效入口

| 文档 | 用途 |
| --- | --- |
| [`归档清单.md`](./归档清单.md) | 已清理范围、当前替代入口和使用规则。 |
| `docs/review/aigc-agent-refactor/2026-07-01/` | 已通过 review 的设计依据。 |
| `docs/active/README.md` | M7 active 拆分和字段级契约事实源入口。 |

## 归档触发

- 阶段或里程碑已经完成，计划和过程记录不再指导当前开发。
- 文档被新的 PRD、技术设计、契约或规范替代。
- 文档只保留历史讨论价值，继续读取会误导当前实现。
- 模板、草图或前置讨论已经被正式文档承接。
