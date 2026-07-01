# 状态枚举注册表说明

状态：active  
owner：文档与契约责任域  
更新时间：2026-07-01  
适用范围：Agent Runtime、业务服务、前端、AG-UI、SQL、fixture 的状态枚举

## Canonical File

字段事实源为：

```text
api/schemas/common/state-enum-registry.schema.json
```

## 规则

1. 所有状态字段必须引用或等价映射 `StateEnumRegistry.v1`。
2. SQL `status` 字段、OpenAPI enum、Thrift enum/string 常量、前端类型和 fixture 必须保持一致。
3. 新增状态必须先更新 registry，再补 fixture 和 contract test。
4. 破坏性状态变更必须新增版本，不直接重写既有语义。

## 首批枚举

| Enum | 归属 | 说明 |
| --- | --- | --- |
| `RunStatus` | Agent Runtime | Agent run 生命周期 |
| `BoardStatus` | Agent Runtime | Creative Board 生命周期 |
| `GraphPlanStatus` | Agent Runtime | Eino GraphPlan 生命周期 |
| `ToolPlanStatus` | Agent Runtime / Business Credit | ToolPlan 预估、确认、冻结和执行 |
| `ToolTaskStatus` | Agent Runtime | 单个 Tool task 状态 |
| `SkillVersionStatus` | Business Skill | Skill 内容版本状态 |
| `MarketplaceListingStatus` | Business Marketplace | 市场上架状态 |
| `SkillUsageStatus` | Business Credit / Settlement | 市场 Skill 使用费状态机 |
| `SkillUsageChargeStatus` | Business Credit | Skill 使用费冻结、扣费和释放状态 |
| `SkillUsageRefundStatus` | Business Credit / Settlement | Skill 使用费退款状态 |
| `SettlementStatus` | Business Settlement | 创作者结算状态 |
| `InstallationStatus` | Business Marketplace | Skill 安装状态 |
| `InstallationUpgradeStatus` | Business Marketplace | 安装升级状态 |
| `RefundCaseStatus` | Business Marketplace | 退款仲裁状态 |
