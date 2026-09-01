import { defineConfig } from 'vite'
import { svelte } from '@sveltejs/vite-plugin-svelte'
import { fileURLToPath } from 'node:url'

// Renders one component on its own, against fixed data, so a change to it can
// be looked at instead of argued about.
//
// This exists because two visual bugs shipped in one week that no test could
// have caught — invented Tailwind class names, which build clean and render
// unstyled, and a grid whose cells showed `atk_` and `pyro_dmg_` to a player.
// Both were obvious in a screenshot and invisible in a diff.
//
//   npx vite build --config vite.harness.js && (cd dist-harness && python3 -m http.server 8137)
//
// The alias replaces the api module, so the harness needs no server, no
// account and no game data. Keep the fixture's shape identical to what the
// endpoint returns — a mock that has drifted proves nothing. It once used
// camelCase stat keys while the API returns the GOOD format's `atk_`, and the
// screenshot looked fine while production would not have.
export default defineConfig({
  plugins: [svelte()],
  resolve: {
    alias: [
      {
        find: /^\.\/api\.js$/,
        replacement: fileURLToPath(new URL('./src/harness/mockapi.js', import.meta.url)),
      },
    ],
  },
  build: {
    outDir: 'dist-harness',
    emptyOutDir: true,
    rollupOptions: { input: 'harness.html' },
  },
})
