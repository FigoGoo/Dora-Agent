import { describe, expect, it, vi } from 'vitest';
import { toolCatalogFixture } from '../../test/toolCatalogFixtures.js';
import { WORKSPACE_IDS } from '../../test/workspaceFixtures.js';
import { loadToolCatalog, toolCatalogPath } from './toolCatalogApi.js';

describe('Tool Catalog API', () => {
  it('loads the strict catalog from the cookie-authenticated same-origin GET without Query', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(toolCatalogFixture()));
    vi.stubGlobal('fetch', fetchMock);
    const controller = new AbortController();

    const result = await loadToolCatalog(WORKSPACE_IDS.session, { signal: controller.signal });

    expect(result.items).toHaveLength(6);
    expect(fetchMock).toHaveBeenCalledWith(
      `/api/v1/agent/sessions/${WORKSPACE_IDS.session}/tools`,
      expect.objectContaining({
        method: 'GET',
        cache: 'no-store',
        credentials: 'include',
        signal: controller.signal,
        headers: expect.objectContaining({ Accept: 'application/json' })
      })
    );
    expect(fetchMock.mock.calls[0][0]).not.toContain('?');
  });

  it('fails closed on an invalid payload instead of filling from local constants', async () => {
    const payload = toolCatalogFixture();
    payload.items.pop();
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(jsonResponse(payload)));

    await expect(loadToolCatalog(WORKSPACE_IDS.session)).rejects.toMatchObject({
      code: 'INVALID_TOOL_CATALOG_RESPONSE',
      status: 502
    });
  });

  it('rejects a non-UUIDv7 session before issuing a request', async () => {
    const fetchMock = vi.fn();
    vi.stubGlobal('fetch', fetchMock);
    expect(() => toolCatalogPath('../forged?run=1')).toThrow('UUIDv7');
    await expect(loadToolCatalog('../forged?run=1')).rejects.toThrow('UUIDv7');
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it('surfaces a retryable BFF dependency failure', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(jsonResponse({
      error: {
        code: 'DEPENDENCY_UNAVAILABLE',
        message: '工具目录依赖暂时不可用',
        request_id: WORKSPACE_IDS.request,
        retryable: true
      }
    }, 503)));

    await expect(loadToolCatalog(WORKSPACE_IDS.session)).rejects.toMatchObject({
      status: 503,
      code: 'DEPENDENCY_UNAVAILABLE',
      retryable: true,
      requestID: WORKSPACE_IDS.request
    });
  });
});

function jsonResponse(payload, status = 200) {
  return new Response(JSON.stringify(payload), { status, headers: { 'Content-Type': 'application/json' } });
}
