<script>
  import { api } from './api.js'
  import ThemePicker from './ThemePicker.svelte'

  let { onauthenticated, theme, mode, setTheme } = $props()

  let username = $state('')
  let password = $state('')
  let confirm = $state('')
  let error = $state('')
  let busy = $state(false)

  // An instance with no users at all cannot be logged into, so the page
  // offers to create the first administrator instead. The offer is not
  // guarded by a flag or a timer that could be left on — it is the absence
  // of any user, and it disappears the moment one exists.
  let first = $state(false)
  let checked = $state(false)

  api
    .bootstrapStatus()
    .then((s) => (first = s.needed))
    .catch(() => {})
    .finally(() => (checked = true))

  async function submit(event) {
    event.preventDefault()
    error = ''

    if (first) {
      if (password !== confirm) {
        error = 'De to adgangskoder er ikke ens.'
        return
      }
      if (password.length < 12) {
        error = 'Adgangskoden skal være mindst 12 tegn.'
        return
      }
    }

    busy = true
    try {
      const user = first
        ? await api.bootstrap(username, password)
        : await api.login(username, password)
      onauthenticated(user)
    } catch (err) {
      error = err.hint ? `${err.message} — ${err.hint}` : err.message
      // Somebody else got there first; fall back to the login form rather
      // than leaving a create form that can no longer work.
      if (err.status === 409) first = false
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

    {#if !checked}
      <div class="card grid h-40 place-items-center p-6">
        <span class="h-2 w-2 animate-ping rounded-full bg-accent"></span>
      </div>
    {:else}
      <form class="card space-y-4 p-6" onsubmit={submit}>
        {#if first}
          <div class="rounded-xl border border-accent/40 bg-accent/10 px-3 py-2.5">
            <p class="text-sm font-medium">Opret den første administrator</p>
            <p class="mt-1 text-xs text-muted">
              Instansen er tom. Mimir har ingen standardbruger — den her form vises kun indtil
              den første konto findes, og forsvinder derefter af sig selv.
            </p>
          </div>
        {/if}

        <div>
          <label class="label" for="username">Brugernavn</label>
          <input
            id="username"
            class="field"
            bind:value={username}
            autocomplete="username"
            required
          />
        </div>

        <div>
          <label class="label" for="password">Adgangskode</label>
          <input
            id="password"
            class="field"
            type="password"
            bind:value={password}
            autocomplete={first ? 'new-password' : 'current-password'}
            required
          />
          {#if first}
            <p class="mt-1.5 text-xs text-muted">Mindst 12 tegn.</p>
          {/if}
        </div>

        {#if first}
          <div>
            <label class="label" for="confirm">Gentag adgangskoden</label>
            <input
              id="confirm"
              class="field"
              type="password"
              bind:value={confirm}
              autocomplete="new-password"
              required
            />
          </div>
        {/if}

        {#if error}
          <p class="rounded-xl border border-bad/40 bg-bad/10 px-3 py-2 text-sm text-bad" role="alert">
            {error}
          </p>
        {/if}

        <button class="btn-primary w-full" disabled={busy}>
          {busy
            ? first
              ? 'Opretter…'
              : 'Logger ind…'
            : first
              ? 'Opret og log ind'
              : 'Log ind'}
        </button>
      </form>
    {/if}

    <div class="mt-6 flex justify-center">
      <ThemePicker {theme} {mode} {setTheme} />
    </div>
  </div>
</div>
