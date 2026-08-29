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
  import Signup from './Signup.svelte'
  import { api } from '../api.js'

  let { theme, mode, setTheme, onsignin, onauthenticated } = $props()

  // Whether this instance takes signups at all. Asked rather than assumed,
  // because the buttons that lead to the form must not exist when the form
  // would refuse.
  let open = $state(false)
  api
    .instance()
    .then((i) => (open = !!i?.registration))
    .catch(() => (open = false))

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

<div class="mx-auto flex min-h-dvh max-w-4xl flex-col px-5 py-6">
  <header class="mb-10 flex items-center justify-between gap-4">
    <a href="/" onclick={(e) => go('/', e)} class="flex items-center gap-2.5">
      <span class="grid h-8 w-8 place-items-center rounded-xl bg-accent/15">🜁</span>
      <span class="font-semibold tracking-tight">Mimir</span>
    </a>
    <div class="flex items-center gap-2">
      <!--
        Ten controls for a preference, in front of somebody who has not
        decided whether to sign up. On a phone they pushed "Sign in" off the
        edge; on any size they are the wrong thing to offer first.
      -->
      <div class="hidden sm:flex">
        <ThemePicker {theme} {mode} {setTheme} />
      </div>
      <button class="btn-ghost shrink-0 text-sm" onclick={onsignin}>Sign in</button>
    </div>
  </header>

  <main class="flex-1">
    {#if path === '/signup' && open}
      <Signup {onauthenticated} />
    {:else if path === '/pricing'}
      <h1 class="text-2xl font-semibold tracking-tight">{PRICING.title}</h1>
      <p class="mt-2 text-muted">{PRICING.intro}</p>
      <p class="mt-1 text-sm text-muted">{PRICING.taxNote}</p>

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

      {#if open}
        <div class="mt-6">
          <a href="/signup" onclick={(e) => go('/signup', e)} class="btn-primary">
            Start {PRICE.trialDays} days free
          </a>
        </div>
      {/if}

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
      <!-- Hero. One claim, one button, and then the thing itself. -->
      <section class="pb-2">
        <p class="text-sm font-medium tracking-tight text-accent">{LANDING.kicker}</p>
        <h1 class="mt-2 text-3xl font-semibold leading-tight tracking-tight sm:text-4xl">
          {LANDING.tagline}
        </h1>
        <p class="mt-4 max-w-2xl leading-relaxed text-muted">{LANDING.intro}</p>

        <div class="mt-8 flex flex-wrap items-center gap-3">
          <a
            href={open ? '/signup' : '/pricing'}
            onclick={(e) => go(open ? '/signup' : '/pricing', e)}
            class="btn-primary"
          >
            {open ? `Start ${PRICE.trialDays} days free` : 'What it costs'}
          </a>
          {#if open}
            <span class="text-sm text-muted">
              No card. {PRICE.monthly} a month after, or {PRICE.yearly} a year.
            </span>
          {/if}
        </div>
      </section>

      <!--
        The screenshots are the argument, so they are large and they are real.
        The caveats visible inside them are not a flaw in the picture: a page
        that cropped them out would be selling a different product.

        This one is eager rather than lazy: it is the largest thing above the
        fold, and deferring the hero image is how a page ends up showing a
        stranger an empty rectangle.
      -->
      <figure class="mt-10">
        <div class="relative">
          <div
            class="pointer-events-none absolute -inset-x-8 -inset-y-6 rounded-[2rem] bg-accent/10 blur-3xl"
            aria-hidden="true"
          ></div>
          <img
            src={LANDING.proof.shot}
            alt={LANDING.proof.alt}
            width="1408"
            height="636"
            fetchpriority="high"
            class="relative w-full rounded-2xl border border-line shadow-2xl"
          />
        </div>
        <figcaption class="mt-2 text-xs text-muted">{LANDING.proof.caption}</figcaption>
      </figure>

      <section class="mt-14">
        <h2 class="text-xl font-medium tracking-tight">{LANDING.proof.title}</h2>
        <p class="mt-2 max-w-2xl leading-relaxed text-muted">{LANDING.proof.body}</p>
      </section>

      {#each LANDING.sections as section}
        <section class="mt-14">
          <h2 class="text-xl font-medium tracking-tight">{section.title}</h2>
          <p class="mt-2 max-w-2xl leading-relaxed text-muted">{section.body}</p>
          <img
            src={section.shot}
            alt={section.alt}
            width={section.w}
            height={section.h}
            loading="lazy"
            class="mt-5 w-full rounded-2xl border border-line shadow-lg"
          />
        </section>
      {/each}

      <section class="mt-14 rounded-2xl border border-line bg-raised p-6">
        <h2 class="text-xl font-medium tracking-tight">{LANDING.honest.title}</h2>
        <p class="mt-2 max-w-2xl leading-relaxed text-muted">{LANDING.honest.body}</p>
      </section>

      <div class="mt-14 grid gap-6 sm:grid-cols-2">
        {#each LANDING.points as [heading, body]}
          <section>
            <h3 class="font-medium">{heading}</h3>
            <p class="mt-1.5 text-sm leading-relaxed text-muted">{body}</p>
          </section>
        {/each}
      </div>

      <section class="card mt-14 p-6">
        <h2 class="font-medium">{LANDING.why.title}</h2>
        <p class="mt-1.5 text-sm leading-relaxed text-muted">{LANDING.why.body}</p>
      </section>

      <!-- Asked again at the bottom, for somebody who read the whole page. -->
      <section class="mt-14 text-center">
        <a
          href={open ? '/signup' : '/pricing'}
          onclick={(e) => go(open ? '/signup' : '/pricing', e)}
          class="btn-primary"
        >
          {open ? `Start ${PRICE.trialDays} days free` : 'What it costs'}
        </a>
        {#if open}
          <p class="mt-3 text-sm text-muted">
            No card, and nothing to cancel if you stop.
          </p>
        {/if}
      </section>
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
