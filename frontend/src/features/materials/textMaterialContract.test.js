import { describe, expect, it } from 'vitest';
import { WORKSPACE_IDS } from '../../test/workspaceFixtures.js';
import {
  normalizeTextMaterialContent,
  parseTextMaterialCreateResponse,
  parseTextMaterialListResponse
} from './textMaterialContract.js';

const SECOND_ASSET_ID = '019f0000-0000-7000-8000-000000000032';

describe('Text Material contract', () => {
  it('normalizes user text to NFC and parses strict create/list responses', () => {
    expect(normalizeTextMaterialContent('e\u0301')).toBe('é');
    const created = parseTextMaterialCreateResponse({
      material: material(), replayed: false, request_id: WORKSPACE_IDS.request
    });
    expect(created).toMatchObject({ replayed: false, material: { assetID: WORKSPACE_IDS.asset, content: '完整正文' } });

    const list = parseTextMaterialListResponse({
      items: [material(), material({ asset_id: SECOND_ASSET_ID, created_at: '2026-07-17T09:00:00Z' })],
      request_id: WORKSPACE_IDS.request
    });
    expect(list.items).toHaveLength(2);
  });

  it('rejects unknown fields, non-NFC response content, duplicates and unstable order', () => {
    expect(() => parseTextMaterialCreateResponse({
      material: material(), replayed: false, request_id: WORKSPACE_IDS.request, debug: true
    })).toThrow('字段集合');
    expect(() => parseTextMaterialCreateResponse({
      material: material({ content: 'e\u0301' }), replayed: false, request_id: WORKSPACE_IDS.request
    })).toThrow('NFC');
    expect(() => parseTextMaterialListResponse({
      items: [material(), material()], request_id: WORKSPACE_IDS.request
    })).toThrow('重复');
    expect(() => parseTextMaterialListResponse({
      items: [material({ created_at: '2026-07-17T09:00:00Z' }), material({ asset_id: SECOND_ASSET_ID })],
      request_id: WORKSPACE_IDS.request
    })).toThrow('排序');
  });
});

function material(overrides = {}) {
  return {
    asset_id: WORKSPACE_IDS.asset,
    asset_version: 1,
    media_type: 'text',
    status: 'ready',
    content: '完整正文',
    created_at: '2026-07-17T10:00:00Z',
    ...overrides
  };
}
