import { createEmptySkillDefinition } from '../features/skills/skillContract.js';

export const SKILL_IDS = Object.freeze({
  skill: '019f0000-0000-7000-8000-000000000121',
  review: '019f0000-0000-7000-8000-000000000122',
  request: '019f0000-0000-7000-8000-000000000123'
});

export function skillDefinitionFixture(overrides = {}) {
  const definition = createEmptySkillDefinition();
  Object.assign(definition, {
    name: '剧情短片 Skill',
    summary: '把故事目标整理成可执行的短片创作流程。',
    category: '短剧',
    tags: ['剧情', '短片'],
    input_description: '文字、图片或视频素材。',
    output_description: '故事板、Prompt 与候选媒体。',
    invocation_rules: '当用户明确要制作剧情短片时调用。',
    examples: [{ input: '制作雨夜重逢短片', output: '生成完整短片方案' }],
    starter_prompts: ['制作一支雨夜重逢短片'],
    market_listing: {
      cover_asset_id: null,
      detail: '适合剧情短片创作。',
      copyright_notice: '请确保素材版权清晰。',
      user_notice: '生成前请核对关键人物设定。'
    }
  });
  Object.keys(definition).forEach((field) => {
    if (definition[field]?.applicability === 'enabled') {
      definition[field] = { applicability: 'enabled', guidance: `${field} guidance`, not_applicable_reason: '' };
    }
  });
  return { ...definition, ...overrides };
}

export function ownerSkillFixture(overrides = {}) {
  const hasUnpublishedChanges = overrides.has_unpublished_changes ?? true;
  const reviewStatus = Object.hasOwn(overrides, 'review_status') ? overrides.review_status : null;
  return {
    skill_id: SKILL_IDS.skill,
    definition: skillDefinitionFixture(),
    content_status: 'draft',
    has_unpublished_changes: hasUnpublishedChanges,
    review_status: reviewStatus,
    review_reason_code: null,
    review_updated_at: reviewStatus == null ? null : '2026-07-14T10:00:00+08:00',
    governance_status: 'active',
    allowed_actions: hasUnpublishedChanges && reviewStatus !== 'reviewing'
      ? ['edit_draft', 'submit_review']
      : ['edit_draft'],
    draft_etag: '"draft-etag-1"',
    ...overrides
  };
}

export function ownerSkillResponseFixture(overrides = {}) {
  return { skill: ownerSkillFixture(), request_id: SKILL_IDS.request, ...overrides };
}
