import { defineConfig } from 'vite';
import path from 'path';

export default defineConfig({
  root: '.',
  build: {
    outDir: path.resolve(__dirname, '../cmd/claude-monitor/static'),
    emptyDir: true,
  },
  server: {
    port: 5173,
    proxy: {
      '/api': 'http://localhost:7700',
      '/ws': {
        target: 'ws://localhost:7700',
        ws: true,
      },
      '/health': 'http://localhost:7700',
    },
  },
});
