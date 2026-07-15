import { defineConfig } from '@playwright/test';

const baseURL = process.env.DORA_E2E_BASE_URL || 'http://127.0.0.1:3200';
const useExternalServer = process.env.DORA_E2E_EXTERNAL_SERVER === '1';
const webServerPort = new URL(baseURL).port || '3200';
if (!/^\d{1,5}$/.test(webServerPort) || Number(webServerPort) > 65535) {
  throw new TypeError('DORA_E2E_BASE_URL 必须提供有效的本地前端端口');
}
const businessAPITarget = process.env.DORA_E2E_BUSINESS_API_TARGET
  || process.env.VITE_BUSINESS_API_TARGET
  || 'http://127.0.0.1:18081';

export default defineConfig({
  testDir: './e2e',
  testMatch: '**/*.spec.js',
  outputDir: process.env.DORA_E2E_OUTPUT_DIR || '../.local/playwright/w0',
  fullyParallel: false,
  workers: 1,
  timeout: 45_000,
  expect: {
    timeout: 12_000
  },
  reporter: [['list']],
  use: {
    baseURL,
    browserName: 'chromium',
    screenshot: 'off',
    trace: 'off',
    video: 'off'
  },
  webServer: useExternalServer
    ? undefined
    : {
        command: `npm run dev -- --port ${webServerPort} --strictPort`,
        env: {
          ...process.env,
          VITE_BUSINESS_API_TARGET: businessAPITarget
        },
        reuseExistingServer: process.env.DORA_E2E_REUSE_EXISTING_SERVER === '1',
        timeout: 30_000,
        url: baseURL
      }
});
