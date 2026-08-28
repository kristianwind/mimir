<script>
  /**
   * The signed-in person's own account: their password and their second
   * factor.
   *
   * Separate from Users, which is for an administrator managing everybody
   * else. Changing your own password used to live there, which meant that
   * gating Users behind the admin role took a normal user's password away
   * with it — the two things were only ever on the same page because there
   * was nowhere else to put them.
   */
  import { api } from './api.js'
  import ThemePicker from './ThemePicker.svelte'
  import Subscription from './Subscription.svelte'

  let { me, theme, mode, setTheme } = $props()

  let error = $state('')
  let message = $state('')
  let busy = $state('')

  let current = $state('')
  let next = $state('')

  // The second factor.
  let status = $state(null)
  let secret = $state('')
  let uri = $state('')
  let code = $state('')
  // Shown exactly once, at the moment they are generated. They are stored
  // hashed, so nobody — including whoever runs the server — can produce them
  // again.
  let recoveryCodes = $state(null)
  let confirmPassword = $state('')

  async function loadStatus() {
    try {
      status = await api.twoFactorStatus()
    } catch (err) {
      error = err.message
    }
  }
  loadStatus()

  async function run(tag, fn, after) {
    busy = tag
    error = ''
    message = ''
    try {
      const result = await fn()
      message = after ? after(result) : ''
    } catch (err) {
      error = err.hint ? `${err.message} — ${err.hint}` : err.message
    } finally {
      busy = ''
    }
  }

  function begin() {
    return run('begin', () => api.twoFactorBegin(), (r) => {
      secret = r.secret
      uri = r.uri
      return ''
    })
  }

  function confirm() {
    return run('confirm', () => api.twoFactorConfirm(code), (r) => {
      recoveryCodes = r.recoveryCodes
      secret = ''
      uri = ''
      code = ''
      loadStatus()
      return ''
    })
  }

  function disable() {
    return run('disable', () => api.twoFactorDisable(confirmPassword), () => {
      confirmPassword = ''
      recoveryCodes = null
      loadStatus()
      return 'Two-factor authentication is off.'
    })
  }

  function regenerate() {
    return run('regen', () => api.twoFactorRecovery(confirmPassword), (r) => {
      recoveryCodes = r.recoveryCodes
      confirmPassword = ''
      loadStatus()
      return ''
    })
  }

  // Displayed in groups, because somebody is going to type this by hand when
  // their camera will not focus on the code.
  const grouped = $derived(secret ? secret.match(/.{1,4}/g).join(' ') : '')
</script>

<div class="space-y-6">
  {#if error}
    <p class="card border-bad p-4 text-sm text-bad">{error}</p>
  {/if}
  {#if message}
    <p class="card p-4 text-sm text-good">{message}</p>
  {/if}

  <Subscription />

  <!-- ------------------------------------------------ second factor -->
  <section class="card p-5">
    <h2 class="mb-1 font-medium">Two-factor authentication</h2>
    <p class="mb-4 text-xs leading-relaxed text-muted">
      A code from an app on your phone, in addition to your password. It means a stolen password
      is not enough on its own.
    </p>

    {#if recoveryCodes}
      <!--
        The one moment these exist in readable form. Said plainly, because a
        user who closes this page without saving them has lost their way back
        in and will not find out until the day they need it.
      -->
      <div class="rounded-xl border border-accent bg-raised p-4">
        <p class="text-sm font-medium">Save these recovery codes now</p>
        <p class="mt-1 text-xs leading-relaxed text-muted">
          Each one signs you in once if you lose your phone. They are stored hashed, so this is the
          only time they can be shown — not even the server can print them again. Closing this page
          without keeping them means your only way back in is an administrator at the machine.
        </p>
        <ul class="mt-3 grid grid-cols-2 gap-x-4 gap-y-1 font-mono text-sm">
          {#each recoveryCodes as c}
            <li>{c}</li>
          {/each}
        </ul>
        <div class="mt-3 flex gap-2">
          <button
            class="btn-ghost text-xs"
            onclick={() => navigator.clipboard?.writeText(recoveryCodes.join('\n'))}
          >
            Copy
          </button>
          <button class="btn-ghost text-xs" onclick={() => (recoveryCodes = null)}>
            I have saved them
          </button>
        </div>
      </div>
    {:else if secret}
      <div class="space-y-3">
        <p class="text-sm">
          Add this to your authenticator app, then type the code it shows to prove it worked.
        </p>
        <a href={uri} class="btn-ghost inline-block text-sm">Open in an authenticator app</a>
        <div>
          <p class="label">Or enter this key by hand</p>
          <p class="select-all font-mono text-sm tracking-wide">{grouped}</p>
        </div>
        <div class="flex flex-wrap items-end gap-3">
          <div class="min-w-40">
            <label class="label" for="totp-code">The six digits it shows</label>
            <input
              id="totp-code"
              class="field font-mono"
              inputmode="numeric"
              autocomplete="one-time-code"
              maxlength="6"
              bind:value={code}
            />
          </div>
          <button class="btn-primary" disabled={busy === 'confirm' || code.length !== 6} onclick={confirm}>
            {busy === 'confirm' ? 'Checking…' : 'Turn it on'}
          </button>
          <button class="btn-ghost" onclick={() => { secret = ''; uri = ''; code = '' }}>
            Cancel
          </button>
        </div>
        <p class="text-xs text-muted">
          Nothing is protected until you finish this step, so stopping here leaves your account
          exactly as it was.
        </p>
      </div>
    {:else if status?.enrolled}
      <p class="text-sm text-good">On.</p>
      <p class="mt-1 text-xs text-muted">
        {status.recoveryRemaining} of your recovery codes are unused.
        {#if status.recoveryRemaining <= 2}
          <span class="text-warn">That is nearly none — generate a new set.</span>
        {/if}
      </p>
      <div class="mt-4 flex flex-wrap items-end gap-3">
        <div class="min-w-48">
          <label class="label" for="tf-pass">Your password</label>
          <input
            id="tf-pass"
            class="field"
            type="password"
            autocomplete="current-password"
            bind:value={confirmPassword}
          />
        </div>
        <button class="btn-ghost" disabled={busy === 'regen' || !confirmPassword} onclick={regenerate}>
          New recovery codes
        </button>
        <button class="btn-ghost text-bad" disabled={busy === 'disable' || !confirmPassword} onclick={disable}>
          Turn off
        </button>
      </div>
      <p class="mt-2 text-xs text-muted">
        Both ask for your password again. A session only proves somebody signed in once, and
        surviving a stolen session is what this is for.
      </p>
    {:else}
      {#if status?.pending}
        <p class="mb-3 text-xs text-warn">
          You started setting this up and did not finish, so nothing is protecting the account yet.
          Starting again gives you a fresh key.
        </p>
      {/if}
      <button class="btn-primary" disabled={busy === 'begin'} onclick={begin}>
        {busy === 'begin' ? 'Preparing…' : 'Set it up'}
      </button>
    {/if}
  </section>

  <!--
    On a wide screen this also lives in the header. On a phone the header is
    for the page you are on, so this is where it is.
  -->
  {#if setTheme}
    <section class="card p-5 sm:hidden">
      <h2 class="mb-1 font-medium">Appearance</h2>
      <p class="mb-4 text-xs text-muted">Follows your account to any device you sign in on.</p>
      <ThemePicker {theme} {mode} {setTheme} />
    </section>
  {/if}

  <!-- ------------------------------------------------------ password -->
  <section class="card p-5">
    <h2 class="mb-1 font-medium">Change your password</h2>
    <p class="mb-4 text-xs text-muted">
      The current password is required — otherwise a borrowed session could be made permanent. Your
      other logins are signed out.
    </p>
    <div class="flex flex-wrap items-end gap-3">
      <div class="min-w-40 flex-1">
        <label class="label" for="cur-pass">Current</label>
        <input id="cur-pass" class="field" type="password" bind:value={current} autocomplete="current-password" />
      </div>
      <div class="min-w-40 flex-1">
        <label class="label" for="next-pass">New</label>
        <input id="next-pass" class="field" type="password" bind:value={next} autocomplete="new-password" />
      </div>
      <button
        class="btn-primary"
        disabled={busy === 'pw' || !current || !next}
        onclick={() =>
          run('pw', () => api.changePassword(current, next), () => {
            current = ''
            next = ''
            return 'The password has been changed.'
          })}
      >
        {busy === 'pw' ? 'Changing…' : 'Change'}
      </button>
      <p class="w-full text-xs text-muted">At least 12 characters. Signed in as {me?.username}.</p>
    </div>
  </section>
</div>
