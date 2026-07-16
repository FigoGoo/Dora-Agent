export const SKILL_MARKET_IDS = Object.freeze({
  skill: '019f0000-0000-7000-8000-000000000101',
  secondSkill: '019f0000-0000-7000-8000-000000000104',
  publisher: '019f0000-0000-7000-8000-000000000102',
  request: '019f0000-0000-7000-8000-000000000103'
});

export function skillMarketListItemFixture(overrides = {}) {
  return {
    skill_id: SKILL_MARKET_IDS.skill,
    name: '短片提示词助手',
    summary: '帮助整理图片与视频提示词。',
    category: '视频',
    tags: ['提示词', '视频'],
    publisher: {
      publisher_id: SKILL_MARKET_IDS.publisher,
      display_name: 'Dora Creator'
    },
    published_at: '2026-07-14T10:00:00Z',
    cover_asset: null,
    declared_capability_keys: ['analyze_materials', 'write_prompts'],
    ...overrides
  };
}

export function skillMarketDetailFixture(overrides = {}) {
  return {
    ...skillMarketListItemFixture(),
    input_description: '输入创作主题与目标媒体。',
    output_description: '输出结构化提示词建议。',
    examples: [{ input: '城市夜景短片', output: '镜头与提示词示例' }],
    starter_prompts: ['帮我写一个城市夜景短片提示词'],
    market_detail: '公开市场详情。',
    copyright_notice: '请确保素材版权清晰。',
    user_notice: '生成前请核对关键设定。',
    ...overrides
  };
}

export function skillMarketListResponseFixture(overrides = {}) {
  return {
    items: [skillMarketListItemFixture()],
    next_cursor: null,
    request_id: SKILL_MARKET_IDS.request,
    ...overrides
  };
}

export function skillMarketDetailResponseFixture(overrides = {}) {
  return {
    skill: skillMarketDetailFixture(),
    request_id: SKILL_MARKET_IDS.request,
    ...overrides
  };
}
