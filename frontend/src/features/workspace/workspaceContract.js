// W0.5 Workspace 前端只接受冻结的 Snapshot、Event 与控制帧版本。
export const WORKSPACE_SNAPSHOT_SCHEMA = 'session.workspace.v1';
export const WORKSPACE_EVENT_SCHEMA = 'workspace.event.v1';
export const SESSION_EVENT_PAYLOAD_SCHEMA = 'session.event.v1';
export const WORKSPACE_STREAM_CONTROL_SCHEMA = 'workspace.stream-control.v1';

export const WORKSPACE_PERSISTENT_EVENTS = Object.freeze([
  'session.created',
  'session.input.accepted'
]);

export const WORKSPACE_INPUT_STATUSES = Object.freeze([
  'pending',
  'claimed',
  'running',
  'retry_wait',
  'resolved',
  'dead'
]);

const SESSION_STATUSES = new Set(['active', 'archived']);
const INPUT_STATUSES = new Set(WORKSPACE_INPUT_STATUSES);
const PROJECT_LIFECYCLE_STATUSES = new Set(['active', 'archived', 'trash', 'deleted']);
const PROJECT_RUN_STATUSES = new Set([
  'idle', 'queued', 'running', 'waiting_user', 'waiting_async', 'succeeded', 'partial_failed', 'failed', 'cancelled'
]);
const INITIAL_PROMPT_STATUSES = new Set(['absent', 'pending', 'accepted', 'failed']);
const RESET_REASONS = new Set(['cursor_expired', 'event_gap', 'projection_invalid']);
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
  const value = object(payload, 'Workspace Snapshot');
  exact(value.schema_version, WORKSPACE_SNAPSHOT_SCHEMA, 'schema_version');
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
    if (!messageIDs.has(input.messageID)) {
      throw contractError('Workspace Snapshot Input 引用了不存在的 Message');
    }
  });
  const eventHighWatermark = safeInteger(value.event_high_watermark, 'event_high_watermark', { minimum: 0 });
  const minAvailableSeq = safeInteger(value.min_available_seq, 'min_available_seq', { minimum: 1 });
  if (minAvailableSeq > eventHighWatermark + 1) {
    throw contractError('Workspace Snapshot 的 Event Window 不一致');
  }
  return {
    schemaVersion: value.schema_version,
    requestID: uuid(value.request_id, 'request_id'),
    session,
    messages,
    inputs,
    eventHighWatermark,
    minAvailableSeq
  };
}

// parsePersistentWorkspaceEvent 校验 SSE id、event、JSON Envelope 和强类型 Payload 的一致性。
export function parsePersistentWorkspaceEvent(messageEvent, { expectedProjectID, expectedSessionID } = {}) {
  const value = parseMessageData(messageEvent, 'Workspace Event');
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
  validateEventPayload(event);
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
  const session = object(value, 'session');
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
  const message = object(value, 'message');
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
  const input = object(value, 'input');
  return {
    id: uuid(input.id, 'input.id'),
    messageID: uuid(input.message_id, 'input.message_id'),
    sourceType: enumValue(input.source_type, new Set(['user_message']), 'input.source_type'),
    status: enumValue(input.status, INPUT_STATUSES, 'input.status'),
    enqueueSeq: safeInteger(input.enqueue_seq, 'input.enqueue_seq', { minimum: 1 }),
    availableAt: timestamp(input.available_at, 'input.available_at'),
    createdAt: timestamp(input.created_at, 'input.created_at'),
    updatedAt: timestamp(input.updated_at, 'input.updated_at')
  };
}

function parseControl(messageEvent, expectedEvent, expectedSessionID) {
  if (String(messageEvent?.lastEventId || '') !== '') {
    throw contractError(`${expectedEvent} 不得设置 SSE id`);
  }
  const value = parseMessageData(messageEvent, expectedEvent);
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
    return;
  }
  if (event.aggregateType !== 'session_input') {
    throw contractError('session.input.accepted Aggregate Type 不一致');
  }
  exact(uuid(event.payload.session_id, 'payload.session_id'), event.sessionID, 'payload.session_id');
  exact(uuid(event.payload.input_id, 'payload.input_id'), event.aggregateID, 'payload.input_id');
  uuid(event.payload.message_id, 'payload.message_id');
  safeInteger(event.payload.enqueue_seq, 'payload.enqueue_seq', { minimum: 1 });
  exact(event.payload.status, 'pending', 'payload.status');
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
