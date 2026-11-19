<script lang="ts">
  import type { Snippet } from 'svelte';

  let {
    open = $bindable(false),
    title,
    subtitle = '',
    badge = '',
    badgeTone = 'neutral',
    streaming = false,
    variant = 'tool',
    children
  }: {
    open?: boolean;
    title: string;
    subtitle?: string;
    badge?: string;
    badgeTone?: 'neutral' | 'ok' | 'bad' | 'warn' | 'accent';
    streaming?: boolean;
    variant?: 'think' | 'tool';
    children: Snippet;
  } = $props();
</script>

<div class="col" class:open class:think={variant === 'think'} class:tool={variant === 'tool'}>
  <button type="button" class="col-head" aria-expanded={open} onclick={() => (open = !open)}>
    <svg class="col-chev" class:rot={open} viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
      <path d="m9 18 6-6-6-6" />
    </svg>
    {#if streaming}
      <span class="col-pulse" aria-hidden="true"></span>
    {/if}
    <span class="col-title">{title}</span>
    {#if subtitle}
      <span class="col-sub">{subtitle}</span>
    {/if}
    {#if badge}
      <span class="col-badge tone-{badgeTone}">{badge}</span>
    {/if}
  </button>

  <div class="col-panel" class:open>
    <div class="col-inner">
      {@render children()}
    </div>
  </div>
</div>

<style>
  .col {
    border: 1px solid var(--line);
    border-radius: 10px;
    background: var(--sunken);
    overflow: hidden;
    transition: border-color 180ms var(--ease-out), background-color 180ms var(--ease-out);
  }

  .col.tool {
    background: var(--card);
  }

  .col:hover {
    border-color: color-mix(in srgb, var(--line-strong) 45%, var(--line));
  }

  .col-head {
    width: 100%;
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 9px 12px;
    border: 0;
    background: transparent;
    color: var(--fg);
    font-family: var(--font-sans);
    font-size: var(--text-sm);
    text-align: left;
    cursor: pointer;
    min-width: 0;
    transition: background-color 140ms var(--ease-out);
  }

  .col-head:hover {
    background: color-mix(in srgb, var(--raised) 65%, transparent);
  }

  .col-chev {
    width: 15px;
    height: 15px;
    flex-shrink: 0;
    color: var(--fg-3);
    transition: transform 220ms var(--ease-spring), color 150ms;
  }

  .col-chev.rot {
    transform: rotate(90deg);
    color: var(--fg);
  }

  .col-pulse {
    width: 7px;
    height: 7px;
    border-radius: 999px;
    background: var(--accent);
    flex-shrink: 0;
    animation: col-pulse 1.2s ease-in-out infinite;
  }

  .col.think .col-title {
    color: var(--fg-2);
  }

  .col.tool .col-title {
    font-family: var(--font-mono);
    color: var(--accent);
    flex-shrink: 0;
  }

  .col-sub {
    flex: 1;
    min-width: 0;
    font-family: var(--font-mono);
    font-size: var(--text-xs);
    color: var(--fg-3);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .col-badge {
    margin-left: auto;
    font-size: var(--text-xs);
    padding: 1px 8px;
    border-radius: 999px;
    flex-shrink: 0;
    white-space: nowrap;
  }

  .tone-neutral {
    background: var(--raised);
    color: var(--fg-2);
  }

  .tone-ok {
    background: var(--ok-soft);
    color: var(--ok);
  }

  .tone-bad {
    background: var(--bad-soft);
    color: var(--bad);
  }

  .tone-warn {
    background: var(--warn-soft);
    color: var(--warn);
  }

  .tone-accent {
    background: var(--accent-soft);
    color: var(--accent);
  }

  .col-panel {
    display: grid;
    grid-template-rows: 0fr;
    transition: grid-template-rows 240ms var(--ease-out);
  }

  .col-panel.open {
    grid-template-rows: 1fr;
  }

  .col-inner {
    overflow: hidden;
    min-height: 0;
  }

  @keyframes col-pulse {
    0%, 100% { opacity: 1; transform: scale(1); }
    50% { opacity: 0.35; transform: scale(0.75); }
  }
</style>
