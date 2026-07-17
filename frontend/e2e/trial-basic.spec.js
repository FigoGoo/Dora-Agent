import { expect, test } from '@playwright/test';
import { chmod, mkdir, rename, rm, writeFile } from 'node:fs/promises';
import { dirname } from 'node:path';

const enabled = process.env.DORA_E2E_TRIAL_BASIC === '1';
const email = process.env.DORA_E2E_USER_EMAIL || '';
const password = process.env.DORA_E2E_USER_PASSWORD || '';
const resultPath = process.env.DORA_E2E_TRIAL_BASIC_RESULT_PATH || '';
const UUID_V7 = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;

test.describe('@trial-basic unified six-tool browser chain', () => {
  test.skip(!enabled, '只由 make trial-basic 启动；默认 skip 不形成验收证据');

  test('login -> project -> six tools -> PNG -> playable MP4 -> Range -> reload', async ({ page, browserName }) => {
    test.setTimeout(360_000);
    expect(email).not.toBe('');
    expect(password).not.toBe('');
    expect(resultPath).not.toBe('');

    const sseEvents = [];
    const cdp = await page.context().newCDPSession(page);
    await cdp.send('Network.enable');
    cdp.on('Network.eventSourceMessageReceived', (message) => sseEvents.push(message));

    const initialPrompt = `Dora 基本功能一键验收 ${Date.now()}`;
    await page.goto('/');
    const appOrigin = new URL(page.url()).origin;
    await page.getByPlaceholder('由一个想法或故事开始...').fill(initialPrompt);
    await page.getByRole('button', { name: '开始创作' }).click();
    const login = page.getByRole('dialog', { name: '登录后继续创作' });
    await login.getByRole('textbox', { name: '邮箱' }).fill(email);
    await login.getByLabel('密码').fill(password);
    const quickCreateResponse = waitForJSONResponse(page, 'POST', '/api/v1/projects:quick-create');
    await login.getByRole('button', { name: '登录并继续' }).click();
    const quickCreate = await readSuccessfulJSON(await quickCreateResponse, 201);
    const projectID = String(quickCreate.project_id || '');
    expect(projectID).toMatch(UUID_V7);

    const workspace = page.locator('main[data-workspace-state]');
    await expect(workspace).toHaveAttribute('data-workspace-state', 'ready', { timeout: 45_000 });
    await expect(workspace).toHaveAttribute('data-stream-state', 'live', { timeout: 45_000 });
    const sessionID = String(await workspace.getAttribute('data-session-id') || '');
    expect(sessionID).toMatch(UUID_V7);
    await expect(page.getByRole('heading', { name: '需求已接收' })).toBeVisible({ timeout: 60_000 });

    await expect(page.getByLabel('创作目标')).toHaveValue(initialPrompt);
    await page.getByLabel('目标受众（可选）').fill('本地 MVP 验收用户');
    const creationResponse = waitForJSONResponse(
      page, 'POST', `/api/v1/agent/sessions/${sessionID}/creation-spec-previews`
    );
    await page.getByRole('button', { name: '生成开发预览' }).click();
    const creationReceipt = await readSuccessfulJSON(await creationResponse, 202);
    const creationCard = page.locator('article.creation-spec-card');
    await expect(creationCard).toBeVisible({ timeout: 60_000 });
    const creationSpecID = String(await creationCard.getAttribute('data-creation-spec-id') || '');
    expect(creationSpecID).toMatch(UUID_V7);

    const materialText = '新品是一款轻量智能相机，核心卖点是快速启动、稳定画质与便携设计。';
    await page.getByLabel('文本素材正文').fill(materialText);
    const materialResponse = waitForJSONResponse(page, 'POST', `/api/v1/projects/${projectID}/text-materials`);
    await page.getByRole('button', { name: '保存文本素材' }).click();
    const materialReceipt = await readSuccessfulJSON(await materialResponse, 201);
    await expect(page.getByText('文本素材已保存并可选择。')).toBeVisible();
    await expect(page.getByText('1/8 条已选择')).toBeVisible();

    await page.getByLabel('分析目标').fill('提取核心卖点、叙事元素与风险。');
    const analyzeResponse = waitForJSONResponse(
      page, 'POST', `/api/v1/agent/sessions/${sessionID}/analyze-materials-previews`
    );
    await page.getByRole('button', { name: '提交素材分析' }).click();
    const analyzeReceipt = await readSuccessfulJSON(await analyzeResponse, 202);
    await expect(page.getByRole('heading', { name: '素材分析已完成' })).toBeVisible({ timeout: 60_000 });

    await page.getByLabel('故事板规划要求').fill('用开场主视觉、产品演示与行动号召组织 30 秒短片。');
    await page.getByLabel('目标时长（秒，可选）').fill('30');
    const storyboardResponse = waitForJSONResponse(
      page, 'POST', `/api/v1/agent/sessions/${sessionID}/plan-storyboard-previews`
    );
    await page.getByRole('button', { name: '生成故事板开发预览' }).click();
    const storyboardReceipt = await readSuccessfulJSON(await storyboardResponse, 202);
    const storyboardCard = page.locator('article.storyboard-preview-card[data-storyboard-preview-status="completed"]');
    await expect(storyboardCard).toBeVisible({ timeout: 60_000 });

    await page.getByLabel('提示词写作要求').fill('为每个槽位生成简洁、可直接执行的中文商业视觉提示词。');
    const promptsResponse = waitForJSONResponse(
      page, 'POST', `/api/v1/agent/sessions/${sessionID}/write-prompts-previews`
    );
    await page.getByRole('button', { name: '生成提示词开发预览' }).click();
    const promptsReceipt = await readSuccessfulJSON(await promptsResponse, 202);
    const promptCard = page.locator('article.prompt-preview-card[data-prompt-preview-status="completed"]');
    await expect(promptCard).toBeVisible({ timeout: 60_000 });

    const generateResponse = waitForJSONResponse(
      page, 'POST', `/api/v1/agent/sessions/${sessionID}/generate-media-previews`
    );
    await page.getByRole('button', { name: '生成测试 PNG' }).click();
    const generateReceipt = await readSuccessfulJSON(await generateResponse, 202);
    const pngCard = page.locator('article.media-preview-card[data-media-preview-status="completed"]')
      .filter({ has: page.locator('img') });
    await expect(pngCard).toBeVisible({ timeout: 90_000 });
    const pngAssetID = String(await pngCard.getAttribute('data-media-preview-asset-id') || '');
    expect(pngAssetID).toMatch(UUID_V7);

    const assembleResponse = waitForJSONResponse(
      page, 'POST', `/api/v1/agent/sessions/${sessionID}/assemble-output-previews`
    );
    await page.getByRole('button', { name: '装配测试 MP4' }).click();
    const assembleReceipt = await readSuccessfulJSON(await assembleResponse, 202);
    const mp4Card = page.locator('article.media-preview-card[data-media-preview-status="completed"]')
      .filter({ has: page.locator('video') });
    await expect(mp4Card).toBeVisible({ timeout: 120_000 });
    const mp4AssetID = String(await mp4Card.getAttribute('data-media-preview-asset-id') || '');
    expect(mp4AssetID).toMatch(UUID_V7);
    const mp4URL = String(await mp4Card.locator('video').getAttribute('src') || '');
    expect(mp4URL).toBe(`/api/v1/projects/${projectID}/media-preview-assets/${mp4AssetID}/content`);

    const contentChecks = await page.evaluate(async (url) => {
      const head = await fetch(url, { method: 'HEAD', credentials: 'same-origin' });
      const partial = await fetch(url, {
        credentials: 'same-origin', headers: { Range: 'bytes=0-15' }
      });
      const invalid = await fetch(url, {
        credentials: 'same-origin', headers: { Range: 'bytes=999999999-' }
      });
      return {
        headStatus: head.status,
        contentType: head.headers.get('content-type'),
        acceptRanges: head.headers.get('accept-ranges'),
        partialStatus: partial.status,
        contentRange: partial.headers.get('content-range'),
        partialBytes: (await partial.arrayBuffer()).byteLength,
        invalidStatus: invalid.status
      };
    }, mp4URL);
    expect(contentChecks).toMatchObject({
      headStatus: 200,
      contentType: 'video/mp4',
      acceptRanges: 'bytes',
      partialStatus: 206,
      partialBytes: 16,
      invalidStatus: 416
    });
    expect(contentChecks.contentRange).toMatch(/^bytes 0-15\/\d+$/);

    const terminalSnapshot = await sameOriginSnapshot(page, sessionID);
    assertTerminalSnapshot(terminalSnapshot, { projectID, sessionID, pngAssetID, mp4AssetID });
    await page.reload();
    await expect(workspace).toHaveAttribute('data-workspace-state', 'ready', { timeout: 45_000 });
    await expect(workspace).toHaveAttribute('data-stream-state', 'live', { timeout: 45_000 });
    await expect(pngCard).toBeVisible();
    await expect(mp4Card).toBeVisible();
    const recoveredSnapshot = await sameOriginSnapshot(page, sessionID);
    assertTerminalSnapshot(recoveredSnapshot, { projectID, sessionID, pngAssetID, mp4AssetID });

    const toolReceipts = {
      plan_creation_spec: creationReceipt,
      analyze_materials: analyzeReceipt,
      plan_storyboard: storyboardReceipt,
      write_prompts: promptsReceipt,
      generate_media: generateReceipt,
      assemble_output: assembleReceipt
    };
    const terminalCards = recoveredSnapshot.media_previews.filter((card) => card.status === 'completed');
    const mediaResults = terminalCards
      .map((card) => ({
        tool_key: card.tool_key,
        operation_id: card.operation_id,
        batch_id: card.batch_id,
        job_id: card.job_id,
        asset_id: card.asset_ref.id,
        content_digest: card.asset_ref.content_digest,
        size_bytes: card.asset_ref.size_bytes,
        mime_type: card.asset_ref.mime_type
      }))
      .sort((left, right) => left.tool_key.localeCompare(right.tool_key));
    const assertions = {
      chromium_browser: browserName === 'chromium',
      same_origin_bff: new URL(page.url()).origin === appOrigin,
      initial_user_message_completed: recoveredSnapshot.latest_turn_output?.status === 'completed',
      six_tool_receipts: Object.keys(toolReceipts).length === 6,
      text_material_persisted: materialReceipt.material?.asset_id === analyzeReceipt.input_id
        || Boolean(materialReceipt.material?.asset_id),
      png_decodable_in_browser: await pngCard.locator('img').evaluate((image) => image.complete && image.naturalWidth === 640 && image.naturalHeight === 360),
      mp4_video_element_ready: await mp4Card.locator('video').evaluate((video) => video.readyState >= 1),
      range_200_206_416: contentChecks.headStatus === 200 && contentChecks.partialStatus === 206 && contentChecks.invalidStatus === 416,
      snapshot_v5_recovered: recoveredSnapshot.schema_version === 'session.workspace.v5' && terminalCards.length === 2,
      two_terminal_media_jobs: mediaResults.length === 2 && mediaResults.every((result) =>
        UUID_V7.test(result.operation_id) && UUID_V7.test(result.batch_id) && UUID_V7.test(result.job_id) &&
        /^[0-9a-f]{64}$/.test(result.content_digest) && Number.isSafeInteger(result.size_bytes) && result.size_bytes > 0),
      zero_external_media_fields: !/(provider|price|billing|approval|tos|secret|object_key)/i.test(JSON.stringify(recoveredSnapshot.media_previews))
    };
    expect(Object.values(assertions).every(Boolean)).toBe(true);
    await writeAtomicJSON(resultPath, {
      schema_version: 'trial_basic.browser_result.v1',
      status: 'passed',
      produced_at: new Date().toISOString(),
      project_id: projectID,
      session_id: sessionID,
      tool_receipts: toolReceipts,
      asset_ids: { png: pngAssetID, mp4: mp4AssetID },
      media_results: mediaResults,
      content_checks: contentChecks,
      observed_media_sse_events: sseEvents
        .map((message) => message.eventName)
        .filter((name) => name.startsWith('media.preview.')),
      assertions
    });
  });
});

function waitForJSONResponse(page, method, pathname) {
  return page.waitForResponse((response) => {
    const url = new URL(response.url());
    return response.request().method() === method && url.pathname === pathname;
  }, { timeout: 60_000 });
}

async function readSuccessfulJSON(response, expectedStatus) {
  expect(response.status()).toBe(expectedStatus);
  return response.json();
}

async function sameOriginSnapshot(page, sessionID) {
  const result = await page.evaluate(async (id) => {
    const response = await fetch(`/api/v1/agent/sessions/${id}/workspace`, {
      credentials: 'same-origin', headers: { Accept: 'application/json' }
    });
    return { status: response.status, payload: await response.json() };
  }, sessionID);
  expect(result.status).toBe(200);
  return result.payload;
}

function assertTerminalSnapshot(snapshot, { projectID, sessionID, pngAssetID, mp4AssetID }) {
  expect(snapshot.schema_version).toBe('session.workspace.v5');
  expect(snapshot.session).toMatchObject({ id: sessionID, project_id: projectID });
  expect(snapshot.messages).toEqual(expect.arrayContaining([expect.objectContaining({ role: 'user' })]));
  const completed = snapshot.media_previews.filter((card) => card.status === 'completed');
  expect(completed).toHaveLength(2);
  expect(completed).toEqual(expect.arrayContaining([
    expect.objectContaining({ tool_key: 'generate_media', asset_ref: expect.objectContaining({ id: pngAssetID, status: 'ready' }) }),
    expect.objectContaining({ tool_key: 'assemble_output', asset_ref: expect.objectContaining({ id: mp4AssetID, status: 'ready' }) })
  ]));
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
