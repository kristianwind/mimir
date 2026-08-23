<script>
  /**
   * Asking follow-up questions.
   *
   * The opinion cards answer "how do I get better" for one page. This is the
   * question after that, and it is the one place the model chooses what to
   * look at — it can call the engine, and the answer says which calls it made.
   * It still cannot produce a number, only fetch one.
   */
  import { api } from './api.js'
  import { t } from './lang.svelte.js'
  import { kvasirStatus } from './Kvasir.svelte'

  let { account, surface = 'plan', subject = '' } = $props()

  // Written as labels rather than bare strings so the translation coverage
  // test sees them: a source string it cannot find renders English inside an
  // otherwise Danish page, which reads as working software.
  const SUGGESTIONS = [
    { label: 'What should I spend tomorrow’s resin on?' },
    { label: 'Which of my characters is furthest from their potential?' },
    { label: 'What am I farming that is not worth the resin?' },
  ]

  let enabled = $state(false)
  let messages = $state([])
  let question = $state('')
  let pending = $state(false)
  let error = $state(null)

  kvasirStatus().then((s) => (enabled = !!s?.enabled))

  async function send(text) {
    const content = (text ?? question).trim()
    if (!content || pending) return
    question = ''
    error = null
    messages = [...messages, { role: 'user', content }]
    pending = true
    try {
      const answer = await api.kvasirChat(account.id, {
        surface,
        subject,
        messages: messages.map((m) => ({ role: m.role, content: m.content })),
      })
      messages = [
        ...messages,
        {
          role: 'assistant',
          content: answer.reply,
          used: answer.used ?? [],
          unsourced: answer.unsourced ?? [],
        },
      ]
    } catch (err) {
      error = err
      // The question stays in the box on a failure: retyping it is the
      // worst possible response to a model that timed out.
      question = content
      messages = messages.slice(0, -1)
    } finally {
      pending = false
    }
  }

  function onkeydown(event) {
    if (event.key === 'Enter' && !event.shiftKey) {
      event.preventDefault()
      send()
    }
  }
</script>

{#if !enabled}
  <div class="card p-8 text-center">
    <p class="text-muted">{t('Kvasir is not switched on.')}</p>
    <p class="mx-auto mt-2 max-w-prose text-sm text-muted">
      {t(
        'Point MIMIR_LLM_BASE_URL at an OpenAI-compatible endpoint — LM Studio, Ollama, vLLM — and Kvasir appears on every page. Nothing else in Mimir depends on it: no number here comes from a language model.',
      )}
    </p>
  </div>
{:else}
  <div class="space-y-4">
    {#if messages.length === 0}
      <div class="card p-6">
        <p class="text-sm leading-relaxed">
          {t(
            'Kvasir reads what the engine calculated for this account and answers questions about it. It looks things up rather than remembering them, and every figure it uses has to come from a calculation — so it will tell you when it cannot answer.',
          )}
        </p>
        <div class="mt-4 flex flex-wrap gap-2">
          {#each SUGGESTIONS as suggestion (suggestion.label)}
            <button
              type="button"
              class="chip hover:border-accent hover:text-ink"
              onclick={() => send(t(suggestion.label))}
            >
              {t(suggestion.label)}
            </button>
          {/each}
        </div>
      </div>
    {/if}

    {#each messages as message, i (i)}
      <div class="card p-4 {message.role === 'user' ? 'border-accent/40' : ''}">
        <p class="mb-1.5 text-[11px] uppercase tracking-wide text-muted">
          {message.role === 'user' ? t('You') : 'Kvasir'}
        </p>
        <p class="whitespace-pre-wrap text-sm leading-relaxed">{message.content}</p>

        {#if message.used?.length}
          <p class="mt-2 text-[11px] text-muted">
            {t('Looked up: {tools}', { tools: [...new Set(message.used)].join(', ') })}
          </p>
        {/if}

        <!--
          Flagged rather than removed: a bullet can be cut and leave the rest
          true, but a sentence taken out of a paragraph leaves an argument
          missing its middle. So the answer stands and the figures are named.
        -->
        {#if message.unsourced?.length}
          <p class="mt-2 rounded-lg border border-warn/40 bg-warn/10 px-2.5 py-1.5 text-xs text-warn">
            {t('Do not trust these figures — no calculation produced them: {numbers}', {
              numbers: message.unsourced.join(', '),
            })}
          </p>
        {/if}
      </div>
    {/each}

    {#if pending}
      <p class="text-sm text-muted">{t('Kvasir is reading the numbers…')}</p>
    {/if}

    {#if error}
      <div class="card border-bad/40 p-4">
        <p class="text-sm text-bad">{error.message}</p>
        {#if error.hint}<p class="mt-1 text-xs text-muted">{error.hint}</p>{/if}
      </div>
    {/if}

    <div class="flex items-end gap-2">
      <textarea
        class="field min-h-11 flex-1 resize-y"
        rows="2"
        bind:value={question}
        {onkeydown}
        placeholder={t('Ask about your account…')}
      ></textarea>
      <button class="btn-primary" disabled={pending || !question.trim()} onclick={() => send()}>
        {t('Ask')}
      </button>
    </div>
  </div>
{/if}
