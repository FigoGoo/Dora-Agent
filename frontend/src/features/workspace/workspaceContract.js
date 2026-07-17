import {
  parseCreationSpecPreviewCard,
  parseCreationSpecPreviewFailure,
  parseCreationSpecPreviewProjection
} from '../aigc/creationSpecPreviewContract.js';
import {
  parseAnalyzeMaterialsPreviewCard,
  parseAnalyzeMaterialsProjection
} from '../aigc/analyzeMaterialsPreviewContract.js';
import {
  parseStoryboardPreviewCard,
  parseStoryboardPreviewProjection
} from '../aigc/planStoryboardPreviewContract.js';
import {
  parsePromptPreviewCard,
  parsePromptPreviewProjection,
  validatePromptPreviewSourceBinding
} from '../aigc/writePromptsPreviewContract.js';
import {
  parseDirectResponseCard,
  parseTurnFailureCard,
  parseTurnOutputProjection
} from './turnOutputContract.js';
import {
  parseMediaPreviewCard,
  parseMediaPreviewProjection
} from '../media/mediaPreviewContract.js';

// Workspace 前端只接受冻结的 Snapshot、Event 与控制帧版本。
export const WORKSPACE_SNAPSHOT_SCHEMA_V1 = 'session.workspace.v1';
export const WORKSPACE_SNAPSHOT_SCHEMA_V2 = 'session.workspace.v2';
export const WORKSPACE_SNAPSHOT_SCHEMA_V3 = 'session.workspace.v3';
export const WORKSPACE_SNAPSHOT_SCHEMA_V4 = 'session.workspace.v4';
export const WORKSPACE_SNAPSHOT_SCHEMA_V5 = 'session.workspace.v5';
export const WORKSPACE_SNAPSHOT_SCHEMA = WORKSPACE_SNAPSHOT_SCHEMA_V5;
export const WORKSPACE_EVENT_SCHEMA = 'workspace.event.v1';
export const SESSION_EVENT_PAYLOAD_SCHEMA = 'session.event.v1';
export const WORKSPACE_STREAM_CONTROL_SCHEMA = 'workspace.stream-control.v1';

export const WORKSPACE_PERSISTENT_EVENTS = Object.freeze([
  'session.created',
  'session.input.accepted',
  'session.turn.completed',
  'session.turn.failed',
  'session.turn.recovery_pending',
  'creation_spec.preview.completed',
  'creation_spec.preview.failed',
  'analyze_materials.preview.accepted',
  'analyze_materials.preview.completed',
  'analyze_materials.preview.partial',
  'analyze_materials.preview.failed',
  'analyze_materials.preview.runtime_failed',
  'plan_storyboard.preview.accepted',
  'plan_storyboard.preview.completed',
  'plan_storyboard.preview.failed',
  'plan_storyboard.preview.runtime_failed',
  'write_prompts.preview.accepted',
  'write_prompts.preview.completed',
  'write_prompts.preview.failed',
  'write_prompts.preview.runtime_failed',
  'media.preview.accepted',
  'media.preview.completed',
  'media.preview.failed',
  'media.preview.runtime_failed'
]);

export const WORKSPACE_INPUT_STATUSES = Object.freeze([
  'pending',
  'claimed',
  'running',
  'retry_wait',
  'recovery_pending',
  'resolved',
  'dead'
]);

export const WORKSPACE_INPUT_SOURCE_TYPES = Object.freeze([
  'user_message',
  'creation_spec_preview',
  'analyze_materials_preview',
  'plan_storyboard_preview',
  'write_prompts_preview',
  'generate_media_preview_request',
  'assemble_output_preview_request',
  'media_job_preview_terminal'
]);

const SESSION_STATUSES = new Set(['active', 'archived']);
const INPUT_STATUSES = new Set(WORKSPACE_INPUT_STATUSES);
const INPUT_SOURCE_TYPES = new Set(WORKSPACE_INPUT_SOURCE_TYPES);
const PROJECT_LIFECYCLE_STATUSES = new Set(['active', 'archived', 'trash', 'deleted']);
const PROJECT_RUN_STATUSES = new Set([
  'idle', 'queued', 'running', 'waiting_user', 'waiting_async', 'succeeded', 'partial_failed', 'failed', 'cancelled'
]);
const INITIAL_PROMPT_STATUSES = new Set(['absent', 'pending', 'accepted', 'failed']);
const RESET_REASONS = new Set(['cursor_expired', 'event_gap', 'projection_invalid']);
const WORKSPACE_SNAPSHOT_V1_FIELDS = Object.freeze([
  'schema_version', 'request_id', 'session', 'messages', 'inputs', 'creation_spec_preview',
  'event_high_watermark', 'min_available_seq'
]);
const WORKSPACE_SNAPSHOT_V2_FIELDS = Object.freeze([
  ...WORKSPACE_SNAPSHOT_V1_FIELDS, 'latest_turn_output', 'analyze_materials_preview'
]);
const WORKSPACE_SNAPSHOT_V3_FIELDS = Object.freeze([
  ...WORKSPACE_SNAPSHOT_V2_FIELDS, 'plan_storyboard_preview'
]);
const WORKSPACE_SNAPSHOT_V4_FIELDS = Object.freeze([
  ...WORKSPACE_SNAPSHOT_V3_FIELDS, 'write_prompts_preview'
]);
const WORKSPACE_SNAPSHOT_V5_FIELDS = Object.freeze([
  ...WORKSPACE_SNAPSHOT_V4_FIELDS, 'media_previews'
]);
const SESSION_FIELDS = Object.freeze([
  'id', 'project_id', 'status', 'version', 'created_at', 'updated_at'
]);
const MESSAGE_FIELDS = Object.freeze(['id', 'message_seq', 'role', 'content', 'created_at']);
const INPUT_FIELDS = Object.freeze([
  'id', 'message_id', 'source_type', 'status', 'enqueue_seq', 'available_at', 'created_at', 'updated_at'
]);
const WORKSPACE_EVENT_FIELDS = Object.freeze([
  'schema_version', 'payload_schema_version', 'event_id', 'session_id', 'project_id', 'seq', 'event',
  'occurred_at', 'aggregate_type', 'aggregate_id', 'aggregate_version', 'payload'
]);
const SESSION_CREATED_PAYLOAD_FIELDS = Object.freeze(['session_id', 'project_id', 'status', 'version']);
const INPUT_ACCEPTED_PAYLOAD_FIELDS = Object.freeze([
  'session_id', 'input_id', 'message_id', 'enqueue_seq', 'status'
]);
const ANALYZE_ACCEPTED_PAYLOAD_FIELDS = Object.freeze([
  'input_id', 'session_id', 'turn_id', 'run_id', 'request_id', 'source_type', 'intent_digest', 'tool_call_id',
  'context_digest'
]);
const PLAN_STORYBOARD_ACCEPTED_PAYLOAD_FIELDS = Object.freeze([
  'schema_version', 'input_id', 'turn_id', 'run_id', 'tool_call_id', 'business_command_id', 'intent_digest',
  'context_digest', 'creation_spec_id', 'creation_spec_version', 'creation_spec_content_digest'
]);
const WRITE_PROMPTS_ACCEPTED_PAYLOAD_FIELDS = Object.freeze([
  'schema_version', 'input_id', 'turn_id', 'run_id', 'tool_call_id', 'business_command_id', 'intent_digest',
  'context_digest', 'storyboard_preview_id', 'storyboard_preview_version', 'storyboard_preview_content_digest'
]);
const STREAM_READY_FIELDS = Object.freeze([
  'schema_version', 'event', 'session_id', 'cursor', 'min_available_seq', 'latest_seq'
]);
const STREAM_RESET_FIELDS = Object.freeze([
  'schema_version', 'event', 'session_id', 'reason', 'snapshot_required', 'min_available_seq', 'latest_seq'
]);
const UUID_V7_PATTERN = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
const MAX_CONTENT_BYTES = 64 * 1024;
const RFC3339_PATTERN = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$/;

// WorkspaceContractError 表示前端无法安全消费已成功 HTTP/SSE 响应，禁止猜测缺失字段。
export class WorkspaceContractError extends Error {
  constructor(message, code = 'INVALID_WORKSPACE_RESPONSE') {
    super(message);
    this.name = 'WorkspaceContractError';
    this.code = code;
    this.status = 502;
    this.retryable = false;
  }
}

// parseProjectBootstrap 校验 Business 权威 Project/Session Binding，不从 URL 或本地缓存猜测 Session。
export function parseProjectBootstrap(payload, expectedProjectID) {
  const value = object(payload, 'Project Bootstrap');
  const projectID = uuid(value.project_id, 'project_id');
  if (projectID !== uuid(expectedProjectID, 'expected project_id')) {
    throw contractError('Project Bootstrap 返回了错误的 project_id');
  }
  const creationStatus = enumValue(value.creation_status, new Set(['provisioning', 'ready']), 'creation_status');
  const sessionID = nullableUUID(value.session_id, 'session_id');
  const inputID = nullableUUID(value.input_id, 'input_id');
  const result = {
    projectID,
    title: nonEmptyString(value.title, 'title'),
    lifecycleStatus: enumValue(value.lifecycle_status, PROJECT_LIFECYCLE_STATUSES, 'lifecycle_status'),
    recentRunStatus: enumValue(value.recent_run_status, PROJECT_RUN_STATUSES, 'recent_run_status'),
    initialPromptStatus: enumValue(value.initial_prompt_status, INITIAL_PROMPT_STATUSES, 'initial_prompt_status'),
    creationStatus,
    sessionID,
    inputID,
    updatedAt: timestamp(value.updated_at, 'updated_at'),
    requestID: uuid(value.request_id, 'request_id')
  };
  if (creationStatus === 'provisioning' && (sessionID || inputID)) {
    throw contractError('Project Bootstrap provisioning 不得包含 Session/Input');
  }
  if (creationStatus === 'ready' && !sessionID) {
    throw contractError('Project Bootstrap ready 必须包含 session_id');
  }
  if (creationStatus === 'ready' && result.initialPromptStatus === 'absent' && inputID) {
    throw contractError('空 Prompt Project Bootstrap 不得包含 input_id');
  }
  return result;
}

// parseWorkspaceSnapshot 严格校验 Agent Snapshot，并交叉验证路由 Project 与 Business Binding。
export function parseWorkspaceSnapshot(payload, { expectedProjectID, expectedSessionID } = {}) {
  const candidate = object(payload, 'Workspace Snapshot');
  const schemaVersion = candidate.schema_version;
  const expectedFields = schemaVersion === WORKSPACE_SNAPSHOT_SCHEMA_V1
    ? WORKSPACE_SNAPSHOT_V1_FIELDS
    : schemaVersion === WORKSPACE_SNAPSHOT_SCHEMA_V2
      ? WORKSPACE_SNAPSHOT_V2_FIELDS
      : schemaVersion === WORKSPACE_SNAPSHOT_SCHEMA_V3
        ? WORKSPACE_SNAPSHOT_V3_FIELDS
        : schemaVersion === WORKSPACE_SNAPSHOT_SCHEMA_V4
          ? WORKSPACE_SNAPSHOT_V4_FIELDS
          : schemaVersion === WORKSPACE_SNAPSHOT_SCHEMA_V5 ? WORKSPACE_SNAPSHOT_V5_FIELDS : null;
  if (!expectedFields) throw contractError('Workspace Snapshot schema_version 未知');
  const value = exactObject(candidate, expectedFields, 'Workspace Snapshot');
  const session = parseSession(value.session);
  const projectID = uuid(expectedProjectID, 'expected project_id');
  const sessionID = uuid(expectedSessionID, 'expected session_id');
  if (session.projectID !== projectID || session.id !== sessionID) {
    throw contractError('Workspace Snapshot 的 Project/Session Binding 不一致');
  }
  if (!Array.isArray(value.messages) || !Array.isArray(value.inputs)) {
    throw contractError('Workspace Snapshot 的 messages/inputs 必须为数组');
  }
  const messages = value.messages.map(parseMessage);
  const inputs = value.inputs.map(parseInput);
  assertStrictlyIncreasing(messages, 'messageSeq', 'messages');
  assertStrictlyIncreasing(inputs, 'enqueueSeq', 'inputs');
  assertUnique(messages, 'id', 'messages');
  assertUnique(inputs, 'id', 'inputs');
  const messageIDs = new Set(messages.map((message) => message.id));
  inputs.forEach((input) => {
    if (input.messageID && !messageIDs.has(input.messageID)) {
      throw contractError('Workspace Snapshot Input 引用了不存在的 Message');
    }
  });
  const eventHighWatermark = safeInteger(value.event_high_watermark, 'event_high_watermark', { minimum: 0 });
  const minAvailableSeq = safeInteger(value.min_available_seq, 'min_available_seq', { minimum: 1 });
  if (minAvailableSeq > eventHighWatermark + 1) {
    throw contractError('Workspace Snapshot 的 Event Window 不一致');
  }
  const creationSpecPreview = parseCreationSpecPreviewProjection(value.creation_spec_preview, {
    expectedProjectID: projectID
  });
  const hasV2Projections = schemaVersion === WORKSPACE_SNAPSHOT_SCHEMA_V2
    || schemaVersion === WORKSPACE_SNAPSHOT_SCHEMA_V3 || schemaVersion === WORKSPACE_SNAPSHOT_SCHEMA_V4
    || schemaVersion === WORKSPACE_SNAPSHOT_SCHEMA_V5;
  const latestTurnOutput = hasV2Projections
    ? parseTurnOutputProjection(value.latest_turn_output)
    : null;
  const analyzeMaterialsPreview = hasV2Projections
    ? parseAnalyzeMaterialsProjection(value.analyze_materials_preview)
    : null;
  const planStoryboardPreview = schemaVersion === WORKSPACE_SNAPSHOT_SCHEMA_V3 || schemaVersion === WORKSPACE_SNAPSHOT_SCHEMA_V4
    || schemaVersion === WORKSPACE_SNAPSHOT_SCHEMA_V5
    ? parsePlanStoryboardProjection(value.plan_storyboard_preview, projectID)
    : null;
  const writePromptsPreview = schemaVersion === WORKSPACE_SNAPSHOT_SCHEMA_V4 || schemaVersion === WORKSPACE_SNAPSHOT_SCHEMA_V5
    ? parseWritePromptsProjection(value.write_prompts_preview, projectID, planStoryboardPreview)
    : null;
  const mediaPreviews = schemaVersion === WORKSPACE_SNAPSHOT_SCHEMA_V5
    ? parseMediaPreviewProjection(value.media_previews, { expectedProjectID: projectID })
    : [];
  if (latestTurnOutput && !inputs.some((input) => input.id === latestTurnOutput.inputID)) {
    throw contractError('Workspace Snapshot Turn Output 引用了不存在的 Input');
  }
  if (analyzeMaterialsPreview && !inputs.some((input) => input.id === analyzeMaterialsPreview.inputID)) {
    throw contractError('Workspace Snapshot Analyze Materials Preview 引用了不存在的 Input');
  }
  if (planStoryboardPreview?.kind === 'plan_storyboard_preview'
      && !inputs.some((input) => input.id === planStoryboardPreview.inputID)) {
    throw contractError('Workspace Snapshot Plan Storyboard Preview 引用了不存在的 Input');
  }
  if (writePromptsPreview && !inputs.some((input) => input.id === writePromptsPreview.inputID)) {
    throw contractError('Workspace Snapshot Write Prompts Preview 引用了不存在的 Input');
  }
  if (mediaPreviews.some((card) => !inputs.some((input) => input.id === card.inputID))) {
    throw contractError('Workspace Snapshot Media Preview 引用了不存在的原始请求 Input');
  }
  return {
    schemaVersion: value.schema_version,
    requestID: uuid(value.request_id, 'request_id'),
    session,
    messages,
    inputs,
    creationSpecPreview,
    creationSpecPreviewFailure: null,
    latestTurnOutput,
    analyzeMaterialsPreview,
    planStoryboardPreview,
    writePromptsPreview,
    mediaPreviews,
    eventHighWatermark,
    minAvailableSeq
  };
}

// parsePersistentWorkspaceEvent 校验 SSE id、event、JSON Envelope 和强类型 Payload 的一致性。
export function parsePersistentWorkspaceEvent(messageEvent, { expectedProjectID, expectedSessionID } = {}) {
  const value = exactObject(
    parseMessageData(messageEvent, 'Workspace Event'),
    WORKSPACE_EVENT_FIELDS,
    'Workspace Event'
  );
  exact(value.schema_version, WORKSPACE_EVENT_SCHEMA, 'schema_version');
  exact(value.payload_schema_version, SESSION_EVENT_PAYLOAD_SCHEMA, 'payload_schema_version');
  const eventName = enumValue(value.event, new Set(WORKSPACE_PERSISTENT_EVENTS), 'event');
  exact(messageEvent?.type, eventName, 'SSE event');
  const seq = safeInteger(value.seq, 'seq', { minimum: 1 });
  if (canonicalCursor(messageEvent?.lastEventId, 'SSE id') !== seq) {
    throw contractError('Workspace Event 的 SSE id 与 seq 不一致');
  }
  const sessionID = uuid(value.session_id, 'session_id');
  const projectID = uuid(value.project_id, 'project_id');
  if (sessionID !== uuid(expectedSessionID, 'expected session_id') || projectID !== uuid(expectedProjectID, 'expected project_id')) {
    throw contractError('Workspace Event 的 Project/Session Binding 不一致');
  }
  const event = {
    schemaVersion: value.schema_version,
    payloadSchemaVersion: value.payload_schema_version,
    eventID: uuid(value.event_id, 'event_id'),
    sessionID,
    projectID,
    seq,
    event: eventName,
    occurredAt: timestamp(value.occurred_at, 'occurred_at'),
    aggregateType: nonEmptyString(value.aggregate_type, 'aggregate_type'),
    aggregateID: uuid(value.aggregate_id, 'aggregate_id'),
    aggregateVersion: safeInteger(value.aggregate_version, 'aggregate_version', { minimum: 1 }),
    payload: object(value.payload, 'payload')
  };
  event.payload = validateEventPayload(event);
  return event;
}

// parseStreamReady 校验不推进 Cursor 的 Ready 控制帧。
export function parseStreamReady(messageEvent, expectedSessionID) {
  const value = parseControl(messageEvent, 'stream.ready', expectedSessionID);
  const cursor = safeInteger(value.cursor, 'cursor', { minimum: 0 });
  const minAvailableSeq = safeInteger(value.min_available_seq, 'min_available_seq', { minimum: 1 });
  const latestSeq = safeInteger(value.latest_seq, 'latest_seq', { minimum: 0 });
  if (cursor > latestSeq || minAvailableSeq > latestSeq + 1) {
    throw contractError('stream.ready 的 Event Window 不一致');
  }
  return { event: 'stream.ready', sessionID: value.session_id, cursor, minAvailableSeq, latestSeq };
}

// parseStreamReset 校验 Reset 白名单；Reset 帧不得携带 SSE id。
export function parseStreamReset(messageEvent, expectedSessionID) {
  const value = parseControl(messageEvent, 'stream.reset', expectedSessionID);
  if (String(messageEvent?.lastEventId || '') !== '') {
    throw contractError('stream.reset 不得设置 SSE id');
  }
  if (value.snapshot_required !== true) {
    throw contractError('stream.reset 必须要求 Snapshot');
  }
  const minAvailableSeq = safeInteger(value.min_available_seq, 'min_available_seq', { minimum: 1 });
  const latestSeq = safeInteger(value.latest_seq, 'latest_seq', { minimum: 0 });
  if (minAvailableSeq > latestSeq + 1) {
    throw contractError('stream.reset 的 Event Window 不一致');
  }
  return {
    event: 'stream.reset',
    sessionID: value.session_id,
    reason: enumValue(value.reason, RESET_REASONS, 'reason'),
    snapshotRequired: true,
    minAvailableSeq,
    latestSeq
  };
}

// canonicalCursor 只接受唯一规范的非负十进制 JavaScript safe integer。
export function canonicalCursor(value, field = 'cursor') {
  const raw = typeof value === 'number' ? String(value) : String(value ?? '');
  if (!/^(0|[1-9][0-9]*)$/.test(raw)) {
    throw contractError(`${field} 不是规范非负整数`, 'INVALID_WORKSPACE_CURSOR');
  }
  const parsed = Number(raw);
  if (!Number.isSafeInteger(parsed)) {
    throw contractError(`${field} 超出安全整数范围`, 'INVALID_WORKSPACE_CURSOR');
  }
  return parsed;
}

function parseSession(value) {
  const session = exactObject(value, SESSION_FIELDS, 'session');
  return {
    id: uuid(session.id, 'session.id'),
    projectID: uuid(session.project_id, 'session.project_id'),
    status: enumValue(session.status, SESSION_STATUSES, 'session.status'),
    version: safeInteger(session.version, 'session.version', { minimum: 1 }),
    createdAt: timestamp(session.created_at, 'session.created_at'),
    updatedAt: timestamp(session.updated_at, 'session.updated_at')
  };
}

function parseMessage(value) {
  const message = exactObject(value, MESSAGE_FIELDS, 'message');
  const content = stringValue(message.content, 'message.content');
  if (!validUnicodeScalarString(content) || new TextEncoder().encode(content).byteLength > MAX_CONTENT_BYTES) {
    throw contractError('Workspace Snapshot Message 正文不是有效 UTF-8 或超出上限');
  }
  return {
    id: uuid(message.id, 'message.id'),
    messageSeq: safeInteger(message.message_seq, 'message.message_seq', { minimum: 1 }),
    role: enumValue(message.role, new Set(['user']), 'message.role'),
    content,
    createdAt: timestamp(message.created_at, 'message.created_at')
  };
}

function parseInput(value) {
  const input = exactObject(value, INPUT_FIELDS, 'input');
  const sourceType = enumValue(input.source_type, INPUT_SOURCE_TYPES, 'input.source_type');
  const messageID = nullableUUID(input.message_id, 'input.message_id');
  const messageLessInput = sourceType === 'analyze_materials_preview' || sourceType === 'plan_storyboard_preview'
    || sourceType === 'write_prompts_preview' || sourceType === 'generate_media_preview_request'
    || sourceType === 'assemble_output_preview_request' || sourceType === 'media_job_preview_terminal';
  if (messageLessInput ? Boolean(messageID) : !messageID) {
    throw contractError('Workspace Snapshot Input 的 source_type/message_id 组合不合法');
  }
  return {
    id: uuid(input.id, 'input.id'),
    messageID,
    sourceType,
    status: enumValue(input.status, INPUT_STATUSES, 'input.status'),
    enqueueSeq: safeInteger(input.enqueue_seq, 'input.enqueue_seq', { minimum: 1 }),
    availableAt: timestamp(input.available_at, 'input.available_at'),
    createdAt: timestamp(input.created_at, 'input.created_at'),
    updatedAt: timestamp(input.updated_at, 'input.updated_at')
  };
}

// parsePlanStoryboardProjection 复核 completed Card 的 Project Binding；未知未来版本只保留安全版本标记。
function parsePlanStoryboardProjection(value, expectedProjectID) {
  const preview = parseStoryboardPreviewProjection(value);
  if (preview?.kind === 'plan_storyboard_preview'
      && preview.status === 'completed'
      && preview.projectID !== expectedProjectID) {
    throw contractError('Workspace Snapshot Plan Storyboard Preview 的 Project Binding 不一致');
  }
  return preview;
}

// parseWritePromptsProjection 复核 Project、Source Storyboard 与完整 target set Binding。
function parseWritePromptsProjection(value, expectedProjectID, storyboardPreview) {
  const preview = parsePromptPreviewProjection(value);
  if (preview?.status === 'completed') {
    if (preview.projectID !== expectedProjectID) {
      throw contractError('Workspace Snapshot Write Prompts Preview 的 Project Binding 不一致');
    }
    validatePromptPreviewSourceBinding(preview, storyboardPreview);
  }
  return preview;
}

function parseControl(messageEvent, expectedEvent, expectedSessionID) {
  if (String(messageEvent?.lastEventId || '') !== '') {
    throw contractError(`${expectedEvent} 不得设置 SSE id`);
  }
  const value = parseMessageData(messageEvent, expectedEvent);
  exactObject(
    value,
    expectedEvent === 'stream.ready' ? STREAM_READY_FIELDS : STREAM_RESET_FIELDS,
    expectedEvent
  );
  exact(value.schema_version, WORKSPACE_STREAM_CONTROL_SCHEMA, 'schema_version');
  exact(value.event, expectedEvent, 'event');
  exact(messageEvent?.type, expectedEvent, 'SSE event');
  const sessionID = uuid(value.session_id, 'session_id');
  if (sessionID !== uuid(expectedSessionID, 'expected session_id')) {
    throw contractError(`${expectedEvent} 的 session_id 不一致`);
  }
  return value;
}

function validateEventPayload(event) {
  if (event.event === 'session.created') {
    exactObject(event.payload, SESSION_CREATED_PAYLOAD_FIELDS, 'session.created payload');
    if (event.aggregateType !== 'session' || event.aggregateID !== event.sessionID) {
      throw contractError('session.created Aggregate 不一致');
    }
    exact(uuid(event.payload.session_id, 'payload.session_id'), event.sessionID, 'payload.session_id');
    exact(uuid(event.payload.project_id, 'payload.project_id'), event.projectID, 'payload.project_id');
    enumValue(event.payload.status, SESSION_STATUSES, 'payload.status');
    const version = safeInteger(event.payload.version, 'payload.version', { minimum: 1 });
    if (version !== event.aggregateVersion) {
      throw contractError('session.created Aggregate Version 不一致');
    }
    return event.payload;
  }
  if (event.event === 'creation_spec.preview.completed') {
    const preview = parseCreationSpecPreviewCard(event.payload, { expectedProjectID: event.projectID });
    if (
      event.aggregateType !== 'creation_spec'
      || event.aggregateID !== preview.creationSpecID
      || event.aggregateVersion !== preview.version
    ) {
      throw contractError('creation_spec.preview.completed Aggregate 不一致');
    }
    return preview;
  }
  if (event.event === 'creation_spec.preview.failed') {
    const failure = parseCreationSpecPreviewFailure(event.payload);
    if (event.aggregateType !== 'session_input' || event.aggregateID !== failure.inputID) {
      throw contractError('creation_spec.preview.failed Aggregate 不一致');
    }
    return failure;
  }
  if (event.event === 'analyze_materials.preview.accepted') {
    exactObject(event.payload, ANALYZE_ACCEPTED_PAYLOAD_FIELDS, 'analyze_materials.preview.accepted payload');
    if (event.aggregateType !== 'session_input' || event.aggregateID !== uuid(event.payload.input_id, 'payload.input_id')
      || event.aggregateVersion !== 1) {
      throw contractError('analyze_materials.preview.accepted Aggregate 不一致');
    }
    exact(uuid(event.payload.session_id, 'payload.session_id'), event.sessionID, 'payload.session_id');
    uuid(event.payload.turn_id, 'payload.turn_id');
    uuid(event.payload.run_id, 'payload.run_id');
    uuid(event.payload.request_id, 'payload.request_id');
    uuid(event.payload.tool_call_id, 'payload.tool_call_id');
    exact(event.payload.source_type, 'analyze_materials_preview', 'payload.source_type');
    sha256(event.payload.intent_digest, 'payload.intent_digest');
    sha256(event.payload.context_digest, 'payload.context_digest');
    return event.payload;
  }
  if (event.event === 'plan_storyboard.preview.accepted') {
    exactObject(event.payload, PLAN_STORYBOARD_ACCEPTED_PAYLOAD_FIELDS, 'plan_storyboard.preview.accepted payload');
    const inputID = uuid(event.payload.input_id, 'payload.input_id');
    if (event.aggregateType !== 'plan_storyboard_preview' || event.aggregateID !== inputID
      || event.aggregateVersion !== 1) {
      throw contractError('plan_storyboard.preview.accepted Aggregate 不一致');
    }
    exact(event.payload.schema_version, 'plan_storyboard.preview.accepted.v1', 'payload.schema_version');
    uuid(event.payload.turn_id, 'payload.turn_id');
    uuid(event.payload.run_id, 'payload.run_id');
    uuid(event.payload.tool_call_id, 'payload.tool_call_id');
    uuid(event.payload.business_command_id, 'payload.business_command_id');
    uuid(event.payload.creation_spec_id, 'payload.creation_spec_id');
    exact(safeInteger(event.payload.creation_spec_version, 'payload.creation_spec_version', { minimum: 1 }), 1,
      'payload.creation_spec_version');
    sha256(event.payload.intent_digest, 'payload.intent_digest');
    sha256(event.payload.context_digest, 'payload.context_digest');
    sha256(event.payload.creation_spec_content_digest, 'payload.creation_spec_content_digest');
    return event.payload;
  }
  if (event.event === 'write_prompts.preview.accepted') {
    exactObject(event.payload, WRITE_PROMPTS_ACCEPTED_PAYLOAD_FIELDS, 'write_prompts.preview.accepted payload');
    const inputID = uuid(event.payload.input_id, 'payload.input_id');
    if (event.aggregateType !== 'write_prompts_preview' || event.aggregateID !== inputID
      || event.aggregateVersion !== 1) {
      throw contractError('write_prompts.preview.accepted Aggregate 不一致');
    }
    exact(event.payload.schema_version, 'write_prompts.preview.accepted.v1', 'payload.schema_version');
    uuid(event.payload.turn_id, 'payload.turn_id');
    uuid(event.payload.run_id, 'payload.run_id');
    uuid(event.payload.tool_call_id, 'payload.tool_call_id');
    uuid(event.payload.business_command_id, 'payload.business_command_id');
    uuid(event.payload.storyboard_preview_id, 'payload.storyboard_preview_id');
    exact(safeInteger(event.payload.storyboard_preview_version, 'payload.storyboard_preview_version', { minimum: 1 }), 1,
      'payload.storyboard_preview_version');
    sha256(event.payload.intent_digest, 'payload.intent_digest');
    sha256(event.payload.context_digest, 'payload.context_digest');
    sha256(event.payload.storyboard_preview_content_digest, 'payload.storyboard_preview_content_digest');
    return event.payload;
  }
  if (event.event.startsWith('analyze_materials.preview.')) {
    const output = parseAnalyzeMaterialsPreviewCard(event.payload);
    const expected = {
      'analyze_materials.preview.completed': ['completed', null],
      'analyze_materials.preview.partial': ['partial', null],
      'analyze_materials.preview.failed': ['failed', 'tool'],
      'analyze_materials.preview.runtime_failed': ['failed', 'runtime']
    }[event.event];
    if (!expected || output.status !== expected[0] || output.failureKind !== expected[1]
      || event.aggregateType !== 'session_turn' || event.aggregateID !== output.turnID || event.aggregateVersion !== 1) {
      throw contractError(`${event.event} Aggregate、status 或 failure_kind 不一致`);
    }
    return output;
  }
  if (event.event.startsWith('plan_storyboard.preview.')) {
    const output = parseStoryboardPreviewCard(event.payload);
    const expected = {
      'plan_storyboard.preview.completed': ['completed', null],
      'plan_storyboard.preview.failed': ['failed', 'tool'],
      'plan_storyboard.preview.runtime_failed': ['failed', 'runtime']
    }[event.event];
    if (!expected || output.status !== expected[0] || output.failureKind !== expected[1]
      || event.aggregateType !== 'plan_storyboard_preview' || event.aggregateID !== output.inputID
      || event.aggregateVersion !== 1 || (output.status === 'completed' && output.projectID !== event.projectID)) {
      throw contractError(`${event.event} Aggregate、Project、status 或 failure_kind 不一致`);
    }
    return output;
  }
  if (event.event.startsWith('write_prompts.preview.')) {
    const output = parsePromptPreviewCard(event.payload);
    const expected = {
      'write_prompts.preview.completed': ['completed', null],
      'write_prompts.preview.failed': ['failed', 'tool'],
      'write_prompts.preview.runtime_failed': ['failed', 'runtime']
    }[event.event];
    if (!expected || output.status !== expected[0] || output.failureKind !== expected[1]
      || event.aggregateType !== 'write_prompts_preview' || event.aggregateID !== output.inputID
      || event.aggregateVersion !== 1 || (output.status === 'completed' && output.projectID !== event.projectID)) {
      throw contractError(`${event.event} Aggregate、Project、status 或 failure_kind 不一致`);
    }
    return output;
  }
  if (event.event.startsWith('media.preview.')) {
    const output = parseMediaPreviewCard(event.payload, { expectedProjectID: event.projectID });
    const isEarlyFailure = output.status === 'failed' && output.operationID === '';
    const validVariant = event.event === 'media.preview.accepted'
      ? output.status === 'accepted' && event.aggregateID === output.inputID
      : event.event === 'media.preview.completed'
        ? output.status === 'completed' && event.aggregateID !== output.inputID
        : event.event === 'media.preview.runtime_failed'
          ? isEarlyFailure && event.aggregateID === output.inputID
          : event.event === 'media.preview.failed'
            ? output.status === 'failed'
              && (isEarlyFailure ? event.aggregateID === output.inputID : event.aggregateID !== output.inputID)
            : false;
    if (!validVariant || event.aggregateType !== 'session_input' || event.aggregateVersion !== 1) {
      throw contractError(`${event.event} Aggregate 或 Media Card 变体不一致`);
    }
    return output;
  }
  if (event.event === 'session.turn.completed') {
    const output = parseDirectResponseCard(event.payload);
    if (event.aggregateType !== 'session_turn' || event.aggregateID !== output.turnID) {
      throw contractError('session.turn.completed Aggregate 不一致');
    }
    return output;
  }
  if (event.event === 'session.turn.failed' || event.event === 'session.turn.recovery_pending') {
    const output = parseTurnFailureCard(event.payload);
    const expectedStatus = event.event === 'session.turn.failed' ? 'failed' : 'recovery_pending';
    if (output.status !== expectedStatus || event.aggregateType !== 'session_turn' || event.aggregateID !== output.turnID) {
      throw contractError(`${event.event} Aggregate 或 status 不一致`);
    }
    return output;
  }
  if (event.aggregateType !== 'session_input') {
    throw contractError('session.input.accepted Aggregate Type 不一致');
  }
  exactObject(event.payload, INPUT_ACCEPTED_PAYLOAD_FIELDS, 'session.input.accepted payload');
  exact(uuid(event.payload.session_id, 'payload.session_id'), event.sessionID, 'payload.session_id');
  exact(uuid(event.payload.input_id, 'payload.input_id'), event.aggregateID, 'payload.input_id');
  uuid(event.payload.message_id, 'payload.message_id');
  safeInteger(event.payload.enqueue_seq, 'payload.enqueue_seq', { minimum: 1 });
  exact(event.payload.status, 'pending', 'payload.status');
  return event.payload;
}

function parseMessageData(messageEvent, label) {
  if (typeof messageEvent?.data !== 'string') {
    throw contractError(`${label} 缺少 JSON data`);
  }
  try {
    return object(JSON.parse(messageEvent.data), label);
  } catch (error) {
    if (error instanceof WorkspaceContractError) throw error;
    throw contractError(`${label} 不是有效 JSON`);
  }
}

function object(value, field) {
  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    throw contractError(`${field} 必须为对象`);
  }
  return value;
}

function exactObject(value, expectedFields, field) {
  object(value, field);
  const actual = Object.keys(value).sort();
  const expected = [...expectedFields].sort();
  if (actual.length !== expected.length || actual.some((key, index) => key !== expected[index])) {
    throw contractError(`${field} 字段集合不符合冻结契约`);
  }
  return value;
}

function uuid(value, field) {
  const parsed = stringValue(value, field);
  if (!UUID_V7_PATTERN.test(parsed)) {
    throw contractError(`${field} 必须为规范 UUIDv7`);
  }
  return parsed;
}

function nullableUUID(value, field) {
  return value == null ? '' : uuid(value, field);
}

function timestamp(value, field) {
  const parsed = nonEmptyString(value, field);
  if (!RFC3339_PATTERN.test(parsed) || Number.isNaN(Date.parse(parsed))) {
    throw contractError(`${field} 必须为 RFC3339 时间`);
  }
  return parsed;
}

function safeInteger(value, field, { minimum } = {}) {
  if (!Number.isSafeInteger(value) || (minimum != null && value < minimum)) {
    throw contractError(`${field} 必须为安全整数`);
  }
  return value;
}

function enumValue(value, allowed, field) {
  const parsed = nonEmptyString(value, field);
  if (!allowed.has(parsed)) {
    throw contractError(`${field} 使用了未知状态`);
  }
  return parsed;
}

function nonEmptyString(value, field) {
  const parsed = stringValue(value, field);
  if (!parsed) throw contractError(`${field} 不能为空`);
  return parsed;
}

function stringValue(value, field) {
  if (typeof value !== 'string') throw contractError(`${field} 必须为字符串`);
  return value;
}

function exact(actual, expected, field) {
  if (actual !== expected) throw contractError(`${field} 不符合冻结契约`);
  return actual;
}

function sha256(value, field) {
  if (typeof value !== 'string' || !/^[0-9a-f]{64}$/.test(value)) {
    throw contractError(`${field} 必须为小写 SHA-256`);
  }
  return value;
}

function assertStrictlyIncreasing(items, key, field) {
  for (let index = 1; index < items.length; index += 1) {
    if (items[index][key] <= items[index - 1][key]) {
      throw contractError(`${field} 未按冻结序号严格递增`);
    }
  }
}

function assertUnique(items, key, field) {
  if (new Set(items.map((item) => item[key])).size !== items.length) {
    throw contractError(`${field} 包含重复标识`);
  }
}

function validUnicodeScalarString(value) {
  for (let index = 0; index < value.length; index += 1) {
    const code = value.charCodeAt(index);
    if (code >= 0xd800 && code <= 0xdbff) {
      const low = value.charCodeAt(index + 1);
      if (low < 0xdc00 || low > 0xdfff) return false;
      index += 1;
    } else if (code >= 0xdc00 && code <= 0xdfff) {
      return false;
    }
  }
  return true;
}

function contractError(message, code) {
  return new WorkspaceContractError(message, code);
}
