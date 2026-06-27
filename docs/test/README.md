# 服务端测试用例索引

状态：active  
owner：测试与验收责任域
更新时间：2026-06-28  
适用范围：Dora-Agent 第一版用户端、管理端、业务服务和 Agent 微服务的服务端能力测试用例  
相关代码路径：`tests/contract/**`、`tests/business/**`、`tests/agent/**`、`tests/e2e/**`、`api/thrift/**`、`api/openapi/**`、`api/agui/**`、`db/migrations/iterations/**`  
相关契约：`docs/current/README.md`、`docs/contracts/**`、`docs/standards/测试规范.md`

## 背景

本目录承接当前产品、技术、契约和开发规范，沉淀可执行的测试用例。测试范围覆盖 HTTP API、Agent API、RPC、AG-UI 事件生产、数据库、事务、权限、幂等、日志和服务级链路；进入前端阶段后再补页面视觉、组件渲染、浏览器点击或 CSS 布局测试。

## 目标

- 按用户端、管理端、后端、Agent 四个视角覆盖全部功能点的服务端能力。
- 每个功能点至少覆盖正常路径、权限路径、业务错误、幂等或分页边界。
- 涉及跨服务能力时同时覆盖 HTTP/API、RPC、数据库和 AG-UI/Agent 事件证据。
- 明确 Agent DB 与业务 DB 的边界，避免把业务事实写入 Agent DB，或把 Agent Runtime 数据写入业务 DB。

## 非目标

- 不编写页面测试、浏览器自动化脚本、视觉回归或组件快照。
- 不定义新的业务字段、RPC 方法、AG-UI 事件或数据库表。
- 不替代真实测试报告；实现后仍需按命令输出和证据补充测试结果。
- 不覆盖部署上线、CI/CD、SLO、告警和生产运行手册。

## 文档清单

| 文档 | 覆盖范围 | 主要证据 |
| --- | --- | --- |
| [用户端服务端能力测试用例](./用户端服务端能力测试用例.md) | C 端、公开端、企业空间、用户 Skill、积分、资产、作品、通知等 HTTP API 与业务效果 | HTTP contract、业务 DB、RPC side effect、隐私字段过滤 |
| [管理端服务端能力测试用例](./管理端服务端能力测试用例.md) | 平台管理员登录、用户管理、模型、Tool、Skill、积分、兑换码、公开作品下架和审计 | Admin HTTP contract、业务 DB、审计、日志脱敏 |
| [后端服务级能力测试用例](./后端服务级能力测试用例.md) | 通用 DTO、幂等、错误码、事务、migration、业务 RPC、数据库边界和服务级 E2E | RPC contract、integration、repository、migration、日志 |
| [Agent服务端能力测试用例](./Agent服务端能力测试用例.md) | Agent API、TurnLoop、AG-UI、SSE 补偿、Agent DB、RPC client、Skill 测试、模型 Tool | Agent API contract、AG-UI replay、Agent DB、RPC fixture |

## 通用测试数据

| Fixture | 必须包含 |
| --- | --- |
| 账户和空间 | 未登录、个人用户、企业 owner、企业 member、被移除成员、禁用用户、平台管理员、初始管理员 |
| 项目 | active 项目、archived 项目、跨空间项目、企业空间本人项目、企业空间他人项目 |
| 配置 | enabled/disabled 模型、默认模型、价格快照、enabled/disabled Tool、高风险 Tool、Published/Draft/Deprecated Skill |
| 积分 | 余额充足、余额不足、即将过期批次、已冻结、已扣费、已释放、绑定用户/企业/渠道兑换码 |
| 资产 | 未确认上传、可访问资产、跨空间资产、企业成员移除后的资产、生成产物对象槽、过期 slot |
| 作品 | private、shared、taken_down 作品，公开快照，点赞记录，下架记录 |
| Agent | active session、running run、waiting_confirmation run、cancelled/failed/completed run、事件缺口、snapshot |

## 统一断言

- 所有列表默认 `page_size=10`，最大 `50`；字典类例外必须在契约中声明。
- 所有写 HTTP API 必须校验 `Idempotency-Key`，重复同 hash 返回同一结果，同 key 不同 hash 返回 `IDEMPOTENCY_CONFLICT`。
- 所有写 RPC 必须携带 `RequestMeta.idempotency_key`，并记录 trace。
- 普通用户端、公开端和管理端响应不得泄露系统 Prompt、完整用户提示词、API Key、TOS 签名、供应商原始响应、内部成本、私有素材正文、完整手机号和完整邮箱。
- 业务 DB 不保存 Agent session、run、message、event、tool_call、blackboard、memory。
- Agent DB 不保存积分余额、积分流水、最终资产事实、作品公开状态、企业成员关系、业务权限事实。
- migration 不允许出现数据库级 `FOREIGN KEY` 或 `REFERENCES`。
- AG-UI 事件必须有 `event_id`、canonical `type`、`session_id`、`run_id`、`project_id`、`space_id`、`sequence`、`timestamp`、`trace_id` 和合法 payload。

## 用例状态

本文档集是当前测试用例基线。实现完成后，每条用例应补充到对应自动化测试或人工验证记录中，并在测试报告中标注通过、失败、阻塞、未执行原因和证据位置。
