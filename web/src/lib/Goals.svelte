<script>
  import { api } from './api.js'
  import Kvasir from './Kvasir.svelte'
  import CharacterArt from './CharacterArt.svelte'

  let { account } = $props()

  const SLOTS = [
    { key: 'auto', label: 'Normal attack' },
    { key: 'skill', label: 'Elemental skill' },
    { key: 'burst', label: 'Elemental burst' },
  ]

  let goals = $state([])
  let characters = $state([])
  let error = $state('')
  let busy = $state(false)

  // Editor state
  let editing = $state(null) // character key
  let talents = $state(null)
  let steps = $state([])
  let duration = $state(20)
  let priority = $state(0)
  let conditions = $state({})
  let conditionFields = $state([])

  async function refresh() {
    error = ''
    try {
      goals = (await api.goals(account.id)) ?? []
      characters = (await api.characters(account.id)) ?? []
    } catch (err) {
      error = err.message
    }
  }

  $effect(() => {
    account.id
    refresh()
  })

  async function edit(key) {
    error = ''
    editing = key
    talents = null
    conditionFields = []
    const existing = goals.find((g) => g.characterKey === key)
    steps = existing?.rotation?.steps?.map((s) => ({ ...s })) ?? []
    duration = existing?.rotation?.duration ?? 20
    priority = existing?.priority ?? 0
    conditions = { ...(existing?.conditions ?? {}) }
    try {
      talents = await api.talents(account.id, key)
    } catch (err) {
      error = err.hint ? `${err.message} — ${err.hint}` : err.message
      return
    }
    // The build sheet knows which conditional bonuses this character's gear
    // actually triggers, so the form only asks about sets they are wearing.
    try {
      const build = await api.build(account.id, key)
      const asked = build.sheet.undeclared ?? []
      const known = (build.sheet.effects ?? [])
        .map((g) => g.source)
        .filter(Boolean)
      conditionFields = [
        ...asked,
        ...Object.keys(conditions)
          .filter((k) => !asked.some((m) => m.key === k))
          .map((k) => ({ key: k, source: known.find((s) => s.startsWith(k.split('.')[0])) ?? k })),
      ]
    } catch {
      conditionFields = []
    }
  }

  function damageEntries(slot) {
    return talents?.talents?.[slot]?.entries?.filter((e) => e.isDamage) ?? []
  }

  function hitsFor(slot, label) {
    return steps.find((s) => s.talent === slot && s.entry === label)?.hits ?? 0
  }

  function setHits(slot, label, hits) {
    const n = Math.max(0, Math.min(99, Math.round(hits)))
    const i = steps.findIndex((s) => s.talent === slot && s.entry === label)
    if (n === 0) {
      if (i >= 0) steps = [...steps.slice(0, i), ...steps.slice(i + 1)]
      return
    }
    if (i >= 0) {
      steps = steps.map((s, j) => (j === i ? { ...s, hits: n } : s))
    } else {
      steps = [...steps, { talent: slot, entry: label, hits: n }]
    }
  }

  async function save() {
    if (steps.length === 0) {
      error = 'Pick at least one attack, otherwise there is nothing to measure the gain on.'
      return
    }
    busy = true
    error = ''
    try {
      await api.saveGoal(account.id, {
        characterKey: editing,
        priority,
        conditions,
        rotation: { name: `${editing}-rotation`, duration, steps },
      })
      editing = null
      await refresh()
    } catch (err) {
      error = err.hint ? `${err.message} — ${err.hint}` : err.message
    } finally {
      busy = false
    }
  }

  async function remove(key) {
    busy = true
    try {
      await api.deleteGoal(account.id, key)
      await refresh()
    } catch (err) {
      error = err.message
    } finally {
      busy = false
    }
  }
</script>

{#if error}
  <p class="mb-4 rounded-xl border border-bad/40 bg-bad/10 px-4 py-3 text-sm text-bad" role="alert">
    {error}
  </p>
{/if}

{#if editing}
  <div class="card p-5">
    <div class="mb-4 flex flex-wrap items-baseline justify-between gap-3">
      <h2 class="text-lg font-medium">Rotation for {editing}</h2>
      <button class="btn-ghost text-xs" onclick={() => (editing = null)}>Cancel</button>
    </div>

    <p class="mb-5 max-w-prose text-sm text-muted">
      Pick which attacks make up one rotation, and how many times each of them hits. The numbers are
      your actual talent levels. The rotation is what gains are measured on — a burst you never use
      is worth no gain at all.
    </p>

    <!-- Only for a goal that already exists: there is nothing to have an
         opinion about until the rotation has been saved once. -->
    {#if goals.some((g) => g.characterKey === editing)}
      <Kvasir {account} surface="goal" subject={editing} compact />
    {/if}

    {#if !talents}
      <p class="text-sm text-muted">Fetching talent table…</p>
    {:else}
      <div class="space-y-6">
        {#each SLOTS as slot (slot.key)}
          {@const entries = damageEntries(slot.key)}
          {#if entries.length}
            <section>
              <h3 class="label">
                {slot.label}
                <span class="ml-1 text-muted">· level {talents.levels[slot.key]}</span>
                {#if talents.baseLevels && talents.levels[slot.key] > talents.baseLevels[slot.key]}
                  <span class="ml-1 text-accent">
                    ({talents.baseLevels[slot.key]} +
                    {talents.levels[slot.key] - talents.baseLevels[slot.key]} from constellation)
                  </span>
                {/if}
              </h3>
              <div class="space-y-1">
                {#each entries as entry (entry.label)}
                  {@const hits = hitsFor(slot.key, entry.label)}
                  <div
                    class="flex items-center gap-3 rounded-xl px-3 py-2 transition
                           {hits > 0 ? 'bg-accent/10' : 'hover:bg-raised'}"
                  >
                    <span class="min-w-0 flex-1 truncate text-sm">{entry.label}</span>
                    <span class="hidden font-mono text-xs text-muted sm:inline">
                      {(entry.atLevel * 100).toFixed(1)} % {entry.scaling}
                    </span>
                    <div class="flex items-center gap-1">
                      <button
                        type="button"
                        class="btn-ghost h-7 w-7 !px-0"
                        aria-label="Fewer"
                        onclick={() => setHits(slot.key, entry.label, hits - 1)}>−</button
                      >
                      <span class="w-6 text-center text-sm tabular-nums">{hits}</span>
                      <button
                        type="button"
                        class="btn-ghost h-7 w-7 !px-0"
                        aria-label="More"
                        onclick={() => setHits(slot.key, entry.label, hits + 1)}>+</button
                      >
                    </div>
                  </div>
                {/each}
              </div>
            </section>
          {/if}
        {/each}
      </div>

      {#if conditionFields.length}
        <section class="mt-6 border-t border-line pt-5">
          <h3 class="label">Conditions</h3>
          <p class="mb-3 max-w-prose text-xs text-muted">
            Some bonuses — from sets, constellations and weapons — depend on how you play, and some
            weapons and constellations land their own extra hit. Mimir does not guess: a bonus
            switched off because nobody asked looks exactly like a bonus that does not exist. Set 0
            if you never have it up.
          </p>
          <div class="space-y-2">
            {#each conditionFields as field (field.key)}
              <div class="flex flex-wrap items-center gap-3 rounded-xl bg-raised px-3 py-2">
                <div class="min-w-0 flex-1">
                  <p class="truncate text-sm">{field.source}</p>
                  {#if field.note}<p class="text-xs text-muted">{field.note}</p>{/if}
                </div>
                <input
                  class="field w-20 text-center"
                  type="number"
                  min="0"
                  max={field.maxStacks || 99}
                  value={conditions[field.key] ?? 0}
                  oninput={(e) => (conditions = { ...conditions, [field.key]: Number(e.target.value) })}
                />
                {#if field.maxStacks > 1}
                  <span class="text-xs text-muted">of {field.maxStacks}</span>
                {/if}
              </div>
            {/each}
          </div>
        </section>
      {/if}

      <div class="mt-6 flex flex-wrap items-end gap-4 border-t border-line pt-5">
        <div>
          <label class="label" for="duration">Rotation length (sec.)</label>
          <input id="duration" class="field w-32" type="number" min="1" bind:value={duration} />
        </div>
        <div>
          <label class="label" for="priority">Priority</label>
          <input id="priority" class="field w-24" type="number" bind:value={priority} />
        </div>
        <button class="btn-primary ml-auto" disabled={busy} onclick={save}>
          {busy ? 'Saving…' : 'Save goal'}
        </button>
      </div>
    {/if}
  </div>
{:else}
  <div class="space-y-6">
    {#if goals.length}
      <Kvasir {account} surface="goals" />
    {/if}
    {#if goals.length}
      <div class="space-y-3">
        {#each goals as goal (goal.characterKey)}
          <div class="card relative flex flex-wrap items-center gap-4 overflow-hidden p-4">
            <CharacterArt character={goal.characterKey} scrim="both" />

            <div class="relative min-w-40 flex-1">
              <p class="font-medium">{goal.characterKey}</p>
              <p class="text-xs text-muted">
                {goal.rotation?.steps?.reduce((n, s) => n + (s.hits ?? 1), 0) ?? 0} attacks over
                {goal.rotation?.duration ?? 0} sec. · priority {goal.priority}
              </p>
            </div>
            <button class="btn-ghost relative" onclick={() => edit(goal.characterKey)}>Edit</button>
            <button class="btn-ghost relative" onclick={() => remove(goal.characterKey)}>
              Delete
            </button>
          </div>
        {/each}
      </div>
    {:else}
      <p class="text-sm text-muted">No goals yet. Pick a character below.</p>
    {/if}

    <div>
      <h3 class="label">Add a goal</h3>
      <div class="flex flex-wrap gap-2">
        {#each characters.filter((c) => !goals.some((g) => g.characterKey === c.key)) as character (character.id)}
          <button type="button" class="chip hover:border-accent" onclick={() => edit(character.key)}>
            {character.key}
            <span class="text-muted">lvl {character.level}</span>
          </button>
        {/each}
      </div>
    </div>
  </div>
{/if}
