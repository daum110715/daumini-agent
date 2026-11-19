<script lang="ts">
  import { pop, popOut } from '../motion';

  export interface PickerOption {
    value: string;
    label: string;
    sub?: string;
    badge?: string;
  }

  let {
    value,
    options,
    placeholder = '请选择',
    ariaLabel = '',
    align = 'start',
    compact = false,
    disabled = false,
    onChange
  }: {
    value: string;
    options: PickerOption[];
    placeholder?: string;
    ariaLabel?: string;
    align?: 'start' | 'end';
    compact?: boolean;
    disabled?: boolean;
    onChange: (value: string) => void;
  } = $props();

  let open = $state(false);
  let activeIndex = $state(0);
  let rootEl = $state<HTMLDivElement | null>(null);
  const listId = `pk-${Math.random().toString(36).slice(2, 8)}`;

  const selected = $derived(options.find((o) => o.value === value));

  function toggle() {
    if (disabled) return;
    open = !open;
    if (open) {
      const idx = options.findIndex((o) => o.value === value);
      activeIndex = idx >= 0 ? idx : 0;
    }
  }

  function pick(val: string) {
    onChange(val);
    open = false;
  }

  function handleWindowClick(e: MouseEvent) {
    if (!open || !rootEl) return;
    if (!rootEl.contains(e.target as Node)) {
      open = false;
    }
  }

  function handleKeydown(e: KeyboardEvent) {
    if (!open) {
      if (e.key === 'ArrowDown' || e.key === 'Enter' || e.key === ' ') {
        e.preventDefault();
        toggle();
      }
      return;
    }
    if (e.key === 'Escape') {
      e.preventDefault();
      open = false;
    } else if (e.key === 'ArrowDown') {
      e.preventDefault();
      activeIndex = (activeIndex + 1) % Math.max(options.length, 1);
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      activeIndex = (activeIndex - 1 + options.length) % Math.max(options.length, 1);
    } else if (e.key === 'Enter') {
      e.preventDefault();
      const opt = options[activeIndex];
      if (opt) pick(opt.value);
    }
  }
</script>

<svelte:window onclick={handleWindowClick} />

<div class="picker" class:compact bind:this={rootEl}>
  <button
    type="button"
    class="picker-trigger"
    class:open
    role="combobox"
    aria-controls={listId}
    aria-expanded={open}
    aria-haspopup="listbox"
    aria-label={ariaLabel || placeholder}
    {disabled}
    onclick={toggle}
    onkeydown={handleKeydown}
  >
    <span class="picker-label">{selected?.label || placeholder}</span>
    {#if selected?.badge}
      <span class="picker-badge">{selected.badge}</span>
    {/if}
    <svg class="picker-chev" class:rot={open} viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
      <path d="m6 9 6 6 6-6" />
    </svg>
  </button>

  {#if open}
    <div
      id={listId}
      class="picker-menu"
      class:align-end={align === 'end'}
      role="listbox"
      in:pop={{ y: -5, start: 0.96, blur: 4, duration: 200 }}
      out:popOut={{ y: -3, end: 0.97, blur: 2, duration: 120 }}
    >
      {#each options as opt, i (opt.value)}
        <button
          type="button"
          class="picker-item"
          class:selected={opt.value === value}
          class:focused={i === activeIndex}
          role="option"
          aria-selected={opt.value === value}
          onmouseenter={() => (activeIndex = i)}
          onclick={() => pick(opt.value)}
        >
          <span class="picker-check" aria-hidden="true">
            {#if opt.value === value}
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round">
                <path d="M20 6 9 17l-5-5" />
              </svg>
            {/if}
          </span>
          <span class="picker-item-body">
            <span class="picker-item-title">{opt.label}</span>
            {#if opt.sub}
              <span class="picker-item-sub">{opt.sub}</span>
            {/if}
          </span>
          {#if opt.badge}
            <span class="picker-item-badge">{opt.badge}</span>
          {/if}
        </button>
      {/each}
    </div>
  {/if}
</div>

<style>
  .picker {
    position: relative;
    display: inline-flex;
    min-width: 0;
  }

  .picker-trigger {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    height: 30px;
    padding: 0 8px 0 10px;
    border-radius: 7px;
    border: 1px solid var(--line);
    background: var(--field);
    color: var(--fg);
    font-family: var(--font-sans);
    font-size: var(--text-xs);
    cursor: pointer;
    white-space: nowrap;
    min-width: 0;
    transition: border-color 150ms var(--ease-out), background-color 150ms var(--ease-out), transform 180ms var(--ease-spring), box-shadow 150ms var(--ease-out);
  }

  .picker-trigger:hover:not(:disabled),
  .picker-trigger.open {
    border-color: var(--line-strong);
  }

  .picker-trigger:active:not(:disabled) {
    transform: scale(0.97);
  }

  .picker-trigger:focus-visible {
    outline: none;
    border-color: var(--accent);
    box-shadow: 0 0 0 3px color-mix(in srgb, var(--accent) 18%, transparent);
  }

  .picker-trigger:disabled {
    opacity: 0.55;
    cursor: not-allowed;
  }

  .picker-label {
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .picker-badge {
    font-size: var(--text-xs);
    padding: 0 5px;
    line-height: 18px;
    border-radius: 999px;
    background: var(--mark-soft);
    color: var(--fg);
  }

  .picker-chev {
    width: 14px;
    height: 14px;
    flex-shrink: 0;
    color: var(--fg-3);
    transition: transform 240ms var(--ease-spring), color 150ms;
  }

  .picker-chev.rot {
    transform: rotate(180deg);
    color: var(--fg);
  }

  .picker-menu {
    position: absolute;
    top: calc(100% + 5px);
    left: 0;
    min-width: max(100%, 196px);
    max-width: min(320px, calc(100vw - 24px));
    max-height: 268px;
    overflow-y: auto;
    z-index: 80;
    padding: 4px;
    border-radius: 11px;
    border: 1px solid var(--line);
    background: var(--panel);
    box-shadow: var(--shadow-pop);
    transform-origin: top left;
  }

  .picker-menu.align-end {
    left: auto;
    right: 0;
    transform-origin: top right;
  }

  .picker-item {
    width: 100%;
    display: flex;
    align-items: center;
    gap: 7px;
    padding: 5px 8px;
    border: 0;
    border-radius: 8px;
    background: transparent;
    color: var(--fg);
    font-family: var(--font-sans);
    font-size: var(--text-sm);
    text-align: left;
    cursor: pointer;
    transition: background-color 120ms var(--ease-out), transform 140ms var(--ease-out);
  }

  .picker-item.focused,
  .picker-item:hover {
    background: var(--raised);
    transform: translateX(2px);
  }

  .picker-item.selected {
    background: var(--mark-soft);
  }

  .picker-check {
    width: 14px;
    height: 14px;
    display: grid;
    place-items: center;
    flex-shrink: 0;
    color: var(--accent);
  }

  .picker-check svg {
    width: 13px;
    height: 13px;
  }

  .picker-item-body {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 1px;
  }

  .picker-item-title {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .picker-item-sub {
    font-size: var(--text-xs);
    color: var(--fg-3);
    font-family: var(--font-mono);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .picker-item-badge {
    font-size: var(--text-xs);
    color: var(--fg-2);
    padding: 0 5px;
    border-radius: 5px;
    background: var(--sunken);
    flex-shrink: 0;
  }
</style>
