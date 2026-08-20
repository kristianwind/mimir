<script>
  import { api } from './api.js'
  import ThemePicker from './ThemePicker.svelte'

  let { onauthenticated, theme, mode, setTheme } = $props()

  let username = $state('')
  let password = $state('')
  let error = $state('')
  let busy = $state(false)

  async function submit(event) {
    event.preventDefault()
    busy = true
    error = ''
    try {
      onauthenticated(await api.login(username, password))
    } catch (err) {
      error = err.message
    } finally {
      busy = false
    }
  }
</script>

<div class="grid min-h-dvh place-items-center p-6">
  <div class="w-full max-w-sm">
    <div class="mb-8 text-center">
      <div class="mx-auto mb-4 grid h-14 w-14 place-items-center rounded-2xl bg-accent/15 text-2xl">
        🜁
      </div>
      <h1 class="text-2xl font-semibold tracking-tight">Mimir</h1>
      <p class="mt-1 text-sm text-muted">Rådgiveren ved brønden</p>
    </div>

    <form class="card space-y-4 p-6" onsubmit={submit}>
      <div>
        <label class="label" for="username">Brugernavn</label>
        <input id="username" class="field" bind:value={username} autocomplete="username" required />
      </div>
      <div>
        <label class="label" for="password">Adgangskode</label>
        <input
          id="password"
          class="field"
          type="password"
          bind:value={password}
          autocomplete="current-password"
          required
        />
      </div>

      {#if error}
        <p class="rounded-xl border border-bad/40 bg-bad/10 px-3 py-2 text-sm text-bad" role="alert">
          {error}
        </p>
      {/if}

      <button class="btn-primary w-full" disabled={busy}>
        {busy ? 'Logger ind…' : 'Log ind'}
      </button>
    </form>

    <div class="mt-6 flex justify-center">
      <ThemePicker {theme} {mode} {setTheme} />
    </div>
  </div>
</div>
