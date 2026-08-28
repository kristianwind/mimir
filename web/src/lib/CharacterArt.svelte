<script>
  /**
   * The game's own art behind a card's text.
   *
   * An <img> rather than a CSS background, so a missing picture is an event
   * the card can react to — a background-image fails silently and leaves a
   * hole where the scrim still darkens nothing. On an error the whole
   * treatment removes itself, scrim included, and the card reads exactly as
   * it did before there were pictures.
   *
   * The scrim is built from the surface colour, so it follows the theme
   * rather than assuming a light or a dark page. Two shapes, because the
   * cards have two shapes: `left` for a card whose text is all on one side,
   * `both` for a row that carries a name on the left and a number on the
   * right and would otherwise print the number over the picture. `both`
   * keeps a floor of opacity across the whole middle as well, because these
   * rows carry a third line of small muted text that reaches further right
   * than the heading does — and a caveat nobody can read is a caveat nobody
   * heeded. It also reaches full opacity well before the right edge, because
   * a row's controls live there and a button that is a solid pill on one card
   * and a bare word on the next is not the same button.
   */

  let { character, scrim = 'left' } = $props()

  let missing = $state(false)

  const SCRIMS = {
    left: 'bg-gradient-to-r from-surface from-30% via-surface/80 to-transparent',
    both: 'bg-gradient-to-r from-surface from-35% via-surface/55 via-60% to-surface to-80%',
  }
</script>

{#if !missing}
  <img
    src="/api/art/{character}"
    alt=""
    aria-hidden="true"
    loading="lazy"
    onerror={() => (missing = true)}
    class="pointer-events-none absolute inset-y-0 right-0 h-full w-3/4 object-cover object-center"
  />
  <div class="pointer-events-none absolute inset-0 {SCRIMS[scrim]}"></div>
{/if}
