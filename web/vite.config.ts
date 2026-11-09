import { svelte } from '@sveltejs/vite-plugin-svelte';
import { defineConfig } from 'vite';

// In development the panel's API runs separately on :8080; Vite passes /api through to it.
export default defineConfig({
  plugins: [svelte()],
  server: {
    proxy: { '/api': { target: 'http://localhost:8080', ws: true } },
  },
  build: { outDir: 'dist', emptyOutDir: true },
});
