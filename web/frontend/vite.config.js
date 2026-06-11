import { defineConfig } from 'vite';

export default defineConfig({
  base: '/web/',
  build: {
    outDir: '../static',
    emptyOutDir: true
  },
  server: {
    open: '/web/upload'
  }
});
