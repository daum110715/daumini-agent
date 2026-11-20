<script lang="ts">
  import { pop, popOut } from '../motion';
  import { THEME_PACKS } from '../themes';
  import type { ColorTheme, UIStyle } from '../types';

  let {
    uiStyle,
    theme,
    onSelectStyle,
    onSelectTheme
  }: {
    uiStyle: UIStyle;
    theme: ColorTheme;
    onSelectStyle: (style: UIStyle) => void;
    onSelectTheme: (theme: ColorTheme) => void;
  } = $props();

  let open = $state(false);
  let rootEl = $state<HTMLDivElement | null>(null);
  let triggerEl = $state<HTMLButtonElement | null>(null);
  let menuTop = $state(52);
  let menuLeft = $state(12);

  function updatePosition() {
    if (!triggerEl || typeof window === 'undefined') return;
    const rect = triggerEl.getBoundingClientRect();
    const menuW = 280;
    const pad = 10;
    let left = rect.left;
    if (left + menuW > window.innerWidth - pad) {
      left = Math.max(pad, window.innerWidth - menuW - pad);
    }
    if (left < pad) left = pad;
    menuLeft = Math.round(left);
    menuTop = Math.round(rect.bottom + 8);
  }

  function toggleOpen() {
    if (!open) updatePosition();
    open = !open;
  }

  function handleWindowClick(e: MouseEvent) {
    if (!open || !rootEl) return;
    if (!rootEl.contains(e.target as Node)) open = false;
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape' && open) {
      e.stopPropagation();
      open = false;
    }
  }
</script>

<svelte:window onclick={handleWindowClick} onkeydown={handleKeydown} onresize={() => open && updatePosition()} />

<div class="tp-wrap" bind:this={rootEl}>
  <button
    bind:this={triggerEl}
    type="button"
    class="wobble btn btn-icon btn-ghost tp-trigger"
    class:active={open}
    title="外观与主题"
    aria-label="外观与主题"
    aria-expanded={open}
    onclick={toggleOpen}
  >
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
      <circle cx="13.5" cy="6.5" r=".5" fill="currentColor"/>
      <circle cx="17.5" cy="10.5" r=".5" fill="currentColor"/>
      <circle cx="8.5" cy="7.5" r=".5" fill="currentColor"/>
      <circle cx="6.5" cy="12.5" r=".5" fill="currentColor"/>
      <path d="M12 2C6.5 2 2 6.5 2 12s4.5 10 10 10c.926 0 1.648-.746 1.648-1.688 0-.437-.18-.835-.437-1.125-.29-.289-.438-.652-.438-1.125a1.64 1.64 0 0 1 1.668-1.668h1.996c3.051 0 5.555-2.503 5.555-5.554C21.965 6.012 17.461 2 12 2z"/>
    </svg>
  </button>

  {#if open}
    <div
      class="tp-menu border-line-strong"
      role="dialog"
      aria-label="外观主题设置"
      style:top="{menuTop}px"
      style:left="{menuLeft}px"
      in:pop={{ y: -6, start: 0.96, blur: 4, duration: 200 }}
      out:popOut={{ y: -4, duration: 120 }}
    >
      <div class="tp-head">
        <span class="tp-title">主题风格</span>
        <div class="tp-mode-row" role="group" aria-label="明暗模式">
          <button
            type="button"
            class="tp-mode-btn"
            class:selected={theme === 'light'}
            onclick={() => onSelectTheme('light')}
          >
            浅色
          </button>
          <button
            type="button"
            class="tp-mode-btn"
            class:selected={theme === 'dark'}
            onclick={() => onSelectTheme('dark')}
          >
            深色
          </button>
        </div>
      </div>

      <div class="tp-packs">
        {#each THEME_PACKS as pack (pack.id)}
          {@const active = uiStyle === pack.id}
          {@const swatches = theme === 'dark' ? pack.preview.dark : pack.preview.light}
          <button
            type="button"
            class="tp-pack"
            class:selected={active}
            onclick={() => onSelectStyle(pack.id)}
          >
            <div class="tp-pack-top">
              <div class="tp-swatches" aria-hidden="true">
                {#each swatches as color}
                  <span class="tp-swatch" style:background={color}></span>
                {/each}
              </div>
              <span class="tp-pack-name">{pack.name}</span>
              <span class="tp-motion-tag">{pack.motionTag}</span>
            </div>
            <div class="tp-pack-sub">{pack.tagline}</div>
          </button>
        {/each}
      </div>
    </div>
  {/if}
</div>

<style>
  .tp-wrap {
    position: relative;
  }

  .tp-trigger.active {
    background: var(--raised);
    color: var(--fg);
  }

  .tp-menu {
    position: fixed;
    width: 280px;
    padding: 10px;
    border-radius: 12px;
    background: var(--panel);
    box-shadow: var(--shadow-pop);
    z-index: 220;
    display: flex;
    flex-direction: column;
    gap: 10px;
  }

  .tp-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
  }

  .tp-title {
    font-size: var(--text-xs);
    color: var(--fg-2);
    font-weight: 500;
  }

  .tp-mode-row {
    display: inline-flex;
    padding: 2px;
    border-radius: 7px;
    background: var(--sunken);
    border: 1px solid var(--line);
    gap: 2px;
  }

  .tp-mode-btn {
    padding: 3px 9px;
    border-radius: 5px;
    font-size: var(--text-xs);
    color: var(--fg-3);
    transition: background-color 150ms var(--ease-out), color 150ms var(--ease-out);
  }
  .tp-mode-btn:hover {
    color: var(--fg);
  }
  .tp-mode-btn.selected {
    background: var(--panel);
    color: var(--fg);
    box-shadow: 0 1px 2px rgb(0 0 0 / 0.08);
  }

  .tp-packs {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .tp-pack {
    width: 100%;
    text-align: left;
    padding: 9px 10px;
    border-radius: 9px;
    border: 1px solid var(--line);
    background: var(--sunken);
    display: flex;
    flex-direction: column;
    gap: 4px;
    transition:
      border-color 160ms var(--ease-out),
      background-color 160ms var(--ease-out),
      transform 160ms var(--ease-out);
  }
  .tp-pack:hover {
    background: var(--raised);
    border-color: var(--line-strong);
  }
  .tp-pack.selected {
    background: var(--accent-soft);
    border-color: color-mix(in srgb, var(--accent) 52%, var(--line));
  }

  .tp-pack-top {
    display: flex;
    align-items: center;
    gap: 7px;
  }

  .tp-swatches {
    display: inline-flex;
    align-items: center;
    gap: 3px;
    flex-shrink: 0;
  }

  .tp-swatch {
    width: 9px;
    height: 9px;
    border-radius: 99px;
    border: 1px solid rgb(128 128 128 / 0.25);
  }

  .tp-pack-name {
    font-size: var(--text-sm);
    color: var(--fg);
    font-weight: 500;
  }

  .tp-motion-tag {
    margin-left: auto;
    font-size: 11px;
    padding: 1px 6px;
    border-radius: 99px;
    background: var(--panel);
    color: var(--fg-2);
    border: 1px solid var(--line);
    white-space: nowrap;
  }

  .tp-pack-sub {
    font-size: var(--text-xs);
    color: var(--fg-3);
    line-height: 1.4;
  }

  /* Atelier borderless tonal popover */
  :global([data-style="atelier"]) .tp-menu {
    border: 0;
    border-radius: 16px;
    padding: 12px;
  }
  :global([data-style="atelier"]) .tp-mode-row {
    border: 0;
    border-radius: 9px;
    padding: 3px;
  }
  :global([data-style="atelier"]) .tp-mode-btn.selected {
    color: var(--accent);
    box-shadow: var(--shadow-subtle);
  }
  :global([data-style="atelier"]) .tp-pack {
    border: 0;
    border-radius: 11px;
    padding: 10px 11px;
  }
  :global([data-style="atelier"]) .tp-pack:hover {
    transform: translateX(2px);
  }
  :global([data-style="atelier"]) .tp-pack.selected {
    background: var(--accent-soft);
    box-shadow: inset 3px 0 0 var(--accent);
  }
  :global([data-style="atelier"]) .tp-motion-tag {
    border: 0;
    background: var(--panel);
    color: var(--accent);
  }
</style>
