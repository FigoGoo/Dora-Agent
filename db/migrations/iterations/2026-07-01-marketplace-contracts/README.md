# 2026-07-01 Marketplace Contracts Migration

状态：active  
owner：Business Skill Marketplace / Business Settlement / 数据库责任域  
更新时间：2026-07-01  
适用范围：PR-4 Skill 市场、安装、SkillUsageRecord、结算和退款治理表结构

## 说明

本目录承接 `docs/active/contracts/pr-4-marketplace-contracts.md`。

- 只在 business 目录保存业务事实。
- Agent Runtime 只能通过 RPC 改变这些事实。
- 不添加数据库级外键约束。
- 本地真实 PostgreSQL dry-run 和 down-test 已由 `services/business/internal/infra/repository/businesscore` integration test 覆盖。
- 发布前仍需在 test 环境重放 migration gate。
