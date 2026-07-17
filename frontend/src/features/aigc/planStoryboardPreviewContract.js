export const PLAN_STORYBOARD_PREVIEW_ENQUEUE_REQUEST_SCHEMA = 'plan_storyboard.preview.enqueue-request.v1';
export const PLAN_STORYBOARD_PREVIEW_INTENT_SCHEMA = 'plan_storyboard.preview.intent.v1';
export const PLAN_STORYBOARD_PREVIEW_ENQUEUE_SCHEMA = 'plan_storyboard.preview.enqueue.v1';
export const STORYBOARD_PREVIEW_CARD_SCHEMA = 'storyboard.preview.card.v1';
export const STORYBOARD_PREVIEW_MAX_CANONICAL_CONTENT_BYTES = 64 * 1024;

export const STORYBOARD_PREVIEW_ELEMENT_TYPES = Object.freeze([
  'scene',
  'shot',
  'narration',
  'caption',
  'audio'
]);
export const STORYBOARD_PREVIEW_SLOT_TYPES = Object.freeze([
  'image',
  'video',
  'audio',
  'voiceover',
  'caption'
]);
export const STORYBOARD_PREVIEW_COMPLETED_RESULT_CODES = Object.freeze([
  'STORYBOARD_PREVIEW_DRAFT_CREATED'
]);
export const STORYBOARD_PREVIEW_TOOL_FAILURE_RESULT_CODES = Object.freeze([
  'STORYBOARD_PREVIEW_INVALID_ARGUMENT',
  'STORYBOARD_CREATION_SPEC_NOT_FOUND',
  'STORYBOARD_CREATION_SPEC_CONFLICT',
  'STORYBOARD_PREVIEW_CANDIDATE_INVALID',
  'STORYBOARD_PREVIEW_DEPENDENCY_INVALID',
  'STORYBOARD_PREVIEW_CONFLICT',
  'STORYBOARD_PREVIEW_DISABLED',
  'STORYBOARD_PREVIEW_INTERNAL'
]);
export const STORYBOARD_PREVIEW_RUNTIME_FAILURE_RESULT_CODES = Object.freeze([
  'PLAN_STORYBOARD_RUNTIME_FAILED'
]);

const ELEMENT_TYPES = new Set(STORYBOARD_PREVIEW_ELEMENT_TYPES);
const SLOT_TYPES = new Set(STORYBOARD_PREVIEW_SLOT_TYPES);
const COMPLETED_RESULT_CODES = new Set(STORYBOARD_PREVIEW_COMPLETED_RESULT_CODES);
const TOOL_FAILURE_RESULT_CODES = new Set(STORYBOARD_PREVIEW_TOOL_FAILURE_RESULT_CODES);
const RUNTIME_FAILURE_RESULT_CODES = new Set(STORYBOARD_PREVIEW_RUNTIME_FAILURE_RESULT_CODES);
const UUID_V7_PATTERN = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
const SHA256_PATTERN = /^[0-9a-f]{64}$/;
const RESULT_CODE_PATTERN = /^[A-Z][A-Z0-9_]{0,63}$/;
const RFC3339_PATTERN = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$/;
const SECTION_KEY_PATTERN = /^section_([1-8])$/;
const ELEMENT_KEY_PATTERN = /^element_([1-9]|1[0-9]|2[0-4])$/;
const SLOT_KEY_PATTERN = /^slot_([1-9]|[1-8][0-9]|9[0-6])$/;
const PHASE_KEY_PATTERN = /^phase_[1-6]$/;
const REQUEST_FIELDS = Object.freeze(['schema_version', 'creation_spec_ref', 'tool_intent']);
const CREATION_SPEC_REF_FIELDS = Object.freeze(['id', 'version', 'content_digest']);
const INTENT_REQUIRED_FIELDS = Object.freeze(['schema_version', 'planning_instruction']);
const INTENT_WITH_DURATION_FIELDS = Object.freeze([...INTENT_REQUIRED_FIELDS, 'target_duration_seconds']);
const ENQUEUE_FIELDS = Object.freeze([
  'schema_version', 'request_id', 'session_id', 'input_id', 'turn_id', 'run_id', 'tool_call_id', 'status', 'replayed'
]);
const COMPLETED_CARD_FIELDS = Object.freeze([
  'schema_version', 'input_id', 'turn_id', 'run_id', 'tool_call_id', 'status', 'result_code', 'updated_at',
  'storyboard_preview_id', 'project_id', 'creation_spec_ref', 'version', 'content_digest', 'title', 'summary',
  'sections', 'elements', 'slots'
]);
const FAILED_CARD_FIELDS = Object.freeze([
  'schema_version', 'input_id', 'turn_id', 'run_id', 'tool_call_id', 'status', 'result_code', 'updated_at',
  'failure_kind', 'summary', 'retryable'
]);
const SECTION_FIELDS = Object.freeze(['key', 'title', 'objective']);
const ELEMENT_FIELDS = Object.freeze([
  'key', 'section_key', 'order', 'element_type', 'title', 'narrative_purpose', 'duration_seconds',
  'source_phase_key', 'dependency_keys'
]);
const SLOT_FIELDS = Object.freeze(['key', 'element_key', 'slot_type', 'purpose', 'required']);

// PlanStoryboardPreviewContractError 表示前端无法安全消费 Storyboard Development Preview 契约。
export class PlanStoryboardPreviewContractError extends Error {
  constructor(message, field = '', code = 'INVALID_PLAN_STORYBOARD_PREVIEW') {
    super(message);
    this.name = 'PlanStoryboardPreviewContractError';
    this.field = field;
    this.code = code;
    this.status = 502;
    this.retryable = false;
  }
}

// normalizePlanStoryboardPreviewEnqueueRequest 把已验证 CreationSpec Card 与用户规划字段编码为隔离 wire DTO。
export function normalizePlanStoryboardPreviewEnqueueRequest({ creationSpecRef, toolIntent } = {}) {
  const ref = object(creationSpecRef, 'creationSpecRef');
  const intent = object(toolIntent, 'toolIntent');
  const normalized = {
    schema_version: PLAN_STORYBOARD_PREVIEW_ENQUEUE_REQUEST_SCHEMA,
    creation_spec_ref: {
      id: uuidV7(ref.id, 'creationSpecRef.id'),
      version: exactVersion(ref.version, 'creationSpecRef.version'),
      content_digest: sha256(ref.contentDigest, 'creationSpecRef.contentDigest')
    },
    tool_intent: {
      schema_version: PLAN_STORYBOARD_PREVIEW_INTENT_SCHEMA,
      planning_instruction: normalizedText(intent.planningInstruction, 'toolIntent.planningInstruction', 1, 1000)
    }
  };
  if (intent.targetDurationSeconds !== undefined && intent.targetDurationSeconds !== null && intent.targetDurationSeconds !== '') {
    normalized.tool_intent.target_duration_seconds = boundedInteger(
      Number(intent.targetDurationSeconds),
      'toolIntent.targetDurationSeconds',
      5,
      600
    );
  }
  return normalized;
}

// parsePlanStoryboardPreviewEnqueueRequest 严格解析公开请求；资源引用与模型可控 Tool Intent 保持分栏。
export function parsePlanStoryboardPreviewEnqueueRequest(payload) {
  const value = exactObject(payload, REQUEST_FIELDS, 'Plan Storyboard Preview Enqueue Request');
  exact(value.schema_version, PLAN_STORYBOARD_PREVIEW_ENQUEUE_REQUEST_SCHEMA, 'schema_version');
  const ref = exactObject(value.creation_spec_ref, CREATION_SPEC_REF_FIELDS, 'creation_spec_ref');
  const intentCandidate = object(value.tool_intent, 'tool_intent');
  const intentFields = Object.hasOwn(intentCandidate, 'target_duration_seconds')
    ? INTENT_WITH_DURATION_FIELDS
    : INTENT_REQUIRED_FIELDS;
  const intent = exactObject(intentCandidate, intentFields, 'tool_intent');
  exact(intent.schema_version, PLAN_STORYBOARD_PREVIEW_INTENT_SCHEMA, 'tool_intent.schema_version');
  const result = {
    schema_version: value.schema_version,
    creation_spec_ref: {
      id: uuidV7(ref.id, 'creation_spec_ref.id'),
      version: exactVersion(ref.version, 'creation_spec_ref.version'),
      content_digest: sha256(ref.content_digest, 'creation_spec_ref.content_digest')
    },
    tool_intent: {
      schema_version: intent.schema_version,
      planning_instruction: strictText(intent.planning_instruction, 'tool_intent.planning_instruction', 1, 1000)
    }
  };
  if (Object.hasOwn(intent, 'target_duration_seconds')) {
    result.tool_intent.target_duration_seconds = boundedInteger(
      intent.target_duration_seconds,
      'tool_intent.target_duration_seconds',
      5,
      600
    );
  }
  return result;
}

// parsePlanStoryboardPreviewEnqueueResponse 只接受绑定当前 Session 的 202 pending/replayed 回执。
export function parsePlanStoryboardPreviewEnqueueResponse(payload, expectedSessionID) {
  const value = exactObject(payload, ENQUEUE_FIELDS, 'Plan Storyboard Preview Enqueue');
  exact(value.schema_version, PLAN_STORYBOARD_PREVIEW_ENQUEUE_SCHEMA, 'schema_version');
  const sessionID = uuidV7(value.session_id, 'session_id');
  if (sessionID !== uuidV7(expectedSessionID, 'expected session_id')) {
    throw contractError('Plan Storyboard Preview Enqueue 的 session_id 不一致', 'session_id');
  }
  exact(value.status, 'pending', 'status');
  if (typeof value.replayed !== 'boolean') throw contractError('replayed 必须为布尔值', 'replayed');
  return {
    schemaVersion: value.schema_version,
    requestID: uuidV7(value.request_id, 'request_id'),
    sessionID,
    inputID: uuidV7(value.input_id, 'input_id'),
    turnID: uuidV7(value.turn_id, 'turn_id'),
    runID: uuidV7(value.run_id, 'run_id'),
    toolCallID: uuidV7(value.tool_call_id, 'tool_call_id'),
    status: value.status,
    replayed: value.replayed
  };
}

// parseStoryboardPreviewCard 严格解析 completed/failed 安全 Card，并复核全部局部引用与依赖 DAG。
export function parseStoryboardPreviewCard(payload) {
  const candidate = object(payload, 'Storyboard Preview Card');
  const completed = candidate.status === 'completed';
  const value = exactObject(candidate, completed ? COMPLETED_CARD_FIELDS : FAILED_CARD_FIELDS, 'Storyboard Preview Card');
  exact(value.schema_version, STORYBOARD_PREVIEW_CARD_SCHEMA, 'schema_version');
  const parsedResultCode = resultCode(value.result_code, 'result_code');
  const base = {
    kind: 'plan_storyboard_preview',
    schemaVersion: value.schema_version,
    inputID: uuidV7(value.input_id, 'input_id'),
    turnID: uuidV7(value.turn_id, 'turn_id'),
    runID: uuidV7(value.run_id, 'run_id'),
    toolCallID: uuidV7(value.tool_call_id, 'tool_call_id'),
    status: value.status,
    resultCode: parsedResultCode,
    updatedAt: timestamp(value.updated_at, 'updated_at')
  };
  if (!completed) {
    exact(value.status, 'failed', 'status');
    if (typeof value.retryable !== 'boolean') throw contractError('retryable 必须为布尔值', 'retryable');
    const failureKind = enumValue(value.failure_kind, new Set(['tool', 'runtime']), 'failure_kind');
    const allowedResultCodes = failureKind === 'runtime' ? RUNTIME_FAILURE_RESULT_CODES : TOOL_FAILURE_RESULT_CODES;
    enumValue(parsedResultCode, allowedResultCodes, 'result_code');
    return {
      ...base,
      failureKind,
      storyboardPreviewID: '',
      version: 0,
      contentDigest: '',
      title: '',
      summary: strictText(value.summary, 'summary', 1, 1000),
      sections: [],
      elements: [],
      slots: [],
      retryable: value.retryable
    };
  }

  enumValue(parsedResultCode, COMPLETED_RESULT_CODES, 'result_code');

  const sections = parseSections(value.sections);
  const elements = parseElements(value.elements, sections);
  const slots = parseSlots(value.slots, elements);
  validateDependencyDAG(elements);
  validateDurationBudget(elements);
  validateSectionCoverage(sections, elements);
  validateSlotCounts(elements, slots);
  validateCanonicalContentBytes(value);
  return {
    ...base,
    failureKind: null,
    storyboardPreviewID: uuidV7(value.storyboard_preview_id, 'storyboard_preview_id'),
    projectID: uuidV7(value.project_id, 'project_id'),
    creationSpecRef: parseCreationSpecRef(value.creation_spec_ref, 'creation_spec_ref'),
    version: exactVersion(value.version, 'version'),
    contentDigest: sha256(value.content_digest, 'content_digest'),
    title: strictText(value.title, 'title', 1, 120),
    summary: strictText(value.summary, 'summary', 1, 1000),
    sections,
    elements,
    slots,
    retryable: false
  };
}

function parseCreationSpecRef(value, field) {
  const ref = exactObject(value, CREATION_SPEC_REF_FIELDS, field);
  return {
    id: uuidV7(ref.id, `${field}.id`),
    version: exactVersion(ref.version, `${field}.version`),
    contentDigest: sha256(ref.content_digest, `${field}.content_digest`)
  };
}

// parseStoryboardPreviewProjection 只对未知未来 Card 版本安全降级，绝不展示其正文。
export function parseStoryboardPreviewProjection(payload) {
  if (payload === null) return null;
  const value = object(payload, 'Storyboard Preview Projection');
  if (value.schema_version !== STORYBOARD_PREVIEW_CARD_SCHEMA) {
    return Object.freeze({
      kind: 'unsupported',
      schemaVersion: strictText(value.schema_version, 'schema_version', 1, 64)
    });
  }
  return parseStoryboardPreviewCard(value);
}

// isPlanStoryboardPreviewUUIDV7 判断值是否为规范小写 UUIDv7。
export function isPlanStoryboardPreviewUUIDV7(value) {
  return typeof value === 'string' && UUID_V7_PATTERN.test(value);
}

function parseSections(value) {
  return objectArray(value, 'sections', 1, 8, (item, index) => {
    const field = `sections[${index}]`;
    exactObject(item, SECTION_FIELDS, field);
    const key = localKey(item.key, `${field}.key`, SECTION_KEY_PATTERN);
    if (key !== `section_${index + 1}`) throw contractError(`${field}.key 必须按数组顺序连续`, `${field}.key`);
    return {
      key,
      title: strictText(item.title, `${field}.title`, 1, 100),
      objective: strictText(item.objective, `${field}.objective`, 1, 500)
    };
  });
}

function parseElements(value, sections) {
  const sectionKeys = new Set(sections.map((section) => section.key));
  return objectArray(value, 'elements', 1, 24, (item, index) => {
    const field = `elements[${index}]`;
    exactObject(item, ELEMENT_FIELDS, field);
    const key = localKey(item.key, `${field}.key`, ELEMENT_KEY_PATTERN);
    if (key !== `element_${index + 1}`) throw contractError(`${field}.key 必须按数组顺序连续`, `${field}.key`);
    const sectionKey = localKey(item.section_key, `${field}.section_key`, SECTION_KEY_PATTERN);
    if (!sectionKeys.has(sectionKey)) throw contractError(`${field}.section_key 引用了未知 Section`, `${field}.section_key`);
    const order = boundedInteger(item.order, `${field}.order`, 1, 24);
    if (order !== index + 1) throw contractError(`${field}.order 必须从 1 全局连续`, `${field}.order`);
    const dependencies = stringArray(item.dependency_keys, `${field}.dependency_keys`, 0, 8, (dependency, dependencyField) => (
      localKey(dependency, dependencyField, ELEMENT_KEY_PATTERN)
    ));
    assertUnique(dependencies, `${field}.dependency_keys`);
    if (dependencies.includes(key)) throw contractError(`${field}.dependency_keys 不得自引用`, `${field}.dependency_keys`);
    return {
      key,
      sectionKey,
      order,
      elementType: enumValue(item.element_type, ELEMENT_TYPES, `${field}.element_type`),
      title: strictText(item.title, `${field}.title`, 1, 120),
      narrativePurpose: strictText(item.narrative_purpose, `${field}.narrative_purpose`, 1, 1000),
      durationSeconds: boundedInteger(item.duration_seconds, `${field}.duration_seconds`, 1, 600),
      sourcePhaseKey: localKey(item.source_phase_key, `${field}.source_phase_key`, PHASE_KEY_PATTERN),
      dependencyKeys: dependencies
    };
  });
}

function parseSlots(value, elements) {
  const elementKeys = new Set(elements.map((element) => element.key));
  return objectArray(value, 'slots', 0, 96, (item, index) => {
    const field = `slots[${index}]`;
    exactObject(item, SLOT_FIELDS, field);
    const key = localKey(item.key, `${field}.key`, SLOT_KEY_PATTERN);
    if (key !== `slot_${index + 1}`) throw contractError(`${field}.key 必须按数组顺序连续`, `${field}.key`);
    const elementKey = localKey(item.element_key, `${field}.element_key`, ELEMENT_KEY_PATTERN);
    if (!elementKeys.has(elementKey)) throw contractError(`${field}.element_key 引用了未知 Element`, `${field}.element_key`);
    if (typeof item.required !== 'boolean') throw contractError(`${field}.required 必须为布尔值`, `${field}.required`);
    return {
      key,
      elementKey,
      slotType: enumValue(item.slot_type, SLOT_TYPES, `${field}.slot_type`),
      purpose: strictText(item.purpose, `${field}.purpose`, 1, 500),
      required: item.required
    };
  });
}

function validateDependencyDAG(elements) {
  const byKey = new Map(elements.map((element) => [element.key, element]));
  elements.forEach((element) => {
    element.dependencyKeys.forEach((dependency) => {
      if (!byKey.has(dependency)) {
        throw contractError(`elements.${element.key}.dependency_keys 引用了未知 Element`, 'elements.dependency_keys');
      }
    });
  });
  const visiting = new Set();
  const visited = new Set();
  const visit = (key) => {
    if (visiting.has(key)) throw contractError('elements.dependency_keys 形成依赖环', 'elements.dependency_keys');
    if (visited.has(key)) return;
    visiting.add(key);
    byKey.get(key).dependencyKeys.forEach(visit);
    visiting.delete(key);
    visited.add(key);
  };
  elements.forEach((element) => visit(element.key));
}

function validateDurationBudget(elements) {
  const total = elements.reduce((sum, element) => sum + element.durationSeconds, 0);
  if (!Number.isSafeInteger(total) || total < 5 || total > 600) {
    throw contractError('elements.duration_seconds 总时长必须为 5 至 600 秒', 'elements.duration_seconds');
  }
}

// Business 使用 Go encoding/json 生成最多 64 KiB 的 canonical Content；前端按同样的 HTML escape 语义复核 UTF-8 字节数。
function validateCanonicalContentBytes(value) {
  let encoded;
  try {
    encoded = JSON.stringify({
      title: value.title,
      summary: value.summary,
      sections: value.sections,
      elements: value.elements,
      slots: value.slots
    });
  } catch {
    throw contractError('Storyboard Preview Content 无法编码为 canonical JSON', 'Storyboard Preview Content');
  }
  const goCanonical = encoded.replace(/[<>&]/gu, (character) => ({
    '<': '\\u003c',
    '>': '\\u003e',
    '&': '\\u0026'
  })[character]);
  if (new TextEncoder().encode(goCanonical).byteLength > STORYBOARD_PREVIEW_MAX_CANONICAL_CONTENT_BYTES) {
    throw contractError('Storyboard Preview Content 超出 64 KiB UTF-8 字节上限', 'Storyboard Preview Content');
  }
}

function validateSectionCoverage(sections, elements) {
  const used = new Set(elements.map((element) => element.sectionKey));
  if (sections.some((section) => !used.has(section.key))) {
    throw contractError('sections 包含没有 Element 的空 Section', 'sections');
  }
}

function validateSlotCounts(elements, slots) {
  const counts = new Map(elements.map((element) => [element.key, 0]));
  slots.forEach((slot) => counts.set(slot.elementKey, counts.get(slot.elementKey) + 1));
  if ([...counts.values()].some((count) => count > 4)) {
    throw contractError('单个 Element 的 Slot 数量超过 4', 'slots');
  }
}

function normalizedText(value, field, minimum, maximum) {
  if (typeof value !== 'string') throw contractError(`${field} 必须为字符串`, field);
  return strictText(value.trim().normalize('NFC'), field, minimum, maximum);
}

function strictText(value, field, minimum, maximum) {
  if (typeof value !== 'string') throw contractError(`${field} 必须为字符串`, field);
  if (!isUnicodeScalarString(value) || value !== value.normalize('NFC')) {
    throw contractError(`${field} 必须为合法 NFC Unicode 字符串`, field);
  }
  if (value.trim() !== value || /[\u0000-\u001f\u007f-\u009f\u2028\u2029]/u.test(value)) {
    throw contractError(`${field} 包含边界空白或控制字符`, field);
  }
  const length = [...value].length;
  if (length < minimum || length > maximum) throw contractError(`${field} 长度超出冻结范围`, field);
  return value;
}

function isUnicodeScalarString(value) {
  for (let index = 0; index < value.length; index += 1) {
    const unit = value.charCodeAt(index);
    if (unit >= 0xd800 && unit <= 0xdbff) {
      const next = value.charCodeAt(index + 1);
      if (!Number.isInteger(next) || next < 0xdc00 || next > 0xdfff) return false;
      index += 1;
    } else if (unit >= 0xdc00 && unit <= 0xdfff) {
      return false;
    }
  }
  return true;
}

function exactObject(value, fields, label) {
  object(value, label);
  const actual = Object.keys(value).sort();
  const expected = [...fields].sort();
  if (actual.length !== expected.length || actual.some((key, index) => key !== expected[index])) {
    throw contractError(`${label} 字段集合不符合冻结契约`, label);
  }
  return value;
}

function object(value, label) {
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw contractError(`${label} 必须为对象`, label);
  return value;
}

function objectArray(value, field, minimum, maximum, parser) {
  if (!Array.isArray(value) || value.length < minimum || value.length > maximum) {
    throw contractError(`${field} 数量不符合冻结契约`, field);
  }
  return value.map((item, index) => parser(object(item, `${field}[${index}]`), index));
}

function stringArray(value, field, minimum, maximum, parser) {
  if (!Array.isArray(value) || value.length < minimum || value.length > maximum) {
    throw contractError(`${field} 数量不符合冻结契约`, field);
  }
  return value.map((item, index) => parser(item, `${field}[${index}]`));
}

function localKey(value, field, pattern) {
  if (typeof value !== 'string' || !pattern.test(value)) throw contractError(`${field} 不是冻结局部 key`, field);
  return value;
}

function enumValue(value, allowed, field) {
  if (typeof value !== 'string' || !allowed.has(value)) throw contractError(`${field} 使用了未知枚举`, field);
  return value;
}

function uuidV7(value, field) {
  if (!isPlanStoryboardPreviewUUIDV7(value)) throw contractError(`${field} 必须为规范小写 UUIDv7`, field);
  return value;
}

function sha256(value, field) {
  if (typeof value !== 'string' || !SHA256_PATTERN.test(value)) throw contractError(`${field} 必须为小写 SHA-256`, field);
  return value;
}

function exactVersion(value, field) {
  const version = boundedInteger(value, field, 1, 1);
  return version;
}

function boundedInteger(value, field, minimum, maximum) {
  if (!Number.isSafeInteger(value) || value < minimum || value > maximum) {
    throw contractError(`${field} 必须为 ${minimum} 至 ${maximum} 的安全整数`, field);
  }
  return value;
}

function resultCode(value, field) {
  if (typeof value !== 'string' || !RESULT_CODE_PATTERN.test(value)) throw contractError(`${field} 不是稳定错误码`, field);
  return value;
}

function timestamp(value, field) {
  const parsed = strictText(value, field, 1, 64);
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
  return new PlanStoryboardPreviewContractError(message, field, code);
}
