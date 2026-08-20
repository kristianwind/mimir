<script>
  import { ELEMENTS } from './theme.js'

  let { theme, mode, setTheme } = $props()

  const MODES = [
    { key: 'light', label: 'Lys', icon: '☀' },
    { key: 'dark', label: 'Mørk', icon: '☾' },
    { key: 'system', label: 'System', icon: '◐' },
  ]
</script>

<div class="flex flex-wrap items-center gap-3">
  <div class="flex items-center gap-1.5" role="radiogroup" aria-label="Elementtema">
    {#each ELEMENTS as el (el.key)}
      <button
        type="button"
        role="radio"
        aria-checked={theme === el.key}
        aria-label={el.label}
        title={el.label}
        onclick={() => setTheme(el.key, mode)}
        class="relative h-7 w-7 rounded-full border transition
               focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2
               focus-visible:outline-accent
               {theme === el.key
                 ? 'border-ink/60 scale-110 shadow-glow'
                 : 'border-line/60 hover:scale-105 opacity-70 hover:opacity-100'}"
        style="background: radial-gradient(circle at 30% 30%, {el.hex}, {el.hex}99)"
      ></button>
    {/each}
  </div>

  <div class="flex overflow-hidden rounded-xl border border-line" role="radiogroup" aria-label="Lystilstand">
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
