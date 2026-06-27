# 架构设计文档入口

状态：active  
owner：文档与契约责任域  
更新时间：2026-06-28  
适用范围：当前架构设计、架构决策和跨服务边界文档导航

## 当前状态

当前没有独立的最新架构设计正文。后续涉及系统架构、服务边界、数据边界、部署拓扑或关键技术选型变更时，应在本目录新增 active 架构设计文档或 ADR。

第一阶段早期架构草案已归档在 `docs/architecture/**`，只用于追溯背景，不作为新迭代默认事实源。

## 当前事实源

| 范围 | 当前入口 |
| --- | --- |
| 系统边界 | `AGENTS.md`、`docs/current/README.md` |
| RPC/API/AG-UI/数据模型 | `docs/contracts/README.md` |
| 编码、数据库、安全和测试约束 | `docs/standards/README.md` |
| 第一阶段历史设计 | `docs/releases/phase-01-server/README.md` |

## 新增规则

- 架构变更必须说明背景、目标、非目标、影响服务、影响契约、数据迁移、测试方式和回滚策略。
- 涉及跨服务读写时，先更新 `docs/contracts/**`，再更新实现。
- 需要记录不可逆或长期有效的技术决策时，优先使用 `docs/templates/ADR模板.md`。

