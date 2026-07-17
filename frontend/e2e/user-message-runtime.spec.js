import { expect, test } from '@playwright/test';
import { randomUUID } from 'node:crypto';
import { chmod, mkdir, rename, rm, writeFile } from 'node:fs/promises';
import { dirname } from 'node:path';

const enabled = process.env.DORA_E2E_USER_MESSAGE_RUNTIME === '1';
const email = process.env.DORA_E2E_USER_EMAIL || '';
const password = process.env.DORA_E2E_USER_PASSWORD || '';
const resultPath = process.env.DORA_E2E_USER_MESSAGE_RESULT_PATH || '';
const prompt = process.env.DORA_E2E_USER_MESSAGE_PROMPT
  || `User Message Runtime 浏览器冒烟 ${Date.now()}`;

const UUID_V7_PATTERN = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
const RESULT_SCHEMA = 'user_message_runtime.browser_result.v1';
const WORKSPACE_SCHEMA = 'session.workspace.v2';
const DIRECT_RESPONSE_SCHEMA = 'session.turn.direct_response.card.v1';
const DIRECT_RESPONSE_SUMMARY = '已收到你的创作需求。你可以继续打开工具箱选择下一步流程。';

test.describe('@user-message-runtime real browser vertical slice', () => {
  test.skip(
    !enabled,
    '该用例只在 DORA_E2E_USER_MESSAGE_RUNTIME=1 时运行；默认测试不得伪造真实 Runtime Evidence'
  );

  test('Landing prompt -> QuickCreate -> Workspace v2/SSE -> Card -> toolbox focus -> reload', async ({
    page,
    browserName
  }) => {
    test.setTimeout(180_000);
    requireRuntimeInputs();

    const apiRequests = [];
    const eventSourceMessages = [];
    const eventSourceRequests = [];
    const cdp = await page.context().newCDPSession(page);
    await cdp.send('Network.enable');
    cdp.on('Network.eventSourceMessageReceived', (message) => eventSourceMessages.push(message));
    cdp.on('Network.requestWillBeSent', ({ request }) => {
      const url = new URL(request.url);
      if (url.pathname.endsWith('/events')) eventSourceRequests.push(url.href);
    });
    page.on('request', (request) => {
      const url = new URL(request.url());
      if (url.pathname.startsWith('/api/')) {
        apiRequests.push({
          method: request.method(),
          origin: url.origin,
          pathname: url.pathname
        });
      }
    });

    await page.goto('/');
    const appOrigin = new URL(page.url()).origin;
    expect(browserName).toBe('chromium');
    expect(await page.evaluate(() => navigator.userAgent)).toMatch(/(?:Headless)?Chrome\//);

    await page.getByPlaceholder('由一个想法或故事开始...').fill(prompt);
    await page.getByRole('button', { name: '开始创作' }).click();
    const loginDialog = page.getByRole('dialog', { name: '登录后继续创作' });
    await expect(loginDialog).toBeVisible();
    await loginDialog.getByRole('textbox', { name: '邮箱' }).fill(email);
    await loginDialog.getByLabel('密码').fill(password);

    const loginResponsePromise = waitForJSONResponse(page, 'POST', '/api/v1/auth/session');
    const quickCreateResponsePromise = waitForJSONResponse(page, 'POST', '/api/v1/projects:quick-create');
    const initialSnapshotPromise = waitForWorkspaceResponse(page);
    const initialStreamPromise = waitForStreamResponse(page);
    await loginDialog.getByRole('button', { name: '登录并继续' }).click();

    const loginResponse = await loginResponsePromise;
    expect(loginResponse.status()).toBe(200);

    const quickCreateResponse = await quickCreateResponsePromise;
    expect(quickCreateResponse.status()).toBe(201);
    const quickCreateRequest = quickCreateResponse.request();
    expect(quickCreateRequest.headers()['idempotency-key']).toBeTruthy();
    expect(quickCreateRequest.headers()['x-csrf-token']).toBeTruthy();
    expect(quickCreateRequest.postDataJSON()).toEqual({ initial_prompt: prompt });

    const quickCreatePayload = await quickCreateResponse.json();
    const projectID = String(quickCreatePayload.project_id || '');
    expect(projectID).toMatch(UUID_V7_PATTERN);
    const workspacePath = `/projects/${projectID}/workspace`;
    await expect(page).toHaveURL((url) => url.pathname === workspacePath);

    const initialSnapshotResponse = await initialSnapshotPromise;
    expect(initialSnapshotResponse.status()).toBe(200);
    const initialSnapshot = await initialSnapshotResponse.json();
    expect(initialSnapshot.schema_version).toBe(WORKSPACE_SCHEMA);
    expect(initialSnapshot.session?.project_id).toBe(projectID);

    const initialStreamResponse = await initialStreamPromise;
    expect(initialStreamResponse.status()).toBe(200);
    expect(initialStreamResponse.headers()['content-type'] || '').toContain('text/event-stream');

    const workspace = page.locator('main[data-workspace-state]');
    await expect(workspace).toHaveAttribute('data-workspace-state', 'ready', { timeout: 30_000 });
    await expect(workspace).toHaveAttribute('data-stream-state', 'live', { timeout: 30_000 });
    await expect(workspace).toHaveAttribute('data-project-id', projectID);
    const sessionID = String(await workspace.getAttribute('data-session-id') || '');
    expect(sessionID).toMatch(UUID_V7_PATTERN);
    expect(initialSnapshot.session?.id).toBe(sessionID);
    await expect.poll(() => hasStreamReady(eventSourceMessages, sessionID), { timeout: 30_000 }).toBe(true);

    const directResponseCard = page.locator('article.turn-output-card[data-turn-output-status="completed"]');
    await expect(directResponseCard).toBeVisible({ timeout: 60_000 });
    await expect(directResponseCard.getByRole('heading', { name: '需求已接收' })).toBeVisible();
    await expect(directResponseCard).toContainText(DIRECT_RESPONSE_SUMMARY);

    const terminalSnapshotResponse = await sameOriginJSON(
      page,
      `/api/v1/agent/sessions/${sessionID}/workspace`
    );
    expect(terminalSnapshotResponse.status).toBe(200);
    const terminalSnapshot = terminalSnapshotResponse.payload;
    const output = assertCompletedSnapshot(terminalSnapshot, { projectID, sessionID, prompt });
    const inputID = output.input_id;
    const turnID = output.turn_id;
    const runID = output.run_id;
    await expect(directResponseCard).toHaveAttribute('data-turn-id', turnID);
    await assertNoAssistantMessage(page, terminalSnapshot, prompt);

    const terminalEvent = findCompletedEvent(eventSourceMessages, { projectID, sessionID, inputID });
    if (terminalEvent) expect(terminalEvent.payload).toEqual(output);

    const toolbox = page.locator('#workspace-toolbox');
    await expect(toolbox).toBeVisible();
    await expect(toolbox).not.toBeFocused();
    const URLBeforeToolbox = page.url();
    const mutatingRequestCountBeforeToolbox = countMutatingAPIRequests(apiRequests);
    await directResponseCard.getByRole('button', { name: '打开工具箱' }).click();
    await expect(toolbox).toBeFocused();
    const toolboxFocused = await toolbox.evaluate((element) => document.activeElement === element);
    expect(page.url()).toBe(URLBeforeToolbox);
    await expect(directResponseCard).toHaveAttribute('data-turn-id', turnID);
    await page.waitForTimeout(200);
    expect(countMutatingAPIRequests(apiRequests)).toBe(mutatingRequestCountBeforeToolbox);

    const reloadSnapshotPromise = waitForWorkspaceResponse(page, sessionID);
    const reloadStreamPromise = waitForStreamResponse(page, sessionID);
    const streamRequestCountBeforeReload = eventSourceRequests.length;
    await page.reload();

    const reloadSnapshotResponse = await reloadSnapshotPromise;
    expect(reloadSnapshotResponse.status()).toBe(200);
    const reloadSnapshot = await reloadSnapshotResponse.json();
    const reloadOutput = assertCompletedSnapshot(reloadSnapshot, { projectID, sessionID, prompt });
    expect(reloadOutput).toEqual(output);

    const reloadStreamResponse = await reloadStreamPromise;
    expect(reloadStreamResponse.status()).toBe(200);
    expect(reloadStreamResponse.headers()['content-type'] || '').toContain('text/event-stream');
    await expect(workspace).toHaveAttribute('data-workspace-state', 'ready', { timeout: 30_000 });
    await expect(workspace).toHaveAttribute('data-stream-state', 'live', { timeout: 30_000 });
    await expect(workspace).toHaveAttribute('data-session-id', sessionID);
    await expect(directResponseCard).toBeVisible();
    await expect(directResponseCard).toHaveAttribute('data-turn-id', turnID);
    await expect(directResponseCard).toContainText(DIRECT_RESPONSE_SUMMARY);
    await assertNoAssistantMessage(page, reloadSnapshot, prompt);
    await expect.poll(
      () => eventSourceRequests.length > streamRequestCountBeforeReload,
      { timeout: 30_000 }
    ).toBe(true);

    const sameOriginBusinessBFF = apiRequests.length > 0
      && apiRequests.every((request) => request.origin === appOrigin);
    const toolboxFocusOnly = toolboxFocused
      && page.url() === `${appOrigin}${workspacePath}`
      && countMutatingAPIRequests(apiRequests) === mutatingRequestCountBeforeToolbox;
    const assertions = {
      chromium_browser: browserName === 'chromium',
      same_origin_business_bff: sameOriginBusinessBFF,
      quick_create_input_preserved: quickCreateRequest.postDataJSON().initial_prompt === prompt,
      workspace_snapshot_v2: initialSnapshot.schema_version === WORKSPACE_SCHEMA
        && terminalSnapshot.schema_version === WORKSPACE_SCHEMA
        && reloadSnapshot.schema_version === WORKSPACE_SCHEMA,
      workspace_sse_live: initialStreamResponse.status() === 200
        && reloadStreamResponse.status() === 200
        && hasStreamReady(eventSourceMessages, sessionID),
      direct_response_card_rendered: await directResponseCard.isVisible()
        && await directResponseCard.getAttribute('data-turn-id') === turnID,
      open_toolbox_focus_only: toolboxFocusOnly,
      reload_same_turn_run_input: reloadOutput.turn_id === turnID
        && reloadOutput.run_id === runID
        && reloadOutput.input_id === inputID,
      user_messages_only: reloadSnapshot.messages.every((message) => message.role === 'user'),
      no_assistant_message: !reloadSnapshot.messages.some((message) => message.role === 'assistant')
    };
    expect(Object.values(assertions).every((value) => value === true)).toBe(true);

    if (resultPath) {
      await writeAtomicJSON(resultPath, {
        schema_version: RESULT_SCHEMA,
        status: 'passed',
        produced_at: new Date().toISOString(),
        project_id: projectID,
        session_id: sessionID,
        input_id: inputID,
        turn_id: turnID,
        run_id: runID,
        terminal_delivery: terminalEvent ? 'sse' : 'snapshot',
        assertions
      });
    }
  });
});

function requireRuntimeInputs() {
  const missing = Object.entries({ email, password })
    .filter(([, value]) => !value)
    .map(([name]) => name);
  if (missing.length > 0) {
    throw new Error(`user-message-runtime smoke 缺少运行输入：${missing.join(', ')}`);
  }
  if (!prompt.trim()) throw new Error('user-message-runtime smoke 的 Prompt 不能为空');
}

function waitForJSONResponse(page, method, pathname) {
  return page.waitForResponse((response) => {
    const url = new URL(response.url());
    return response.request().method() === method && url.pathname === pathname;
  }, { timeout: 45_000 });
}

function waitForWorkspaceResponse(page, sessionID = '') {
  return page.waitForResponse((response) => {
    const url = new URL(response.url());
    return response.request().method() === 'GET'
      && url.pathname.startsWith('/api/v1/agent/sessions/')
      && url.pathname.endsWith('/workspace')
      && (!sessionID || url.pathname === `/api/v1/agent/sessions/${sessionID}/workspace`);
  }, { timeout: 45_000 });
}

function waitForStreamResponse(page, sessionID = '') {
  return page.waitForResponse((response) => {
    const url = new URL(response.url());
    return response.request().method() === 'GET'
      && url.pathname.startsWith('/api/v1/agent/sessions/')
      && url.pathname.endsWith('/events')
      && (!sessionID || url.pathname === `/api/v1/agent/sessions/${sessionID}/events`);
  }, { timeout: 45_000 });
}

function hasStreamReady(messages, sessionID) {
  return messages.some((message) => {
    if (message.eventName !== 'stream.ready') return false;
    try {
      const payload = JSON.parse(message.data);
      return payload.schema_version === 'workspace.stream-control.v1'
        && payload.event === 'stream.ready'
        && payload.session_id === sessionID;
    } catch {
      return false;
    }
  });
}

function findCompletedEvent(messages, { projectID, sessionID, inputID }) {
  for (const message of messages) {
    if (message.eventName !== 'session.turn.completed') continue;
    try {
      const envelope = JSON.parse(message.data);
      if (
        envelope.schema_version === 'workspace.event.v1'
        && envelope.event === 'session.turn.completed'
        && envelope.project_id === projectID
        && envelope.session_id === sessionID
        && envelope.payload?.input_id === inputID
      ) return envelope;
    } catch {
      // 忽略浏览器调试协议中不属于冻结 JSON Envelope 的帧。
    }
  }
  return null;
}

function assertCompletedSnapshot(snapshot, { projectID, sessionID, prompt: expectedPrompt }) {
  expect(snapshot.schema_version).toBe(WORKSPACE_SCHEMA);
  expect(snapshot.session?.project_id).toBe(projectID);
  expect(snapshot.session?.id).toBe(sessionID);
  expect(snapshot.messages).toHaveLength(1);
  expect(snapshot.messages[0]).toMatchObject({ role: 'user', content: expectedPrompt });
  expect(snapshot.inputs).toHaveLength(1);
  expect(snapshot.event_high_watermark).toBe(3);

  const output = snapshot.latest_turn_output;
  expect(Object.keys(output || {}).sort()).toEqual([
    'available_actions',
    'input_id',
    'message_code',
    'run_id',
    'schema_version',
    'status',
    'summary',
    'turn_id'
  ]);
  expect(output).toMatchObject({
    schema_version: DIRECT_RESPONSE_SCHEMA,
    status: 'completed',
    message_code: 'creation_request_received',
    summary: DIRECT_RESPONSE_SUMMARY,
    available_actions: ['open_toolbox']
  });
  expect(output.turn_id).toMatch(UUID_V7_PATTERN);
  expect(output.run_id).toMatch(UUID_V7_PATTERN);
  expect(output.input_id).toMatch(UUID_V7_PATTERN);
  expect(snapshot.inputs[0]).toMatchObject({
    id: output.input_id,
    message_id: snapshot.messages[0].id,
    source_type: 'user_message',
    status: 'resolved'
  });
  return output;
}

async function assertNoAssistantMessage(page, snapshot, expectedPrompt) {
  expect(snapshot.messages.every((message) => message.role === 'user')).toBe(true);
  expect(snapshot.messages.some((message) => message.role === 'assistant')).toBe(false);
  const messagesSection = page.locator('section[aria-labelledby="workspace-messages-title"]');
  await expect(messagesSection.locator('li')).toHaveCount(1);
  await expect(messagesSection.locator('li')).toHaveText(expectedPrompt);
  await expect(messagesSection).not.toContainText(DIRECT_RESPONSE_SUMMARY);
  await expect(page.getByText('Assistant Message', { exact: true })).toHaveCount(0);
}

function countMutatingAPIRequests(requests) {
  return requests.filter((request) => ['POST', 'PUT', 'PATCH', 'DELETE'].includes(request.method)).length;
}

async function sameOriginJSON(page, path) {
  return page.evaluate(async (requestPath) => {
    const response = await fetch(requestPath, {
      method: 'GET',
      credentials: 'include',
      headers: { Accept: 'application/json' }
    });
    const text = await response.text();
    return { status: response.status, payload: text ? JSON.parse(text) : null };
  }, path);
}

async function writeAtomicJSON(path, payload) {
  await mkdir(dirname(path), { recursive: true, mode: 0o700 });
  const temporaryPath = `${path}.${process.pid}.${randomUUID()}.tmp`;
  try {
    await writeFile(temporaryPath, `${JSON.stringify(payload)}\n`, {
      encoding: 'utf8',
      mode: 0o600,
      flag: 'wx'
    });
    await chmod(temporaryPath, 0o600);
    await rename(temporaryPath, path);
    await chmod(path, 0o600);
  } finally {
    await rm(temporaryPath, { force: true });
  }
}
