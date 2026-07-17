import { describe, expect, it, vi } from 'vitest';
import { WORKSPACE_IDS } from '../../test/workspaceFixtures.js';
import { createTextMaterial, loadTextMaterials, textMaterialPath } from './textMaterialApi.js';

describe('Text Material API', () => {
  it('creates with CSRF/UUIDv7 idempotency and loads the owner-only list', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(response(201, {
        material: material(), replayed: false, request_id: WORKSPACE_IDS.request
      }))
      .mockResolvedValueOnce(response(200, {
        items: [material()], request_id: WORKSPACE_IDS.request
      }));
    vi.stubGlobal('fetch', fetchMock);

    const created = await createTextMaterial({
      projectID: WORKSPACE_IDS.project,
      content: 'e\u0301',
      idempotencyKey: WORKSPACE_IDS.asset,
      csrfToken: 'csrf-material'
    });
    expect(created.material.assetID).toBe(WORKSPACE_IDS.asset);
    expect(fetchMock).toHaveBeenNthCalledWith(1, textMaterialPath(WORKSPACE_IDS.project), expect.objectContaining({
      method: 'POST', credentials: 'include',
      headers: expect.objectContaining({ 'Idempotency-Key': WORKSPACE_IDS.asset, 'X-CSRF-Token': 'csrf-material' }),
      body: JSON.stringify({ content: 'é' })
    }));

    const listed = await loadTextMaterials({ projectID: WORKSPACE_IDS.project });
    expect(listed.items).toHaveLength(1);
    expect(fetchMock).toHaveBeenNthCalledWith(2, textMaterialPath(WORKSPACE_IDS.project), expect.objectContaining({ method: 'GET' }));
  });

  it('rejects malformed IDs, missing CSRF and drifted response fields', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(response(200, {
      material: { ...material(), owner_user_id: WORKSPACE_IDS.user }, replayed: false, request_id: WORKSPACE_IDS.request
    })));
    await expect(createTextMaterial({
      projectID: WORKSPACE_IDS.project, content: '正文', idempotencyKey: WORKSPACE_IDS.asset, csrfToken: 'csrf'
    })).rejects.toThrow('字段集合');
    await expect(loadTextMaterials({ projectID: 'v4' })).rejects.toThrow('project_id');
    await expect(createTextMaterial({
      projectID: WORKSPACE_IDS.project, content: '正文', idempotencyKey: 'v4', csrfToken: 'csrf'
    })).rejects.toThrow('UUIDv7');
    await expect(createTextMaterial({
      projectID: WORKSPACE_IDS.project, content: '正文', idempotencyKey: WORKSPACE_IDS.asset, csrfToken: ''
    })).rejects.toThrow('CSRF');
  });
});

function material() {
  return {
    asset_id: WORKSPACE_IDS.asset, asset_version: 1, media_type: 'text', status: 'ready',
    content: 'é', created_at: '2026-07-17T10:00:00Z'
  };
}

function response(status, body) {
  return {
    ok: status >= 200 && status < 300,
    status,
    statusText: '',
    headers: { get: () => null },
    text: vi.fn().mockResolvedValue(JSON.stringify(body))
  };
}
