# 数据所有权与读写边界

状态：active  
owner：文档与契约责任域  
更新时间：2026-07-01  
适用范围：Agent 服务、业务服务、前端、管理端、测试夹具和数据库迁移

## 总原则

1. Agent 服务只保存 Agent Runtime 数据。
2. 业务事实由业务服务和 Business DB 维护。
3. Agent 服务通过 RPC 改变业务事实，不直接写 Business DB。
4. Business 服务不直接写 Agent DB。
5. 前端只消费 OpenAPI / AG-UI，不发明字段。

## 读写矩阵

| 数据 | Owner | Writer | Reader | 事实源 |
| --- | --- | --- | --- | --- |
| RouterDecision | Agent Runtime | Agent Runtime | Frontend / Test | `api/schemas/router/router-decision.v1.schema.json` |
| AG-UI Event | Agent Runtime | Agent Runtime | Frontend / Test | `api/agui/agent-workbench-events.schema.json` |
| Creative Board | Agent Runtime | Agent Runtime | Frontend / Test | 后续 PR-2 |
| GraphPlan | Agent Runtime | Agent Runtime | Agent Runtime / Test | 后续 PR-2 |
| ToolPlan | Agent Runtime + Business Credit | Agent Runtime | Business Credit / Frontend / Test | 后续 PR-3 |
| Credit Ledger | Business Credit | Business Service | Agent Runtime via RPC / Admin / Test | 后续 PR-3 |
| SkillMarketplaceListing | Business Marketplace | Business Service | Agent Runtime / Frontend / Admin / Test | 后续 PR-4 |
| SkillUsageRecord | Business Credit / Settlement | Business Service | Agent Runtime via RPC / Admin / Test | 后续 PR-4 |
| SkillInstallation | Business Marketplace | Business Service | Agent Runtime / Frontend / Admin / Test | 后续 PR-4 |

## 创作者数据隔离

创作者 API 默认只能读取：

1. Skill 草稿、审核结果、发布版本和 listing 配置。
2. 聚合用量、收入、评分、举报数量。
3. 脱敏错误摘要。

创作者 API 默认不能读取：

1. 用户原始输入。
2. 上传资产。
3. Creative Board 详情。
4. 生成资产 URL 或完整内容。
5. 系统 Prompt、供应商响应和内部策略。
