/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{svelte,js}'],
  // Bound to the app's own switch rather than to prefers-color-scheme. The
  // mode is a stored preference on <html>, and a user who picked light while
  // their OS is dark would otherwise get dark: styles on a light page.
  darkMode: ['selector', '[data-mode="dark"]'],
  theme: {
    extend: {
      // Every colour is a CSS custom property so the seven element themes
      // swap by changing one attribute on <html>, with no rebuild and no
      // duplicated utility classes.
      colors: {
        bg: 'rgb(var(--bg) / <alpha-value>)',
        surface: 'rgb(var(--surface) / <alpha-value>)',
        raised: 'rgb(var(--raised) / <alpha-value>)',
        line: 'rgb(var(--line) / <alpha-value>)',
        ink: 'rgb(var(--ink) / <alpha-value>)',
        muted: 'rgb(var(--muted) / <alpha-value>)',
        accent: 'rgb(var(--accent) / <alpha-value>)',
        'accent-soft': 'rgb(var(--accent-soft) / <alpha-value>)',
        'accent-ink': 'rgb(var(--accent-ink) / <alpha-value>)',
        good: 'rgb(var(--good) / <alpha-value>)',
        warn: 'rgb(var(--warn) / <alpha-value>)',
        bad: 'rgb(var(--bad) / <alpha-value>)',
      },
      fontFamily: {
        sans: ['Inter var', 'Inter', 'system-ui', '-apple-system', 'Segoe UI', 'sans-serif'],
        mono: ['ui-monospace', 'SFMono-Regular', 'Menlo', 'monospace'],
      },
      boxShadow: {
        glow: '0 0 0 1px rgb(var(--accent) / 0.25), 0 8px 32px -8px rgb(var(--accent) / 0.35)',
      },
    },
  },
  plugins: [],
}
