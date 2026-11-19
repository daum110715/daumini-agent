<script lang="ts">
  let { live = false, suffix = '-agent' }: { live?: boolean; suffix?: string } = $props();
</script>

<span class="wordmark {live ? 'live' : ''}" aria-label="daumini{suffix}" role="img">
  <span aria-hidden="true">daum<span class="i">ı<span class="lamp"></span></span>n<span class="i">ı<span class="lamp"></span></span>{suffix}</span>
</span>

<style>
  .wordmark {
    display: inline-block;
    font-family: var(--font-display);
    font-weight: 400;
    letter-spacing: 0;
    line-height: 1;
    white-space: nowrap;
  }
  .i {
    position: relative;
    display: inline-block;
  }
  .lamp {
    position: absolute;
    top: 0.12em;
    left: 52%;
    width: 0.16em;
    height: 0.15em;
    border-radius: 60% 40% 55% 45%;
    translate: -50% 0;
    background: var(--mark);
  }
  .live .i:last-child .lamp {
    animation: wordmark-pulse 4s ease-in-out infinite;
  }
  @keyframes wordmark-pulse {
    0%, 86%, 100% { opacity: 1; }
    92% { opacity: 0.25; }
  }
  @media (prefers-reduced-motion: reduce) {
    .live .i:last-child .lamp { animation: none; }
  }
</style>
