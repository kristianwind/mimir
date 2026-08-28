<script>
  /**
   * Creating an account, on the public side of the hosted instance.
   *
   * It asks for three things and explains why it asks for the one that is not
   * obvious. An email is wanted here where the rest of Mimir treats it as
   * optional, because a paying customer has to be reachable — a receipt has
   * to go somewhere, and so does the message saying a card stopped working.
   *
   * No card, and the page says so twice: once above the button and once
   * after it. That is the single fact most likely to stop somebody starting,
   * and repeating it costs a line.
   */
  import { api } from '../api.js'
  import { PRICE } from './site.js'

  let { onauthenticated } = $props()

  let username = $state('')
  let email = $state('')
  let password = $state('')
  let error = $state('')
  let busy = $state(false)

  async function submit(event) {
    event.preventDefault()
    error = ''
    if (password.length < 12) {
      error = 'The password must be at least 12 characters.'
      return
    }
    busy = true
    try {
      const user = await api.signup(username, email, password)
      onauthenticated(user)
    } catch (err) {
      error = err.hint ? `${err.message} — ${err.hint}` : err.message
    } finally {
      busy = false
    }
  }
</script>

<h1 class="text-2xl font-semibold tracking-tight">Start your {PRICE.trialDays} days</h1>
<p class="mt-2 text-muted">
  No card, and nothing to cancel. When the {PRICE.trialDays} days are up the account simply stops
  until you decide — nothing is charged and nothing is deleted.
</p>

<form class="mt-6 max-w-sm space-y-4" onsubmit={submit}>
  <div>
    <label class="label" for="su-user">Username</label>
    <input id="su-user" class="field" bind:value={username} autocomplete="username" required />
  </div>
  <div>
    <label class="label" for="su-email">Email</label>
    <input id="su-email" class="field" type="email" bind:value={email} autocomplete="email" required />
    <p class="mt-1.5 text-xs text-muted">
      For receipts, and so there is somewhere to tell you if a payment fails. Nothing else is sent
      to it.
    </p>
  </div>
  <div>
    <label class="label" for="su-pass">Password</label>
    <input
      id="su-pass"
      class="field"
      type="password"
      bind:value={password}
      autocomplete="new-password"
      required
    />
    <p class="mt-1.5 text-xs text-muted">At least 12 characters.</p>
  </div>

  {#if error}
    <p class="rounded-xl border border-bad/40 bg-bad/10 px-3 py-2 text-sm text-bad" role="alert">
      {error}
    </p>
  {/if}

  <button class="btn-primary w-full" disabled={busy}>
    {busy ? 'Creating…' : `Start ${PRICE.trialDays} days free`}
  </button>
  <p class="text-xs text-muted">
    You are not asked for a card and nothing is charged. By continuing you agree to the terms and
    the privacy policy, linked below.
  </p>
</form>
