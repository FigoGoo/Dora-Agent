import { describe, expect, it, vi } from 'vitest';
import {
  PROJECT_QUICK_CREATE_PATH,
  PROJECT_QUICK_CREATE_MAX_SKILL_COUNT,
  PROJECT_QUICK_CREATE_SCHEMA_V2,
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

  it('uses the explicit v2 variant only when the selection is non-empty', async () => {
    const fetchMock = vi.fn().mockResolvedValue(response({
      project_id: 'p1', creation_status: 'provisioning'
    }, 201));
    vi.stubGlobal('fetch', fetchMock);

    await quickCreateProject({
      prompt: '做一支短片',
      enabledSkillIDs: [
        '019f0000-0000-7000-8000-000000000122',
        '019f0000-0000-7000-8000-000000000121'
      ],
      idempotencyKey: 'quick-create-idem-v2',
      csrfToken: 'csrf-1'
    });

    expect(JSON.parse(fetchMock.mock.calls[0][1].body)).toEqual({
      schema_version: PROJECT_QUICK_CREATE_SCHEMA_V2,
      initial_prompt: '做一支短片',
      enabled_skill_ids: [
        '019f0000-0000-7000-8000-000000000121',
        '019f0000-0000-7000-8000-000000000122'
      ]
    });
  });

  it('keeps the frozen v1 body when the selection is empty', async () => {
    const fetchMock = vi.fn().mockResolvedValue(response({
      project_id: 'p1', creation_status: 'provisioning'
    }, 201));
    vi.stubGlobal('fetch', fetchMock);

    await quickCreateProject({
      prompt: '继续使用 V1',
      enabledSkillIDs: [],
      idempotencyKey: 'quick-create-idem-v1',
      csrfToken: 'csrf-1'
    });

    expect(JSON.parse(fetchMock.mock.calls[0][1].body)).toEqual({ initial_prompt: '继续使用 V1' });
  });

  it('rejects an ambiguous duplicate selection before transport', () => {
    const fetchMock = vi.fn();
    vi.stubGlobal('fetch', fetchMock);
    expect(() => quickCreateProject({
      enabledSkillIDs: ['019f0000-0000-7000-8000-000000000121', '019f0000-0000-7000-8000-000000000121'],
      idempotencyKey: 'quick-create-idem-v2',
      csrfToken: 'csrf-1'
    })).toThrow('重复');
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it('rejects a non-canonical Skill ID before transport', () => {
    const fetchMock = vi.fn();
    vi.stubGlobal('fetch', fetchMock);
    expect(() => quickCreateProject({
      enabledSkillIDs: ['019F0000-0000-7000-8000-000000000121'],
      idempotencyKey: 'quick-create-idem-v2',
      csrfToken: 'csrf-1'
    })).toThrow('UUIDv7');
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it('rejects a selection above the frozen 16-item limit before transport', () => {
    const fetchMock = vi.fn();
    vi.stubGlobal('fetch', fetchMock);
    expect(() => quickCreateProject({
      enabledSkillIDs: skillIDs(PROJECT_QUICK_CREATE_MAX_SKILL_COUNT + 1),
      idempotencyKey: 'quick-create-idem-v2',
      csrfToken: 'csrf-1'
    })).toThrow('最多包含 16 个');
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it('rejects a request without an idempotency key before transport', () => {
    expect(() => quickCreateProject({ prompt: '' })).toThrow('Idempotency-Key');
  });

  it('rejects an unsafe request without the in-memory CSRF token', () => {
    expect(() => quickCreateProject({ prompt: '', idempotencyKey: 'idem-1' })).toThrow('CSRF Token');
  });
});

function skillIDs(count) {
  return Array.from({ length: count }, (_, index) => (
    `019f0000-0000-7000-8000-${String(index + 1).padStart(12, '0')}`
  ));
}

function response(body, status = 200) {
  return {
    ok: status >= 200 && status < 300,
    status,
    statusText: '',
    headers: { get: () => null },
    text: vi.fn().mockResolvedValue(body == null ? '' : JSON.stringify(body))
  };
}
