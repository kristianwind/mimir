<script>
  import { api } from './api.js'
  import ThemePicker from './ThemePicker.svelte'
  import Manual from './Manual.svelte'
  import Accounts from './Accounts.svelte'
  import Characters from './Characters.svelte'
  import Artifacts from './Artifacts.svelte'
  import Plan from './Plan.svelte'
  import Potential from './Potential.svelte'
  import Compare from './Compare.svelte'
  import Goals from './Goals.svelte'
  import KvasirChat from './KvasirChat.svelte'
  import { kvasirStatus } from './Kvasir.svelte'
  import Account from './Account.svelte'
  import System from './System.svelte'
  import Users from './Users.svelte'

  let { user, theme, mode, setTheme, logout, hosted = false } = $props()
  const me = $derived(user)

  const PAGES = [
    { key: 'plan', label: 'Plan', icon: '◎', hint: 'What should you spend resin on?' },
    { key: 'kvasir', label: 'Kvasir', icon: '🜛', hint: 'Ask how to get better', ai: true },
    {
      key: 'potential',
      label: 'Potential',
      icon: '△',
      hint: 'Who is worth building, measured with one ruler',
    },
    { key: 'goals', label: 'Goals', icon: '⌖', hint: 'Who are you building, and how do you play them?' },
    { key: 'characters', label: 'Characters', icon: '☗', hint: 'Your roster' },
    {
      key: 'compare',
      label: 'Compare',
      icon: '⇄',
      hint: 'Your builds against a published showcase, on the same ruler',
    },
    { key: 'artifacts', label: 'Artifacts', icon: '✦', hint: 'The whole inventory' },
    { key: 'accounts', label: 'Accounts', icon: '⌂', hint: 'UID and import' },
    { key: 'account', label: 'Settings', icon: '☖', hint: 'Your sign-in, subscription and appearance' },
    { key: 'system', label: 'System', icon: '⚙', hint: 'Version, updates and beacon', admin: true },
    { key: 'users', label: 'Users', icon: '☺', hint: 'Accounts, roles and passwords', admin: true },
  ]

  // The AI layer is optional, so its page only exists when a model does.
  // Everything else on this list works whether or not one is configured.
  let ai = $state(false)
  kvasirStatus().then((s) => (ai = !!s?.enabled))

  // Two axes decide what is on the list. The AI page exists only when a
  // model does, and the two instance-operation pages exist only for an
  // administrator — everything else is the product and belongs to whoever
  // is signed in.
  //
  // The server has always refused these to a normal user; the nav offered
  // them anyway, so the refusal arrived as a page full of errors instead of
  // as an absence. Someone given an account to use the thing should not be
  // shown the controls for the machine it runs on.
  const NAV = $derived(
    PAGES.filter((item) => (!item.ai || ai) && (!item.admin || me?.role === 'admin')),
  )

  // A view can survive a change of role — a demotion, or a session restored
  // from a link — so the shell falls back rather than rendering a page the
  // reader is not allowed to see.
  $effect(() => {
    if (!NAV.some((item) => item.key === view)) view = 'plan'
  })

  // The manual opens beside the page, on the section for whatever view is
  // showing, and leaves the page usable underneath.
  let manual = $state(false)

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

<!--
  The manual is a fixed panel, so the page is given room for it rather than
  being covered by it: the point is to read the manual and use the thing it
  describes at the same time. Below the breakpoint the panel takes the screen,
  which is the only honest option on a phone.
-->
<div
  class="mx-auto flex min-h-dvh max-w-7xl gap-6 p-4 transition-[padding] sm:p-6
         {manual ? 'md:pr-[27rem]' : ''}"
>
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
          Game data is missing. Run a sync, or nothing can be calculated.
        </p>
      {/if}
      <p class="text-xs text-muted">{user.username}</p>
      <button class="btn-ghost w-full text-xs" onclick={logout}>Log out</button>
    </div>
  </aside>

  <main class="min-w-0 flex-1">
    <header class="mb-6 flex flex-wrap items-center justify-between gap-4">
      <div>
        <h1 class="text-xl font-semibold tracking-tight">
          {NAV.find((n) => n.key === view)?.label ?? ''}
        </h1>
        <p class="text-sm text-muted">{NAV.find((n) => n.key === view)?.hint ?? ''}</p>
      </div>
      <div class="flex items-center gap-2">
        <button
          type="button"
          class="btn-ghost px-3 text-sm"
          aria-expanded={manual}
          title="Manual"
          onclick={() => (manual = !manual)}
        >
          {manual ? 'Close manual' : 'Manual'}
        </button>
        <!--
          Ten controls for a preference somebody sets once. On a phone they
          filled the space above the content, so on a narrow screen they move
          to the Account page and the header keeps only what is about the
          page you are on.
        -->
        <div class="hidden sm:flex">
          <ThemePicker {theme} {mode} {setTheme} />
        </div>
      </div>
    </header>

    <!--
      Wrapped rather than scrolled sideways. A horizontal scroll put the last
      entries off the edge of a phone with nothing to say they were there —
      Compare was cut to "Co".
    -->
    <nav class="mb-6 flex flex-wrap gap-1 md:hidden">
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

    {#if view === 'account'}
      <Account {me} {theme} {mode} {setTheme} />
    {:else if view === 'system'}
      <System user={me} {hosted} />
    {:else if view === 'users'}
      <Users {me} />
    {:else if view === 'accounts'}
      <Accounts {accounts} onchange={refresh} />
    {:else if !selected}
      <div class="card p-8 text-center">
        <p class="text-muted">Add your UID under Accounts to get started.</p>
        <button class="btn-primary mt-4" onclick={() => (view = 'accounts')}>Add an account</button>
      </div>
    {:else if view === 'characters'}
      <Characters account={selected} />
    {:else if view === 'artifacts'}
      <Artifacts account={selected} />
    {:else if view === 'compare'}
      <Compare account={selected} />
    {:else if view === 'potential'}
      <Potential account={selected} ongotogoals={() => (view = 'goals')} />
    {:else if view === 'goals'}
      <Goals account={selected} />
    {:else if view === 'kvasir'}
      <KvasirChat account={selected} />
    {:else}
      <Plan account={selected} ongotogoals={() => (view = 'goals')} />
    {/if}
  </main>

  <Manual open={manual} section={view} onclose={() => (manual = false)} />
</div>
