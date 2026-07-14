import { describe, expect, it } from 'vitest';
import {
  SKILL_MARKET_CAPABILITY_KEYS,
  parseSkillMarketDetailResponse,
  parseSkillMarketListResponse
} from './skillMarketContract.js';
import {
  SKILL_MARKET_IDS,
  skillMarketDetailResponseFixture,
  skillMarketListItemFixture,
  skillMarketListResponseFixture
} from '../../test/skillMarketFixtures.js';

describe('Skill Market public contract', () => {
  it('parses the exact list and detail projections without Owner-only fields', () => {
    const list = parseSkillMarketListResponse(skillMarketListResponseFixture());
    const detail = parseSkillMarketDetailResponse(skillMarketDetailResponseFixture());

    expect(list).toMatchObject({
      nextCursor: null,
      requestID: SKILL_MARKET_IDS.request,
      items: [{
        skillID: SKILL_MARKET_IDS.skill,
        publisher: { publisherID: SKILL_MARKET_IDS.publisher, displayName: 'Dora Creator' },
        declaredCapabilityKeys: ['analyze_materials', 'write_prompts']
      }]
    });
    expect(detail.skill).toMatchObject({
      skillID: SKILL_MARKET_IDS.skill,
      inputDescription: '输入创作主题与目标媒体。',
      marketDetail: '公开市场详情。'
    });
    expect(detail.skill).not.toHaveProperty('invocationRules');
    expect(detail.skill).not.toHaveProperty('governanceStatus');
  });

  it('accepts every declared capability only in the frozen product order', () => {
    const response = skillMarketListResponseFixture({
      items: [skillMarketListItemFixture({ declared_capability_keys: [...SKILL_MARKET_CAPABILITY_KEYS] })]
    });
    expect(parseSkillMarketListResponse(response).items[0].declaredCapabilityKeys)
      .toEqual(SKILL_MARKET_CAPABILITY_KEYS);

    response.items[0].declared_capability_keys = ['write_prompts', 'analyze_materials'];
    expect(() => parseSkillMarketListResponse(response)).toThrow(/重复或乱序/);
    response.items[0].declared_capability_keys = ['unknown_capability'];
    expect(() => parseSkillMarketListResponse(response)).toThrow(/未知能力/);
  });

  it('rejects extra fields, duplicate IDs, invalid UUID/time, null arrays and non-null covers', () => {
    const cases = [
      () => skillMarketListResponseFixture({ unexpected: true }),
      () => skillMarketListResponseFixture({
        items: [skillMarketListItemFixture(), skillMarketListItemFixture()]
      }),
      () => skillMarketListResponseFixture({
        items: [skillMarketListItemFixture({ skill_id: 'not-a-uuid' })]
      }),
      () => skillMarketListResponseFixture({
        items: [skillMarketListItemFixture({ published_at: '2026-02-30T10:00:00Z' })]
      }),
      () => skillMarketListResponseFixture({ items: [skillMarketListItemFixture({ tags: null })] }),
      () => skillMarketListResponseFixture({
        items: [skillMarketListItemFixture({ declared_capability_keys: null })]
      }),
      () => skillMarketListResponseFixture({ items: [skillMarketListItemFixture({ cover_asset: {} })] })
    ];

    cases.forEach((createPayload) => {
      expect(() => parseSkillMarketListResponse(createPayload())).toThrow(expect.objectContaining({
        code: 'INVALID_SKILL_MARKET_RESPONSE',
        status: 502
      }));
    });
  });

  it('enforces exact publisher and detail shapes without inventing publisher normalization', () => {
    const displayName = '  Dora Creator  ';
    const response = skillMarketListResponseFixture({
      items: [skillMarketListItemFixture({
        publisher: { publisher_id: SKILL_MARKET_IDS.publisher, display_name: displayName }
      })]
    });
    expect(parseSkillMarketListResponse(response).items[0].publisher.displayName).toBe(displayName);

    response.items[0].publisher.email = 'private@example.com';
    expect(() => parseSkillMarketListResponse(response)).toThrow(/字段不符合/);

    const detail = skillMarketDetailResponseFixture();
    detail.skill.invocation_rules = '不应公开';
    expect(() => parseSkillMarketDetailResponse(detail)).toThrow(/字段不符合/);
  });

  it('rejects null detail collections and invalid or padded cursors', () => {
    const detail = skillMarketDetailResponseFixture();
    detail.skill.examples = null;
    expect(() => parseSkillMarketDetailResponse(detail)).toThrow(/examples 必须为数组/);

    expect(() => parseSkillMarketListResponse(skillMarketListResponseFixture({ next_cursor: 'abc=' })))
      .toThrow(/Base64URL/);
    expect(() => parseSkillMarketListResponse(skillMarketListResponseFixture({ next_cursor: '' })))
      .toThrow(/不能为空/);
  });
});
