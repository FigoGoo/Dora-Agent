import { requestJSON, requestJSONWithResponse } from '../../platform/api/apiClient.js';
import {
  parseGovernanceDecisionResponse,
  parseGovernanceDetailResponse,
  parseGovernanceListResponse,
  SkillGovernanceContractError,
  SKILL_GOVERNANCE_ACTIONS,
  SKILL_GOVERNANCE_STATUSES
} from './governanceContract.js';

export const SKILL_GOVERNANCE_PATH = '/api/v1/admin/skill-governance';

const UUID_V7_PATTERN = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
const STRONG_ETAG_PATTERN = /^"[\x21\x23-\x7e\x80-\xff]+"$/;
const CURSOR_PATTERN = /^[A-Za-z0-9_-]{1,1024}$/;
const APPROVAL_REFERENCE_PATTERN = /^[A-Z][A-Z0-9_]{1,31}-[A-Za-z0-9][A-Za-z0-9._-]{0,126}$/;
const IDEMPOTENCY_KEY_PATTERN = /^[\x21-\x7e]{1,128}$/;
const REASON_CODES_BY_ACTION = Object.freeze({
  suspend: new Set([
    'content_safety',
    'copyright_risk',
    'privacy_risk',
    'fraud_or_abuse',
    'tool_dependency_risk',
    'policy_violation',
    'incident_containment'
  ]),
  resume: new Set([
    'risk_cleared',
    'appeal_approved',
    'incident_resolved',
    'dependency_restored',
    'policy_remediated'
  ]),
  offline: new Set([
    'content_safety',
    'copyright_risk',
    'privacy_risk',
    'fraud_or_abuse',
    'tool_dependency_risk',
    'policy_violation',
    'owner_request',
    'repeated_violation'
  ])
});
const RESULT_STATUS_BY_ACTION = Object.freeze({
  suspend: 'suspended',
  resume: 'active',
  offline: 'offline'
});

export async function listGovernanceSkills({ status, cursor, signal } = {}) {
  assertStatus(status);
  const query = new URLSearchParams({ status });
  if (cursor != null) {
    if (typeof cursor !== 'string' || !CURSOR_PATTERN.test(cursor)) {
      throw new TypeError('Skill 治理 cursor 必须为无填充 Base64URL opaque string');
    }
    query.set('cursor', cursor);
  }
  const payload = await requestJSON(`${SKILL_GOVERNANCE_PATH}?${query.toString()}`, { method: 'GET', signal });
  const result = parseGovernanceListResponse(payload);
  if (result.items.some((item) => item.governanceStatus !== status)) {
    throw new SkillGovernanceContractError('治理列表响应状态与请求筛选不一致', 'items');
  }
  return result;
}

export async function getGovernanceSkill(skillID, { signal } = {}) {
  const { payload, response } = await requestJSONWithResponse(governanceSkillPath(skillID), {
    method: 'GET',
    signal
  });
  const result = parseGovernanceDetailResponse(payload);
  assertSkillID(result.skill.skillID, skillID);
  assertResponseETag(result.skill.governanceETag, response);
  return result;
}

export async function decideGovernanceSkill({
  skillID,
  action,
  reasonCode,
  approvalReference,
  idempotencyKey,
  governanceETag,
  csrfToken,
  signal
} = {}) {
  const path = governanceSkillPath(skillID);
  assertDecisionInput({ action, reasonCode, approvalReference, idempotencyKey, governanceETag, csrfToken });
  const { payload, response } = await requestJSONWithResponse(`${path}/decisions`, {
    method: 'POST',
    headers: {
      'Idempotency-Key': idempotencyKey,
      'If-Match': governanceETag,
      'X-CSRF-Token': csrfToken
    },
    body: JSON.stringify({
      action,
      reason_code: reasonCode,
      approval_reference: approvalReference
    }),
    signal
  });
  const result = parseGovernanceDecisionResponse(payload);
  assertSkillID(result.skill.skillID, skillID);
  if (result.skill.governanceStatus !== RESULT_STATUS_BY_ACTION[action]) {
    throw new SkillGovernanceContractError('治理决定响应状态与请求 action 不一致', 'skill.governance_status');
  }
  assertResponseETag(result.skill.governanceETag, response);
  return result;
}

export function createGovernanceDecisionKey() {
  if (typeof globalThis.crypto?.randomUUID !== 'function') {
    throw new Error('当前环境不支持安全生成 Governor Idempotency-Key');
  }
  return `skill-governance-decision-${globalThis.crypto.randomUUID()}`;
}

function governanceSkillPath(skillID) {
  if (typeof skillID !== 'string' || !UUID_V7_PATTERN.test(skillID)) {
    throw new TypeError('Governor API 需要规范小写 UUIDv7 skill_id');
  }
  return `${SKILL_GOVERNANCE_PATH}/${skillID}`;
}

function assertStatus(status) {
  if (!SKILL_GOVERNANCE_STATUSES.includes(status)) {
    throw new TypeError(`Skill 治理 status 必须为 ${SKILL_GOVERNANCE_STATUSES.join('|')}`);
  }
}

function assertDecisionInput({ action, reasonCode, approvalReference, idempotencyKey, governanceETag, csrfToken }) {
  if (!SKILL_GOVERNANCE_ACTIONS.includes(action)) {
    throw new TypeError(`Skill 治理 action 必须为 ${SKILL_GOVERNANCE_ACTIONS.join('|')}`);
  }
  if (typeof reasonCode !== 'string' || !REASON_CODES_BY_ACTION[action].has(reasonCode)) {
    throw new TypeError('Skill 治理 reason_code 与 action 不符合冻结闭集');
  }
  if (typeof approvalReference !== 'string' || !APPROVAL_REFERENCE_PATTERN.test(approvalReference)) {
    throw new TypeError('Skill 治理 approval_reference 不符合冻结格式');
  }
  if (typeof idempotencyKey !== 'string' || !IDEMPOTENCY_KEY_PATTERN.test(idempotencyKey)) {
    throw new TypeError('Skill 治理需要可安全转发的 Idempotency-Key');
  }
  if (typeof governanceETag !== 'string' || !STRONG_ETAG_PATTERN.test(governanceETag)) {
    throw new TypeError('Skill 治理需要单个 quoted strong governance_etag');
  }
  if (typeof csrfToken !== 'string' || !csrfToken) {
    throw new TypeError('Skill 治理需要内存 CSRF Token');
  }
}

function assertSkillID(actual, expected) {
  if (actual !== expected) {
    throw new SkillGovernanceContractError('Governor 响应 skill_id 与请求资源不一致', 'skill.skill_id');
  }
}

function assertResponseETag(bodyETag, response) {
  const headerETag = response?.headers?.get?.('etag');
  if (typeof headerETag !== 'string' || !STRONG_ETAG_PATTERN.test(headerETag) || headerETag !== bodyETag) {
    throw new SkillGovernanceContractError('Governor HTTP ETag 与响应 Body governance_etag 不一致', 'ETag');
  }
}
