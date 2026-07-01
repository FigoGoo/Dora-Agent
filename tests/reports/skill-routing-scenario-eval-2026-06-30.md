# Skill 路由真实业务场景评估报告

日期：2026-06-30

结论：通过。已把可路由 seed 从单一 `Storyboard` 扩到 9 个真实业务 Skill，并用 24 条场景矩阵验证命中率。当前结果支持先继续使用可解释 `route_hints` 规则路由，暂不接 DeepSeek 分类器。

## 本轮发现的问题与处理

| 问题 | 影响 | 处理 | 是否偏离方向 |
| --- | --- | --- | --- |
| 评估矩阵只有 storyboard 单场景 | 只能证明“不会乱兜底”，不能证明多 Skill 间的真实命中率 | `scripts/eval-skill-routing.sh` 扩到 24 条场景：13 条强命中、8 条近邻混淆、3 条无关负例 | 未偏离；仍围绕“系统 Skill 如何按用户语义路由” |
| 真实业务 Skill seed 不足 | 后台看起来有能力，前台用户语义无法命中对应 Skill | `tests/business/seed/business_core_seed.sql` 新增商品文案、品牌策略、社媒日历、SEO 长文、会议纪要、客服回复、经营数据分析、出图提示词 8 个 seed Skill，加上 storyboard 共 9 个 | 未偏离；是路由命中率评估底座 |
| “不要 X / 不是 X，而是 Y” 会被纯关键词误杀或误加分 | 用户自然排除前一个意图时，目标 Skill 可能被 `negative_keywords` 屏蔽，或被被否定的 Skill 抢走 | `services/agent/internal/runtime/skill/router.go` 增加否定语境窗口；否定语境中的 hint 不参与正向计分，也不触发负向屏蔽 | 未偏离；属于语义路由必要边界 |
| seed 重放会打印 `business_spaces` / `credit_accounts` 主键冲突 | 后续扩场景时容易把老 seed 幂等噪音误判成新增问题 | 两个基础 seed 块改成按固定 `id` upsert，`psql -v ON_ERROR_STOP=1` 重放通过 | 未偏离；减少验证噪音 |

## 当前 seed Skill 池

| Skill | 场景 | 输出元素 | active 测试用例 |
| --- | --- | --- | --- |
| `sk_seed_storyboard` | 广告短片分镜 / 故事板 | `image_ref`、`storyboard` | 3 |
| `sk_seed_product_copy` | 商品卖点、详情页、直播间转化文案 | `rich_text`、`tag_group` | 3 |
| `sk_seed_brand_strategy` | 品牌定位、人群、差异化、品牌语气 | `structured_object`、`long_text` | 3 |
| `sk_seed_social_calendar` | 抖音、小红书、公众号内容日历 | `list`、`tag_group` | 3 |
| `sk_seed_seo_article` | SEO 关键词、文章大纲、选购指南 | `rich_text`、`tag_group` | 3 |
| `sk_seed_meeting_summary` | 会议纪要、决议、待办、负责人 | `rich_text`、`list` | 3 |
| `sk_seed_support_reply` | 客诉、售后、退款、补偿建议 | `rich_text`、`structured_object` | 3 |
| `sk_seed_data_insight` | 转化率、客单价、ROI、投放分析 | `structured_object`、`list` | 3 |
| `sk_seed_image_prompt` | MJ / Midjourney / 海报出图提示词 | `prompt`、`parameter_group` | 3 |

本地 DB 验证：

```text
sk_seed_brand_strategy|brand_strategy|published|skv_seed_brand_strategy_100|3|2
sk_seed_data_insight|data_insight|published|skv_seed_data_insight_100|3|2
sk_seed_image_prompt|image_prompt|published|skv_seed_image_prompt_100|3|2
sk_seed_meeting_summary|meeting_summary|published|skv_seed_meeting_summary_100|3|2
sk_seed_product_copy|product_copy|published|skv_seed_product_copy_100|3|2
sk_seed_seo_article|seo_article|published|skv_seed_seo_article_100|3|2
sk_seed_social_calendar|social_calendar|published|skv_seed_social_calendar_100|3|2
sk_seed_storyboard|storyboard|published|skv_seed_storyboard_100|3|2
sk_seed_support_reply|support_reply|published|skv_seed_support_reply_100|3|2
```

## 场景矩阵

执行命令：

```bash
scripts/eval-skill-routing.sh
```

失败基线：新增矩阵后、扩 seed 前为 7/24，准确率 0.29。

修复后结果：24/24，准确率 1.00。

| 场景 | 期望 | 结果 |
| --- | --- | --- |
| `storyboard_cn` / `storyboard_visual_plan` / `storyboard_en` | `sk_seed_storyboard` | 通过 |
| `product_copy_cn` / `product_copy_detail_page` | `sk_seed_product_copy` | 通过 |
| `brand_strategy_cn` / `brand_strategy_en` | `sk_seed_brand_strategy` | 通过 |
| `social_calendar_cn` | `sk_seed_social_calendar` | 通过 |
| `seo_article_cn` | `sk_seed_seo_article` | 通过 |
| `meeting_summary_cn` | `sk_seed_meeting_summary` | 通过 |
| `customer_support_reply` | `sk_seed_support_reply` | 通过 |
| `data_insight_cn` | `sk_seed_data_insight` | 通过 |
| `image_prompt_cn` | `sk_seed_image_prompt` | 通过 |
| `storyboard_vs_copy` / `copy_vs_brand` / `brand_vs_social` / `seo_vs_social` / `summary_vs_support` / `support_vs_copy` / `data_vs_seo` / `prompt_vs_storyboard` | 对应目标 Skill | 通过 |
| `email_negative` / `invoice_negative` / `generic_chat` | 不命中，`no_route_hint_match` | 通过 |

## DeepSeek 分类器判断

当前暂不接 DeepSeek 分类器，依据如下：

- 9 个真实业务 Skill、24 条正反例矩阵已达到 1.00 准确率。
- 当前规则路由可解释，可直接在报告和事件里看到 `matched_reason=route_hint:keywords`。
- 分类器会新增延迟、成本、不可解释误判和回归难度；在当前候选池规模下收益不足。
- 更大的真实 Skill 池会先暴露另一个问题：Agent 当前 `ListRoutableSkills` 只拉前 10 个候选。Skill 数量继续增加时，应先做分页/候选召回或后台 route_hints 产品化，再考虑 DeepSeek 二阶段分类。

## 已知边界

- 本轮验证的是 Skill 语义路由与 seed 可发布结构，不声称文本类 Skill 已完成端到端文本资产生成。
- Agent 当前生成链路仍偏 image-first；文本类 output elements 会进入 `skill_selection` snapshot，但最终产物组织仍需要后续支持文本/结构化 asset carrier。
- `scripts/eval-skill-routing.sh` 已改为调用 `services/agent/cmd/skill-routing-eval`，评估命令内部复用生产 `services/agent/internal/runtime/skill` Router，不再维护脚本内嵌路由逻辑。

## 验证命令

| 命令 | 结果 |
| --- | --- |
| `docker exec -i doraigc-postgres psql -v ON_ERROR_STOP=1 -U doraigc -d doraigc < tests/business/seed/business_core_seed.sql` | 通过 |
| `scripts/eval-skill-routing.sh` | 2026-06-30 通过，24/24；2026-07-01 当前环境缺少 `doraigc-postgres` 容器，未重跑 DB 导出段 |
| `go test ./services/agent/cmd/skill-routing-eval ./services/agent/internal/runtime/skill` | 通过 |
| `go test ./services/agent/internal/runtime/skill -count=1` | 通过 |
| `bash -n scripts/eval-skill-routing.sh` | 通过 |

## 后续建议

- 后台新增真实 Skill 时，同时补强命中、近邻误命中、无关负例三类评估场景。
- 将 `route_hints` 产品化为后台字段：`keywords`、`negative_keywords`、`priority`、`examples`、`resource_type`。
- 当 published Skill 超过 10 个时，先修候选召回/分页，再评估是否加 DeepSeek 分类器。
