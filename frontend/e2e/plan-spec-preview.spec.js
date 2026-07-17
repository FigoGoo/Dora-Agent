import { expect, test } from '@playwright/test';
import { randomBytes } from 'node:crypto';
import { chmod, readFile, rename, writeFile } from 'node:fs/promises';

const enabled = process.env.DORA_E2E_PLAN_SPEC_PREVIEW === '1';
const email = process.env.DORA_E2E_USER_EMAIL || '';
const password = process.env.DORA_E2E_USER_PASSWORD || '';
const previewGoal = process.env.DORA_E2E_PREVIEW_GOAL || '';
const legacyGoal = process.env.DORA_E2E_LEGACY_GOAL || '';
const resultPath = process.env.DORA_E2E_PLAN_SPEC_RESULT_PATH || '';
const controlDir = process.env.DORA_E2E_PLAN_SPEC_CONTROL_DIR || '';

const UUID_V7_PATTERN = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
const RESULT_SCHEMA = 'plan_spec_preview.browser_result.v1';
const POSITIVE_CHECKPOINT_SCHEMA = 'plan_spec_preview.positive_checkpoint.v1';
const POSITIVE_ACK_SCHEMA = 'plan_spec_preview.positive_checkpoint_ack.v1';
const BLOCKED_CHECKPOINT_SCHEMA = 'plan_spec_preview.blocked_checkpoint.v1';
const BLOCKED_ACK_SCHEMA = 'plan_spec_preview.blocked_checkpoint_ack.v1';

test.describe('@plan-spec-preview real browser vertical slice', () => {
  test.skip(
    !enabled,
    '该用例只由 canonical plan-spec-preview-smoke 启动；canonical 脚本还会严格校验结果文件，skip 不会形成 passed Evidence'
  );

  test('QuickCreate null lane -> BFF 202 -> SSE Card -> replay/recovery -> blocked legacy lane', async ({ page, browserName }) => {
    test.setTimeout(240_000);
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
        apiRequests.push({ origin: url.origin, pathname: url.pathname, port: url.port });
      }
    });

    await page.goto('/');
    const appOrigin = new URL(page.url()).origin;
    expect(browserName).toBe('chromium');
    expect(await page.evaluate(() => navigator.userAgent)).toMatch(/(?:Headless)?Chrome\//);

    await page.getByPlaceholder('由一个想法或故事开始...').fill(previewGoal);
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
    const loginPayload = await loginResponse.json();
    const creatorUserID = String(loginPayload.principal?.id || '');
    const csrfToken = String(loginPayload.csrf_token || '');
    expect(creatorUserID).toMatch(UUID_V7_PATTERN);
    expect(csrfToken).not.toBe('');

    const quickCreateResponse = await quickCreateResponsePromise;
    expect(quickCreateResponse.status()).toBe(201);
    const quickCreateRequest = quickCreateResponse.request();
    expect(quickCreateRequest.postDataJSON()).toEqual({ initial_prompt: null });
    expect(quickCreateRequest.headers()['idempotency-key']).toBeTruthy();
    expect(quickCreateRequest.headers()['x-csrf-token']).toBeTruthy();
    const quickCreatePayload = await quickCreateResponse.json();
    const projectID = String(quickCreatePayload.project_id || '');
    expect(projectID).toMatch(UUID_V7_PATTERN);

    const workspacePath = `/projects/${projectID}/workspace`;
    await expect(page).toHaveURL((url) => url.pathname === workspacePath);
    const initialSnapshotResponse = await initialSnapshotPromise;
    expect(initialSnapshotResponse.status()).toBe(200);
    const initialSnapshot = await initialSnapshotResponse.json();
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
    expect(initialSnapshot.session?.project_id).toBe(projectID);
    expect(initialSnapshot.messages).toEqual([]);
    expect(initialSnapshot.inputs).toEqual([]);
    expect(initialSnapshot.creation_spec_preview).toBeNull();
    expect(initialSnapshot.event_high_watermark).toBe(1);

    const goalField = page.getByRole('textbox', { name: '创作目标', exact: true });
    await expect(page.getByRole('heading', { name: 'Creation Spec 开发预览' })).toBeVisible();
    await expect(goalField).toHaveValue(previewGoal);
    await page.getByLabel('目标受众（可选）').fill('本地试跑受众');
    await page.getByLabel('约束（每行一项，最多 8 项）').fill('时长 30 秒\n保持品牌一致');

    const previewResponsePromise = waitForPreviewResponse(page, sessionID);
    await page.getByRole('button', { name: '生成开发预览' }).click();
    const previewResponse = await previewResponsePromise;
    expect(previewResponse.status()).toBe(202);
    const previewRequest = previewResponse.request();
    const previewRequestHeaders = previewRequest.headers();
    const idempotencyKey = String(previewRequestHeaders['idempotency-key'] || '');
    expect(idempotencyKey).toMatch(UUID_V7_PATTERN);
    expect(previewRequestHeaders['x-csrf-token']).toBeTruthy();
    expect(previewRequest.postDataJSON()).toEqual({
      schema_version: 'plan_creation_spec.preview.intent.v1',
      goal: previewGoal,
      deliverable_type: 'video',
      audience: '本地试跑受众',
      locale: 'zh-CN',
      constraints: ['时长 30 秒', '保持品牌一致']
    });
    const previewAccepted = await previewResponse.json();
    expect(Object.keys(previewAccepted).sort()).toEqual([
      'input_id', 'request_id', 'schema_version', 'session_id', 'status'
    ]);
    expect(previewAccepted).toMatchObject({
      schema_version: 'plan_creation_spec.preview.enqueue.v1',
      session_id: sessionID,
      status: 'pending'
    });
    const inputID = String(previewAccepted.input_id || '');
    const requestID = String(previewAccepted.request_id || '');
    expect(inputID).toMatch(UUID_V7_PATTERN);
    expect(requestID).toMatch(UUID_V7_PATTERN);
    await expect(page.getByText(/请求已受理，正在等待 Creation Spec Draft/)).toBeVisible();

    const card = page.locator('article.creation-spec-card');
    await expect(card).toBeVisible({ timeout: 45_000 });
    const creationSpecID = String(await card.getAttribute('data-creation-spec-id') || '');
    const creationSpecVersion = Number(await card.getAttribute('data-creation-spec-version'));
    expect(creationSpecID).toMatch(UUID_V7_PATTERN);
    expect(creationSpecVersion).toBe(1);
    await expect(card.getByRole('heading', { name: '视频创作规格' })).toBeVisible();
    await expect(card).toContainText(previewGoal);
    await expect(card).toContainText('本地试跑受众');
    await expect(card).toContainText('时长 30 秒');
    await expect(card).toContainText('保持品牌一致');
    await expect(card).toContainText('创作规划');
    await expect(card).toContainText('交付结果符合已冻结目标、类型和全部硬约束');
    await expect.poll(() => hasCompletedEvent(eventSourceMessages, {
      projectID, sessionID, inputID, creationSpecID
    }), { timeout: 45_000 }).toBe(true);

    await writeControlJSON('positive-before-replay.json', {
      schema_version: POSITIVE_CHECKPOINT_SCHEMA,
      project_id: projectID,
      session_id: sessionID,
      input_id: inputID,
      creation_spec_id: creationSpecID
    });
    const positiveAck = await waitForControlJSON('positive-replay-ack.json');
    expect(positiveAck).toEqual({
      schema_version: POSITIVE_ACK_SCHEMA,
      project_id: projectID,
      session_id: sessionID,
      input_id: inputID,
      creation_spec_id: creationSpecID,
      authority_captured: true
    });

    const replayResponsePromise = waitForPreviewResponse(page, sessionID);
    await page.getByRole('button', { name: '再次确认受理状态' }).click();
    const replayResponse = await replayResponsePromise;
    expect(replayResponse.status()).toBe(202);
    expect(replayResponse.request().headers()['idempotency-key']).toBe(idempotencyKey);
    const replayPayload = await replayResponse.json();
    expect(replayPayload).toEqual({ ...previewAccepted, request_id: replayPayload.request_id });
    expect(replayPayload.input_id).toBe(inputID);
    expect(replayPayload.session_id).toBe(sessionID);
    expect(replayPayload.status).toBe('pending');
    expect(String(replayPayload.request_id || '')).toMatch(UUID_V7_PATTERN);

    let disconnectRecovered = false;
    const eventSourceCountBeforeDisconnect = eventSourceRequests.length;
    await page.context().setOffline(true);
    try {
      await expect(workspace).toHaveAttribute('data-stream-state', 'reconnecting', { timeout: 45_000 });
      await expect(card).toHaveAttribute('data-creation-spec-id', creationSpecID);
    } finally {
      await page.context().setOffline(false);
    }
    await expect(workspace).toHaveAttribute('data-workspace-state', 'ready', { timeout: 30_000 });
    await expect(workspace).toHaveAttribute('data-stream-state', 'live', { timeout: 30_000 });
    await expect(workspace).toHaveAttribute('data-session-id', sessionID);
    await expect(card).toHaveAttribute('data-creation-spec-id', creationSpecID);
    await expect.poll(() => eventSourceRequests.length > eventSourceCountBeforeDisconnect, {
      timeout: 30_000
    }).toBe(true);
    disconnectRecovered = true;

    const reloadSnapshotPromise = waitForWorkspaceResponse(page, sessionID);
    const reloadStreamPromise = waitForStreamResponse(page, sessionID);
    await page.reload();
    const reloadSnapshotResponse = await reloadSnapshotPromise;
    expect(reloadSnapshotResponse.status()).toBe(200);
    const reloadSnapshot = await reloadSnapshotResponse.json();
    const reloadStreamResponse = await reloadStreamPromise;
    expect(reloadStreamResponse.status()).toBe(200);
    await expect(workspace).toHaveAttribute('data-workspace-state', 'ready', { timeout: 30_000 });
    await expect(workspace).toHaveAttribute('data-stream-state', 'live', { timeout: 30_000 });
    await expect(card).toHaveAttribute('data-creation-spec-id', creationSpecID);
    await expect(card).toHaveAttribute('data-creation-spec-version', String(creationSpecVersion));
    expect(reloadSnapshot.creation_spec_preview?.creation_spec_id).toBe(creationSpecID);
    expect(reloadSnapshot.creation_spec_preview?.content_digest).toMatch(/^[0-9a-f]{64}$/);
    expect(reloadSnapshot.creation_spec_preview?.goal).toBe(previewGoal);
    await expect(goalField).toHaveValue('');

    const legacyCreate = await sameOriginJSON(page, '/api/v1/projects:quick-create', {
      method: 'POST',
      csrfToken,
      idempotencyKey: createFixtureUUIDv7(),
      body: { initial_prompt: legacyGoal }
    });
    expect(legacyCreate.status).toBe(201);
    const legacyProjectID = String(legacyCreate.payload?.project_id || '');
    expect(legacyProjectID).toMatch(UUID_V7_PATTERN);
    const legacyBootstrap = await waitForBootstrap(page, legacyProjectID);
    expect(legacyBootstrap.creation_status).toBe('ready');
    expect(legacyBootstrap.initial_prompt_status).toBe('accepted');
    const legacySessionID = String(legacyBootstrap.session_id || '');
    const legacyInputID = String(legacyBootstrap.input_id || '');
    expect(legacySessionID).toMatch(UUID_V7_PATTERN);
    expect(legacyInputID).toMatch(UUID_V7_PATTERN);
    const legacySnapshotBeforeResponse = await sameOriginJSON(
      page,
      `/api/v1/agent/sessions/${legacySessionID}/workspace`
    );
    expect(legacySnapshotBeforeResponse.status).toBe(200);
    const legacySnapshotBefore = safeWorkspaceFact(legacySnapshotBeforeResponse.payload);
    expect(legacySnapshotBefore).toEqual({
      session_id: legacySessionID,
      message_ids: [String(legacySnapshotBeforeResponse.payload.messages[0].id)],
      input_ids: [legacyInputID],
      input_statuses: ['pending'],
      event_high_watermark: 2,
      min_available_seq: 1,
      creation_spec_id: null
    });

    await writeControlJSON('blocked-before-post.json', {
      schema_version: BLOCKED_CHECKPOINT_SCHEMA,
      project_id: legacyProjectID,
      session_id: legacySessionID,
      input_id: legacyInputID
    });
    const blockedAck = await waitForControlJSON('blocked-post-ack.json');
    expect(blockedAck).toEqual({
      schema_version: BLOCKED_ACK_SCHEMA,
      project_id: legacyProjectID,
      session_id: legacySessionID,
      input_id: legacyInputID,
      authority_captured: true
    });

    const blockedResponse = await sameOriginJSON(
      page,
      `/api/v1/agent/sessions/${legacySessionID}/creation-spec-previews`,
      {
        method: 'POST',
        csrfToken,
        idempotencyKey: createFixtureUUIDv7(),
        body: {
          schema_version: 'plan_creation_spec.preview.intent.v1',
          goal: `${legacyGoal} 的结构化预览`,
          deliverable_type: 'video',
          locale: 'zh-CN',
          constraints: []
        }
      }
    );
    expect(blockedResponse.status).toBe(409);
    expect(Object.keys(blockedResponse.payload || {})).toEqual(['error']);
    expect(blockedResponse.payload.error).toMatchObject({
      code: 'SESSION_LANE_BLOCKED',
      retryable: false,
      details: {}
    });
    const blockedRequestID = String(blockedResponse.payload.error.request_id || '');
    expect(blockedRequestID).toMatch(UUID_V7_PATTERN);
    const legacySnapshotAfterResponse = await sameOriginJSON(
      page,
      `/api/v1/agent/sessions/${legacySessionID}/workspace`
    );
    expect(legacySnapshotAfterResponse.status).toBe(200);
    expect(safeWorkspaceFact(legacySnapshotAfterResponse.payload)).toEqual(legacySnapshotBefore);

    const sameOriginBusinessBFF = apiRequests.length > 0
      && apiRequests.every((request) => request.origin === appOrigin && request.port !== '18082');
    const assertions = {
      chromium_browser: browserName === 'chromium',
      same_origin_business_bff: sameOriginBusinessBFF,
      quick_create_initial_prompt_null: quickCreateRequest.postDataJSON().initial_prompt === null,
      empty_lane_bootstrap_ready: initialSnapshot.messages.length === 0
        && initialSnapshot.inputs.length === 0
        && initialSnapshot.event_high_watermark === 1,
      preview_goal_handoff: (await card.textContent()).includes(previewGoal),
      preview_enqueue_202: previewResponse.status() === 202 && previewAccepted.status === 'pending',
      preview_sse_completed: hasCompletedEvent(eventSourceMessages, { projectID, sessionID, inputID, creationSpecID }),
      creation_spec_card_rendered: await card.isVisible()
        && await card.getAttribute('data-creation-spec-id') === creationSpecID,
      idempotent_replay_same_input: replayResponse.status() === 202
        && replayResponse.request().headers()['idempotency-key'] === idempotencyKey
        && replayPayload.input_id === inputID,
      sse_disconnect_recovered: disconnectRecovered,
      hard_refresh_snapshot_recovered: reloadSnapshot.creation_spec_preview?.creation_spec_id === creationSpecID,
      handoff_not_persisted_after_refresh: await goalField.inputValue() === '',
      legacy_quick_create_nonempty: legacyBootstrap.initial_prompt_status === 'accepted'
        && legacySnapshotBefore.input_ids.length === 1,
      legacy_session_lane_blocked_409: blockedResponse.status === 409
        && blockedResponse.payload.error.code === 'SESSION_LANE_BLOCKED',
      legacy_workspace_zero_delta: JSON.stringify(safeWorkspaceFact(legacySnapshotAfterResponse.payload))
        === JSON.stringify(legacySnapshotBefore)
    };
    expect(Object.values(assertions).every((value) => value === true)).toBe(true);

    await writeAtomicJSON(resultPath, {
      schema_version: RESULT_SCHEMA,
      creator_user_id: creatorUserID,
      project_id: projectID,
      session_id: sessionID,
      input_id: inputID,
      request_id: requestID,
      creation_spec_id: creationSpecID,
      creation_spec_version: creationSpecVersion,
      content_digest: String(reloadSnapshot.creation_spec_preview.content_digest),
      legacy_project_id: legacyProjectID,
      legacy_session_id: legacySessionID,
      legacy_input_id: legacyInputID,
      blocked_request_id: blockedRequestID,
      assertions
    });
  });
});

function requireRuntimeInputs() {
  const missing = Object.entries({ email, password, previewGoal, legacyGoal, resultPath, controlDir })
    .filter(([, value]) => !value)
    .map(([name]) => name);
  if (missing.length > 0) throw new Error(`plan-spec-preview smoke 缺少运行输入：${missing.join(', ')}`);
}

function waitForJSONResponse(page, method, pathname) {
  return page.waitForResponse((response) => {
    const url = new URL(response.url());
    return response.request().method() === method && url.pathname === pathname;
  }, { timeout: 45_000 });
}

function waitForPreviewResponse(page, sessionID) {
  return waitForJSONResponse(page, 'POST', `/api/v1/agent/sessions/${sessionID}/creation-spec-previews`);
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

function hasCompletedEvent(messages, { projectID, sessionID, inputID, creationSpecID }) {
  return messages.some((message) => {
    if (message.eventName !== 'creation_spec.preview.completed') return false;
    try {
      const payload = JSON.parse(message.data);
      return payload.schema_version === 'workspace.event.v1'
        && payload.event === 'creation_spec.preview.completed'
        && payload.project_id === projectID
        && payload.session_id === sessionID
        && payload.aggregate_id === creationSpecID
        && payload.payload?.creation_spec_id === creationSpecID
        && payload.payload?.project_id === projectID
        && payload.payload?.status === 'draft'
        && payload.payload?.creation_spec_id !== inputID;
    } catch {
      return false;
    }
  });
}

async function sameOriginJSON(page, path, { method = 'GET', csrfToken = '', idempotencyKey = '', body } = {}) {
  return page.evaluate(async ({ requestPath, requestMethod, csrf, idempotency, requestBody }) => {
    const headers = { Accept: 'application/json' };
    if (requestBody !== undefined) headers['Content-Type'] = 'application/json';
    if (csrf) headers['X-CSRF-Token'] = csrf;
    if (idempotency) headers['Idempotency-Key'] = idempotency;
    const response = await fetch(requestPath, {
      method: requestMethod,
      credentials: 'include',
      headers,
      body: requestBody === undefined ? undefined : JSON.stringify(requestBody)
    });
    const text = await response.text();
    return { status: response.status, payload: text ? JSON.parse(text) : null };
  }, {
    requestPath: path,
    requestMethod: method,
    csrf: csrfToken,
    idempotency: idempotencyKey,
    requestBody: body
  });
}

async function waitForBootstrap(page, projectID) {
  const deadline = Date.now() + 45_000;
  let last = null;
  while (Date.now() < deadline) {
    const response = await sameOriginJSON(page, `/api/v1/projects/${projectID}/bootstrap`);
    last = response;
    if (response.status === 200 && response.payload?.creation_status === 'ready') return response.payload;
    if (response.status >= 400 && response.status !== 503) {
      throw new Error(`legacy Project Bootstrap 返回不可恢复状态 ${response.status}`);
    }
    await new Promise((resolve) => setTimeout(resolve, 100));
  }
  throw new Error(`legacy Project Bootstrap 未进入 ready，最后状态 ${last?.status || 0}`);
}

function safeWorkspaceFact(snapshot) {
  return {
    session_id: String(snapshot?.session?.id || ''),
    message_ids: Array.isArray(snapshot?.messages) ? snapshot.messages.map((message) => String(message.id)) : [],
    input_ids: Array.isArray(snapshot?.inputs) ? snapshot.inputs.map((input) => String(input.id)) : [],
    input_statuses: Array.isArray(snapshot?.inputs) ? snapshot.inputs.map((input) => String(input.status)) : [],
    event_high_watermark: Number(snapshot?.event_high_watermark),
    min_available_seq: Number(snapshot?.min_available_seq),
    creation_spec_id: snapshot?.creation_spec_preview?.creation_spec_id || null
  };
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
  await writeAtomicJSON(`${controlDir}/${name}`, payload);
}

async function waitForControlJSON(name, timeout = 60_000) {
  const path = `${controlDir}/${name}`;
  const deadline = Date.now() + timeout;
  while (Date.now() < deadline) {
    try {
      return JSON.parse(await readFile(path, 'utf8'));
    } catch (error) {
      if (error?.code !== 'ENOENT' && !(error instanceof SyntaxError)) throw error;
    }
    await new Promise((resolve) => setTimeout(resolve, 50));
  }
  throw new Error(`等待 plan-spec-preview 控制文件超时：${name}`);
}

async function writeAtomicJSON(path, payload) {
  const temporaryPath = `${path}.${process.pid}.tmp`;
  await writeFile(temporaryPath, `${JSON.stringify(payload)}\n`, { encoding: 'utf8', mode: 0o600 });
  await chmod(temporaryPath, 0o600);
  await rename(temporaryPath, path);
  await chmod(path, 0o600);
}
