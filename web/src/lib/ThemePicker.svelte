<script>
  import { ELEMENTS } from './theme.js'

  let { theme, mode, setTheme } = $props()

  const MODES = [
    { key: 'light', label: 'Light', icon: '☀' },
    { key: 'dark', label: 'Dark', icon: '☾' },
    { key: 'system', label: 'System', icon: '◐' },
  ]

  const current = $derived(ELEMENTS.find((e) => e.key === theme) ?? ELEMENTS[0])
</script>

<!--
  Seven colour swatches in a row, permanently, for a preference somebody sets
  once and then never touches. A tester's first reaction to the app was that it
  was overwhelming, and this was a row of controls above the content competing
  with the page for attention every single visit.

  A select collapses them to one control and brings two things a row of
  buttons never had for free: it is a real listbox to a screen reader without
  hand-rolled radio semantics, and on a phone it opens the OS picker instead
  of asking for a 28-pixel tap target.

  Light and dark stay as they were. Three options that change the whole page
  are worth seeing at a glance, and they were never the noisy part.
-->
<div class="flex flex-wrap items-center gap-2">
  <div class="relative flex items-center">
    <span
      class="pointer-events-none absolute left-2 h-3.5 w-3.5 rounded-full border border-line/60"
      style="background: radial-gradient(circle at 30% 30%, {current.hex}, {current.hex}99)"
      aria-hidden="true"
    ></span>
    <select
      aria-label="Colour theme"
      value={theme}
      onchange={(e) => setTheme(e.currentTarget.value, mode)}
      class="cursor-pointer appearance-none rounded-xl border border-line bg-transparent
             py-1.5 pl-7 pr-7 text-xs text-muted transition hover:bg-raised
             focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2
             focus-visible:outline-accent"
    >
      {#each ELEMENTS as el (el.key)}
        <option value={el.key}>{el.label}</option>
      {/each}
    </select>
    <span class="pointer-events-none absolute right-2 text-[0.6rem] text-muted" aria-hidden="true">▾</span>
  </div>

  <div class="flex overflow-hidden rounded-xl border border-line" role="radiogroup" aria-label="Light mode">
    {#each MODES as m (m.key)}
      <button
        type="button"
        role="radio"
        aria-checked={mode === m.key}
        onclick={() => setTheme(theme, m.key)}
        class="px-2.5 py-1.5 text-xs transition
               {mode === m.key ? 'bg-accent text-accent-ink' : 'text-muted hover:bg-raised'}"
        title={m.label}
      >
        <span aria-hidden="true">{m.icon}</span>
        <span class="sr-only">{m.label}</span>
      </button>
    {/each}
  </div>
</div>
