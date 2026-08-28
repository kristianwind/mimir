<script>
  /**
   * The roster measured with one ruler.
   *
   * The plan needs a goal with a rotation before it can say anything, which
   * makes every character without one invisible. This page answers the
   * question before that: who is worth building at all, and what is the one
   * thing to do for each. It states its method on the page rather than in a
   * tooltip — a ranking whose ruler is hidden reads as a verdict.
   */
  import { api } from './api.js'
  import Kvasir from './Kvasir.svelte'

  let { account, ongotogoals } = $props()

  let ranking = $state(null)
  let error = $state(null)
  let loading = $state(true)
  let deriving = $state(false)
  let derived = $state(null)
  let showMethod = $state(false)
  let open = $state(null)

  // A character whose picture the server could not produce renders without
  // one rather than as an empty frame.
  let artless = $state(new Set())

  function noArt(key) {
    artless = new Set(artless).add(key)
  }

  $effect(() => {
    const id = account.id
    loading = true
    error = null
    ranking = null
    derived = null
    api
      .potential(id)
      .then((data) => (ranking = data))
      .catch((err) => (error = err))
      .finally(() => (loading = false))
  })

  async function deriveGoals() {
    deriving = true
    try {
      derived = await api.deriveGoals(account.id, {})
    } catch (err) {
      error = err
    } finally {
      deriving = false
    }
  }

  function pct(v) {
    return `${(v * 100).toFixed(1)} %`
  }

  function score(v) {
    return Math.round(v).toLocaleString('en')
  }

  const unlevelled = $derived(
    (ranking?.characters ?? []).flatMap((c) =>
      (c.actions ?? []).filter((a) => a.kind === 'artifact').map((a) => ({ character: c.character, ...a })),
    ),
  )
</script>

{#if loading}
  <p class="text-sm text-muted">Measuring every character…</p>
{:else if error}
  <div class="card p-8">
    <h2 class="text-lg font-medium">{error.message}</h2>
    {#if error.hint}<p class="mt-2 text-sm text-muted">{error.hint}</p>{/if}
  </div>
{:else if ranking}
  <Kvasir {account} surface="potential" />

  <div class="card mb-6 p-4">
    <div class="flex flex-wrap items-center justify-between gap-3">
      <div>
        <h2 class="text-sm font-medium">One ruler for the whole roster</h2>
        <p class="mt-0.5 text-xs text-muted">
          One cast of the elemental skill and one of the burst, at each character's own talent
          levels, against a level 90 enemy. No teams, no rotations, no resin.
        </p>
      </div>
      <button type="button" class="btn-ghost text-xs" onclick={() => (showMethod = !showMethod)}>
        {showMethod ? 'Hide the limits' : 'What this does not measure'}
      </button>
    </div>
    {#if showMethod}
      <ul class="mt-3 space-y-1.5 text-xs text-muted">
        {#each ranking.caveats ?? [] as caveat}
          <li>· {caveat}</li>
        {/each}
      </ul>
    {/if}

    <!--
      Why the same set keeps coming up. With the pieces for one arrangement
      in the bag, every character is told to switch to it — which reads as
      Mimir believing one set suits everybody, when what it means is that
      there is nothing to compare it against.
    -->
    {#if ranking.setConfigs?.length}
      <p class="mt-3 text-xs text-muted">
        Your inventory can assemble {ranking.setConfigs.length}
        {ranking.setConfigs.length === 1 ? 'set arrangement' : 'set arrangements'}:
        {ranking.setConfigs.slice(0, 6).join(', ')}{ranking.setConfigs.length > 6
          ? `, and ${ranking.setConfigs.length - 6} more`
          : ''}. Everything above is chosen from those — if one keeps winning, it may be
        that the others are not in the bag yet.
      </p>
    {/if}
  </div>

  <ol class="space-y-3">
    {#each ranking.characters as c, index (c.character)}
      <li class="card relative overflow-hidden p-4">
        <!--
          The same treatment as the roster: the game's own art behind the
          text, with a scrim that is a hard wall on the left. A ranking is
          read by scanning names, and a picture is faster to recognise than a
          key like KaedeharaKazuha.
        -->
        {#if !artless.has(c.character)}
          <img
            src="/api/art/{c.character}"
            alt=""
            aria-hidden="true"
            loading="lazy"
            onerror={() => noArt(c.character)}
            class="pointer-events-none absolute inset-y-0 right-0 h-full w-3/4 object-cover object-center"
          />
          <!--
            Opaque at both edges, not just the left. This card carries a name
            on one side and a score on the other, so the roster's single wall
            of colour leaves the numbers sitting on top of the picture and
            unreadable. The picture shows through the middle instead.
          -->
          <div
            class="pointer-events-none absolute inset-0 bg-gradient-to-r
                   from-surface from-30% via-surface/25 via-55% to-surface to-85%"
          ></div>
        {/if}

        <div class="relative flex flex-wrap items-start gap-4">
          <span class="grid h-8 w-8 shrink-0 place-items-center rounded-lg bg-accent/15 text-sm font-medium">
            {index + 1}
          </span>

          <div class="min-w-48 flex-1">
            <p class="font-medium">{c.character}</p>
            {#if c.topAction}
              <p class="mt-0.5 text-sm">{c.topAction.headline}</p>
              <p class="mt-0.5 text-xs text-muted">
                +{pct(c.topAction.gainPct)} · the biggest single upgrade available
                {#if c.topAction.blockedBy}· blocked: {c.topAction.blockedBy}{/if}
              </p>
            {:else}
              <p class="mt-0.5 text-xs text-muted">Nothing found: this build is the best its gear allows.</p>
            {/if}
          </div>

          <!--
            Headroom is the gain from rearranging gear you already own, and
            the top action is the largest upgrade of any kind. When the
            largest upgrade *is* the rearrangement they are the same number by
            construction — so showing both reads as two findings when there is
            one, and the more useful thing to say is that it costs nothing.
          -->
          <div class="text-right">
            <p class="text-lg font-medium text-good">+{score(c.topGain)}</p>
            <p class="text-xs text-muted">damage added</p>
            {#if c.topAction?.kind === 'reequip'}
              <p class="mt-1 text-xs text-accent">already yours — just re-equip</p>
            {:else if c.headroom > 0}
              <p class="mt-1 text-xs text-accent">
                {pct(c.headroom)} more sitting unequipped
              </p>
            {/if}
          </div>
        </div>

        {#if (c.actions ?? []).length > 1}
          <button
            type="button"
            class="btn-ghost relative mt-3 w-full text-xs backdrop-blur-sm"
            onclick={() => (open = open === c.character ? null : c.character)}
          >
            {open === c.character ? 'Hide' : `Everything for ${c.character} (${c.actions.length})`}
          </button>
        {/if}
        {#if open === c.character}
          <ul class="relative mt-3 space-y-2">
            {#each c.actions as a}
              <li class="rounded-xl bg-raised/60 p-3 text-xs">
                <p class="font-medium">{a.headline}</p>
                <p class="mt-0.5 text-muted">
                  +{pct(a.gainPct)}
                  {#if a.note}· {a.note}{/if}
                  {#if a.blockedBy}· blocked: {a.blockedBy}{/if}
                </p>
              </li>
            {/each}
          </ul>
          {#if (c.skipped ?? []).length}
            <ul class="mt-2 space-y-1 text-xs text-muted">
              {#each c.skipped as line}
                <li>· {line}</li>
              {/each}
            </ul>
          {/if}
        {/if}
      </li>
    {/each}
  </ol>

  <!--
    Called out on its own because it is the one upgrade that costs no resin and
    no domain run, and the one everybody forgets: a +8 piece on a finished
    build is free damage sitting in a drawer.
  -->
  {#if unlevelled.length}
    <div class="card mt-6 p-4">
      <h3 class="text-sm font-medium">Pieces that are not levelled</h3>
      <p class="mt-1 text-xs text-muted">
        Artifact experience is not resin. These are the equipped pieces still below their cap.
      </p>
      <ul class="mt-3 space-y-1.5 text-xs">
        {#each unlevelled as piece}
          <li>
            <span class="text-accent">{piece.character}</span>
            · {piece.headline} · <span class="text-good">+{pct(piece.gainPct)}</span>
          </li>
        {/each}
      </ul>
    </div>
  {/if}

  {#if (ranking.skipped ?? []).length}
    <div class="card mt-4 p-4">
      <h3 class="text-sm font-medium">Not measured</h3>
      <ul class="mt-2 space-y-1 text-xs text-muted">
        {#each ranking.skipped as line}
          <li>· {line}</li>
        {/each}
      </ul>
    </div>
  {/if}

  <div class="card mt-6 p-5">
    <h3 class="text-sm font-medium">Turn this into goals</h3>
    <p class="mt-2 max-w-prose text-xs text-muted">
      Mimir will write a goal for each character above, in this order, using the same two casts it
      measured them with. That rotation is Mimir's guess, not yours — and every number the plan
      reports is measured against it, so the goals are marked as derived until you open one and say
      what you actually press. A goal you wrote is never touched.
    </p>
    <button class="btn-primary mt-4" disabled={deriving} onclick={deriveGoals}>
      {deriving ? 'Writing goals…' : 'Create the goals'}
    </button>

    {#if derived}
      <p class="mt-3 text-sm text-good">{derived.created} goals created.</p>
      <ul class="mt-2 space-y-1 text-xs text-muted">
        {#each derived.goals as g}
          <li>
            {g.character}
            {#if g.created}· priority {g.priority} · {g.rotation}{:else}· {g.reason}{/if}
          </li>
        {/each}
      </ul>
      <button class="btn-ghost mt-3 text-xs" onclick={ongotogoals}>Open the goals</button>
    {/if}
  </div>
{/if}
