# Skill 路由真实场景评估报告

日期：2026-06-30

结论：通过。已清理本地 E2E/DeepSeek 验证遗留的 published 测试 Skill，避免污染用户语义判断；Agent Skill 路由已从“无命中时默认选择第一个 published Skill”改为“必须显式命中 route_hints”，并支持多关键词、负向关键词和优先级。

## 本轮发现的问题

| 问题 | 影响 | 处理 |
| --- | --- | --- |
| 本地业务库存在 13 个 E2E/DeepSeek 验证 Skill，状态为 `published` | 它们会进入 `ListRoutableSkills`，旧路由无命中时会兜底选中最近更新的测试 Skill | 已通过后台 deprecate API 废弃这些测试 Skill |
| `published_default` 兜底过强 | 用户写邮件、发票、泛聊等无关 prompt 也可能命中任意 Skill | 已移除任意 published 兜底，无显式 hint 时返回 `no_route_hint_match` |
| 只支持单值 `route_hints` 包含匹配 | 多场景、多 Skill 时不可控，无法表达排除词和优先级 | 新增 `keywords` 多词、`negative_keywords` 排除词、`priority` 排序 |
| E2E 脚本默认留下 published 测试 Skill | 后续命中率评估会再次被污染 | 三个 E2E 脚本默认在退出时 deprecate 自己创建的 Skill，必要时可用 `KEEP_E2E_SKILL=true` 保留 |

## 当前真实场景矩阵

执行命令：

```bash
scripts/eval-skill-routing.sh
```

结果：6/6，准确率 1.00。

| 场景 | Prompt 摘要 | 期望 | 实际 | 结果 |
| --- | --- | --- | --- | --- |
| `storyboard_cn` | 城市香水 30 秒广告短片，三条分镜 | `sk_seed_storyboard` | `sk_seed_storyboard` | 通过 |
| `storyboard_visual_plan` | 护肤新品主视觉、镜头氛围、故事板 | `sk_seed_storyboard` | `sk_seed_storyboard` | 通过 |
| `storyboard_en` | product launch video storyboard | `sk_seed_storyboard` | `sk_seed_storyboard` | 通过 |
| `email_negative` | 给客户的道歉邮件 | 不命中 | 不命中，`no_route_hint_match` | 通过 |
| `invoice_negative` | 发票报销说明 | 不命中 | 不命中，`no_route_hint_match` | 通过 |
| `generic_chat` | 今天适合做什么运动 | 不命中 | 不命中，`no_route_hint_match` | 通过 |

## 当前可路由池

本地清理后，`published` 可路由 Skill 只剩 seed 真实 Skill：

- `sk_seed_storyboard`
- `skill_key=storyboard`
- `route_hints={"intent":"storyboard","keywords":"storyboard,故事板,分镜,镜头,广告短片,广告片,视觉方案,主视觉","priority":"80","negative_keywords":"邮件,道歉信,合同,发票"}`

## 验证命令

| 命令 | 结果 |
| --- | --- |
| `go test ./services/agent/internal/runtime/skill ./services/agent/internal/application/workbench ./services/agent/internal/runtime/modeltool ./services/agent/internal/infra/config ./services/agent/cmd/agent -count=1` | 通过 |
| `bash -n scripts/eval-skill-routing.sh scripts/e2e-admin-agent-full-flow.sh scripts/e2e-agent-runtime-config.sh scripts/e2e-deepseek-v4-flash-real-output.sh` | 通过 |
| `scripts/eval-skill-routing.sh` | 通过，6/6 |

## 后续建议

- 每新增一个后台 Skill，都应补 3 类场景：强命中、相邻能力误命中、无关负例。
- 后续如果 Skill 数量变多，应把 `route_hints` 从自由 JSON 进一步产品化为后台表单字段：`keywords`、`negative_keywords`、`priority`、`resource_type`、`examples`。
- 语义路由可以接 DeepSeek 分类或 embedding，但应以本脚本矩阵作为回归门禁，避免不可解释误命中。
