import { describe, expect, it, vi } from 'vitest';
import {
  AUTH_SESSION_PATH,
  bootstrapAuthSession,
  loginAuthSession,
  logoutAuthSession
} from './authApi.js';

describe('Auth API', () => {
  it('bootstraps the Cookie session without sending a bearer token', async () => {
    const fetchMock = vi.fn().mockResolvedValue(response(authPayload()));
    vi.stubGlobal('fetch', fetchMock);

    await expect(bootstrapAuthSession()).resolves.toMatchObject({ principal: { id: 'u1' } });
    expect(fetchMock).toHaveBeenCalledWith(AUTH_SESSION_PATH, {
      credentials: 'include',
      method: 'GET',
      signal: undefined,
      headers: { Accept: 'application/json' }
    });
  });

  it('lets the Provider own auth endpoint 401 handling without global expiry broadcast', async () => {
    const listener = vi.fn();
    window.addEventListener('dora:auth-session-expired', listener);
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(response({ code: 'UNAUTHENTICATED' }, 401)));

    await expect(bootstrapAuthSession()).rejects.toMatchObject({ status: 401, code: 'UNAUTHENTICATED' });
    expect(listener).not.toHaveBeenCalled();
    window.removeEventListener('dora:auth-session-expired', listener);
  });

  it('posts credentials only in the login request body', async () => {
    const fetchMock = vi.fn().mockResolvedValue(response(authPayload()));
    vi.stubGlobal('fetch', fetchMock);

    await loginAuthSession({
      email: 'dora@example.com',
      password: 'secret',
      user_id: 'must-not-cross-boundary',
      access_token: 'must-not-cross-boundary'
    });

    expect(fetchMock.mock.calls[0][1]).toMatchObject({
      credentials: 'include',
      method: 'POST',
      body: JSON.stringify({ email: 'dora@example.com', password: 'secret' })
    });
    expect(fetchMock.mock.calls[0][1].headers.Authorization).toBeUndefined();
  });

  it('sends the in-memory CSRF token when revoking the session', async () => {
    const fetchMock = vi.fn().mockResolvedValue(response(null, 204));
    vi.stubGlobal('fetch', fetchMock);

    await expect(logoutAuthSession({ csrfToken: 'csrf-memory-only' })).resolves.toBeNull();
    expect(fetchMock.mock.calls[0][1]).toMatchObject({
      credentials: 'include',
      method: 'DELETE',
      headers: {
        Accept: 'application/json',
        'X-CSRF-Token': 'csrf-memory-only'
      }
    });
  });

  it('rejects logout without the session-bound CSRF token before transport', () => {
    expect(() => logoutAuthSession()).toThrow('CSRF Token');
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

function authPayload() {
  return {
    status: 'authenticated',
    principal: {
      id: 'u1',
      display_name: 'Dora',
      email: 'd***@example.com',
      account_status: 'active',
      roles: ['user'],
      capabilities: ['project.read']
    },
    csrf_token: 'csrf-1',
    session_expires_at: '2026-07-15T08:00:00Z'
  };
}
