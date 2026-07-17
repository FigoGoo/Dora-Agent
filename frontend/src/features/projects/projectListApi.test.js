import { describe, expect, it, vi } from 'vitest';
import { listProjects } from './projectListApi.js';
import { parseProjectListResponse, ProjectListContractError } from './projectListContract.js';

const PROJECT_ID = '019f0000-0000-7000-8000-000000000201';
const REQUEST_ID = '019f0000-0000-7000-8000-000000000202';

describe('Project list API', () => {
  it('sends bounded limit/after and parses the typed response', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(projectListResponse({ next_after: 'next_1' })));
    vi.stubGlobal('fetch', fetchMock);

    const result = await listProjects({ limit: 2, after: 'after_1' });

    expect(fetchMock).toHaveBeenCalledWith('/api/v1/projects?limit=2&after=after_1', expect.objectContaining({
      method: 'GET', credentials: 'include'
    }));
    expect(result.items[0]).toMatchObject({ projectID: PROJECT_ID, title: '真实项目' });
    expect(result.nextAfter).toBe('next_1');
  });

  it('rejects invalid query input before fetch', async () => {
    const fetchMock = vi.fn();
    vi.stubGlobal('fetch', fetchMock);
    await expect(listProjects({ limit: 101 })).rejects.toThrow('limit');
    await expect(listProjects({ after: 'bad=' })).rejects.toThrow('after');
    expect(fetchMock).not.toHaveBeenCalled();
  });
});

describe('Project list contract', () => {
  it('rejects additional fields and a workspace route for another project', () => {
    const extra = projectListResponse();
    extra.debug = true;
    expect(() => parseProjectListResponse(extra)).toThrow(ProjectListContractError);

    const wrongWorkspace = projectListResponse();
    wrongWorkspace.items[0].workspace_ref = '/projects/019f0000-0000-7000-8000-000000000299/workspace';
    expect(() => parseProjectListResponse(wrongWorkspace)).toThrow('workspace_ref');
  });
});

function projectListResponse(overrides = {}) {
  return {
    items: [{
      project_id: PROJECT_ID,
      title: '真实项目',
      lifecycle_status: 'active',
      recent_run_status: 'running',
      initial_prompt_status: 'accepted',
      updated_at: '2026-07-17T09:08:07.123Z',
      workspace_ref: `/projects/${PROJECT_ID}/workspace`
    }],
    next_after: null,
    request_id: REQUEST_ID,
    ...overrides
  };
}

function jsonResponse(payload, status = 200) {
  return new Response(JSON.stringify(payload), { status, headers: { 'Content-Type': 'application/json' } });
}
