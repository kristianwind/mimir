import { describe, expect, it } from 'vitest'
import { readdirSync, readFileSync, statSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { dictionaries, translate } from './i18n.js'

// fileURLToPath rather than URL.pathname: this repository's own path contains
// an "æ", and pathname hands back the percent-encoded form, which no
// filesystem call can open.
const SRC = dirname(fileURLToPath(import.meta.url))

function sources(dir = SRC, out = []) {
  for (const name of readdirSync(dir)) {
    const path = join(dir, name)
    if (statSync(path).isDirectory()) sources(path, out)
    else if (/\.(svelte|js)$/.test(name) && !name.endsWith('.test.js')) out.push(path)
  }
  return out
}

/** Every t('…') call site, including the multi-line form the formatter emits. */
function calledSources() {
  const found = new Set()
  const direct = /\bt\(\s*'((?:[^'\\]|\\.)*)'/gs
  // Labels held in arrays or maps and translated later via t(item.label).
  const indirect = /(?:label|hint):\s*'((?:[^'\\]|\\.)*)'/g
  // Proper nouns keep their own spelling in both languages: the game's seven
  // elements are named the same in Danish, and a language is always listed in
  // the language it names.
  const properNouns = /(theme|i18n)\.js$/
  for (const path of sources()) {
    const text = readFileSync(path, 'utf8')
    for (const m of text.matchAll(direct)) found.add(m[1].replace(/\\'/g, "'"))
    if (properNouns.test(path)) continue
    for (const m of text.matchAll(indirect)) found.add(m[1].replace(/\\'/g, "'"))
  }
  return found
}

describe('translation coverage', () => {
  // A missing translation does not fail at runtime — it renders the English
  // source inside an otherwise Danish page, which reads as working software.
  it('has Danish for every source string in the UI', () => {
    const missing = [...calledSources()].filter((s) => !(s in dictionaries.da))
    expect(missing).toEqual([])
  })

  it('finds source strings at all', () => {
    expect(calledSources().size).toBeGreaterThan(50)
  })

  // Placeholders that differ between a source and its translation drop a value
  // silently: "{n} resin" against "resin" renders no number and throws nothing.
  it('keeps the same placeholders in every translation', () => {
    const holders = (s) => (s.match(/\{[a-zA-Z]+\}/g) ?? []).sort()
    for (const [source, translated] of Object.entries(dictionaries.da)) {
      expect(holders(translated), `placeholders differ for "${source}"`).toEqual(holders(source))
    }
  })
})

describe('translate', () => {
  it('falls back to the source when nothing is translated', () => {
    expect(translate('da', 'a sentence nobody has translated')).toBe(
      'a sentence nobody has translated',
    )
  })

  it('treats English as the source', () => {
    expect(translate('en', 'Log out')).toBe('Log out')
    expect(translate('da', 'Log out')).toBe('Log ud')
  })

  it('substitutes named placeholders', () => {
    expect(translate('da', '{n} resin', { n: 20 })).toBe('20 resin')
    expect(translate('da', 'Delete {name}?', { name: 'kw' })).toBe('Slet kw?')
  })
})
