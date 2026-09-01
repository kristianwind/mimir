<script>
  import { api } from './api.js'
  import Kvasir from './Kvasir.svelte'
  import CharacterArt from './CharacterArt.svelte'

  let { account } = $props()

  let characters = $state([])
  let error = $state('')
  let loading = $state(true)

  // One build at a time, and only when asked. The roster card above is worth
  // fetching on sight; twenty build opinions nobody asked for is a local
  // model chewing through an afternoon.
  let asking = $state(null)

  // Characters whose picture the server could not produce — a Traveler, a
  // snapshot mined before the art was carried. The card then renders as it
  // always did rather than as an empty frame.

  // What the character should aim for, computed rather than looked up. One at
  // a time and only when asked: it searches every farmable set against every
  // main stat, which is cheap once and wasteful sixty times over.
  let aiming = $state(null)
  let targets = $state({})
  let targetError = $state({})

  async function showTarget(key) {
    if (aiming === key) {
      aiming = null
      return
    }
    aiming = key
    if (targets[key] || targetError[key]) return
    try {
      targets = { ...targets, [key]: await api.target(account.id, key) }
    } catch (err) {
      targetError = { ...targetError, [key]: err }
    }
  }

  const SLOT_ORDER = ['sands', 'goblet', 'circlet']
  const SLOT_LABELS = { sands: 'Sands', goblet: 'Goblet', circlet: 'Circlet' }

  $effect(() => {
    const id = account.id
    loading = true
    api
      .characters(id)
      .then((data) => (characters = data ?? []))
      .catch((err) => (error = err.message))
      .finally(() => (loading = false))
  })
</script>

{#if loading}
  <p class="text-sm text-muted">Fetching characters…</p>
{:else if error}
  <p class="rounded-xl border border-bad/40 bg-bad/10 px-4 py-3 text-sm text-bad">{error}</p>
{:else if characters.length === 0}
  <div class="card p-8 text-center">
    <p class="text-muted">No characters yet. Fetch from Enka or upload a .good file.</p>
  </div>
{:else}
  <Kvasir {account} surface="roster" />

  <div class="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
    {#each characters as character (character.id)}
      <article class="card relative overflow-hidden p-4">
        <CharacterArt character={character.key} />

        <div class="relative">
          <div class="flex items-baseline justify-between gap-2">
            <h2 class="font-medium">{character.key}</h2>
            <span class="chip backdrop-blur-sm">C{character.constellation}</span>
          </div>
          <p class="mt-1 text-sm text-muted">Level {character.level}</p>
          <dl class="mt-3 grid grid-cols-3 gap-2 text-center text-xs">
            <div class="rounded-lg bg-raised/80 py-2 backdrop-blur-sm">
              <dt class="text-muted">Normal</dt>
              <dd class="text-sm font-medium">{character.talentAuto}</dd>
            </div>
            <div class="rounded-lg bg-raised/80 py-2 backdrop-blur-sm">
              <dt class="text-muted">Skill</dt>
              <dd class="text-sm font-medium">{character.talentSkill}</dd>
            </div>
            <div class="rounded-lg bg-raised/80 py-2 backdrop-blur-sm">
              <dt class="text-muted">Burst</dt>
              <dd class="text-sm font-medium">{character.talentBurst}</dd>
            </div>
          </dl>

          <button
            type="button"
            class="btn-ghost mt-3 w-full text-xs backdrop-blur-sm"
            onclick={() => showTarget(character.key)}
          >
            {aiming === character.key ? 'Hide the target' : 'What should this character aim for?'}
          </button>
          {#if aiming === character.key}
            <div class="mt-3 rounded-xl bg-raised/80 p-3 text-xs backdrop-blur-sm">
              {#if targetError[character.key]}
                <p>{targetError[character.key].message}</p>
                {#if targetError[character.key].hint}
                  <p class="mt-1 text-muted">{targetError[character.key].hint}</p>
                {/if}
              {:else if !targets[character.key]}
                <p class="text-muted">Working it out…</p>
              {:else}
                {@const t = targets[character.key]}
                <p class="font-medium">
                  {SLOT_ORDER.map((slot) => `${SLOT_LABELS[slot]} ${t.mainStats?.[slot] ?? '—'}`).join(
                    ' · ',
                  )}
                </p>
                <ul class="mt-2 space-y-1">
                  {#each (t.sets ?? []).slice(0, 4) as set}
                    <li class="flex items-baseline justify-between gap-2">
                      <span class:text-muted={!set.owned}>
                        4pc {set.config}{#if set.owned}<span class="text-good"> · you have it</span
                          >{/if}{#if !set.modelled}<span
                            class="text-warn"
                            title="This set's four-piece bonus is conditional wording rather than
                                   numbers, so it is not in the score. The entry was ranked on its
                                   stats alone."
                          >
                            · stats only</span
                          >{/if}
                      </span>
                      <span class="shrink-0 text-muted">
                        {set.behind ? `−${(set.behind * 100).toFixed(0)} %` : 'best'}
                      </span>
                    </li>
                  {/each}
                </ul>
                <p class="mt-2 text-muted">
                  Substats to chase:
                  {Object.entries(t.substats ?? {})
                    .sort((a, b) => b[1] - a[1])
                    .map(([k, n]) => `${k} ×${n}`)
                    .join(', ')}
                </p>
                <details class="mt-2">
                  <summary class="cursor-pointer text-muted">What this does not measure</summary>
                  <ul class="mt-1 space-y-1 text-muted">
                    {#each t.caveats ?? [] as caveat}<li>· {caveat}</li>{/each}
                  </ul>
                </details>
              {/if}
            </div>
          {/if}

          <button
            type="button"
            class="btn-ghost mt-2 w-full text-xs backdrop-blur-sm"
            onclick={() => (asking = asking === character.key ? null : character.key)}
          >
            {asking === character.key ? 'Hide' : 'What does Mimir make of this build?'}
          </button>
          {#if asking === character.key}
            <div class="mt-3">
              <Kvasir {account} surface="character" subject={character.key} compact />
            </div>
          {/if}
        </div>
      </article>
    {/each}
  </div>
{/if}
