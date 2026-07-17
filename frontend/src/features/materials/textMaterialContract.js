export const TEXT_MATERIAL_MAX_ITEMS = 100;
export const TEXT_MATERIAL_MAX_CHARACTERS = 2000;

const UUID_V7_PATTERN = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
const RFC3339_UTC_PATTERN = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?Z$/;
const MATERIAL_FIELDS = ['asset_id', 'asset_version', 'media_type', 'status', 'content', 'created_at'];
const CREATE_FIELDS = ['material', 'replayed', 'request_id'];
const LIST_FIELDS = ['items', 'request_id'];

// TextMaterialContractError 表示 Business 文本素材响应不符合严格字段与排序契约。
export class TextMaterialContractError extends Error {
  constructor(message) {
    super(message);
    this.name = 'TextMaterialContractError';
    this.code = 'INVALID_TEXT_MATERIAL_RESPONSE';
    this.status = 502;
    this.retryable = false;
  }
}

// normalizeTextMaterialContent 把用户输入规范化为 NFC，并拒绝空白、控制字符和长度越界。
export function normalizeTextMaterialContent(value) {
  if (typeof value !== 'string') throw contractError('文本素材 content 必须为字符串');
  const content = value.normalize('NFC');
  if ([...content].length < 1 || [...content].length > TEXT_MATERIAL_MAX_CHARACTERS || content.trim() === '') {
    throw contractError('文本素材 content 必须为 1..2000 个非空 Unicode 字符');
  }
  for (const character of content) {
    const code = character.codePointAt(0);
    if ((code <= 0x1f || (code >= 0x7f && code <= 0x9f)) && character !== '\n' && character !== '\r' && character !== '\t') {
      throw contractError('文本素材 content 包含不允许的控制字符');
    }
  }
  return content;
}

// parseTextMaterialCreateResponse 严格解析首次创建或同义幂等重放响应。
export function parseTextMaterialCreateResponse(payload) {
  const value = exactObject(payload, CREATE_FIELDS, 'Text Material Create');
  if (typeof value.replayed !== 'boolean') throw contractError('Text Material replayed 必须为布尔值');
  return {
    material: parseTextMaterial(value.material),
    replayed: value.replayed,
    requestID: uuid(value.request_id, 'request_id')
  };
}

// parseTextMaterialListResponse 严格解析最多一百条、created_at DESC/asset_id DESC 的完整正文列表。
export function parseTextMaterialListResponse(payload) {
  const value = exactObject(payload, LIST_FIELDS, 'Text Material List');
  if (!Array.isArray(value.items) || value.items.length > TEXT_MATERIAL_MAX_ITEMS) {
    throw contractError('Text Material items 数量越界');
  }
  const items = value.items.map(parseTextMaterial);
  if (new Set(items.map((item) => item.assetID)).size !== items.length) {
    throw contractError('Text Material items 包含重复 asset_id');
  }
  for (let index = 1; index < items.length; index += 1) {
    const previous = items[index - 1];
    const current = items[index];
    if (previous.createdAtMs < current.createdAtMs ||
        (previous.createdAtMs === current.createdAtMs && previous.assetID <= current.assetID)) {
      throw contractError('Text Material items 排序不符合 created_at DESC/asset_id DESC');
    }
  }
  return { items, requestID: uuid(value.request_id, 'request_id') };
}

// isCanonicalTextMaterialUUIDv7 判断值是否为规范小写 UUIDv7。
export function isCanonicalTextMaterialUUIDv7(value) {
  return typeof value === 'string' && UUID_V7_PATTERN.test(value);
}

function parseTextMaterial(payload) {
  const value = exactObject(payload, MATERIAL_FIELDS, 'Text Material');
  if (value.asset_version !== 1) throw contractError('Text Material asset_version 必须为 1');
  if (value.media_type !== 'text' || value.status !== 'ready') {
    throw contractError('Text Material 必须是 ready text');
  }
  if (typeof value.content !== 'string' || value.content !== value.content.normalize('NFC')) {
    throw contractError('Text Material content 必须已经是 NFC');
  }
  const content = normalizeTextMaterialContent(value.content);
  const createdAt = timestamp(value.created_at, 'created_at');
  return {
    assetID: uuid(value.asset_id, 'asset_id'),
    assetVersion: value.asset_version,
    mediaType: value.media_type,
    status: value.status,
    content,
    createdAt: value.created_at,
    createdAtMs: createdAt
  };
}

function exactObject(value, fields, label) {
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw contractError(`${label} 必须为对象`);
  const actual = Object.keys(value).sort();
  const expected = [...fields].sort();
  if (actual.length !== expected.length || actual.some((field, index) => field !== expected[index])) {
    throw contractError(`${label} 字段集合不符合严格契约`);
  }
  return value;
}

function uuid(value, field) {
  if (!isCanonicalTextMaterialUUIDv7(value)) throw contractError(`${field} 必须为规范小写 UUIDv7`);
  return value;
}

function timestamp(value, field) {
  if (typeof value !== 'string' || !RFC3339_UTC_PATTERN.test(value)) throw contractError(`${field} 必须为 UTC RFC3339`);
  const parsed = Date.parse(value);
  if (!Number.isFinite(parsed)) throw contractError(`${field} 不是有效时间`);
  return parsed;
}

function contractError(message) {
  return new TextMaterialContractError(message);
}
