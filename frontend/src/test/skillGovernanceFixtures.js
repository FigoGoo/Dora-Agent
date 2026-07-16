import { SKILL_IDS, skillDefinitionFixture } from './skillFixtures.js';

export const SKILL_GOVERNANCE_IDS = Object.freeze({
  ...SKILL_IDS,
  secondSkill: '019f0000-0000-7000-8000-000000000130'
});

export function skillGovernanceListItemFixture(overrides = {}) {
  return {
    skill_id: SKILL_GOVERNANCE_IDS.skill,
    name: '剧情短片 Skill',
    summary: '当前发布内容',
    category: '短剧',
    published_at: '2026-07-14T10:00:00.123456789+08:00',
    governance_status: 'active',
    governance_epoch: 1,
    allowed_actions: ['suspend', 'offline'],
    ...overrides
  };
}

export function skillGovernanceListResponseFixture(overrides = {}) {
  return {
    items: [skillGovernanceListItemFixture()],
    next_cursor: null,
    request_id: SKILL_GOVERNANCE_IDS.request,
    ...overrides
  };
}

export function skillGovernanceDetailFixture(overrides = {}) {
  return {
    skill_id: SKILL_GOVERNANCE_IDS.skill,
    definition: skillDefinitionFixture(),
    published_at: '2026-07-14T10:00:00.123456789+08:00',
    governance_status: 'active',
    governance_epoch: 1,
    governance_etag: '"skill-governance-etag-1"',
    allowed_actions: ['suspend', 'offline'],
    ...overrides
  };
}

export function skillGovernanceDetailResponseFixture(overrides = {}) {
  return {
    skill: skillGovernanceDetailFixture(),
    request_id: SKILL_GOVERNANCE_IDS.request,
    ...overrides
  };
}

export function skillGovernanceDecisionFixture(overrides = {}) {
  return {
    skill_id: SKILL_GOVERNANCE_IDS.skill,
    governance_status: 'suspended',
    governance_epoch: 2,
    transitioned_at: '2026-07-14T10:05:00.123456789+08:00',
    governance_etag: '"skill-governance-etag-2"',
    allowed_actions: ['resume', 'offline'],
    ...overrides
  };
}

export function skillGovernanceDecisionResponseFixture(overrides = {}) {
  return {
    skill: skillGovernanceDecisionFixture(),
    request_id: SKILL_GOVERNANCE_IDS.request,
    ...overrides
  };
}
