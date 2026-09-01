<script>
  import { api } from './api.js'
  import { statLabel } from './stats.js'

  let { account, characters = [] } = $props()

  const SLOTS = ['flower', 'plume', 'sands', 'goblet', 'circlet']
  const SLOT_LABELS = {
    flower: 'Flower',
    plume: 'Plume',
    sands: 'Sands',
    goblet: 'Goblet',
    circlet: 'Circlet',
  }

  // Eight is the server's cap and it is not arbitrary: every character is
  // scored against every artifact in the bag, so the wait grows with the list.
  const LIMIT = 8

  let picked = $state([])
  let rows = $state([])
  let missing = $state([])
  let error = $state('')
  let busy = $state(false)
  let ran = $state(false)

  const roster = $derived([...characters].sort((a, b) => a.key.localeCompare(b.key)))

  function toggle(key) {
    if (picked.includes(key)) picked = picked.filter((k) => k !== key)
    else if (picked.length < LIMIT) picked = [...picked, key]
  }

  async function run() {
    if (picked.length === 0) return
    busy = true
    error = ''
    try {
      const data = await api.artifactGrid(account.id, picked)
      rows = data.rows ?? []
      missing = data.missing ?? []
      ran = true
    } catch (err) {
      error = err.message
    } finally {
      busy = false
    }
  }

  // The worn piece is what the grid shows: the question is "what should I
  // upgrade", and that is about what is on the character, not about the best
  // thing in the bag. The best thing in the bag is the reason a cell is
  // yellow, and it says so.
  function wornIn(row, slot) {
    const s = row.slots?.find((x) => x.slot === slot)
    if (!s) return null
    return s.pieces?.find((p) => p.worn) ?? null
  }

  function idealFor(row, slot) {
    return row.slots?.find((x) => x.slot === slot)?.ideal ?? ''
  }

  const TONE = {
    good: 'border-good/50 bg-good/20 text-good',
    ok: 'border-warn/50 bg-warn/20 text-warn',
    replace: 'border-bad/50 bg-bad/20 text-bad',
  }

  // Colour alone cannot carry the verdict. Around one man in twelve cannot
  // separate the red cell from the green one, and in the light theme the
  // yellow and the green sit close together even for those who can. So every
  // verdict that asks for something also carries a mark; "nothing to do" is
  // the common case and stays unmarked, which keeps a finished row quiet.
  const MARK = { ok: '\u2022', replace: '\u25B2' }

  // The substats a piece actually rolled. They are already in the score — the
  // damage engine reads the whole stat block — but nothing on the page said
  // so, and the first question a reader asked was whether only main stats
  // were being ranked.
  function rolls(p) {
    if (!p.substats?.length) return 'none'
    return p.substats
      .map((s) => `${statLabel(s.key)} ${s.value < 1 ? (s.value * 100).toFixed(1) + '%' : Math.round(s.value)}`)
      .join(', ')
  }
</script>

<!--
  Sabrina's spreadsheet, with the colours computed instead of typed. She keeps
  characters down the side and the five slots across, each cell coloured by
  hand from a wiki; this is the same shape with the engine filling it in.

  Choosing who is in it was her request — "hvor jeg kan vælge til og fra" — and
  it is also what makes the view possible. Scoring one piece is a damage
  calculation and a full account is thousands of them.
-->
<section class="card p-5">
  <h2 class="font-medium">Which pieces to upgrade</h2>
  <p class="mt-2 max-w-prose text-sm text-muted">
    Every slot on the characters you choose, scored against what that character actually wants.
    Zero is the slot left empty and a hundred is the ideal piece for them — so sixty means the same
    thing on everybody. Pick up to {LIMIT}: each one is measured against every artifact in the bag.
  </p>

  <div class="mt-4 flex flex-wrap gap-1.5">
    {#each roster as c (c.key)}
      <button
        type="button"
        disabled={!picked.includes(c.key) && picked.length >= LIMIT}
        onclick={() => toggle(c.key)}
        class="chip whitespace-nowrap disabled:opacity-40
               {picked.includes(c.key) ? 'border-accent text-ink' : ''}"
      >
        {c.key}
      </button>
    {/each}
  </div>

  <div class="mt-4 flex flex-wrap items-center gap-3">
    <button class="btn-primary" disabled={busy || picked.length === 0} onclick={run}>
      {busy ? 'Measuring…' : `Measure ${picked.length || ''}`.trim()}
    </button>
    {#if picked.length > 0}
      <button class="btn-ghost text-sm" onclick={() => (picked = [])}>Clear</button>
    {/if}
  </div>

  {#if error}
    <p class="mt-4 text-sm text-warn">{error}</p>
  {/if}

  {#if ran && rows.length > 0}
    <div class="mt-5 overflow-x-auto">
      <table class="w-full min-w-[38rem] border-separate border-spacing-1 text-left text-sm">
        <thead class="text-xs uppercase tracking-wide text-muted">
          <tr>
            <th class="px-2 py-1 font-medium">Character</th>
            {#each SLOTS as slot (slot)}
              <th class="px-2 py-1 font-medium">{SLOT_LABELS[slot]}</th>
            {/each}
          </tr>
        </thead>
        <tbody>
          {#each rows as row (row.character)}
            <tr>
              <th class="px-2 py-1 text-left align-middle font-medium">{row.character}</th>
              {#each SLOTS as slot (slot)}
                {@const p = wornIn(row, slot)}
                <td class="align-top">
                  {#if p}
                    <div
                      class="rounded-lg border px-2 py-1.5 {TONE[p.verdict] ?? 'border-line text-muted'}"
                      title={`${p.why}.\n\nMain stat: ${statLabel(p.mainStat)}\nSubstats: ${rolls(p)}\n\nThe score counts all of them — main stat, substats and the set bonus — not the main stat alone.`}
                    >
                      <div class="flex items-baseline gap-1">
                        <span class="text-sm font-semibold tabular-nums">{Math.round(p.score)}</span>
                        {#if MARK[p.verdict]}
                          <span class="text-[0.7rem] leading-none" aria-hidden="true">{MARK[p.verdict]}</span>
                        {/if}
                        <span class="sr-only">{p.verdict}</span>
                      </div>
                      <div class="truncate text-[0.65rem] opacity-80">{statLabel(p.mainStat)}</div>
                    </div>
                  {:else}
                    <div
                      class="rounded-lg border border-dashed border-line px-2 py-1.5 text-muted"
                      title={`Nothing equipped here. Wants ${statLabel(idealFor(row, slot)) || 'an unknown main stat'}.`}
                    >
                      <div class="text-sm font-semibold">—</div>
                      <div class="truncate text-[0.65rem] opacity-80">
                        {statLabel(idealFor(row, slot)) || 'empty'}
                      </div>
                    </div>
                  {/if}
                </td>
              {/each}
            </tr>
          {/each}
        </tbody>
      </table>
    </div>

    <p class="mt-3 text-xs text-muted">
      The score counts the main stat, the substats and the set bonus together. The colour is
      narrower: it says what you can <em>do</em>. Plain green is nothing to do — right main stat,
      at its cap, nothing you own beats it — which is not the same as a high score, and the number
      is there to tell you which. A dot (•) is fixable: not at its cap, or something better is in
      the bag. A triangle (▲) means levelling will not help. Hover any cell for its rolls.
    </p>
  {/if}

  {#if ran && rows.length === 0 && !error}
    <p class="mt-4 text-sm text-muted">Nothing could be measured for that selection.</p>
  {/if}

  {#if missing.length > 0}
    <ul class="mt-3 space-y-1 text-xs text-warn">
      {#each missing as m (m)}
        <li>{m}</li>
      {/each}
    </ul>
  {/if}
</section>
