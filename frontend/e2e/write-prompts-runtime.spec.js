import { expect, test } from '@playwright/test';
import { chmod, mkdir, readFile, rename, rm, writeFile } from 'node:fs/promises';
import { dirname } from 'node:path';

const enabled = process.env.DORA_E2E_WRITE_PROMPTS_RUNTIME === '1';
const email = process.env.DORA_E2E_USER_EMAIL || '';
const password = process.env.DORA_E2E_USER_PASSWORD || '';
const resultPath = process.env.DORA_E2E_WRITE_PROMPTS_RESULT_PATH || '';
const controlDir = process.env.DORA_E2E_WRITE_PROMPTS_CONTROL_DIR || '';
const writingInstruction = process.env.DORA_E2E_WRITE_PROMPTS_INSTRUCTION || '';
const outputLanguage = process.env.DORA_E2E_WRITE_PROMPTS_OUTPUT_LANGUAGE || 'zh-CN';
const expected = Object.freeze({
  projectID: process.env.DORA_E2E_PROJECT_ID || '',
  sessionID: process.env.DORA_E2E_SESSION_ID || '',
  storyboardPreviewID: process.env.DORA_E2E_STORYBOARD_PREVIEW_ID || '',
  storyboardPreviewVersion: Number(process.env.DORA_E2E_STORYBOARD_PREVIEW_VERSION || '0'),
  storyboardContentDigest: process.env.DORA_E2E_STORYBOARD_CONTENT_DIGEST || '',
  sourceHighWatermark: Number(process.env.DORA_E2E_SOURCE_HIGH_WATERMARK || '0')
});

const UUID_V7_PATTERN = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
const SHA256_PATTERN = /^[0-9a-f]{64}$/;
const RESULT_SCHEMA = 'write_prompts_runtime.browser_result.v1';
const RESTART_REQUEST_SCHEMA = 'write_prompts_runtime.restart_request.v1';
const DISCONNECT_SCHEMA = 'write_prompts_runtime.disconnect_observed.v1';
const RESTART_ACK_SCHEMA = 'write_prompts_runtime.restart_ack.v1';

test.describe('@write-prompts-runtime canonical browser', () => {
  test.skip(!enabled, '该用例只由 canonical write-prompts-runtime-v2-smoke 启动；skip 不形成 passed Evidence');

  test('authoritative Storyboard -> Prompt form -> SSE Card -> reload -> Agent reconnect', async ({
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
        apiRequests.push({ method: request.method(), origin: url.origin, pathname: url.pathname });
      }
    });

    await page.goto('/');
    const appOrigin = new URL(page.url()).origin;
    expect(browserName).toBe('chromium');
    expect(await page.evaluate(() => navigator.userAgent)).toMatch(/(?:Headless)?Chrome\//);

    const login = await page.evaluate(async ({ loginEmail, loginPassword }) => {
      const response = await fetch('/api/v1/auth/session', {
        method: 'POST', credentials: 'same-origin', headers: { 'Content-Type': 'application/json' },
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

    const initialSnapshot = await sameOriginSnapshot(page, expected.sessionID);
    const source = initialSnapshot.plan_storyboard_preview;
    expect(initialSnapshot.schema_version).toBe('session.workspace.v4');
    expect(source).toMatchObject({
      schema_version: 'storyboard.preview.card.v1', status: 'completed',
      storyboard_preview_id: expected.storyboardPreviewID, version: expected.storyboardPreviewVersion,
      content_digest: expected.storyboardContentDigest
    });
    expect(source.slots.length).toBeGreaterThan(0);
    expect(initialSnapshot.write_prompts_preview).toBeNull();
    const targetCount = source.slots.length;

    const storyboardCard = page.locator('article.storyboard-preview-card[data-storyboard-preview-status="completed"]');
    await expect(storyboardCard).toHaveAttribute('data-storyboard-preview-id', expected.storyboardPreviewID);
    await expect(page.getByRole('heading', { name: 'Prompt JSON Draft 开发预览' })).toBeVisible();

    const catalogItem = page.locator('[data-tool-key="write_prompts"]');
    await expect(catalogItem).toBeVisible();
    await expect(catalogItem).toHaveAttribute('aria-disabled', 'true');
    await expect(catalogItem).toHaveAttribute('data-tool-availability', 'unavailable');
    await expect(catalogItem).toContainText('不可用');

    await page.getByLabel('提示词写作要求').fill(writingInstruction);
    await page.getByLabel('输出语言（可选）').selectOption(outputLanguage);
    const enqueueResponsePromise = waitForJSONResponse(
      page, 'POST', `/api/v1/agent/sessions/${expected.sessionID}/write-prompts-previews`
    );
    await page.getByRole('button', { name: '生成提示词开发预览' }).click();
    const enqueueResponse = await enqueueResponsePromise;
    expect(enqueueResponse.status()).toBe(202);
    const enqueueRequest = enqueueResponse.request();
    expect(enqueueRequest.headers()['idempotency-key']).toMatch(UUID_V7_PATTERN);
    expect(enqueueRequest.headers()['x-csrf-token']).toBeTruthy();
    expect(enqueueRequest.postDataJSON()).toEqual({
      schema_version: 'write_prompts.preview.enqueue-request.v1',
      storyboard_preview_ref: {
        id: expected.storyboardPreviewID, version: expected.storyboardPreviewVersion,
        content_digest: expected.storyboardContentDigest
      },
      tool_intent: {
        schema_version: 'write_prompts.preview.intent.v1',
        writing_instruction: writingInstruction,
        output_language: outputLanguage
      }
    });

    const accepted = await enqueueResponse.json();
    expect(Object.keys(accepted).sort()).toEqual([
      'input_id', 'replayed', 'request_id', 'run_id', 'schema_version',
      'session_id', 'status', 'tool_call_id', 'turn_id'
    ]);
    expect(accepted).toMatchObject({
      schema_version: 'write_prompts.preview.enqueue.v1', session_id: expected.sessionID,
      status: 'pending', replayed: false
    });
    for (const field of ['request_id', 'input_id', 'turn_id', 'run_id', 'tool_call_id']) {
      expect(String(accepted[field] || '')).toMatch(UUID_V7_PATTERN);
    }
    await expect(page.getByText(/请求已受理，正在等待 Prompt JSON Draft/)).toBeVisible();

    const promptCard = page.locator('article.prompt-preview-card[data-prompt-preview-status="completed"]');
    await expect(promptCard).toBeVisible({ timeout: 60_000 });
    const promptPreviewID = String(await promptCard.getAttribute('data-prompt-preview-id') || '');
    expect(promptPreviewID).toMatch(UUID_V7_PATTERN);
    await expect(promptCard).toHaveAttribute('data-prompt-preview-version', '1');
    await expect(promptCard).toContainText('未审核/未扣费/不可生成媒体');
    await expect(promptCard).toContainText(`目标槽位${targetCount}`);
    await expect(promptCard).toContainText('结果码：PROMPT_PREVIEW_DRAFT_CREATED');

    await expect.poll(() => hasPromptEvent(
      eventSourceMessages, 'write_prompts.preview.accepted', accepted, ''
    ), { timeout: 60_000 }).toBe(true);
    await expect.poll(() => hasPromptEvent(
      eventSourceMessages, 'write_prompts.preview.completed', accepted, promptPreviewID
    ), { timeout: 60_000 }).toBe(true);

    const reloadSnapshotPromise = waitForWorkspaceResponse(page, expected.sessionID);
    const reloadStreamPromise = waitForStreamResponse(page, expected.sessionID);
    await page.reload();
    const reloadSnapshotResponse = await reloadSnapshotPromise;
    expect(reloadSnapshotResponse.status()).toBe(200);
    const reloadSnapshot = await reloadSnapshotResponse.json();
    expect((await reloadStreamPromise).status()).toBe(200);
    assertPromptSnapshot(reloadSnapshot, accepted, promptPreviewID, targetCount);
    await expect(workspace).toHaveAttribute('data-workspace-state', 'ready', { timeout: 30_000 });
    await expect(workspace).toHaveAttribute('data-stream-state', 'live', { timeout: 30_000 });
    await expect(promptCard).toHaveAttribute('data-prompt-preview-id', promptPreviewID);

    const eventSourceCountBeforeRestart = eventSourceRequests.length;
    await writeControlJSON('agent-restart-request.json', {
      schema_version: RESTART_REQUEST_SCHEMA,
      project_id: expected.projectID, session_id: expected.sessionID,
      input_id: accepted.input_id, turn_id: accepted.turn_id, run_id: accepted.run_id,
      tool_call_id: accepted.tool_call_id, prompt_preview_id: promptPreviewID
    });
    await expect(workspace).toHaveAttribute('data-stream-state', 'reconnecting', { timeout: 60_000 });
    await expect(promptCard).toHaveAttribute('data-prompt-preview-id', promptPreviewID);
    await writeControlJSON('agent-disconnect-observed.json', {
      schema_version: DISCONNECT_SCHEMA, session_id: expected.sessionID,
      prompt_preview_id: promptPreviewID, stream_state: 'reconnecting'
    });
    const restartAck = await waitForControlJSON('agent-restart-ack.json');
    expect(restartAck).toEqual({
      schema_version: RESTART_ACK_SCHEMA, session_id: expected.sessionID,
      prompt_preview_id: promptPreviewID, agent_ready: true
    });
    await recoverWorkspaceAfterAgentRestart(page, workspace);
    await expect(workspace).toHaveAttribute('data-stream-state', 'live', { timeout: 60_000 });
    await expect(promptCard).toHaveAttribute('data-prompt-preview-id', promptPreviewID);
    await expect.poll(() => eventSourceRequests.length > eventSourceCountBeforeRestart, { timeout: 60_000 }).toBe(true);

    const recoveredSnapshot = await sameOriginSnapshot(page, expected.sessionID);
    assertPromptSnapshot(recoveredSnapshot, accepted, promptPreviewID, targetCount);
    const assertions = {
      chromium_browser: browserName === 'chromium',
      authoritative_storyboard_bound: recoveredSnapshot.write_prompts_preview.storyboard_preview_ref.id === expected.storyboardPreviewID,
      full_exact_target_set: recoveredSnapshot.write_prompts_preview.target_count === targetCount,
      same_origin_business_bff: apiRequests.length > 0 && apiRequests.every((request) => request.origin === appOrigin),
      write_prompts_form_submitted: enqueueResponse.status() === 202,
      accepted_sse_observed: hasPromptEvent(eventSourceMessages, 'write_prompts.preview.accepted', accepted, ''),
      terminal_sse_observed: hasPromptEvent(eventSourceMessages, 'write_prompts.preview.completed', accepted, promptPreviewID),
      prompt_card_visible: await promptCard.isVisible(),
      hard_reload_recovered: reloadSnapshot.write_prompts_preview?.prompt_preview_id === promptPreviewID,
      agent_disconnect_observed: true,
      agent_reconnect_recovered: recoveredSnapshot.write_prompts_preview?.prompt_preview_id === promptPreviewID,
      static_catalog_unavailable: await catalogItem.getAttribute('data-tool-availability') === 'unavailable'
    };
    expect(Object.values(assertions).every(Boolean)).toBe(true);
    await writeAtomicJSON(resultPath, {
      schema_version: RESULT_SCHEMA, status: 'passed', project_id: expected.projectID,
      session_id: expected.sessionID, storyboard_preview_id: expected.storyboardPreviewID,
      storyboard_content_digest: expected.storyboardContentDigest, target_count: targetCount,
      input_id: accepted.input_id, request_id: accepted.request_id, turn_id: accepted.turn_id,
      run_id: accepted.run_id, tool_call_id: accepted.tool_call_id,
      prompt_preview_id: promptPreviewID,
      prompt_content_digest: String(recoveredSnapshot.write_prompts_preview.content_digest),
      event_high_watermark: recoveredSnapshot.event_high_watermark,
      assertions
    });
  });
});

function requireRuntimeInputs() {
  const required = { email, password, resultPath, controlDir, writingInstruction };
  const missing = Object.entries(required).filter(([, value]) => !value).map(([name]) => name);
  if (missing.length > 0) throw new Error(`write-prompts-runtime smoke 缺少运行输入：${missing.join(', ')}`);
  for (const value of [expected.projectID, expected.sessionID, expected.storyboardPreviewID]) expect(value).toMatch(UUID_V7_PATTERN);
  expect(expected.storyboardPreviewVersion).toBe(1);
  expect(expected.storyboardContentDigest).toMatch(SHA256_PATTERN);
  expect(expected.sourceHighWatermark).toBeGreaterThanOrEqual(1);
  expect(['zh-CN', 'en-US']).toContain(outputLanguage);
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

function hasPromptEvent(messages, eventName, accepted, promptPreviewID) {
  return messages.some((message) => {
    if (message.eventName !== eventName) return false;
    try {
      const envelope = JSON.parse(message.data);
      if (envelope.schema_version !== 'workspace.event.v1' || envelope.event !== eventName
        || envelope.project_id !== expected.projectID || envelope.session_id !== expected.sessionID
        || envelope.aggregate_type !== 'write_prompts_preview' || envelope.aggregate_id !== accepted.input_id
        || envelope.payload?.input_id !== accepted.input_id || envelope.payload?.turn_id !== accepted.turn_id
        || envelope.payload?.run_id !== accepted.run_id || envelope.payload?.tool_call_id !== accepted.tool_call_id) return false;
      if (eventName === 'write_prompts.preview.accepted') {
        return envelope.payload?.schema_version === 'write_prompts.preview.accepted.v1'
          && envelope.payload?.storyboard_preview_id === expected.storyboardPreviewID;
      }
      return envelope.payload?.schema_version === 'prompt.preview.card.v1'
        && envelope.payload?.status === 'completed'
        && envelope.payload?.prompt_preview_id === promptPreviewID;
    } catch {
      return false;
    }
  });
}

function assertPromptSnapshot(snapshot, accepted, promptPreviewID, targetCount) {
  expect(snapshot.schema_version).toBe('session.workspace.v4');
  expect(snapshot.session?.id).toBe(expected.sessionID);
  expect(snapshot.session?.project_id).toBe(expected.projectID);
  expect(snapshot.plan_storyboard_preview).toMatchObject({
    storyboard_preview_id: expected.storyboardPreviewID, version: 1,
    content_digest: expected.storyboardContentDigest, status: 'completed'
  });
  expect(snapshot.inputs).toEqual(expect.arrayContaining([expect.objectContaining({
    id: accepted.input_id, message_id: null, source_type: 'write_prompts_preview', status: 'resolved'
  })]));
  expect(snapshot.write_prompts_preview).toMatchObject({
    schema_version: 'prompt.preview.card.v1', input_id: accepted.input_id,
    turn_id: accepted.turn_id, run_id: accepted.run_id, tool_call_id: accepted.tool_call_id,
    status: 'completed', result_code: 'PROMPT_PREVIEW_DRAFT_CREATED',
    prompt_preview_id: promptPreviewID, project_id: expected.projectID,
    storyboard_preview_ref: { id: expected.storyboardPreviewID, version: 1, content_digest: expected.storyboardContentDigest },
    version: 1, target_count: targetCount
  });
  expect(snapshot.write_prompts_preview.content_digest).toMatch(SHA256_PATTERN);
  expect(snapshot.write_prompts_preview.prompts).toHaveLength(targetCount);
  expect(snapshot.event_high_watermark).toBe(expected.sourceHighWatermark + 2);
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
  throw new Error(`等待 write-prompts-runtime 控制文件超时：${name}`);
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
