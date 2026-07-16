import { requestJSON } from '../../platform/api/apiClient.js';
import {
  parseSkillReviewDecisionResponse,
  parseSkillReviewDetailResponse,
  parseSkillReviewQueueResponse,
  SkillReviewContractError,
  SKILL_REVIEW_DECISION_APPROVED
} from './skillReviewContract.js';

export const SKILL_REVIEW_QUEUE_PATH = '/api/v1/admin/skill-reviews';
const UUID_V7_PATTERN = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;

export async function listSkillReviews({ cursor, signal } = {}) {
  const query = new URLSearchParams({ status: 'reviewing' });
  if (cursor != null) {
    if (!String(cursor)) throw new TypeError('Skill 审核 cursor 不能为空');
    query.set('cursor', String(cursor));
  }
  const payload = await requestJSON(`${SKILL_REVIEW_QUEUE_PATH}?${query.toString()}`, { method: 'GET', signal });
  return parseSkillReviewQueueResponse(payload);
}

export async function getSkillReview(reviewID, { signal } = {}) {
  const payload = await requestJSON(skillReviewPath(reviewID), { method: 'GET', signal });
  const result = parseSkillReviewDetailResponse(payload);
  assertReviewID(result.review.reviewID, reviewID);
  return result;
}

export async function approveSkillReview({ reviewID, idempotencyKey, reviewETag, csrfToken, signal } = {}) {
  if (!idempotencyKey) throw new TypeError('批准 Skill 审核需要 Idempotency-Key');
  if (!reviewETag) throw new TypeError('批准 Skill 审核需要 review_etag');
  if (!csrfToken) throw new TypeError('批准 Skill 审核需要内存 CSRF Token');
  const payload = await requestJSON(`${skillReviewPath(reviewID)}/decisions`, {
    method: 'POST',
    headers: {
      'Idempotency-Key': String(idempotencyKey),
      'If-Match': String(reviewETag),
      'X-CSRF-Token': String(csrfToken)
    },
    body: JSON.stringify({ decision: SKILL_REVIEW_DECISION_APPROVED }),
    signal
  });
  const result = parseSkillReviewDecisionResponse(payload);
  assertReviewID(result.review.reviewID, reviewID);
  return result;
}

export function createSkillReviewDecisionKey() {
  if (typeof globalThis.crypto?.randomUUID !== 'function') {
    throw new Error('当前环境不支持安全生成 Reviewer Idempotency-Key');
  }
  return `skill-review-decision-${globalThis.crypto.randomUUID()}`;
}

function skillReviewPath(reviewID) {
  if (!UUID_V7_PATTERN.test(String(reviewID || ''))) {
    throw new TypeError('Reviewer API 需要规范小写 UUIDv7 review_id');
  }
  return `${SKILL_REVIEW_QUEUE_PATH}/${reviewID}`;
}

function assertReviewID(actual, expected) {
  if (actual !== expected) {
    throw new SkillReviewContractError('Reviewer 响应 review_id 与请求资源不一致', 'review.review_id');
  }
}
