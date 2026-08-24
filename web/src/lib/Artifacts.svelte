<script>
  import { api } from './api.js'
  import Kvasir from './Kvasir.svelte'

  let { account } = $props()

  const SLOTS = ['flower', 'plume', 'sands', 'goblet', 'circlet']
  const SLOT_LABELS = {
    flower: 'Flower',
    plume: 'Plume',
    sands: 'Sands',
    goblet: 'Goblet',
    circlet: 'Circlet',
  }

  let artifacts = $state([])
  let error = $state('')
  let loading = $state(true)
  let slot = $state('')

  $effect(() => {
    const id = account.id
    loading = true
    api
      .artifacts(id)
      .then((data) => (artifacts = data ?? []))
      .catch((err) => (error = err.message))
      .finally(() => (loading = false))
  })

  const shown = $derived(slot ? artifacts.filter((a) => a.slotKey === slot) : artifacts)

  // Crit value: 2×CR% + CD%. A triage number, not a verdict — the optimizer
  // decides what actually goes on a character.
  function critValue(artifact) {
    let cv = 0
    for (const sub of artifact.substats ?? []) {
      if (sub.key === 'critRate_') cv += 2 * sub.value * 100
      if (sub.key === 'critDMG_') cv += sub.value * 100
    }
    return cv
  }

  function display(sub) {
    const percent = sub.key.endsWith('_')
    const value = percent ? `${(sub.value * 100).toFixed(1)}%` : Math.round(sub.value)
    return `${sub.key.replace(/_$/, '')} +${value}`
  }
</script>

{#if artifacts.length}
  <Kvasir {account} surface="artifacts" />
{/if}

<div class="mb-4 flex flex-wrap items-center gap-2">
  <button type="button" class="chip {slot === '' ? 'border-accent text-ink' : ''}" onclick={() => (slot = '')}>
    All ({artifacts.length})
  </button>
  {#each SLOTS as s (s)}
    <button type="button" class="chip {slot === s ? 'border-accent text-ink' : ''}" onclick={() => (slot = s)}>
      {SLOT_LABELS[s]}
    </button>
  {/each}
</div>

{#if loading}
  <p class="text-sm text-muted">Fetching artifacts…</p>
{:else if error}
  <p class="rounded-xl border border-bad/40 bg-bad/10 px-4 py-3 text-sm text-bad">{error}</p>
{:else if artifacts.length === 0}
  <div class="card p-8 text-center">
    <p class="text-muted">No inventory yet.</p>
    <p class="mt-2 text-sm text-muted">
      Enka only gives the equipped pieces. Upload a .good file from Inventory Kamera for the whole
      inventory.
    </p>
  </div>
{:else}
  <div class="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
    {#each shown.slice(0, 120) as artifact (artifact.id)}
      <article class="card p-4">
        <div class="flex items-baseline justify-between gap-2">
          <h2 class="truncate text-sm font-medium">{artifact.setKey}</h2>
          <span class="chip">+{artifact.level}</span>
        </div>
        <p class="mt-1 text-xs text-muted">
          {SLOT_LABELS[artifact.slotKey]} · {artifact.mainStatKey}
          {#if artifact.location}· {artifact.location}{/if}
        </p>
        <ul class="mt-3 space-y-1 text-xs text-muted">
          {#each artifact.substats ?? [] as sub}
            <li>{display(sub)}</li>
          {/each}
        </ul>
        <p class="mt-3 text-xs">
          <span class="text-muted">CV</span>
          <span class="font-medium">{critValue(artifact).toFixed(1)}</span>
        </p>
      </article>
    {/each}
  </div>
  {#if shown.length > 120}
    <p class="mt-4 text-center text-xs text-muted">
      Showing 120 of {shown.length}. Filtering and sorting arrive with the optimizer view.
    </p>
  {/if}
{/if}
