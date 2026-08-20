<script>
  import { api } from './api.js'

  let { account, ongotogoals } = $props()

  let plan = $state(null)
  let error = $state(null)
  let loading = $state(true)
  let filter = $state('') // '' = every goal

  const KIND_LABELS = {
    reequip: 'Omrokering',
    weapon: 'Våben',
    talent: 'Talent',
    ascend: 'Level',
    level: 'Level',
    farm: 'Farm',
  }

  $effect(() => {
    const id = account.id
    loading = true
    error = null
    plan = null
    api
      .accountPlan(id)
      .then((data) => (plan = data.plan))
      .catch((err) => (error = err))
      .finally(() => (loading = false))
  })

  const goals = $derived([...new Set((plan?.ranked ?? []).map((a) => a.goal))])
  const shown = $derived((plan?.ranked ?? []).filter((a) => !filter || a.goal === filter))
  const actionable = $derived(shown.filter((a) => !a.blockedBy).length)

  function pct(v) {
    return `${(v * 100).toFixed(2)} %`
  }

  function cost(a) {
    if (a.free) return 'gratis'
    if (a.unpriced) return 'ikke prissat'
    return `${Math.round(a.resinCost)} resin`
  }
</script>

{#if loading}
  <p class="text-sm text-muted">Regner…</p>
{:else if error && error.status === 404}
  <div class="card p-8">
    <h2 class="text-lg font-medium">{error.message}</h2>
    <p class="mt-2 max-w-prose text-sm text-muted">
      Planen rangerer hver mulig opgradering på tværs af hele din konto efter forventet
      skadesgevinst pr. resin — de gratis omrokeringer først, derefter talenter, niveauer og
      artifact-domæner. Den kræver mindst ét mål: hvilken karakter, og hvilken rotation gevinsten
      skal måles på.
    </p>
    <button class="btn-primary mt-6" onclick={ongotogoals}>Opret et mål</button>
  </div>
{:else if error}
  <div class="card p-8">
    <h2 class="text-lg font-medium">{error.message}</h2>
    {#if error.hint}<p class="mt-2 text-sm text-muted">{error.hint}</p>{/if}
  </div>
{:else if plan}
  {#if goals.length > 1}
    <div class="mb-5 flex flex-wrap gap-2">
      <button type="button" class="chip {filter === '' ? 'border-accent text-ink' : ''}" onclick={() => (filter = '')}>
        Alle mål
      </button>
      {#each goals as goal (goal)}
        <button type="button" class="chip {filter === goal ? 'border-accent text-ink' : ''}" onclick={() => (filter = goal)}>
          {goal}
        </button>
      {/each}
    </div>
  {/if}

  {#if shown.length === 0}
    <div class="card p-8 text-center">
      <p class="text-muted">Ingen opgraderinger fundet — dine builds er allerede det bedste, dine ting kan give.</p>
    </div>
  {:else}
    <p class="mb-4 text-sm text-muted">
      {actionable} ting du kan gøre nu{shown.length > actionable
        ? `, ${shown.length - actionable} blokeret`
        : ''}.
    </p>

    <ol class="space-y-3">
      {#each shown as action, index (action.goal + action.kind + action.subject + index)}
        <li class="card p-4 {action.blockedBy ? 'opacity-70' : ''}">
          <div class="flex flex-wrap items-start gap-4">
            <span
              class="grid h-8 w-8 shrink-0 place-items-center rounded-lg text-sm font-medium
                     {action.blockedBy ? 'bg-raised text-muted' : action.free ? 'bg-good/20 text-good' : 'bg-accent/15'}"
            >
              {index + 1}
            </span>

            <div class="min-w-48 flex-1">
              <p class="font-medium">{action.headline}</p>
              <p class="mt-0.5 text-xs text-muted">
                <span class="text-accent">{action.goal}</span>
                · {KIND_LABELS[action.kind] ?? action.kind}
                {#if action.note}· {action.note}{/if}
              </p>
              {#if action.blockedBy}
                <p class="mt-1 text-xs text-warn">Blokeret: {action.blockedBy}</p>
              {/if}
              {#if action.kind === 'farm' && action.detail}
                <p class="mt-1 text-xs text-muted">
                  median {pct(action.detail.medianGain)} · spænd {pct(action.detail.p10Gain)}–{pct(
                    action.detail.p90Gain,
                  )} · giver intet {(action.detail.noImprovementChance * 100).toFixed(0)} % af gangene
                </p>
              {/if}
            </div>

            <div class="text-right">
              <p class="text-lg font-medium {action.blockedBy ? 'text-muted' : 'text-good'}">
                +{pct(action.gainPct)}
              </p>
              <p class="text-xs text-muted">{cost(action)}</p>
              {#if action.efficiency > 0}
                <p class="text-xs text-muted">{(action.efficiency * 100).toFixed(2)} %/100 resin</p>
              {/if}
            </div>
          </div>
        </li>
      {/each}
    </ol>
  {/if}

  {#if plan.conflicts?.length}
    <div class="card mt-6 p-4">
      <h3 class="text-sm font-medium">Kampen om udstyret</h3>
      <ul class="mt-2 space-y-1 text-xs text-muted">
        {#each plan.conflicts as conflict}
          <li>
            <span class="text-ink">{conflict.wants}</span> vil have
            <span class="text-ink">{conflict.item}</span> fra
            <span class="text-ink">{conflict.holds}</span> — {conflict.resolution}
          </li>
        {/each}
      </ul>
    </div>
  {/if}

  {#if plan.plans?.some((p) => p.skipped?.length) || plan.caveats?.length}
    <div class="card mt-4 p-4">
      <h3 class="text-sm font-medium">Forbehold</h3>
      <ul class="mt-2 space-y-1 text-xs text-muted">
        {#each plan.caveats ?? [] as line}
          <li>· {line}</li>
        {/each}
        {#each plan.plans ?? [] as p}
          {#each p.skipped ?? [] as line}
            <li>· {p.goal}: {line}</li>
          {/each}
        {/each}
      </ul>
    </div>
  {/if}
{/if}
