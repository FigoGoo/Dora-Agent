import { expect, test } from '@playwright/test';
import { randomUUID } from 'node:crypto';
import { chmod, mkdir, writeFile } from 'node:fs/promises';
import { dirname } from 'node:path';

const email = process.env.DORA_E2E_USER_EMAIL || '';
const password = process.env.DORA_E2E_USER_PASSWORD || '';
const ownerBEmail = process.env.DORA_E2E_OWNER_B_EMAIL || '';
const ownerBPassword = process.env.DORA_E2E_OWNER_B_PASSWORD || '';
const resultPath = process.env.DORA_E2E_W05_RESULT_PATH || '';
const prompt = process.env.DORA_E2E_PROMPT || `W0 浏览器冒烟 ${Date.now()}`;

const FORMAL_AGENT_API_PREFIX = '/api/v1/agent/';
const LEGACY_DEMO_API_PREFIX = '/api/aigc/';
const UUID_V7_PATTERN = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
const W05_RESULT_SCHEMA = 'w05.workspace-browser.smoke.result.v1';

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
    !email || !password || !ownerBEmail || !ownerBPassword || !resultPath,
    '需要同时提供用户 A、Owner B 真实冒烟账号和 DORA_E2E_W05_RESULT_PATH'
  );

  test('login -> Quick Create -> controlled disconnect -> cross-owner gate', async ({ page }) => {
    test.setTimeout(150_000);

    const requests = [];
    let controlledDisconnect = false;
    let sameSessionRecovery = false;
    let crossOwnerNotFound = false;
    let crossOwnerAgentBlocked = false;
    let resourceFactsNotDisclosed = false;
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
    const creatorLoginResponsePromise = page.waitForResponse((response) => {
      const url = new URL(response.url());
      return response.request().method() === 'POST' && url.pathname === '/api/v1/auth/session';
    });
    const initialTransportResponses = waitForWorkspaceTransportResponses(page);
    await loginDialog.getByRole('button', { name: '登录并继续' }).click();

    const creatorLoginResponse = await creatorLoginResponsePromise;
    expect(creatorLoginResponse.status()).toBe(200);
    const creatorLoginPayload = await creatorLoginResponse.json();
    const creatorUserID = String(creatorLoginPayload.principal?.id || '');
    expect(creatorUserID).toMatch(UUID_V7_PATTERN);

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
    const inputID = String(initialSnapshot.inputs?.[0]?.id || '');
    expect(inputID).toMatch(UUID_V7_PATTERN);

    const reloadTransportResponses = waitForWorkspaceTransportResponses(page);
    await page.reload();
    await expect(page).toHaveURL((url) => url.pathname === workspacePath);
    await assertWorkspaceTransportResponses(reloadTransportResponses);
    await assertLiveWorkspace(page, { projectID, sessionID, firstPrompt: prompt });

    const workspaceBeforeDisconnect = page.locator('main[data-workspace-state]');
    await page.context().setOffline(true);
    try {
      expect(await page.evaluate(() => navigator.onLine)).toBe(false);
      await expect(workspaceBeforeDisconnect).toHaveAttribute('data-workspace-state', 'ready', { timeout: 20_000 });
      // 已建立的 SSE socket 可能存活到服务端连接期限；等待窗口必须覆盖本地门禁的 25 秒上限。
      await expect(workspaceBeforeDisconnect).toHaveAttribute('data-stream-state', 'reconnecting', { timeout: 40_000 });
      const retainedProjectID = await workspaceBeforeDisconnect.getAttribute('data-project-id');
      const retainedSessionID = await workspaceBeforeDisconnect.getAttribute('data-session-id');
      const retainedPromptVisible = await page.getByText(prompt, { exact: true }).isVisible();
      controlledDisconnect = retainedProjectID === projectID
        && retainedSessionID === sessionID
        && retainedPromptVisible;
      expect(controlledDisconnect).toBe(true);
    } finally {
      const recoveryTransportResponses = waitForWorkspaceTransportResponses(page);
      await page.context().setOffline(false);
      expect(await page.evaluate(() => navigator.onLine)).toBe(true);
      await assertWorkspaceTransportResponses(recoveryTransportResponses);
    }
    const recoveredWorkspace = await assertLiveWorkspace(page, { projectID, sessionID, firstPrompt: prompt });
    sameSessionRecovery = await recoveredWorkspace.getAttribute('data-project-id') === projectID
      && await recoveredWorkspace.getAttribute('data-session-id') === sessionID;
    expect(sameSessionRecovery).toBe(true);

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

    await page.goto('/');
    const ownerBLoginPayload = await loginAs(page, ownerBEmail, ownerBPassword);
    const crossOwnerUserID = String(ownerBLoginPayload.principal?.id || '');
    expect(crossOwnerUserID).toMatch(UUID_V7_PATTERN);
    expect(crossOwnerUserID).not.toBe(creatorUserID);

    const agentRequestCountBeforeCrossOwner = requests.filter(
      (request) => request.pathname.startsWith(FORMAL_AGENT_API_PREFIX)
    ).length;
    const crossOwnerBootstrapPromise = page.waitForResponse((response) => {
      const url = new URL(response.url());
      return response.request().method() === 'GET'
        && url.pathname === `/api/v1/projects/${projectID}/bootstrap`;
    });
    await page.goto(workspacePath);
    const crossOwnerBootstrapResponse = await crossOwnerBootstrapPromise;
    expect(crossOwnerBootstrapResponse.headers()['content-type'] || '').toContain('application/json');
    const crossOwnerBootstrapBody = await crossOwnerBootstrapResponse.json();
    expect(Object.keys(crossOwnerBootstrapBody).sort()).toEqual(['error']);
    expect(Object.keys(crossOwnerBootstrapBody.error || {}).sort()).toEqual([
      'code', 'details', 'message', 'request_id', 'retryable'
    ]);
    expect(crossOwnerBootstrapBody.error).toMatchObject({
      code: 'PROJECT_NOT_FOUND',
      message: '项目不存在或不可访问',
      retryable: false
    });
    expect(crossOwnerBootstrapBody.error?.details).toEqual({});
    expect(String(crossOwnerBootstrapBody.error?.request_id || '')).toMatch(UUID_V7_PATTERN);
    const crossOwnerWorkspace = page.locator('main[data-workspace-state]');
    await expect(crossOwnerWorkspace).toHaveAttribute('data-workspace-state', 'not_found');
    await expect(crossOwnerWorkspace).toHaveAttribute('data-stream-state', 'closed');
    await expect(page.getByRole('heading', { name: '工作台不存在或不可访问' })).toBeVisible();
    await expect(crossOwnerWorkspace).not.toHaveAttribute('data-project-id', /.+/);
    await expect(crossOwnerWorkspace).not.toHaveAttribute('data-session-id', /.+/);
    crossOwnerNotFound = crossOwnerBootstrapResponse.status() === 404;
    expect(crossOwnerNotFound).toBe(true);

    const agentRequestCountAfterCrossOwner = requests.filter(
      (request) => request.pathname.startsWith(FORMAL_AGENT_API_PREFIX)
    ).length;
    crossOwnerAgentBlocked = agentRequestCountAfterCrossOwner === agentRequestCountBeforeCrossOwner;
    expect(crossOwnerAgentBlocked).toBe(true);

    const crossOwnerResponseText = JSON.stringify(crossOwnerBootstrapBody);
    const crossOwnerDocument = await page.content();
    const forbiddenFacts = [projectID, sessionID, inputID, creatorUserID, email, prompt];
    resourceFactsNotDisclosed = forbiddenFacts.every((fact) => (
      !crossOwnerResponseText.includes(fact) && !crossOwnerDocument.includes(fact)
    ));
    expect(resourceFactsNotDisclosed).toBe(true);

    expect(requests.some(
      (request) => request.method === 'POST' && request.pathname === '/api/aigc/sessions'
    )).toBe(false);
    expect(requests.filter((request) => request.pathname.startsWith(LEGACY_DEMO_API_PREFIX))).toEqual([]);
    const formalAgentRequests = requests.filter(
      (request) => request.pathname.startsWith(FORMAL_AGENT_API_PREFIX)
    );
    expect(formalAgentRequests.length).toBeGreaterThan(0);
    expect(formalAgentRequests.every((request) => request.origin === appOrigin)).toBe(true);

    await writeW05Result(resultPath, {
      schema_version: W05_RESULT_SCHEMA,
      creator_user_id: creatorUserID,
      cross_owner_user_id: crossOwnerUserID,
      project_id: projectID,
      session_id: sessionID,
      controlled_disconnect: controlledDisconnect,
      same_session_recovery: sameSessionRecovery,
      cross_owner_not_found: crossOwnerNotFound,
      cross_owner_agent_blocked: crossOwnerAgentBlocked,
      resource_facts_not_disclosed: resourceFactsNotDisclosed
    });
  });
});

async function loginAs(page, userEmail, userPassword) {
  await page.getByRole('button', { name: '登录' }).click();
  const loginDialog = page.getByRole('dialog', { name: '登录后继续创作' });
  await loginDialog.getByRole('textbox', { name: '邮箱' }).fill(userEmail);
  await loginDialog.getByLabel('密码').fill(userPassword);
  const loginResponsePromise = page.waitForResponse((response) => (
    response.request().method() === 'POST' && new URL(response.url()).pathname === '/api/v1/auth/session'
  ));
  await loginDialog.getByRole('button', { name: '登录并继续' }).click();
  const loginResponse = await loginResponsePromise;
  expect(loginResponse.status()).toBe(200);
  const payload = await loginResponse.json();
  await expect(page.getByRole('button', { name: '用户菜单' })).toBeVisible();
  return payload;
}

async function writeW05Result(path, result) {
  await mkdir(dirname(path), { recursive: true, mode: 0o700 });
  await writeFile(path, `${JSON.stringify(result)}\n`, { encoding: 'utf8', mode: 0o600 });
  await chmod(path, 0o600);
}
