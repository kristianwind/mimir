<script>
  /**
   * One character, and what to do about them.
   *
   * Sabrina asked for two things on this page and they are the two sections
   * below: how far along the build is, and which piece is worth upgrading
   * next. Everything else on it exists to keep those two honest.
   *
   * The ordering is the argument. Slots come back from the server already
   * ranked by what is available in them, so the top row is the answer and the
   * rest is the working. Sorting them again here would be a second opinion.
   */
  import { api } from './api.js'
  import CharacterArt from './CharacterArt.svelte'
  import { statLabel } from './stats.js'

  let { account, characterKey, onback } = $props()

  let page = $state(null)
  let error = $state(null)
  let loading = $state(true)

  $effect(() => {
    const id = account.id
    const key = characterKey
    loading = true
    error = null
    api
      .character(id, key)
      .then((data) => (page = data))
      .catch((err) => (error = err))
      .finally(() => (loading = false))
  })

  const pct = (n) => `${(n * 100).toFixed(1)} %`

  // What the top row is telling you to do, in the words a player would use.
  // The server names the action; this is the only place it becomes a
  // sentence, so the wording lives here rather than in the JSON.
  function sentence(slot) {
    switch (slot.action) {
      case 'level':
        return `Level it to +${slot.levelTo}`
      case 'swap':
        return slot.swapWornBy
          ? `Swap to a piece ${slot.swapWornBy} is wearing`
          : 'Swap to a piece in your bag'
      case 'equip':
        return 'Put something in this slot'
      default:
        return 'Nothing available'
    }
  }
</script>

<button type="button" class="btn-ghost mb-4 text-sm" onclick={onback}>← Back to the roster</button>

{#if loading}
  <p class="text-sm text-muted">Measuring {characterKey}…</p>
{:else if error}
  <p class="rounded-xl border border-bad/40 bg-bad/10 px-4 py-3 text-sm text-bad">
    {error.message}{#if error.hint}<span class="mt-1 block text-muted">{error.hint}</span>{/if}
  </p>
{:else if page}
  <article class="card relative mb-4 overflow-hidden p-5">
    <CharacterArt character={page.character} />
    <div class="relative">
      <div class="flex flex-wrap items-baseline justify-between gap-2">
        <h1 class="text-xl font-medium tracking-tight">{page.character}</h1>
        <span class="chip backdrop-blur-sm">C{page.constellation}</span>
      </div>
      <p class="mt-1 text-sm text-muted">
        Level {page.level}{#if page.weapon}{' · '}{page.weapon}{/if}
      </p>

      <!--
        The status, and the sentence under it is not decoration. A bar that
        fills to 38% invites being read as a grade out of a hundred, and this
        one is a ratio against a build with perfect substats in all five
        slots — which nobody has. Saying so next to the number is cheaper than
        explaining it to everybody who asks why their finished character is
        "not even half built".
      -->
      <div class="mt-4 rounded-xl bg-raised/80 p-4 backdrop-blur-sm">
        <div class="flex items-baseline justify-between gap-2">
          <h2 class="text-sm font-medium">How far along this build is</h2>
          <span class="text-2xl font-semibold tracking-tight">{pct(page.built)}</span>
        </div>
        <div class="mt-2 h-2 overflow-hidden rounded-full bg-line">
          <div
            class="h-full rounded-full bg-accent"
            style="width: {Math.min(100, page.built * 100).toFixed(1)}%"
          ></div>
        </div>
        <p class="mt-2 text-xs leading-relaxed text-muted">
          Against an idealised build — right main stat in every slot, every substat roll allocated
          the way the target view allocates them, at +20. Nothing reaches a hundred, because no real
          account rolls perfectly five times. It is a ruler, not a grade.
        </p>
      </div>
    </div>
  </article>

  <section class="card mb-4 p-5">
    <h2 class="font-medium">What you get most out of upgrading</h2>
    <p class="mt-1 text-xs text-muted">
      Every slot, ranked by how much damage is sitting in it. The percentage is what this
      character's rotation gains.
    </p>

    <ul class="mt-4 space-y-2">
      {#each page.slots as slot (slot.slot)}
        <li class="rounded-xl bg-raised px-3 py-2.5">
          <div class="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1">
            <span class="text-sm font-medium capitalize">{slot.slot}</span>
            <span class="text-sm {slot.best > 0 ? 'text-good' : 'text-muted'}">
              {slot.best > 0 ? `+${pct(slot.best)}` : 'nothing available'}
            </span>
          </div>

          <p class="mt-0.5 text-xs text-muted">
            {#if slot.worn}
              {slot.worn.set} · {statLabel(slot.worn.mainStat)} · +{slot.worn.level} · scores {slot
                .worn.score.toFixed(0)} of 100
            {:else}
              empty{#if slot.ideal}{' · wants '}{statLabel(slot.ideal)}{/if}
            {/if}
          </p>

          {#if slot.best > 0}
            <p class="mt-1.5 text-xs">
              <span class="font-medium">{sentence(slot)}</span>
              {#if slot.action === 'level'}
                <span class="text-muted">
                  · main stat only, so the real gain is this or better
                </span>
              {/if}
            </p>
          {:else if slot.worn?.why}
            <p class="mt-1.5 text-xs text-muted">{slot.worn.why}</p>
          {/if}
        </li>
      {/each}
    </ul>
  </section>

  <!--
    The bag-independent half, and it is labelled as such in the heading.
    Everything above this point is about the account — level this, swap that —
    and a tester reading those rows reasonably asked whether Mimir only ever
    considers what she already owns. It does not, but the answer lived on a
    different screen, which is the same as not having it.
  -->
  {#if page.aim}
    <section class="card mb-4 p-5">
      <h2 class="font-medium">What to farm towards, whether or not you own it</h2>
      <p class="mt-1 text-xs text-muted">
        This half ignores your bag completely. It is what the character wants, computed against
        this constellation and these talent levels — so it can tell you to chase a set you have
        never seen a piece of.
      </p>

      {#if page.aim.mainStats}
        <p class="mt-4 text-sm">
          {['sands', 'goblet', 'circlet']
            .map((slot) => `${slot[0].toUpperCase()}${slot.slice(1)} ${statLabel(page.aim.mainStats[slot]) || '—'}`)
            .join(' · ')}
        </p>
      {/if}

      <ul class="mt-3 space-y-1.5">
        {#each (page.aim.sets ?? []).slice(0, 5) as set (set.config)}
          <li class="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1 text-sm">
            <span>
              4pc {set.config}
              {#if set.owned}<span class="text-good">· you have it</span>{/if}
              {#if !set.modelled}
                <span
                  class="text-warn"
                  title="This set's four-piece bonus is conditional wording rather than numbers, so
                         it is not in the score. The entry was ranked on its stats alone."
                >· stats only</span>
              {:else if set.undeclared?.length}
                <!--
                  Modelled, but scored with the bonus switched off, because
                  nobody has said whether the condition holds. Without this
                  the entry ranks low for an invisible reason and the
                  "modelled" flag reads as "priced".
                -->
                <span
                  class="text-warn"
                  title="Mimir has numbers for this set's four-piece, but it waits on a condition
                         you have not answered — {set.undeclared.join(', ')} — so it was scored
                         with that bonus off. Answer it on the goal and this entry moves."
                >· needs an answer</span>
              {/if}
            </span>
            <span class="shrink-0 text-muted">
              {set.behind ? `−${(set.behind * 100).toFixed(0)} %` : 'best'}
            </span>
          </li>
        {/each}
      </ul>

      <!--
        Not a footnote. Seven of sixty-one sets have a four-piece the engine
        can score, so for most of this list "best" means "best on its stats",
        which is a different claim from the one a reader will take away. It
        goes under the list at full weight rather than inside a details
        element somebody has to open.
      -->
      {#each page.aim.caveats ?? [] as caveat}
        {#if caveat.includes('four-piece')}
          <p class="mt-3 rounded-xl bg-raised px-3 py-2.5 text-xs leading-relaxed text-warn">
            {caveat}
          </p>
        {/if}
      {/each}
    </section>
  {/if}

  {#if page.substats?.length}
    <section class="card mb-4 p-5">
      <h2 class="font-medium">What the next substat roll should go to</h2>
      <p class="mt-1 text-xs text-muted">
        Measured on this build as it stands, not copied from a guide — so it moves as the build
        does. A stat stops being worth chasing the moment this says it has.
      </p>

      <ul class="mt-4 space-y-1.5">
        {#each page.substats as sub (sub.stat)}
          <li class="flex flex-wrap items-center gap-x-3 gap-y-1">
            <span class="w-32 shrink-0 text-xs">{statLabel(sub.stat)}</span>
            <span class="h-1.5 min-w-24 flex-1 overflow-hidden rounded-full bg-raised">
              <span
                class="block h-full rounded-full {sub.perRoll > 0 ? 'bg-accent' : 'bg-line'}"
                style="width: {Math.max(0, sub.relative * 100).toFixed(1)}%"
              ></span>
            </span>
            <span class="w-16 shrink-0 text-right text-xs tabular-nums text-muted">
              {sub.perRoll > 0 ? `+${(sub.perRoll * 100).toFixed(2)} %` : '—'}
            </span>
            <!--
              Six of the ten stats come back at zero on a typical build, and
              annotating all six in warning yellow buried the one that has to
              be read. So the colour follows what the zero means: yellow only
              where Mimir is the limit rather than the game, muted where the
              build genuinely cannot use the stat, and nothing at all for the
              flat "does not scale" case — the dash and the empty bar have
              already said it, and the full wording is in the caveats below.
            -->
            {#if sub.note && sub.unmeasured}
              <span class="basis-full text-xs text-warn">{sub.note}</span>
            {:else if sub.note && sub.stat === 'critRate_'}
              <span class="basis-full text-xs text-muted">{sub.note}</span>
            {/if}
          </li>
        {/each}
      </ul>
      <p class="mt-3 text-xs text-muted">Per roll, at the average of the four roll tiers.</p>
    </section>
  {/if}

  {#if page.caveats?.length || page.skipped?.length}
    <details class="card p-5">
      <summary class="cursor-pointer text-sm font-medium">What this does not measure</summary>
      <ul class="mt-3 space-y-2 text-xs leading-relaxed text-muted">
        {#each page.caveats ?? [] as caveat}<li>· {caveat}</li>{/each}
        {#each (page.aim?.caveats ?? []).filter((c) => !c.includes('four-piece')) as caveat}
          <li>· {caveat}</li>
        {/each}
        {#each page.skipped ?? [] as skip}<li class="text-warn">· {skip}</li>{/each}
      </ul>
    </details>
  {/if}
{/if}
