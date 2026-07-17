export const GENERATE_MEDIA_REQUEST_SCHEMA = 'generate_media.preview.enqueue-request.v1';
export const GENERATE_MEDIA_INTENT_SCHEMA = 'generate_media.intent.v3preview1';
export const ASSEMBLE_OUTPUT_REQUEST_SCHEMA = 'assemble_output.preview.enqueue-request.v1';
export const ASSEMBLE_OUTPUT_INTENT_SCHEMA = 'assemble_output.intent.v3preview1';
export const MEDIA_PREVIEW_ENQUEUE_SCHEMA = 'media_preview.enqueue.v1';
export const MEDIA_PREVIEW_CARD_SCHEMA = 'media_preview.card.v1';

const UUID_V7 = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
const SHA256 = /^[0-9a-f]{64}$/;
const TARGET_LOCAL_KEY = /^[a-z][a-z0-9]*(?:_[a-z0-9]+)*$/;
const RESULT_CODE = /^[A-Z][A-Z0-9_]{0,63}$/;
const RFC3339 = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$/;
const CONTENT_PATH = /^\/api\/v1\/projects\/([0-9a-f-]{36})\/media-preview-assets\/([0-9a-f-]{36})\/content$/;

const GENERATE_ROOT = ['schema_version', 'prompt_preview_ref', 'tool_intent'];
const GENERATE_REF = ['id', 'version', 'content_digest'];
const GENERATE_INTENT = [
  'schema_version', 'prompt_preview_id', 'expected_prompt_version',
  'expected_prompt_content_digest', 'target_local_key', 'output_profile'
];
const ASSEMBLE_ROOT = ['schema_version', 'source_asset_ref', 'tool_intent'];
const ASSEMBLE_REF = ['id', 'version', 'content_digest'];
const ASSEMBLE_INTENT = [
  'schema_version', 'source_asset_id', 'expected_source_version',
  'expected_source_content_digest', 'output_profile'
];
const ENQUEUE_FIELDS = [
  'schema_version', 'request_id', 'session_id', 'input_id', 'turn_id', 'run_id', 'tool_call_id',
  'tool_key', 'status', 'replayed'
];
const CARD_BASE = [
  'schema_version', 'input_id', 'turn_id', 'run_id', 'tool_call_id', 'tool_key',
  'status', 'result_code', 'updated_at'
];
const ACCEPTED_FIELDS = [...CARD_BASE, 'operation_id', 'batch_id', 'asset_ref'];
const COMPLETED_FIELDS = [...ACCEPTED_FIELDS, 'job_id', 'content_url'];
const EARLY_FAILED_FIELDS = [...CARD_BASE, 'error_code'];
const TERMINAL_FAILED_FIELDS = [...EARLY_FAILED_FIELDS, 'operation_id', 'batch_id', 'job_id', 'asset_ref'];
const RESERVED_ASSET_FIELDS = ['id', 'version', 'status', 'media_kind', 'mime_type'];
const READY_ASSET_FIELDS = [...RESERVED_ASSET_FIELDS, 'content_digest', 'size_bytes'];

export class MediaPreviewContractError extends Error {
  constructor(message) {
    super(message);
    this.name = 'MediaPreviewContractError';
    this.code = 'INVALID_MEDIA_PREVIEW_CONTRACT';
    this.status = 502;
    this.retryable = false;
  }
}

// normalizeGenerateMediaPreviewRequest 只编码 Prompt Draft 引用、图片目标与固定输出 Profile。
export function normalizeGenerateMediaPreviewRequest({ promptPreview, targetLocalKey } = {}) {
  if (!promptPreview || promptPreview.status !== 'completed') throw error('Generate Media 需要 completed Prompt Preview');
  const target = promptPreview.prompts?.find((item) => item.targetLocalKey === targetLocalKey);
  if (!target || target.mediaKind !== 'image') throw error('Generate Media 目标必须是 Prompt Preview 中唯一图片目标');
  const id = uuid(promptPreview.promptPreviewID, 'promptPreview.id');
  const version = versionOne(promptPreview.version, 'promptPreview.version');
  const digest = sha(promptPreview.contentDigest, 'promptPreview.contentDigest');
  const key = localKey(targetLocalKey, 'targetLocalKey');
  return {
    schema_version: GENERATE_MEDIA_REQUEST_SCHEMA,
    prompt_preview_ref: { id, version, content_digest: digest },
    tool_intent: {
      schema_version: GENERATE_MEDIA_INTENT_SCHEMA,
      prompt_preview_id: id,
      expected_prompt_version: version,
      expected_prompt_content_digest: digest,
      target_local_key: key,
      output_profile: 'png_640x360.v1'
    }
  };
}

// parseGenerateMediaPreviewRequest 是 API helper 与测试共用的递归严格编码门禁。
export function parseGenerateMediaPreviewRequest(payload) {
  const root = exactObject(payload, GENERATE_ROOT, 'Generate Media Request');
  exact(root.schema_version, GENERATE_MEDIA_REQUEST_SCHEMA, 'schema_version');
  const ref = parseVersionedRef(root.prompt_preview_ref, GENERATE_REF, 'prompt_preview_ref');
  const intent = exactObject(root.tool_intent, GENERATE_INTENT, 'tool_intent');
  exact(intent.schema_version, GENERATE_MEDIA_INTENT_SCHEMA, 'tool_intent.schema_version');
  const normalized = {
    schema_version: root.schema_version,
    prompt_preview_ref: ref,
    tool_intent: {
      schema_version: intent.schema_version,
      prompt_preview_id: uuid(intent.prompt_preview_id, 'tool_intent.prompt_preview_id'),
      expected_prompt_version: versionOne(intent.expected_prompt_version, 'tool_intent.expected_prompt_version'),
      expected_prompt_content_digest: sha(intent.expected_prompt_content_digest, 'tool_intent.expected_prompt_content_digest'),
      target_local_key: localKey(intent.target_local_key, 'tool_intent.target_local_key'),
      output_profile: exact(intent.output_profile, 'png_640x360.v1', 'tool_intent.output_profile')
    }
  };
  assertRefMatchesIntent(ref, normalized.tool_intent, 'prompt_preview_id', 'expected_prompt_version', 'expected_prompt_content_digest');
  return normalized;
}

// normalizeAssembleOutputPreviewRequest 只接受当前工作台 completed PNG Card 的 ready Asset Ref。
export function normalizeAssembleOutputPreviewRequest({ mediaCard } = {}) {
  if (!mediaCard || mediaCard.status !== 'completed' || mediaCard.toolKey !== 'generate_media' ||
      mediaCard.assetRef?.status !== 'ready' || mediaCard.assetRef?.mediaKind !== 'image' ||
      mediaCard.assetRef?.mimeType !== 'image/png') {
    throw error('Assemble Output 需要 completed ready PNG Card');
  }
  const id = uuid(mediaCard.assetRef.id, 'assetRef.id');
  const version = versionOne(mediaCard.assetRef.version, 'assetRef.version');
  const digest = sha(mediaCard.assetRef.contentDigest, 'assetRef.contentDigest');
  return {
    schema_version: ASSEMBLE_OUTPUT_REQUEST_SCHEMA,
    source_asset_ref: { id, version, content_digest: digest },
    tool_intent: {
      schema_version: ASSEMBLE_OUTPUT_INTENT_SCHEMA,
      source_asset_id: id,
      expected_source_version: version,
      expected_source_content_digest: digest,
      output_profile: 'mp4_h264_640x360_2s.v1'
    }
  };
}

export function parseAssembleOutputPreviewRequest(payload) {
  const root = exactObject(payload, ASSEMBLE_ROOT, 'Assemble Output Request');
  exact(root.schema_version, ASSEMBLE_OUTPUT_REQUEST_SCHEMA, 'schema_version');
  const ref = parseVersionedRef(root.source_asset_ref, ASSEMBLE_REF, 'source_asset_ref');
  const intent = exactObject(root.tool_intent, ASSEMBLE_INTENT, 'tool_intent');
  exact(intent.schema_version, ASSEMBLE_OUTPUT_INTENT_SCHEMA, 'tool_intent.schema_version');
  const normalized = {
    schema_version: root.schema_version,
    source_asset_ref: ref,
    tool_intent: {
      schema_version: intent.schema_version,
      source_asset_id: uuid(intent.source_asset_id, 'tool_intent.source_asset_id'),
      expected_source_version: versionOne(intent.expected_source_version, 'tool_intent.expected_source_version'),
      expected_source_content_digest: sha(intent.expected_source_content_digest, 'tool_intent.expected_source_content_digest'),
      output_profile: exact(intent.output_profile, 'mp4_h264_640x360_2s.v1', 'tool_intent.output_profile')
    }
  };
  assertRefMatchesIntent(ref, normalized.tool_intent, 'source_asset_id', 'expected_source_version', 'expected_source_content_digest');
  return normalized;
}

export function parseMediaPreviewEnqueue(payload, expectedSessionID, expectedToolKey) {
  const value = exactObject(payload, ENQUEUE_FIELDS, 'Media Preview Enqueue');
  exact(value.schema_version, MEDIA_PREVIEW_ENQUEUE_SCHEMA, 'schema_version');
  const sessionID = uuid(value.session_id, 'session_id');
  if (sessionID !== uuid(expectedSessionID, 'expectedSessionID')) throw error('Media Preview Enqueue session_id 不一致');
  const toolKey = tool(value.tool_key);
  if (toolKey !== expectedToolKey) throw error('Media Preview Enqueue tool_key 不一致');
  exact(value.status, 'pending', 'status');
  if (typeof value.replayed !== 'boolean') throw error('Media Preview Enqueue replayed 必须为布尔值');
  return {
    schemaVersion: value.schema_version,
    requestID: uuid(value.request_id, 'request_id'),
    sessionID,
    inputID: uuid(value.input_id, 'input_id'),
    turnID: uuid(value.turn_id, 'turn_id'),
    runID: uuid(value.run_id, 'run_id'),
    toolCallID: uuid(value.tool_call_id, 'tool_call_id'),
    toolKey,
    status: value.status,
    replayed: value.replayed
  };
}

// parseMediaPreviewCard 严格区分 accepted、completed、派发前 failed 和 Worker terminal failed。
export function parseMediaPreviewCard(payload, { expectedProjectID } = {}) {
  const candidate = object(payload, 'Media Preview Card');
  let fields;
  if (candidate.status === 'accepted') fields = ACCEPTED_FIELDS;
  else if (candidate.status === 'completed') fields = COMPLETED_FIELDS;
  else if (candidate.status === 'failed') fields = Object.hasOwn(candidate, 'operation_id') ? TERMINAL_FAILED_FIELDS : EARLY_FAILED_FIELDS;
  else throw error('Media Preview Card status 未知');
  const value = exactObject(candidate, fields, 'Media Preview Card');
  exact(value.schema_version, MEDIA_PREVIEW_CARD_SCHEMA, 'schema_version');
  const base = {
    kind: 'media_preview', schemaVersion: value.schema_version,
    inputID: uuid(value.input_id, 'input_id'), turnID: uuid(value.turn_id, 'turn_id'),
    runID: uuid(value.run_id, 'run_id'), toolCallID: uuid(value.tool_call_id, 'tool_call_id'),
    toolKey: tool(value.tool_key), status: value.status,
    resultCode: resultCode(value.result_code), updatedAt: timestamp(value.updated_at, 'updated_at')
  };
  if (value.status === 'failed' && !Object.hasOwn(value, 'operation_id')) {
    return { ...base, operationID: '', batchID: '', jobID: '', assetRef: null, contentURL: '', errorCode: resultCode(value.error_code) };
  }
  const assetRef = parseAssetRef(value.asset_ref, value.status, base.toolKey);
  const common = {
    ...base, operationID: uuid(value.operation_id, 'operation_id'),
    batchID: uuid(value.batch_id, 'batch_id'), assetRef,
    jobID: Object.hasOwn(value, 'job_id') ? uuid(value.job_id, 'job_id') : ''
  };
  if (value.status === 'completed') {
    return {
      ...common,
      contentURL: contentPath(value.content_url, assetRef.id, expectedProjectID),
      errorCode: ''
    };
  }
  if (value.status === 'failed') {
    return { ...common, contentURL: '', errorCode: resultCode(value.error_code) };
  }
  return { ...common, contentURL: '', errorCode: '' };
}

export function parseMediaPreviewProjection(payload, options = {}) {
  if (payload === null) return [];
  if (!Array.isArray(payload) || payload.length > 16) throw error('Media Preview Projection 必须是最多 16 条 Card');
  const cards = payload.map((card) => parseMediaPreviewCard(card, options));
  const keys = cards.map((card) => `${card.inputID}:${card.status}:${card.jobID}`);
  if (new Set(keys).size !== keys.length) throw error('Media Preview Projection 包含重复 Card');
  return cards;
}

export function isMediaPreviewUUIDV7(value) {
  return typeof value === 'string' && UUID_V7.test(value);
}

function parseVersionedRef(value, fields, label) {
  const ref = exactObject(value, fields, label);
  return { id: uuid(ref.id, `${label}.id`), version: versionOne(ref.version, `${label}.version`), content_digest: sha(ref.content_digest, `${label}.content_digest`) };
}

function assertRefMatchesIntent(ref, intent, idField, versionField, digestField) {
  if (ref.id !== intent[idField] || ref.version !== intent[versionField] || ref.content_digest !== intent[digestField]) {
    throw error('Media Preview 外层 Ref 与 tool_intent 不一致');
  }
}

function parseAssetRef(value, status, toolKey) {
  const ready = status === 'completed';
  const ref = exactObject(value, ready ? READY_ASSET_FIELDS : RESERVED_ASSET_FIELDS, 'asset_ref');
  const expectedKind = toolKey === 'generate_media' ? 'image' : 'video';
  const expectedMIME = toolKey === 'generate_media' ? 'image/png' : 'video/mp4';
  const expectedStatus = ready ? 'ready' : status === 'accepted' ? 'reserved' : 'failed';
  exact(ref.status, expectedStatus, 'asset_ref.status');
  exact(ref.media_kind, expectedKind, 'asset_ref.media_kind');
  exact(ref.mime_type, expectedMIME, 'asset_ref.mime_type');
  const parsed = {
    id: uuid(ref.id, 'asset_ref.id'), version: versionOne(ref.version, 'asset_ref.version'),
    status: ref.status, mediaKind: ref.media_kind, mimeType: ref.mime_type,
    contentDigest: '', sizeBytes: 0
  };
  if (ready) {
    parsed.contentDigest = sha(ref.content_digest, 'asset_ref.content_digest');
    if (!Number.isSafeInteger(ref.size_bytes) || ref.size_bytes < 1) throw error('asset_ref.size_bytes 必须为正整数');
    parsed.sizeBytes = ref.size_bytes;
  }
  return parsed;
}

function contentPath(value, assetID, expectedProjectID) {
  if (typeof value !== 'string') throw error('content_url 必须是同源相对路径');
  const match = CONTENT_PATH.exec(value);
  if (!match || !UUID_V7.test(match[1]) || match[2] !== assetID) throw error('content_url 不符合受保护媒体端点');
  if (expectedProjectID != null && match[1] !== uuid(expectedProjectID, 'expectedProjectID')) {
    throw error('content_url 的 project_id 与 Workspace Binding 不一致');
  }
  return value;
}

function exactObject(value, fields, label) {
  object(value, label);
  const actual = Object.keys(value).sort();
  const expected = [...fields].sort();
  if (actual.length !== expected.length || actual.some((key, index) => key !== expected[index])) {
    throw error(`${label} 字段集合不符合冻结契约`);
  }
  return value;
}

function object(value, label) {
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw error(`${label} 必须为对象`);
  return value;
}

function uuid(value, field) {
  if (!isMediaPreviewUUIDV7(value)) throw error(`${field} 必须为规范 UUIDv7`);
  return value;
}

function sha(value, field) {
  if (typeof value !== 'string' || !SHA256.test(value)) throw error(`${field} 必须为 lowercase SHA-256`);
  return value;
}

function localKey(value, field) {
  if (typeof value !== 'string' || value.length > 128 || !TARGET_LOCAL_KEY.test(value)) throw error(`${field} 不是规范 target local key`);
  return value;
}

function versionOne(value, field) {
  if (value !== 1) throw error(`${field} 必须为版本 1`);
  return value;
}

function tool(value) {
  if (value !== 'generate_media' && value !== 'assemble_output') throw error('tool_key 未知');
  return value;
}

function resultCode(value) {
  if (typeof value !== 'string' || !RESULT_CODE.test(value)) throw error('结果码不符合白名单格式');
  return value;
}

function timestamp(value, field) {
  if (typeof value !== 'string' || !RFC3339.test(value) || !Number.isFinite(Date.parse(value))) throw error(`${field} 不是 RFC3339`);
  return value;
}

function exact(actual, expected, field) {
  if (actual !== expected) throw error(`${field} 不符合冻结契约`);
  return actual;
}

function error(message) {
  return new MediaPreviewContractError(message);
}
