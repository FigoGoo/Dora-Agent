import { act, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import {
  AUTH_SESSION_EXPIRED_EVENT,
  AUTH_SESSION_STATUS,
  AuthSessionProvider,
  useAuthSession
} from './authSession.js';

describe('AuthSessionProvider', () => {
  it('shares authentication state without persisting credentials', () => {
    const storage = {
      clear: vi.fn(),
      getItem: vi.fn().mockReturnValue(null),
      removeItem: vi.fn(),
      setItem: vi.fn()
    };
    vi.stubGlobal('localStorage', storage);
    render(
      <AuthSessionProvider initialSession={{ status: AUTH_SESSION_STATUS.AUTHENTICATED, user: { id: 'u1', name: 'Dora', access_token: 'never-store' } }}>
        <SessionProbe />
      </AuthSessionProvider>
    );

    expect(screen.getByTestId('status')).toHaveTextContent('authenticated:Dora');
    expect(screen.getByTestId('snapshot')).not.toHaveTextContent('never-store');
    expect(storage.setItem).not.toHaveBeenCalled();
  });

  it('bootstraps an authenticated principal and keeps CSRF only in memory', async () => {
    const client = {
      bootstrap: vi.fn().mockResolvedValue({
        status: 'authenticated',
        principal: {
          id: 'u1',
          display_name: 'Dora',
          email: 'd***@example.com',
          account_status: 'active',
          roles: ['user'],
          capabilities: ['project.read'],
          access_token: 'never-store'
        },
        csrf_token: 'csrf-1',
        session_expires_at: '2026-07-15T08:00:00Z'
      }),
      login: vi.fn(),
      logout: vi.fn()
    };
    render(
      <AuthSessionProvider client={client}>
        <SessionProbe />
      </AuthSessionProvider>
    );

    expect(screen.getByTestId('status')).toHaveTextContent('bootstrapping');
    await waitFor(() => expect(screen.getByTestId('status')).toHaveTextContent('authenticated:Dora'));
    expect(screen.getByTestId('csrf')).toHaveTextContent('csrf-1');
    expect(screen.getByTestId('snapshot')).not.toHaveTextContent('never-store');
  });

  it('distinguishes an absent session from an unavailable auth service and supports retry', async () => {
    const user = userEvent.setup();
    const client = {
      bootstrap: vi.fn()
        .mockRejectedValueOnce(Object.assign(new Error('offline'), { status: 503, code: 'AUTH_UNAVAILABLE' }))
        .mockResolvedValueOnce(authPayload('csrf-2')),
      login: vi.fn(),
      logout: vi.fn()
    };
    render(
      <AuthSessionProvider client={client}>
        <SessionProbe />
      </AuthSessionProvider>
    );

    await waitFor(() => expect(screen.getByTestId('status')).toHaveTextContent('unavailable'));
    expect(screen.getByTestId('error')).toHaveTextContent('AUTH_UNAVAILABLE');

    await user.click(screen.getByRole('button', { name: '重试 Bootstrap' }));
    await waitFor(() => expect(screen.getByTestId('status')).toHaveTextContent('authenticated:Dora'));
    expect(client.bootstrap).toHaveBeenCalledTimes(2);
  });

  it('treats a bootstrap 401 as an anonymous session instead of infrastructure failure', async () => {
    const client = {
      bootstrap: vi.fn().mockRejectedValue(Object.assign(new Error('no session'), { status: 401, code: 'UNAUTHENTICATED' })),
      login: vi.fn(),
      logout: vi.fn()
    };
    render(
      <AuthSessionProvider client={client}>
        <SessionProbe />
      </AuthSessionProvider>
    );

    await waitFor(() => expect(screen.getByTestId('status')).toHaveTextContent('anonymous'));
    expect(screen.getByTestId('error')).toBeEmptyDOMElement();
  });

  it('fails closed when the auth success DTO drifts from the frozen v1 shape', async () => {
    const client = {
      bootstrap: vi.fn().mockResolvedValue({ principal: { principal_id: 'u1', name: 'Dora' } }),
      login: vi.fn(),
      logout: vi.fn()
    };
    render(
      <AuthSessionProvider client={client}>
        <SessionProbe />
      </AuthSessionProvider>
    );

    await waitFor(() => expect(screen.getByTestId('status')).toHaveTextContent('unavailable'));
    expect(screen.getByTestId('error')).toHaveTextContent('INVALID_AUTH_SESSION_RESPONSE');
  });

  it('calls login and logout with an in-memory CSRF token', async () => {
    const user = userEvent.setup();
    const client = {
      bootstrap: vi.fn(),
      login: vi.fn().mockResolvedValue(authPayload('csrf-login')),
      logout: vi.fn().mockResolvedValue(null)
    };
    render(
      <AuthSessionProvider autoBootstrap={false} client={client}>
        <SessionProbe />
      </AuthSessionProvider>
    );

    await user.click(screen.getByRole('button', { name: '真实登录' }));
    await waitFor(() => expect(screen.getByTestId('status')).toHaveTextContent('authenticated:Dora'));
    expect(client.login).toHaveBeenCalledWith({ email: 'dora@example.com', password: 'secret' });

    await user.click(screen.getByRole('button', { name: '真实退出' }));
    await waitFor(() => expect(screen.getByTestId('status')).toHaveTextContent('anonymous'));
    expect(client.logout).toHaveBeenCalledWith({ csrfToken: 'csrf-login' });
  });

  it('keeps the authoritative Principal intact and latches a capability 403 separately', async () => {
    const user = userEvent.setup();
    const client = {
      bootstrap: vi.fn().mockResolvedValue(authPayload('csrf-reviewer', ['project.read', 'skill.review'])),
      login: vi.fn(),
      logout: vi.fn()
    };
    render(
      <AuthSessionProvider
        autoBootstrap={false}
        client={client}
        initialSession={{
          status: AUTH_SESSION_STATUS.AUTHENTICATED,
          user: { id: 'u1', name: 'Dora', capabilities: ['project.read', 'skill.review'] },
          csrfToken: 'csrf-1',
          sessionExpiresAt: '2026-07-15T08:00:00Z'
        }}
      >
        <SessionProbe />
      </AuthSessionProvider>
    );

    await user.click(screen.getByRole('button', { name: '按 403 重解析' }));
    await waitFor(() => expect(screen.getByTestId('denied-capabilities')).toHaveTextContent('skill.review'));
    expect(screen.getByTestId('status')).toHaveTextContent('authenticated:Dora');
    expect(screen.getByTestId('snapshot')).toHaveTextContent('project.read');
    expect(screen.getByTestId('snapshot')).toHaveTextContent('skill.review');
    expect(screen.getByTestId('can-review')).toHaveTextContent('false');
    expect(screen.getByTestId('csrf')).toHaveTextContent('csrf-reviewer');
    expect(client.bootstrap).toHaveBeenCalledTimes(1);
  });

  it('returns to anonymous when the API layer reports session expiry', () => {
    render(
      <AuthSessionProvider
        initialSession={{ status: AUTH_SESSION_STATUS.AUTHENTICATED, user: { id: 'u1', name: 'Dora' } }}
      >
        <SessionProbe />
      </AuthSessionProvider>
    );

    expect(screen.getByTestId('status')).toHaveTextContent('authenticated:Dora');
    act(() => {
      window.dispatchEvent(new CustomEvent(AUTH_SESSION_EXPIRED_EVENT, { detail: { status: 401 } }));
    });

    expect(screen.getByTestId('status')).toHaveTextContent('anonymous');
  });
});

function authPayload(csrfToken, capabilities = ['project.read']) {
  return {
    status: 'authenticated',
    principal: {
      id: 'u1',
      display_name: 'Dora',
      email: 'd***@example.com',
      account_status: 'active',
      roles: ['user'],
      capabilities
    },
    csrf_token: csrfToken,
    session_expires_at: '2026-07-15T08:00:00Z'
  };
}

function SessionProbe() {
  const session = useAuthSession();
  return (
    <div>
      <span data-testid="status">{session.status}{session.user ? `:${session.user.name}` : ''}</span>
      <span data-testid="csrf">{session.csrfToken}</span>
      <span data-testid="error">{session.error?.code}</span>
      <span data-testid="snapshot">{JSON.stringify(session.user)}</span>
      <span data-testid="denied-capabilities">{JSON.stringify(session.deniedCapabilities)}</span>
      <span data-testid="can-review">{String(session.hasCapability('skill.review'))}</span>
      <button type="button" onClick={() => session.login({ email: 'dora@example.com', password: 'secret' })}>
        真实登录
      </button>
      <button type="button" onClick={() => session.logout()}>
        真实退出
      </button>
      <button type="button" onClick={() => session.retryBootstrap()}>
        重试 Bootstrap
      </button>
      <button type="button" onClick={() => session.retryBootstrap({ deniedCapability: 'skill.review' })}>
        按 403 重解析
      </button>
    </div>
  );
}
