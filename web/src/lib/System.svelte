<script>
  import { api } from './api.js'
  import { t } from './lang.svelte.js'

  let status = $state(null)
  let error = $state('')
  let busy = $state('')
  let message = $state('')

  let receiver = $state(null)
  let mine = $state(null)
  let ai = $state(null)
  let probe = $state(null)
  let gameVersion = $state('')
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
  <p class="text-sm text-muted">{t('Loading…')}</p>
{:else}
  <div class="space-y-4">
    <section class="card p-5">
      <div class="flex flex-wrap items-baseline justify-between gap-3">
        <h2 class="font-medium">{t('Version')}</h2>
        <span class="chip font-mono">{status.version}</span>
      </div>

      {#if update?.error}
        <p class="mt-3 text-sm text-warn">
          {t('Could not check for updates: {error}', { error: update.error })}
        </p>
      {:else if update?.updateAvailable}
        <p class="mt-3 text-sm">
          {t('{version} has been released.', { version: update.latest })}
        </p>
        {#if update.notes}
          <pre class="mt-3 max-h-56 overflow-auto whitespace-pre-wrap rounded-xl bg-raised p-3 text-xs text-muted">{update.notes}</pre>
        {/if}

        {#if update.canApply}
          <p class="mt-3 max-w-prose text-xs text-muted">
            {@html t(
              'Mimir downloads the binary, verifies its checksum, <em>starts it</em> and waits for it to answer a health check — only then is anything replaced. If the new version still fails to come up afterwards, a watchdog rolls back to {version}.',
              { version: status.version },
            )}
          </p>
          <button
            class="btn-primary mt-4"
            disabled={busy === 'update'}
            onclick={() =>
              run('update', api.applyUpdate, (r) => t('Updated to {to}. {note}', { to: r.to, note: r.note }))}
          >
            {busy === 'update'
              ? t('Updating…')
              : t('Update to {version}', { version: update.latest })}
          </button>
        {:else}
          <p class="mt-3 max-w-prose rounded-xl border border-warn/40 bg-warn/10 px-3 py-2 text-sm text-warn">
            {update.reason}
          </p>
        {/if}
      {:else}
        <p class="mt-3 text-sm text-muted">
          {update?.latest
            ? t('You are running the newest version ({version}).', { version: update.latest })
            : t('No newer version found.')}
        </p>
      {/if}

      <div class="mt-4 flex flex-wrap gap-2">
        <button class="btn-ghost text-xs" disabled={busy === 'check'} onclick={() => run('check', api.checkUpdate)}>
          {busy === 'check' ? t('Checking…') : t('Check now')}
        </button>
        {#if update?.backup}
          <button
            class="btn-ghost text-xs"
            disabled={busy === 'rollback'}
            onclick={() =>
              run('rollback', api.rollback, (r) =>
                t('Restored {restored}. {note}', { restored: r.restored, note: r.note }),
              )}
          >
            {t('Roll back to {version}', { version: update.backup })}
          </button>
        {/if}
      </div>

      {#if update?.appliedAt}
        <p class="mt-3 text-xs text-muted">
          {t('Last updated to {version} on {date}.', {
            version: update.appliedTo,
            date: update.appliedAt.slice(0, 10),
          })}
        </p>
      {/if}
    </section>

    <section class="card p-5">
      <h2 class="font-medium">{t('Game data')}</h2>
      <p class="mt-2 max-w-prose text-sm text-muted">
        {t(
          'Fetches from the public datamines, verifies the effect rules against the game’s own wording and activates the result. Takes about half a minute. If anything fails, nothing is swapped — the current snapshot stays.',
        )}
      </p>

      <div class="mt-4 flex flex-wrap items-end gap-3">
        <div>
          <label class="label" for="gv">{t('Game version')}</label>
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
          disabled={mine?.running || busy === 'mine' || !gameVersion.trim()}
          onclick={startMine}
        >
          {mine?.running ? t('Syncing… {n}s', { n: mine.elapsed }) : t('Sync game data')}
        </button>
      </div>

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
        <h2 class="font-medium">{t('Kvasir')}</h2>
        <span class="chip">{ai?.enabled ? t('on') : t('off')}</span>
      </div>

      <p class="mt-3 max-w-prose text-sm text-muted">
        {t(
          'The AI layer explains what the engine calculated and answers questions about it. It never calculates: every figure it writes is checked back against the numbers it was given, and one that is not there is removed before you see it.',
        )}
      </p>

      {#if ai?.enabled}
        <p class="mt-3 text-sm">{t('Model: {name}', { name: ai.model || t('whatever the endpoint serves') })}</p>
        <button class="btn-ghost mt-4" disabled={busy === 'ai'} onclick={checkAI}>
          {busy === 'ai' ? t('Checking…') : t('Check the endpoint')}
        </button>
        {#if probe}
          <p class="mt-3 text-sm {probe.ok ? 'text-good' : 'text-bad'}">
            {probe.ok
              ? t('{endpoint} answered, and serves {n} models.', {
                  endpoint: probe.endpoint,
                  n: probe.models?.length ?? 0,
                })
              : probe.error}
          </p>
        {/if}
      {:else}
        <p class="mt-3 max-w-prose text-sm text-muted">
          {t(
            'Set MIMIR_LLM_BASE_URL to an OpenAI-compatible endpoint — LM Studio, Ollama, vLLM — and restart. Leave it blank and every other part of Mimir works exactly as it does now.',
          )}
        </p>
      {/if}
    </section>

    <section class="card p-5">
      <div class="flex flex-wrap items-baseline justify-between gap-3">
        <h2 class="font-medium">{t('Beacon')}</h2>
        <span class="chip">{beacon?.enabled ? t('on') : t('off')}</span>
      </div>

      <p class="mt-3 max-w-prose text-sm text-muted">
        {t(
          'One ping a day, so the project can see how many installations exist and which version they run. It sends exactly this and nothing else — no UIDs, no accounts, no inventory:',
        )}
      </p>
      <pre class="mt-3 overflow-auto rounded-xl bg-raised p-3 text-xs">{JSON.stringify(
          beacon?.payload ?? {},
          null,
          2,
        )}</pre>
      <div class="mt-4">
        <label class="label" for="collector">{t('Collector address')}</label>
        <input
          id="collector"
          class="field font-mono text-xs"
          placeholder="https://din-collector/api/beacon"
          bind:value={collector}
        />
        <p class="mt-1.5 text-xs text-muted">
          {t(
            'There is deliberately no default address. A beacon has to know where it reports — otherwise the ping either goes nowhere or somewhere it does not belong.',
          )}
        </p>
      </div>

      {#if !beacon?.chosen}
        <p class="mt-3 text-sm">{t('It is switched off until you say otherwise.')}</p>
      {/if}

      <div class="mt-4 flex flex-wrap gap-2">
        <button
          class={beacon?.enabled ? 'btn-ghost' : 'btn-primary'}
          disabled={busy === 'beacon'}
          onclick={() => run('beacon', () => api.setBeacon(!beacon.enabled, collector))}
        >
          {beacon?.enabled ? t('Turn off') : t('Turn on')}
        </button>
      </div>

      {#if beacon?.lastError}
        <p class="mt-3 text-xs text-warn">
          {t('The last attempt failed: {error}', { error: beacon.lastError })}
        </p>
      {:else if beacon?.lastDay}
        <p class="mt-3 text-xs text-muted">
          {t('Last sent {day} as {version}.', {
            day: beacon.lastDay,
            version: beacon.lastVersion,
          })}
        </p>
      {/if}
    </section>

    {#if receiver}
      <section class="card p-5">
        <div class="flex flex-wrap items-baseline justify-between gap-3">
          <h2 class="font-medium">{t('This instance as collector')}</h2>
          <span class="chip">{receiver.enabled ? t('on') : t('off')}</span>
        </div>

        <p class="mt-3 max-w-prose text-sm text-muted">
          {t(
            'One instance can receive the others’ beacons. Switch it on here, and point the other installations at the address below. Only the anonymous instance id and the version are stored — no IP, no user agent, no request data. The sender promises its operator that nothing else leaves the machine, and that promise has to hold at this end too.',
          )}
        </p>

        <div class="mt-3">
          <p class="label">{t('Address for the other instances')}</p>
          <pre class="overflow-auto rounded-xl bg-raised p-3 text-xs">{receiver.endpoint}</pre>
          {#if !receiver.enabled}
            <p class="mt-1.5 text-xs text-muted">
              {t(
                'The endpoint answers 404 until you switch it on — an instance that is not a collector should not advertise something it rejects anyway.',
              )}
            </p>
          {/if}
        </div>

        {#if receiver.enabled}
          <dl class="mt-4 grid grid-cols-3 gap-2 text-center">
            <div class="rounded-xl bg-raised py-3">
              <dt class="text-xs text-muted">{t('installations')}</dt>
              <dd class="text-lg font-medium">{receiver.total}</dd>
            </div>
            <div class="rounded-xl bg-raised py-3">
              <dt class="text-xs text-muted">{t('active 7 days')}</dt>
              <dd class="text-lg font-medium">{receiver.active7d}</dd>
            </div>
            <div class="rounded-xl bg-raised py-3">
              <dt class="text-xs text-muted">{t('active 30 days')}</dt>
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
          {receiver.enabled ? t('Turn collector off') : t('Turn collector on')}
        </button>
      </section>
    {/if}
  </div>
{/if}
