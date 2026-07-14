import { describe, expect, it, vi } from 'vitest';
import {
  SKILL_REVIEW_IDS,
  skillReviewDecisionResponseFixture,
  skillReviewDetailResponseFixture,
  skillReviewQueueResponseFixture
} from '../../test/skillReviewFixtures.js';
import {
  approveSkillReview,
  getSkillReview,
  listSkillReviews
} from './skillReviewApi.js';

describe('Reviewer API client', () => {
  it('uses the exact reviewing queue query and opaque cursor', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(jsonResponse(skillReviewQueueResponseFixture()))
      .mockResolvedValueOnce(jsonResponse(skillReviewQueueResponseFixture()));
    vi.stubGlobal('fetch', fetchMock);

    await listSkillReviews();
    await listSkillReviews({ cursor: 'cursor / + 2' });

    expect(fetchMock.mock.calls[0][0]).toBe('/api/v1/admin/skill-reviews?status=reviewing');
    expect(fetchMock.mock.calls[1][0]).toBe('/api/v1/admin/skill-reviews?status=reviewing&cursor=cursor+%2F+%2B+2');
    expect(fetchMock.mock.calls[0][1]).toMatchObject({ method: 'GET', credentials: 'include' });
  });

  it('loads the exact detail resource and rejects a mismatched response identity', async () => {
    const mismatch = skillReviewDetailResponseFixture();
    mismatch.review = { ...mismatch.review, review_id: SKILL_REVIEW_IDS.secondReview };
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(jsonResponse(skillReviewDetailResponseFixture()))
      .mockResolvedValueOnce(jsonResponse(mismatch));
    vi.stubGlobal('fetch', fetchMock);

    const result = await getSkillReview(SKILL_REVIEW_IDS.review);
    expect(result.review.reviewID).toBe(SKILL_REVIEW_IDS.review);
    expect(fetchMock.mock.calls[0][0]).toBe(`/api/v1/admin/skill-reviews/${SKILL_REVIEW_IDS.review}`);
    await expect(getSkillReview(SKILL_REVIEW_IDS.review)).rejects.toMatchObject({
      code: 'INVALID_SKILL_REVIEW_RESPONSE',
      status: 502
    });
  });

  it('posts the exact approved decision with CSRF, stable key and strong If-Match', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(skillReviewDecisionResponseFixture()));
    vi.stubGlobal('fetch', fetchMock);

    const result = await approveSkillReview({
      reviewID: SKILL_REVIEW_IDS.review,
      idempotencyKey: 'stable-decision-key',
      reviewETag: '"skill-review-etag-1"',
      csrfToken: 'csrf-reviewer'
    });

    expect(result.review.publishedSnapshotID).toBe(SKILL_REVIEW_IDS.snapshot);
    expect(fetchMock).toHaveBeenCalledWith(
      `/api/v1/admin/skill-reviews/${SKILL_REVIEW_IDS.review}/decisions`,
      expect.objectContaining({
        method: 'POST',
        credentials: 'include',
        headers: expect.objectContaining({
          'Idempotency-Key': 'stable-decision-key',
          'If-Match': '"skill-review-etag-1"',
          'X-CSRF-Token': 'csrf-reviewer',
          'Content-Type': 'application/json'
        }),
        body: JSON.stringify({ decision: 'approved' })
      })
    );
  });

  it('fails before fetch when identifiers or decision preconditions are absent', async () => {
    const fetchMock = vi.fn();
    vi.stubGlobal('fetch', fetchMock);

    await expect(getSkillReview('NOT-A-UUID')).rejects.toThrow('规范小写 UUIDv7');
    await expect(approveSkillReview({
      reviewID: SKILL_REVIEW_IDS.review,
      reviewETag: '"etag"',
      csrfToken: 'csrf'
    })).rejects.toThrow('Idempotency-Key');
    await expect(approveSkillReview({
      reviewID: SKILL_REVIEW_IDS.review,
      idempotencyKey: 'key',
      csrfToken: 'csrf'
    })).rejects.toThrow('review_etag');
    await expect(approveSkillReview({
      reviewID: SKILL_REVIEW_IDS.review,
      idempotencyKey: 'key',
      reviewETag: '"etag"'
    })).rejects.toThrow('CSRF');
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it('treats malformed successful decision bodies as an unknown-outcome class error', async () => {
    const malformed = skillReviewDecisionResponseFixture();
    malformed.review = { ...malformed.review, published_snapshot_id: 'invalid' };
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(jsonResponse(malformed)));

    await expect(approveSkillReview({
      reviewID: SKILL_REVIEW_IDS.review,
      idempotencyKey: 'stable-decision-key',
      reviewETag: '"skill-review-etag-1"',
      csrfToken: 'csrf-reviewer'
    })).rejects.toMatchObject({ status: 502, code: 'INVALID_SKILL_REVIEW_RESPONSE' });
  });
});

function jsonResponse(payload, status = 200) {
  return new Response(JSON.stringify(payload), { status, headers: { 'Content-Type': 'application/json' } });
}
