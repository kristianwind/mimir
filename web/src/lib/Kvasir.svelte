<script module>
  import { api } from './api.js'

  /**
   * The AI layer's availability, fetched once for the whole app.
   *
   * Module state rather than a prop: every page asks, the answer never
   * changes while the tab is open, and a card per page each doing its own
   * round trip would make an optional feature cost four requests on load.
   *
   * When it is off the cards render nothing at all. An optional feature that
   * is switched off should be invisible, not a row of boxes explaining that
   * the operator has not configured a model.
   */
  let statusPromise = null

  export function kvasirStatus() {
    if (!statusPromise) statusPromise = api.kvasirStatus().catch(() => ({ enabled: false }))
    return statusPromise
  }
</script>

<script>

  let { account, surface, subject = '', compact = false } = $props()

  let enabled = $state(false)
  let data = $state(null)
  let error = $state(null)
  let loading = $state(false)
  let showBrief = $state(false)

  kvasirStatus().then((s) => (enabled = !!s?.enabled))

  async function ask(refresh = false) {
    loading = true
    error = null
    if (refresh) data = null
    try {
      data = await api.kvasirOpinion(account.id, { surface, subject, refresh })
    } catch (err) {
      error = err
    } finally {
      loading = false
    }
  }

  // Re-asks when the page's subject changes.
  $effect(() => {
    const id = account?.id
    const key = `${surface}:${subject}`
    if (!enabled || !id || !key) return
    data = null
    error = null
    ask()
  })
</script>

{#if enabled}
  <section class="card p-4 sm:p-5 {compact ? '' : 'mb-6'}">
    <header class="flex flex-wrap items-center justify-between gap-3">
      <div class="flex items-center gap-2">
        <span class="grid h-7 w-7 place-items-center rounded-lg bg-accent/15 text-sm" aria-hidden="true">🜛</span>
        <h2 class="text-sm font-medium">What Mimir makes of this</h2>
        {#if data?.cached}
          <span class="chip">unchanged since last time</span>
        {/if}
      </div>
      <div class="flex items-center gap-2">
        {#if data}
          <button type="button" class="btn-ghost px-2.5 py-1 text-xs" onclick={() => (showBrief = !showBrief)}>
            {showBrief ? 'Hide the facts' : 'What was it told?'}
          </button>
        {/if}
        <button type="button" class="btn-ghost px-2.5 py-1 text-xs" disabled={loading} onclick={() => ask(true)}>
          Ask again
        </button>
      </div>
    </header>

    {#if loading}
      <p class="mt-3 text-sm text-muted">Reading the numbers…</p>
    {:else if error}
      <p class="mt-3 text-sm text-muted">{error.message}</p>
      {#if error.hint}<p class="mt-1 text-xs text-muted">{error.hint}</p>{/if}
    {:else if data}
      {#if data.opinion.verdict}
        <p class="mt-3 text-sm leading-relaxed">{data.opinion.verdict}</p>
      {/if}

      {#if data.opinion.points?.length}
        <ol class="mt-4 space-y-3">
          {#each data.opinion.points as point, i (point.headline + i)}
            <li class="rounded-xl bg-raised/60 p-3">
              <p class="text-sm font-medium">{point.headline}</p>
              <p class="mt-1 text-xs leading-relaxed text-muted">{point.why}</p>
              {#if point.do}
                <p class="mt-1 text-xs text-accent">{point.do}</p>
              {/if}
            </li>
          {/each}
        </ol>
      {/if}

      {#if data.opinion.questions?.length && !compact}
        <div class="mt-4">
          <h3 class="text-xs font-medium uppercase tracking-wide text-muted">
            What it would need to know
          </h3>
          <ul class="mt-1.5 space-y-1 text-xs text-muted">
            {#each data.opinion.questions as question}
              <li>· {question}</li>
            {/each}
          </ul>
        </div>
      {/if}

      <!--
        A number it could not source is removed before it reaches the
        page, and what was removed is said out loud. A silently edited
        opinion is not one the reader can weigh.
      -->
      {#if data.dropped?.length}
        <div class="mt-4 rounded-xl border border-warn/40 bg-warn/10 p-3">
          <p class="text-xs text-warn">
            {data.dropped.length} things it said were removed: they contained figures that
            are nowhere in the calculation.
          </p>
          <ul class="mt-1.5 space-y-1 text-xs text-muted">
            {#each data.dropped as cut}
              <li>· {cut.text} — {cut.numbers.join(', ')}</li>
            {/each}
          </ul>
        </div>
      {/if}

      {#if showBrief}
        <div class="mt-4">
          <p class="text-xs text-muted">
            This is everything it was given. It is the engine’s own output, and every figure in
            the answer had to appear in it.
          </p>
          <pre class="mt-2 max-h-96 overflow-auto rounded-xl bg-raised p-3 text-[11px] leading-relaxed text-muted whitespace-pre-wrap">{data.brief}</pre>
        </div>
      {/if}

      {#if data.model}
        <p class="mt-3 text-[11px] text-muted">Answered by {data.model}</p>
      {/if}
    {/if}
  </section>
{/if}
