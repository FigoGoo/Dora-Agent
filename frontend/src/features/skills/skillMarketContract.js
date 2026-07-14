export const SKILL_MARKET_CAPABILITY_KEYS = Object.freeze([
  'plan_creation_spec',
  'analyze_materials',
  'plan_storyboard',
  'generate_media',
  'write_prompts',
  'assemble_output'
]);

export const SKILL_MARKET_CAPABILITY_LABELS = Object.freeze({
  plan_creation_spec: '流程规划',
  analyze_materials: '素材分析',
  plan_storyboard: '故事板设计',
  generate_media: '媒体生成',
  write_prompts: '提示词写法',
  assemble_output: '视频剪辑'
});

const UUID_V7_PATTERN = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
const RFC3339_NANO_PATTERN = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.\d{1,9})?(?:Z|[+-](?:[01]\d|2[0-3]):[0-5]\d)$/;
const RAW_BASE64URL_PATTERN = /^[A-Za-z0-9_-]+$/;

const LIST_ENVELOPE_FIELDS = Object.freeze(['items', 'next_cursor', 'request_id']);
const DETAIL_ENVELOPE_FIELDS = Object.freeze(['request_id', 'skill']);
const LIST_ITEM_FIELDS = Object.freeze([
  'category',
  'cover_asset',
  'declared_capability_keys',
  'name',
  'published_at',
  'publisher',
  'skill_id',
  'summary',
  'tags'
]);
const DETAIL_ITEM_FIELDS = Object.freeze([
  ...LIST_ITEM_FIELDS,
  'copyright_notice',
  'examples',
  'input_description',
  'market_detail',
  'output_description',
  'starter_prompts',
  'user_notice'
].sort());
const PUBLISHER_FIELDS = Object.freeze(['display_name', 'publisher_id']);
const EXAMPLE_FIELDS = Object.freeze(['input', 'output']);

export class SkillMarketContractError extends Error {
  constructor(message, field = '') {
    super(message);
    this.name = 'SkillMarketContractError';
    this.field = field;
    this.code = 'INVALID_SKILL_MARKET_RESPONSE';
    this.status = 502;
    this.retryable = false;
  }
}

export function isCanonicalSkillMarketUUIDv7(value) {
  return UUID_V7_PATTERN.test(String(value || ''));
}

export function parseSkillMarketListResponse(payload) {
  const value = exactObject(payload, LIST_ENVELOPE_FIELDS, 'Skill 市场列表响应');
  if (!Array.isArray(value.items)) {
    throw contractError('items 必须为数组', 'items');
  }
  const items = value.items.map((item, index) => parseMarketSkill(item, LIST_ITEM_FIELDS, `items[${index}]`));
  assertUniqueSkillIDs(items, 'items');
  return {
    items,
    nextCursor: nullableCursor(value.next_cursor, 'next_cursor'),
    requestID: uuid(value.request_id, 'request_id')
  };
}

export function parseSkillMarketDetailResponse(payload) {
  const value = exactObject(payload, DETAIL_ENVELOPE_FIELDS, 'Skill 市场详情响应');
  return {
    skill: parseMarketSkill(value.skill, DETAIL_ITEM_FIELDS, 'skill', true),
    requestID: uuid(value.request_id, 'request_id')
  };
}

function parseMarketSkill(payload, fields, field, detail = false) {
  const value = exactObject(payload, fields, field);
  const skillID = uuid(value.skill_id, `${field}.skill_id`);
  const publisher = parsePublisher(value.publisher, `${field}.publisher`);
  if (value.cover_asset !== null) {
    throw contractError(`${field}.cover_asset 当前必须为 null`, `${field}.cover_asset`);
  }

  const skill = {
    skillID,
    name: requiredText(value.name, `${field}.name`),
    summary: text(value.summary, `${field}.summary`),
    category: text(value.category, `${field}.category`),
    tags: uniqueSortedTextList(value.tags, `${field}.tags`),
    publisher,
    publishedAt: timestamp(value.published_at, `${field}.published_at`),
    coverAsset: null,
    declaredCapabilityKeys: capabilityKeys(value.declared_capability_keys, `${field}.declared_capability_keys`)
  };

  if (!detail) return skill;
  return {
    ...skill,
    inputDescription: text(value.input_description, `${field}.input_description`),
    outputDescription: text(value.output_description, `${field}.output_description`),
    examples: examples(value.examples, `${field}.examples`),
    starterPrompts: uniqueSortedTextList(value.starter_prompts, `${field}.starter_prompts`),
    marketDetail: text(value.market_detail, `${field}.market_detail`),
    copyrightNotice: text(value.copyright_notice, `${field}.copyright_notice`),
    userNotice: text(value.user_notice, `${field}.user_notice`)
  };
}

function parsePublisher(payload, field) {
  const value = exactObject(payload, PUBLISHER_FIELDS, field);
  return {
    publisherID: uuid(value.publisher_id, `${field}.publisher_id`),
    displayName: publisherDisplayName(value.display_name, `${field}.display_name`)
  };
}

function examples(value, field) {
  if (!Array.isArray(value)) throw contractError(`${field} 必须为数组`, field);
  const parsed = value.map((item, index) => {
    const exampleField = `${field}[${index}]`;
    const example = exactObject(item, EXAMPLE_FIELDS, exampleField);
    return {
      input: requiredText(example.input, `${exampleField}.input`),
      output: requiredText(example.output, `${exampleField}.output`)
    };
  });
  for (let index = 1; index < parsed.length; index += 1) {
    if (compareExamples(parsed[index - 1], parsed[index]) >= 0) {
      throw contractError(`${field} 必须去重并按 UTF-8 字节序排列`, field);
    }
  }
  return parsed;
}

function capabilityKeys(value, field) {
  if (!Array.isArray(value)) throw contractError(`${field} 必须为数组`, field);
  const indexes = value.map((item, index) => {
    const key = requiredText(item, `${field}[${index}]`);
    const position = SKILL_MARKET_CAPABILITY_KEYS.indexOf(key);
    if (position === -1) throw contractError(`${field} 包含未知能力`, `${field}[${index}]`);
    return position;
  });
  if (indexes.some((position, index) => index > 0 && position <= indexes[index - 1])) {
    throw contractError(`${field} 包含重复或乱序能力`, field);
  }
  return [...value];
}

function uniqueSortedTextList(value, field) {
  if (!Array.isArray(value)) throw contractError(`${field} 必须为数组`, field);
  const items = value.map((item, index) => requiredText(item, `${field}[${index}]`));
  for (let index = 1; index < items.length; index += 1) {
    if (compareUTF8(items[index - 1], items[index]) >= 0) {
      throw contractError(`${field} 必须去重并按 UTF-8 字节序排列`, field);
    }
  }
  return items;
}

function assertUniqueSkillIDs(items, field) {
  if (new Set(items.map((item) => item.skillID)).size !== items.length) {
    throw contractError(`${field} 包含重复 skill_id`, field);
  }
}

function exactObject(value, expectedFields, field) {
  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    throw contractError(`${field} 必须为对象`, field);
  }
  const actualFields = Object.keys(value).sort();
  const expected = [...expectedFields].sort();
  if (actualFields.length !== expected.length || actualFields.some((key, index) => key !== expected[index])) {
    throw contractError(`${field} 字段不符合公开冻结契约`, field);
  }
  return value;
}

function text(value, field) {
  if (typeof value !== 'string') throw contractError(`${field} 必须为字符串`, field);
  if (value !== value.trim() || value !== value.normalize('NFC')) {
    throw contractError(`${field} 必须为规范化文本`, field);
  }
  return value;
}

function requiredText(value, field) {
  const parsed = text(value, field);
  if (!parsed) throw contractError(`${field} 不能为空`, field);
  return parsed;
}

function publisherDisplayName(value, field) {
  if (typeof value !== 'string') throw contractError(`${field} 必须为字符串`, field);
  if (!value.trim() || [...value].length > 160 || /\p{Cc}/u.test(value)) {
    throw contractError(`${field} 必须为 TrimSpace 后非空、最多 160 字且不含控制字符的展示名`, field);
  }
  return value;
}

function uuid(value, field) {
  const parsed = requiredText(value, field);
  if (!isCanonicalSkillMarketUUIDv7(parsed)) {
    throw contractError(`${field} 必须为规范小写 UUIDv7`, field);
  }
  return parsed;
}

function timestamp(value, field) {
  const parsed = requiredText(value, field);
  const match = parsed.match(RFC3339_NANO_PATTERN);
  if (!match || !validCalendarDate(match) || Number.isNaN(Date.parse(parsed))) {
    throw contractError(`${field} 必须为 RFC3339Nano 时间`, field);
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

function nullableCursor(value, field) {
  if (value === null) return null;
  const parsed = requiredText(value, field);
  if (new TextEncoder().encode(parsed).length > 1024 || !RAW_BASE64URL_PATTERN.test(parsed)) {
    throw contractError(`${field} 必须为不超过 1024 字节的无填充 Base64URL`, field);
  }
  return parsed;
}

function compareUTF8(left, right) {
  const leftBytes = new TextEncoder().encode(left);
  const rightBytes = new TextEncoder().encode(right);
  const length = Math.min(leftBytes.length, rightBytes.length);
  for (let index = 0; index < length; index += 1) {
    if (leftBytes[index] !== rightBytes[index]) return leftBytes[index] - rightBytes[index];
  }
  return leftBytes.length - rightBytes.length;
}

function compareExamples(left, right) {
  return compareUTF8(left.input, right.input) || compareUTF8(left.output, right.output);
}

function contractError(message, field) {
  return new SkillMarketContractError(message, field);
}
