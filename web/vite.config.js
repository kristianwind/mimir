import { defineConfig } from 'vite'
import { svelte } from '@sveltejs/vite-plugin-svelte'

export default defineConfig({
  plugins: [svelte()],
  build: {
    // Emitted into web/dist, which the Go binary embeds.
    outDir: 'dist',
    emptyOutDir: true,
  },
  server: {
    // In dev the Svelte app runs on 5173 and talks to the Go server on 8080.
    proxy: {
      '/api': 'http://localhost:8080',
    },
  },
})
