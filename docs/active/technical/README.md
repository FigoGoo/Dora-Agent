# Active 技术设计入口

状态：active  
owner：文档与契约责任域 / Agent 服务责任域 / 业务服务责任域  
更新时间：2026-07-01  
适用范围：AIGC Agent 核心重构 active 技术设计导航

## 当前 Active 文档

| 文档 | 作用 |
| --- | --- |
| `m7-active-split.md` | M7 active 拆分和发布治理总入口 |
| `pr-0-development-ci-gate.md` | PR-0 开发准备、CI gate、本地验证和后续 PR 准入 |
| `pr-1-contract-runtime.md` | PR-1 Contract Runtime 基础实现、共享 Go 类型和 fixture 漂移防护 |
| `pr-2-agent-runtime-board-graph.md` | PR-2 Agent Runtime / Board / Graph 基础实现和 fixture 漂移防护 |
| `eino-graph-runtime.md` | M3 Published Skill Runtime Spec 与 Eino Graph Runtime 实现 |
| `pr-3-tool-credit-asset.md` | PR-3 Tool / Credit / Asset 基础实现和 fixture 漂移防护 |
| `pr-4-marketplace-skill-usage-settlement.md` | PR-4 Marketplace / SkillUsage / Settlement 基础实现和 fixture 漂移防护 |
| `pr-5-e2e-fake-provider-release-gate.md` | PR-5 E2E / Fake Provider / Release Gate 基础实现和 fixture 漂移防护 |
| `release-governance.md` | PR-5 fake provider、E2E、feature flag、观测、回滚和数据修复 gate |

## 拆分计划

| 技术设计 | 来源 review | 批次 |
| --- | --- | --- |
| `router.md` | M1 | PR-1 / PR-2 |
| `creative-board.md` | M2 | PR-2 |
| `eino-graph-runtime.md` | M3 | PR-2 |
| `tool-runtime.md` | M4 / 09 | PR-3 |
| `marketplace-billing.md` | M5 / 10 | PR-4 |
| `release-governance.md` | M7 | PR-5 |

技术设计只解释架构、流程、边界、幂等和错误；字段以 schema、RPC、OpenAPI、SQL 和 fixture 为准。
