import { SKILL_IDS, skillDefinitionFixture } from './skillFixtures.js';

export const SKILL_REVIEW_IDS = Object.freeze({
  ...SKILL_IDS,
  owner: '019f0000-0000-7000-8000-000000000126',
  snapshot: '019f0000-0000-7000-8000-000000000127',
  secondReview: '019f0000-0000-7000-8000-000000000128',
  secondSkill: '019f0000-0000-7000-8000-000000000129'
});

export function skillReviewQueueItemFixture(overrides = {}) {
  return {
    review_id: SKILL_REVIEW_IDS.review,
    skill_id: SKILL_REVIEW_IDS.skill,
    name: '剧情短片 Skill',
    summary: '冻结提交 sentinel A',
    category: '短剧',
    status: 'reviewing',
    submitted_at: '2026-07-14T10:00:00.123456789+08:00',
    allowed_actions: ['approve_and_publish'],
    ...overrides
  };
}

export function skillReviewQueueResponseFixture(overrides = {}) {
  return {
    items: [skillReviewQueueItemFixture()],
    next_cursor: null,
    request_id: SKILL_REVIEW_IDS.request,
    ...overrides
  };
}

export function currentPublishedFixture(overrides = {}) {
  return {
    published_snapshot_id: SKILL_REVIEW_IDS.snapshot,
    published_at: '2026-07-13T09:30:00.987654321Z',
    definition: skillDefinitionFixture({ summary: '当前已发布内容' }),
    ...overrides
  };
}

export function skillReviewDetailFixture(overrides = {}) {
  return {
    review_id: SKILL_REVIEW_IDS.review,
    skill_id: SKILL_REVIEW_IDS.skill,
    owner_user_id: SKILL_REVIEW_IDS.owner,
    status: 'reviewing',
    submitted_at: '2026-07-14T10:00:00.123456789+08:00',
    updated_at: '2026-07-14T10:00:01.123456789+08:00',
    definition: skillDefinitionFixture({ summary: '冻结提交 sentinel A' }),
    current_published: null,
    comparison: { has_current_published: false, same_content: false },
    review_etag: '"skill-review-etag-1"',
    allowed_actions: ['approve_and_publish'],
    ...overrides
  };
}

export function skillReviewDetailResponseFixture(overrides = {}) {
  return {
    review: skillReviewDetailFixture(),
    request_id: SKILL_REVIEW_IDS.request,
    ...overrides
  };
}

export function skillReviewDecisionResponseFixture(overrides = {}) {
  return {
    review: {
      review_id: SKILL_REVIEW_IDS.review,
      skill_id: SKILL_REVIEW_IDS.skill,
      status: 'approved',
      published_snapshot_id: SKILL_REVIEW_IDS.snapshot,
      decided_at: '2026-07-14T10:05:00.123456789+08:00',
      allowed_actions: []
    },
    request_id: SKILL_REVIEW_IDS.request,
    ...overrides
  };
}
