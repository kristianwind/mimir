/**
 * The active language.
 *
 * Module-level state rather than a prop threaded through the tree: every
 * component needs the language, and passing it down through Shell into each
 * page would put a `lang` prop on components that otherwise take none. Reading
 * it through `t()` keeps the call sites as short as the literal they replace.
 *
 * Mirrors the theme's local-first rule — see App.svelte — so a language picked
 * on the login screen survives logging in.
 */

import { LANGUAGES, translate } from './i18n.js'

const KEY = 'mimir.lang'

function stored() {
  const v = localStorage.getItem(KEY)
  if (LANGUAGES.some((l) => l.key === v)) return v
  // No stored choice: follow the browser rather than assuming. A Danish
  // browser gets Danish; everything else gets the source language.
  return (navigator.language || '').toLowerCase().startsWith('da') ? 'da' : 'en'
}

let current = $state(stored())

// Whether the language was chosen here during this page session. Same role as
// the theme's `touched`: a deliberate click beats the server, anything else
// defers to the account so the choice follows the user to a new device.
let touched = $state(false)

export function lang() {
  return current
}

export function langTouched() {
  return touched
}

/**
 * setLang switches language and remembers it.
 *
 * `deliberate` is false when the value comes from the server, so adopting an
 * account's stored language does not itself count as a local choice — that
 * would make the server's own value beat the server on the next load.
 */
export function setLang(next, { deliberate = true } = {}) {
  if (!LANGUAGES.some((l) => l.key === next)) return
  current = next
  if (deliberate) touched = true
  applyLang(next)
}

/** Sets the document language and persists it. Also called before first paint. */
export function applyLang(next) {
  document.documentElement.lang = next
  localStorage.setItem(KEY, next)
}

/** Renders an English source string in the active language. */
export function t(source, vars) {
  return translate(current, source, vars)
}

export { LANGUAGES }
