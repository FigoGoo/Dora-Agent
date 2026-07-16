import { describe, expect, it, vi } from 'vitest';
import { WORKSPACE_IDS } from '../../test/workspaceFixtures.js';
import { loadAgentWorkspaceSnapshot, workspaceEventsPath } from './workspaceApi.js';

describe('Workspace API', () => {
  it('loads Snapshot exclusively through the Business same-origin BFF path', async () => {
    const fetchMock = vi.fn().mockResolvedValue(response({ schema_version: 'session.workspace.v1' }));
    vi.stubGlobal('fetch', fetchMock);

    await loadAgentWorkspaceSnapshot(WORKSPACE_IDS.session);

    expect(fetchMock).toHaveBeenCalledWith(
      `/api/v1/agent/sessions/${WORKSPACE_IDS.session}/workspace`,
      expect.objectContaining({ method: 'GET', credentials: 'include' })
    );
    expect(fetchMock.mock.calls[0][0]).not.toContain('18082');
    expect(fetchMock.mock.calls[0][0]).not.toContain('/api/aigc/');
  });

  it('builds the frozen Events path with a canonical cursor', () => {
    expect(workspaceEventsPath(WORKSPACE_IDS.session, 42)).toBe(
      `/api/v1/agent/sessions/${WORKSPACE_IDS.session}/events?after_seq=42`
    );
    expect(() => workspaceEventsPath(WORKSPACE_IDS.session, '042')).toThrow('规范非负整数');
    expect(() => workspaceEventsPath(WORKSPACE_IDS.session, Number.MAX_SAFE_INTEGER + 1)).toThrow('安全整数');
  });
});

function response(body) {
  return {
    ok: true,
    status: 200,
    statusText: '',
    headers: { get: () => null },
    text: vi.fn().mockResolvedValue(JSON.stringify(body))
  };
}
