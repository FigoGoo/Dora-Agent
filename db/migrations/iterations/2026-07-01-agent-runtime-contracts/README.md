# 2026-07-01 Agent Runtime Contracts Migration

状态：active  
owner：Agent Runtime / 数据库责任域  
更新时间：2026-07-01  
适用范围：PR-2 Agent Runtime Board / Graph / AG-UI replay 表结构草案

## 说明

本目录承接 `docs/active/contracts/pr-2-agent-runtime-contracts.md`。

当前 SQL 是 PR-2 字段级契约的一部分，用于冻结 Agent Runtime 表边界：

- 只保存 Agent Runtime 数据。
- 不保存订单、支付、结算、用户资产等业务事实。
- 不添加数据库级外键约束。
- 业务事实必须通过业务微服务 RPC 产生。

真实 PostgreSQL dry-run 和 down-test 作为后续 CI gate。
