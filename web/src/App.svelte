<script>
  import { api, ApiError } from './lib/api.js'
  import { applyTheme, storedMode, storedTheme, watchSystemMode } from './lib/theme.js'
  import Login from './lib/Login.svelte'
  import Shell from './lib/Shell.svelte'

  let user = $state(null)
  let loading = $state(true)
  let theme = $state(storedTheme())
  let mode = $state(storedMode())

  // Whether the user has picked a theme during this page session. It decides
  // who wins when the local choice and the stored one disagree: a deliberate
  // click here beats the server, anything else defers to the server so the
  // theme follows the account to a new device.
  let touched = $state(false)

  $effect(() => watchSystemMode(() => mode))

  async function adoptServerPrefs(me) {
    if (!me) return
    if (!touched && me.theme) {
      theme = me.theme
      mode = me.themeMode ?? 'system'
      applyTheme(theme, mode)
    }
  }

  async function boot() {
    try {
      const me = await api.me()
      user = me
      await adoptServerPrefs(me)
    } catch (err) {
      if (!(err instanceof ApiError) || err.status !== 401) console.error(err)
      user = null
    } finally {
      loading = false
    }
  }

  boot()

  function save() {
    api.setPrefs(theme, mode).catch((err) => console.error(err))
  }

  // Picking a theme on the login screen has to survive logging in, so the
  // choice is pushed up the moment there is a session to push it to.
  async function authenticated(u) {
    user = u
    if (touched) {
      save()
      return
    }
    await api.me().then(adoptServerPrefs).catch((err) => console.error(err))
  }

  function setTheme(next, nextMode) {
    theme = next
    mode = nextMode
    touched = true
    applyTheme(next, nextMode)
    if (user) save()
  }

  async function logout() {
    await api.logout()
    user = null
    touched = false
  }
</script>

{#if loading}
  <div class="grid min-h-dvh place-items-center">
    <div class="flex items-center gap-3 text-muted">
      <span class="h-2 w-2 animate-ping rounded-full bg-accent"></span>
      Loading…
    </div>
  </div>
{:else if user}
  <Shell {user} {theme} {mode} {setTheme} {logout} />
{:else}
  <Login onauthenticated={authenticated} {theme} {mode} {setTheme} />
{/if}
