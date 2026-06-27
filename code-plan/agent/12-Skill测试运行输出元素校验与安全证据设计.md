# 12-Skill测试运行输出元素校验与安全证据设计

状态：production-design-ready
owner：Go Eino 智能体微服务架构工程师
更新时间：2026-06-27
适用范围：Skill 发布前测试运行、测试样例、输出元素校验、测试隔离、安全证据和测试结果回传
相关代码路径：`services/agent/internal/runtime/skilltest/**`、`services/agent/internal/runtime/skill/**`、`services/agent/internal/runtime/safety/**`
相关契约：`docs/product/prd/05-SkillBuilder与审核PRD.md`、`docs/product/prd/10-内容安全治理PRD.md`、`api/thrift/business_agent_service.thrift`

## 文档目标

- 定义 Skill 发布前至少 3 个测试样例的 Agent 侧执行能力。
- 定义测试运行如何隔离正式会话、积分扣费、资产保存和业务写入。
- 定义输出元素结构校验规则。
- 定义测试样例安全评估和安全证据结构。
- 定义测试结果如何回传业务服务，用于 Skill 发布和审核。

## 功能范围

- 系统 Skill、企业 Skill、个人 Skill 的测试运行。
- 测试样例数量校验。
- Skill runtime spec 读取。
- 测试输入安全评估。
- Tool 白名单和风险策略校验。
- 输出元素结构校验。
- 过程态测试 artifact 生成。
- 测试结果保存或回传。
- 测试失败原因分类。

## 测试运行边界

| 项目 | 规则 |
| --- | --- |
| 会话 | Skill 测试使用独立 `test_run_id`，不写入用户正式 session。 |
| 积分 | 生产级测试运行不实际扣费；如调用真实供应商产生成本，需要业务侧定义平台内部成本处理，不进入用户积分账户。 |
| 资产 | 测试产物不创建用户可见业务资产；只保存隔离测试 artifact 摘要。 |
| Tool | 只能调用平台开放 Tool；高风险、业务写入 Tool 在测试中必须走 preview 或隔离测试 adapter，不允许直接改业务事实。 |
| 安全 | 测试输入和 Skill 组装提示词必须产生 `scene=skill_test` 的安全证据。 |
| 输出 | 必须验证 Skill 声明的必填资产元素是否存在、元素类型是否合法、render hint 是否可用。 |

## 核心函数必须覆盖

| 函数 | 入参 | 出参 |
| --- | --- | --- |
| `RunSkillTestCase` | `skill_id`、`skill_version`、`test_case_id`、`test_input`、`auth_context`、`request_meta` | `test_run_id`、`status`、`output_summary`、`validation_result`。 |
| `EvaluateSkillTestSafety` | `test_case_id`、`assembled_prompt_digest`、`scene=skill_test` | `safety_evidence`、`blocked_reason`。 |
| `ValidateSkillOutputElements` | `skill_output_schema`、`actual_elements[]`、`asset_element_types[]` | `missing_required[]`、`invalid_types[]`、`renderable`。 |
| `BuildSkillTestReport` | `test_run_id`、`tool_calls[]`、`validation_result`、`safety_evidence` | `test_report`。 |
| `SubmitSkillTestResult` | `skill_id`、`version`、`test_report`、`idempotency_key` | `saved`、`business_status`。 |

## Skill Test Runtime 架构

```text
SkillTestApplication
  -> SkillCatalog RPC 读取待测试 spec
  -> 校验 test_cases 数量 >= 3
  -> 为每个 case 创建独立 test_run
  -> EvaluateSkillTestSafety
  -> 使用 Eino Graph 执行测试流程
  -> ValidateSkillOutputElements
  -> BuildSkillTestReport
  -> SubmitSkillTestResult
```

测试运行使用 `services/agent/internal/runtime/skilltest`，不进入正式 TurnLoop 的积分冻结和资产 commit 流程。Eino 使用 `Graph` 复用正式 Skill 执行节点，使用 `Callback` 捕获 tool call、模型输出和安全事件。

## 测试状态机

| 状态 | 进入条件 | 可流转到 |
| --- | --- | --- |
| `pending` | 业务提交测试请求后 | `running`、`rejected` |
| `running` | 安全通过并开始执行 | `passed`、`failed`、`blocked`、`timeout` |
| `blocked` | 安全评估不通过 | 终态 |
| `failed` | Tool、模型或输出校验失败 | 终态 |
| `timeout` | 超过测试策略时间 | 终态 |
| `passed` | 输出元素校验通过 | 终态 |
| `rejected` | 测试样例少于 3 个或 spec 无效 | 终态 |

## DTO 设计

```go
// SkillTestCaseDTO 是业务侧提交给 Agent 的单个测试样例。
type SkillTestCaseDTO struct {
    TestCaseID   string
    InputText    string
    Controls     map[string]any
    ExpectedTags []string
}

// SkillOutputValidationResult 描述 Skill 输出元素是否满足发布要求。
type SkillOutputValidationResult struct {
    Passed            bool
    MissingRequired   []string
    InvalidTypes      []string
    UnrenderableHints []string
    ElementCount      int
}

// SkillTestReport 是 Agent 回传业务服务的测试报告。
type SkillTestReport struct {
    TestRunID          string
    TestCaseID         string
    Status             string
    OutputSummary      string
    ValidationResult   SkillOutputValidationResult
    SafetyEvidenceID   string
    ToolCallSummaries  []ToolCallSummaryDTO
    TraceID            string
}
```

## 输出元素校验算法

1. 从业务返回的 `skill_spec.output_schema` 读取必填元素、允许元素类型和 render hint。
2. 调 `PlatformDictionaryService.ListAssetElementTypes(page_size=50)` 获取合法元素类型。
3. 遍历 `actual_elements[]`，按 `element_type`、`required`、`render_hint`、`metadata_schema` 校验。
4. 输出 `missing_required[]`、`invalid_types[]`、`unrenderable_hints[]`。
5. 任一必填缺失或非法类型存在时，测试 case 标记 `failed`。

## 安全证据字段

Skill 测试安全证据必须包含：

- `scene=skill_test`
- `target_type=skill_test_prompt`
- `target_ref_id=test_case_id`
- `evaluated_object_digest`
- `policy_version`
- `evidence_version`
- `result`
- `evaluated_at`
- `expires_at`
- `source_run_id=test_run_id`
- `trace_id`

## 【业务开发】需要提供的能力与参数

| 能力 | 请求参数 | 响应参数 |
| --- | --- | --- |
| 读取待测试 Skill 规格 | `auth_context`、`skill_id`、`version`、`request_meta` | `skill_spec`、`output_schema`、`tool_refs[]`、`test_cases[]`、`status`。 |
| 保存 Skill 测试结果 | `auth_context`、`skill_id`、`version`、`test_case_results[]`、`output_validation`、`safety_evidence_refs[]`、`idempotency_key` | `saved`、`skill_test_status`、`failed_reasons[]`。 |
| 查询资产元素类型 | `auth_context`、`page_size=50`、`schema_version` | `element_types[]`、`schema_version`。 |
| Tool 测试策略 | `tool_refs[]`、`auth_context`、`test_mode=true` | `allowed`、`test_double_required`、`timeout_ms`、`risk_level`。 |

## Tool 隔离测试 / preview 策略

| Tool 类型 | 测试策略 |
| --- | --- |
| 纯文本分析 | 可真实执行，保存摘要。 |
| 图片/音乐/视频生成 | 测试环境必须配置隔离测试供应商或隔离测试 adapter；生产环境未配置真实供应商时必须失败。 |
| 业务写入 | 只能 preview 或隔离测试 adapter，不允许改变业务事实。 |
| 高风险 Tool | 测试模式直接失败并记录 `risk_tool_not_allowed_in_test`。 |

## 日志和测试矩阵

| 场景 | 断言 |
| --- | --- |
| 少于 3 个样例 | 状态 `rejected`，不执行 Tool。 |
| 安全阻断 | 状态 `blocked`，报告含 `safety_evidence_id`。 |
| 输出缺必填元素 | 状态 `failed`，`missing_required[]` 非空。 |
| 非法元素类型 | 状态 `failed`，`invalid_types[]` 非空。 |
| 全部通过 | 每个 case `passed`，业务侧 `skill_test_status=passed`。 |
| 重复回传 | 相同幂等键返回同一保存结果。 |

日志字段：`skill_id`、`version`、`test_run_id`、`test_case_id`、`status`、`validation_passed`、`tool_call_count`、`trace_id`。
