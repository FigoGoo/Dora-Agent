import {
  parseSkillDefinition,
  SKILL_CAPABILITY_FIELDS
} from '../skills/skillContract.js';

export const SKILL_REVIEW_ACTION_APPROVE = 'approve_and_publish';
export const SKILL_REVIEW_DECISION_APPROVED = 'approved';
export const SKILL_REVIEW_CAPABILITY_REQUIRED_CODE = 'SKILL_REVIEW_CAPABILITY_REQUIRED';

const REVIEW_STATUSES = new Set(['reviewing', 'approved', 'rejected', 'withdrawn']);
const UUID_V7_PATTERN = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
const RFC3339_NANO_PATTERN = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.(\d{1,9}))?(?:Z|([+-])(\d{2}):(\d{2}))$/;
const STRONG_ETAG_PATTERN = /^"[\x21\x23-\x7e\x80-\xff]+"$/;
const DEFINITION_KEYS = Object.freeze([
  'schema_version',
  'name',
  'summary',
  'category',
  'tags',
  'input_description',
  'output_description',
  'invocation_rules',
  ...SKILL_CAPABILITY_FIELDS,
  'examples',
  'starter_prompts',
  'market_listing',
  'public_tool_refs'
]);

export class SkillReviewContractError extends Error {
  constructor(message, field = '') {
    super(message);
    this.name = 'SkillReviewContractError';
    this.code = 'INVALID_SKILL_REVIEW_RESPONSE';
    this.field = field;
    this.status = 502;
    this.retryable = false;
  }
}

export function parseSkillReviewQueueResponse(payload) {
  const value = exactObject(payload, 'Skill 审核队列响应', ['items', 'next_cursor', 'request_id']);
  if (!Array.isArray(value.items)) fail('items 必须为数组', 'items');
  const items = value.items.map((item, index) => parseQueueItem(item, `items[${index}]`));
  if (new Set(items.map((item) => item.reviewID)).size !== items.length) {
    fail('items 包含重复 review_id', 'items');
  }
  if (new Set(items.map((item) => item.skillID)).size !== items.length) {
    fail('items 包含重复 skill_id', 'items');
  }
  return {
    items,
    nextCursor: nullableCursor(value.next_cursor, 'next_cursor'),
    requestID: uuid(value.request_id, 'request_id')
  };
}

export function parseSkillReviewDetailResponse(payload) {
  const value = exactObject(payload, 'Skill 审核详情响应', ['review', 'request_id']);
  const review = exactObject(value.review, 'review', [
    'review_id',
    'skill_id',
    'owner_user_id',
    'status',
    'submitted_at',
    'updated_at',
    'definition',
    'current_published',
    'comparison',
    'review_etag',
    'allowed_actions'
  ]);
  const status = reviewStatus(review.status, 'review.status');
  const definition = strictSkillDefinition(review.definition, 'review.definition');
  const currentPublished = review.current_published === null
    ? null
    : parseCurrentPublished(review.current_published);
  const comparison = exactObject(review.comparison, 'review.comparison', ['has_current_published', 'same_content']);
  const hasCurrentPublished = booleanValue(comparison.has_current_published, 'review.comparison.has_current_published');
  const sameContent = booleanValue(comparison.same_content, 'review.comparison.same_content');
  if (hasCurrentPublished !== (currentPublished !== null)) {
    fail('comparison.has_current_published 与 current_published 不一致', 'review.comparison.has_current_published');
  }
  if (!hasCurrentPublished && sameContent) {
    fail('无当前发布内容时 comparison.same_content 必须为 false', 'review.comparison.same_content');
  }
  if (currentPublished && sameContent !== sameDefinition(definition, currentPublished.definition)) {
    fail('comparison.same_content 与两份 Definition 内容不一致', 'review.comparison.same_content');
  }
  return {
    review: {
      reviewID: uuid(review.review_id, 'review.review_id'),
      skillID: uuid(review.skill_id, 'review.skill_id'),
      ownerUserID: uuid(review.owner_user_id, 'review.owner_user_id'),
      status,
      submittedAt: timestamp(review.submitted_at, 'review.submitted_at'),
      updatedAt: timestamp(review.updated_at, 'review.updated_at'),
      definition,
      currentPublished,
      comparison: { hasCurrentPublished, sameContent },
      reviewETag: strongETag(review.review_etag, 'review.review_etag'),
      allowedActions: detailActions(review.allowed_actions, status, 'review.allowed_actions')
    },
    requestID: uuid(value.request_id, 'request_id')
  };
}

export function parseSkillReviewDecisionResponse(payload) {
  const value = exactObject(payload, 'Skill 审核决定响应', ['review', 'request_id']);
  const review = exactObject(value.review, 'review', [
    'review_id',
    'skill_id',
    'status',
    'published_snapshot_id',
    'decided_at',
    'allowed_actions'
  ]);
  if (review.status !== SKILL_REVIEW_DECISION_APPROVED) {
    fail('review.status 必须为 approved', 'review.status');
  }
  emptyActions(review.allowed_actions, 'review.allowed_actions');
  return {
    review: {
      reviewID: uuid(review.review_id, 'review.review_id'),
      skillID: uuid(review.skill_id, 'review.skill_id'),
      status: SKILL_REVIEW_DECISION_APPROVED,
      publishedSnapshotID: uuid(review.published_snapshot_id, 'review.published_snapshot_id'),
      decidedAt: timestamp(review.decided_at, 'review.decided_at'),
      allowedActions: []
    },
    requestID: uuid(value.request_id, 'request_id')
  };
}

function parseQueueItem(payload, field) {
  const value = exactObject(payload, field, [
    'review_id', 'skill_id', 'name', 'summary', 'category', 'status', 'submitted_at', 'allowed_actions'
  ]);
  if (value.status !== 'reviewing') fail(`${field}.status 必须为 reviewing`, `${field}.status`);
  approveAction(value.allowed_actions, `${field}.allowed_actions`);
  return {
    reviewID: uuid(value.review_id, `${field}.review_id`),
    skillID: uuid(value.skill_id, `${field}.skill_id`),
    name: requiredText(value.name, `${field}.name`),
    summary: text(value.summary, `${field}.summary`),
    category: text(value.category, `${field}.category`),
    status: 'reviewing',
    submittedAt: timestamp(value.submitted_at, `${field}.submitted_at`),
    allowedActions: [SKILL_REVIEW_ACTION_APPROVE]
  };
}

function parseCurrentPublished(payload) {
  const value = exactObject(payload, 'review.current_published', [
    'published_snapshot_id', 'published_at', 'definition'
  ]);
  return {
    publishedSnapshotID: uuid(value.published_snapshot_id, 'review.current_published.published_snapshot_id'),
    publishedAt: timestamp(value.published_at, 'review.current_published.published_at'),
    definition: strictSkillDefinition(value.definition, 'review.current_published.definition')
  };
}

function strictSkillDefinition(payload, field) {
  const definition = exactObject(payload, field, DEFINITION_KEYS);
  SKILL_CAPABILITY_FIELDS.forEach((key) => {
    exactObject(definition[key], `${field}.${key}`, ['applicability', 'guidance', 'not_applicable_reason']);
  });
  if (!Array.isArray(definition.examples)) fail(`${field}.examples 必须为数组`, `${field}.examples`);
  definition.examples.forEach((example, index) => {
    exactObject(example, `${field}.examples[${index}]`, ['input', 'output']);
  });
  exactObject(definition.market_listing, `${field}.market_listing`, [
    'cover_asset_id', 'detail', 'copyright_notice', 'user_notice'
  ]);
  try {
    return parseSkillDefinition(definition);
  } catch (error) {
    fail(error?.message || `${field} 不符合 SkillDefinitionV1`, error?.field || field);
  }
}

function sameDefinition(left, right) {
  return JSON.stringify(left) === JSON.stringify(right);
}

function detailActions(value, status, field) {
  if (status === 'reviewing') {
    approveAction(value, field);
    return [SKILL_REVIEW_ACTION_APPROVE];
  }
  emptyActions(value, field);
  return [];
}

function approveAction(value, field) {
  if (!Array.isArray(value) || value.length !== 1 || value[0] !== SKILL_REVIEW_ACTION_APPROVE) {
    fail(`${field} 必须精确为 [${SKILL_REVIEW_ACTION_APPROVE}]`, field);
  }
}

function emptyActions(value, field) {
  if (!Array.isArray(value) || value.length !== 0) fail(`${field} 必须为空数组`, field);
}

function exactObject(payload, field, keys) {
  if (!payload || typeof payload !== 'object' || Array.isArray(payload)) fail(`${field} 必须为对象`, field);
  const actual = Object.keys(payload).sort();
  const expected = [...keys].sort();
  if (actual.length !== expected.length || actual.some((key, index) => key !== expected[index])) {
    fail(`${field} 字段集合不符合冻结契约`, field);
  }
  return payload;
}

function reviewStatus(value, field) {
  if (typeof value !== 'string' || !REVIEW_STATUSES.has(value)) fail(`${field} 包含未知审核状态`, field);
  return value;
}

function uuid(value, field) {
  if (typeof value !== 'string' || !UUID_V7_PATTERN.test(value)) fail(`${field} 必须为规范小写 UUIDv7`, field);
  return value;
}

function timestamp(value, field) {
  const match = typeof value === 'string' ? value.match(RFC3339_NANO_PATTERN) : null;
  if (!match || !isValidRFC3339Calendar(match)) {
    fail(`${field} 必须为 RFC3339Nano`, field);
  }
  return value;
}

function isValidRFC3339Calendar(match) {
  const year = Number(match[1]);
  const month = Number(match[2]);
  const day = Number(match[3]);
  const hour = Number(match[4]);
  const minute = Number(match[5]);
  const second = Number(match[6]);
  const offsetHour = match[8] ? Number(match[9]) : 0;
  const offsetMinute = match[8] ? Number(match[10]) : 0;
  if (
    month < 1 || month > 12
    || day < 1 || day > daysInMonth(year, month)
    || hour > 23
    || minute > 59
    || second > 59
    || offsetHour > 23
    || offsetMinute > 59
  ) return false;
  return true;
}

function daysInMonth(year, month) {
  if (month === 2) return isLeapYear(year) ? 29 : 28;
  return [4, 6, 9, 11].includes(month) ? 30 : 31;
}

function isLeapYear(year) {
  return year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0);
}

function strongETag(value, field) {
  if (typeof value !== 'string' || !STRONG_ETAG_PATTERN.test(value) || value.startsWith('W/')) {
    fail(`${field} 必须为 quoted strong ETag`, field);
  }
  return value;
}

function nullableCursor(value, field) {
  if (value === null) return null;
  if (typeof value !== 'string' || !value || value.trim() !== value || /[\u0000-\u001f\u007f]/.test(value)) {
    fail(`${field} 必须为 null 或非空 opaque string`, field);
  }
  return value;
}

function requiredText(value, field) {
  const parsed = text(value, field);
  if (!parsed) fail(`${field} 不能为空`, field);
  return parsed;
}

function text(value, field) {
  if (typeof value !== 'string') fail(`${field} 必须为字符串`, field);
  return value;
}

function booleanValue(value, field) {
  if (typeof value !== 'boolean') fail(`${field} 必须为布尔值`, field);
  return value;
}

function fail(message, field) {
  throw new SkillReviewContractError(message, field);
}
