import { expect, test } from '@playwright/test';
import { randomBytes, randomUUID } from 'node:crypto';
import { chmod, mkdir, readFile, rename, writeFile } from 'node:fs/promises';
import { dirname } from 'node:path';

const email = process.env.DORA_E2E_USER_EMAIL || '';
const password = process.env.DORA_E2E_USER_PASSWORD || '';
const ownerBEmail = process.env.DORA_E2E_OWNER_B_EMAIL || '';
const ownerBPassword = process.env.DORA_E2E_OWNER_B_PASSWORD || '';
const resultPath = process.env.DORA_E2E_W05_RESULT_PATH || '';
const retentionControlDir = process.env.DORA_E2E_W05_RETENTION_CONTROL_DIR || '';
const prompt = process.env.DORA_E2E_PROMPT || `W0 浏览器冒烟 ${Date.now()}`;

const FORMAL_AGENT_API_PREFIX = '/api/v1/agent/';
const LEGACY_DEMO_API_PREFIX = '/api/aigc/';
const UUID_V7_PATTERN = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
const W05_RESULT_SCHEMA = 'w05.workspace-browser.smoke.result.v2';
const RETENTION_REQUEST_SCHEMA = 'w05.retention-window.fixture.request.v1';
const RETENTION_ACK_SCHEMA = 'w05.retention-window.fixture.ack.v1';

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

function createFixtureUUIDv7() {
  const bytes = randomBytes(16);
  const timestamp = BigInt(Date.now());
  for (let index = 5; index >= 0; index -= 1) {
    bytes[index] = Number((timestamp >> BigInt((5 - index) * 8)) & 0xffn);
  }
  bytes[6] = (bytes[6] & 0x0f) | 0x70;
  bytes[8] = (bytes[8] & 0x3f) | 0x80;
  const hex = bytes.toString('hex');
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
}

async function writeControlJSON(name, payload) {
  const path = `${retentionControlDir}/${name}`;
  const temporaryPath = `${path}.${process.pid}.tmp`;
  await writeFile(temporaryPath, `${JSON.stringify(payload)}\n`, { encoding: 'utf8', mode: 0o600 });
  await chmod(temporaryPath, 0o600);
  await rename(temporaryPath, path);
}

async function waitForControlJSON(name, timeout = 30_000) {
  const path = `${retentionControlDir}/${name}`;
  const deadline = Date.now() + timeout;
  while (Date.now() < deadline) {
    try {
      return JSON.parse(await readFile(path, 'utf8'));
    } catch (error) {
      if (error?.code !== 'ENOENT' && !(error instanceof SyntaxError)) throw error;
    }
    await new Promise((resolve) => setTimeout(resolve, 50));
  }
  throw new Error(`等待 W0.5 Retention 控制文件超时: ${name}`);
}

function isSessionWorkspaceResponse(response, sessionID) {
  const url = new URL(response.url());
  return response.request().method() === 'GET'
    && url.pathname === `${FORMAL_AGENT_API_PREFIX}sessions/${sessionID}/workspace`
    && response.status() === 200;
}

test.describe('W0 real browser transport smoke', () => {
  test.skip(
    !email || !password || !ownerBEmail || !ownerBPassword || !resultPath || !retentionControlDir,
    '需要同时提供用户 A、Owner B 真实冒烟账号、结果路径和 Retention 控制目录'
  );

  test('login -> Quick Create -> retention reset -> controlled disconnect -> cross-owner gate', async ({ page }) => {
    test.setTimeout(210_000);

    const requests = [];
    const eventSourceMessages = [];
    const eventSourceRequestURLs = new Map();
    let controlledDisconnect = false;
    let sameSessionRecovery = false;
    let retentionResetReceived = false;
    let retentionResetWithoutID = false;
    let retentionSnapshotRetained = false;
    let retentionSnapshotReloaded = false;
    let retentionSameSessionRecovery = false;
    let retentionNoStaleEventReplayed = false;
    let crossOwnerNotFound = false;
    let crossOwnerAgentBlocked = false;
    let resourceFactsNotDisclosed = false;
    const cdp = await page.context().newCDPSession(page);
    await cdp.send('Network.enable');
    cdp.on('Network.eventSourceMessageReceived', (message) => {
      eventSourceMessages.push(message);
    });
    cdp.on('Network.requestWillBeSent', (request) => {
      eventSourceRequestURLs.set(request.requestId, request.request.url);
    });
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

    await page.evaluate(({ expectedProjectID, expectedSessionID, expectedPrompt }) => {
      const transitions = [];
      const record = () => {
        const workspace = document.querySelector('main[data-workspace-state]');
        if (!workspace) return;
        transitions.push({
          state: workspace.getAttribute('data-workspace-state'),
          stream: workspace.getAttribute('data-stream-state'),
          projectID: workspace.getAttribute('data-project-id'),
          sessionID: workspace.getAttribute('data-session-id'),
          busy: workspace.getAttribute('aria-busy'),
          expectedProjectionRetained: workspace.getAttribute('data-project-id') === expectedProjectID
            && workspace.getAttribute('data-session-id') === expectedSessionID
            && document.body.textContent.includes(expectedPrompt),
          resetStatusVisible: document.body.textContent.includes('正在同步最新工作台状态…')
        });
      };
      const observer = new MutationObserver(record);
      observer.observe(document.documentElement, {
        attributes: true,
        attributeFilter: ['data-workspace-state', 'data-stream-state', 'aria-busy'],
        childList: true,
        subtree: true
      });
      window.__doraW05WorkspaceTransitions = transitions;
      window.__doraW05WorkspaceObserver = observer;
      record();
    }, { expectedProjectID: projectID, expectedSessionID: sessionID, expectedPrompt: prompt });

    const retentionEventID = createFixtureUUIDv7();
    const retentionBootstrapPromise = page.waitForResponse((response) => {
      const url = new URL(response.url());
      return response.request().method() === 'GET'
        && url.pathname === `/api/v1/projects/${projectID}/bootstrap`
        && response.status() === 200;
    }, { timeout: 30_000 });
    const retentionSnapshotPromise = page.waitForResponse(async (response) => {
      if (!isSessionWorkspaceResponse(response, sessionID)) return false;
      try {
        const payload = await response.json();
        return payload?.session?.id === sessionID
          && payload?.session?.project_id === projectID
          && payload?.event_high_watermark === 3
          && payload?.min_available_seq === 3;
      } catch {
        return false;
      }
    }, { timeout: 30_000 });
    await writeControlJSON('request.json', {
      schema_version: RETENTION_REQUEST_SCHEMA,
      project_id: projectID,
      session_id: sessionID,
      input_id: inputID,
      event_id: retentionEventID
    });
    const retentionAck = await waitForControlJSON('ack.json');
    expect(Object.keys(retentionAck).sort()).toEqual([
      'event_id', 'input_id', 'inserted_events', 'last_seq', 'min_available_seq', 'project_id',
      'pruned_events', 'retained_event_seq', 'schema_version', 'session_id'
    ]);
    expect(retentionAck).toEqual({
      schema_version: RETENTION_ACK_SCHEMA,
      project_id: projectID,
      session_id: sessionID,
      input_id: inputID,
      event_id: retentionEventID,
      inserted_events: 1,
      pruned_events: 2,
      last_seq: 3,
      min_available_seq: 3,
      retained_event_seq: 3
    });

    await expect.poll(async () => page.evaluate(() => (
      window.__doraW05WorkspaceTransitions.some((transition) => (
        transition.state === 'reset'
        && transition.stream === 'connecting'
        && transition.busy === 'true'
        && transition.expectedProjectionRetained
        && transition.resetStatusVisible
      ))
    )), { timeout: 30_000 }).toBe(true);
    retentionSnapshotRetained = true;

    const [retentionBootstrapResponse, retentionSnapshotResponse] = await Promise.all([
      retentionBootstrapPromise,
      retentionSnapshotPromise
    ]);
    expect(retentionBootstrapResponse.status()).toBe(200);
    const retentionSnapshot = await retentionSnapshotResponse.json();
    expect(retentionSnapshot.session?.id).toBe(sessionID);
    expect(retentionSnapshot.session?.project_id).toBe(projectID);
    expect(retentionSnapshot.event_high_watermark).toBe(3);
    expect(retentionSnapshot.min_available_seq).toBe(3);
    expect(retentionSnapshot.messages?.some((message) => message.content === prompt)).toBe(true);
    expect(retentionSnapshot.inputs?.some((input) => input.id === inputID)).toBe(true);
    retentionSnapshotReloaded = true;

    await expect.poll(() => eventSourceMessages.some((message) => {
      if (message.eventName !== 'stream.reset') return false;
      try {
        const payload = JSON.parse(message.data);
        return payload.schema_version === 'workspace.stream-control.v1'
          && payload.event === 'stream.reset'
          && payload.session_id === sessionID
          && payload.reason === 'cursor_expired'
          && payload.snapshot_required === true
          && payload.min_available_seq === 3
          && payload.latest_seq === 3;
      } catch {
        return false;
      }
    }), { timeout: 30_000 }).toBe(true);
    const retentionResetMessage = eventSourceMessages.find((message) => {
      if (message.eventName !== 'stream.reset') return false;
      try {
        return JSON.parse(message.data).session_id === sessionID;
      } catch {
        return false;
      }
    });
    retentionResetReceived = Boolean(retentionResetMessage);
    retentionResetWithoutID = retentionResetMessage?.eventId === '';
    expect(retentionResetReceived).toBe(true);
    expect(retentionResetWithoutID).toBe(true);

    const workspaceAfterRetentionReset = await assertLiveWorkspace(page, {
      projectID,
      sessionID,
      firstPrompt: prompt
    });
    retentionSameSessionRecovery = await workspaceAfterRetentionReset.getAttribute('data-project-id') === projectID
      && await workspaceAfterRetentionReset.getAttribute('data-session-id') === sessionID;
    expect(retentionSameSessionRecovery).toBe(true);
    await expect.poll(() => eventSourceMessages.some((message) => {
      if (message.eventName !== 'stream.ready') return false;
      try {
        const payload = JSON.parse(message.data);
        return payload.schema_version === 'workspace.stream-control.v1'
          && payload.event === 'stream.ready'
          && payload.session_id === sessionID
          && payload.cursor === 3
          && payload.min_available_seq === 3
          && payload.latest_seq === 3;
      } catch {
        return false;
      }
    }), { timeout: 30_000 }).toBe(true);
    const retentionReadyMessage = eventSourceMessages.find((message) => {
      if (message.eventName !== 'stream.ready') return false;
      try {
        const payload = JSON.parse(message.data);
        return payload.session_id === sessionID
          && payload.cursor === 3
          && payload.min_available_seq === 3
          && payload.latest_seq === 3;
      } catch {
        return false;
      }
    });
    expect(retentionReadyMessage?.eventId).toBe('');
    expect(retentionReadyMessage?.requestId).toBeTruthy();
    expect(retentionReadyMessage?.requestId).not.toBe(retentionResetMessage?.requestId);
    const retentionReadyURL = new URL(eventSourceRequestURLs.get(retentionReadyMessage.requestId));
    expect(retentionReadyURL.pathname).toBe(`${FORMAL_AGENT_API_PREFIX}sessions/${sessionID}/events`);
    expect([...retentionReadyURL.searchParams.entries()]).toEqual([['after_seq', '3']]);
    retentionNoStaleEventReplayed = !eventSourceMessages.some((message) => {
      if (message.eventName !== 'session.input.accepted') return false;
      try {
        const payload = JSON.parse(message.data);
        return payload.session_id === sessionID && payload.seq === 3;
      } catch {
        return false;
      }
    });
    expect(retentionNoStaleEventReplayed).toBe(true);

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
      retention_reset_received: retentionResetReceived,
      retention_reset_without_id: retentionResetWithoutID,
      retention_snapshot_retained: retentionSnapshotRetained,
      retention_snapshot_reloaded: retentionSnapshotReloaded,
      retention_same_session_recovery: retentionSameSessionRecovery,
      retention_no_stale_event_replayed: retentionNoStaleEventReplayed,
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
