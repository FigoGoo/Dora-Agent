import { requestJSON } from '../../platform/api/apiClient.js';
import {
  compareExamples,
  compareUTF8,
  parseOwnerSkillListResponse,
  parseOwnerSkillResponse,
  parseReviewSubmissionResponse,
  parseSkillDefinition
} from './skillContract.js';

export const OWNER_SKILLS_PATH = '/api/v1/skills';

export async function createOwnerSkill({ definition, idempotencyKey, csrfToken, signal } = {}) {
  if (!idempotencyKey) throw new TypeError('createOwnerSkill 需要 Idempotency-Key');
  const headers = mutationHeaders({ idempotencyKey, csrfToken });
  const payload = await requestJSON(OWNER_SKILLS_PATH, {
    method: 'POST',
    headers,
    body: JSON.stringify({ definition: requestDefinition(definition) }),
    signal
  });
  return parseOwnerSkillResponse(payload);
}

export async function listOwnerSkills({ cursor, signal } = {}) {
  const query = new URLSearchParams({ scope: 'mine' });
  if (cursor) query.set('cursor', String(cursor));
  const payload = await requestJSON(`${OWNER_SKILLS_PATH}?${query.toString()}`, { method: 'GET', signal });
  return parseOwnerSkillListResponse(payload);
}

export async function getOwnerSkill(skillID, { signal } = {}) {
  const payload = await requestJSON(ownerSkillPath(skillID), { method: 'GET', signal });
  return parseOwnerSkillResponse(payload);
}

export async function updateOwnerSkillDraft({ skillID, definition, draftETag, csrfToken, signal } = {}) {
  if (!draftETag) throw new TypeError('updateOwnerSkillDraft 需要 draft_etag');
  const headers = mutationHeaders({ csrfToken });
  headers['If-Match'] = String(draftETag);
  const payload = await requestJSON(`${ownerSkillPath(skillID)}/draft`, {
    method: 'PUT',
    headers,
    body: JSON.stringify({ definition: requestDefinition(definition) }),
    signal
  });
  return parseOwnerSkillResponse(payload);
}

export async function submitOwnerSkillReview({ skillID, idempotencyKey, draftETag, csrfToken, signal } = {}) {
  if (!idempotencyKey) throw new TypeError('submitOwnerSkillReview 需要 Idempotency-Key');
  if (!draftETag) throw new TypeError('submitOwnerSkillReview 需要 draft_etag');
  const headers = mutationHeaders({ idempotencyKey, csrfToken });
  headers['If-Match'] = String(draftETag);
  const payload = await requestJSON(`${ownerSkillPath(skillID)}/reviews`, {
    method: 'POST',
    headers,
    signal
  });
  return parseReviewSubmissionResponse(payload);
}

export function createSkillCommandKey(scope = 'skill') {
  if (typeof globalThis.crypto?.randomUUID !== 'function') {
    throw new Error('当前环境不支持安全生成 Idempotency-Key');
  }
  return `${scope}-${globalThis.crypto.randomUUID()}`;
}

function ownerSkillPath(skillID) {
  if (!skillID) throw new TypeError('Owner Skill API 需要 skill_id');
  return `${OWNER_SKILLS_PATH}/${encodeURIComponent(skillID)}`;
}

function mutationHeaders({ idempotencyKey, csrfToken } = {}) {
  if (!csrfToken) throw new TypeError('Owner Skill 写请求需要内存 CSRF Token');
  const headers = { 'X-CSRF-Token': csrfToken };
  if (idempotencyKey != null) {
    if (!idempotencyKey) throw new TypeError('Owner Skill 命令需要 Idempotency-Key');
    headers['Idempotency-Key'] = String(idempotencyKey);
  }
  return headers;
}

function requestDefinition(definition) {
  const candidate = definition && typeof definition === 'object' && !Array.isArray(definition)
    ? { ...definition }
    : definition;
  if (candidate && Array.isArray(candidate.tags) && candidate.tags.every((item) => typeof item === 'string')) {
    candidate.tags = [...candidate.tags].sort(compareUTF8);
  }
  if (candidate && Array.isArray(candidate.starter_prompts) && candidate.starter_prompts.every((item) => typeof item === 'string')) {
    candidate.starter_prompts = [...candidate.starter_prompts].sort(compareUTF8);
  }
  if (candidate && Array.isArray(candidate.examples) && candidate.examples.every((item) => (
    item && typeof item === 'object' && typeof item.input === 'string' && typeof item.output === 'string'
  ))) {
    candidate.examples = [...candidate.examples].sort(compareExamples);
  }
  return parseSkillDefinition(candidate);
}
