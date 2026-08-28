<script>
  /**
   * One account against another, on the same ruler.
   *
   * What people ask for is a percentile against everybody. Mimir does not have
   * that and says so on the page rather than approximating it: the public
   * leaderboards are built from accounts whose owners chose to submit them, so
   * a rank against that population measures who bothers to publish.
   *
   * What it can do is exact. Somebody publishes a showcase; Mimir fetches it,
   * runs the same yardstick over it that it runs over yours, and puts the two
   * numbers next to each other. Weakest first, because the point is what to
   * work on.
   */
  import { api } from './api.js'
  import CharacterArt from './CharacterArt.svelte'

  let { account } = $props()

  let uid = $state('')
  let result = $state(null)
  let error = $state(null)
  let loading = $state(false)
  let open = $state(null)

  async function compare() {
    if (!uid.trim()) return
    loading = true
    error = null
    result = null
    try {
      result = await api.compare(account.id, uid.trim())
    } catch (err) {
      error = err
    } finally {
      loading = false
    }
  }

  function score(v) {
    return Math.round(v).toLocaleString('en')
  }

  function gap(ratio) {
    if (!ratio) return '—'
    const pct = (ratio - 1) * 100
    const sign = pct >= 0 ? '+' : ''
    return `${sign}${pct.toFixed(0)} %`
  }

  function stat(m, key, asPercent = true) {
    const v = m.stats?.[key]
    if (v === undefined) return '—'
    return asPercent ? `${(v * 100).toFixed(1)} %` : Math.round(v).toLocaleString('en')
  }

  function sets(m) {
    return Object.entries(m.sets ?? {})
      .filter(([, n]) => n >= 2)
      .map(([k, n]) => `${n}pc ${k}`)
      .join(' + ')
  }
</script>

<div class="card mb-6 p-5">
  <h2 class="font-medium">Compare against another account</h2>
  <p class="mt-2 max-w-prose text-sm text-muted">
    Enter a UID whose owner has switched their showcase on. Mimir reads what they published,
    measures it with the same yardstick it measures you with, and shows both. Nothing about your
    account is sent anywhere, and nothing about theirs is kept.
  </p>

  <form class="mt-4 flex flex-wrap gap-2" onsubmit={(e) => (e.preventDefault(), compare())}>
    <input
      class="field w-48 font-mono"
      bind:value={uid}
      placeholder="UID, e.g. 800000001"
      inputmode="numeric"
    />
    <button class="btn-primary" disabled={loading || !uid.trim()}>
      {loading ? 'Measuring…' : 'Compare'}
    </button>
  </form>

  <p class="mt-3 max-w-prose text-xs text-muted">
    There is no "how do I rank against everyone". The public leaderboards are made of accounts whose
    owners chose to submit them, so a position in one says more about who publishes than about
    whether a build is good — and Mimir will not print a number it cannot stand behind.
  </p>
</div>

{#if error}
  <div class="card p-6">
    <h3 class="font-medium">{error.message}</h3>
    {#if error.hint}<p class="mt-2 text-sm text-muted">{error.hint}</p>{/if}
  </div>
{/if}

{#if result}
  <div class="card mb-4 p-4">
    <p class="text-sm">
      <span class="font-medium">{result.nickname || result.uid}</span>
      {#if result.arLevel}<span class="text-muted"> · AR {result.arLevel}</span>{/if}
      <span class="text-muted"> · {result.characters.length} characters in common</span>
    </p>
    {#if result.stale}
      <p class="mt-1 text-xs text-warn">
        Enka could not be reached, so this showcase came from a cache and may be out of date.
      </p>
    {/if}
  </div>

  <ol class="space-y-3">
    {#each result.characters as c (c.character)}
      <li class="card relative overflow-hidden p-4">
        <CharacterArt character={c.character} scrim="both" />

        <div class="relative flex flex-wrap items-start gap-4">
          <div class="min-w-48 flex-1">
            <p class="font-medium">{c.character}</p>
            <p class="mt-0.5 text-xs text-muted">
              you: C{c.yours.constellation} · lv {c.yours.level} ·
              {c.yours.weapon ?? 'no weapon'}{c.yours.refinement ? ` R${c.yours.refinement}` : ''}
            </p>
            <p class="text-xs text-muted">
              them: C{c.theirs.constellation} · lv {c.theirs.level} ·
              {c.theirs.weapon ?? 'no weapon'}{c.theirs.refinement ? ` R${c.theirs.refinement}` : ''}
            </p>
          </div>

          <div class="text-right">
            <p class="text-lg font-medium {c.ratio >= 1 ? 'text-good' : 'text-warn'}">
              {gap(c.ratio)}
            </p>
            <p class="text-xs text-muted">{score(c.yours.score)} vs {score(c.theirs.score)}</p>
          </div>
        </div>

        <button
          type="button"
          class="btn-ghost mt-3 w-full text-xs"
          onclick={() => (open = open === c.character ? null : c.character)}
        >
          {open === c.character ? 'Hide the stats' : 'Where the difference is'}
        </button>

        {#if open === c.character}
          <table class="mt-3 w-full text-xs">
            <thead class="text-muted">
              <tr>
                <th class="text-left font-normal"></th>
                <th class="text-right font-normal">you</th>
                <th class="text-right font-normal">them</th>
              </tr>
            </thead>
            <tbody>
              {#each [['Crit rate', 'critRate_', true], ['Crit damage', 'critDMG_', true], ['ATK', 'atk', false], ['Energy recharge', 'enerRech_', true], ['Elemental mastery', 'eleMas', false]] as [label, key, asPct]}
                <tr>
                  <td class="py-0.5 text-muted">{label}</td>
                  <td class="py-0.5 text-right">{stat(c.yours, key, asPct)}</td>
                  <td class="py-0.5 text-right">{stat(c.theirs, key, asPct)}</td>
                </tr>
              {/each}
              <tr>
                <td class="py-0.5 text-muted">Talents</td>
                <td class="py-0.5 text-right">
                  {c.yours.talents.auto}/{c.yours.talents.skill}/{c.yours.talents.burst}
                </td>
                <td class="py-0.5 text-right">
                  {c.theirs.talents.auto}/{c.theirs.talents.skill}/{c.theirs.talents.burst}
                </td>
              </tr>
              <tr>
                <td class="py-0.5 text-muted">Set</td>
                <td class="py-0.5 text-right">{sets(c.yours) || '—'}</td>
                <td class="py-0.5 text-right">{sets(c.theirs) || '—'}</td>
              </tr>
            </tbody>
          </table>
          {#if c.yours.measuredOn?.length}
            <p class="mt-2 text-xs text-muted">
              Measured on: {c.yours.measuredOn.join(', ')}.
            </p>
          {/if}
        {/if}
      </li>
    {/each}
  </ol>

  {#if result.onlyTheirs?.length || result.onlyYours?.length}
    <div class="card mt-4 p-4 text-xs text-muted">
      {#if result.onlyTheirs?.length}
        <p>Only on their showcase: {result.onlyTheirs.join(', ')}.</p>
      {/if}
      {#if result.onlyYours?.length}
        <p class="mt-1">
          Only on yours: {result.onlyYours.slice(0, 12).join(', ')}{result.onlyYours.length > 12
            ? `, and ${result.onlyYours.length - 12} more`
            : ''}.
        </p>
      {/if}
    </div>
  {/if}

  {#if result.skipped?.length}
    <div class="card mt-4 p-4">
      <h3 class="text-sm font-medium">Not measured</h3>
      <ul class="mt-2 space-y-1 text-xs text-muted">
        {#each result.skipped as line}<li>· {line}</li>{/each}
      </ul>
    </div>
  {/if}

  <div class="card mt-4 p-4">
    <h3 class="text-sm font-medium">What this does not measure</h3>
    <ul class="mt-2 space-y-1.5 text-xs text-muted">
      {#each result.caveats as caveat}<li>· {caveat}</li>{/each}
    </ul>
  </div>
{/if}
