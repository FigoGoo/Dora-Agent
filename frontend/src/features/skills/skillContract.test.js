import { describe, expect, it } from 'vitest';
import { ownerSkillFixture, ownerSkillResponseFixture, SKILL_IDS, skillDefinitionFixture } from '../../test/skillFixtures.js';
import {
  parseOwnerSkillListResponse,
  parseOwnerSkillResponse,
  parseReviewSubmissionResponse,
  parseSkillDefinition,
  compareUTF8,
  SKILL_CAPABILITY_FIELDS
} from './skillContract.js';

describe('W1 Owner Skill frozen contract', () => {
  it('parses all six independent capability fields and the exact Owner projection', () => {
    const parsed = parseOwnerSkillResponse(ownerSkillResponseFixture());

    expect(parsed.skill.skillID).toBe(SKILL_IDS.skill);
    expect(parsed.skill.draftETag).toBe('"draft-etag-1"');
    expect(parsed.skill.allowedActions).toEqual(['edit_draft', 'submit_review']);
    expect(SKILL_CAPABILITY_FIELDS.map((field) => parsed.skill.definition[field].applicability))
      .toEqual(Array(6).fill('enabled'));
  });

  it('accepts not_applicable only with an exclusive reason', () => {
    const definition = skillDefinitionFixture({
      write_prompts: { applicability: 'not_applicable', guidance: '', not_applicable_reason: '该 Skill 不生成 Prompt。' }
    });
    expect(parseSkillDefinition(definition).write_prompts).toMatchObject({ applicability: 'not_applicable' });

    expect(() => parseSkillDefinition(skillDefinitionFixture({
      write_prompts: { applicability: 'not_applicable', guidance: 'forged', not_applicable_reason: 'reason' }
    }))).toThrow('not_applicable');
  });

  it.each([
    [{ schema_version: 'skill_definition.v2' }, 'schema_version'],
    [{ public_tool_refs: [{ tool_id: 'forged' }] }, '公共 Tool'],
    [{ market_listing: {
      ...skillDefinitionFixture().market_listing,
      cover_asset_id: '019f0000-0000-7000-8000-000000000125'
    } }, 'cover_asset_id'],
    [{ plan_storyboard: { applicability: 'enabled', guidance: '', not_applicable_reason: '' } }, 'enabled']
  ])('fails closed on invalid Definition %o', (overrides, message) => {
    expect(() => parseSkillDefinition(skillDefinitionFixture(overrides))).toThrow(message);
  });

  it.each([
    [{ content_status: 'reviewing' }, 'content_status'],
    [{ review_status: 'pending' }, 'review_status'],
    [{ governance_status: 'deleted' }, 'governance_status'],
    [{ allowed_actions: ['submit_review', 'edit_draft'] }, '顺序'],
    [{ draft_etag: 'draft-etag-1' }, 'quoted opaque ETag']
  ])('fails closed on unknown or drifted Owner state %o', (overrides, message) => {
    expect(() => parseOwnerSkillResponse(ownerSkillResponseFixture({ skill: ownerSkillFixture(overrides) }))).toThrow(message);
  });

  it('parses list and review response envelopes with correlation IDs', () => {
    expect(parseOwnerSkillListResponse({
      items: [ownerSkillFixture()], next_cursor: 'cursor-2', request_id: SKILL_IDS.request
    })).toMatchObject({ nextCursor: 'cursor-2', requestID: SKILL_IDS.request });
    expect(parseReviewSubmissionResponse({
      skill: ownerSkillFixture({ review_status: 'reviewing' }),
      review_id: SKILL_IDS.review,
      request_id: SKILL_IDS.request
    })).toMatchObject({ reviewID: SKILL_IDS.review });
    expect(parseOwnerSkillListResponse({
      items: [], next_cursor: null, request_id: SKILL_IDS.request
    })).toMatchObject({ items: [], nextCursor: null });
  });

  it('distinguishes required explicit null fields from missing or empty values', () => {
    const missingReviewStatus = ownerSkillFixture();
    delete missingReviewStatus.review_status;
    expect(() => parseOwnerSkillResponse(ownerSkillResponseFixture({ skill: missingReviewStatus })))
      .toThrow('review_status');

    const missingReviewReason = ownerSkillFixture();
    delete missingReviewReason.review_reason_code;
    expect(() => parseOwnerSkillResponse(ownerSkillResponseFixture({ skill: missingReviewReason })))
      .toThrow('review_reason_code');

    expect(() => parseOwnerSkillListResponse({ items: [], request_id: SKILL_IDS.request }))
      .toThrow('next_cursor');
    expect(() => parseOwnerSkillListResponse({
      items: [], next_cursor: '', request_id: SKILL_IDS.request
    })).toThrow('next_cursor');
  });

  it('matches Go sort.Strings UTF-8 byte order for non-BMP values and examples', () => {
    const privateUse = '\uE000';
    const emoji = '😀';
    expect([privateUse, emoji].sort(compareUTF8)).toEqual([privateUse, emoji]);
    expect([privateUse, emoji].sort()).toEqual([emoji, privateUse]);

    const definition = skillDefinitionFixture({
      tags: [privateUse, emoji],
      starter_prompts: [privateUse, emoji],
      examples: [
        { input: privateUse, output: '输出' },
        { input: emoji, output: '输出' }
      ]
    });
    expect(parseSkillDefinition(definition).examples).toHaveLength(2);
    expect(() => parseSkillDefinition({ ...definition, tags: [emoji, privateUse] })).toThrow('UTF-8');
    expect(() => parseSkillDefinition({ ...definition, examples: [...definition.examples].reverse() })).toThrow('UTF-8');
  });

  it.each([
    [{ has_unpublished_changes: false }, 'draft 内容'],
    [{
      content_status: 'published',
      has_unpublished_changes: false,
      allowed_actions: ['edit_draft', 'submit_review']
    }, 'submit_review'],
    [{
      content_status: 'published',
      has_unpublished_changes: false,
      review_status: 'reviewing',
      review_updated_at: '2026-07-14T10:00:00+08:00',
      allowed_actions: ['edit_draft']
    }, '审核中的内容'],
    [{ review_reason_code: 'REASON_WITHOUT_REVIEW' }, '不得携带审核原因'],
    [{ review_status: 'approved', review_updated_at: null }, '审核更新时间']
  ])('rejects inconsistent Owner projection invariants %o', (overrides, message) => {
    expect(() => parseOwnerSkillResponse(ownerSkillResponseFixture({
      skill: ownerSkillFixture(overrides)
    }))).toThrow(message);
  });
});
