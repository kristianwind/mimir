/**
 * Theme state.
 *
 * Kept deliberately tiny and dependency-free: the theme must be applied
 * before the first paint, so this module is imported at the very top of
 * main.js and reads localStorage synchronously. Anything heavier here shows
 * up as a flash of the wrong colour on every load.
 */

export const ELEMENTS = [
  { key: 'pyro', label: 'Pyro', hex: '#EF7938' },
  { key: 'hydro', label: 'Hydro', hex: '#4CC2F1' },
  { key: 'anemo', label: 'Anemo', hex: '#74C2A8' },
  { key: 'electro', label: 'Electro', hex: '#B08FC5' },
  { key: 'dendro', label: 'Dendro', hex: '#A5C83B' },
  { key: 'cryo', label: 'Cryo', hex: '#9FD6E3' },
  { key: 'geo', label: 'Geo', hex: '#FAB72E' },
]

const THEME_KEY = 'mimir.theme'
const MODE_KEY = 'mimir.mode'

/** @returns {string} the stored element theme, defaulting to anemo. */
export function storedTheme() {
  const v = localStorage.getItem(THEME_KEY)
  return ELEMENTS.some((e) => e.key === v) ? v : 'anemo'
}

/** @returns {'light'|'dark'|'system'} */
export function storedMode() {
  const v = localStorage.getItem(MODE_KEY)
  return v === 'light' || v === 'dark' || v === 'system' ? v : 'system'
}

/** Resolves 'system' against the OS preference. */
export function resolveMode(mode) {
  if (mode !== 'system') return mode
  return window.matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark'
}

export function applyTheme(theme, mode) {
  const root = document.documentElement
  root.dataset.theme = theme
  root.dataset.mode = resolveMode(mode)
  localStorage.setItem(THEME_KEY, theme)
  localStorage.setItem(MODE_KEY, mode)
}

/** Re-applies on OS change, but only while the user is on 'system'. */
export function watchSystemMode(getMode) {
  const mq = window.matchMedia('(prefers-color-scheme: light)')
  const onChange = () => {
    if (getMode() === 'system') {
      document.documentElement.dataset.mode = resolveMode('system')
    }
  }
  mq.addEventListener('change', onChange)
  return () => mq.removeEventListener('change', onChange)
}
