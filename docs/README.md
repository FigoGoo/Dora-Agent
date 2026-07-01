# Dora-Agent 文档入口

状态：active  
owner：文档与契约责任域  
更新时间：2026-06-30  
适用范围：Dora-Agent 当前文档导航

## 当前状态

现存项目设计文档已在 Agent 核心重构前完成物理归档，避免旧产品、技术、契约和测试口径干扰新一轮设计。

旧文档统一归档到：

- `docs/archive/pre-agent-core-refactor-2026-06-30/`

## 当前读取入口

- 当前事实源：`docs/current/README.md`
- 文档规范：`docs/standards/文档规范.md`
- 文档模板：`docs/templates/README.md`
- 历史归档：`docs/archive/README.md`

## 使用规则

- 归档文档只用于历史追溯，不作为 Agent 核心重构实现依据。
- 新重构文档必须从用户提供的 `AIGC_Creation_Agent v1_1.md` 重新导入并按模板落入当前目录。
- 需要复用旧结论时，先迁移到新的 active 文档，再进入代码开发。
