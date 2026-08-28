<script>
  /**
   * The manual, in a panel beside the page rather than instead of it.
   *
   * A help page you navigate to is a help page you read with the thing you
   * were confused about no longer on screen. This opens at the side, scrolled
   * to the section for whatever view you are on, and leaves the page working
   * underneath — there is no backdrop and nothing is disabled, so you can
   * read a sentence and click the thing it describes without closing it.
   *
   * It is also the whole document, not just the current section, so it can be
   * read end to end by somebody who has just installed this and does not yet
   * know what any of it is.
   */
  import { MANUAL } from './manual.js'

  let { open = false, section = '', onclose } = $props()

  let panel = $state(null)

  // Scroll to the section for the current page each time it opens, and when
  // the page changes underneath an open panel.
  $effect(() => {
    if (!open || !panel) return
    const target = panel.querySelector(`[data-section="${section}"]`)
    if (target) target.scrollIntoView({ block: 'start' })
    else panel.scrollTop = 0
  })

  function onkeydown(event) {
    if (event.key === 'Escape') onclose?.()
  }
</script>

<svelte:window {onkeydown} />

{#if open}
  <aside
    class="fixed inset-y-0 right-0 z-40 flex w-full max-w-md flex-col border-l border-line bg-surface shadow-2xl"
    aria-label="Manual"
  >
    <header class="flex items-center justify-between gap-3 border-b border-line px-5 py-4">
      <div>
        <h2 class="font-semibold tracking-tight">Manual</h2>
        <p class="text-xs text-muted">Stays open while you work</p>
      </div>
      <button class="btn-ghost text-xs" onclick={() => onclose?.()}>Close</button>
    </header>

    <div bind:this={panel} class="min-h-0 flex-1 overflow-y-auto px-5 py-4">
      {#each MANUAL as part (part.id)}
        <section data-section={part.id} class="scroll-mt-4 pb-7">
          <h3
            class="text-sm font-medium {part.id === section ? 'text-accent' : ''}"
          >
            {part.title}
          </h3>

          {#each part.blocks as block}
            {#if typeof block === 'string'}
              <p class="mt-2 text-sm leading-relaxed text-muted">{block}</p>
            {:else if block.list}
              <ul class="mt-2 space-y-1.5 text-sm text-muted">
                {#each block.list as item}
                  <li>· {item}</li>
                {/each}
              </ul>
            {:else if block.steps}
              <ol class="mt-2 space-y-2.5">
                {#each block.steps as [name, detail], i}
                  <li class="flex gap-3">
                    <span
                      class="grid h-5 w-5 shrink-0 place-items-center rounded-md bg-accent/15 text-xs font-medium"
                    >
                      {i + 1}
                    </span>
                    <span class="text-sm">
                      <span class="font-medium">{name}</span>
                      <span class="text-muted"> — {detail}</span>
                    </span>
                  </li>
                {/each}
              </ol>
            {:else if block.terms}
              <dl class="mt-2 space-y-2.5">
                {#each block.terms as [term, meaning]}
                  <div>
                    <dt class="text-sm font-medium">{term}</dt>
                    <dd class="text-sm leading-relaxed text-muted">{meaning}</dd>
                  </div>
                {/each}
              </dl>
            {:else if block.note}
              <p class="mt-2 rounded-xl bg-raised px-3 py-2 text-xs leading-relaxed text-muted">
                {block.note}
              </p>
            {/if}
          {/each}
        </section>
      {/each}
    </div>
  </aside>
{/if}
