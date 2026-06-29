import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

export default defineConfig({
  plugins: [react()],
  test: {
    environment: 'jsdom',
    setupFiles: './src/test/setup.js'
  },
  server: {
    proxy: {
      '/api/agent': {
        target: 'http://localhost:18080',
        changeOrigin: true
      },
      '/api': {
        target: 'http://localhost:19080',
        changeOrigin: true
      }
    }
  }
});
