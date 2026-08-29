<script>
  import { api } from './api.js'
  import ThemePicker from './ThemePicker.svelte'
  import { supported as passkeySupported, get as passkeyGet } from './passkey.js'

  let { onauthenticated, theme, mode, setTheme } = $props()

  let username = $state('')
  let password = $state('')
  // Only asked for once the server says this account has a second factor.
  // Asking everybody up front would tell anyone who can type a username
  // which accounts have one.
  let code = $state('')
  let needsCode = $state(false)
  let passkeyBusy = $state(false)
  // Whether this instance can do passkeys at all. Asked rather than assumed:
  // an instance with no origin configured has none, and offering a button
  // that fails on press is worse than not offering one.
  let passkeysHere = $state(false)
  api
    .instance()
    .then((i) => (passkeysHere = !!i?.passkeys))
    .catch(() => (passkeysHere = false))
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

  // Signing in with a passkey asks for no username. The credential says who
  // it belongs to, which is friendlier than a form and also quieter: a page
  // that asks for a name first tells anybody who types one whether that
  // account exists.
  async function withPasskey() {
    passkeyBusy = true
    error = ''
    try {
      const { options, challenge } = await api.passkeyLoginBegin()
      const response = await passkeyGet(options)
      const user = await api.passkeyLoginFinish(challenge, response)
      onauthenticated(user)
    } catch (err) {
      // Dismissing the system prompt is a choice, not a failure.
      if (err.name !== 'NotAllowedError') {
        error = err.hint ? `${err.message} — ${err.hint}` : err.message
      }
    } finally {
      passkeyBusy = false
    }
  }

  async function submit(event) {
    event.preventDefault()
    error = ''

    if (first) {
      if (password !== confirm) {
        error = 'The two passwords do not match.'
        return
      }
      if (password.length < 12) {
        error = 'The password must be at least 12 characters.'
        return
      }
    }

    busy = true
    try {
      const user = first
        ? await api.bootstrap(username, password)
        : await api.login(username, password, code)
      onauthenticated(user)
    } catch (err) {
      error = err.hint ? `${err.message} — ${err.hint}` : err.message
      // The password was right and is not enough. The form grows a field
      // rather than starting over — retyping a correct password to satisfy
      // a second step is a punishment for having security switched on.
      //
      // The first time, the field appearing IS the message, so nothing is
      // printed in red: being asked for a code is not a mistake the reader
      // made. A second refusal is, and says so.
      if (err.secondFactor) {
        if (!needsCode) error = ''
        needsCode = true
        code = ''
      }
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
      <p class="mt-1 text-sm text-muted">The adviser at the well</p>
    </div>

    {#if !checked}
      <div class="card grid h-40 place-items-center p-6">
        <span class="h-2 w-2 animate-ping rounded-full bg-accent"></span>
      </div>
    {:else}
      <form class="card space-y-4 p-6" onsubmit={submit}>
        {#if first}
          <div class="rounded-xl border border-accent/40 bg-accent/10 px-3 py-2.5">
            <p class="text-sm font-medium">Create the first administrator</p>
            <p class="mt-1 text-xs text-muted">
              This instance is empty. Mimir has no default account — this form only appears until
              the first one exists, and disappears by itself afterwards.
            </p>
          </div>
        {/if}

        <div>
          <label class="label" for="username">Username</label>
          <input
            id="username"
            class="field"
            bind:value={username}
            autocomplete="username"
            required
          />
        </div>

        <div>
          <label class="label" for="password">Password</label>
          <input
            id="password"
            class="field"
            type="password"
            bind:value={password}
            autocomplete={first ? 'new-password' : 'current-password'}
            required
          />
          {#if first}
            <p class="mt-1.5 text-xs text-muted">At least 12 characters.</p>
          {/if}
        </div>

        {#if first}
          <div>
            <label class="label" for="confirm">Repeat the password</label>
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

        {#if needsCode}
          <div>
            <label class="label" for="code">Code from your authenticator app</label>
            <input
              id="code"
              class="field font-mono tracking-widest"
              inputmode="numeric"
              autocomplete="one-time-code"
              bind:value={code}
              required
            />
            <p class="mt-1.5 text-xs text-muted">
              Or one of your recovery codes, if you do not have your phone.
            </p>
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
              ? 'Creating…'
              : 'Logging in…'
            : first
              ? 'Create and log in'
              : 'Log in'}
        </button>

        {#if !first && passkeysHere && passkeySupported()}
          <button
            type="button"
            class="btn-ghost w-full text-sm"
            disabled={passkeyBusy}
            onclick={withPasskey}
          >
            {passkeyBusy ? 'Waiting for your device…' : 'Use a passkey instead'}
          </button>
        {/if}
      </form>
    {/if}

    <div class="mt-6 flex justify-center">
      <ThemePicker {theme} {mode} {setTheme} />
    </div>
  </div>
</div>
