import { expect, test } from '@playwright/test';
import { randomUUID } from 'node:crypto';

const email = process.env.DORA_E2E_USER_EMAIL || '';
const password = process.env.DORA_E2E_USER_PASSWORD || '';
const prompt = process.env.DORA_E2E_PROMPT || `W0 浏览器冒烟 ${Date.now()}`;

const FORMAL_AGENT_API_PREFIX = '/api/v1/agent/';
const LEGACY_DEMO_API_PREFIX = '/api/aigc/';

function waitForWorkspaceTransportResponses(page) {
  return {
    snapshot: page.waitForResponse((response) => {
      const url = new URL(response.url());
      return response.request().method() === 'GET'
        && url.pathname.startsWith(`${FORMAL_AGENT_API_PREFIX}sessions/`)
        && url.pathname.endsWith('/workspace');
    }),
    stream: page.waitForResponse((response) => {
      const url = new URL(response.url());
      return response.request().method() === 'GET'
        && url.pathname.startsWith(`${FORMAL_AGENT_API_PREFIX}sessions/`)
        && url.pathname.endsWith('/events');
    })
  };
}

async function assertWorkspaceTransportResponses(responses) {
  const [snapshotResponse, streamResponse] = await Promise.all([
    responses.snapshot,
    responses.stream
  ]);
  expect(snapshotResponse.status()).toBe(200);
  expect(streamResponse.status()).toBe(200);
  expect(streamResponse.headers()['content-type'] || '').toContain('text/event-stream');
  return snapshotResponse;
}

async function assertLiveWorkspace(page, { projectID, sessionID, firstPrompt }) {
  const workspace = page.locator('main[data-workspace-state]');
  await expect(workspace).toHaveAttribute('data-workspace-state', 'ready', { timeout: 20_000 });
  await expect(workspace).toHaveAttribute('data-stream-state', 'live', { timeout: 20_000 });
  await expect(workspace).toHaveAttribute('data-project-id', projectID);
  if (sessionID) {
    await expect(workspace).toHaveAttribute('data-session-id', sessionID);
  } else {
    await expect(workspace).toHaveAttribute('data-session-id', /.+/);
  }
  await expect(page.getByText(firstPrompt, { exact: true })).toBeVisible();
  return workspace;
}

function createNonexistentProjectID(existingProjectID) {
  let candidate;
  do {
    const uuid = randomUUID();
    candidate = `${uuid.slice(0, 14)}7${uuid.slice(15)}`;
  } while (candidate === existingProjectID);
  return candidate;
}

test.describe('W0 real browser transport smoke', () => {
  test.skip(
    !email || !password,
    '需要通过 DORA_E2E_USER_EMAIL 和 DORA_E2E_USER_PASSWORD 提供真实冒烟账号'
  );

  test('login -> Quick Create -> reload recovery -> not found -> logout gate', async ({ page }) => {
    test.setTimeout(90_000);

    const requests = [];
    page.on('request', (request) => {
      const url = new URL(request.url());
      requests.push({ method: request.method(), origin: url.origin, pathname: url.pathname });
    });

    await page.goto('/');
    const appOrigin = new URL(page.url()).origin;
    await expect(page.getByRole('button', { name: '登录' })).toBeVisible();

    await page.getByPlaceholder('由一个想法或故事开始...').fill(prompt);
    await page.getByRole('button', { name: '开始创作' }).click();

    const loginDialog = page.getByRole('dialog', { name: '登录后继续创作' });
    await expect(loginDialog).toBeVisible();
    await loginDialog.getByRole('textbox', { name: '邮箱' }).fill(email);
    await loginDialog.getByLabel('密码').fill(password);

    const quickCreateResponsePromise = page.waitForResponse((response) => {
      const url = new URL(response.url());
      return response.request().method() === 'POST' && url.pathname === '/api/v1/projects:quick-create';
    });
    const initialTransportResponses = waitForWorkspaceTransportResponses(page);
    await loginDialog.getByRole('button', { name: '登录并继续' }).click();

    const quickCreateResponse = await quickCreateResponsePromise;
    expect(quickCreateResponse.status()).toBe(201);
    const quickCreateRequest = quickCreateResponse.request();
    expect(quickCreateRequest.headers()['idempotency-key']).toBeTruthy();
    expect(quickCreateRequest.headers()['x-csrf-token']).toBeTruthy();
    expect(quickCreateRequest.postDataJSON()).toEqual({ initial_prompt: prompt });

    const quickCreatePayload = await quickCreateResponse.json();
    expect(String(quickCreatePayload.project_id || '')).not.toBe('');
    const projectID = String(quickCreatePayload.project_id);
    const workspacePath = `/projects/${encodeURIComponent(projectID)}/workspace`;

    await expect(page).toHaveURL((url) => url.pathname === workspacePath);
    await expect(page.getByRole('heading', { name: '创作工作台' })).toBeVisible();
    const initialSnapshotResponse = await assertWorkspaceTransportResponses(initialTransportResponses);
    const initialSnapshot = await initialSnapshotResponse.json();
    const initialWorkspace = await assertLiveWorkspace(page, { projectID, firstPrompt: prompt });
    const sessionID = await initialWorkspace.getAttribute('data-session-id');
    expect(sessionID).toBeTruthy();
    expect(initialSnapshot.session?.project_id).toBe(projectID);
    expect(initialSnapshot.session?.id).toBe(sessionID);
    expect(initialSnapshot.messages?.some((message) => message.content === prompt)).toBe(true);

    const reloadTransportResponses = waitForWorkspaceTransportResponses(page);
    await page.reload();
    await expect(page).toHaveURL((url) => url.pathname === workspacePath);
    await assertWorkspaceTransportResponses(reloadTransportResponses);
    await assertLiveWorkspace(page, { projectID, sessionID, firstPrompt: prompt });

    const nonexistentProjectID = createNonexistentProjectID(projectID);
    const nonexistentWorkspacePath = `/projects/${nonexistentProjectID}/workspace`;
    const notFoundResponsePromise = page.waitForResponse((response) => {
      const url = new URL(response.url());
      return response.request().method() === 'GET'
        && url.pathname === `/api/v1/projects/${nonexistentProjectID}/bootstrap`;
    });
    await page.goto(nonexistentWorkspacePath);
    const notFoundResponse = await notFoundResponsePromise;
    expect(notFoundResponse.status()).toBe(404);
    const unavailableWorkspace = page.locator('main[data-workspace-state]');
    await expect(unavailableWorkspace).toHaveAttribute('data-workspace-state', 'not_found');
    await expect(unavailableWorkspace).toHaveAttribute('data-stream-state', 'closed');
    await expect(page.getByRole('heading', { name: '工作台不存在或不可访问' })).toBeVisible();
    await expect(unavailableWorkspace).not.toHaveAttribute('data-session-id', /.+/);

    const returnTransportResponses = waitForWorkspaceTransportResponses(page);
    await page.goto(workspacePath);
    await assertWorkspaceTransportResponses(returnTransportResponses);
    await assertLiveWorkspace(page, { projectID, sessionID, firstPrompt: prompt });

    await page.goto('/');
    const accountButton = page.getByRole('button', { name: '用户菜单' });
    await expect(accountButton).toBeVisible();
    await accountButton.click();

    const logoutResponsePromise = page.waitForResponse((response) => {
      const url = new URL(response.url());
      return response.request().method() === 'DELETE' && url.pathname === '/api/v1/auth/session';
    });
    await page.getByRole('dialog', { name: '账户与积分' }).getByRole('button', { name: '退出登录' }).click();

    const logoutResponse = await logoutResponsePromise;
    expect(logoutResponse.status()).toBe(204);
    await expect(page.getByRole('button', { name: '登录' })).toBeVisible();

    const formalAgentRequestCountBeforeGate = requests.filter(
      (request) => request.pathname.startsWith(FORMAL_AGENT_API_PREFIX)
    ).length;
    const unauthorizedBootstrapPromise = page.waitForResponse((response) => {
      const url = new URL(response.url());
      return response.request().method() === 'GET'
        && url.pathname === '/api/v1/auth/session'
        && response.status() === 401;
    });
    await page.goto(workspacePath);
    await unauthorizedBootstrapPromise;
    await expect(page.getByRole('heading', { name: '请先登录' })).toBeVisible();
    await expect(page.getByText('工作台已就绪')).toHaveCount(0);
    expect(requests.filter((request) => request.pathname.startsWith(FORMAL_AGENT_API_PREFIX))).toHaveLength(
      formalAgentRequestCountBeforeGate
    );

    expect(requests.some(
      (request) => request.method === 'POST' && request.pathname === '/api/aigc/sessions'
    )).toBe(false);
    expect(requests.filter((request) => request.pathname.startsWith(LEGACY_DEMO_API_PREFIX))).toEqual([]);
    const formalAgentRequests = requests.filter(
      (request) => request.pathname.startsWith(FORMAL_AGENT_API_PREFIX)
    );
    expect(formalAgentRequests.length).toBeGreaterThan(0);
    expect(formalAgentRequests.every((request) => request.origin === appOrigin)).toBe(true);
  });
});
