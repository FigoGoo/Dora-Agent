# 2026-07-01 Tool Credit Asset Contracts Migration

状态：active  
owner：Agent Runtime / Business Credit / Business Asset / Business Tool / 数据库责任域  
更新时间：2026-07-01  
适用范围：PR-3 ToolPlan、ToolTask、Credit hold、Asset commit 和 Tool pricing 表结构草案

## 说明

本目录承接 `docs/active/contracts/pr-3-tool-credit-asset-contracts.md`。

- Agent 目录只保存 Agent Runtime 侧 ToolPlan / ToolTask 状态。
- Business 目录保存业务事实：credit hold、ledger、asset commit、tool pricing snapshot。
- 不添加数据库级外键约束。
- 真实 PostgreSQL dry-run 和 down-test 作为后续 CI gate。
