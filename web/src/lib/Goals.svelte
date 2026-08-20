<script>
  import { api } from './api.js'

  let { account } = $props()

  const SLOTS = [
    { key: 'auto', label: 'Normalt angreb' },
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
      error = 'Vælg mindst ét angreb, ellers er der ikke noget at måle gevinsten på.'
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
      <button class="btn-ghost text-xs" onclick={() => (editing = null)}>Annullér</button>
    </div>

    <p class="mb-5 max-w-prose text-sm text-muted">
      Vælg hvilke angreb der indgår i én rotation, og hvor mange gange hvert af dem rammer.
      Tallene er dine faktiske talentniveauer. Rotationen er det gevinsterne måles på — et burst
      du aldrig bruger, er ingen gevinst værd.
    </p>

    {#if !talents}
      <p class="text-sm text-muted">Henter talenttabel…</p>
    {:else}
      <div class="space-y-6">
        {#each SLOTS as slot (slot.key)}
          {@const entries = damageEntries(slot.key)}
          {#if entries.length}
            <section>
              <h3 class="label">
                {slot.label}
                <span class="ml-1 text-muted">· niveau {talents.levels[slot.key]}</span>
                {#if talents.baseLevels && talents.levels[slot.key] > talents.baseLevels[slot.key]}
                  <span class="ml-1 text-accent">
                    ({talents.baseLevels[slot.key]} + {talents.levels[slot.key] -
                      talents.baseLevels[slot.key]} fra constellation)
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
                        aria-label="Færre"
                        onclick={() => setHits(slot.key, entry.label, hits - 1)}>−</button
                      >
                      <span class="w-6 text-center text-sm tabular-nums">{hits}</span>
                      <button
                        type="button"
                        class="btn-ghost h-7 w-7 !px-0"
                        aria-label="Flere"
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
          <h3 class="label">Betingelser</h3>
          <p class="mb-3 max-w-prose text-xs text-muted">
            Nogle bonusser — fra sæt, constellations og våben — afhænger af hvordan du
            spiller, og nogle våben og constellations lander deres eget ekstra hit. Mimir
            gætter ikke: en bonus der er slukket fordi ingen spurgte, ser i en rangering
            præcis ud som en bonus der ikke findes. Sæt 0, hvis du aldrig har den oppe.
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
                  <span class="text-xs text-muted">af {field.maxStacks}</span>
                {/if}
              </div>
            {/each}
          </div>
        </section>
      {/if}

      <div class="mt-6 flex flex-wrap items-end gap-4 border-t border-line pt-5">
        <div>
          <label class="label" for="duration">Rotationens længde (sek.)</label>
          <input id="duration" class="field w-32" type="number" min="1" bind:value={duration} />
        </div>
        <div>
          <label class="label" for="priority">Prioritet</label>
          <input id="priority" class="field w-24" type="number" bind:value={priority} />
        </div>
        <button class="btn-primary ml-auto" disabled={busy} onclick={save}>
          {busy ? 'Gemmer…' : 'Gem mål'}
        </button>
      </div>
    {/if}
  </div>
{:else}
  <div class="space-y-6">
    {#if goals.length}
      <div class="space-y-3">
        {#each goals as goal (goal.characterKey)}
          <div class="card flex flex-wrap items-center gap-4 p-4">
            <div class="min-w-40 flex-1">
              <p class="font-medium">{goal.characterKey}</p>
              <p class="text-xs text-muted">
                {goal.rotation?.steps?.reduce((n, s) => n + (s.hits ?? 1), 0) ?? 0} angreb over
                {goal.rotation?.duration ?? 0} sek. · prioritet {goal.priority}
              </p>
            </div>
            <button class="btn-ghost" onclick={() => edit(goal.characterKey)}>Redigér</button>
            <button class="btn-ghost" onclick={() => remove(goal.characterKey)}>Slet</button>
          </div>
        {/each}
      </div>
    {:else}
      <p class="text-sm text-muted">Ingen mål endnu. Vælg en karakter nedenfor.</p>
    {/if}

    <div>
      <h3 class="label">Tilføj et mål</h3>
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
