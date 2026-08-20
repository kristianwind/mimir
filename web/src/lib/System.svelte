<script>
  import { api } from './api.js'

  let status = $state(null)
  let error = $state('')
  let busy = $state('')
  let message = $state('')

  let receiver = $state(null)

  async function load() {
    error = ''
    try {
      status = await api.system()
    } catch (err) {
      error = err.message
    }
    // Only administrators can see the collector, so a failure here is a
    // permission answer rather than a fault.
    receiver = await api.receiver().catch(() => null)
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
  <p class="text-sm text-muted">Henter…</p>
{:else}
  <div class="space-y-4">
    <section class="card p-5">
      <div class="flex flex-wrap items-baseline justify-between gap-3">
        <h2 class="font-medium">Version</h2>
        <span class="chip font-mono">{status.version}</span>
      </div>

      {#if update?.error}
        <p class="mt-3 text-sm text-warn">Kunne ikke tjekke for opdateringer: {update.error}</p>
      {:else if update?.updateAvailable}
        <p class="mt-3 text-sm">
          <span class="font-medium text-good">{update.latest}</span> er udkommet.
        </p>
        {#if update.notes}
          <pre class="mt-3 max-h-56 overflow-auto whitespace-pre-wrap rounded-xl bg-raised p-3 text-xs text-muted">{update.notes}</pre>
        {/if}

        {#if update.canApply}
          <p class="mt-3 max-w-prose text-xs text-muted">
            Mimir henter binæren, tjekker dens checksum, <em>starter den</em> og venter på at den
            svarer på et helbredstjek — først derefter udskiftes noget. Kommer den nye version
            alligevel ikke op bagefter, ruller en vagthund tilbage til {status.version}.
          </p>
          <button
            class="btn-primary mt-4"
            disabled={busy === 'update'}
            onclick={() =>
              run('update', api.applyUpdate, (r) => `Opdateret til ${r.to}. ${r.note}`)}
          >
            {busy === 'update' ? 'Opdaterer…' : `Opdatér til ${update.latest}`}
          </button>
        {:else}
          <p class="mt-3 max-w-prose rounded-xl border border-warn/40 bg-warn/10 px-3 py-2 text-sm text-warn">
            {update.reason}
          </p>
        {/if}
      {:else}
        <p class="mt-3 text-sm text-muted">
          {update?.latest ? `Du kører den nyeste version (${update.latest}).` : 'Ingen nyere version fundet.'}
        </p>
      {/if}

      <div class="mt-4 flex flex-wrap gap-2">
        <button class="btn-ghost text-xs" disabled={busy === 'check'} onclick={() => run('check', api.checkUpdate)}>
          {busy === 'check' ? 'Tjekker…' : 'Tjek nu'}
        </button>
        {#if update?.backup}
          <button
            class="btn-ghost text-xs"
            disabled={busy === 'rollback'}
            onclick={() => run('rollback', api.rollback, (r) => `Gendannede ${r.restored}. ${r.note}`)}
          >
            Rul tilbage til {update.backup}
          </button>
        {/if}
      </div>

      {#if update?.appliedAt}
        <p class="mt-3 text-xs text-muted">
          Sidst opdateret til {update.appliedTo} den {update.appliedAt.slice(0, 10)}.
        </p>
      {/if}
    </section>

    <section class="card p-5">
      <div class="flex flex-wrap items-baseline justify-between gap-3">
        <h2 class="font-medium">Beacon</h2>
        <span class="chip">{beacon?.enabled ? 'til' : 'fra'}</span>
      </div>

      <p class="mt-3 max-w-prose text-sm text-muted">
        Én ping om dagen, så projektet kan se hvor mange installationer der findes og hvilken
        version de kører. Den sender præcis dette og intet andet — ingen UID'er, ingen konti,
        intet inventar:
      </p>
      <pre class="mt-3 overflow-auto rounded-xl bg-raised p-3 text-xs">{JSON.stringify(
          beacon?.payload ?? {},
          null,
          2,
        )}</pre>
      <div class="mt-4">
        <label class="label" for="collector">Collector-adresse</label>
        <input
          id="collector"
          class="field font-mono text-xs"
          placeholder="https://din-collector/api/beacon"
          bind:value={collector}
        />
        <p class="mt-1.5 text-xs text-muted">
          Der er ingen standardadresse med vilje. En beacon skal vide hvor den rapporterer
          hen — ellers ender pingen enten ingen steder eller et sted den ikke hører til.
        </p>
      </div>

      {#if !beacon?.chosen}
        <p class="mt-3 text-sm">Den er slået fra, indtil du siger andet.</p>
      {/if}

      <div class="mt-4 flex flex-wrap gap-2">
        <button
          class={beacon?.enabled ? 'btn-ghost' : 'btn-primary'}
          disabled={busy === 'beacon'}
          onclick={() => run('beacon', () => api.setBeacon(!beacon.enabled, collector))}
        >
          {beacon?.enabled ? 'Slå fra' : 'Slå til'}
        </button>
      </div>

      {#if beacon?.lastError}
        <p class="mt-3 text-xs text-warn">
          Sidste forsøg mislykkedes: {beacon.lastError}
        </p>
      {:else if beacon?.lastDay}
        <p class="mt-3 text-xs text-muted">
          Sidst sendt {beacon.lastDay} som {beacon.lastVersion}.
        </p>
      {/if}
    </section>

    {#if receiver}
      <section class="card p-5">
        <div class="flex flex-wrap items-baseline justify-between gap-3">
          <h2 class="font-medium">Denne instans som collector</h2>
          <span class="chip">{receiver.enabled ? 'til' : 'fra'}</span>
        </div>

        <p class="mt-3 max-w-prose text-sm text-muted">
          Én instans kan tage imod de andres beacons. Slå det til her, og peg de øvrige
          installationer på adressen nedenfor. Der gemmes kun det anonyme instans-id og
          versionen — ingen IP, ingen brugeragent, ingen forespørgselsdata. Afsenderen lover
          sin operatør at intet andet forlader maskinen, og det løfte skal også holde i denne
          ende.
        </p>

        <div class="mt-3">
          <p class="label">Adresse til de andre instanser</p>
          <pre class="overflow-auto rounded-xl bg-raised p-3 text-xs">{receiver.endpoint}</pre>
          {#if !receiver.enabled}
            <p class="mt-1.5 text-xs text-muted">
              Endepunktet svarer 404 indtil du slår det til — en instans der ikke er collector,
              skal ikke reklamere for noget den alligevel afviser.
            </p>
          {/if}
        </div>

        {#if receiver.enabled}
          <dl class="mt-4 grid grid-cols-3 gap-2 text-center">
            <div class="rounded-xl bg-raised py-3">
              <dt class="text-xs text-muted">installationer</dt>
              <dd class="text-lg font-medium">{receiver.total}</dd>
            </div>
            <div class="rounded-xl bg-raised py-3">
              <dt class="text-xs text-muted">aktive 7 dage</dt>
              <dd class="text-lg font-medium">{receiver.active7d}</dd>
            </div>
            <div class="rounded-xl bg-raised py-3">
              <dt class="text-xs text-muted">aktive 30 dage</dt>
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
          {receiver.enabled ? 'Slå collector fra' : 'Slå collector til'}
        </button>
      </section>
    {/if}
  </div>
{/if}
