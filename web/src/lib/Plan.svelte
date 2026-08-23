<script>
  import { api } from './api.js'
  import { t } from './lang.svelte.js'
  import Kvasir from './Kvasir.svelte'

  let { account, ongotogoals } = $props()

  let plan = $state(null)
  let error = $state(null)
  let loading = $state(true)
  let filter = $state('') // '' = every goal

  const KIND_LABELS = {
    reequip: 'Rearrange',
    weapon: 'Weapon',
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
    if (a.free) return t('free')
    if (a.unpriced) return t('not priced')
    return t('{n} resin', { n: Math.round(a.resinCost) })
  }
</script>

{#if loading}
  <p class="text-sm text-muted">{t('Calculating…')}</p>
{:else if error && error.status === 404}
  <div class="card p-8">
    <h2 class="text-lg font-medium">{error.message}</h2>
    <p class="mt-2 max-w-prose text-sm text-muted">
      {t(
        'The plan ranks every possible upgrade across your whole account by expected damage gained per resin — the free rearrangements first, then talents, levels and artifact domains. It needs at least one goal: which character, and which rotation the gain is measured on.',
      )}
    </p>
    <button class="btn-primary mt-6" onclick={ongotogoals}>{t('Create a goal')}</button>
  </div>
{:else if error}
  <div class="card p-8">
    <h2 class="text-lg font-medium">{error.message}</h2>
    {#if error.hint}<p class="mt-2 text-sm text-muted">{error.hint}</p>{/if}
  </div>
{:else if plan}
  <Kvasir {account} surface="plan" />

  {#if goals.length > 1}
    <div class="mb-5 flex flex-wrap gap-2">
      <button type="button" class="chip {filter === '' ? 'border-accent text-ink' : ''}" onclick={() => (filter = '')}>
        {t('All goals')}
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
      <p class="text-muted">
        {t('No upgrades found — your builds are already the best your gear allows.')}
      </p>
    </div>
  {:else}
    <p class="mb-4 text-sm text-muted">
      {t('{n} things you can do now', { n: actionable })}{shown.length > actionable
        ? t(', {n} blocked', { n: shown.length - actionable })
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
                · {t(KIND_LABELS[action.kind] ?? action.kind)}
                {#if action.note}· {action.note}{/if}
              </p>
              {#if action.blockedBy}
                <p class="mt-1 text-xs text-warn">{t('Blocked: {what}', { what: action.blockedBy })}</p>
              {/if}
              {#if action.kind === 'farm' && action.detail}
                <p class="mt-1 text-xs text-muted">
                  {t('median {median} · spread {low}–{high} · gives nothing {none} of the time', {
                    median: pct(action.detail.medianGain),
                    low: pct(action.detail.p10Gain),
                    high: pct(action.detail.p90Gain),
                    none: `${(action.detail.noImprovementChance * 100).toFixed(0)} %`,
                  })}
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
      <h3 class="text-sm font-medium">{t('The fight over the gear')}</h3>
      <ul class="mt-2 space-y-1 text-xs text-muted">
        {#each plan.conflicts as conflict}
          <li>
            {t('{wants} wants {item} from {holds} — {resolution}', {
              wants: conflict.wants,
              item: conflict.item,
              holds: conflict.holds,
              resolution: conflict.resolution,
            })}
          </li>
        {/each}
      </ul>
    </div>
  {/if}

  {#if plan.plans?.some((p) => p.skipped?.length) || plan.caveats?.length}
    <div class="card mt-4 p-4">
      <h3 class="text-sm font-medium">{t('Caveats')}</h3>
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
