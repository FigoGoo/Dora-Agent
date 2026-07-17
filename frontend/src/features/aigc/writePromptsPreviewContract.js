export const WRITE_PROMPTS_PREVIEW_ENQUEUE_REQUEST_SCHEMA = 'write_prompts.preview.enqueue-request.v1';
export const WRITE_PROMPTS_PREVIEW_INTENT_SCHEMA = 'write_prompts.preview.intent.v1';
export const WRITE_PROMPTS_PREVIEW_ENQUEUE_SCHEMA = 'write_prompts.preview.enqueue.v1';
export const PROMPT_PREVIEW_CARD_SCHEMA = 'prompt.preview.card.v1';

export const PROMPT_PREVIEW_SLOT_TYPES = Object.freeze(['image', 'video', 'audio', 'voiceover', 'caption']);
export const PROMPT_PREVIEW_MEDIA_KINDS = Object.freeze(['image', 'video', 'audio', 'text']);
export const PROMPT_PREVIEW_OUTPUT_LANGUAGES = Object.freeze(['zh-CN', 'en-US']);
export const PROMPT_PREVIEW_COMPLETED_RESULT_CODES = Object.freeze(['PROMPT_PREVIEW_DRAFT_CREATED']);
export const PROMPT_PREVIEW_TOOL_FAILURE_RESULT_CODES = Object.freeze([
  'PROMPT_PREVIEW_INVALID_ARGUMENT',
  'PROMPT_PREVIEW_STORYBOARD_NOT_FOUND',
  'PROMPT_PREVIEW_STORYBOARD_CONFLICT',
  'PROMPT_PREVIEW_NO_TARGETS',
  'PROMPT_PREVIEW_TARGET_BUDGET_EXCEEDED',
  'PROMPT_PREVIEW_CANDIDATE_INVALID',
  'PROMPT_PREVIEW_EXACT_TARGET_SET_INVALID',
  'PROMPT_PREVIEW_CONFLICT',
  'PROMPT_PREVIEW_DISABLED'
]);
export const PROMPT_PREVIEW_RUNTIME_FAILURE_RESULT_CODES = Object.freeze(['WRITE_PROMPTS_RUNTIME_FAILED']);

const UUID_V7_PATTERN = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
const SHA256_PATTERN = /^[0-9a-f]{64}$/;
const RFC3339_PATTERN = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$/;
const TARGET_KEY_PATTERN = /^slot_([1-9]|[1-8][0-9]|9[0-6])$/;
const ELEMENT_KEY_PATTERN = /^element_([1-9]|1[0-9]|2[0-4])$/;
const REQUEST_FIELDS = Object.freeze(['schema_version', 'storyboard_preview_ref', 'tool_intent']);
const STORYBOARD_REF_FIELDS = Object.freeze(['id', 'version', 'content_digest']);
const INTENT_REQUIRED_FIELDS = Object.freeze(['schema_version', 'writing_instruction']);
const INTENT_WITH_LANGUAGE_FIELDS = Object.freeze([...INTENT_REQUIRED_FIELDS, 'output_language']);
const ENQUEUE_FIELDS = Object.freeze([
  'schema_version', 'request_id', 'session_id', 'input_id', 'turn_id', 'run_id', 'tool_call_id', 'status', 'replayed'
]);
const COMPLETED_CARD_FIELDS = Object.freeze([
  'schema_version', 'input_id', 'turn_id', 'run_id', 'tool_call_id', 'status', 'result_code', 'updated_at',
  'prompt_preview_id', 'project_id', 'storyboard_preview_ref', 'version', 'content_digest', 'target_count', 'prompts'
]);
const FAILED_CARD_FIELDS = Object.freeze([
  'schema_version', 'input_id', 'turn_id', 'run_id', 'tool_call_id', 'status', 'result_code', 'updated_at',
  'failure_kind', 'summary', 'retryable'
]);
const PROMPT_FIELDS = Object.freeze([
  'target_local_key', 'element_local_key', 'slot_type', 'media_kind', 'purpose', 'required',
  'positive_prompt', 'negative_constraints', 'output_language'
]);
const SLOT_TYPES = new Set(PROMPT_PREVIEW_SLOT_TYPES);
const MEDIA_KINDS = new Set(PROMPT_PREVIEW_MEDIA_KINDS);
const OUTPUT_LANGUAGES = new Set(PROMPT_PREVIEW_OUTPUT_LANGUAGES);
const COMPLETED_RESULT_CODES = new Set(PROMPT_PREVIEW_COMPLETED_RESULT_CODES);
const TOOL_FAILURE_RESULT_CODES = new Set(PROMPT_PREVIEW_TOOL_FAILURE_RESULT_CODES);
const RUNTIME_FAILURE_RESULT_CODES = new Set(PROMPT_PREVIEW_RUNTIME_FAILURE_RESULT_CODES);
const SLOT_MEDIA_KIND = Object.freeze({ image: 'image', video: 'video', audio: 'audio', voiceover: 'audio', caption: 'text' });

// WritePromptsPreviewContractError 表示前端无法安全消费 Prompt Development Preview 契约。
export class WritePromptsPreviewContractError extends Error {
  constructor(message, field = '', code = 'INVALID_WRITE_PROMPTS_PREVIEW') {
    super(message);
    this.name = 'WritePromptsPreviewContractError';
    this.field = field;
    this.code = code;
    this.status = 502;
    this.retryable = false;
  }
}

// normalizeWritePromptsPreviewEnqueueRequest 只编码已验证 Source 引用与模型可控写作字段。
export function normalizeWritePromptsPreviewEnqueueRequest({ storyboardPreviewRef, toolIntent } = {}) {
  const ref = object(storyboardPreviewRef, 'storyboardPreviewRef');
  const intent = object(toolIntent, 'toolIntent');
  const normalized = {
    schema_version: WRITE_PROMPTS_PREVIEW_ENQUEUE_REQUEST_SCHEMA,
    storyboard_preview_ref: {
      id: uuidV7(ref.id, 'storyboardPreviewRef.id'),
      version: versionOne(ref.version, 'storyboardPreviewRef.version'),
      content_digest: sha256(ref.contentDigest, 'storyboardPreviewRef.contentDigest')
    },
    tool_intent: {
      schema_version: WRITE_PROMPTS_PREVIEW_INTENT_SCHEMA,
      writing_instruction: normalizedText(intent.writingInstruction, 'toolIntent.writingInstruction', 1, 1000)
    }
  };
  if (intent.outputLanguage !== undefined && intent.outputLanguage !== null && intent.outputLanguage !== '') {
    normalized.tool_intent.output_language = enumValue(intent.outputLanguage, OUTPUT_LANGUAGES, 'toolIntent.outputLanguage');
  }
  return normalized;
}

// parseWritePromptsPreviewEnqueueRequest 严格递归解析公开 BFF 请求。
export function parseWritePromptsPreviewEnqueueRequest(payload) {
  const value = exactObject(payload, REQUEST_FIELDS, 'Write Prompts Preview Enqueue Request');
  exact(value.schema_version, WRITE_PROMPTS_PREVIEW_ENQUEUE_REQUEST_SCHEMA, 'schema_version');
  const ref = parseStoryboardPreviewRef(value.storyboard_preview_ref, 'storyboard_preview_ref');
  const intentCandidate = object(value.tool_intent, 'tool_intent');
  const intent = exactObject(
    intentCandidate,
    Object.hasOwn(intentCandidate, 'output_language') ? INTENT_WITH_LANGUAGE_FIELDS : INTENT_REQUIRED_FIELDS,
    'tool_intent'
  );
  exact(intent.schema_version, WRITE_PROMPTS_PREVIEW_INTENT_SCHEMA, 'tool_intent.schema_version');
  const parsed = {
    schema_version: value.schema_version,
    storyboard_preview_ref: toWireRef(ref),
    tool_intent: {
      schema_version: intent.schema_version,
      writing_instruction: strictText(intent.writing_instruction, 'tool_intent.writing_instruction', 1, 1000)
    }
  };
  if (Object.hasOwn(intent, 'output_language')) {
    parsed.tool_intent.output_language = enumValue(intent.output_language, OUTPUT_LANGUAGES, 'tool_intent.output_language');
  }
  return parsed;
}

// parseWritePromptsPreviewEnqueueResponse 只接受绑定当前 Session 的 pending/replayed 回执。
export function parseWritePromptsPreviewEnqueueResponse(payload, expectedSessionID) {
  const value = exactObject(payload, ENQUEUE_FIELDS, 'Write Prompts Preview Enqueue');
  exact(value.schema_version, WRITE_PROMPTS_PREVIEW_ENQUEUE_SCHEMA, 'schema_version');
  const sessionID = uuidV7(value.session_id, 'session_id');
  if (sessionID !== uuidV7(expectedSessionID, 'expected session_id')) {
    throw contractError('Write Prompts Preview Enqueue 的 session_id 不一致', 'session_id');
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

// parsePromptPreviewCard 是 Snapshot 与 SSE 共用的严格递归 Card Parser。
export function parsePromptPreviewCard(payload) {
  const candidate = object(payload, 'Prompt Preview Card');
  const completed = candidate.status === 'completed';
  const value = exactObject(candidate, completed ? COMPLETED_CARD_FIELDS : FAILED_CARD_FIELDS, 'Prompt Preview Card');
  exact(value.schema_version, PROMPT_PREVIEW_CARD_SCHEMA, 'schema_version');
  const resultCode = nonEmptyString(value.result_code, 'result_code');
  const base = {
    kind: 'write_prompts_preview',
    schemaVersion: value.schema_version,
    inputID: uuidV7(value.input_id, 'input_id'),
    turnID: uuidV7(value.turn_id, 'turn_id'),
    runID: uuidV7(value.run_id, 'run_id'),
    toolCallID: uuidV7(value.tool_call_id, 'tool_call_id'),
    status: value.status,
    resultCode,
    updatedAt: timestamp(value.updated_at, 'updated_at')
  };
  if (!completed) {
    exact(value.status, 'failed', 'status');
    const failureKind = enumValue(value.failure_kind, new Set(['tool', 'runtime']), 'failure_kind');
    enumValue(resultCode, failureKind === 'runtime' ? RUNTIME_FAILURE_RESULT_CODES : TOOL_FAILURE_RESULT_CODES, 'result_code');
    if (typeof value.retryable !== 'boolean') throw contractError('retryable 必须为布尔值', 'retryable');
    return {
      ...base,
      failureKind,
      promptPreviewID: '',
      projectID: '',
      storyboardPreviewRef: null,
      version: 0,
      contentDigest: '',
      targetCount: 0,
      prompts: [],
      summary: strictText(value.summary, 'summary', 1, 1000),
      retryable: value.retryable
    };
  }

  exact(value.status, 'completed', 'status');
  enumValue(resultCode, COMPLETED_RESULT_CODES, 'result_code');
  const targetCount = boundedInteger(value.target_count, 'target_count', 1, 96);
  const prompts = objectArray(value.prompts, 'prompts', 1, 96, parsePrompt);
  if (prompts.length !== targetCount) throw contractError('target_count 与 prompts 数量不一致', 'target_count');
  assertUnique(prompts, 'targetLocalKey', 'prompts.target_local_key');
  return {
    ...base,
    failureKind: null,
    promptPreviewID: uuidV7(value.prompt_preview_id, 'prompt_preview_id'),
    projectID: uuidV7(value.project_id, 'project_id'),
    storyboardPreviewRef: parseStoryboardPreviewRef(value.storyboard_preview_ref, 'storyboard_preview_ref'),
    version: versionOne(value.version, 'version'),
    contentDigest: sha256(value.content_digest, 'content_digest'),
    targetCount,
    prompts,
    summary: '',
    retryable: false
  };
}

// parsePromptPreviewProjection 对 nullable Workspace 字段执行同一严格 Card Parser；未知 Schema 失败关闭。
export function parsePromptPreviewProjection(payload) {
  return payload === null ? null : parsePromptPreviewCard(payload);
}

// validatePromptPreviewSourceBinding 复核 Prompt 全集与当前 Storyboard Card 的 Source、Slot 与顺序完全一致。
export function validatePromptPreviewSourceBinding(promptPreview, storyboardPreview) {
  if (!promptPreview || promptPreview.status !== 'completed') return promptPreview;
  if (!storyboardPreview || storyboardPreview.kind !== 'plan_storyboard_preview' || storyboardPreview.status !== 'completed') {
    throw contractError('Prompt Preview 缺少可验证的 Storyboard Source', 'storyboard_preview_ref');
  }
  const ref = promptPreview.storyboardPreviewRef;
  if (ref.id !== storyboardPreview.storyboardPreviewID || ref.version !== storyboardPreview.version
      || ref.contentDigest !== storyboardPreview.contentDigest) {
    throw contractError('Prompt Preview 的 Storyboard Source Binding 不一致', 'storyboard_preview_ref');
  }
  if (promptPreview.prompts.length !== storyboardPreview.slots.length) {
    throw contractError('Prompt Preview 未覆盖 Storyboard 的完整 Slot 集合', 'prompts');
  }
  const elementOrder = new Map(storyboardPreview.elements.map((element) => [element.key, element.order]));
  const sourceSlots = [...storyboardPreview.slots].sort((left, right) => {
    const order = elementOrder.get(left.elementKey) - elementOrder.get(right.elementKey);
    return order || promptKeyNumber(left.key) - promptKeyNumber(right.key);
  });
  promptPreview.prompts.forEach((prompt, index) => {
    const slot = sourceSlots[index];
    if (!slot || prompt.targetLocalKey !== slot.key || prompt.elementLocalKey !== slot.elementKey
        || prompt.slotType !== slot.slotType || prompt.mediaKind !== SLOT_MEDIA_KIND[slot.slotType]
        || prompt.purpose !== slot.purpose || prompt.required !== slot.required) {
      throw contractError('Prompt Preview 的 target 引用或可信字段与 Storyboard Slot 不一致', `prompts[${index}]`);
    }
  });
  return promptPreview;
}

export function isWritePromptsPreviewUUIDV7(value) {
  return typeof value === 'string' && UUID_V7_PATTERN.test(value);
}

function parsePrompt(value, index) {
  const field = `prompts[${index}]`;
  const prompt = exactObject(value, PROMPT_FIELDS, field);
  const slotType = enumValue(prompt.slot_type, SLOT_TYPES, `${field}.slot_type`);
  const mediaKind = enumValue(prompt.media_kind, MEDIA_KINDS, `${field}.media_kind`);
  if (mediaKind !== SLOT_MEDIA_KIND[slotType]) {
    throw contractError(`${field}.media_kind 与 slot_type 不一致`, `${field}.media_kind`);
  }
  if (typeof prompt.required !== 'boolean') throw contractError(`${field}.required 必须为布尔值`, `${field}.required`);
  const negativeConstraints = stringArray(prompt.negative_constraints, `${field}.negative_constraints`, 0, 16, 1, 500);
  if (new Set(negativeConstraints).size !== negativeConstraints.length) {
    throw contractError(`${field}.negative_constraints 包含重复项`, `${field}.negative_constraints`);
  }
  return {
    targetLocalKey: localKey(prompt.target_local_key, `${field}.target_local_key`, TARGET_KEY_PATTERN),
    elementLocalKey: localKey(prompt.element_local_key, `${field}.element_local_key`, ELEMENT_KEY_PATTERN),
    slotType,
    mediaKind,
    purpose: strictText(prompt.purpose, `${field}.purpose`, 1, 500),
    required: prompt.required,
    positivePrompt: strictText(prompt.positive_prompt, `${field}.positive_prompt`, 1, 4000),
    negativeConstraints,
    outputLanguage: enumValue(prompt.output_language, OUTPUT_LANGUAGES, `${field}.output_language`)
  };
}

function parseStoryboardPreviewRef(value, field) {
  const ref = exactObject(value, STORYBOARD_REF_FIELDS, field);
  return {
    id: uuidV7(ref.id, `${field}.id`),
    version: versionOne(ref.version, `${field}.version`),
    contentDigest: sha256(ref.content_digest, `${field}.content_digest`)
  };
}

function toWireRef(ref) {
  return { id: ref.id, version: ref.version, content_digest: ref.contentDigest };
}

function objectArray(value, field, minimum, maximum, parser) {
  if (!Array.isArray(value) || value.length < minimum || value.length > maximum) {
    throw contractError(`${field} 数量必须为 ${minimum} 至 ${maximum}`, field);
  }
  return value.map((item, index) => parser(item, index));
}

function stringArray(value, field, minimum, maximum, textMinimum, textMaximum) {
  if (!Array.isArray(value) || value.length < minimum || value.length > maximum) {
    throw contractError(`${field} 数量必须为 ${minimum} 至 ${maximum}`, field);
  }
  return value.map((item, index) => strictText(item, `${field}[${index}]`, textMinimum, textMaximum));
}

function object(value, field) {
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw contractError(`${field} 必须为对象`, field);
  return value;
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

function normalizedText(value, field, minimum, maximum) {
  if (typeof value !== 'string' || !validUnicodeScalarString(value)) throw contractError(`${field} 不是有效 NFC Unicode`, field);
  return strictText(value.trim().normalize('NFC'), field, minimum, maximum);
}

function strictText(value, field, minimum, maximum) {
  if (typeof value !== 'string' || !validUnicodeScalarString(value) || value !== value.normalize('NFC')) {
    throw contractError(`${field} 不是有效 NFC Unicode`, field);
  }
  if (value !== value.trim() || /[\u0000-\u0008\u000b\u000c\u000e-\u001f\u007f]/u.test(value)) {
    throw contractError(`${field} 包含边界空白或控制字符`, field);
  }
  const length = [...value].length;
  if (length < minimum || length > maximum) throw contractError(`${field} 长度必须为 ${minimum} 至 ${maximum}`, field);
  return value;
}

function validUnicodeScalarString(value) {
  for (let index = 0; index < value.length; index += 1) {
    const code = value.charCodeAt(index);
    if (code >= 0xd800 && code <= 0xdbff) {
      const next = value.charCodeAt(index + 1);
      if (!(next >= 0xdc00 && next <= 0xdfff)) return false;
      index += 1;
    } else if (code >= 0xdc00 && code <= 0xdfff) return false;
  }
  return true;
}

function localKey(value, field, pattern) {
  const parsed = nonEmptyString(value, field);
  if (!pattern.test(parsed)) throw contractError(`${field} 不是合法局部 key`, field);
  return parsed;
}

function uuidV7(value, field) {
  const parsed = nonEmptyString(value, field);
  if (!UUID_V7_PATTERN.test(parsed)) throw contractError(`${field} 必须为规范小写 UUIDv7`, field);
  return parsed;
}

function sha256(value, field) {
  if (typeof value !== 'string' || !SHA256_PATTERN.test(value)) throw contractError(`${field} 必须为小写 SHA-256`, field);
  return value;
}

function timestamp(value, field) {
  const parsed = nonEmptyString(value, field);
  if (!RFC3339_PATTERN.test(parsed) || Number.isNaN(Date.parse(parsed))) throw contractError(`${field} 必须为 RFC3339 时间`, field);
  return parsed;
}

function versionOne(value, field) {
  exact(value, 1, field);
  return value;
}

function boundedInteger(value, field, minimum, maximum) {
  if (!Number.isSafeInteger(value) || value < minimum || value > maximum) {
    throw contractError(`${field} 必须为 ${minimum} 至 ${maximum} 的安全整数`, field);
  }
  return value;
}

function enumValue(value, allowed, field) {
  const parsed = nonEmptyString(value, field);
  if (!allowed.has(parsed)) throw contractError(`${field} 使用了未知枚举`, field);
  return parsed;
}

function nonEmptyString(value, field) {
  if (typeof value !== 'string' || value === '') throw contractError(`${field} 必须为非空字符串`, field);
  return value;
}

function exact(actual, expected, field) {
  if (actual !== expected) throw contractError(`${field} 不符合冻结契约`, field);
  return actual;
}

function assertUnique(items, key, field) {
  if (new Set(items.map((item) => item[key])).size !== items.length) throw contractError(`${field} 包含重复项`, field);
}

function promptKeyNumber(value) {
  return Number(String(value).slice(String(value).lastIndexOf('_') + 1));
}

function contractError(message, field = '') {
  return new WritePromptsPreviewContractError(message, field);
}
