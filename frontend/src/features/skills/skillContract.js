export const SKILL_DEFINITION_SCHEMA = 'skill_definition.v1';

export const SKILL_CAPABILITY_FIELDS = Object.freeze([
  'plan_creation_spec',
  'analyze_materials',
  'plan_storyboard',
  'generate_media',
  'write_prompts',
  'assemble_output'
]);

export const SKILL_CAPABILITY_LABELS = Object.freeze({
  plan_creation_spec: '流程规划',
  analyze_materials: '素材分析',
  plan_storyboard: '故事板设计',
  generate_media: '媒体生成',
  write_prompts: '提示词写法',
  assemble_output: '视频剪辑'
});

const CONTENT_STATUSES = new Set(['draft', 'published']);
const REVIEW_STATUSES = new Set(['reviewing', 'approved', 'rejected', 'withdrawn']);
const GOVERNANCE_STATUSES = new Set(['active', 'suspended', 'offline']);
const CAPABILITY_APPLICABILITY = new Set(['enabled', 'not_applicable']);
const ALLOWED_ACTIONS = Object.freeze(['edit_draft', 'submit_review']);
const UUID_V7_PATTERN = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
const RFC3339_PATTERN = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$/;
const QUOTED_ETAG_PATTERN = /^"[^"\r\n]+"$/;

export class SkillContractError extends Error {
  constructor(message, field = '', code = 'INVALID_SKILL_RESPONSE') {
    super(message);
    this.name = 'SkillContractError';
    this.field = field;
    this.code = code;
    this.status = 502;
    this.retryable = false;
  }
}

export function createEmptySkillDefinition() {
  const definition = {
    schema_version: SKILL_DEFINITION_SCHEMA,
    name: '',
    summary: '',
    category: '',
    tags: [],
    input_description: '',
    output_description: '',
    invocation_rules: '',
    examples: [],
    starter_prompts: [],
    market_listing: {
      cover_asset_id: null,
      detail: '',
      copyright_notice: '',
      user_notice: ''
    },
    public_tool_refs: []
  };
  SKILL_CAPABILITY_FIELDS.forEach((field) => {
    definition[field] = { applicability: 'enabled', guidance: '', not_applicable_reason: '' };
  });
  return definition;
}

// parseSkillDefinition 严格消费和规范化 SkillDefinitionV1；当前基线拒绝任何公共 Tool 引用。
export function parseSkillDefinition(payload) {
  const value = object(payload, 'definition');
  exact(value.schema_version, SKILL_DEFINITION_SCHEMA, 'definition.schema_version');
  const definition = {
    schema_version: SKILL_DEFINITION_SCHEMA,
    name: requiredText(value.name, 'definition.name'),
    summary: text(value.summary, 'definition.summary'),
    category: text(value.category, 'definition.category'),
    tags: uniqueTextList(value.tags, 'definition.tags'),
    input_description: text(value.input_description, 'definition.input_description'),
    output_description: text(value.output_description, 'definition.output_description'),
    invocation_rules: text(value.invocation_rules, 'definition.invocation_rules'),
    examples: examples(value.examples),
    starter_prompts: uniqueTextList(value.starter_prompts, 'definition.starter_prompts'),
    market_listing: marketListing(value.market_listing),
    public_tool_refs: publicToolRefs(value.public_tool_refs)
  };
  SKILL_CAPABILITY_FIELDS.forEach((field) => {
    definition[field] = capabilityGuidance(value[field], `definition.${field}`);
  });
  return definition;
}

export function parseOwnerSkillResponse(payload) {
  const value = object(payload, 'Owner Skill 响应');
  return {
    skill: parseOwnerSkill(value.skill),
    requestID: uuid(value.request_id, 'request_id')
  };
}

export function parseOwnerSkillListResponse(payload) {
  const value = object(payload, 'Owner Skill 列表响应');
  if (!Array.isArray(value.items)) throw contractError('items 必须为数组', 'items');
  const items = value.items.map(parseOwnerSkill);
  if (new Set(items.map((item) => item.skillID)).size !== items.length) {
    throw contractError('items 包含重复 skill_id', 'items');
  }
  return {
    items,
    nextCursor: nullableCursor(value.next_cursor, 'next_cursor'),
    requestID: uuid(value.request_id, 'request_id')
  };
}

export function parseReviewSubmissionResponse(payload) {
  const value = object(payload, 'Skill Review 响应');
  return {
    skill: parseOwnerSkill(value.skill),
    reviewID: uuid(value.review_id, 'review_id'),
    requestID: uuid(value.request_id, 'request_id')
  };
}

export function parseOwnerSkill(payload) {
  const value = object(payload, 'skill');
  const reviewStatus = nullableEnum(value.review_status, REVIEW_STATUSES, 'skill.review_status');
  const actions = allowedActions(value.allowed_actions);
  const contentStatus = enumValue(value.content_status, CONTENT_STATUSES, 'skill.content_status');
  const hasUnpublishedChanges = booleanValue(value.has_unpublished_changes, 'skill.has_unpublished_changes');
  const reviewReasonCode = nullableText(value.review_reason_code, 'skill.review_reason_code');
  const reviewUpdatedAt = nullableTimestamp(value.review_updated_at, 'skill.review_updated_at');
  ownerStateInvariants({
    contentStatus,
    hasUnpublishedChanges,
    reviewStatus,
    reviewReasonCode,
    reviewUpdatedAt,
    actions
  });
  return {
    skillID: uuid(value.skill_id, 'skill.skill_id'),
    definition: parseSkillDefinition(value.definition),
    contentStatus,
    hasUnpublishedChanges,
    reviewStatus,
    reviewReasonCode,
    reviewUpdatedAt,
    governanceStatus: enumValue(value.governance_status, GOVERNANCE_STATUSES, 'skill.governance_status'),
    allowedActions: actions,
    draftETag: quotedETag(value.draft_etag, 'skill.draft_etag')
  };
}

function capabilityGuidance(payload, field) {
  const value = object(payload, field);
  const applicability = enumValue(value.applicability, CAPABILITY_APPLICABILITY, `${field}.applicability`);
  const guidance = text(value.guidance, `${field}.guidance`);
  const reason = text(value.not_applicable_reason, `${field}.not_applicable_reason`);
  if (applicability === 'enabled' && (!guidance || reason)) {
    throw contractError(`${field} enabled 必须仅填写 guidance`, field);
  }
  if (applicability === 'not_applicable' && (guidance || !reason)) {
    throw contractError(`${field} not_applicable 必须仅填写 reason`, field);
  }
  return { applicability, guidance, not_applicable_reason: reason };
}

function examples(value) {
  if (!Array.isArray(value)) throw contractError('definition.examples 必须为数组', 'definition.examples');
  const parsed = value.map((item, index) => {
    const example = object(item, `definition.examples[${index}]`);
    return {
      input: requiredText(example.input, `definition.examples[${index}].input`),
      output: requiredText(example.output, `definition.examples[${index}].output`)
    };
  });
  for (let index = 1; index < parsed.length; index += 1) {
    if (compareExamples(parsed[index - 1], parsed[index]) >= 0) {
      throw contractError('definition.examples 必须去重并按 UTF-8 字节序排列', 'definition.examples');
    }
  }
  return parsed;
}

function marketListing(payload) {
  const value = object(payload, 'definition.market_listing');
  if (value.cover_asset_id !== null) {
    throw contractError('definition.market_listing.cover_asset_id 当前必须为 null', 'definition.market_listing.cover_asset_id');
  }
  return {
    cover_asset_id: null,
    detail: text(value.detail, 'definition.market_listing.detail'),
    copyright_notice: text(value.copyright_notice, 'definition.market_listing.copyright_notice'),
    user_notice: text(value.user_notice, 'definition.market_listing.user_notice')
  };
}

function publicToolRefs(value) {
  if (!Array.isArray(value)) throw contractError('definition.public_tool_refs 必须为数组', 'definition.public_tool_refs');
  if (value.length !== 0) {
    throw contractError('当前 W1 基线不允许客户端提交公共 Tool 引用', 'definition.public_tool_refs', 'SKILL_TOOL_REFERENCE_FORBIDDEN');
  }
  return [];
}

function allowedActions(value) {
  if (!Array.isArray(value)) throw contractError('skill.allowed_actions 必须为数组', 'skill.allowed_actions');
  const actions = value.map((action, index) => requiredText(action, `skill.allowed_actions[${index}]`));
  if (new Set(actions).size !== actions.length || actions.some((action) => !ALLOWED_ACTIONS.includes(action))) {
    throw contractError('skill.allowed_actions 包含未知或重复动作', 'skill.allowed_actions');
  }
  const indexes = actions.map((action) => ALLOWED_ACTIONS.indexOf(action));
  if (indexes.some((index, offset) => offset > 0 && index <= indexes[offset - 1])) {
    throw contractError('skill.allowed_actions 顺序不符合冻结契约', 'skill.allowed_actions');
  }
  return actions;
}

function uniqueTextList(value, field) {
  if (!Array.isArray(value)) throw contractError(`${field} 必须为数组`, field);
  const items = value.map((item, index) => requiredText(item, `${field}[${index}]`));
  for (let index = 1; index < items.length; index += 1) {
    if (compareUTF8(items[index - 1], items[index]) >= 0) {
      throw contractError(`${field} 必须去重并按 UTF-8 字节序排列`, field);
    }
  }
  return items;
}

function ownerStateInvariants({
  contentStatus,
  hasUnpublishedChanges,
  reviewStatus,
  reviewReasonCode,
  reviewUpdatedAt,
  actions
}) {
  if (contentStatus === 'draft' && !hasUnpublishedChanges) {
    throw contractError('draft 内容必须标记为有未发布修改', 'skill.has_unpublished_changes');
  }
  if (reviewStatus == null && (reviewReasonCode != null || reviewUpdatedAt != null)) {
    throw contractError('未提交审核时不得携带审核原因或更新时间', 'skill.review_status');
  }
  if (reviewStatus != null && reviewUpdatedAt == null) {
    throw contractError('存在审核状态时必须携带审核更新时间', 'skill.review_updated_at');
  }
  if (reviewStatus === 'reviewing' && !hasUnpublishedChanges) {
    throw contractError('审核中的内容必须仍有未发布修改', 'skill.has_unpublished_changes');
  }
  if (actions[0] !== 'edit_draft') {
    throw contractError('Owner 投影必须允许 edit_draft', 'skill.allowed_actions');
  }
  const canSubmitReview = actions.includes('submit_review');
  const shouldAllowSubmitReview = hasUnpublishedChanges && reviewStatus !== 'reviewing';
  if (canSubmitReview !== shouldAllowSubmitReview) {
    throw contractError('submit_review 与草稿及审核状态不一致', 'skill.allowed_actions');
  }
}

export function compareUTF8(left, right) {
  const leftBytes = new TextEncoder().encode(left);
  const rightBytes = new TextEncoder().encode(right);
  const length = Math.min(leftBytes.length, rightBytes.length);
  for (let index = 0; index < length; index += 1) {
    if (leftBytes[index] !== rightBytes[index]) return leftBytes[index] - rightBytes[index];
  }
  return leftBytes.length - rightBytes.length;
}

export function compareExamples(left, right) {
  const inputOrder = compareUTF8(left.input, right.input);
  return inputOrder || compareUTF8(left.output, right.output);
}

function object(value, field) {
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw contractError(`${field} 必须为对象`, field);
  return value;
}

function text(value, field) {
  if (typeof value !== 'string') throw contractError(`${field} 必须为字符串`, field);
  if (value !== value.normalize('NFC') || value !== value.trim()) throw contractError(`${field} 必须为规范化文本`, field);
  return value;
}

function requiredText(value, field) {
  const parsed = text(value, field);
  if (!parsed) throw contractError(`${field} 不能为空`, field);
  return parsed;
}

function nullableText(value, field) {
  if (value === null) return null;
  return text(value, field);
}

function nullableCursor(value, field) {
  if (value === null) return null;
  return requiredText(value, field);
}

function booleanValue(value, field) {
  if (typeof value !== 'boolean') throw contractError(`${field} 必须为布尔值`, field);
  return value;
}

function enumValue(value, allowed, field) {
  const parsed = requiredText(value, field);
  if (!allowed.has(parsed)) throw contractError(`${field} 使用未知状态`, field);
  return parsed;
}

function nullableEnum(value, allowed, field) {
  return value === null ? null : enumValue(value, allowed, field);
}

function uuid(value, field) {
  const parsed = requiredText(value, field);
  if (!UUID_V7_PATTERN.test(parsed)) throw contractError(`${field} 必须为规范 UUIDv7`, field);
  return parsed;
}

function nullableTimestamp(value, field) {
  if (value === null) return null;
  const parsed = requiredText(value, field);
  if (!RFC3339_PATTERN.test(parsed) || Number.isNaN(Date.parse(parsed))) {
    throw contractError(`${field} 必须为 RFC3339 时间`, field);
  }
  return parsed;
}

function quotedETag(value, field) {
  const parsed = requiredText(value, field);
  if (!QUOTED_ETAG_PATTERN.test(parsed)) throw contractError(`${field} 必须为 quoted opaque ETag`, field);
  return parsed;
}

function exact(actual, expected, field) {
  if (actual !== expected) throw contractError(`${field} 不符合冻结契约`, field);
}

function contractError(message, field, code) {
  return new SkillContractError(message, field, code);
}
