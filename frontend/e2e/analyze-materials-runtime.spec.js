import { expect, test } from '@playwright/test';
import { chmod, mkdir, rename, rm, writeFile } from 'node:fs/promises';
import { dirname } from 'node:path';

const enabled = process.env.DORA_E2E_ANALYZE_MATERIALS_RUNTIME === '1';
const email = process.env.DORA_E2E_USER_EMAIL || '';
const password = process.env.DORA_E2E_USER_PASSWORD || '';
const resultPath = process.env.DORA_E2E_ANALYZE_MATERIALS_RESULT_PATH || '';
const expected = Object.freeze({
  projectID: process.env.DORA_E2E_PROJECT_ID || '',
  sessionID: process.env.DORA_E2E_SESSION_ID || '',
  inputID: process.env.DORA_E2E_INPUT_ID || '',
  turnID: process.env.DORA_E2E_TURN_ID || '',
  runID: process.env.DORA_E2E_RUN_ID || '',
  toolCallID: process.env.DORA_E2E_TOOL_CALL_ID || ''
});

const UUID_V7_PATTERN = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
const RESULT_SCHEMA = 'analyze_materials_runtime.browser_result.v1';

test.describe('@analyze-materials-runtime read-only canonical browser', () => {
  test.skip(
    !enabled,
    '该用例只在 DORA_E2E_ANALYZE_MATERIALS_RUNTIME=1 时运行；默认测试不得伪造 canonical Evidence'
  );

  test('login -> existing Project Workspace -> completed Card -> unavailable Catalog -> reload', async ({
    page,
    browserName
  }) => {
    test.setTimeout(120_000);
    requireRuntimeInputs();

    const apiMutations = [];
    page.on('request', (request) => {
      const url = new URL(request.url());
      if (url.pathname.startsWith('/api/') && !['GET', 'HEAD', 'OPTIONS'].includes(request.method())) {
        apiMutations.push({ method: request.method(), pathname: url.pathname });
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
      await response.json();
      return { status: response.status };
    }, { loginEmail: email, loginPassword: password });
    expect(login.status).toBe(200);

    const workspacePath = `/projects/${expected.projectID}/workspace`;
    await page.goto(workspacePath);
    await expect(page).toHaveURL((url) => url.origin === appOrigin && url.pathname === workspacePath);

    const workspace = page.locator('main[data-workspace-state]');
    await expect(workspace).toHaveAttribute('data-workspace-state', 'ready', { timeout: 30_000 });
    await expect(workspace).toHaveAttribute('data-stream-state', 'live', { timeout: 30_000 });
    await expect(workspace).toHaveAttribute('data-project-id', expected.projectID);
    await expect(workspace).toHaveAttribute('data-session-id', expected.sessionID);

    const card = page.locator('article.turn-output-card[data-turn-output-status="completed"]');
    await expect(card).toBeVisible({ timeout: 30_000 });
    await expect(card).toHaveAttribute('data-turn-id', expected.turnID);
    await expect(card).toHaveAttribute('data-tool-call-id', expected.toolCallID);
    await expect(card.getByRole('heading', { name: '素材分析已完成' })).toBeVisible();
    await expect(card).toContainText('开发预览 · 非权威结果 · 不会写入素材分析资源');
    await expect(card).toContainText('结果码：MATERIAL_ANALYSIS_PREVIEW_COMPLETED');

    const analyzeCatalogItem = page.locator('[data-tool-key="analyze_materials"]');
    await expect(analyzeCatalogItem).toBeVisible();
    await expect(analyzeCatalogItem).toHaveAttribute('aria-disabled', 'true');
    await expect(analyzeCatalogItem).toHaveAttribute('data-tool-availability', 'unavailable');
    await expect(analyzeCatalogItem).toContainText('不可用');

    const snapshot = await sameOriginSnapshot(page, expected.sessionID);
    assertSnapshot(snapshot);
    const mutatingBeforeReload = [...apiMutations];

    await page.reload();
    await expect(workspace).toHaveAttribute('data-workspace-state', 'ready', { timeout: 30_000 });
    await expect(workspace).toHaveAttribute('data-stream-state', 'live', { timeout: 30_000 });
    await expect(card).toBeVisible();
    await expect(card).toHaveAttribute('data-turn-id', expected.turnID);
    await expect(analyzeCatalogItem).toHaveAttribute('data-tool-availability', 'unavailable');

    expect(mutatingBeforeReload).toEqual([{ method: 'POST', pathname: '/api/v1/auth/session' }]);
    expect(apiMutations).toEqual(mutatingBeforeReload);
    const assertions = {
      chromium_browser: browserName === 'chromium',
      existing_project_read_only: apiMutations.length === 1,
      workspace_snapshot_v2: snapshot.schema_version === 'session.workspace.v2',
      workspace_sse_live: await workspace.getAttribute('data-stream-state') === 'live',
      completed_card_visible: await card.isVisible(),
      stable_card_identity: await card.getAttribute('data-turn-id') === expected.turnID
        && await card.getAttribute('data-tool-call-id') === expected.toolCallID,
      analyze_materials_catalog_unavailable: await analyzeCatalogItem.getAttribute('data-tool-availability') === 'unavailable',
      hard_reload_recovered: await workspace.getAttribute('data-workspace-state') === 'ready'
    };
    expect(Object.values(assertions).every(Boolean)).toBe(true);
    await writeAtomicJSON(resultPath, {
      schema_version: RESULT_SCHEMA,
      status: 'passed',
      project_id: expected.projectID,
      session_id: expected.sessionID,
      input_id: expected.inputID,
      turn_id: expected.turnID,
      run_id: expected.runID,
      tool_call_id: expected.toolCallID,
      assertions
    });
  });
});

function requireRuntimeInputs() {
  expect(email).not.toBe('');
  expect(password).not.toBe('');
  expect(resultPath).not.toBe('');
  Object.values(expected).forEach((value) => expect(value).toMatch(UUID_V7_PATTERN));
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

function assertSnapshot(snapshot) {
  expect(snapshot.schema_version).toBe('session.workspace.v2');
  expect(snapshot.session?.id).toBe(expected.sessionID);
  expect(snapshot.session?.project_id).toBe(expected.projectID);
  expect(snapshot.messages).toEqual([]);
  expect(snapshot.inputs).toEqual(expect.arrayContaining([
    expect.objectContaining({ id: expected.inputID, message_id: null, source_type: 'analyze_materials_preview', status: 'resolved' })
  ]));
  expect(snapshot.analyze_materials_preview).toMatchObject({
    schema_version: 'analyze_materials.preview.card.v1',
    input_id: expected.inputID,
    turn_id: expected.turnID,
    run_id: expected.runID,
    tool_call_id: expected.toolCallID,
    status: 'completed',
    result_code: 'MATERIAL_ANALYSIS_PREVIEW_COMPLETED'
  });
  expect(snapshot.event_high_watermark).toBe(3);
}

async function writeAtomicJSON(path, payload) {
  const temporaryPath = `${path}.tmp`;
  await mkdir(dirname(path), { recursive: true, mode: 0o700 });
  await rm(temporaryPath, { force: true });
  await writeFile(temporaryPath, `${JSON.stringify(payload)}\n`, { encoding: 'utf8', mode: 0o600, flag: 'wx' });
  await chmod(temporaryPath, 0o600);
  await rename(temporaryPath, path);
  await chmod(path, 0o600);
}
