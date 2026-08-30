<script>
  /**
   * What this account is entitled to, and how to change it.
   *
   * It shows nothing at all on a self-hosted instance. There is nothing to
   * sell there, and a subscription panel on software somebody is running
   * themselves would be an advert in their own house.
   *
   * Every state here is a state the server told us about. Nothing is inferred
   * from having just come back from Stripe, because coming back from Stripe
   * is a URL anybody can be sent.
   */
  import { api } from './api.js'

  let { onstarted } = $props()

  let state = $state(null)
  let error = $state('')
  let busy = $state('')

  async function load() {
    try {
      state = await api.billing()
    } catch (err) {
      error = err.message
    }
  }
  load()

  // Coming back from checkout, Stripe's redirect lands here with a marker.
  // It is used only to know that waiting for the webhook is worth doing —
  // never to decide that anything was paid.
  const returned = new URLSearchParams(location.search).get('checkout')

  // Cleared once acknowledged, so a refresh does not thank somebody twice for
  // the same payment, and a bookmarked URL never thanks them at all.
  function forgetTheMarker() {
    const url = new URL(location.href)
    url.searchParams.delete('checkout')
    history.replaceState({}, '', url)
  }

  let waiting = $state(returned === 'done')
  let waitedTooLong = $state(false)

  if (returned === 'done') {
    let tries = 0
    const poll = setInterval(async () => {
      await load()
      if (state?.access?.reason === 'subscribed') {
        waiting = false
        clearInterval(poll)
      } else if (++tries >= 5) {
        // Said plainly rather than hidden. The payment is Stripe's to
        // confirm and it usually takes a second or two, but if the webhook
        // is misconfigured this is the moment somebody finds out — and the
        // worst thing to do is pretend nothing happened after taking money.
        waiting = false
        waitedTooLong = true
        clearInterval(poll)
      }
    }, 2000)
  }

  async function go(fn, tag) {
    busy = tag
    error = ''
    try {
      const { url } = await fn()
      location.href = url
    } catch (err) {
      error = err.hint ? `${err.message} — ${err.hint}` : err.message
      busy = ''
    }
  }

  const day = (iso) =>
    iso && !iso.startsWith('0001')
      ? new Date(iso).toLocaleDateString(undefined, { day: 'numeric', month: 'long', year: 'numeric' })
      : ''
</script>

<!--
  The moment after somebody hands over money. It used to be silent: Stripe
  redirected to a path this app does not route, so the reader landed on Plan
  with no acknowledgement that anything had happened.

  It thanks them for what the server has confirmed, and never for the redirect
  itself — coming back from Stripe is a URL anybody can be sent, and the only
  thing that means a payment happened is the webhook having written it down.
-->
{#if returned === 'done' && (waiting || waitedTooLong || state?.access?.reason === 'subscribed')}
  <section class="card mb-6 border-accent p-6">
    {#if waiting}
      <h2 class="font-medium">Thank you — confirming with Stripe</h2>
      <p class="mt-2 max-w-prose text-sm leading-relaxed text-muted">
        The payment has gone through on their side. Mimir is waiting to hear it from Stripe
        directly rather than taking the browser's word for it, which usually takes a second or
        two.
      </p>
    {:else if waitedTooLong}
      <h2 class="font-medium">Thank you — but Stripe has not confirmed it yet</h2>
      <p class="mt-2 max-w-prose text-sm leading-relaxed text-muted">
        Your payment is not lost, and you have not been charged twice. Mimir has simply not been
        told about it yet. Give it a minute and reload; if it still says this, get in touch and it
        will be sorted out by hand.
      </p>
    {:else}
      <h2 class="font-medium">Thank you. You are subscribed.</h2>
      <p class="mt-2 max-w-prose text-sm leading-relaxed text-muted">
        Everything is on. Import your inventory under Accounts if you have not yet, and the plan
        will rank every upgrade you can make — starting, usually, with the free ones.
      </p>
      <div class="mt-4 flex flex-wrap gap-2">
        <button class="btn-primary" onclick={() => { forgetTheMarker(); onstarted?.() }}>
          Go to the plan
        </button>
      </div>
    {/if}
  </section>
{/if}

{#if state?.sellable}
  <section class="card p-5">
    <h2 class="mb-1 font-medium">Subscription</h2>

    {#if error}
      <p class="mb-3 text-sm text-bad">{error}</p>
    {/if}

    {#if state.access.reason === 'comped'}
      <p class="text-sm text-good">Free access, given by an administrator.</p>
      <p class="mt-1 text-xs text-muted">Nothing to pay, and nothing to renew.</p>
    {:else if state.access.reason === 'subscribed'}
      <p class="text-sm text-good">Subscribed.</p>
      <p class="mt-1 text-xs text-muted">
        {#if state.access.cancelAtPeriodEnd}
          Cancelled — you keep it until {day(state.access.renewsAt)}, and it will not renew.
        {:else}
          Renews {day(state.access.renewsAt)}.
        {/if}
      </p>
    {:else if state.access.reason === 'trial'}
      <p class="text-sm">
        You are on the free trial until <strong>{day(state.access.trialEndsAt)}</strong>.
      </p>
      <p class="mt-1 text-xs text-muted">No card has been taken. Nothing happens automatically.</p>
    {:else if state.access.reason === 'trial-over'}
      <p class="text-sm text-warn">Your trial ended {day(state.access.trialEndsAt)}.</p>
      <p class="mt-1 text-xs text-muted">Nothing has been deleted. Subscribe and it is all still here.</p>
    {:else if state.access.reason === 'lapsed'}
      <p class="text-sm text-warn">Your subscription has stopped.</p>
      <p class="mt-1 text-xs text-muted">
        Usually an expired card. Manage billing below to put a new one in.
      </p>
    {/if}

    {#if state.access.reason !== 'comped'}
      <div class="mt-4 flex flex-wrap items-center gap-2">
        {#if state.access.reason !== 'subscribed'}
          <button class="btn-primary" disabled={busy} onclick={() => go(() => api.checkout('yearly'), 'y')}>
            {busy === 'y' ? 'Opening…' : '$40 a year'}
          </button>
          <button class="btn-ghost" disabled={busy} onclick={() => go(() => api.checkout('monthly'), 'm')}>
            {busy === 'm' ? 'Opening…' : '$4 a month'}
          </button>
          <span class="text-xs text-muted">Two months cheaper yearly.</span>
        {/if}
        {#if state.hasBilling}
          <button class="btn-ghost text-xs" disabled={busy} onclick={() => go(api.billingPortal, 'p')}>
            {busy === 'p' ? 'Opening…' : 'Manage billing, cancel, change card'}
          </button>
        {/if}
      </div>
    {/if}

    <p class="mt-4 text-xs leading-relaxed text-muted">
      Payment is handled by Stripe; this service never sees your card. Cancelling leaves everything
      in place until the period you paid for runs out, and deletes nothing when it does.
    </p>
  </section>
{/if}
