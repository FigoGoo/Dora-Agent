#!/usr/bin/env node
import { spawn, spawnSync } from 'node:child_process';
import { existsSync } from 'node:fs';
import http from 'node:http';
import net from 'node:net';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { chromium } from 'playwright-core';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(__dirname, '../../..');
const defaultChrome = '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome';
const chromePathCandidates = [
  defaultChrome,
  '/usr/bin/google-chrome',
  '/usr/bin/google-chrome-stable',
  '/usr/bin/chromium',
  '/usr/bin/chromium-browser'
];
const chromeBinaryCandidates = [
  'google-chrome',
  'google-chrome-stable',
  'chromium',
  'chromium-browser'
];

function log(message) {
  console.log(`[pr5-browser] ${message}`);
}

function resolveChromeExecutable() {
  if (process.env.CHROME_EXECUTABLE) {
    return process.env.CHROME_EXECUTABLE;
  }
  for (const candidate of chromePathCandidates) {
    if (existsSync(candidate)) {
      return candidate;
    }
  }
  for (const binary of chromeBinaryCandidates) {
    const result = spawnSync('which', [binary], { encoding: 'utf8' });
    if (result.status === 0 && result.stdout.trim()) {
      return result.stdout.trim().split('\n')[0];
    }
  }
  throw new Error('Chrome executable not found; set CHROME_EXECUTABLE to a local Chrome or Chromium binary.');
}

function run(command, args, options = {}) {
  return new Promise((resolve, reject) => {
    const child = spawn(command, args, {
      cwd: repoRoot,
      stdio: 'inherit',
      env: { ...process.env, ...options.env }
    });
    child.on('error', reject);
    child.on('exit', (code) => {
      if (code === 0) {
        resolve();
        return;
      }
      reject(new Error(`${command} ${args.join(' ')} exited with ${code}`));
    });
  });
}

function getFreePort() {
  return new Promise((resolve, reject) => {
    const server = net.createServer();
    server.listen(0, '127.0.0.1', () => {
      const address = server.address();
      server.close(() => resolve(address.port));
    });
    server.on('error', reject);
  });
}

function httpReady(url) {
  return new Promise((resolve) => {
    const request = http.get(url, (response) => {
      response.resume();
      resolve(response.statusCode >= 200 && response.statusCode < 500);
    });
    request.on('error', () => resolve(false));
    request.setTimeout(1000, () => {
      request.destroy();
      resolve(false);
    });
  });
}

async function waitForHttp(url, label) {
  const deadline = Date.now() + 30000;
  while (Date.now() < deadline) {
    if (await httpReady(url)) {
      return;
    }
    await new Promise((resolve) => setTimeout(resolve, 300));
  }
  throw new Error(`${label} did not become ready at ${url}`);
}

async function startPreview(label, directory, port) {
  const child = spawn('npm', ['exec', 'vite', '--', 'preview', '--host', '127.0.0.1', '--port', String(port), '--strictPort'], {
    cwd: path.join(repoRoot, directory),
    stdio: ['ignore', 'pipe', 'pipe'],
    env: process.env
  });
  child.stdout.on('data', (chunk) => process.stdout.write(`[${label}] ${chunk}`));
  child.stderr.on('data', (chunk) => process.stderr.write(`[${label}] ${chunk}`));
  child.on('exit', (code) => {
    if (code !== null && code !== 0) {
      console.error(`[${label}] exited with ${code}`);
    }
  });
  await waitForHttp(`http://127.0.0.1:${port}/`, label);
  return child;
}

async function expectVisible(locator, label) {
  await locator.waitFor({ state: 'visible', timeout: 10000 }).catch((error) => {
    throw new Error(`${label} was not visible: ${error.message}`);
  });
}

async function setupUserFrontend(page) {
  await page.addInitScript(() => {
    window.__doraBrowserCalls = [];
    window.__doraInstalledSkill = null;
    window.__doraCreatorSkill = null;

    function ok(data) {
      return Promise.resolve(new Response(JSON.stringify({ code: 'OK', message: 'ok', data, trace_id: 'trace_pr5_browser_frontend' }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' }
      }));
    }

    window.fetch = async (input, init = {}) => {
      const url = new URL(String(input), window.location.origin);
      const method = String(init.method || 'GET').toUpperCase();
      let body = {};
      if (init.body) {
        body = JSON.parse(init.body);
      }
      window.__doraBrowserCalls.push({
        path: url.pathname,
        method,
        body,
        idempotency_key: init.headers?.['Idempotency-Key'] || init.headers?.get?.('Idempotency-Key') || ''
      });

      if (method === 'GET' && url.pathname === '/api/marketplace/skills') {
        return ok({
          items: [
            {
              listing_id: 'listing_browser_city_001',
              skill_id: 'skill_browser_city',
              skill_version_id: 'skv_browser_city_1',
              skill_version: '1.0.0',
              skill_name: '文旅城市名片',
              skill_description: '浏览器 E2E 市场 Skill。',
              creator_user_id: 'creator_browser_001',
              status: 'listed',
              pricing_model: 'fixed',
              usage_credits: 120
            }
          ],
          next_cursor: ''
        });
      }
      if (method === 'GET' && url.pathname === '/api/marketplace/my-skills') {
        return ok({ items: window.__doraInstalledSkill ? [window.__doraInstalledSkill] : [] });
      }
      if (method === 'POST' && url.pathname === '/api/marketplace/installations') {
        window.__doraInstalledSkill = {
          installation_id: 'sinst_browser_city_001',
          account_id: 'acct_browser_001',
          account_scope: 'personal',
          listing_id: 'listing_browser_city_001',
          skill_id: 'skill_browser_city',
          skill_name: '文旅城市名片',
          installed_version: '1.0.0',
          version_strategy: 'latest_published',
          status: 'installed',
          upgrade_status: 'none'
        };
        return ok({ installation: window.__doraInstalledSkill, idempotent_replay: false });
      }
      if (method === 'GET' && url.pathname === '/api/creator/listings') {
        return ok({ items: window.__doraCreatorSkill ? [window.__doraCreatorSkill] : [] });
      }
      if (method === 'GET' && url.pathname === '/api/creator/analytics/skill-usage') {
        return ok({ usage_count: 0, revenue_hold_amount: 0, refund_count: 0, failure_code_summary: {} });
      }
      if (method === 'POST' && url.pathname === '/api/creator/skills') {
        window.__doraCreatorSkill = {
          skill_id: 'skill_browser_creator_001',
          name: body.name,
          description: body.description,
          visibility: 'review_only',
          version: 'v1',
          skill_version_id: 'skv_browser_creator_001',
          version_status: 'draft',
          review_status: 'not_submitted',
          listing_status: 'not_listed',
          pricing_model: 'free',
          usage_credits: 0,
          value_delivered_stage: 'storyboard_ready'
        };
        return ok({ skill: window.__doraCreatorSkill });
      }
      if (method === 'POST' && url.pathname === '/api/creator/skills/skill_browser_creator_001/versions/v1/submit') {
        window.__doraCreatorSkill = {
          ...window.__doraCreatorSkill,
          version_status: 'submitted',
          review_status: 'submitted',
          submitted_at: '2026-07-01T08:00:00Z'
        };
        return ok({ skill_version: window.__doraCreatorSkill });
      }

      return Promise.resolve(new Response(JSON.stringify({ code: 'NOT_FOUND', message: `unexpected ${method} ${url.pathname}` }), {
        status: 404,
        headers: { 'Content-Type': 'application/json' }
      }));
    };
  });
}

async function setupAdminFrontend(page) {
  await page.addInitScript(() => {
    localStorage.setItem('dora_admin_session', JSON.stringify({
      admin_id: 'adm_browser_001',
      account: 'browser-admin',
      role: 'super_admin',
      access_token: 'admin-browser-token',
      expires_at: '2099-01-01T00:00:00Z',
      must_rotate_password: false
    }));
    window.__doraAdminCalls = [];
    window.__doraSettlementStatuses = {
      settle_browser_pending: 'pending_hold',
      settle_browser_eligible: 'eligible'
    };

    function settlementRows() {
      return [
        {
          settlement_id: 'settle_browser_pending',
          usage_id: 'susage_browser_pending',
          skill_id: 'skill_browser_city',
          skill_name: '文旅城市名片',
          creator_user_id: 'creator_browser_001',
          status: window.__doraSettlementStatuses.settle_browser_pending,
          gross_credits: 120,
          platform_fee_credits: 24,
          creator_credits: 96,
          hold_until: '2026-07-01T00:00:00Z',
          created_at: '2026-07-01T00:00:00Z',
          updated_at: '2026-07-01T00:00:00Z'
        },
        {
          settlement_id: 'settle_browser_eligible',
          usage_id: 'susage_browser_eligible',
          skill_id: 'skill_browser_story',
          skill_name: '故事板策划',
          creator_user_id: 'creator_browser_002',
          status: window.__doraSettlementStatuses.settle_browser_eligible,
          gross_credits: 200,
          platform_fee_credits: 40,
          creator_credits: 160,
          hold_until: '2026-07-01T00:00:00Z',
          created_at: '2026-07-01T00:00:00Z',
          updated_at: '2026-07-01T00:00:00Z'
        }
      ];
    }

    function ok(data) {
      return Promise.resolve(new Response(JSON.stringify({ code: 'OK', message: 'ok', data, trace_id: 'trace_pr5_browser_admin' }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' }
      }));
    }

    window.fetch = async (input, init = {}) => {
      const url = new URL(String(input), window.location.origin);
      const method = String(init.method || 'GET').toUpperCase();
      let body = {};
      if (init.body) {
        body = JSON.parse(init.body);
      }
      window.__doraAdminCalls.push({
        path: url.pathname,
        method,
        body,
        idempotency_key: init.headers?.['Idempotency-Key'] || init.headers?.get?.('Idempotency-Key') || ''
      });

      if (method === 'GET' && url.pathname === '/api/admin/marketplace/settlements') {
        return ok({ items: settlementRows(), limit: 10, offset: 0, total: 2 });
      }
      if (method === 'POST' && url.pathname === '/api/admin/settlements/settle_browser_pending/release-hold') {
        window.__doraSettlementStatuses.settle_browser_pending = 'eligible';
        return ok({
          settlement: settlementRows()[0],
          payout: {
            payout_id: 'spayout_browser_release',
            settlement_id: 'settle_browser_pending',
            action: 'release_hold',
            status_before: 'pending_hold',
            status_after: 'eligible',
            reason_code: body.reason_code,
            operator_admin_id: 'adm_browser_001'
          }
        });
      }
      if (method === 'POST' && url.pathname === '/api/admin/settlements/settle_browser_eligible/confirm-payout') {
        window.__doraSettlementStatuses.settle_browser_eligible = 'settled';
        return ok({
          settlement: settlementRows()[1],
          payout: {
            payout_id: 'spayout_browser_confirm',
            settlement_id: 'settle_browser_eligible',
            action: 'confirm_payout',
            status_before: 'eligible',
            status_after: 'settled',
            payout_reference: body.payout_reference,
            reason_code: body.reason_code,
            operator_admin_id: 'adm_browser_001'
          }
        });
      }

      return Promise.resolve(new Response(JSON.stringify({ code: 'NOT_FOUND', message: `unexpected ${method} ${url.pathname}` }), {
        status: 404,
        headers: { 'Content-Type': 'application/json' }
      }));
    };
  });
}

async function runUserFrontendSmoke(browser, baseURL) {
  const page = await browser.newPage({ viewport: { width: 1366, height: 900 } });
  await setupUserFrontend(page);
  await page.goto(`${baseURL}/skill`, { waitUntil: 'networkidle' });

  await expectVisible(page.getByRole('heading', { name: 'Skill' }), 'Skill 页面');
  await expectVisible(page.getByRole('tab', { name: '市场', selected: true }), '市场 tab');
  await page.getByTestId('skill-card').first().getByRole('button', { name: '安装' }).click();
  await page.getByRole('button', { name: '登录并继续' }).click();
  await expectVisible(page.getByText('文旅城市名片'), '市场 listing');
  await page.getByTestId('skill-card').filter({ hasText: '文旅城市名片' }).getByRole('button', { name: '安装' }).click();
  await expectVisible(page.getByTestId('skill-card').filter({ hasText: '文旅城市名片' }).getByText('已安装'), '安装状态');

  await page.getByRole('tab', { name: '创作台' }).click();
  await expectVisible(page.locator('form[aria-label="创建 Skill 草稿"]'), '创作者草稿表单');
  await page.getByLabel('Skill 名称').fill('浏览器脚本策划');
  await page.getByLabel('Skill 说明').fill('浏览器联动提交 Skill。');
  await page.getByRole('button', { name: '保存草稿' }).click();
  await expectVisible(page.getByText('草稿已保存，可提交审核。'), '草稿保存提示');
  await page.getByTestId('creator-skill-card').getByRole('button', { name: '提交审核' }).click();
  await expectVisible(page.getByText('已提交审核，等待平台确认。'), '提交审核提示');

  const calls = await page.evaluate(() => window.__doraBrowserCalls);
  const installCall = calls.find((call) => call.path === '/api/marketplace/installations' && call.method === 'POST');
  if (!installCall || installCall.body.listing_id !== 'listing_browser_city_001' || installCall.body.target_scope !== 'personal') {
    throw new Error(`bad marketplace install call: ${JSON.stringify(installCall)}`);
  }
  if (installCall.idempotency_key !== 'install:listing_browser_city_001:personal') {
    throw new Error(`bad marketplace install idempotency key: ${installCall.idempotency_key}`);
  }
  const submitCall = calls.find((call) => call.path === '/api/creator/skills/skill_browser_creator_001/versions/v1/submit');
  if (!submitCall || submitCall.idempotency_key !== 'creator-submit:skill_browser_creator_001:v1') {
    throw new Error(`bad creator submit call: ${JSON.stringify(submitCall)}`);
  }
  await page.close();
  log('user frontend browser smoke passed');
}

async function runAdminFrontendSmoke(browser, baseURL) {
  const page = await browser.newPage({ viewport: { width: 1440, height: 960 } });
  await setupAdminFrontend(page);
  await page.goto(`${baseURL}/admin/skills/settlements`, { waitUntil: 'networkidle' });

  await expectVisible(page.getByRole('heading', { name: 'Skill 结算' }), 'Skill 结算页面');
  await page.getByRole('button', { name: '解除 hold' }).click();
  await expectVisible(page.getByRole('dialog').getByText('解除 hold'), '解除 hold 弹窗');
  await page.getByRole('button', { name: '保存' }).click();
  await expectVisible(page.getByText('Settlement hold 已解除'), '解除 hold 成功提示');

  const payoutRow = page.locator('tbody tr').filter({ hasText: '故事板策划' }).first();
  await payoutRow.getByRole('button', { name: '确认出账' }).click();
  await expectVisible(page.getByRole('dialog').getByText('确认出账'), '确认出账弹窗');
  await page.getByLabel('出账引用').fill('manual-ledger-browser-001');
  await page.getByRole('button', { name: '保存' }).click();
  await expectVisible(page.getByText('Settlement 已确认出账'), '确认出账成功提示');

  const calls = await page.evaluate(() => window.__doraAdminCalls);
  const releaseCall = calls.find((call) => call.path === '/api/admin/settlements/settle_browser_pending/release-hold');
  if (!releaseCall || releaseCall.body.reason_code !== 'hold_period_completed') {
    throw new Error(`bad settlement release call: ${JSON.stringify(releaseCall)}`);
  }
  if (releaseCall.idempotency_key !== 'settlement_release:settle_browser_pending') {
    throw new Error(`bad settlement release idempotency key: ${releaseCall.idempotency_key}`);
  }
  const payoutCall = calls.find((call) => call.path === '/api/admin/settlements/settle_browser_eligible/confirm-payout');
  if (!payoutCall || payoutCall.body.payout_reference !== 'manual-ledger-browser-001' || payoutCall.body.reason_code !== 'manual_payout_confirmed') {
    throw new Error(`bad settlement payout call: ${JSON.stringify(payoutCall)}`);
  }
  if (payoutCall.idempotency_key !== 'settlement_payout:settle_browser_eligible:manual-ledger-browser-001') {
    throw new Error(`bad settlement payout idempotency key: ${payoutCall.idempotency_key}`);
  }
  await page.close();
  log('admin frontend browser smoke passed');
}

async function main() {
  const chromeExecutable = resolveChromeExecutable();
  log(`using Chrome executable: ${chromeExecutable}`);
  const skipBuild = process.argv.includes('--skip-build');
  if (!skipBuild) {
    log('building frontend bundles');
    await run('npm', ['--prefix', 'frontend', 'run', 'build']);
    await run('npm', ['--prefix', 'admin_frontend', 'run', 'build']);
  }

  const frontendPort = await getFreePort();
  const adminPort = await getFreePort();
  const processes = [];
  let browser;
  try {
    processes.push(await startPreview('frontend', 'frontend', frontendPort));
    processes.push(await startPreview('admin_frontend', 'admin_frontend', adminPort));
    browser = await chromium.launch({
      executablePath: chromeExecutable,
      headless: true,
      args: ['--no-sandbox', '--disable-dev-shm-usage']
    });
    await runUserFrontendSmoke(browser, `http://127.0.0.1:${frontendPort}`);
    await runAdminFrontendSmoke(browser, `http://127.0.0.1:${adminPort}`);
    log('PR-5 frontend browser smoke passed');
  } finally {
    if (browser) {
      await browser.close();
    }
    for (const child of processes.reverse()) {
      child.kill('SIGTERM');
    }
  }
}

main().catch((error) => {
  console.error(error);
  process.exit(1);
});
