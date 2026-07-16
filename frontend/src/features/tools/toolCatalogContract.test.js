import { describe, expect, it } from 'vitest';
import { toolCatalogFixture } from '../../test/toolCatalogFixtures.js';
import { parseToolCatalogResponse, ToolCatalogContractError } from './toolCatalogContract.js';

describe('Tool Catalog contract', () => {
  it('accepts only the frozen six-item exact set and preserves server order', () => {
    const parsed = parseToolCatalogResponse(toolCatalogFixture());

    expect(parsed).toEqual({
      schemaVersion: 'tool_definition_catalog.v1',
      requestID: '019f0000-0000-7000-8000-000000000001',
      items: [
        item('plan_creation_spec', '流程规划', 1),
        item('analyze_materials', '素材分析', 2),
        item('plan_storyboard', '故事板设计', 3),
        item('generate_media', '媒体生成', 4),
        item('write_prompts', '提示词写法', 5),
        item('assemble_output', '视频剪辑', 6)
      ]
    });
  });

  it.each([
    ['missing item', () => toolCatalogFixture({ items: toolCatalogFixture().items.slice(0, 5) })],
    ['duplicate item', () => {
      const payload = toolCatalogFixture();
      payload.items[1] = { ...payload.items[0] };
      return payload;
    }],
    ['reordered items', () => {
      const payload = toolCatalogFixture();
      [payload.items[0], payload.items[1]] = [payload.items[1], payload.items[0]];
      return payload;
    }],
    ['unknown item field', () => {
      const payload = toolCatalogFixture();
      payload.items[0].description = 'forged';
      return payload;
    }],
    ['unknown availability', () => {
      const payload = toolCatalogFixture();
      payload.items[0].availability = 'pending';
      return payload;
    }],
    ['available item', () => {
      const payload = toolCatalogFixture();
      payload.items[0].availability = 'available';
      return payload;
    }],
    ['wrong reason', () => {
      const payload = toolCatalogFixture();
      payload.items[0].reason_code = 'READY';
      return payload;
    }],
    ['wrong name', () => {
      const payload = toolCatalogFixture();
      payload.items[0].display_name = '规划';
      return payload;
    }],
    ['wrong order', () => {
      const payload = toolCatalogFixture();
      payload.items[0].order = 2;
      return payload;
    }]
  ])('rejects %s without normalizing or filling it', (_label, fixture) => {
    expect(() => parseToolCatalogResponse(fixture())).toThrow(ToolCatalogContractError);
  });

  it.each(['run_url', 'action', 'requested_tool_key', 'input_schema', 'graph_version', 'executable_definition'])
    ('rejects forbidden execution field %s', (field) => {
      const payload = toolCatalogFixture();
      payload.items[0][field] = 'forged';
      expect(() => parseToolCatalogResponse(payload)).toThrow('字段集合');
    });

  it('rejects an unknown envelope field', () => {
    const payload = toolCatalogFixture();
    payload.generated_at = '2026-07-14T00:00:00Z';
    expect(() => parseToolCatalogResponse(payload)).toThrow('字段集合');
  });

  it('rejects a missing envelope field', () => {
    const payload = toolCatalogFixture();
    delete payload.items;
    expect(() => parseToolCatalogResponse(payload)).toThrow('字段集合');
  });

  it.each([
    'not-a-uuid',
    '019f0000-0000-4000-8000-000000000001',
    '019F0000-0000-7000-8000-000000000001',
    '019f0000-0000-7000-c000-000000000001'
  ])('rejects non-canonical request UUIDv7 %s', (requestID) => {
    expect(() => parseToolCatalogResponse(toolCatalogFixture({ request_id: requestID }))).toThrow('UUIDv7');
  });
});

function item(toolKey, displayName, order) {
  return {
    toolKey,
    displayName,
    order,
    availability: 'unavailable',
    reasonCode: 'DESIGN_REVIEW_PENDING'
  };
}
