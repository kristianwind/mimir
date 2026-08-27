<script>
  /**
   * A donate link, and nothing else on the wire.
   *
   * Buy Me a Coffee's own generator hands you a <script> tag from their CDN
   * that also pulls a webfont from Google. That is two third-party requests on
   * every page load, for every user — in a self-hosted app whose whole promise
   * is that it does not do that, and which is often run on a box with no
   * outbound internet at all, where the widget renders nothing.
   *
   * The button is a link. This is that link, styled to match theirs, with the
   * cup drawn inline rather than fetched. No script, no CDN, no webfont, no
   * count of who clicked it — there is nothing to learn there worth an
   * analytics call.
   *
   * The cup is an SVG rather than ☕ because the emoji is a different drawing
   * on every platform and a hollow box on a Linux box with no emoji font.
   */

  // loud is the yellow button; quiet is a bordered link that inherits the
  // surrounding text colour, which is what a settings page wants.
  let { variant = 'loud', label = 'Buy me a coffee' } = $props()
</script>

<a
  class="bmc {variant}"
  href="https://buymeacoffee.com/kristianwind"
  target="_blank"
  rel="noopener noreferrer"
>
  <svg
    width="16"
    height="16"
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    stroke-width="1.8"
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
    focusable="false"
  >
    <path d="M4 9h13v6a4 4 0 0 1-4 4H8a4 4 0 0 1-4-4V9Z" fill="var(--bmc-cup, transparent)" />
    <path d="M17 10h1.5a2.5 2.5 0 0 1 0 5H17" />
    <path d="M7.5 5.6c0-.9.8-1.1.8-2M11 5.6c0-.9.8-1.1.8-2M14.5 5.6c0-.9.8-1.1.8-2" />
  </svg>
  {label}</a
>

<style>
  .bmc {
    display: inline-flex;
    align-items: center;
    gap: 0.5rem;
    padding: 0.5rem 0.9rem;
    border-radius: 0.5rem;
    text-decoration: none;
    font-weight: 600;
    font-size: 0.95rem;
  }
  /* The yellow is fixed on purpose: it is the one recognisable thing about
     the brand, and black on it reads in either theme. */
  .loud {
    background: #ffdd00;
    color: #000;
    border: 1px solid rgb(0 0 0 / 0.12);
    --bmc-cup: #fff;
  }
  .quiet {
    border: 1px solid currentColor;
    color: inherit;
    opacity: 0.8;
    font-weight: 500;
    font-size: 0.9rem;
    padding: 0.4rem 0.75rem;
    gap: 0.4rem;
  }
  .quiet:hover {
    opacity: 1;
  }
  .bmc:focus-visible {
    outline: 2px solid currentColor;
    outline-offset: 2px;
  }
</style>
