import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// CLI host/port 参数透传：vite 自身支持 --host/--port，npm run dev -- --port 7100 直接生效
export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      '/api': 'http://localhost:7100',
    },
  },
  build: {
    outDir: 'dist',
  },
});
