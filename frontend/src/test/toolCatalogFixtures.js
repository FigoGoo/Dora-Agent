import { WORKSPACE_IDS } from './workspaceFixtures.js';

export function toolCatalogFixture(overrides = {}) {
  return {
    schema_version: 'tool_definition_catalog.v1',
    request_id: WORKSPACE_IDS.request,
    items: [
      item('plan_creation_spec', '流程规划', 1),
      item('analyze_materials', '素材分析', 2),
      item('plan_storyboard', '故事板设计', 3),
      item('generate_media', '媒体生成', 4),
      item('write_prompts', '提示词写法', 5),
      item('assemble_output', '视频剪辑', 6)
    ],
    ...overrides
  };
}

function item(toolKey, displayName, order) {
  return {
    tool_key: toolKey,
    display_name: displayName,
    order,
    availability: 'unavailable',
    reason_code: 'DESIGN_REVIEW_PENDING'
  };
}
