import { describe, expect, it } from 'vitest';
import {
  currentPublishedFixture,
  SKILL_REVIEW_IDS,
  skillReviewDecisionResponseFixture,
  skillReviewDetailFixture,
  skillReviewDetailResponseFixture,
  skillReviewQueueItemFixture,
  skillReviewQueueResponseFixture
} from '../../test/skillReviewFixtures.js';
import {
  parseSkillReviewDecisionResponse,
  parseSkillReviewDetailResponse,
  parseSkillReviewQueueResponse
} from './skillReviewContract.js';

describe('Reviewer frozen DTO contract', () => {
  it('parses the reviewing queue with its opaque cursor and exact action', () => {
    const parsed = parseSkillReviewQueueResponse(skillReviewQueueResponseFixture({ next_cursor: 'opaque-cursor-2' }));

    expect(parsed).toMatchObject({ nextCursor: 'opaque-cursor-2', requestID: SKILL_REVIEW_IDS.request });
    expect(parsed.items[0]).toMatchObject({
      reviewID: SKILL_REVIEW_IDS.review,
      skillID: SKILL_REVIEW_IDS.skill,
      status: 'reviewing',
      allowedActions: ['approve_and_publish']
    });
  });

  it('parses the complete frozen Definition and current published comparison', () => {
    const currentPublished = currentPublishedFixture();
    const parsed = parseSkillReviewDetailResponse(skillReviewDetailResponseFixture({
      review: skillReviewDetailFixture({
        current_published: currentPublished,
        comparison: { has_current_published: true, same_content: false }
      })
    }));

    expect(parsed.review.definition.summary).toContain('sentinel A');
    expect(parsed.review.currentPublished).toMatchObject({ publishedSnapshotID: SKILL_REVIEW_IDS.snapshot });
    expect(parsed.review.comparison).toEqual({ hasCurrentPublished: true, sameContent: false });
    expect(parsed.review.reviewETag).toBe('"skill-review-etag-1"');
  });

  it('accepts terminal detail only with an empty action set', () => {
    const parsed = parseSkillReviewDetailResponse(skillReviewDetailResponseFixture({
      review: skillReviewDetailFixture({ status: 'approved', allowed_actions: [] })
    }));
    expect(parsed.review).toMatchObject({ status: 'approved', allowedActions: [] });
  });

  it('accepts a real leap day, nanoseconds and the largest RFC3339 numeric offset hour', () => {
    const parsed = parseSkillReviewDetailResponse(skillReviewDetailResponseFixture({
      review: skillReviewDetailFixture({ submitted_at: '2024-02-29T23:59:59.123456789+23:59' })
    }));

    expect(parsed.review.submittedAt).toBe('2024-02-29T23:59:59.123456789+23:59');
  });

  it('parses only an approved decision containing a published snapshot ID', () => {
    expect(parseSkillReviewDecisionResponse(skillReviewDecisionResponseFixture()).review).toMatchObject({
      status: 'approved',
      publishedSnapshotID: SKILL_REVIEW_IDS.snapshot,
      allowedActions: []
    });
  });

  it.each([
    [() => ({ ...skillReviewQueueResponseFixture(), extra: true }), '字段集合'],
    [() => skillReviewQueueResponseFixture({ next_cursor: '' }), 'next_cursor'],
    [() => skillReviewQueueResponseFixture({ items: null }), 'items'],
    [() => skillReviewQueueResponseFixture({ items: [skillReviewQueueItemFixture({ status: 'pending' })] }), 'reviewing'],
    [() => skillReviewQueueResponseFixture({ items: [skillReviewQueueItemFixture({ allowed_actions: [] })] }), 'approve_and_publish'],
    [() => skillReviewQueueResponseFixture({ items: [
      skillReviewQueueItemFixture(),
      skillReviewQueueItemFixture({ skill_id: SKILL_REVIEW_IDS.secondSkill })
    ] }), '重复 review_id'],
    [() => skillReviewQueueResponseFixture({ items: [
      skillReviewQueueItemFixture(),
      skillReviewQueueItemFixture({ review_id: SKILL_REVIEW_IDS.secondReview })
    ] }), '重复 skill_id']
  ])('fails closed on invalid queue DTOs', (fixture, message) => {
    expect(() => parseSkillReviewQueueResponse(fixture())).toThrow(message);
  });

  it.each([
    [{ owner_user_id: '019F0000-0000-7000-8000-000000000126' }, 'UUIDv7'],
    [{ submitted_at: '2026-07-14T10:00:99Z' }, 'RFC3339Nano'],
    [{ submitted_at: '2026-02-29T00:00:00Z' }, 'RFC3339Nano'],
    [{ submitted_at: '2026-04-31T00:00:00Z' }, 'RFC3339Nano'],
    [{ submitted_at: '2026-01-01T24:00:00Z' }, 'RFC3339Nano'],
    [{ submitted_at: '2026-01-01T00:00:00+24:00' }, 'RFC3339Nano'],
    [{ review_etag: 'W/"weak"' }, 'strong ETag'],
    [{ review_etag: 'unquoted' }, 'strong ETag'],
    [{ review_etag: '"nul\u0000etag"' }, 'strong ETag'],
    [{ review_etag: '"tab\tetag"' }, 'strong ETag'],
    [{ review_etag: '"delete\u007fetag"' }, 'strong ETag'],
    [{ status: 'pending' }, '未知审核状态'],
    [{ status: 'approved', allowed_actions: ['approve_and_publish'] }, '必须为空数组'],
    [{ current_published: currentPublishedFixture() }, 'has_current_published'],
    [{ comparison: { has_current_published: false, same_content: true } }, 'same_content'],
    [{
      current_published: currentPublishedFixture(),
      comparison: { has_current_published: true, same_content: true }
    }, 'Definition 内容不一致'],
    [{
      current_published: currentPublishedFixture({ definition: skillReviewDetailFixture().definition }),
      comparison: { has_current_published: true, same_content: false }
    }, 'Definition 内容不一致']
  ])('fails closed on malformed or inconsistent detail field %o', (overrides, message) => {
    expect(() => parseSkillReviewDetailResponse(skillReviewDetailResponseFixture({
      review: skillReviewDetailFixture(overrides)
    }))).toThrow(message);
  });

  it('rejects extra fields at every Definition boundary', () => {
    const detail = skillReviewDetailFixture();
    detail.definition = { ...detail.definition, forged: true };
    expect(() => parseSkillReviewDetailResponse(skillReviewDetailResponseFixture({ review: detail })))
      .toThrow('字段集合');

    const nested = skillReviewDetailFixture();
    nested.definition.plan_creation_spec = { ...nested.definition.plan_creation_spec, forged: true };
    expect(() => parseSkillReviewDetailResponse(skillReviewDetailResponseFixture({ review: nested })))
      .toThrow('字段集合');

    const market = skillReviewDetailFixture();
    market.definition.market_listing = { ...market.definition.market_listing, forged: true };
    expect(() => parseSkillReviewDetailResponse(skillReviewDetailResponseFixture({ review: market })))
      .toThrow('字段集合');
  });

  it.each([
    [{ status: 'reviewing' }, 'approved'],
    [{ published_snapshot_id: 'not-a-uuid' }, 'UUIDv7'],
    [{ allowed_actions: ['approve_and_publish'] }, '空数组']
  ])('rejects drifted decision responses %o', (reviewOverrides, message) => {
    const fixture = skillReviewDecisionResponseFixture();
    fixture.review = { ...fixture.review, ...reviewOverrides };
    expect(() => parseSkillReviewDecisionResponse(fixture)).toThrow(message);
  });
});
