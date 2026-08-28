<script>
  /**
   * Enrolling and removing passkeys.
   *
   * The panel says what a passkey is for rather than what it is. "A key your
   * phone holds" is a fact about the implementation; "a sign-in that a fake
   * page cannot collect" is the reason to bother — and it is a real reason,
   * because the authenticator will not answer a domain it does not
   * recognise, which is a promise no amount of care while typing can make.
   */
  import { api } from './api.js'
  import { supported, create } from './passkey.js'

  let list = $state([])
  let available = $state(false)
  let error = $state('')
  let busy = $state('')
  let password = $state('')

  // The credential API is unavailable over plain http except on localhost.
  // The browser decides that, not us, so the button explains itself rather
  // than failing when pressed.
  const secure = $derived(
    typeof window !== 'undefined' && (window.isSecureContext ?? location.protocol === 'https:'),
  )

  async function load() {
    try {
      const r = await api.passkeys()
      list = r.passkeys ?? []
      available = r.available
    } catch (err) {
      error = err.message
    }
  }
  load()

  async function add() {
    busy = 'add'
    error = ''
    try {
      const { options, challenge } = await api.passkeyRegisterBegin()
      const name = prompt('What should this passkey be called?', deviceGuess()) ?? deviceGuess()
      const response = await create(options)
      await api.passkeyRegisterFinish(challenge, name, response)
      await load()
    } catch (err) {
      // A refusal at the authenticator is a choice, not a fault, and must
      // not be shouted at somebody who simply changed their mind.
      error = err.name === 'NotAllowedError' ? '' : err.message
    } finally {
      busy = ''
    }
  }

  async function remove(id) {
    busy = `del-${id}`
    error = ''
    try {
      await api.passkeyDelete(id, password)
      password = ''
      await load()
    } catch (err) {
      error = err.hint ? `${err.message} — ${err.hint}` : err.message
    } finally {
      busy = ''
    }
  }

  function deviceGuess() {
    const ua = navigator.userAgent
    if (/iPhone|iPad/.test(ua)) return 'iPhone'
    if (/Android/.test(ua)) return 'Android phone'
    if (/Mac/.test(ua)) return 'Mac'
    if (/Windows/.test(ua)) return 'Windows PC'
    return 'Passkey'
  }

  const day = (iso) => (iso ? new Date(iso.replace(' ', 'T') + 'Z').toLocaleDateString() : '')
</script>

{#if available}
  <section class="card p-5">
    <h2 class="mb-1 font-medium">Passkeys</h2>
    <p class="mb-4 text-xs leading-relaxed text-muted">
      Sign in with your fingerprint, face or device PIN instead of typing anything. A passkey will
      only answer the real address of this site, so a convincing copy of the sign-in page cannot
      collect it — which is the one thing no amount of care while typing can promise you.
    </p>

    {#if error}
      <p class="mb-3 text-sm text-bad">{error}</p>
    {/if}

    {#if list.length}
      <ul class="mb-4 space-y-2">
        {#each list as k (k.id)}
          <li
            class="flex flex-wrap items-center justify-between gap-3 rounded-xl bg-raised px-3 py-2"
          >
            <div>
              <p class="text-sm font-medium">{k.name}</p>
              <p class="text-xs text-muted">
                Added {day(k.createdAt)}{k.lastUsedAt
                  ? ` · last used ${day(k.lastUsedAt)}`
                  : ' · never used'}
              </p>
            </div>
            <button
              class="btn-ghost text-xs text-bad"
              disabled={busy === `del-${k.id}` || !password}
              onclick={() => remove(k.id)}
            >
              Remove
            </button>
          </li>
        {/each}
      </ul>
      <div class="mb-4 max-w-xs">
        <label class="label" for="pk-pass">Your password, to remove one</label>
        <input
          id="pk-pass"
          class="field"
          type="password"
          autocomplete="current-password"
          bind:value={password}
        />
      </div>
    {/if}

    {#if !supported()}
      <p class="text-xs text-muted">This browser does not support passkeys.</p>
    {:else if !secure}
      <p class="text-xs text-warn">
        Passkeys need a secure connection. Reach this site over https and the option appears.
      </p>
    {:else}
      <button class="btn-primary" disabled={busy === 'add'} onclick={add}>
        {busy === 'add' ? 'Waiting for your device…' : list.length ? 'Add another' : 'Add a passkey'}
      </button>
    {/if}
  </section>
{/if}
