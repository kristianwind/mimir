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
        return `Created ${created}.`
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
    <h2 class="mb-4 font-medium">Users</h2>
    <div class="space-y-2">
      {#each users as user (user.id)}
        <div class="flex flex-wrap items-center gap-3 rounded-xl bg-raised px-3 py-2.5">
          <div class="min-w-40 flex-1">
            <p class="text-sm font-medium">
              {user.username}
              {#if user.id === me?.id}<span class="text-xs text-muted">· you</span>{/if}
            </p>
            <p class="text-xs text-muted">
              {user.role === 'admin' ? 'administrator' : 'user'}
              {#if user.disabled}· <span class="text-warn">disabled</span>{/if}
              · {user.accounts} accounts · {user.sessions} active logins
            </p>
          </div>

          <div class="flex flex-wrap gap-1.5">
            {#if lastAdmin(user)}
              <span class="chip text-xs">last administrator</span>
            {:else}
              <button
                class="btn-ghost text-xs"
                disabled={busy === `role-${user.id}`}
                onclick={() =>
                  run(`role-${user.id}`, () =>
                    api.updateUser(user.id, { role: user.role === 'admin' ? 'user' : 'admin' }),
                  )}
              >
                {user.role === 'admin' ? 'Make user' : 'Make admin'}
              </button>
              <button
                class="btn-ghost text-xs"
                disabled={busy === `dis-${user.id}`}
                onclick={() =>
                  run(`dis-${user.id}`, () => api.updateUser(user.id, { disabled: !user.disabled }))}
              >
                {user.disabled ? 'Enable' : 'Disable'}
              </button>
              <button
                class="btn-ghost text-xs text-bad"
                disabled={busy === `del-${user.id}`}
                onclick={() => {
                  const what =
                    user.accounts > 0
                      ? `Delete ${user.username}? That takes ${user.accounts} game accounts ` +
                        'and all their inventory with it.'
                      : `Delete ${user.username}?`
                  if (confirm(what)) run(`del-${user.id}`, () => api.deleteUser(user.id))
                }}
              >
                Delete
              </button>
            {/if}
          </div>
        </div>
      {:else}
        <p class="text-sm text-muted">No users.</p>
      {/each}
    </div>
  </section>

  <section class="card p-5">
    <h2 class="mb-1 font-medium">Add a user</h2>
    <p class="mb-4 text-xs text-muted">
      Every user has their own game accounts and goals. An administrator can also update Mimir and
      manage users.
    </p>
    <div class="flex flex-wrap items-end gap-3">
      <div class="min-w-40 flex-1">
        <label class="label" for="new-name">Username</label>
        <input id="new-name" class="field" bind:value={name} autocomplete="off" />
      </div>
      <div class="min-w-40 flex-1">
        <label class="label" for="new-pass">Password</label>
        <input id="new-pass" class="field" type="password" bind:value={password} autocomplete="new-password" />
      </div>
      <div>
        <label class="label" for="new-role">Role</label>
        <select id="new-role" class="field w-36" bind:value={role}>
          <option value="user">user</option>
          <option value="admin">administrator</option>
        </select>
      </div>
      <button class="btn-primary" disabled={busy === 'create' || !name || !password} onclick={create}>
        {busy === 'create' ? 'Creating…' : 'Create'}
      </button>
      <p class="w-full text-xs text-muted">At least 12 characters.</p>
    </div>
  </section>

</div>
