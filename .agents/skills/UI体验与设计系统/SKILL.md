---
name: UI体验与设计系统
description: 用于设计网站主题、页面结构、交互组件、Agent 流式输出 UI 和状态规范。
---

# UI体验与设计系统

## 目标

为 AIGC Agent SaaS 建立一致、可实现、可测试的页面结构和交互体验规范。

## 使用场景

- 设计生成音乐、图片、视频等 Agent 工作台。
- 定义页面布局、导航、组件和状态展示。
- 设计 Agent 流式输出、Tool 调用、人工确认和任务状态。

## 输入

- PRD、用户流程和验收标准。
- API 契约和 AG-UI 事件协议。
- 现有前端设计系统或组件库约束。

## 文档使用规则

- 开始任务先读 `AGENTS.md` 和 `docs/current/README.md`，再按任务读取 `docs/technical/README.md`、`docs/product/README.md`、`docs/contracts/README.md`、`docs/standards/README.md` 或 `docs/test/README.md`。
- 开发前若缺产品目标、技术设计、契约、数据模型、SQL 或测试口径，先补对应 `draft` 或 `active` 文档，不直接从历史归档派生实现。
- 开发前需要设计时，按 `docs/templates/` 下对应模板新建或更新 `docs/technical/**`、`docs/product/**`、`docs/contracts/**` 或 `docs/test/**`，状态先用 `draft` 或 `review`；达到当前事实源条件后再改为 `active`。
- 旧文档不再承接当前迭代、内容被字段级事实源替代、包含已完成阶段计划或与当前代码冲突时，标记为 `archived` 或 `deprecated`，移动到 `docs/archive/**` 或 `docs/releases/**`，并在当前入口写明替代文档。
- 开发中新增或改变 RPC、API、AG-UI、数据模型、SQL、配置、权限、错误码或测试夹具时，同步更新对应文档和目录 README。
- 开发完成后，在相关设计、契约或测试报告中记录实现状态、验证命令、证据路径、未执行原因、遗留风险和后续动作。
- `code-plan/**`、`docs/releases/**`、`docs/archive/**` 默认只用于历史追溯；需要复用其中结论时，先迁移到当前 active 文档。

## 执行流程

1. 页面结构：定义页面分区、主工作区、侧栏、历史记录、产物预览和任务详情。
2. 导航结构：说明全局导航、模块导航、面包屑或标签页。
3. 主题风格：定义色彩、字体、间距、密度和品牌调性，不把装饰凌驾于任务效率。
4. 组件规范：定义输入框、提示词编辑器、文件上传、模型参数、产物卡片、任务列表。
5. 表单规范：定义校验、禁用、默认值、提交中和失败重试。
6. Agent 流式输出 UI：支持 agent.run.started、agent.message.delta、agent.message.completed 的增量渲染。
7. Tool 调用 UI：展示 tool.call.started、tool.call.completed 或 tool.call.failed、耗时、状态和错误。
8. 人工确认 UI：展示 confirmation.required，支持确认、拒绝、修改后继续。
9. 状态规范：覆盖加载态、空态、错误态、成功态、streaming、interrupt、resume。
10. 响应式规则：明确移动端、平板、桌面布局变化。

## 输出

- 页面结构。
- 导航结构。
- 主题风格。
- 组件规范。
- 表单规范。
- Agent 流式输出 UI。
- Tool 调用 UI。
- 人工确认 UI。
- 加载态、空态、错误态、成功态。
- 响应式规则。

## 检查表

- [ ] 是否覆盖 Agent 运行全过程。
- [ ] 是否定义 Tool 调用和 Tool 结果展示。
- [ ] 是否定义人工确认状态。
- [ ] 是否处理 loading、empty、error、success、streaming、interrupt、resume。
- [ ] 是否与 AG-UI 事件协议一致。
- [ ] 是否给前端实现留出清晰组件边界。

## 注意事项

- UI 规范不发明后端字段。
- 事件字段以 AG-UI 协议为准。
- 不把产品说明文字塞进界面代替可理解的交互状态。
