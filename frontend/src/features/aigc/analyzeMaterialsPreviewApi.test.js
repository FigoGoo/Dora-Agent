import { describe, expect, it, vi } from 'vitest';
import { WORKSPACE_IDS } from '../../test/workspaceFixtures.js';
import { analyzeMaterialsPreviewPath, enqueueAnalyzeMaterialsPreview } from './analyzeMaterialsPreviewApi.js';

describe('Analyze Materials Preview API', () => {
  it('POSTs only the strict typed Intent with CSRF and UUIDv7 idempotency key', async () => {
    const fetchMock = vi.fn().mockResolvedValue(response(202, {
      schema_version: 'analyze_materials.preview.enqueue.v1', request_id: WORKSPACE_IDS.request,
      session_id: WORKSPACE_IDS.session, input_id: WORKSPACE_IDS.input, turn_id: WORKSPACE_IDS.turn,
      run_id: WORKSPACE_IDS.run, tool_call_id: WORKSPACE_IDS.toolCall, status: 'pending', replayed: false
    }));
    vi.stubGlobal('fetch', fetchMock);
    const intent = {
      schema_version: 'analyze_materials.preview.intent.v1', asset_ids: [WORKSPACE_IDS.asset],
      analysis_goal: '分析素材', focus_dimensions: ['visual'], output_language: 'zh-CN',
      expected_assets: [{ asset_id: WORKSPACE_IDS.asset, asset_version: 1 }]
    };
    const result = await enqueueAnalyzeMaterialsPreview({
      sessionID: WORKSPACE_IDS.session, idempotencyKey: WORKSPACE_IDS.request,
      csrfToken: 'csrf-analyze', intent
    });
    expect(result).toMatchObject({ status: 'pending', toolCallID: WORKSPACE_IDS.toolCall });
    expect(fetchMock).toHaveBeenCalledWith(analyzeMaterialsPreviewPath(WORKSPACE_IDS.session), expect.objectContaining({
      method: 'POST', credentials: 'include',
      headers: expect.objectContaining({ 'Idempotency-Key': WORKSPACE_IDS.request, 'X-CSRF-Token': 'csrf-analyze' }),
      body: JSON.stringify(intent)
    }));
  });

  it('rejects malformed IDs, missing CSRF, non-202 and non-exact Intent', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(response(200, {})));
    const base = { sessionID: WORKSPACE_IDS.session, idempotencyKey: WORKSPACE_IDS.request, csrfToken: 'csrf', intent: validIntent() };
    await expect(enqueueAnalyzeMaterialsPreview(base)).rejects.toMatchObject({ code: 'UNEXPECTED_HTTP_STATUS', status: 200 });
    await expect(enqueueAnalyzeMaterialsPreview({ ...base, sessionID: 'v4' })).rejects.toThrow('session_id');
    await expect(enqueueAnalyzeMaterialsPreview({ ...base, csrfToken: '' })).rejects.toThrow('CSRF');
    await expect(enqueueAnalyzeMaterialsPreview({ ...base, intent: { ...validIntent(), debug: true } })).rejects.toThrow('字段集合');
  });
});

function validIntent() {
  return {
    schema_version: 'analyze_materials.preview.intent.v1', asset_ids: [WORKSPACE_IDS.asset],
    analysis_goal: '分析素材', focus_dimensions: ['visual'], output_language: 'zh-CN',
    expected_assets: [{ asset_id: WORKSPACE_IDS.asset, asset_version: 1 }]
  };
}

function response(status, body) {
  return { ok: status >= 200 && status < 300, status, statusText: '', headers: { get: () => null }, text: vi.fn().mockResolvedValue(JSON.stringify(body)) };
}
