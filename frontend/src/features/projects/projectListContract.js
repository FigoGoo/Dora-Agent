const UUID_V7_PATTERN = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
const UTC_RFC3339_NANO_PATTERN = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.\d{1,9})?Z$/;
const RAW_BASE64URL_PATTERN = /^[A-Za-z0-9_-]+$/;
const LIFECYCLE_STATUSES = new Set(['active', 'archived']);
const RECENT_RUN_STATUSES = new Set([
  'idle', 'queued', 'running', 'waiting_user', 'waiting_async', 'succeeded', 'partial_failed', 'failed', 'cancelled'
]);
const INITIAL_PROMPT_STATUSES = new Set(['absent', 'pending', 'accepted', 'failed']);
const ENVELOPE_FIELDS = Object.freeze(['items', 'next_after', 'request_id']);
const ITEM_FIELDS = Object.freeze([
  'initial_prompt_status',
  'lifecycle_status',
  'project_id',
  'recent_run_status',
  'title',
  'updated_at',
  'workspace_ref'
]);

export class ProjectListContractError extends Error {
  constructor(message, field = '') {
    super(message);
    this.name = 'ProjectListContractError';
    this.field = field;
    this.code = 'INVALID_PROJECT_LIST_RESPONSE';
    this.status = 502;
    this.retryable = false;
  }
}

// parseProjectListResponse 严格消费 Business 项目列表契约，拒绝额外字段、重复 ID 和非正式工作台路由。
export function parseProjectListResponse(payload) {
  const value = exactObject(payload, ENVELOPE_FIELDS, '项目列表响应');
  if (!Array.isArray(value.items) || value.items.length > 100) {
    throw contractError('items 必须为不超过 100 条的数组', 'items');
  }
  const items = value.items.map((item, index) => parseProjectListItem(item, `items[${index}]`));
  if (new Set(items.map((item) => item.projectID)).size !== items.length) {
    throw contractError('items 包含重复 project_id', 'items');
  }
  return {
    items,
    nextAfter: nullableAfter(value.next_after, 'next_after'),
    requestID: uuid(value.request_id, 'request_id')
  };
}

function parseProjectListItem(payload, field) {
  const value = exactObject(payload, ITEM_FIELDS, field);
  const projectID = uuid(value.project_id, `${field}.project_id`);
  const workspaceRef = requiredText(value.workspace_ref, `${field}.workspace_ref`);
  const expectedWorkspaceRef = `/projects/${projectID}/workspace`;
  if (workspaceRef !== expectedWorkspaceRef) {
    throw contractError(`${field}.workspace_ref 必须指向当前项目正式工作台`, `${field}.workspace_ref`);
  }
  return {
    projectID,
    title: title(value.title, `${field}.title`),
    lifecycleStatus: enumValue(value.lifecycle_status, LIFECYCLE_STATUSES, `${field}.lifecycle_status`),
    recentRunStatus: enumValue(value.recent_run_status, RECENT_RUN_STATUSES, `${field}.recent_run_status`),
    initialPromptStatus: enumValue(value.initial_prompt_status, INITIAL_PROMPT_STATUSES, `${field}.initial_prompt_status`),
    updatedAt: timestamp(value.updated_at, `${field}.updated_at`),
    workspaceRef
  };
}

function exactObject(value, expectedFields, field) {
  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    throw contractError(`${field} 必须为对象`, field);
  }
  const actual = Object.keys(value).sort();
  const expected = [...expectedFields].sort();
  if (actual.length !== expected.length || actual.some((key, index) => key !== expected[index])) {
    throw contractError(`${field} 字段不符合冻结契约`, field);
  }
  return value;
}

function requiredText(value, field) {
  if (typeof value !== 'string' || !value) {
    throw contractError(`${field} 必须为非空字符串`, field);
  }
  return value;
}

function title(value, field) {
  const parsed = requiredText(value, field);
  if ([...parsed].length > 160 || /\p{Cc}/u.test(parsed)) {
    throw contractError(`${field} 必须为不超过 160 字且不含控制字符的标题`, field);
  }
  return parsed;
}

function enumValue(value, allowed, field) {
  const parsed = requiredText(value, field);
  if (!allowed.has(parsed)) throw contractError(`${field} 包含未知状态`, field);
  return parsed;
}

function uuid(value, field) {
  const parsed = requiredText(value, field);
  if (!UUID_V7_PATTERN.test(parsed)) throw contractError(`${field} 必须为规范小写 UUIDv7`, field);
  return parsed;
}

function timestamp(value, field) {
  const parsed = requiredText(value, field);
  const match = parsed.match(UTC_RFC3339_NANO_PATTERN);
  if (!match || !validCalendarDate(match) || Number.isNaN(Date.parse(parsed))) {
    throw contractError(`${field} 必须为 UTC RFC3339Nano 时间`, field);
  }
  return parsed;
}

function validCalendarDate(match) {
  const year = Number(match[1]);
  const month = Number(match[2]);
  const day = Number(match[3]);
  const hour = Number(match[4]);
  const minute = Number(match[5]);
  const second = Number(match[6]);
  if (month < 1 || month > 12 || hour > 23 || minute > 59 || second > 59) return false;
  const leap = year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0);
  const days = [31, leap ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31];
  return day >= 1 && day <= days[month - 1];
}

function nullableAfter(value, field) {
  if (value === null) return null;
  const parsed = requiredText(value, field);
  if (new TextEncoder().encode(parsed).length > 512 || !RAW_BASE64URL_PATTERN.test(parsed)) {
    throw contractError(`${field} 必须为不超过 512 字节的无填充 Base64URL`, field);
  }
  return parsed;
}

function contractError(message, field) {
  return new ProjectListContractError(message, field);
}
