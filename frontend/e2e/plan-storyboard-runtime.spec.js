import { expect, test } from '@playwright/test';
import { chmod, mkdir, readFile, rename, rm, writeFile } from 'node:fs/promises';
import { dirname } from 'node:path';

const enabled = process.env.DORA_E2E_PLAN_STORYBOARD_RUNTIME === '1';
const email = process.env.DORA_E2E_USER_EMAIL || '';
const password = process.env.DORA_E2E_USER_PASSWORD || '';
const resultPath = process.env.DORA_E2E_PLAN_STORYBOARD_RESULT_PATH || '';
const controlDir = process.env.DORA_E2E_PLAN_STORYBOARD_CONTROL_DIR || '';
const planningInstruction = process.env.DORA_E2E_PLAN_STORYBOARD_INSTRUCTION || '';
const targetDurationSeconds = Number(process.env.DORA_E2E_PLAN_STORYBOARD_DURATION || '30');
const expected = Object.freeze({
  projectID: process.env.DORA_E2E_PROJECT_ID || '',
  sessionID: process.env.DORA_E2E_SESSION_ID || '',
  creationSpecID: process.env.DORA_E2E_CREATION_SPEC_ID || '',
  creationSpecVersion: Number(process.env.DORA_E2E_CREATION_SPEC_VERSION || '0'),
  creationSpecContentDigest: process.env.DORA_E2E_CREATION_SPEC_CONTENT_DIGEST || '',
  creationHighWatermark: Number(process.env.DORA_E2E_CREATION_HIGH_WATERMARK || '0')
});

const UUID_V7_PATTERN = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
const SHA256_PATTERN = /^[0-9a-f]{64}$/;
const RESULT_SCHEMA = 'plan_storyboard_runtime.browser_result.v1';
const RESTART_REQUEST_SCHEMA = 'plan_storyboard_runtime.restart_request.v1';
const DISCONNECT_SCHEMA = 'plan_storyboard_runtime.disconnect_observed.v1';
const RESTART_ACK_SCHEMA = 'plan_storyboard_runtime.restart_ack.v1';

test.describe('@plan-storyboard-runtime canonical browser', () => {
  test.skip(
    !enabled,
    '该用例只由 canonical plan-storyboard-runtime-v2-smoke 启动；skip 不会形成 passed Evidence'
  );

  test('existing CreationSpec -> Storyboard form -> SSE Card -> reload -> Agent reconnect', async ({
    page,
    browserName
  }) => {
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
        apiRequests.push({ method: request.method(), origin: url.origin, pathname: url.pathname, port: url.port });
      }
    });

    await page.goto('/');
    const appOrigin = new URL(page.url()).origin;
    expect(browserName).toBe('chromium');
    expect(await page.evaluate(() => navigator.userAgent)).toMatch(/(?:Headless)?Chrome\//);

    const login = await page.evaluate(async ({ loginEmail, loginPassword }) => {
      const response = await fetch('/api/v1/auth/session', {
        method: 'POST',
        credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ email: loginEmail, password: loginPassword })
      });
      return { status: response.status, payload: await response.json() };
    }, { loginEmail: email, loginPassword: password });
    expect(login.status).toBe(200);
    expect(String(login.payload?.principal?.id || '')).toMatch(UUID_V7_PATTERN);

    const workspacePath = `/projects/${expected.projectID}/workspace`;
    await page.goto(workspacePath);
    await expect(page).toHaveURL((url) => url.origin === appOrigin && url.pathname === workspacePath);
    const workspace = page.locator('main[data-workspace-state]');
    await expect(workspace).toHaveAttribute('data-workspace-state', 'ready', { timeout: 30_000 });
    await expect(workspace).toHaveAttribute('data-stream-state', 'live', { timeout: 30_000 });
    await expect(workspace).toHaveAttribute('data-project-id', expected.projectID);
    await expect(workspace).toHaveAttribute('data-session-id', expected.sessionID);

    const creationSpecCard = page.locator('article.creation-spec-card');
    await expect(creationSpecCard).toBeVisible();
    await expect(creationSpecCard).toHaveAttribute('data-creation-spec-id', expected.creationSpecID);
    await expect(creationSpecCard).toHaveAttribute(
      'data-creation-spec-version',
      String(expected.creationSpecVersion)
    );
    await expect(page.getByRole('heading', { name: 'Storyboard JSON Draft 开发预览' })).toBeVisible();
    await expect(page.getByRole('heading', { name: '故事板 JSON Draft' })).toBeVisible();

    const planCatalogItem = page.locator('[data-tool-key="plan_storyboard"]');
    await expect(planCatalogItem).toBeVisible();
    await expect(planCatalogItem).toHaveAttribute('aria-disabled', 'true');
    await expect(planCatalogItem).toHaveAttribute('data-tool-availability', 'unavailable');
    await expect(planCatalogItem).toContainText('不可用');

    const instructionField = page.getByLabel('故事板规划要求');
    const durationField = page.getByLabel('目标时长（秒，可选）');
    await instructionField.fill(planningInstruction);
    await durationField.fill(String(targetDurationSeconds));

    const enqueueResponsePromise = waitForJSONResponse(
      page,
      'POST',
      `/api/v1/agent/sessions/${expected.sessionID}/plan-storyboard-previews`
    );
    await page.getByRole('button', { name: '生成故事板开发预览' }).click();
    const enqueueResponse = await enqueueResponsePromise;
    expect(enqueueResponse.status()).toBe(202);
    const enqueueRequest = enqueueResponse.request();
    expect(enqueueRequest.headers()['idempotency-key']).toMatch(UUID_V7_PATTERN);
    expect(enqueueRequest.headers()['x-csrf-token']).toBeTruthy();
    expect(enqueueRequest.postDataJSON()).toEqual({
      schema_version: 'plan_storyboard.preview.enqueue-request.v1',
      creation_spec_ref: {
        id: expected.creationSpecID,
        version: expected.creationSpecVersion,
        content_digest: expected.creationSpecContentDigest
      },
      tool_intent: {
        schema_version: 'plan_storyboard.preview.intent.v1',
        planning_instruction: planningInstruction,
        target_duration_seconds: targetDurationSeconds
      }
    });

    const accepted = await enqueueResponse.json();
    expect(Object.keys(accepted).sort()).toEqual([
      'input_id', 'replayed', 'request_id', 'run_id', 'schema_version',
      'session_id', 'status', 'tool_call_id', 'turn_id'
    ]);
    expect(accepted).toMatchObject({
      schema_version: 'plan_storyboard.preview.enqueue.v1',
      session_id: expected.sessionID,
      status: 'pending',
      replayed: false
    });
    for (const field of ['request_id', 'input_id', 'turn_id', 'run_id', 'tool_call_id']) {
      expect(String(accepted[field] || '')).toMatch(UUID_V7_PATTERN);
    }
    await expect(page.getByText(/请求已受理，正在等待 Storyboard JSON Draft/)).toBeVisible();

    const storyboardCard = page.locator('article.storyboard-preview-card[data-storyboard-preview-status="completed"]');
    await expect(storyboardCard).toBeVisible({ timeout: 60_000 });
    const storyboardPreviewID = String(await storyboardCard.getAttribute('data-storyboard-preview-id') || '');
    expect(storyboardPreviewID).toMatch(UUID_V7_PATTERN);
    await expect(storyboardCard).toHaveAttribute('data-storyboard-preview-version', '1');
    await expect(storyboardCard).toContainText('开发预览 · 隔离 JSON Draft · 未激活/未扣费');
    await expect(storyboardCard).toContainText('故事板章节');
    await expect(storyboardCard).toContainText('规划元素');
    await expect(storyboardCard).toContainText('结果码：STORYBOARD_PREVIEW_DRAFT_CREATED');

    await expect.poll(() => hasStoryboardEvent(
      eventSourceMessages,
      'plan_storyboard.preview.accepted',
      accepted.input_id,
      accepted.turn_id,
      accepted.run_id,
      accepted.tool_call_id
    ), { timeout: 60_000 }).toBe(true);
    await expect.poll(() => hasStoryboardEvent(
      eventSourceMessages,
      'plan_storyboard.preview.completed',
      accepted.input_id,
      accepted.turn_id,
      accepted.run_id,
      accepted.tool_call_id,
      storyboardPreviewID
    ), { timeout: 60_000 }).toBe(true);

    const reloadSnapshotPromise = waitForWorkspaceResponse(page, expected.sessionID);
    const reloadStreamPromise = waitForStreamResponse(page, expected.sessionID);
    await page.reload();
    const reloadSnapshotResponse = await reloadSnapshotPromise;
    expect(reloadSnapshotResponse.status()).toBe(200);
    const reloadSnapshot = await reloadSnapshotResponse.json();
    expect((await reloadStreamPromise).status()).toBe(200);
    assertStoryboardSnapshot(reloadSnapshot, accepted, storyboardPreviewID);
    await expect(workspace).toHaveAttribute('data-workspace-state', 'ready', { timeout: 30_000 });
    await expect(workspace).toHaveAttribute('data-stream-state', 'live', { timeout: 30_000 });
    await expect(storyboardCard).toHaveAttribute('data-storyboard-preview-id', storyboardPreviewID);

    const eventSourceCountBeforeRestart = eventSourceRequests.length;
    await writeControlJSON('agent-restart-request.json', {
      schema_version: RESTART_REQUEST_SCHEMA,
      project_id: expected.projectID,
      session_id: expected.sessionID,
      input_id: accepted.input_id,
      turn_id: accepted.turn_id,
      run_id: accepted.run_id,
      tool_call_id: accepted.tool_call_id,
      storyboard_preview_id: storyboardPreviewID
    });
    await expect(workspace).toHaveAttribute('data-stream-state', 'reconnecting', { timeout: 60_000 });
    await expect(storyboardCard).toHaveAttribute('data-storyboard-preview-id', storyboardPreviewID);
    await writeControlJSON('agent-disconnect-observed.json', {
      schema_version: DISCONNECT_SCHEMA,
      session_id: expected.sessionID,
      storyboard_preview_id: storyboardPreviewID,
      stream_state: 'reconnecting'
    });
    const restartAck = await waitForControlJSON('agent-restart-ack.json');
    expect(restartAck).toEqual({
      schema_version: RESTART_ACK_SCHEMA,
      session_id: expected.sessionID,
      storyboard_preview_id: storyboardPreviewID,
      agent_ready: true
    });
    await recoverWorkspaceAfterAgentRestart(page, workspace);
    await expect(workspace).toHaveAttribute('data-stream-state', 'live', { timeout: 60_000 });
    await expect(storyboardCard).toHaveAttribute('data-storyboard-preview-id', storyboardPreviewID);
    await expect.poll(() => eventSourceRequests.length > eventSourceCountBeforeRestart, {
      timeout: 60_000
    }).toBe(true);

    const recoveredSnapshot = await sameOriginSnapshot(page, expected.sessionID);
    assertStoryboardSnapshot(recoveredSnapshot, accepted, storyboardPreviewID);
    const sameOriginBusinessBFF = apiRequests.length > 0
      && apiRequests.every((request) => request.origin === appOrigin);
    const assertions = {
      chromium_browser: browserName === 'chromium',
      existing_creation_spec_bound: reloadSnapshot.creation_spec_preview?.creation_spec_id === expected.creationSpecID,
      same_origin_business_bff: sameOriginBusinessBFF,
      storyboard_form_submitted: enqueueResponse.status() === 202,
      accepted_sse_observed: hasStoryboardEvent(
        eventSourceMessages,
        'plan_storyboard.preview.accepted',
        accepted.input_id,
        accepted.turn_id,
        accepted.run_id,
        accepted.tool_call_id
      ),
      terminal_sse_observed: hasStoryboardEvent(
        eventSourceMessages,
        'plan_storyboard.preview.completed',
        accepted.input_id,
        accepted.turn_id,
        accepted.run_id,
        accepted.tool_call_id,
        storyboardPreviewID
      ),
      storyboard_card_visible: await storyboardCard.isVisible(),
      hard_reload_recovered: reloadSnapshot.plan_storyboard_preview?.storyboard_preview_id === storyboardPreviewID,
      agent_disconnect_observed: true,
      agent_reconnect_recovered: recoveredSnapshot.plan_storyboard_preview?.storyboard_preview_id === storyboardPreviewID,
      static_catalog_unavailable: await planCatalogItem.getAttribute('data-tool-availability') === 'unavailable'
    };
    expect(Object.values(assertions).every(Boolean)).toBe(true);
    await writeAtomicJSON(resultPath, {
      schema_version: RESULT_SCHEMA,
      status: 'passed',
      project_id: expected.projectID,
      session_id: expected.sessionID,
      creation_spec_id: expected.creationSpecID,
      creation_spec_version: expected.creationSpecVersion,
      creation_spec_content_digest: expected.creationSpecContentDigest,
      input_id: accepted.input_id,
      request_id: accepted.request_id,
      turn_id: accepted.turn_id,
      run_id: accepted.run_id,
      tool_call_id: accepted.tool_call_id,
      storyboard_preview_id: storyboardPreviewID,
      storyboard_content_digest: String(recoveredSnapshot.plan_storyboard_preview.content_digest),
      event_high_watermark: recoveredSnapshot.event_high_watermark,
      assertions
    });
  });
});

function requireRuntimeInputs() {
  const required = { email, password, resultPath, controlDir, planningInstruction };
  const missing = Object.entries(required).filter(([, value]) => !value).map(([name]) => name);
  if (missing.length > 0) throw new Error(`plan-storyboard-runtime smoke 缺少运行输入：${missing.join(', ')}`);
  for (const value of [expected.projectID, expected.sessionID, expected.creationSpecID]) {
    expect(value).toMatch(UUID_V7_PATTERN);
  }
  expect(expected.creationSpecVersion).toBe(1);
  expect(expected.creationSpecContentDigest).toMatch(SHA256_PATTERN);
  expect(expected.creationHighWatermark).toBeGreaterThanOrEqual(1);
  expect(targetDurationSeconds).toBeGreaterThanOrEqual(5);
  expect(targetDurationSeconds).toBeLessThanOrEqual(600);
}

function waitForJSONResponse(page, method, pathname) {
  return page.waitForResponse((response) => {
    const url = new URL(response.url());
    return response.request().method() === method && url.pathname === pathname;
  }, { timeout: 60_000 });
}

function waitForWorkspaceResponse(page, sessionID) {
  return waitForJSONResponse(page, 'GET', `/api/v1/agent/sessions/${sessionID}/workspace`);
}

function waitForStreamResponse(page, sessionID) {
  return waitForJSONResponse(page, 'GET', `/api/v1/agent/sessions/${sessionID}/events`);
}

function hasStoryboardEvent(messages, eventName, inputID, turnID, runID, toolCallID, storyboardPreviewID = '') {
  return messages.some((message) => {
    if (message.eventName !== eventName) return false;
    try {
      const envelope = JSON.parse(message.data);
      if (envelope.schema_version !== 'workspace.event.v1' || envelope.event !== eventName
        || envelope.project_id !== expected.projectID || envelope.session_id !== expected.sessionID
        || envelope.aggregate_type !== 'plan_storyboard_preview' || envelope.aggregate_id !== inputID
        || envelope.payload?.input_id !== inputID || envelope.payload?.turn_id !== turnID
        || envelope.payload?.run_id !== runID || envelope.payload?.tool_call_id !== toolCallID) return false;
      if (eventName === 'plan_storyboard.preview.accepted') {
        return envelope.payload?.schema_version === 'plan_storyboard.preview.accepted.v1';
      }
      return envelope.payload?.schema_version === 'storyboard.preview.card.v1'
        && envelope.payload?.status === 'completed'
        && envelope.payload?.storyboard_preview_id === storyboardPreviewID;
    } catch {
      return false;
    }
  });
}

function assertStoryboardSnapshot(snapshot, accepted, storyboardPreviewID) {
  expect(snapshot.schema_version).toBe('session.workspace.v3');
  expect(snapshot.session?.id).toBe(expected.sessionID);
  expect(snapshot.session?.project_id).toBe(expected.projectID);
  expect(snapshot.creation_spec_preview).toMatchObject({
    creation_spec_id: expected.creationSpecID,
    version: expected.creationSpecVersion,
    content_digest: expected.creationSpecContentDigest,
    status: 'draft'
  });
  expect(snapshot.inputs).toEqual(expect.arrayContaining([
    expect.objectContaining({
      id: accepted.input_id,
      message_id: null,
      source_type: 'plan_storyboard_preview',
      status: 'resolved'
    })
  ]));
  expect(snapshot.plan_storyboard_preview).toMatchObject({
    schema_version: 'storyboard.preview.card.v1',
    input_id: accepted.input_id,
    turn_id: accepted.turn_id,
    run_id: accepted.run_id,
    tool_call_id: accepted.tool_call_id,
    status: 'completed',
    result_code: 'STORYBOARD_PREVIEW_DRAFT_CREATED',
    storyboard_preview_id: storyboardPreviewID,
    project_id: expected.projectID,
    creation_spec_ref: {
      id: expected.creationSpecID,
      version: expected.creationSpecVersion,
      content_digest: expected.creationSpecContentDigest
    },
    version: 1
  });
  expect(snapshot.plan_storyboard_preview.content_digest).toMatch(SHA256_PATTERN);
  expect(snapshot.event_high_watermark).toBe(expected.creationHighWatermark + 2);
}

async function sameOriginSnapshot(page, sessionID) {
  const result = await page.evaluate(async (id) => {
    const response = await fetch(`/api/v1/agent/sessions/${id}/workspace`, {
      method: 'GET', credentials: 'same-origin', headers: { Accept: 'application/json' }
    });
    return { status: response.status, payload: await response.json() };
  }, sessionID);
  expect(result.status).toBe(200);
  return result.payload;
}

async function recoverWorkspaceAfterAgentRestart(page, workspace) {
  const reconnect = page.getByRole('button', { name: '重新连接工作台' });
  for (let attempt = 0; attempt < 240; attempt += 1) {
    if (await workspace.getAttribute('data-workspace-state') === 'ready') return;
    if (await reconnect.isVisible()) await reconnect.click();
    await page.waitForTimeout(250);
  }
  throw new Error('Agent 重启后工作台未恢复 ready');
}

async function writeControlJSON(name, payload) {
  await writeAtomicJSON(`${controlDir}/${name}`, payload);
}

async function waitForControlJSON(name, timeout = 90_000) {
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
  throw new Error(`等待 plan-storyboard-runtime 控制文件超时：${name}`);
}

async function writeAtomicJSON(path, payload) {
  const temporaryPath = `${path}.${process.pid}.tmp`;
  await mkdir(dirname(path), { recursive: true, mode: 0o700 });
  await rm(temporaryPath, { force: true });
  await writeFile(temporaryPath, `${JSON.stringify(payload)}\n`, { encoding: 'utf8', mode: 0o600, flag: 'wx' });
  await chmod(temporaryPath, 0o600);
  await rename(temporaryPath, path);
  await chmod(path, 0o600);
}
