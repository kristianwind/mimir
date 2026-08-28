<script>
  import { api } from './api.js'
  import BuyMeACoffee from './BuyMeACoffee.svelte'

  let { user } = $props()

  // Syncing, updating and the beacon are administrator actions. The page
  // still shows them, because knowing when the game data was last synced is
  // worth having whoever you are — but it says who can act rather than
  // letting the button fail on click with a permission error.
  const admin = $derived(user?.role === 'admin')

  let status = $state(null)
  let error = $state('')
  let busy = $state('')
  let message = $state('')

  let receiver = $state(null)
  let mine = $state(null)
  let ai = $state(null)
  let probe = $state(null)
  // Seeded from the snapshot in use, because re-syncing the version you are
  // already on is the ordinary case: a new roster, new material bills, the
  // same game version. An empty field with a placeholder that reads like a
  // value left the button greyed out with nothing on the page saying why.
  let gameVersion = $state('')
  let gamedata = $state(null)
  let poll = null

  async function load() {
    error = ''
    try {
      status = await api.system()
    } catch (err) {
      error = err.message
    }
    // Only administrators can see the collector, so a failure here is a
    // permission answer rather than a fault.
    ai = await api.kvasirStatus().catch(() => null)
    receiver = await api.receiver().catch(() => null)
    mine = await api.mineStatus().catch(() => null)
    gamedata = await api.gamedata().catch(() => null)
    if (!gameVersion && gamedata?.active) gameVersion = gamedata.active
    watchMine()
  }

  // Poll only while something is happening. A timer that keeps firing after
  // the job finished is a background request every second for as long as the
  // tab stays open.
  function watchMine() {
    clearInterval(poll)
    if (!mine?.running) return
    poll = setInterval(async () => {
      mine = await api.mineStatus().catch(() => mine)
      if (!mine?.running) {
        clearInterval(poll)
        status = await api.system().catch(() => status)
      }
    }, 1500)
  }

  $effect(() => () => clearInterval(poll))

  // The endpoint is probed rather than trusted: a configured URL a container
  // cannot reach is the usual failure, and it should be visible here rather
  // than as an opinion that never arrives.
  async function checkAI() {
    busy = 'ai'
    probe = null
    try {
      probe = await api.kvasirCheck()
    } catch (err) {
      probe = { ok: false, error: err.hint ? `${err.message} — ${err.hint}` : err.message }
    } finally {
      busy = ''
    }
  }

  async function startMine() {
    error = ''
    message = ''
    busy = 'mine'
    try {
      mine = await api.startMine(gameVersion.trim())
      watchMine()
    } catch (err) {
      error = err.hint ? `${err.message} — ${err.hint}` : err.message
    } finally {
      busy = ''
    }
  }

  load()

  async function run(name, fn, done) {
    busy = name
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

  const update = $derived(status?.update ?? null)
  const beacon = $derived(status?.beacon ?? null)

  let collector = $state('')
  $effect(() => {
    if (beacon && collector === '') collector = beacon.url ?? ''
  })
</script>

{#if error}
  <p class="mb-4 rounded-xl border border-bad/40 bg-bad/10 px-4 py-3 text-sm text-bad" role="alert">
    {error}
  </p>
{/if}
{#if message}
  <p class="mb-4 rounded-xl border border-good/40 bg-good/10 px-4 py-3 text-sm">{message}</p>
{/if}

{#if !status}
  <p class="text-sm text-muted">Loading…</p>
{:else}
  <div class="space-y-4">
    <section class="card p-5">
      <div class="flex flex-wrap items-baseline justify-between gap-3">
        <h2 class="font-medium">Version</h2>
        <span class="chip font-mono">{status.version}</span>
      </div>

      {#if update?.error}
        <p class="mt-3 text-sm text-warn">
          Could not check for updates: {update.error}
        </p>
      {:else if update?.updateAvailable}
        <p class="mt-3 text-sm">
          {update.latest} has been released.
        </p>
        {#if update.notes}
          <pre class="mt-3 max-h-56 overflow-auto whitespace-pre-wrap rounded-xl bg-raised p-3 text-xs text-muted">{update.notes}</pre>
        {/if}

        {#if update.canApply}
          <p class="mt-3 max-w-prose text-xs text-muted">
            {@html 'Mimir downloads the binary, verifies its checksum, <em>starts it</em> and waits ' +
              'for it to answer a health check — only then is anything replaced. If the new version ' +
              `still fails to come up afterwards, a watchdog rolls back to ${status.version}.`}
          </p>
          <button
            class="btn-primary mt-4"
            disabled={busy === 'update'}
            onclick={() =>
              run('update', api.applyUpdate, (r) => `Updated to ${r.to}. ${r.note}`)}
          >
            {busy === 'update'
              ? 'Updating…'
              : `Update to ${update.latest}`}
          </button>
        {:else}
          <p class="mt-3 max-w-prose rounded-xl border border-warn/40 bg-warn/10 px-3 py-2 text-sm text-warn">
            {update.reason}
          </p>
        {/if}
      {:else}
        <p class="mt-3 text-sm text-muted">
          {update?.latest
            ? `You are running the newest version (${update.latest}).`
            : 'No newer version found.'}
        </p>
      {/if}

      <div class="mt-4 flex flex-wrap gap-2">
        <button class="btn-ghost text-xs" disabled={busy === 'check'} onclick={() => run('check', api.checkUpdate)}>
          {busy === 'check' ? 'Checking…' : 'Check now'}
        </button>
        {#if update?.backup}
          <button
            class="btn-ghost text-xs"
            disabled={busy === 'rollback'}
            onclick={() =>
              run('rollback', api.rollback, (r) =>
                `Restored ${r.restored}. ${r.note}`,
              )}
          >
            Roll back to {update.backup}
          </button>
        {/if}
      </div>

      {#if update?.appliedAt}
        <p class="mt-3 text-xs text-muted">
          Last updated to {update.appliedTo} on {update.appliedAt.slice(0, 10)}.
        </p>
      {/if}
    </section>

    <section class="card p-5">
      <h2 class="font-medium">Game data</h2>
      <p class="mt-2 max-w-prose text-sm text-muted">
        Fetches from the public datamines, verifies the effect rules against the game’s own wording
        and activates the result. Takes about half a minute. If anything fails, nothing is swapped —
        the current snapshot stays.
      </p>

      <div class="mt-4 flex flex-wrap items-end gap-3">
        <div>
          <label class="label" for="gv">Game version</label>
          <input
            id="gv"
            class="field w-32"
            placeholder="7.0.0"
            bind:value={gameVersion}
            disabled={mine?.running}
          />
        </div>
        <button
          class="btn-primary"
          disabled={!admin || mine?.running || busy === 'mine' || !gameVersion.trim()}
          onclick={startMine}
        >
          {mine?.running ? `Syncing… ${mine.elapsed}s` : 'Sync game data'}
        </button>
      </div>

      <!--
        A disabled control has to say what would enable it. Without this the
        button is simply dead, and the field beside it looks filled in because
        its placeholder is a plausible version number.
      -->
      {#if !admin}
        <p class="mt-2 text-xs text-muted">
          Syncing is an administrator action. Ask whoever set this instance up.
        </p>
      {:else if !mine?.running && !gameVersion.trim()}
        <p class="mt-2 text-xs text-muted">
          Give it a version to label the snapshot with — the one the game is on, like 7.0.0. It is
          only a label, and syncing the version you are already on is normal: it is how you pick up
          a new roster or new material bills.
        </p>
      {:else if gamedata?.active && gameVersion.trim() === gamedata.active}
        <p class="mt-2 text-xs text-muted">
          This will replace the snapshot in use ({gamedata.active}, {gamedata.characters} characters).
          The old one stays in the list below and you can go back to it.
        </p>
      {/if}

      {#if mine?.lines?.length}
        <pre class="mt-3 max-h-44 overflow-auto whitespace-pre-wrap rounded-xl bg-raised p-3 text-xs">{mine.lines.join(
            '\n',
          )}</pre>
      {/if}
      {#if mine?.error}
        <p class="mt-3 rounded-xl border border-bad/40 bg-bad/10 px-3 py-2 text-xs text-bad">
          {mine.error}
        </p>
      {/if}
      {#if mine?.warnings?.length}
        <ul class="mt-2 space-y-1 text-xs text-warn">
          {#each mine.warnings as line}
            <li>· {line}</li>
          {/each}
        </ul>
      {/if}
    </section>

    <section class="card p-5">
      <div class="flex flex-wrap items-baseline justify-between gap-3">
        <h2 class="font-medium">Kvasir</h2>
        <span class="chip">{ai?.enabled ? 'on' : 'off'}</span>
      </div>

      <p class="mt-3 max-w-prose text-sm text-muted">
        The AI layer explains what the engine calculated and answers questions about it. It never
        calculates: every figure it writes is checked back against the numbers it was given, and one
        that is not there is removed before you see it.
      </p>

      {#if ai?.enabled}
        <p class="mt-3 text-sm">Model: {ai.model || 'whatever the endpoint serves'}</p>
        <button class="btn-ghost mt-4" disabled={busy === 'ai'} onclick={checkAI}>
          {busy === 'ai' ? 'Checking…' : 'Check the endpoint'}
        </button>
        {#if probe}
          <p class="mt-3 text-sm {probe.ok ? 'text-good' : 'text-bad'}">
            {probe.ok
              ? `${probe.endpoint} answered, and serves ${probe.models?.length ?? 0} models.`
              : probe.error}
          </p>
        {/if}
      {:else}
        <p class="mt-3 max-w-prose text-sm text-muted">
          Set MIMIR_LLM_BASE_URL to an OpenAI-compatible endpoint — LM Studio, Ollama, vLLM — and
          restart. Leave it blank and every other part of Mimir works exactly as it does now.
        </p>
      {/if}
    </section>

    <section class="card p-5">
      <div class="flex flex-wrap items-baseline justify-between gap-3">
        <h2 class="font-medium">Beacon</h2>
        <span class="chip">{beacon?.enabled ? 'on' : 'off'}</span>
      </div>

      <p class="mt-3 max-w-prose text-sm text-muted">
        One ping a day, so the project can see how many installations exist and which version they
        run. It sends exactly this and nothing else — no UIDs, no accounts, no inventory:
      </p>
      <pre class="mt-3 overflow-auto rounded-xl bg-raised p-3 text-xs">{JSON.stringify(
          beacon?.payload ?? {},
          null,
          2,
        )}</pre>
      <div class="mt-4">
        <label class="label" for="collector">Collector address</label>
        <input
          id="collector"
          class="field font-mono text-xs"
          placeholder="https://din-collector/api/beacon"
          bind:value={collector}
        />
        <p class="mt-1.5 text-xs text-muted">
          There is deliberately no default address. A beacon has to know where it reports —
          otherwise the ping either goes nowhere or somewhere it does not belong.
        </p>
      </div>

      {#if !beacon?.chosen}
        <p class="mt-3 text-sm">It is switched off until you say otherwise.</p>
      {/if}

      <div class="mt-4 flex flex-wrap gap-2">
        <button
          class={beacon?.enabled ? 'btn-ghost' : 'btn-primary'}
          disabled={busy === 'beacon'}
          onclick={() => run('beacon', () => api.setBeacon(!beacon.enabled, collector))}
        >
          {beacon?.enabled ? 'Turn off' : 'Turn on'}
        </button>
      </div>

      {#if beacon?.lastError}
        <p class="mt-3 text-xs text-warn">
          The last attempt failed: {beacon.lastError}
        </p>
      {:else if beacon?.lastDay}
        <p class="mt-3 text-xs text-muted">
          Last sent {beacon.lastDay} as {beacon.lastVersion}.
        </p>
      {/if}
    </section>

    {#if receiver}
      <section class="card p-5">
        <div class="flex flex-wrap items-baseline justify-between gap-3">
          <h2 class="font-medium">This instance as collector</h2>
          <span class="chip">{receiver.enabled ? 'on' : 'off'}</span>
        </div>

        <p class="mt-3 max-w-prose text-sm text-muted">
          One instance can receive the others’ beacons. Switch it on here, and point the other
          installations at the address below. Only the anonymous instance id and the version are
          stored — no IP, no user agent, no request data. The sender promises its operator that
          nothing else leaves the machine, and that promise has to hold at this end too.
        </p>

        <div class="mt-3">
          <p class="label">Address for the other instances</p>
          <pre class="overflow-auto rounded-xl bg-raised p-3 text-xs">{receiver.endpoint}</pre>
          {#if !receiver.enabled}
            <p class="mt-1.5 text-xs text-muted">
              The endpoint answers 404 until you switch it on — an instance that is not a collector
              should not advertise something it rejects anyway.
            </p>
          {/if}
        </div>

        {#if receiver.enabled}
          <dl class="mt-4 grid grid-cols-3 gap-2 text-center">
            <div class="rounded-xl bg-raised py-3">
              <dt class="text-xs text-muted">installations</dt>
              <dd class="text-lg font-medium">{receiver.total}</dd>
            </div>
            <div class="rounded-xl bg-raised py-3">
              <dt class="text-xs text-muted">active 7 days</dt>
              <dd class="text-lg font-medium">{receiver.active7d}</dd>
            </div>
            <div class="rounded-xl bg-raised py-3">
              <dt class="text-xs text-muted">active 30 days</dt>
              <dd class="text-lg font-medium">{receiver.active30d}</dd>
            </div>
          </dl>

          {#if receiver.versions?.length}
            <div class="mt-3 space-y-1">
              {#each receiver.versions as v (v.version)}
                <div class="flex items-center justify-between rounded-lg bg-raised px-3 py-1.5 text-xs">
                  <span class="font-mono">{v.version}</span>
                  <span class="text-muted">{v.count}</span>
                </div>
              {/each}
            </div>
          {/if}
        {/if}

        <button
          class={receiver.enabled ? 'btn-ghost mt-4' : 'btn-primary mt-4'}
          disabled={busy === 'receiver'}
          onclick={() => run('receiver', () => api.setReceiver(!receiver.enabled))}
        >
          {receiver.enabled ? 'Turn collector off' : 'Turn collector on'}
        </button>
      </section>
    {/if}

    <!--
      Last on the settings page, and nowhere else. Not a banner, not a modal,
      not on first run: a donate prompt standing in front of the thing someone
      has just installed reads as adware. Asking once, quietly, at the bottom
      of the page nobody visits by accident is the whole of it.
    -->
    <section class="card p-5">
      <h2 class="font-medium">Support Mimir</h2>
      <p class="mt-2 max-w-prose text-sm text-muted">
        Mimir is free, with no paid tier and nothing held back — every calculation, the whole
        inventory, the AI layer if you point it at a model. If it saved you an afternoon of
        spreadsheets you can buy the maintainer a coffee. Entirely optional, and nothing in the app
        changes either way.
      </p>
      <div class="mt-4">
        <BuyMeACoffee variant="quiet" label="Buy me a coffee ↗" />
      </div>
    </section>
  </div>
{/if}
