<script lang="ts" generics="T extends string">
  import { tick } from 'svelte';

  let {
    options,
    value = $bindable(),
    label,
    size = 'md',
    full = false,
    onchange
  }: {
    options: { value: T; label: string }[];
    value: T;
    label: string;
    size?: 'sm' | 'md';
    full?: boolean;
    onchange?: (value: T) => void;
  } = $props();

  let buttons = $state<HTMLButtonElement[]>([]);
  let pillEl: HTMLSpanElement | undefined;
  let pill = $state({ x: 0, w: 0 });
  let glide = $state(false);
  let placed = false;

  $effect(() => {
    const idx = options.findIndex((o) => o.value === value);
    const el = buttons[idx];
    if (!el) return;
    const update = () => {
      const w = el.offsetWidth;
      pill = { x: el.offsetLeft, w };
      if (placed || !w) return;
      placed = true;
      void tick().then(() => {
        void pillEl?.offsetWidth;
        glide = true;
      });
    };
    update();
    const observer = new ResizeObserver(update);
    observer.observe(el);
    return () => observer.disconnect();
  });
</script>

<div role="group" aria-label={label} class="seg-group {size === 'sm' ? 'seg-sm' : ''} {full ? 'seg-full' : ''}">
  <span
    bind:this={pillEl}
    aria-hidden="true"
    class="seg-pill border-line {glide ? 'glide' : ''}"
    style:translate="{pill.x}px 0"
    style:width="{pill.w}px"
    style:opacity={pill.w ? 1 : 0}
  ></span>
  {#each options as opt, i (opt.value)}
    <button
      bind:this={buttons[i]}
      type="button"
      aria-pressed={value === opt.value}
      class="seg-btn {value === opt.value ? 'active' : ''}"
      onclick={() => {
        value = opt.value;
        onchange?.(opt.value);
      }}
    >
      {opt.label}
    </button>
  {/each}
</div>

<style>
  .seg-group {
    position: relative;
    display: inline-flex;
    align-items: center;
    max-width: 100%;
    padding: 3px;
    border-radius: 8px;
    background: var(--sunken);
    border: 1px solid transparent;
    border-image-source: var(--sketch-line);
    border-image-slice: 36;
    border-image-width: 12;
    border-image-outset: 1px;
    border-image-repeat: stretch;
  }
  .seg-pill {
    position: absolute;
    top: 3px;
    bottom: 3px;
    left: 0;
    border-radius: 6px;
    background: var(--mark-soft);
    border: 1px solid transparent;
  }
  .seg-pill.glide {
    transition: translate 0.5s var(--ease-spring), width 0.5s var(--ease-spring);
  }
  .seg-btn {
    position: relative;
    z-index: 1;
    height: 30px;
    padding: 0 11px;
    border-radius: 6px;
    white-space: nowrap;
    color: var(--fg-3);
    font-size: 13px;
    transition: color 0.15s;
  }
  .seg-full {
    display: flex;
    width: 100%;
  }
  .seg-full .seg-btn {
    flex: 1;
    text-align: center;
  }
  .seg-sm .seg-btn {
    height: 26px;
    padding: 0 9px;
    font-size: 12px;
  }
  .seg-btn:hover,
  .seg-btn.active {
    color: var(--fg);
  }
  @media (prefers-reduced-motion: reduce) {
    .seg-pill.glide {
      transition: none;
    }
  }
</style>
