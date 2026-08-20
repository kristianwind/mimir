<script>
  import { api } from './api.js'
  import { t } from './lang.svelte.js'

  let { account } = $props()

  let characters = $state([])
  let error = $state('')
  let loading = $state(true)

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
  <p class="text-sm text-muted">{t('Fetching characters…')}</p>
{:else if error}
  <p class="rounded-xl border border-bad/40 bg-bad/10 px-4 py-3 text-sm text-bad">{error}</p>
{:else if characters.length === 0}
  <div class="card p-8 text-center">
    <p class="text-muted">{t('No characters yet. Fetch from Enka or upload a .good file.')}</p>
  </div>
{:else}
  <div class="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
    {#each characters as character (character.id)}
      <article class="card p-4">
        <div class="flex items-baseline justify-between gap-2">
          <h2 class="font-medium">{character.key}</h2>
          <span class="chip">C{character.constellation}</span>
        </div>
        <p class="mt-1 text-sm text-muted">{t('Level {n}', { n: character.level })}</p>
        <dl class="mt-3 grid grid-cols-3 gap-2 text-center text-xs">
          <div class="rounded-lg bg-raised py-2">
            <dt class="text-muted">Normal</dt>
            <dd class="text-sm font-medium">{character.talentAuto}</dd>
          </div>
          <div class="rounded-lg bg-raised py-2">
            <dt class="text-muted">Skill</dt>
            <dd class="text-sm font-medium">{character.talentSkill}</dd>
          </div>
          <div class="rounded-lg bg-raised py-2">
            <dt class="text-muted">Burst</dt>
            <dd class="text-sm font-medium">{character.talentBurst}</dd>
          </div>
        </dl>
      </article>
    {/each}
  </div>
{/if}
