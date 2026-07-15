import {
  parseSkillDefinition,
  SKILL_CAPABILITY_FIELDS
} from '../skills/skillContract.js';

export const SKILL_GOVERNANCE_STATUSES = Object.freeze(['active', 'suspended', 'offline']);
export const SKILL_GOVERNANCE_ACTIONS = Object.freeze(['suspend', 'resume', 'offline']);
export const SKILL_GOVERNANCE_CAPABILITY_REQUIRED_CODE = 'SKILL_GOVERNANCE_CAPABILITY_REQUIRED';

const GOVERNANCE_STATUS_SET = new Set(SKILL_GOVERNANCE_STATUSES);
const UUID_V7_PATTERN = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
const RFC3339_NANO_PATTERN = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.(\d{1,9}))?(?:Z|([+-])(\d{2}):(\d{2}))$/;
const STRONG_ETAG_PATTERN = /^"[\x21\x23-\x7e\x80-\xff]+"$/;
const CURSOR_PATTERN = /^[A-Za-z0-9_-]{1,1024}$/;
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
const ACTIONS_BY_STATUS = Object.freeze({
  active: Object.freeze(['suspend', 'offline']),
  suspended: Object.freeze(['resume', 'offline']),
  offline: Object.freeze([])
});

export class SkillGovernanceContractError extends Error {
  constructor(message, field = '') {
    super(message);
    this.name = 'SkillGovernanceContractError';
    this.code = 'INVALID_SKILL_GOVERNANCE_RESPONSE';
    this.field = field;
    this.status = 502;
    this.retryable = false;
  }
}

export function parseGovernanceListResponse(payload) {
  const value = exactObject(payload, 'Skill 治理列表响应', ['items', 'next_cursor', 'request_id']);
  if (!Array.isArray(value.items)) fail('items 必须为数组', 'items');
  const items = value.items.map((item, index) => parseListItem(item, `items[${index}]`));
  if (new Set(items.map((item) => item.skillID)).size !== items.length) {
    fail('items 包含重复 skill_id', 'items');
  }
  return {
    items,
    nextCursor: nullableCursor(value.next_cursor, 'next_cursor'),
    requestID: uuid(value.request_id, 'request_id')
  };
}

export function parseGovernanceDetailResponse(payload) {
  const value = exactObject(payload, 'Skill 治理详情响应', ['skill', 'request_id']);
  const skill = exactObject(value.skill, 'skill', [
    'skill_id',
    'definition',
    'published_at',
    'governance_status',
    'governance_epoch',
    'governance_etag',
    'allowed_actions'
  ]);
  const status = governanceStatus(skill.governance_status, 'skill.governance_status');
  const epoch = governanceEpoch(skill.governance_epoch, status, 'skill.governance_epoch');
  return {
    skill: {
      skillID: uuid(skill.skill_id, 'skill.skill_id'),
      definition: strictSkillDefinition(skill.definition, 'skill.definition'),
      publishedAt: timestamp(skill.published_at, 'skill.published_at'),
      governanceStatus: status,
      governanceEpoch: epoch,
      governanceETag: strongETag(skill.governance_etag, 'skill.governance_etag'),
      allowedActions: governanceActions(skill.allowed_actions, status, 'skill.allowed_actions')
    },
    requestID: uuid(value.request_id, 'request_id')
  };
}

export function parseGovernanceDecisionResponse(payload) {
  const value = exactObject(payload, 'Skill 治理决定响应', ['skill', 'request_id']);
  const skill = exactObject(value.skill, 'skill', [
    'skill_id',
    'governance_status',
    'governance_epoch',
    'transitioned_at',
    'governance_etag',
    'allowed_actions'
  ]);
  const status = governanceStatus(skill.governance_status, 'skill.governance_status');
  const epoch = governanceEpoch(skill.governance_epoch, status, 'skill.governance_epoch');
  if (epoch < 2) fail('治理决定后的 governance_epoch 必须至少为 2', 'skill.governance_epoch');
  return {
    skill: {
      skillID: uuid(skill.skill_id, 'skill.skill_id'),
      governanceStatus: status,
      governanceEpoch: epoch,
      transitionedAt: timestamp(skill.transitioned_at, 'skill.transitioned_at'),
      governanceETag: strongETag(skill.governance_etag, 'skill.governance_etag'),
      allowedActions: governanceActions(skill.allowed_actions, status, 'skill.allowed_actions')
    },
    requestID: uuid(value.request_id, 'request_id')
  };
}

function parseListItem(payload, field) {
  const value = exactObject(payload, field, [
    'skill_id',
    'name',
    'summary',
    'category',
    'published_at',
    'governance_status',
    'governance_epoch',
    'allowed_actions'
  ]);
  const status = governanceStatus(value.governance_status, `${field}.governance_status`);
  return {
    skillID: uuid(value.skill_id, `${field}.skill_id`),
    name: requiredText(value.name, `${field}.name`),
    summary: text(value.summary, `${field}.summary`),
    category: text(value.category, `${field}.category`),
    publishedAt: timestamp(value.published_at, `${field}.published_at`),
    governanceStatus: status,
    governanceEpoch: governanceEpoch(value.governance_epoch, status, `${field}.governance_epoch`),
    allowedActions: governanceActions(value.allowed_actions, status, `${field}.allowed_actions`)
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

function governanceStatus(value, field) {
  if (typeof value !== 'string' || !GOVERNANCE_STATUS_SET.has(value)) {
    fail(`${field} 包含未知治理状态`, field);
  }
  return value;
}

function governanceEpoch(value, status, field) {
  if (!Number.isSafeInteger(value) || value < 1) fail(`${field} 必须为正安全整数`, field);
  if (status === 'active' && value % 2 !== 1) {
    fail(`${field} 与 active 状态迁移历史不一致`, field);
  }
  if (status === 'suspended' && (value < 2 || value % 2 !== 0)) {
    fail(`${field} 与 suspended 状态迁移历史不一致`, field);
  }
  if (status === 'offline' && value < 2) {
    fail(`${field} 与 offline 状态迁移历史不一致`, field);
  }
  return value;
}

function governanceActions(value, status, field) {
  const expected = ACTIONS_BY_STATUS[status];
  if (!Array.isArray(value)
    || value.length !== expected.length
    || value.some((action, index) => action !== expected[index])) {
    fail(`${field} 与 ${status} 状态不一致`, field);
  }
  return [...expected];
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

function uuid(value, field) {
  if (typeof value !== 'string' || !UUID_V7_PATTERN.test(value)) fail(`${field} 必须为规范小写 UUIDv7`, field);
  return value;
}

function timestamp(value, field) {
  const match = typeof value === 'string' ? value.match(RFC3339_NANO_PATTERN) : null;
  if (!match || !isValidRFC3339Calendar(match)) fail(`${field} 必须为 RFC3339Nano`, field);
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
  return month >= 1 && month <= 12
    && day >= 1 && day <= daysInMonth(year, month)
    && hour <= 23
    && minute <= 59
    && second <= 59
    && offsetHour <= 23
    && offsetMinute <= 59;
}

function daysInMonth(year, month) {
  if (month === 2) return isLeapYear(year) ? 29 : 28;
  return [4, 6, 9, 11].includes(month) ? 30 : 31;
}

function isLeapYear(year) {
  return year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0);
}

function strongETag(value, field) {
  if (typeof value !== 'string' || !STRONG_ETAG_PATTERN.test(value)) {
    fail(`${field} 必须为 quoted strong ETag`, field);
  }
  return value;
}

function nullableCursor(value, field) {
  if (value === null) return null;
  if (typeof value !== 'string' || !CURSOR_PATTERN.test(value)) {
    fail(`${field} 必须为 null 或无填充 Base64URL opaque cursor`, field);
  }
  return value;
}

function requiredText(value, field) {
  const parsed = text(value, field);
  if (!parsed) fail(`${field} 不能为空`, field);
  return parsed;
}

function text(value, field) {
  if (typeof value !== 'string' || value !== value.normalize('NFC') || value !== value.trim()) {
    fail(`${field} 必须为规范化字符串`, field);
  }
  return value;
}

function fail(message, field) {
  throw new SkillGovernanceContractError(message, field);
}
