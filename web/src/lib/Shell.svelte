<script>
  import { api } from './api.js'
  import ThemePicker from './ThemePicker.svelte'
  import Accounts from './Accounts.svelte'
  import Characters from './Characters.svelte'
  import Artifacts from './Artifacts.svelte'
  import Plan from './Plan.svelte'
  import Goals from './Goals.svelte'
  import System from './System.svelte'

  let { user, theme, mode, setTheme, logout } = $props()

  const NAV = [
    { key: 'plan', label: 'Plan', icon: '◎', hint: 'Hvad skal du bruge resin på?' },
    { key: 'goals', label: 'Mål', icon: '⌖', hint: 'Hvem bygger du på, og hvordan spiller du dem?' },
    { key: 'characters', label: 'Karakterer', icon: '☗', hint: 'Din roster' },
    { key: 'artifacts', label: 'Artifacts', icon: '✦', hint: 'Hele inventaret' },
    { key: 'accounts', label: 'Konti', icon: '⌂', hint: 'UID og import' },
    { key: 'system', label: 'System', icon: '⚙', hint: 'Version, opdatering og beacon' },
  ]

  let view = $state('plan')
  let accounts = $state([])
  let selected = $state(null)
  let gamedata = $state(null)

  async function refresh() {
    accounts = (await api.accounts()) ?? []
    if (!selected || !accounts.some((a) => a.id === selected.id)) {
      selected = accounts[0] ?? null
    }
    gamedata = await api.gamedata().catch(() => null)
  }

  refresh()
</script>

<div class="mx-auto flex min-h-dvh max-w-7xl gap-6 p-4 sm:p-6">
  <aside class="hidden w-56 shrink-0 flex-col md:flex">
    <div class="mb-8 flex items-center gap-2 px-2">
      <span class="grid h-9 w-9 place-items-center rounded-xl bg-accent/15 text-lg">🜁</span>
      <span class="text-lg font-semibold tracking-tight">Mimir</span>
    </div>

    <nav class="space-y-1">
      {#each NAV as item (item.key)}
        <button
          type="button"
          onclick={() => (view = item.key)}
          class="flex w-full items-center gap-3 rounded-xl px-3 py-2 text-left text-sm transition
                 {view === item.key
                   ? 'bg-accent/15 text-ink shadow-glow'
                   : 'text-muted hover:bg-raised hover:text-ink'}"
        >
          <span class="w-4 text-center" aria-hidden="true">{item.icon}</span>
          <span>{item.label}</span>
        </button>
      {/each}
    </nav>

    <div class="mt-auto space-y-3 px-2 pt-6">
      {#if gamedata && !gamedata.synced}
        <p class="rounded-xl border border-warn/40 bg-warn/10 px-3 py-2 text-xs text-warn">
          Spildata mangler. Kør en sync, ellers kan der ikke regnes.
        </p>
      {/if}
      <p class="text-xs text-muted">{user.username}</p>
      <button class="btn-ghost w-full text-xs" onclick={logout}>Log ud</button>
    </div>
  </aside>

  <main class="min-w-0 flex-1">
    <header class="mb-6 flex flex-wrap items-center justify-between gap-4">
      <div>
        <h1 class="text-xl font-semibold tracking-tight">
          {NAV.find((n) => n.key === view)?.label}
        </h1>
        <p class="text-sm text-muted">{NAV.find((n) => n.key === view)?.hint}</p>
      </div>
      <ThemePicker {theme} {mode} {setTheme} />
    </header>

    <nav class="mb-6 flex gap-1 overflow-x-auto md:hidden">
      {#each NAV as item (item.key)}
        <button
          type="button"
          onclick={() => (view = item.key)}
          class="chip whitespace-nowrap {view === item.key ? 'border-accent text-ink' : ''}"
        >
          {item.label}
        </button>
      {/each}
    </nav>

    {#if accounts.length > 1}
      <div class="mb-6 flex flex-wrap gap-2">
        {#each accounts as account (account.id)}
          <button
            type="button"
            onclick={() => (selected = account)}
            class="chip {selected?.id === account.id ? 'border-accent text-ink' : ''}"
          >
            {account.nickname || account.uid}
            <span class="text-muted">{account.region}</span>
          </button>
        {/each}
      </div>
    {/if}

    {#if view === 'system'}
      <System />
    {:else if view === 'accounts'}
      <Accounts {accounts} onchange={refresh} />
    {:else if !selected}
      <div class="card p-8 text-center">
        <p class="text-muted">Tilføj dit UID under Konti for at komme i gang.</p>
        <button class="btn-primary mt-4" onclick={() => (view = 'accounts')}>Tilføj konto</button>
      </div>
    {:else if view === 'characters'}
      <Characters account={selected} />
    {:else if view === 'artifacts'}
      <Artifacts account={selected} />
    {:else if view === 'goals'}
      <Goals account={selected} />
    {:else}
      <Plan account={selected} ongotogoals={() => (view = 'goals')} />
    {/if}
  </main>
</div>
