import { describe, expect, it } from 'vitest';
import {
  SKILL_GOVERNANCE_IDS,
  skillGovernanceDecisionFixture,
  skillGovernanceDecisionResponseFixture,
  skillGovernanceDetailFixture,
  skillGovernanceDetailResponseFixture,
  skillGovernanceListItemFixture,
  skillGovernanceListResponseFixture
} from '../../test/skillGovernanceFixtures.js';
import {
  parseGovernanceDecisionResponse,
  parseGovernanceDetailResponse,
  parseGovernanceListResponse,
  SKILL_GOVERNANCE_ACTIONS,
  SKILL_GOVERNANCE_STATUSES
} from './governanceContract.js';

describe('Governor frozen DTO contract', () => {
  it('exports the frozen status and action sets in contract order', () => {
    expect(SKILL_GOVERNANCE_STATUSES).toEqual(['active', 'suspended', 'offline']);
    expect(SKILL_GOVERNANCE_ACTIONS).toEqual(['suspend', 'resume', 'offline']);
    expect(Object.isFrozen(SKILL_GOVERNANCE_STATUSES)).toBe(true);
    expect(Object.isFrozen(SKILL_GOVERNANCE_ACTIONS)).toBe(true);
  });

  it('parses an exact list page with a Base64URL cursor', () => {
    const parsed = parseGovernanceListResponse(skillGovernanceListResponseFixture({ next_cursor: 'opaque_cursor-2' }));

    expect(parsed).toMatchObject({ nextCursor: 'opaque_cursor-2', requestID: SKILL_GOVERNANCE_IDS.request });
    expect(parsed.items[0]).toMatchObject({
      skillID: SKILL_GOVERNANCE_IDS.skill,
      governanceStatus: 'active',
      governanceEpoch: 1,
      allowedActions: ['suspend', 'offline']
    });
  });

  it('parses the complete current published Definition and strong governance ETag', () => {
    const parsed = parseGovernanceDetailResponse(skillGovernanceDetailResponseFixture());

    expect(parsed.skill.definition.name).toBe('剧情短片 Skill');
    expect(parsed.skill.publishedAt).toBe('2026-07-14T10:00:00.123456789+08:00');
    expect(parsed.skill.governanceETag).toBe('"skill-governance-etag-1"');
  });

  it.each([
    ['active', 3, ['suspend', 'offline']],
    ['suspended', 2, ['resume', 'offline']],
    ['offline', 2, []],
    ['offline', 3, []]
  ])('accepts %s only with its exact epoch/action projection', (status, epoch, actions) => {
    const parsed = parseGovernanceListResponse(skillGovernanceListResponseFixture({
      items: [skillGovernanceListItemFixture({
        governance_status: status,
        governance_epoch: epoch,
        allowed_actions: actions
      })]
    }));
    expect(parsed.items[0]).toMatchObject({ governanceStatus: status, governanceEpoch: epoch, allowedActions: actions });
  });

  it('parses a decision with its transition time and post-transition actions', () => {
    expect(parseGovernanceDecisionResponse(skillGovernanceDecisionResponseFixture()).skill).toMatchObject({
      skillID: SKILL_GOVERNANCE_IDS.skill,
      governanceStatus: 'suspended',
      governanceEpoch: 2,
      transitionedAt: '2026-07-14T10:05:00.123456789+08:00',
      allowedActions: ['resume', 'offline']
    });
  });

  it('accepts leap-day nanoseconds and the largest numeric offset hour', () => {
    const parsed = parseGovernanceDetailResponse(skillGovernanceDetailResponseFixture({
      skill: skillGovernanceDetailFixture({ published_at: '2024-02-29T23:59:59.123456789+23:59' })
    }));
    expect(parsed.skill.publishedAt).toBe('2024-02-29T23:59:59.123456789+23:59');
  });

  it.each([
    [() => ({ ...skillGovernanceListResponseFixture(), extra: true }), '字段集合'],
    [() => skillGovernanceListResponseFixture({ items: null }), 'items'],
    [() => skillGovernanceListResponseFixture({ next_cursor: 'not padded=' }), 'Base64URL'],
    [() => skillGovernanceListResponseFixture({ next_cursor: '' }), 'Base64URL'],
    [() => skillGovernanceListResponseFixture({ items: [skillGovernanceListItemFixture({ extra: true })] }), '字段集合'],
    [() => skillGovernanceListResponseFixture({ items: [
      skillGovernanceListItemFixture(),
      skillGovernanceListItemFixture()
    ] }), '重复 skill_id'],
    [() => skillGovernanceListResponseFixture({ items: [
      skillGovernanceListItemFixture({ skill_id: '019F0000-0000-7000-8000-000000000121' })
    ] }), 'UUIDv7'],
    [() => skillGovernanceListResponseFixture({ items: [
      skillGovernanceListItemFixture({ governance_status: 'disabled' })
    ] }), '未知治理状态'],
    [() => skillGovernanceListResponseFixture({ items: [
      skillGovernanceListItemFixture({ governance_epoch: 0 })
    ] }), '正安全整数'],
    [() => skillGovernanceListResponseFixture({ items: [
      skillGovernanceListItemFixture({ governance_epoch: 2 })
    ] }), '迁移历史'],
    [() => skillGovernanceListResponseFixture({ items: [
      skillGovernanceListItemFixture({ governance_status: 'suspended', governance_epoch: 2, allowed_actions: ['offline', 'resume'] })
    ] }), '状态不一致'],
    [() => skillGovernanceListResponseFixture({ items: [
      skillGovernanceListItemFixture({ governance_status: 'offline', governance_epoch: 2, allowed_actions: ['resume'] })
    ] }), '状态不一致'],
    [() => skillGovernanceListResponseFixture({ items: [
      skillGovernanceListItemFixture({ published_at: '2026-04-31T00:00:00Z' })
    ] }), 'RFC3339Nano']
  ])('fails closed on malformed list DTOs', (fixture, message) => {
    expect(() => parseGovernanceListResponse(fixture())).toThrow(message);
  });

  it.each([
    [{ governance_etag: 'W/"weak"' }, 'strong ETag'],
    [{ governance_etag: 'unquoted' }, 'strong ETag'],
    [{ governance_etag: '"tab\tetag"' }, 'strong ETag'],
    [{ governance_etag: '"delete\u007fetag"' }, 'strong ETag'],
    [{ published_at: '2026-02-29T00:00:00Z' }, 'RFC3339Nano'],
    [{ governance_status: 'suspended', governance_epoch: 2, allowed_actions: ['suspend', 'offline'] }, '状态不一致']
  ])('rejects malformed detail fields %o', (overrides, message) => {
    expect(() => parseGovernanceDetailResponse(skillGovernanceDetailResponseFixture({
      skill: skillGovernanceDetailFixture(overrides)
    }))).toThrow(message);
  });

  it('rejects extra fields at every Definition boundary', () => {
    const top = skillGovernanceDetailFixture();
    top.definition = { ...top.definition, forged: true };
    expect(() => parseGovernanceDetailResponse(skillGovernanceDetailResponseFixture({ skill: top })))
      .toThrow('字段集合');

    const capability = skillGovernanceDetailFixture();
    capability.definition.plan_creation_spec = { ...capability.definition.plan_creation_spec, forged: true };
    expect(() => parseGovernanceDetailResponse(skillGovernanceDetailResponseFixture({ skill: capability })))
      .toThrow('字段集合');

    const market = skillGovernanceDetailFixture();
    market.definition.market_listing = { ...market.definition.market_listing, forged: true };
    expect(() => parseGovernanceDetailResponse(skillGovernanceDetailResponseFixture({ skill: market })))
      .toThrow('字段集合');
  });

  it.each([
    [{ governance_status: 'active', governance_epoch: 1, allowed_actions: ['suspend', 'offline'] }, '至少为 2'],
    [{ skill_id: 'not-a-uuid' }, 'UUIDv7'],
    [{ governance_status: 'active', governance_epoch: 3, allowed_actions: [] }, '状态不一致'],
    [{ transitioned_at: '2026-01-01T24:00:00Z' }, 'RFC3339Nano']
  ])('rejects drifted decision responses %o', (overrides, message) => {
    expect(() => parseGovernanceDecisionResponse(skillGovernanceDecisionResponseFixture({
      skill: skillGovernanceDecisionFixture(overrides)
    }))).toThrow(message);
  });
});
