import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { projectBootstrapFixture, WORKSPACE_IDS, workspaceSnapshotFixture } from '../test/workspaceFixtures.js';
import { App } from './App.jsx';

afterEach(() => {
  window.history.pushState({}, '', '/');
});

describe('W0 authenticated create transport flow', () => {
  it('keeps a failed real login anonymous and shows the stable credential error', async () => {
    const fetchMock = vi.fn(async (_input, options = {}) => {
      if ((options.method || 'GET') === 'POST') {
        return errorResponse(401, 'AUTH_INVALID_CREDENTIALS', '凭据无效');
      }
      return errorResponse(401, 'UNAUTHENTICATED', '未登录');
    });
    vi.stubGlobal('fetch', fetchMock);
    const user = userEvent.setup();
    render(<App />);

    await user.click(await screen.findByRole('button', { name: '登录' }));
    const dialog = screen.getByRole('dialog', { name: '登录后继续创作' });
    await fillAndSubmitLogin(user, dialog);

    expect(await within(dialog).findByRole('alert')).toHaveTextContent('邮箱或密码错误');
    expect(screen.queryByRole('button', { name: '用户菜单' })).not.toBeInTheDocument();
    const loginCall = fetchMock.mock.calls.find(([, options]) => options?.method === 'POST');
    expect(JSON.parse(loginCall[1].body)).toEqual({ email: 'user@example.com', password: 'test-password' });
  });

  it('logs out through the AccountMenu and returns the app to anonymous', async () => {
    const fetchMock = authenticatedFetch();
    vi.stubGlobal('fetch', fetchMock);
    const user = userEvent.setup();
    render(<App />);

    await user.click(await screen.findByRole('button', { name: '用户菜单' }));
    const menu = screen.getByRole('dialog', { name: '账户与积分' });
    await user.click(within(menu).getByRole('button', { name: '退出登录' }));

    expect(await screen.findByRole('button', { name: '登录' })).toBeInTheDocument();
    const logoutCall = fetchMock.mock.calls.find(([, options]) => options?.method === 'DELETE');
    expect(logoutCall[1].headers).toMatchObject({ 'X-CSRF-Token': 'csrf-1' });
  });

  it('preserves Prompt and Idempotency-Key across login, lost response, and explicit retry', async () => {
    let quickCreateAttempt = 0;
    const fetchMock = vi.fn(async (input, options = {}) => {
      const path = requestPath(input);
      const method = options.method || 'GET';
      if (path === '/api/v1/auth/session' && method === 'GET') {
        return errorResponse(401, 'UNAUTHENTICATED', '未登录');
      }
      if (path === '/api/v1/auth/session' && method === 'POST') {
        return jsonResponse(authPayload());
      }
      if (path === '/api/v1/projects:quick-create') {
        quickCreateAttempt += 1;
        if (quickCreateAttempt === 1) {
          throw new TypeError('网络响应丢失');
        }
        return jsonResponse({ project_id: WORKSPACE_IDS.project, creation_status: 'provisioning', session_id: null }, 201);
      }
      if (path === `/api/v1/projects/${WORKSPACE_IDS.project}/bootstrap`) {
        return jsonResponse(projectBootstrapFixture({
          recent_run_status: 'queued', initial_prompt_status: 'accepted', input_id: WORKSPACE_IDS.input
        }));
      }
      if (path === `/api/v1/agent/sessions/${WORKSPACE_IDS.session}/workspace`) {
        return jsonResponse(workspaceSnapshotFixture());
      }
      return errorResponse(404, 'NOT_FOUND', 'not found');
    });
    vi.stubGlobal('fetch', fetchMock);
    const user = userEvent.setup();
    render(<App />);

    await user.type(screen.getByPlaceholderText('由一个想法或故事开始...'), '做一支霓虹短片');
    const createButton = screen.getByRole('button', { name: '开始创作' });
    await user.click(createButton);
    await user.click(createButton);
    const dialog = screen.getByRole('dialog', { name: '登录后继续创作' });
    await fillAndSubmitLogin(user, dialog);

    expect(await screen.findByRole('alert')).toHaveTextContent('网络响应丢失');
    await user.click(screen.getByRole('button', { name: '使用原请求重试' }));

    expect(await screen.findByText('工作台已就绪')).toBeInTheDocument();
    expect(window.location.pathname).toBe(`/projects/${WORKSPACE_IDS.project}/workspace`);
    const quickCalls = fetchMock.mock.calls.filter(([input]) => requestPath(input) === '/api/v1/projects:quick-create');
    expect(quickCalls).toHaveLength(2);
    expect(quickCalls[0][1].headers['Idempotency-Key']).toBe(quickCalls[1][1].headers['Idempotency-Key']);
    expect(quickCalls[0][1].headers['X-CSRF-Token']).toBe('csrf-1');
    expect(JSON.parse(quickCalls[0][1].body)).toEqual({ initial_prompt: '做一支霓虹短片' });
    expect(JSON.parse(quickCalls[1][1].body)).toEqual({ initial_prompt: '做一支霓虹短片' });
  });

  it('submits an empty Prompt without inventing a demo session', async () => {
    const fetchMock = authenticatedFetch({ quickCreateReady: true });
    vi.stubGlobal('fetch', fetchMock);
    const storage = { clear: vi.fn(), getItem: vi.fn(() => null), removeItem: vi.fn(), setItem: vi.fn() };
    vi.stubGlobal('localStorage', storage);
    const user = userEvent.setup();
    render(<App />);

    await user.click(screen.getByRole('button', { name: '开始创作' }));
    expect(await screen.findByText('工作台已就绪')).toBeInTheDocument();

    const quickCall = fetchMock.mock.calls.find(([input]) => requestPath(input) === '/api/v1/projects:quick-create');
    expect(JSON.parse(quickCall[1].body)).toEqual({ initial_prompt: '' });
    expect(fetchMock.mock.calls.some(([input]) => requestPath(input) === '/api/aigc/sessions')).toBe(false);
    expect(storage.getItem).not.toHaveBeenCalled();
    expect(storage.setItem).not.toHaveBeenCalled();
  });

  it('clears a formal protected workspace immediately when Bootstrap returns 401', async () => {
    window.history.pushState({}, '', `/projects/${WORKSPACE_IDS.project}/workspace`);
    const fetchMock = vi.fn(async (input) => {
      const path = requestPath(input);
      if (path === '/api/v1/auth/session') {
        return jsonResponse(authPayload());
      }
      return errorResponse(401, 'UNAUTHENTICATED', '会话已撤销');
    });
    vi.stubGlobal('fetch', fetchMock);
    render(<App />);

    expect(await screen.findByRole('heading', { name: '请先登录' })).toBeInTheDocument();
    expect(screen.queryByText('工作台已就绪')).not.toBeInTheDocument();
    expect(screen.queryByText(WORKSPACE_IDS.session)).not.toBeInTheDocument();
  });

  it('cancels an accepted-page transition when logout wins a pending QuickCreate response', async () => {
    let resolveQuickCreate;
    let quickCreateSignal;
    const pendingQuickCreate = new Promise((resolve) => {
      resolveQuickCreate = resolve;
    });
    const fetchMock = vi.fn(async (input, options = {}) => {
      const path = requestPath(input);
      const method = options.method || 'GET';
      if (path === '/api/v1/auth/session' && method === 'GET') {
        return jsonResponse(authPayload());
      }
      if (path === '/api/v1/auth/session' && method === 'DELETE') {
        return new Response(null, { status: 204 });
      }
      if (path === '/api/v1/projects:quick-create') {
        quickCreateSignal = options.signal;
        return pendingQuickCreate;
      }
      return errorResponse(404, 'NOT_FOUND', 'not found');
    });
    vi.stubGlobal('fetch', fetchMock);
    const user = userEvent.setup();
    render(<App />);

    await user.click(await screen.findByRole('button', { name: '开始创作' }));
    await user.click(screen.getByRole('button', { name: '用户菜单' }));
    await user.click(within(screen.getByRole('dialog', { name: '账户与积分' })).getByRole('button', { name: '退出登录' }));

    expect(quickCreateSignal.aborted).toBe(true);
    resolveQuickCreate(jsonResponse({ project_id: 'p-late', creation_status: 'ready', session_id: 's-late' }, 201));
    await waitFor(() => expect(screen.getByRole('button', { name: '登录' })).toBeInTheDocument());
    expect(window.location.pathname).toBe('/');
    expect(screen.queryByText('s-late')).not.toBeInTheDocument();
  });
});

function authenticatedFetch({ quickCreateReady = false } = {}) {
  return vi.fn(async (input, options = {}) => {
    const path = requestPath(input);
    const method = options.method || 'GET';
    if (path === '/api/v1/auth/session' && method === 'GET') {
      return jsonResponse(authPayload());
    }
    if (path === '/api/v1/auth/session' && method === 'DELETE') {
      return new Response(null, { status: 204 });
    }
    if (path === '/api/v1/projects:quick-create') {
      return jsonResponse({
        project_id: WORKSPACE_IDS.project,
        creation_status: quickCreateReady ? 'ready' : 'provisioning',
        session_id: quickCreateReady ? WORKSPACE_IDS.session : null
      }, 201);
    }
    if (path === `/api/v1/projects/${WORKSPACE_IDS.project}/bootstrap`) {
      return jsonResponse(projectBootstrapFixture());
    }
    if (path === `/api/v1/agent/sessions/${WORKSPACE_IDS.session}/workspace`) {
      return jsonResponse(workspaceSnapshotFixture());
    }
    return errorResponse(404, 'NOT_FOUND', 'not found');
  });
}

async function fillAndSubmitLogin(user, dialog) {
  await user.type(within(dialog).getByRole('textbox', { name: '邮箱' }), 'user@example.com');
  await user.type(within(dialog).getByLabelText('密码'), 'test-password');
  await user.click(within(dialog).getByRole('button', { name: '登录并继续' }));
}

function authPayload() {
  return {
    status: 'authenticated',
    principal: {
      id: 'u1', display_name: 'User', email: 'u***@example.com', account_status: 'active', roles: ['user'], capabilities: []
    },
    csrf_token: 'csrf-1',
    session_expires_at: '2026-07-15T08:00:00Z'
  };
}

function requestPath(input) {
  return new URL(typeof input === 'string' ? input : input.url, 'http://localhost').pathname;
}

function jsonResponse(data, status = 200) {
  return new Response(JSON.stringify(data), { status, headers: { 'Content-Type': 'application/json' } });
}

function errorResponse(status, code, message) {
  return jsonResponse({ error: { code, message, retryable: false } }, status);
}
