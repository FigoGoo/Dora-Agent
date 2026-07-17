export const CREATION_SPEC_PREVIEW_INTENT_SCHEMA = 'plan_creation_spec.preview.intent.v1';
export const CREATION_SPEC_PREVIEW_ENQUEUE_SCHEMA = 'plan_creation_spec.preview.enqueue.v1';
export const CREATION_SPEC_PREVIEW_CARD_SCHEMA = 'creation_spec.preview.card.v1';

export const CREATION_SPEC_DELIVERABLE_TYPES = Object.freeze([
  'video',
  'image_set',
  'audio',
  'mixed'
]);
export const CREATION_SPEC_LOCALES = Object.freeze(['zh-CN', 'en-US']);

const DELIVERABLE_TYPES = new Set(CREATION_SPEC_DELIVERABLE_TYPES);
const LOCALES = new Set(CREATION_SPEC_LOCALES);
const UUID_V7_PATTERN = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
const SHA256_PATTERN = /^[0-9a-f]{64}$/;
const RESULT_CODE_PATTERN = /^[A-Z][A-Z0-9_]{0,63}$/;
const RFC3339_PATTERN = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$/;
const INTENT_REQUIRED_FIELDS = Object.freeze(['schema_version', 'goal', 'deliverable_type', 'locale', 'constraints']);
const INTENT_FIELDS_WITH_AUDIENCE = Object.freeze([...INTENT_REQUIRED_FIELDS, 'audience']);
const ENQUEUE_FIELDS = Object.freeze(['schema_version', 'request_id', 'session_id', 'input_id', 'status']);
const CARD_FIELDS = Object.freeze([
  'schema_version', 'creation_spec_id', 'project_id', 'version', 'status', 'content_digest', 'title', 'goal',
  'deliverable_type', 'audience', 'locale', 'phases', 'constraints', 'acceptance_criteria', 'updated_at'
]);
const PHASE_FIELDS = Object.freeze(['key', 'title', 'objective', 'output']);
const FAILURE_FIELDS = Object.freeze(['input_id', 'result_code', 'summary', 'retryable']);

export class CreationSpecPreviewContractError extends Error {
  constructor(message, field = '', code = 'INVALID_CREATION_SPEC_PREVIEW') {
    super(message);
    this.name = 'CreationSpecPreviewContractError';
    this.field = field;
    this.code = code;
    this.status = 502;
    this.retryable = false;
  }
}

// normalizeCreationSpecPreviewIntent 将表单语义收敛为冻结 wire DTO；可选 audience 为空时必须省略。
export function normalizeCreationSpecPreviewIntent({
  goal,
  deliverableType,
  audience,
  locale,
  constraints
} = {}) {
  const normalizedGoal = normalizedText(goal, 'goal', { minimum: 1, maximum: 2000, trim: true });
  const normalizedAudience = audience == null || String(audience).trim() === ''
    ? undefined
    : normalizedText(audience, 'audience', { minimum: 1, maximum: 500, trim: true });
  const normalizedConstraints = normalizeConstraints(constraints);
  const result = {
    schema_version: CREATION_SPEC_PREVIEW_INTENT_SCHEMA,
    goal: normalizedGoal,
    deliverable_type: enumValue(deliverableType, DELIVERABLE_TYPES, 'deliverable_type'),
    locale: enumValue(locale, LOCALES, 'locale'),
    constraints: normalizedConstraints
  };
  if (normalizedAudience !== undefined) result.audience = normalizedAudience;
  return result;
}

// parseCreationSpecPreviewIntent 严格验证已经形成的 wire DTO，不接受 null、未知字段或非 NFC 文本。
export function parseCreationSpecPreviewIntent(payload) {
  const value = object(payload, 'CreationSpec Preview Intent');
  const fields = Object.hasOwn(value, 'audience') ? INTENT_FIELDS_WITH_AUDIENCE : INTENT_REQUIRED_FIELDS;
  exactObject(value, fields, 'CreationSpec Preview Intent');
  exact(value.schema_version, CREATION_SPEC_PREVIEW_INTENT_SCHEMA, 'schema_version');
  const result = {
    schema_version: value.schema_version,
    goal: strictCanonicalText(value.goal, 'goal', { minimum: 1, maximum: 2000 }),
    deliverable_type: enumValue(value.deliverable_type, DELIVERABLE_TYPES, 'deliverable_type'),
    locale: enumValue(value.locale, LOCALES, 'locale'),
    constraints: strictStringArray(value.constraints, 'constraints', {
      minimumItems: 0,
      maximumItems: 8,
      minimumLength: 1,
      maximumLength: 200
    })
  };
  if (Object.hasOwn(value, 'audience')) {
    // Wire DTO 保留 present-empty 与 absent 的区别；表单归一化仍可选择省略空输入。
    result.audience = strictCanonicalText(value.audience, 'audience', { minimum: 0, maximum: 500 });
  }
  return result;
}

// parseCreationSpecPreviewEnqueueResponse 只接受 HTTP 202 对应的 pending 回执。
export function parseCreationSpecPreviewEnqueueResponse(payload, expectedSessionID) {
  const value = exactObject(payload, ENQUEUE_FIELDS, 'CreationSpec Preview Enqueue');
  exact(value.schema_version, CREATION_SPEC_PREVIEW_ENQUEUE_SCHEMA, 'schema_version');
  const sessionID = uuidV7(value.session_id, 'session_id');
  if (sessionID !== uuidV7(expectedSessionID, 'expected session_id')) {
    throw contractError('Preview Enqueue 返回了错误的 session_id', 'session_id');
  }
  exact(value.status, 'pending', 'status');
  return {
    schemaVersion: value.schema_version,
    requestID: uuidV7(value.request_id, 'request_id'),
    sessionID,
    inputID: uuidV7(value.input_id, 'input_id'),
    status: value.status
  };
}

// parseCreationSpecPreviewCard 严格解析可展示的 Draft Card；任何未知字段都不得进入 UI。
export function parseCreationSpecPreviewCard(payload, { expectedProjectID } = {}) {
  const value = exactObject(payload, CARD_FIELDS, 'CreationSpec Preview Card');
  if (value.schema_version !== CREATION_SPEC_PREVIEW_CARD_SCHEMA) {
    throw contractError(
      'Creation Spec Preview Card 使用了不受支持的版本',
      'schema_version',
      'UNSUPPORTED_CREATION_SPEC_PREVIEW_VERSION'
    );
  }
  const projectID = uuidV7(value.project_id, 'project_id');
  if (expectedProjectID != null && projectID !== uuidV7(expectedProjectID, 'expected project_id')) {
    throw contractError('Creation Spec Preview Card 的 project_id 不一致', 'project_id');
  }
  exact(value.status, 'draft', 'status');
  const phases = strictObjectArray(value.phases, 'phases', { minimumItems: 1, maximumItems: 6 }, (phase, index) => {
    const field = `phases[${index}]`;
    exactObject(phase, PHASE_FIELDS, field);
    const key = strictCanonicalText(phase.key, `${field}.key`, { minimum: 1, maximum: 32 });
    if (!/^phase_[1-6]$/.test(key)) throw contractError(`${field}.key 不符合冻结格式`, `${field}.key`);
    return {
      key,
      title: strictCanonicalText(phase.title, `${field}.title`, { minimum: 1, maximum: 80 }),
      objective: strictCanonicalText(phase.objective, `${field}.objective`, { minimum: 1, maximum: 500 }),
      output: strictCanonicalText(phase.output, `${field}.output`, { minimum: 1, maximum: 500 })
    };
  });
  assertUnique(phases.map((phase) => phase.key), 'phases.key');

  return {
    kind: 'card',
    schemaVersion: value.schema_version,
    creationSpecID: uuidV7(value.creation_spec_id, 'creation_spec_id'),
    projectID,
    version: safeInteger(value.version, 'version', 1),
    status: value.status,
    contentDigest: sha256(value.content_digest, 'content_digest'),
    title: strictCanonicalText(value.title, 'title', { minimum: 1, maximum: 80 }),
    goal: strictCanonicalText(value.goal, 'goal', { minimum: 1, maximum: 2000 }),
    deliverableType: enumValue(value.deliverable_type, DELIVERABLE_TYPES, 'deliverable_type'),
    audience: strictCanonicalText(value.audience, 'audience', { minimum: 0, maximum: 500 }),
    locale: enumValue(value.locale, LOCALES, 'locale'),
    phases,
    constraints: strictStringArray(value.constraints, 'constraints', {
      minimumItems: 0, maximumItems: 8, minimumLength: 1, maximumLength: 200
    }),
    acceptanceCriteria: strictStringArray(value.acceptance_criteria, 'acceptance_criteria', {
      minimumItems: 1, maximumItems: 8, minimumLength: 1, maximumLength: 240
    }),
    updatedAt: timestamp(value.updated_at, 'updated_at')
  };
}

// parseCreationSpecPreviewProjection 允许 Snapshot 对未知未来版本安全降级，但绝不展示其字段。
export function parseCreationSpecPreviewProjection(payload, options) {
  if (payload === null) return null;
  const value = object(payload, 'CreationSpec Preview Projection');
  if (value.schema_version !== CREATION_SPEC_PREVIEW_CARD_SCHEMA) {
    return Object.freeze({
      kind: 'unsupported',
      schemaVersion: strictCanonicalText(value.schema_version, 'schema_version', { minimum: 1, maximum: 64 })
    });
  }
  return parseCreationSpecPreviewCard(value, options);
}

// parseCreationSpecPreviewFailure 严格解析持久化失败投影，不允许服务端错误细节穿透。
export function parseCreationSpecPreviewFailure(payload) {
  const value = exactObject(payload, FAILURE_FIELDS, 'CreationSpec Preview Failure');
  if (typeof value.retryable !== 'boolean') throw contractError('retryable 必须为布尔值', 'retryable');
  const resultCode = strictCanonicalText(value.result_code, 'result_code', { minimum: 1, maximum: 64 });
  if (!RESULT_CODE_PATTERN.test(resultCode)) throw contractError('result_code 不符合稳定错误码格式', 'result_code');
  return {
    kind: 'failure',
    inputID: uuidV7(value.input_id, 'input_id'),
    resultCode,
    summary: strictCanonicalText(value.summary, 'summary', { minimum: 1, maximum: 500 }),
    retryable: value.retryable
  };
}

export function isCanonicalPreviewUUIDV7(value) {
  return typeof value === 'string' && UUID_V7_PATTERN.test(value);
}

function normalizeConstraints(value) {
  if (!Array.isArray(value)) throw contractError('constraints 必须为数组', 'constraints');
  if (value.length > 8) throw contractError('constraints 最多包含 8 项', 'constraints');
  const normalized = value.map((item, index) => normalizedText(item, `constraints[${index}]`, {
    minimum: 1, maximum: 200, trim: true
  }));
  assertUnique(normalized, 'constraints');
  return normalized;
}

function strictStringArray(value, field, limits) {
  if (!Array.isArray(value)) throw contractError(`${field} 必须为数组`, field);
  if (value.length < limits.minimumItems || value.length > limits.maximumItems) {
    throw contractError(`${field} 数量超出冻结范围`, field);
  }
  const parsed = value.map((item, index) => strictCanonicalText(item, `${field}[${index}]`, {
    minimum: limits.minimumLength,
    maximum: limits.maximumLength
  }));
  assertUnique(parsed, field);
  return parsed;
}

function strictObjectArray(value, field, limits, parser) {
  if (!Array.isArray(value)) throw contractError(`${field} 必须为数组`, field);
  if (value.length < limits.minimumItems || value.length > limits.maximumItems) {
    throw contractError(`${field} 数量超出冻结范围`, field);
  }
  return value.map((item, index) => parser(object(item, `${field}[${index}]`), index));
}

function normalizedText(value, field, { minimum, maximum, trim }) {
  if (typeof value !== 'string') throw contractError(`${field} 必须为字符串`, field);
  const parsed = (trim ? value.trim() : value).normalize('NFC');
  assertTextLength(parsed, field, minimum, maximum);
  return parsed;
}

function strictCanonicalText(value, field, { minimum, maximum }) {
  if (typeof value !== 'string') throw contractError(`${field} 必须为字符串`, field);
  if (value !== value.normalize('NFC')) throw contractError(`${field} 必须为 NFC 字符串`, field);
  assertTextLength(value, field, minimum, maximum);
  return value;
}

function assertTextLength(value, field, minimum, maximum) {
  const length = [...value].length;
  if (length < minimum || length > maximum) throw contractError(`${field} 长度超出冻结范围`, field);
}

function exactObject(value, expectedFields, field) {
  object(value, field);
  const actual = Object.keys(value).sort();
  const expected = [...expectedFields].sort();
  if (actual.length !== expected.length || actual.some((key, index) => key !== expected[index])) {
    throw contractError(`${field} 字段集合不符合冻结契约`, field);
  }
  return value;
}

function object(value, field) {
  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    throw contractError(`${field} 必须为对象`, field);
  }
  return value;
}

function enumValue(value, allowed, field) {
  if (typeof value !== 'string' || !allowed.has(value)) throw contractError(`${field} 使用了未知枚举`, field);
  return value;
}

function uuidV7(value, field) {
  if (!isCanonicalPreviewUUIDV7(value)) throw contractError(`${field} 必须为规范小写 UUIDv7`, field);
  return value;
}

function sha256(value, field) {
  if (typeof value !== 'string' || !SHA256_PATTERN.test(value)) {
    throw contractError(`${field} 必须为小写 SHA-256`, field);
  }
  return value;
}

function safeInteger(value, field, minimum) {
  if (!Number.isSafeInteger(value) || value < minimum) throw contractError(`${field} 必须为安全整数`, field);
  return value;
}

function timestamp(value, field) {
  const parsed = strictCanonicalText(value, field, { minimum: 1, maximum: 64 });
  if (!RFC3339_PATTERN.test(parsed) || Number.isNaN(Date.parse(parsed))) {
    throw contractError(`${field} 必须为 RFC3339 时间`, field);
  }
  return parsed;
}

function exact(actual, expected, field) {
  if (actual !== expected) throw contractError(`${field} 不符合冻结契约`, field);
  return actual;
}

function assertUnique(values, field) {
  if (new Set(values).size !== values.length) throw contractError(`${field} 包含重复项`, field);
}

function contractError(message, field, code) {
  return new CreationSpecPreviewContractError(message, field, code);
}
