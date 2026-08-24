<script>
  import { api } from './api.js'

  let { accounts, onchange } = $props()

  let uid = $state('')
  let busy = $state('')
  let error = $state('')
  let result = $state(null)

  async function addAccount(event) {
    event.preventDefault()
    error = ''
    busy = 'add'
    try {
      await api.addAccount(uid.trim())
      uid = ''
      await onchange()
    } catch (err) {
      error = err.hint ? `${err.message} — ${err.hint}` : err.message
    } finally {
      busy = ''
    }
  }

  async function importEnka(account) {
    error = ''
    result = null
    busy = `enka-${account.id}`
    try {
      result = { account: account.uid, ...(await api.importEnka(account.id)) }
      await onchange()
    } catch (err) {
      error = err.hint ? `${err.message} — ${err.hint}` : err.message
    } finally {
      busy = ''
    }
  }

  async function importGOOD(account, event) {
    const file = event.target.files?.[0]
    if (!file) return
    error = ''
    result = null
    busy = `good-${account.id}`
    try {
      result = { account: account.uid, ...(await api.importGOOD(account.id, file)) }
      await onchange()
    } catch (err) {
      error = err.hint ? `${err.message} — ${err.hint}` : err.message
    } finally {
      busy = ''
      event.target.value = ''
    }
  }
</script>

<div class="space-y-6">
  <form class="card flex flex-wrap items-end gap-3 p-5" onsubmit={addAccount}>
    <div class="min-w-48 flex-1">
      <label class="label" for="uid">Genshin UID</label>
      <input
        id="uid"
        class="field font-mono"
        bind:value={uid}
        inputmode="numeric"
        placeholder="7xxxxxxxx"
        required
      />
    </div>
    <button class="btn-primary" disabled={busy === 'add'}>Add</button>
    <p class="w-full text-xs text-muted">
      {@html 'The UID is at the bottom right in the game. Enka fetches your showcase characters ' +
        'without a login — remember to switch on <em>Show Character Details</em> in your profile.'}
    </p>
  </form>

  {#if error}
    <p class="rounded-xl border border-bad/40 bg-bad/10 px-4 py-3 text-sm text-bad" role="alert">
      {error}
    </p>
  {/if}

  {#if result}
    <div class="rounded-xl border border-good/40 bg-good/10 px-4 py-3 text-sm">
      <p class="font-medium">
        Import from {result.source} for {result.account}
      </p>
      <p class="text-muted">
        {result.characters} characters · {result.artifacts.inserted} new artifacts,
        {result.artifacts.upgraded} upgraded, {result.artifacts.unchanged} unchanged
        {#if result.stale}· data is from the cache{/if}
      </p>
      {#if result.warnings?.length}
        <ul class="mt-2 list-inside list-disc text-xs text-warn">
          {#each result.warnings as warning}
            <li>{warning}</li>
          {/each}
        </ul>
      {/if}
    </div>
  {/if}

  <div class="space-y-3">
    {#each accounts as account (account.id)}
      <div class="card flex flex-wrap items-center gap-4 p-5">
        <div class="min-w-40 flex-1">
          <p class="font-medium">{account.nickname || 'Unnamed'}</p>
          <p class="font-mono text-xs text-muted">{account.uid} · {account.region}</p>
        </div>
        <div class="flex flex-wrap gap-2">
          <button
            class="btn-ghost"
            onclick={() => importEnka(account)}
            disabled={busy === `enka-${account.id}`}
          >
            {busy === `enka-${account.id}` ? 'Fetching…' : 'Fetch from Enka'}
          </button>
          <label class="btn-ghost cursor-pointer">
            {busy === `good-${account.id}` ? 'Importing…' : 'Upload .good'}
            <input
              type="file"
              accept=".good,.json,application/json"
              class="sr-only"
              onchange={(e) => importGOOD(account, e)}
            />
          </label>
        </div>
      </div>
    {:else}
      <p class="text-sm text-muted">No accounts yet.</p>
    {/each}
  </div>
</div>
