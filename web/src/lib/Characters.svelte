<script>
  import { api } from './api.js'
  import Kvasir from './Kvasir.svelte'

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
  let artless = $state(new Set())

  function noArt(key) {
    artless = new Set(artless).add(key)
  }

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
        <!--
          The game's own art, behind the text rather than beside it. An <img>
          rather than a CSS background so a missing picture is an event the
          card can react to; a background-image fails silently and leaves a
          hole. The scrim is what keeps the numbers legible over it, and it is
          built from the surface colour so it works in both modes.
        -->
        {#if !artless.has(character.key)}
          <img
            src="/api/art/{character.key}"
            alt=""
            aria-hidden="true"
            loading="lazy"
            onerror={() => noArt(character.key)}
            class="pointer-events-none absolute inset-y-0 right-0 h-full w-3/4 object-cover object-center"
          />
          <!--
            Opaque under the text, gone by the right edge. The picture is the
            point, but a name and three talent levels that cannot be read are
            not a card — so the scrim is a hard wall on the left rather than a
            wash over everything.
          -->
          <div
            class="pointer-events-none absolute inset-0
                   bg-gradient-to-r from-surface from-30% via-surface/80 to-transparent"
          ></div>
        {/if}

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
            onclick={() => (asking = asking === character.key ? null : character.key)}
          >
            {asking === character.key ? 'Hide Kvasir' : 'What does Kvasir think of this build?'}
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
