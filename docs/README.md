# Dora-Agent 文档索引

状态：active  
owner：文档与契约责任域
更新时间：2026-06-28  
适用范围：`docs/**` 文档导航和阶段关注入口

## 当前阅读入口

1. 协作和边界规则先读 [`../AGENTS.md`](../AGENTS.md)。
2. 当前事实源入口读 [`current/README.md`](./current/README.md)。
3. 技术文档入口读 [`technical/README.md`](./technical/README.md)。
4. 产品功能和 PRD 读 [`product/README.md`](./product/README.md)。
5. 跨服务契约读 [`contracts/README.md`](./contracts/README.md)。
6. 编码、契约、测试、安全和文档规范读 [`standards/README.md`](./standards/README.md)。
7. 测试用例、测试报告和缺陷记录读 [`test/README.md`](./test/README.md)。

## 目录索引

| 目录索引 | 作用 | 当前关注 |
| --- | --- | --- |
| [`current/README.md`](./current/README.md) | 当前事实源读取入口，定义 Codex 只读最新文档的规则。 | 关注 |
| [`technical/README.md`](./technical/README.md) | 技术设计、后端、前端、主题、CI/CD 和功能迭代文档入口。 | 关注 |
| [`product/README.md`](./product/README.md) | 产品索引、PRD、产品迭代、第一版功能模块清单和前置产品系统设计状态。 | 关注 |
| [`contracts/README.md`](./contracts/README.md) | RPC、API、AG-UI、Agent 数据模型、SQL 契约入口、字段级事实源和成熟度复核。 | 关注 |
| [`standards/README.md`](./standards/README.md) | 开发流程、编码、契约、测试、安全、TOS、CloudWeGo 和本地配置规范。 | 关注 |
| [`test/README.md`](./test/README.md) | 用户端、管理端、后端和 Agent 服务端能力测试用例入口。 | 关注 |
| [`releases/README.md`](./releases/README.md) | 已完成阶段的交付范围、验收结论和历史设计入口。 | 按需关注 |
| [`architecture/README.md`](./architecture/README.md) | 早期 Agent 与业务架构草案索引，当前仅作历史追溯。 | 不关注 |
| [`design/README.md`](./design/README.md) | UI/UE、页面、视觉规范和线框草图索引，当前非前端阶段不主动关注。 | 不关注 |
| [`templates/README.md`](./templates/README.md) | PRD、ADR、契约、数据模型、开发任务、测试报告和缺陷报告模板。 | 不关注 |
| [`archive/README.md`](./archive/README.md) | 过期、废弃或仅供追溯的历史文档入口。 | 不关注 |

## 阶段说明

当前阶段：本地开发，环境为 macOS + Docker。  
线上目标：CentOS 8 单机。  
上线后补充：CI/CD、发布回滚、可观测性、告警、SLO、测试环境矩阵和前端设计系统落地流程。

第一阶段服务端开发设计已经归档到 [`releases/phase-01-server/README.md`](./releases/phase-01-server/README.md)。后续任务默认不再读取 `code-plan/**`，除非需要追溯第一阶段历史设计。
