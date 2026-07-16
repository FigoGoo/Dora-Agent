import { defineConfig, loadEnv } from 'vite';
import react from '@vitejs/plugin-react';

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), '');
  const businessTarget = env.VITE_BUSINESS_API_TARGET || 'http://localhost:18081';
  const agentTarget = env.VITE_AGENT_API_TARGET || 'http://localhost:18082';

  return {
    plugins: [react()],
    test: {
      environment: 'jsdom',
      include: ['src/**/*.{test,spec}.{js,jsx}'],
      setupFiles: './src/test/setup.js'
    },
    server: {
      proxy: {
        // 历史工作台接口明确进入 Agent；其余 API 默认由 Business 承接。
        '/api/aigc': { target: agentTarget, changeOrigin: true },
        '/api': { target: businessTarget, changeOrigin: true }
      }
    }
  };
});
