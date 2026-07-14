import { expect, test } from '@playwright/test';
import { writeFile } from 'node:fs/promises';
import { ownerSkillFixture, SKILL_IDS } from '../src/test/skillFixtures.js';

const email = process.env.DORA_E2E_USER_EMAIL || '';
const password = process.env.DORA_E2E_USER_PASSWORD || '';
const reviewerEmail = process.env.DORA_E2E_REVIEWER_EMAIL || '';
const reviewerPassword = process.env.DORA_E2E_REVIEWER_PASSWORD || '';
const w1ResultPath = process.env.DORA_E2E_W1_RESULT_PATH || '';

const capabilityLabels = [
  '流程规划',
  '素材分析',
  '故事板设计',
  '媒体生成',
  '提示词写法',
  '视频剪辑'
];

function isSkillResponse(response, method, pathname) {
  const url = new URL(response.url());
  return response.request().method() === method && url.pathname === pathname;
}

function isStrictOwnerNotFoundResponse(result, pathname) {
  const responseURL = new URL(result.responseURL);
  const error = result.payload?.error;
  return responseURL.pathname === pathname
    && responseURL.search === ''
    && result.status === 404
    && result.contentType === 'application/json; charset=utf-8'
    && result.cacheControl === 'no-store'
    && Object.keys(result.payload || {}).join(',') === 'error'
    && Object.keys(error || {}).sort().join(',') === 'code,details,message,request_id,retryable'
    && error?.code === 'SKILL_NOT_FOUND'
    && error?.message === 'Skill 不存在或不可访问'
    && error?.retryable === false
    && error?.details !== null
    && typeof error?.details === 'object'
    && !Array.isArray(error?.details)
    && Object.keys(error.details).length === 0
    && /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(error?.request_id || '');
}

async function ownerResponseObservation(response) {
  const responseText = await response.text();
  let payload = null;
  try {
    payload = JSON.parse(responseText);
  } catch {
    // Callers fail closed when the payload is not strict JSON.
  }
  return {
    status: response.status(),
    contentType: response.headers()['content-type'] || '',
    cacheControl: response.headers()['cache-control'] || '',
    responseURL: response.url(),
    responseText,
    payload
  };
}

test.describe('W1 QuickCreate Skill binding browser contract', () => {
  test('@w1-skill selects an available Skill and sends only the explicit v2 variant', async ({ page }) => {
    const draftSkillID = '019f0000-0000-7000-8000-000000000124';
    const published = ownerSkillFixture({
      content_status: 'published',
      has_unpublished_changes: false,
      allowed_actions: ['edit_draft']
    });
    const draft = ownerSkillFixture({
      skill_id: draftSkillID,
      definition: { ...ownerSkillFixture().definition, name: '浏览器草稿 Skill' }
    });
    let quickCreateBody = null;

    await page.route('**/api/v1/**', async (route) => {
      const request = route.request();
      const url = new URL(request.url());
      if (url.pathname === '/api/v1/auth/session' && request.method() === 'GET') {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            status: 'authenticated',
            principal: {
              id: 'w1-browser-user',
              display_name: 'W1 Browser User',
              email: 'w1***@example.com',
              account_status: 'active',
              roles: ['user'],
              capabilities: []
            },
            csrf_token: 'csrf-w1-browser',
            session_expires_at: '2026-07-15T08:00:00Z'
          })
        });
        return;
      }
      if (url.pathname === '/api/v1/skills' && request.method() === 'GET') {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ items: [published, draft], next_cursor: null, request_id: SKILL_IDS.request })
        });
        return;
      }
      if (url.pathname === '/api/v1/projects:quick-create' && request.method() === 'POST') {
        quickCreateBody = request.postDataJSON();
        await route.fulfill({
          status: 201,
          contentType: 'application/json',
          body: JSON.stringify({
            project_id: '019f0000-0000-7000-8000-000000000141',
            session_id: null,
            creation_status: 'provisioning'
          })
        });
        return;
      }
      await route.fulfill({
        status: 404,
        contentType: 'application/json',
        body: JSON.stringify({ error: { code: 'NOT_FOUND', message: 'not found', retryable: false } })
      });
    });

    await page.goto('/');
    await expect(page.getByRole('button', { name: '用户菜单' })).toBeVisible();
    await page.getByRole('button', { name: 'Skill', exact: true }).click();
    const picker = page.getByRole('dialog', { name: '选择 QuickCreate Skill' });
    await expect(picker.getByRole('checkbox', { name: '选择 剧情短片 Skill' })).toBeEnabled();
    await expect(picker.getByRole('checkbox', { name: '选择 浏览器草稿 Skill' })).toBeDisabled();
    await picker.getByRole('checkbox', { name: '选择 剧情短片 Skill' }).check();
    await page.getByPlaceholder('由一个想法或故事开始...').fill('W1 Skill Binding 浏览器契约');
    const requestPromise = page.waitForRequest((request) => (
      request.method() === 'POST' && new URL(request.url()).pathname === '/api/v1/projects:quick-create'
    ));
    await page.getByRole('button', { name: '开始创作' }).click();
    const quickCreateRequest = await requestPromise;

    expect(quickCreateRequest.headers()['idempotency-key']).toBeTruthy();
    expect(quickCreateRequest.headers()['x-csrf-token']).toBe('csrf-w1-browser');
    expect(quickCreateBody).toEqual({
      schema_version: 'project_quick_create.v2',
      initial_prompt: 'W1 Skill Binding 浏览器契约',
      enabled_skill_ids: [SKILL_IDS.skill]
    });
  });
});

test.describe('W1 real Skill Foundation browser smoke', () => {
  test.skip(
    !email || !password,
    '需要通过 DORA_E2E_USER_EMAIL 和 DORA_E2E_USER_PASSWORD 提供真实冒烟账号'
  );

  test('@w1-skill login -> create -> edit -> submit review', async ({ page }) => {
    test.setTimeout(90_000);

    const runSuffix = `${Date.now()}`;
    const skillName = `W1 浏览器 Skill ${runSuffix}`;
    const updatedSkillName = `W1 浏览器 Skill 已编辑 ${runSuffix}`;
    const skillRequests = [];
    page.on('request', (request) => {
      const url = new URL(request.url());
      if (url.pathname.startsWith('/api/v1/skills')) {
        skillRequests.push({ method: request.method(), origin: url.origin, pathname: url.pathname });
      }
    });

    await page.goto('/');
    const appOrigin = new URL(page.url()).origin;
    await page.getByRole('button', { name: '登录' }).click();
    const loginDialog = page.getByRole('dialog', { name: '登录后继续创作' });
    await loginDialog.getByRole('textbox', { name: '邮箱' }).fill(email);
    await loginDialog.getByLabel('密码').fill(password);
    const loginResponsePromise = page.waitForResponse((response) => (
      response.request().method() === 'POST' && new URL(response.url()).pathname === '/api/v1/auth/session'
    ));
    await loginDialog.getByRole('button', { name: '登录并继续' }).click();
    expect((await loginResponsePromise).status()).toBe(200);
    await expect(page.getByRole('button', { name: '用户菜单' })).toBeVisible();

    await page.goto('/my/skills/new');
    await expect(page.getByRole('heading', { name: '创建 Skill' })).toBeVisible();
    await page.getByLabel(/^Skill 名称/).fill(skillName);
    await page.getByLabel('简介').fill('通过真实浏览器验证 W1 Skill 创建、编辑和审核提交。');
    await page.getByLabel('分类').fill('browser-smoke');
    await page.getByLabel('标签（逗号分隔）').fill('w1, browser, smoke');
    await page.getByLabel('输入说明').fill('输入创作目标、素材范围和交付约束。');
    await page.getByLabel('输出说明').fill('输出可审核的结构化创作方案。');
    await page.getByLabel('Skill 调用规则').fill('仅在用户明确请求完整创作规划时调用。');
    for (const label of capabilityLabels) {
      await page.getByLabel(`${label}业务指导`).fill(`${label}必须遵循真实资源、权限和审核边界。`);
    }
    await page.getByRole('button', { name: '添加示例' }).click();
    await page.getByLabel('示例 1 输入').fill('请规划一支品牌介绍短片');
    await page.getByLabel('示例 1 输出').fill('返回包含镜头目标和素材约束的创作方案');
    await page.getByLabel('开场提示（每行一条）').fill('帮我规划介绍视频\n分析已有素材');
    await page.getByLabel('市场详情').fill('W1 浏览器真实链路验收 Skill。');
    await page.getByLabel('版权声明').fill('仅用于本地冒烟。');
    await page.getByLabel('用户须知').fill('不得用于生产内容。');

    const createResponsePromise = page.waitForResponse((response) => isSkillResponse(response, 'POST', '/api/v1/skills'));
    await page.getByRole('button', { name: '创建草稿' }).click();
    const createResponse = await createResponsePromise;
    expect(createResponse.status()).toBe(201);
    expect(createResponse.request().headers()['idempotency-key']).toBeTruthy();
    expect(createResponse.request().headers()['x-csrf-token']).toBeTruthy();
    expect(createResponse.headers().etag || '').toMatch(/^"[^"\r\n]+"$/);
    const createPayload = await createResponse.json();
    const skillID = String(createPayload.skill?.skill_id || '');
    expect(skillID).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/);
    expect(createPayload.skill?.definition?.name).toBe(skillName);
    expect(createPayload.skill?.definition?.public_tool_refs).toEqual([]);

    const editPath = `/my/skills/${encodeURIComponent(skillID)}/edit`;
    await expect(page).toHaveURL((url) => url.pathname === editPath);
    await expect(page.getByRole('heading', { name: '编辑 Skill 草稿' })).toBeVisible();
    await expect(page.getByText('草稿已保存')).toBeVisible();

    await page.getByLabel(/^Skill 名称/).fill(updatedSkillName);
    const updateResponsePromise = page.waitForResponse((response) => (
      isSkillResponse(response, 'PUT', `/api/v1/skills/${skillID}/draft`)
    ));
    await page.getByRole('button', { name: '保存草稿' }).click();
    const updateResponse = await updateResponsePromise;
    expect(updateResponse.status()).toBe(200);
    expect(updateResponse.request().headers()['if-match'] || '').toMatch(/^"[^"\r\n]+"$/);
    expect(updateResponse.request().headers()['x-csrf-token']).toBeTruthy();
    expect((await updateResponse.json()).skill?.definition?.name).toBe(updatedSkillName);
    await expect(page.getByText('草稿已保存')).toBeVisible();

    const reviewResponsePromise = page.waitForResponse((response) => (
      isSkillResponse(response, 'POST', `/api/v1/skills/${skillID}/reviews`)
    ));
    await page.getByRole('button', { name: '提交审核' }).click();
    const reviewResponse = await reviewResponsePromise;
    expect(reviewResponse.status()).toBe(201);
    expect(reviewResponse.request().headers()['idempotency-key']).toBeTruthy();
    expect(reviewResponse.request().headers()['x-csrf-token']).toBeTruthy();
    expect(reviewResponse.request().headers()['if-match'] || '').toMatch(/^"[^"\r\n]+"$/);
    const reviewPayload = await reviewResponse.json();
    expect(String(reviewPayload.review_id || '')).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/);
    expect(reviewPayload.skill?.review_status).toBe('reviewing');
    await expect(page.getByText('已提交审核，后续编辑不会改变本次审核内容。')).toBeVisible();
    await expect(page.getByLabel('Skill 当前状态')).toContainText('审核中');
    await expect(page.getByRole('button', { name: '提交审核' })).toBeDisabled();

    expect(skillRequests).toEqual(expect.arrayContaining([
      expect.objectContaining({ method: 'POST', pathname: '/api/v1/skills' }),
      expect.objectContaining({ method: 'PUT', pathname: `/api/v1/skills/${skillID}/draft` }),
      expect.objectContaining({ method: 'POST', pathname: `/api/v1/skills/${skillID}/reviews` })
    ]));
    expect(skillRequests.every((request) => request.origin === appOrigin)).toBe(true);
  });
});

test.describe('W1 mandatory real Reviewer publish chain', () => {
  test('@w1-real-review creator -> reviewer -> creator publishes and binds the frozen submission', async ({ page }) => {
    test.setTimeout(150_000);

    expect(email, 'DORA_E2E_USER_EMAIL must be preflighted').toBeTruthy();
    expect(password, 'DORA_E2E_USER_PASSWORD must be preflighted').toBeTruthy();
    expect(reviewerEmail, 'DORA_E2E_REVIEWER_EMAIL must be preflighted').toBeTruthy();
    expect(reviewerPassword, 'DORA_E2E_REVIEWER_PASSWORD must be preflighted').toBeTruthy();

    const runSuffix = `${Date.now()}`;
    const skillName = `W1 Reviewer Skill ${runSuffix}`;
    const sentinelA = `W1-REVIEW-SENTINEL-A-${runSuffix}`;
    const sentinelB = `W1-REVIEW-SENTINEL-B-${runSuffix}`;
    const quickCreatePrompt = `W1 Reviewer QuickCreate ${runSuffix}`;
    const businessRequests = [];
    page.on('request', (request) => {
      const url = new URL(request.url());
      if (url.pathname.startsWith('/api/')) {
        businessRequests.push({ method: request.method(), origin: url.origin, pathname: url.pathname });
      }
    });

    await page.goto('/');
    const appOrigin = new URL(page.url()).origin;
    const creatorLogin = await loginAs(page, email, password);
    const creatorID = exactPrincipalID(creatorLogin);
    expect(creatorLogin.principal?.capabilities || []).not.toContain('skill.review');
    await expect(page.getByRole('button', { name: 'Skill 审核' })).toHaveCount(0);

    const reviewerRequestsBeforeCreatorAdminRoute = businessRequests.filter((request) => (
      request.pathname.startsWith('/api/v1/admin/skill-reviews')
    )).length;
    await page.goto('/admin/skills/reviews');
    const creatorAdminDeniedHeading = page.getByRole('heading', { name: '无 Skill 审核权限' });
    const creatorAdminDeniedAlert = page.getByRole('alert');
    await expect(creatorAdminDeniedHeading).toBeVisible();
    await expect(creatorAdminDeniedAlert).toHaveText('当前会话不能使用 skill.review，未加载任何审核数据。');
    await expect(page.getByRole('button', { name: 'Skill 审核' })).toHaveCount(0);
    await page.waitForLoadState('networkidle');
    const reviewerRequestsAfterCreatorAdminRoute = businessRequests.filter((request) => (
      request.pathname.startsWith('/api/v1/admin/skill-reviews')
    )).length;
    const creatorAdminRouteBlocked = await creatorAdminDeniedHeading.isVisible()
      && await creatorAdminDeniedAlert.textContent() === '当前会话不能使用 skill.review，未加载任何审核数据。';
    const creatorAdminImplicitAPIBlocked = reviewerRequestsAfterCreatorAdminRoute
      === reviewerRequestsBeforeCreatorAdminRoute;
    expect(creatorAdminRouteBlocked).toBe(true);
    expect(creatorAdminImplicitAPIBlocked).toBe(true);

    const creatorAdminAPIResponse = await page.evaluate(async () => {
      const response = await fetch('/api/v1/admin/skill-reviews?status=reviewing', {
        method: 'GET',
        credentials: 'include',
        headers: { Accept: 'application/json' }
      });
      const responseText = await response.text();
      let payload = null;
      try {
        payload = JSON.parse(responseText);
      } catch {
        // The outer strict assertion reports a useful mismatch for malformed JSON.
      }
      return {
        status: response.status,
        contentType: response.headers.get('content-type') || '',
        cacheControl: response.headers.get('cache-control') || '',
        responseURL: response.url,
        responseText,
        payload
      };
    });
    expect(creatorAdminAPIResponse.status).toBe(403);
    expect(creatorAdminAPIResponse.contentType).toBe('application/json; charset=utf-8');
    const creatorAdminAPIURL = new URL(creatorAdminAPIResponse.responseURL);
    expect(creatorAdminAPIURL.pathname).toBe('/api/v1/admin/skill-reviews');
    expect(creatorAdminAPIURL.search).toBe('?status=reviewing');
    const creatorAdminDeniedPageText = await page.locator('body').innerText();
    const creatorSensitiveValues = [email, password].filter(Boolean);
    const creatorAdminDenialHasNoSensitiveData = creatorSensitiveValues.every((value) => (
      !creatorAdminAPIResponse.responseText.includes(value) && !creatorAdminDeniedPageText.includes(value)
    ));
    const creatorAdminAPIError = creatorAdminAPIResponse.payload?.error;
    const creatorAdminDenialRequestID = String(creatorAdminAPIError?.request_id || '');
    const creatorAdminAPIForbidden = creatorAdminAPIResponse.status === 403
      && creatorAdminAPIResponse.contentType === 'application/json; charset=utf-8'
      && creatorAdminAPIResponse.cacheControl === 'no-store'
      && Object.keys(creatorAdminAPIResponse.payload || {}).join(',') === 'error'
      && Object.keys(creatorAdminAPIError || {}).sort().join(',') === 'code,details,message,request_id,retryable'
      && creatorAdminAPIError?.code === 'SKILL_REVIEW_CAPABILITY_REQUIRED'
      && creatorAdminAPIError?.message === '当前账号没有 Skill 审核权限'
      && creatorAdminAPIError?.retryable === false
      && creatorAdminAPIError?.details !== null
      && typeof creatorAdminAPIError?.details === 'object'
      && !Array.isArray(creatorAdminAPIError?.details)
      && Object.keys(creatorAdminAPIError.details).length === 0
      && /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(creatorAdminDenialRequestID)
      && creatorAdminDenialHasNoSensitiveData;
    expect(
      creatorAdminAPIForbidden,
      'Creator Reviewer API denial must match the strict non-sensitive 403 contract'
    ).toBe(true);

    await page.goto('/my/skills/new');
    await expect(page.getByRole('heading', { name: '创建 Skill' })).toBeVisible();
    await fillReviewerSmokeSkill(page, { skillName, summary: sentinelA });

    const createResponsePromise = page.waitForResponse((response) => (
      isSkillResponse(response, 'POST', '/api/v1/skills')
    ));
    await page.getByRole('button', { name: '创建草稿' }).click();
    const createResponse = await createResponsePromise;
    expect(createResponse.status()).toBe(201);
    expect(createResponse.request().headers()['idempotency-key']).toBeTruthy();
    expect(createResponse.request().headers()['x-csrf-token']).toBeTruthy();
    const createPayload = await createResponse.json();
    const skillID = String(createPayload.skill?.skill_id || '');
    expect(skillID).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/);
    expect(createPayload.skill?.definition?.summary).toBe(sentinelA);

    const editPath = `/my/skills/${skillID}/edit`;
    await expect(page).toHaveURL((url) => url.pathname === editPath);
    const submitResponsePromise = page.waitForResponse((response) => (
      isSkillResponse(response, 'POST', `/api/v1/skills/${skillID}/reviews`)
    ));
    await page.getByRole('button', { name: '提交审核' }).click();
    const submitResponse = await submitResponsePromise;
    expect(submitResponse.status()).toBe(201);
    expect(submitResponse.request().headers()['idempotency-key']).toBeTruthy();
    expect(submitResponse.request().headers()['x-csrf-token']).toBeTruthy();
    expect(submitResponse.request().headers()['if-match'] || '').toMatch(/^"[^"\r\n]+"$/);
    const submitPayload = await submitResponse.json();
    const reviewID = String(submitPayload.review_id || '');
    expect(reviewID).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/);
    expect(submitPayload.skill?.definition?.summary).toBe(sentinelA);
    expect(submitPayload.skill?.review_status).toBe('reviewing');

    await page.getByLabel('简介').fill(sentinelB);
    const postSubmitDraftPromise = page.waitForResponse((response) => (
      isSkillResponse(response, 'PUT', `/api/v1/skills/${skillID}/draft`)
    ));
    await page.getByRole('button', { name: '保存草稿' }).click();
    const postSubmitDraftResponse = await postSubmitDraftPromise;
    expect(postSubmitDraftResponse.status()).toBe(200);
    const postSubmitDraftPayload = await postSubmitDraftResponse.json();
    expect(postSubmitDraftPayload.skill?.definition?.summary).toBe(sentinelB);
    expect(postSubmitDraftPayload.skill?.review_status).toBe('reviewing');
    const currentDraftDefinition = postSubmitDraftPayload.skill?.definition;
    const currentDraftETag = String(postSubmitDraftPayload.skill?.draft_etag || '');
    const currentDraftHeaderETag = String(postSubmitDraftResponse.headers().etag || '');
    const currentDraftProbeReady = currentDraftDefinition !== null
      && typeof currentDraftDefinition === 'object'
      && !Array.isArray(currentDraftDefinition)
      && currentDraftETag === currentDraftHeaderETag
      && /^"[^"\r\n]+"$/.test(currentDraftETag);
    expect(currentDraftProbeReady, 'Creator current draft must provide a valid opaque ETag and definition').toBe(true);

    await logoutFromCurrentContext(page);
    await page.goto('/');
    const reviewerLogin = await loginAs(page, reviewerEmail, reviewerPassword);
    const reviewerID = exactPrincipalID(reviewerLogin);
    expect(reviewerID).not.toBe(creatorID);
    expect(reviewerLogin.principal?.roles).toEqual(['skill_reviewer']);
    expect(reviewerLogin.principal?.capabilities).toEqual(['skill.review']);
    const reviewerCSRFToken = String(reviewerLogin.csrf_token || '');
    expect(Boolean(reviewerCSRFToken)).toBe(true);
    await expect(page.getByRole('button', { name: 'Skill 审核' })).toBeVisible();

    const ownerDetailPath = `/api/v1/skills/${skillID}`;
    const ownerDraftPath = `${ownerDetailPath}/draft`;
    const ownerReadsBeforeReviewerRoute = businessRequests.filter((request) => (
      request.method === 'GET' && request.pathname === ownerDetailPath
    )).length;
    const ownerWritesBeforeReviewerRoute = businessRequests.filter((request) => (
      request.method === 'PUT' && request.pathname === ownerDraftPath
    )).length;
    const reviewerOwnerReadObservations = [];
    const reviewerOwnerReadObservationPromises = [];
    const observeReviewerOwnerRead = (response) => {
      if (!isSkillResponse(response, 'GET', ownerDetailPath)) return;
      const observationPromise = ownerResponseObservation(response).then((observation) => {
        reviewerOwnerReadObservations.push(observation);
      });
      reviewerOwnerReadObservationPromises.push(observationPromise);
    };
    page.on('response', observeReviewerOwnerRead);
    await page.goto(editPath);
    const reviewerOwnerDeniedAlert = page.getByRole('alert');
    await page.waitForLoadState('networkidle');
    await page.waitForFunction(() => !document.body.innerText.includes('正在加载 Skill 草稿…'));
    page.off('response', observeReviewerOwnerRead);
    await Promise.all(reviewerOwnerReadObservationPromises);
    const reviewerOwnerDeniedPageText = await page.locator('body').innerText();
    const ownerReadsAfterReviewerRoute = businessRequests.filter((request) => (
      request.method === 'GET' && request.pathname === ownerDetailPath
    )).length;
    const ownerWritesAfterReviewerRoute = businessRequests.filter((request) => (
      request.method === 'PUT' && request.pathname === ownerDraftPath
    )).length;
    const reviewerOwnerReadDelta = ownerReadsAfterReviewerRoute - ownerReadsBeforeReviewerRoute;
    const reviewerOwnerReadNotFound = reviewerOwnerReadDelta >= 1
      && reviewerOwnerReadDelta <= 2
      && reviewerOwnerReadObservations.length >= 1
      && reviewerOwnerReadObservations.length <= reviewerOwnerReadDelta
      && reviewerOwnerReadObservations.every((result) => isStrictOwnerNotFoundResponse(result, ownerDetailPath));
    const reviewerOwnerRouteNotFound = await reviewerOwnerDeniedAlert.count() === 1
      && await reviewerOwnerDeniedAlert.textContent() === 'Skill 不存在或不可访问'
      && reviewerOwnerReadDelta >= 1
      && reviewerOwnerReadDelta <= 2
      && ownerWritesAfterReviewerRoute === ownerWritesBeforeReviewerRoute
      && await page.getByRole('button', { name: '返回我的 Skill' }).count() === 1
      && await page.locator('form.skill-builder-form').count() === 0
      && await page.getByRole('heading', { name: '编辑 Skill 草稿' }).count() === 0
      && await page.getByLabel('简介').count() === 0
      && await page.getByRole('button', { name: '保存草稿' }).count() === 0
      && await page.getByRole('button', { name: '提交审核' }).count() === 0;

    const reviewerOwnerWriteResponse = await page.evaluate(async ({ path, csrfToken, draftETag, definition }) => {
      const response = await fetch(path, {
        method: 'PUT',
        credentials: 'include',
        headers: {
          Accept: 'application/json',
          'Content-Type': 'application/json',
          'X-CSRF-Token': csrfToken,
          'If-Match': draftETag
        },
        body: JSON.stringify({ definition })
      });
      const responseText = await response.text();
      let payload = null;
      try {
        payload = JSON.parse(responseText);
      } catch {
        // The outer strict safe boolean fails closed for malformed JSON.
      }
      return {
        status: response.status,
        contentType: response.headers.get('content-type') || '',
        cacheControl: response.headers.get('cache-control') || '',
        responseURL: response.url,
        responseText,
        payload
      };
    }, {
      path: ownerDraftPath,
      csrfToken: reviewerCSRFToken,
      draftETag: currentDraftETag,
      definition: currentDraftDefinition
    });
    const ownerWritesAfterReviewerProbe = businessRequests.filter((request) => (
      request.method === 'PUT' && request.pathname === ownerDraftPath
    )).length;
    let reviewerOwnerWriteNotFound = isStrictOwnerNotFoundResponse(reviewerOwnerWriteResponse, ownerDraftPath)
      && ownerWritesAfterReviewerProbe === ownerWritesAfterReviewerRoute + 1;
    const reviewerOwnerForbiddenValues = [
      skillName,
      sentinelA,
      sentinelB,
      reviewID,
      email,
      password,
      reviewerEmail,
      reviewerPassword
    ].filter(Boolean);
    const reviewerOwnerResourceFactsNotDisclosed = reviewerOwnerForbiddenValues.every((value) => (
      reviewerOwnerReadObservations.every((result) => !result.responseText.includes(value))
      && !reviewerOwnerWriteResponse.responseText.includes(value)
      && !reviewerOwnerDeniedPageText.includes(value)
    ));
    await page.goto('/');
    expect(reviewerOwnerRouteNotFound, 'Reviewer cross-owner edit route must fail closed without an implicit write').toBe(true);
    expect(reviewerOwnerReadNotFound, 'Reviewer Owner detail read must match the strict safe 404 contract').toBe(true);
    expect(reviewerOwnerWriteNotFound, 'Reviewer Owner draft write must match the strict safe 404 contract').toBe(true);
    expect(
      reviewerOwnerResourceFactsNotDisclosed,
      'Reviewer Owner denial responses and UI must not disclose resource facts or login credentials'
    ).toBe(true);

    const queueResponsePromise = page.waitForResponse((response) => {
      const url = new URL(response.url());
      return response.request().method() === 'GET'
        && url.pathname === '/api/v1/admin/skill-reviews'
        && url.searchParams.get('status') === 'reviewing';
    });
    await page.getByRole('button', { name: 'Skill 审核' }).click();
    const queueResponse = await queueResponsePromise;
    expect(queueResponse.status()).toBe(200);
    const reviewCard = page.locator('article.skill-review-card').filter({ hasText: skillName });
    await expect(reviewCard).toHaveCount(1);

    const detailResponsePromise = page.waitForResponse((response) => (
      isSkillResponse(response, 'GET', `/api/v1/admin/skill-reviews/${reviewID}`)
    ));
    await reviewCard.getByRole('button', { name: '查看冻结详情' }).click();
    const detailResponse = await detailResponsePromise;
    expect(detailResponse.status()).toBe(200);
    const detailHeaderETag = detailResponse.headers().etag || '';
    expect(detailHeaderETag).toMatch(/^"[\x21\x23-\x7e\x80-\xff]+"$/);
    const detailPayload = await detailResponse.json();
    const detailBodyETag = String(detailPayload.review?.review_etag || '');
    expect(detailBodyETag).toBe(detailHeaderETag);
    expect(detailPayload.review?.review_id).toBe(reviewID);
    expect(detailPayload.review?.skill_id).toBe(skillID);
    expect(detailPayload.review?.definition?.summary).toBe(sentinelA);
    expect(JSON.stringify(detailPayload.review?.definition || {})).not.toContain(sentinelB);
    await expect(page.getByRole('region', { name: '本次冻结提交', exact: true })).toContainText(sentinelA);
    await expect(page.getByRole('region', { name: '本次冻结提交', exact: true })).not.toContainText(sentinelB);

    const decisionResponsePromise = page.waitForResponse((response) => (
      isSkillResponse(response, 'POST', `/api/v1/admin/skill-reviews/${reviewID}/decisions`)
    ));
    await page.getByRole('button', { name: '批准并发布' }).click();
    const decisionResponse = await decisionResponsePromise;
    expect(decisionResponse.status()).toBe(200);
    const decisionHeaders = decisionResponse.request().headers();
    expect(decisionHeaders['x-csrf-token'] === reviewerCSRFToken).toBe(true);
    expect(Boolean(decisionHeaders['idempotency-key'])).toBe(true);
    expect(decisionHeaders['if-match']).toBe(detailHeaderETag);
    expect(decisionHeaders['if-match']).toBe(detailBodyETag);
    expect(decisionResponse.request().postDataJSON()).toEqual({ decision: 'approved' });
    const decisionPayload = await decisionResponse.json();
    expect(decisionPayload.review).toMatchObject({
      review_id: reviewID,
      skill_id: skillID,
      status: 'approved',
      allowed_actions: []
    });
    const publishedSnapshotID = String(decisionPayload.review?.published_snapshot_id || '');
    expect(publishedSnapshotID).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/);
    await expect(page.getByText('审核已批准，冻结内容已原子发布。')).toBeVisible();

    await logoutFromCurrentContext(page);
    await page.goto('/');
    const creatorRelogin = await loginAs(page, email, password);
    expect(exactPrincipalID(creatorRelogin)).toBe(creatorID);
    expect(creatorRelogin.principal?.capabilities || []).not.toContain('skill.review');

    const creatorPostProbeOwnerResponse = await page.evaluate(async (path) => {
      const response = await fetch(path, {
        method: 'GET',
        credentials: 'include',
        headers: { Accept: 'application/json' }
      });
      const responseText = await response.text();
      let payload = null;
      try {
        payload = JSON.parse(responseText);
      } catch {
        // The outer safe boolean fails closed for malformed JSON.
      }
      return {
        status: response.status,
        contentType: response.headers.get('content-type') || '',
        cacheControl: response.headers.get('cache-control') || '',
        etag: response.headers.get('etag') || '',
        responseURL: response.url,
        payload
      };
    }, ownerDetailPath);
    const creatorPostProbeOwnerURL = new URL(creatorPostProbeOwnerResponse.responseURL);
    const creatorPostProbeOwnerUnchanged = creatorPostProbeOwnerResponse.status === 200
      && creatorPostProbeOwnerResponse.contentType === 'application/json; charset=utf-8'
      && creatorPostProbeOwnerResponse.cacheControl === 'no-store'
      && creatorPostProbeOwnerResponse.etag === currentDraftETag
      && creatorPostProbeOwnerURL.pathname === ownerDetailPath
      && creatorPostProbeOwnerURL.search === ''
      && creatorPostProbeOwnerResponse.payload?.skill?.skill_id === skillID
      && creatorPostProbeOwnerResponse.payload?.skill?.draft_etag === currentDraftETag
      && creatorPostProbeOwnerResponse.payload?.skill?.definition?.summary === sentinelB
      && JSON.stringify(creatorPostProbeOwnerResponse.payload?.skill?.definition || null)
        === JSON.stringify(currentDraftDefinition)
      && /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(
        creatorPostProbeOwnerResponse.payload?.request_id || ''
      );
    reviewerOwnerWriteNotFound = reviewerOwnerWriteNotFound && creatorPostProbeOwnerUnchanged;
    expect(
      creatorPostProbeOwnerUnchanged,
      'Creator draft ETag and definition must remain unchanged after the denied Reviewer write probe'
    ).toBe(true);

    await page.getByRole('button', { name: 'Skill', exact: true }).click();
    const picker = page.getByRole('dialog', { name: '选择 QuickCreate Skill' });
    const publishedSkill = picker.getByRole('checkbox', { name: `选择 ${skillName}` });
    await expect(publishedSkill).toBeEnabled();
    await publishedSkill.check();
    await page.getByPlaceholder('由一个想法或故事开始...').fill(quickCreatePrompt);
    const quickCreateResponsePromise = page.waitForResponse((response) => (
      isSkillResponse(response, 'POST', '/api/v1/projects:quick-create')
    ));
    const toolCatalogResponsePromise = page.waitForResponse((response) => {
      const url = new URL(response.url());
      return response.request().method() === 'GET'
        && /^\/api\/v1\/agent\/sessions\/[0-9a-f-]{36}\/tools$/.test(url.pathname)
        && url.search === '';
    });
    await page.getByRole('button', { name: '开始创作' }).click();
    const quickCreateResponse = await quickCreateResponsePromise;
    expect(quickCreateResponse.status()).toBe(201);
    expect(quickCreateResponse.request().headers()['idempotency-key']).toBeTruthy();
    expect(quickCreateResponse.request().headers()['x-csrf-token']).toBeTruthy();
    expect(quickCreateResponse.request().postDataJSON()).toEqual({
      schema_version: 'project_quick_create.v2',
      initial_prompt: quickCreatePrompt,
      enabled_skill_ids: [skillID]
    });
    const quickCreatePayload = await quickCreateResponse.json();
    const projectID = String(quickCreatePayload.project_id || '');
    expect(projectID).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/);
    await expect(page).toHaveURL((url) => url.pathname === `/projects/${projectID}/workspace`);
    const workspace = page.locator('main[data-workspace-state]');
    await expect(workspace).toHaveAttribute('data-workspace-state', 'ready', { timeout: 30_000 });
    await expect(workspace).toHaveAttribute('data-project-id', projectID);
    const workspaceSessionID = String(await workspace.getAttribute('data-session-id') || '');
    expect(workspaceSessionID).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/);
    const toolCatalogResponse = await toolCatalogResponsePromise;
    expect(toolCatalogResponse.status()).toBe(200);
    expect(new URL(toolCatalogResponse.url()).pathname).toBe(`/api/v1/agent/sessions/${workspaceSessionID}/tools`);
    const toolCatalogPayload = await toolCatalogResponse.json();
    expect(toolCatalogPayload).toEqual({
      schema_version: 'tool_definition_catalog.v1',
      request_id: expect.stringMatching(/^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/),
      items: [
        unavailableTool('plan_creation_spec', '流程规划', 1),
        unavailableTool('analyze_materials', '素材分析', 2),
        unavailableTool('plan_storyboard', '故事板设计', 3),
        unavailableTool('generate_media', '媒体生成', 4),
        unavailableTool('write_prompts', '提示词写法', 5),
        unavailableTool('assemble_output', '视频剪辑', 6)
      ]
    });
    await expect(page.getByText('工作台已就绪')).toBeVisible();
    const workspaceSnapshot = page.getByRole('region', { name: '工作台快照' });
    await expect(workspaceSnapshot.getByText(projectID, { exact: true })).toBeVisible();
    await expect(workspaceSnapshot.getByText(workspaceSessionID, { exact: true })).toBeVisible();
    const toolCatalog = page.getByRole('region', { name: '工具目录' });
    const toolItems = toolCatalog.getByRole('listitem');
    await expect(toolItems).toHaveCount(6);
    for (const [index, label] of capabilityLabels.entries()) {
      const item = toolItems.nth(index);
      await expect(item).toHaveAttribute('aria-disabled', 'true');
      await expect(item).toHaveAttribute('data-tool-order', String(index + 1));
      await expect(item).toHaveAttribute('data-tool-availability', 'unavailable');
      await expect(item.getByText(label, { exact: true })).toBeVisible();
      await expect(item.getByText('设计评审中', { exact: true })).toBeVisible();
    }
    await expect(toolCatalog.getByRole('button')).toHaveCount(0);

    expect(businessRequests.every((request) => request.origin === appOrigin)).toBe(true);
    expect(businessRequests.some((request) => request.pathname.startsWith('/api/aigc/'))).toBe(false);
    expect(businessRequests).toEqual(expect.arrayContaining([
      expect.objectContaining({ method: 'POST', pathname: '/api/v1/skills' }),
      expect.objectContaining({ method: 'GET', pathname: `/api/v1/skills/${skillID}` }),
      expect.objectContaining({ method: 'POST', pathname: `/api/v1/skills/${skillID}/reviews` }),
      expect.objectContaining({ method: 'PUT', pathname: `/api/v1/skills/${skillID}/draft` }),
      expect.objectContaining({ method: 'GET', pathname: '/api/v1/admin/skill-reviews' }),
      expect.objectContaining({ method: 'POST', pathname: `/api/v1/admin/skill-reviews/${reviewID}/decisions` }),
      expect.objectContaining({ method: 'POST', pathname: '/api/v1/projects:quick-create' }),
      expect.objectContaining({ method: 'GET', pathname: `/api/v1/agent/sessions/${workspaceSessionID}/tools` })
    ]));

    if (w1ResultPath) {
      await writeFile(w1ResultPath, JSON.stringify({
        schema_version: 'w1.real-review-result.v3',
        creator_id: creatorID,
        creator_admin_route_blocked: creatorAdminRouteBlocked,
        creator_admin_implicit_api_blocked: creatorAdminImplicitAPIBlocked,
        creator_admin_api_forbidden: creatorAdminAPIForbidden,
        creator_admin_denial_request_id: creatorAdminDenialRequestID,
        reviewer_id: reviewerID,
        reviewer_owner_route_not_found: reviewerOwnerRouteNotFound,
        reviewer_owner_read_not_found: reviewerOwnerReadNotFound,
        reviewer_owner_write_not_found: reviewerOwnerWriteNotFound,
        reviewer_owner_resource_facts_not_disclosed: reviewerOwnerResourceFactsNotDisclosed,
        skill_id: skillID,
        review_id: reviewID,
        published_snapshot_id: publishedSnapshotID,
        project_id: projectID,
        tool_catalog_session_id: workspaceSessionID,
        tool_catalog_request_id: toolCatalogPayload.request_id,
        tool_catalog_exact_unavailable: true,
        submitted_summary: sentinelA,
        current_draft_summary: sentinelB
      }), { encoding: 'utf8', mode: 0o600 });
    }
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

function unavailableTool(toolKey, displayName, order) {
  return {
    tool_key: toolKey,
    display_name: displayName,
    order,
    availability: 'unavailable',
    reason_code: 'DESIGN_REVIEW_PENDING'
  };
}

async function logoutFromCurrentContext(page) {
  await page.getByRole('button', { name: '用户菜单' }).click();
  const accountMenu = page.getByRole('dialog', { name: '账户与积分' });
  const logoutResponsePromise = page.waitForResponse((response) => (
    response.request().method() === 'DELETE' && new URL(response.url()).pathname === '/api/v1/auth/session'
  ));
  await accountMenu.getByRole('button', { name: '退出登录' }).click();
  const logoutResponse = await logoutResponsePromise;
  expect([200, 204]).toContain(logoutResponse.status());
}

async function fillReviewerSmokeSkill(page, { skillName, summary }) {
  await page.getByLabel(/^Skill 名称/).fill(skillName);
  await page.getByLabel('简介').fill(summary);
  await page.getByLabel('分类').fill('browser-review-smoke');
  await page.getByLabel('标签（逗号分隔）').fill('w1, browser, reviewer');
  await page.getByLabel('输入说明').fill('输入真实 Reviewer 链路的创作目标与素材边界。');
  await page.getByLabel('输出说明').fill('输出冻结且可发布的结构化创作方案。');
  await page.getByLabel('Skill 调用规则').fill('仅在用户明确选择本 Skill 时调用。');
  for (const label of capabilityLabels) {
    await page.getByLabel(`${label}业务指导`).fill(`${label}必须遵循真实资源、权限和冻结审核边界。`);
  }
  await page.getByRole('button', { name: '添加示例' }).click();
  await page.getByLabel('示例 1 输入').fill('请规划一支 Reviewer 冒烟短片');
  await page.getByLabel('示例 1 输出').fill('返回含镜头目标、素材约束和发布边界的方案');
  await page.getByLabel('开场提示（每行一条）').fill('规划 Reviewer 冒烟视频\n分析 Reviewer 冒烟素材');
  await page.getByLabel('市场详情').fill('W1 Reviewer 双身份真实链路验收 Skill。');
  await page.getByLabel('版权声明').fill('仅用于本地 Reviewer 冒烟。');
  await page.getByLabel('用户须知').fill('不得用于生产内容。');
}

function exactPrincipalID(payload) {
  const principalID = String(payload.principal?.id || '');
  expect(principalID).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/);
  return principalID;
}
