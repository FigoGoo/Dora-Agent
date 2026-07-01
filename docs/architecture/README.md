# 架构文档入口

状态：active  
owner：架构与文档责任域  
更新时间：2026-06-30  
适用范围：Agent 核心重构架构文档导航

## 当前状态

旧架构草案已归档到 `docs/archive/pre-agent-core-refactor-2026-06-30/docs/architecture/`。

本目录暂无可作为 Agent 核心重构依据的 active 架构文档。后续如需记录跨服务边界、Eino 选型、Redis 事件、缓存、并发和数据边界，应创建新的 ADR 或技术架构文档。

## 使用规则

- 新架构文档必须明确 Agent 服务与业务服务边界。
- 业务事实仍由业务微服务维护，Agent 服务只保存 Runtime 事实。
- 旧架构文档只用于历史追溯。
