<script>
  /**
   * The public face of the hosted instance.
   *
   * These pages need real URLs, not tabs: Stripe links to the terms and the
   * refund policy during review, and a link that only works if you clicked
   * your way there from the front page is not a link. The server already
   * falls back to index.html for unknown paths, so pushState routing survives
   * a hard refresh and a pasted address.
   *
   * It is a dozen lines of routing rather than a router because there are six
   * static pages and one of Mimir's few standing rules is that the frontend
   * carries no dependency it can do without.
   */
  import { LANDING, PRICING, LEGAL, PAGES, PRICE } from './site.js'
  import ThemePicker from '../ThemePicker.svelte'

  let { theme, mode, setTheme, onsignin } = $props()

  let path = $state(window.location.pathname)

  function go(next, event) {
    event?.preventDefault()
    if (next === path) return
    window.history.pushState({}, '', next)
    path = next
    window.scrollTo(0, 0)
  }

  // The back button has to work, or the address bar is lying about where you
  // are.
  function onpopstate() {
    path = window.location.pathname
  }

  const legal = $derived(
    { '/terms': LEGAL.terms, '/privacy': LEGAL.privacy, '/refunds': LEGAL.refunds, '/contact': LEGAL.contact }[
      path
    ],
  )
</script>

<svelte:window {onpopstate} />

<div class="mx-auto flex min-h-dvh max-w-3xl flex-col px-5 py-6">
  <header class="mb-10 flex items-center justify-between gap-4">
    <a href="/" onclick={(e) => go('/', e)} class="flex items-center gap-2.5">
      <span class="grid h-8 w-8 place-items-center rounded-xl bg-accent/15">🜁</span>
      <span class="font-semibold tracking-tight">Mimir</span>
    </a>
    <div class="flex items-center gap-2">
      <ThemePicker {theme} {mode} {setTheme} />
      <button class="btn-ghost text-sm" onclick={onsignin}>Sign in</button>
    </div>
  </header>

  <main class="flex-1">
    {#if path === '/pricing'}
      <h1 class="text-2xl font-semibold tracking-tight">{PRICING.title}</h1>
      <p class="mt-2 text-muted">{PRICING.intro}</p>

      <div class="mt-6 grid gap-3 sm:grid-cols-2">
        {#each PRICING.plans as plan}
          <div class="card p-5 {plan.best ? 'border-accent' : ''}">
            <p class="text-sm text-muted">{plan.name}</p>
            <p class="mt-1">
              <span class="text-3xl font-semibold tracking-tight">{plan.price}</span>
              <span class="text-sm text-muted"> {plan.per}</span>
            </p>
            <p class="mt-2 text-sm text-muted">{plan.note}</p>
          </div>
        {/each}
      </div>

      <ul class="mt-6 space-y-2 text-sm text-muted">
        {#each PRICING.included as line}
          <li>· {line}</li>
        {/each}
      </ul>

      <p class="mt-6 rounded-xl bg-raised px-4 py-3 text-sm leading-relaxed text-muted">
        {PRICING.trial}
      </p>
      <p class="mt-3 text-sm leading-relaxed text-muted">{PRICING.selfHost}</p>
    {:else if legal}
      <h1 class="text-2xl font-semibold tracking-tight">{legal.title}</h1>
      {#if legal.updated}
        <p class="mt-1 text-xs text-muted">Last updated {legal.updated}</p>
      {/if}
      <div class="mt-6 space-y-6">
        {#each legal.sections as [heading, body]}
          <section>
            <h2 class="text-sm font-medium">{heading}</h2>
            <p class="mt-1.5 text-sm leading-relaxed text-muted">{body}</p>
          </section>
        {/each}
      </div>
    {:else}
      <h1 class="text-3xl font-semibold tracking-tight">{LANDING.tagline}</h1>
      <p class="mt-4 leading-relaxed text-muted">{LANDING.intro}</p>

      <div class="mt-8 flex flex-wrap items-center gap-3">
        <a href="/pricing" onclick={(e) => go('/pricing', e)} class="btn-primary">
          Start {PRICE.trialDays} days free
        </a>
        <span class="text-sm text-muted">No card.</span>
      </div>

      <div class="mt-10 space-y-6">
        {#each LANDING.points as [heading, body]}
          <section>
            <h2 class="font-medium">{heading}</h2>
            <p class="mt-1.5 text-sm leading-relaxed text-muted">{body}</p>
          </section>
        {/each}
      </div>

      <!--
        The honest answer to "why pay for something that is free". Given its
        own box rather than buried, because a reader who finds this out later
        rather than here has been misled by omission.
      -->
      <div class="mt-10 card p-5">
        <h2 class="font-medium">{LANDING.why.title}</h2>
        <p class="mt-1.5 text-sm leading-relaxed text-muted">{LANDING.why.body}</p>
      </div>
    {/if}
  </main>

  <footer class="mt-12 border-t border-line pt-5">
    <nav class="flex flex-wrap gap-x-4 gap-y-2 text-xs text-muted">
      {#each PAGES as page}
        <a
          href={page.path}
          onclick={(e) => go(page.path, e)}
          class={path === page.path ? 'text-accent' : 'hover:text-fg'}
        >
          {page.label}
        </a>
      {/each}
    </nav>
    <p class="mt-3 text-xs text-muted">
      Not affiliated with HoYoverse. Game data and imagery belong to their owners.
    </p>
  </footer>
</div>
