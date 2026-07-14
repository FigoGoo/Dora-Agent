import { describe, expect, it, vi } from 'vitest';
import {
  PROJECT_QUICK_CREATE_PATH,
  bootstrapProjectWorkspace,
  quickCreateProject
} from './projectQuickCreate.js';

describe('Project Quick Create API', () => {
  it('sends a stable idempotency key and in-memory CSRF token', async () => {
    const fetchMock = vi.fn().mockResolvedValue(response({
      project_id: 'p1',
      creation_status: 'provisioning'
    }, 201));
    vi.stubGlobal('fetch', fetchMock);

    await expect(quickCreateProject({
      prompt: '做一支短片',
      idempotencyKey: 'quick-create-idem-1',
      csrfToken: 'csrf-1'
    })).resolves.toMatchObject({ project_id: 'p1' });

    expect(fetchMock).toHaveBeenCalledWith(PROJECT_QUICK_CREATE_PATH, {
      credentials: 'include',
      method: 'POST',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
        'Idempotency-Key': 'quick-create-idem-1',
        'X-CSRF-Token': 'csrf-1'
      },
      body: JSON.stringify({ initial_prompt: '做一支短片' }),
      signal: undefined
    });
  });

  it('loads the authoritative workspace bootstrap after project acceptance', async () => {
    const fetchMock = vi.fn().mockResolvedValue(response({ project_id: 'p/1', session_id: 's1' }));
    vi.stubGlobal('fetch', fetchMock);

    await expect(bootstrapProjectWorkspace('p/1')).resolves.toMatchObject({ session_id: 's1' });
    expect(fetchMock.mock.calls[0][0]).toBe('/api/v1/projects/p%2F1/bootstrap');
    expect(fetchMock.mock.calls[0][1]).toMatchObject({ method: 'GET', credentials: 'include' });
  });

  it('rejects a request without an idempotency key before transport', () => {
    expect(() => quickCreateProject({ prompt: '' })).toThrow('Idempotency-Key');
  });

  it('rejects an unsafe request without the in-memory CSRF token', () => {
    expect(() => quickCreateProject({ prompt: '', idempotencyKey: 'idem-1' })).toThrow('CSRF Token');
  });
});

function response(body, status = 200) {
  return {
    ok: status >= 200 && status < 300,
    status,
    statusText: '',
    headers: { get: () => null },
    text: vi.fn().mockResolvedValue(body == null ? '' : JSON.stringify(body))
  };
}
