# Dora-Agent 文档索引

## 开发入口

1. 产品体验设计师先产出《产品系统设计》，并确认 `product_status: Done`。
2. 产品体验设计师将产品系统设计沉淀为 PRD，PRD 入口见 `docs/product/prd/README.md`。
3. 产品体验设计师使用 `docs/templates/项目开发计划模板.md` 安排开发计划、功能点、owner 和里程碑。
4. 工程 subagent 阅读 `docs/standards/产品设计交接与开发约束规范.md`，输出需求映射矩阵。
5. 主控 Codex 按 `AGENTS.md` 调度 subagent，并按功能点推进开发和 GitHub 提交。

## 产品文档

- 产品设计索引：`docs/product/AIGC智能体产品设计索引.md`
- PRD 套件：`docs/product/prd/README.md`
- 系统概要与功能大纲：`docs/product/prd/00-系统概要与功能大纲PRD.md`

## 设计文档

- UI/UE 设计索引：`docs/design/README.md`
- UI/UE 设计总纲：`docs/design/00-UIUE设计总纲.md`
- 信息架构与导航：`docs/design/01-站点信息架构与导航.md`
- 统一 Agent 工作台：`docs/design/02-统一Agent创作工作台体验设计.md`
- 首页体验设计：`docs/design/03-首页体验设计.md`
- 视觉风格与 Token：`docs/design/08-视觉风格与设计Token草案.md`

## 规范

- 开发流程：`docs/standards/开发流程规范.md`
- 产品交接：`docs/standards/产品设计交接与开发约束规范.md`
- GitHub 协作：`docs/standards/GitHub仓库协作规范.md`
- 本地开发配置：`docs/standards/本地开发与配置规范.md`
- 安全：`docs/standards/安全规范.md`
- 后端技术栈：`docs/standards/后端技术栈与操作规范.md`
- CloudWeGo：`docs/standards/CloudWeGo开发操作规范.md`
- SQL 脚本：`docs/standards/迭代SQL脚本规范.md`
- 智能体配置化：`docs/standards/智能体配置化规范.md`

## 模板

- 产品：`docs/templates/PRD模板.md`
- 项目计划：`docs/templates/项目开发计划模板.md`
- 开发任务：`docs/templates/开发任务模板.md`
- PR：`.github/PULL_REQUEST_TEMPLATE.md`
- SQL 清单：`docs/templates/迭代SQL脚本清单模板.md`
- SQL 脚本：`docs/templates/SQL脚本模板.sql`

## 阶段说明

当前阶段：本地开发，环境为 macOS + Docker。
线上目标：CentOS 8 单机。
上线后补充：CI/CD、发布回滚、可观测性、告警、SLO、测试环境矩阵和前端设计系统落地流程。
