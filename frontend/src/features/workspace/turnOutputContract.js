export const DIRECT_RESPONSE_CARD_SCHEMA = 'session.turn.direct_response.card.v1';
export const TURN_FAILURE_CARD_SCHEMA = 'session.turn.failure.card.v1';
export const DIRECT_RESPONSE_SUMMARY = '已收到你的创作需求。你可以继续打开工具箱选择下一步流程。';

const DIRECT_RESPONSE_FIELDS = Object.freeze([
  'schema_version', 'turn_id', 'run_id', 'input_id', 'status', 'message_code', 'summary', 'available_actions'
]);
const FAILURE_FIELDS = Object.freeze([
  'schema_version', 'turn_id', 'run_id', 'input_id', 'status', 'error_code', 'retryable', 'summary'
]);
const UUID_V7_PATTERN = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
const ERROR_CODE_PATTERN = /^[A-Z][A-Z0-9_]{0,63}$/;
const MAX_SUMMARY_BYTES = 2000;

export class TurnOutputContractError extends Error {
  constructor(message) {
    super(message);
    this.name = 'TurnOutputContractError';
    this.code = 'INVALID_TURN_OUTPUT';
    this.status = 502;
    this.retryable = false;
  }
}

export function parseTurnOutputProjection(value) {
  if (value === null) return null;
  const card = object(value, 'latest_turn_output');
  if (card.schema_version === DIRECT_RESPONSE_CARD_SCHEMA) return parseDirectResponseCard(card);
  if (card.schema_version === TURN_FAILURE_CARD_SCHEMA) return parseTurnFailureCard(card);
  throw invalid('latest_turn_output schema_version 未知');
}

export function parseDirectResponseCard(value) {
  const card = exactObject(value, DIRECT_RESPONSE_FIELDS, 'Direct Response Card');
  exact(card.schema_version, DIRECT_RESPONSE_CARD_SCHEMA, 'schema_version');
  exact(card.status, 'completed', 'status');
  exact(card.message_code, 'creation_request_received', 'message_code');
  exact(card.summary, DIRECT_RESPONSE_SUMMARY, 'summary');
  if (!Array.isArray(card.available_actions) || card.available_actions.length !== 1 || card.available_actions[0] !== 'open_toolbox') {
    throw invalid('available_actions 只允许 open_toolbox');
  }
  return {
    kind: 'direct_response',
    schemaVersion: card.schema_version,
    turnID: uuid(card.turn_id, 'turn_id'),
    runID: uuid(card.run_id, 'run_id'),
    inputID: uuid(card.input_id, 'input_id'),
    status: card.status,
    messageCode: card.message_code,
    summary: card.summary,
    availableActions: Object.freeze([...card.available_actions])
  };
}

export function parseTurnFailureCard(value) {
  const card = exactObject(value, FAILURE_FIELDS, 'Turn Failure Card');
  exact(card.schema_version, TURN_FAILURE_CARD_SCHEMA, 'schema_version');
  if (card.status !== 'failed' && card.status !== 'recovery_pending') {
    throw invalid('Failure Card status 未知');
  }
  if (typeof card.error_code !== 'string' || !ERROR_CODE_PATTERN.test(card.error_code)) {
    throw invalid('Failure Card error_code 无效');
  }
  if (typeof card.retryable !== 'boolean') throw invalid('Failure Card retryable 必须为布尔值');
  const summary = safeSummary(card.summary);
  return {
    kind: 'failure',
    schemaVersion: card.schema_version,
    turnID: uuid(card.turn_id, 'turn_id'),
    runID: uuid(card.run_id, 'run_id'),
    inputID: uuid(card.input_id, 'input_id'),
    status: card.status,
    errorCode: card.error_code,
    retryable: card.retryable,
    summary
  };
}

function safeSummary(value) {
  if (typeof value !== 'string' || value.length === 0 || !validUnicodeScalarString(value)) {
    throw invalid('Failure Card summary 无效');
  }
  if (new TextEncoder().encode(value).byteLength > MAX_SUMMARY_BYTES) {
    throw invalid('Failure Card summary 超出上限');
  }
  return value;
}

function exactObject(value, expectedFields, field) {
  object(value, field);
  const actual = Object.keys(value).sort();
  const expected = [...expectedFields].sort();
  if (actual.length !== expected.length || actual.some((key, index) => key !== expected[index])) {
    throw invalid(`${field} 字段集合不符合冻结契约`);
  }
  return value;
}

function object(value, field) {
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw invalid(`${field} 必须为对象`);
  return value;
}

function uuid(value, field) {
  if (typeof value !== 'string' || !UUID_V7_PATTERN.test(value)) throw invalid(`${field} 必须为规范 UUIDv7`);
  return value;
}

function exact(actual, expected, field) {
  if (actual !== expected) throw invalid(`${field} 不符合冻结契约`);
}

function validUnicodeScalarString(value) {
  for (let index = 0; index < value.length; index += 1) {
    const code = value.charCodeAt(index);
    if (code >= 0xd800 && code <= 0xdbff) {
      const next = value.charCodeAt(index + 1);
      if (!(next >= 0xdc00 && next <= 0xdfff)) return false;
      index += 1;
    } else if (code >= 0xdc00 && code <= 0xdfff) {
      return false;
    }
  }
  return true;
}

function invalid(message) {
  return new TurnOutputContractError(message);
}
