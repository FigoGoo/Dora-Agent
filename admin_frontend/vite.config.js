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
      '/api/agent': 'http://localhost:18080',
      '/api': 'http://localhost:19080'
    }
  }
});
