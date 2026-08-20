<script>
  import { api } from './api.js'

  let { me } = $props()

  let users = $state([])
  let error = $state('')
  let message = $state('')
  let busy = $state('')

  // New user
  let name = $state('')
  let password = $state('')
  let role = $state('user')

  // Own password
  let current = $state('')
  let next = $state('')

  async function load() {
    error = ''
    try {
      users = (await api.users()) ?? []
    } catch (err) {
      error = err.hint ? `${err.message} — ${err.hint}` : err.message
    }
  }

  load()

  async function run(tag, fn, done) {
    busy = tag
    error = ''
    message = ''
    try {
      const res = await fn()
      if (done) message = done(res)
      await load()
    } catch (err) {
      error = err.hint ? `${err.message} — ${err.hint}` : err.message
    } finally {
      busy = ''
    }
  }

  function create() {
    return run(
      'create',
      () => api.createUser({ username: name, password, role }),
      () => {
        const created = name
        name = ''
        password = ''
        role = 'user'
        return `Oprettede ${created}.`
      },
    )
  }

  const admins = $derived(users.filter((u) => u.role === 'admin' && !u.disabled).length)

  // The server refuses to strip the last administrator, but a button that
  // looks live and then fails is worse than one that explains itself.
  function lastAdmin(user) {
    return user.role === 'admin' && !user.disabled && admins <= 1
  }
</script>

{#if error}
  <p class="mb-4 rounded-xl border border-bad/40 bg-bad/10 px-4 py-3 text-sm text-bad" role="alert">
    {error}
  </p>
{/if}
{#if message}
  <p class="mb-4 rounded-xl border border-good/40 bg-good/10 px-4 py-3 text-sm">{message}</p>
{/if}

<div class="space-y-4">
  <section class="card p-5">
    <h2 class="mb-4 font-medium">Brugere</h2>
    <div class="space-y-2">
      {#each users as user (user.id)}
        <div class="flex flex-wrap items-center gap-3 rounded-xl bg-raised px-3 py-2.5">
          <div class="min-w-40 flex-1">
            <p class="text-sm font-medium">
              {user.username}
              {#if user.id === me?.id}<span class="text-xs text-muted">· dig</span>{/if}
            </p>
            <p class="text-xs text-muted">
              {user.role === 'admin' ? 'administrator' : 'bruger'}
              {#if user.disabled}· <span class="text-warn">deaktiveret</span>{/if}
              · {user.accounts} konti · {user.sessions} aktive logins
            </p>
          </div>

          <div class="flex flex-wrap gap-1.5">
            {#if lastAdmin(user)}
              <span class="chip text-xs">sidste administrator</span>
            {:else}
              <button
                class="btn-ghost text-xs"
                disabled={busy === `role-${user.id}`}
                onclick={() =>
                  run(`role-${user.id}`, () =>
                    api.updateUser(user.id, { role: user.role === 'admin' ? 'user' : 'admin' }),
                  )}
              >
                {user.role === 'admin' ? 'Gør til bruger' : 'Gør til admin'}
              </button>
              <button
                class="btn-ghost text-xs"
                disabled={busy === `dis-${user.id}`}
                onclick={() =>
                  run(`dis-${user.id}`, () => api.updateUser(user.id, { disabled: !user.disabled }))}
              >
                {user.disabled ? 'Aktivér' : 'Deaktivér'}
              </button>
              <button
                class="btn-ghost text-xs text-bad"
                disabled={busy === `del-${user.id}`}
                onclick={() => {
                  const what =
                    user.accounts > 0
                      ? `Slet ${user.username}? Det tager ${user.accounts} spilkonti med alt inventar med sig.`
                      : `Slet ${user.username}?`
                  if (confirm(what)) run(`del-${user.id}`, () => api.deleteUser(user.id))
                }}
              >
                Slet
              </button>
            {/if}
          </div>
        </div>
      {:else}
        <p class="text-sm text-muted">Ingen brugere.</p>
      {/each}
    </div>
  </section>

  <section class="card p-5">
    <h2 class="mb-1 font-medium">Tilføj en bruger</h2>
    <p class="mb-4 text-xs text-muted">
      Hver bruger har sine egne spilkonti og mål. En administrator kan desuden opdatere Mimir og
      styre brugere.
    </p>
    <div class="flex flex-wrap items-end gap-3">
      <div class="min-w-40 flex-1">
        <label class="label" for="new-name">Brugernavn</label>
        <input id="new-name" class="field" bind:value={name} autocomplete="off" />
      </div>
      <div class="min-w-40 flex-1">
        <label class="label" for="new-pass">Adgangskode</label>
        <input id="new-pass" class="field" type="password" bind:value={password} autocomplete="new-password" />
      </div>
      <div>
        <label class="label" for="new-role">Rolle</label>
        <select id="new-role" class="field w-36" bind:value={role}>
          <option value="user">bruger</option>
          <option value="admin">administrator</option>
        </select>
      </div>
      <button class="btn-primary" disabled={busy === 'create' || !name || !password} onclick={create}>
        {busy === 'create' ? 'Opretter…' : 'Opret'}
      </button>
      <p class="w-full text-xs text-muted">Mindst 12 tegn.</p>
    </div>
  </section>

  <section class="card p-5">
    <h2 class="mb-1 font-medium">Skift din egen adgangskode</h2>
    <p class="mb-4 text-xs text-muted">
      Den nuværende kode skal med — ellers kunne en lånt session gøres permanent. Dine øvrige
      logins bliver logget ud.
    </p>
    <div class="flex flex-wrap items-end gap-3">
      <div class="min-w-40 flex-1">
        <label class="label" for="cur-pass">Nuværende</label>
        <input id="cur-pass" class="field" type="password" bind:value={current} autocomplete="current-password" />
      </div>
      <div class="min-w-40 flex-1">
        <label class="label" for="next-pass">Ny</label>
        <input id="next-pass" class="field" type="password" bind:value={next} autocomplete="new-password" />
      </div>
      <button
        class="btn-primary"
        disabled={busy === 'pw' || !current || !next}
        onclick={() =>
          run('pw', () => api.changePassword(current, next), () => {
            current = ''
            next = ''
            return 'Adgangskoden er skiftet.'
          })}
      >
        {busy === 'pw' ? 'Skifter…' : 'Skift'}
      </button>
    </div>
  </section>
</div>
