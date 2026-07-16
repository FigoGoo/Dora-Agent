export const TOOL_CATALOG_SCHEMA = 'tool_definition_catalog.v1';

const UUID_V7_PATTERN = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
const ENVELOPE_FIELDS = Object.freeze(['schema_version', 'request_id', 'items']);
const ITEM_FIELDS = Object.freeze(['tool_key', 'display_name', 'order', 'availability', 'reason_code']);
const EXPECTED_ITEMS = Object.freeze([
  Object.freeze({ tool_key: 'plan_creation_spec', display_name: '流程规划', order: 1 }),
  Object.freeze({ tool_key: 'analyze_materials', display_name: '素材分析', order: 2 }),
  Object.freeze({ tool_key: 'plan_storyboard', display_name: '故事板设计', order: 3 }),
  Object.freeze({ tool_key: 'generate_media', display_name: '媒体生成', order: 4 }),
  Object.freeze({ tool_key: 'write_prompts', display_name: '提示词写法', order: 5 }),
  Object.freeze({ tool_key: 'assemble_output', display_name: '视频剪辑', order: 6 })
]);

export class ToolCatalogContractError extends Error {
  constructor(message, field = '') {
    super(message);
    this.name = 'ToolCatalogContractError';
    this.field = field;
    this.code = 'INVALID_TOOL_CATALOG_RESPONSE';
    this.status = 502;
    this.retryable = true;
  }
}

// parseToolCatalogResponse 只接受 W1-B2 审核通过的六项静态不可用目录，不做客户端补齐或排序。
export function parseToolCatalogResponse(payload) {
  const envelope = exactObject(payload, ENVELOPE_FIELDS, 'Tool Catalog 响应');
  exact(envelope.schema_version, TOOL_CATALOG_SCHEMA, 'schema_version');
  const requestID = uuidV7(envelope.request_id, 'request_id');
  if (!Array.isArray(envelope.items)) {
    throw contractError('items 必须为数组', 'items');
  }
  if (envelope.items.length !== EXPECTED_ITEMS.length) {
    throw contractError('items 必须恰好包含六项 Tool', 'items');
  }

  const items = envelope.items.map((payloadItem, index) => {
    const field = `items[${index}]`;
    const item = exactObject(payloadItem, ITEM_FIELDS, field);
    const expected = EXPECTED_ITEMS[index];
    exact(item.tool_key, expected.tool_key, `${field}.tool_key`);
    exact(item.display_name, expected.display_name, `${field}.display_name`);
    exact(item.order, expected.order, `${field}.order`);
    exact(item.availability, 'unavailable', `${field}.availability`);
    exact(item.reason_code, 'DESIGN_REVIEW_PENDING', `${field}.reason_code`);
    return {
      toolKey: item.tool_key,
      displayName: item.display_name,
      order: item.order,
      availability: item.availability,
      reasonCode: item.reason_code
    };
  });

  return { schemaVersion: TOOL_CATALOG_SCHEMA, requestID, items };
}

export function canonicalToolCatalogUUIDV7(value, field = 'UUIDv7') {
  return uuidV7(value, field);
}

function exactObject(value, expectedFields, field) {
  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    throw contractError(`${field} 必须为对象`, field);
  }
  const actualFields = Object.keys(value).sort();
  const sortedExpected = [...expectedFields].sort();
  if (
    actualFields.length !== sortedExpected.length
    || actualFields.some((key, index) => key !== sortedExpected[index])
  ) {
    throw contractError(`${field} 字段集合不符合冻结契约`, field);
  }
  return value;
}

function uuidV7(value, field) {
  if (typeof value !== 'string' || !UUID_V7_PATTERN.test(value)) {
    throw contractError(`${field} 必须为规范小写 UUIDv7`, field);
  }
  return value;
}

function exact(actual, expected, field) {
  if (actual !== expected) {
    throw contractError(`${field} 不符合冻结契约`, field);
  }
  return actual;
}

function contractError(message, field) {
  return new ToolCatalogContractError(message, field);
}
