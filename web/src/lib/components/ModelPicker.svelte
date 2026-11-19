<script lang="ts">
  import { tick } from 'svelte';
  import type { ModelInfo } from '../types';

  let {
    models = [],
    fallbackIds = [],
    value,
    loading = false,
    disabled = false,
    onChange,
    onRefresh
  }: {
    models?: ModelInfo[];
    fallbackIds?: string[];
    value: string;
    loading?: boolean;
    disabled?: boolean;
    onChange: (id: string) => void;
    onRefresh?: () => void;
  } = $props();

  let open = $state(false);
  let query = $state('');
  let activeIndex = $state(0);
  let rootEl = $state<HTMLDivElement | null>(null);
  let inputEl = $state<HTMLInputElement | null>(null);

  const mergedModels = $derived.by((): ModelInfo[] => {
    const map = new Map<string, ModelInfo>();
    for (const m of models) {
      map.set(m.id, m);
    }
    for (const id of fallbackIds) {
      if (id && !map.has(id)) {
        map.set(id, { id, name: id });
      }
    }
    if (value && !map.has(value)) {
      map.set(value, { id: value, name: value });
    }
    return Array.from(map.values());
  });

  const selected = $derived(mergedModels.find((m) => m.id === value));

  const filtered = $derived.by(() => {
    const q = query.trim().toLowerCase();
    if (!q) return mergedModels;
    return mergedModels.filter((m) =>
      `${m.name || ''} ${m.id} ${m.author || ''} ${m.description || ''}`.toLowerCase().includes(q)
    );
  });

  const canUseCustom = $derived(
    query.trim() !== '' && !mergedModels.some((m) => m.id.toLowerCase() === query.trim().toLowerCase())
  );

  function fmtPrice(nusdPerMillion?: number): string {
    if (!nusdPerMillion || nusdPerMillion <= 0) return '';
    const usd = nusdPerMillion / 1e9;
    if (usd >= 1) return `$${usd.toFixed(2)}`;
    if (usd >= 0.01) return `$${usd.toFixed(2)}`;
    return `$${usd.toFixed(3)}`;
  }

  function fmtCtx(n?: number): string {
    if (!n || n <= 0) return '';
    if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(n % 1_000_000 === 0 ? 0 : 1)}M`;
    if (n >= 1000) return `${Math.round(n / 1000)}K`;
    return String(n);
  }

  async function toggle() {
    if (disabled) return;
    open = !open;
    if (open) {
      query = '';
      const idx = mergedModels.findIndex((m) => m.id === value);
      activeIndex = idx >= 0 ? idx : 0;
      await tick();
      inputEl?.focus();
    }
  }

  function pick(id: string) {
    onChange(id);
    open = false;
    query = '';
  }

  function handleWindowClick(e: MouseEvent) {
    if (!open || !rootEl) return;
    if (!rootEl.contains(e.target as Node)) {
      open = false;
    }
  }

  function handleKeydown(e: KeyboardEvent) {
    const total = filtered.length + (canUseCustom ? 1 : 0);
    if (e.key === 'Escape') {
      e.preventDefault();
      open = false;
    } else if (e.key === 'ArrowDown' && total > 0) {
      e.preventDefault();
      activeIndex = (activeIndex + 1) % total;
    } else if (e.key === 'ArrowUp' && total > 0) {
      e.preventDefault();
      activeIndex = (activeIndex - 1 + total) % total;
    } else if (e.key === 'Enter') {
      e.preventDefault();
      if (activeIndex < filtered.length) {
        pick(filtered[activeIndex].id);
      } else if (canUseCustom) {
        pick(query.trim());
      }
    }
  }
</script>

<svelte:window onclick={handleWindowClick} />

<div class="mp" bind:this={rootEl}>
  <button
    type="button"
    class="mp-trigger"
    class:open
    role="combobox"
    aria-expanded={open}
    aria-haspopup="listbox"
    aria-label="选择模型"
    {disabled}
    onclick={toggle}
  >
    <span class="mp-title-wrap">
      <span class="mp-name">{selected?.name || value || '选择模型'}</span>
      {#if selected?.author}
        <span class="mp-author">· {selected.author}</span>
      {/if}
    </span>
    {#if selected?.supports_thinking}
      <span class="mp-chip">思考</span>
    {/if}
    {#if selected && selected.name && selected.name !== selected.id}
      <code class="mp-id-pill">{selected.id}</code>
    {/if}
    <svg class="mp-chev" class:rot={open} viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
      <path d="m7 15 5 5 5-5" />
      <path d="m7 9 5-5 5 5" />
    </svg>
  </button>

  {#if open}
    <div class="mp-popover" role="listbox">
      <div class="mp-search-bar">
        <svg class="mp-search-ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
          <circle cx="11" cy="11" r="8" />
          <path d="m21 21-4.3-4.3" />
        </svg>
        <input
          bind:this={inputEl}
          class="mp-search-input"
          bind:value={query}
          placeholder="搜索模型名称或输入自定义 ID"
          oninput={() => (activeIndex = 0)}
          onkeydown={handleKeydown}
        />
        {#if onRefresh}
          <button
            type="button"
            class="mp-refresh"
            class:spin={loading}
            title="刷新模型列表"
            onclick={(e) => {
              e.stopPropagation();
              onRefresh?.();
            }}
          >
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round">
              <path d="M21 12a9 9 0 1 1-2.64-6.36" />
              <path d="M21 3v6h-6" />
            </svg>
          </button>
        {/if}
      </div>

      <div class="mp-list">
        {#if filtered.length === 0 && !canUseCustom}
          <div class="mp-empty">没有匹配的模型</div>
        {:else}
          {#each filtered as m, i (m.id)}
            <button
              type="button"
              class="mp-row"
              class:selected={m.id === value}
              class:focused={i === activeIndex}
              role="option"
              aria-selected={m.id === value}
              onmouseenter={() => (activeIndex = i)}
              onclick={() => pick(m.id)}
            >
              <span class="mp-check" aria-hidden="true">
                {#if m.id === value}
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round">
                    <path d="M20 6 9 17l-5-5" />
                  </svg>
                {/if}
              </span>
              <span class="mp-row-main">
                <span class="mp-row-head">
                  <span class="mp-row-name">{m.name || m.id}</span>
                  {#if m.author}<span class="mp-row-author">· {m.author}</span>{/if}
                  {#if m.supports_thinking}<span class="mp-tag tag-think">思考</span>{/if}
                  {#if m.supports_images}<span class="mp-tag">图片</span>{/if}
                  {#if fmtCtx(m.context_window)}<span class="mp-tag">{fmtCtx(m.context_window)}</span>{/if}
                </span>
                <span class="mp-row-id">{m.id}</span>
                {#if m.description}
                  <span class="mp-row-desc">{m.description}</span>
                {/if}
              </span>
              {#if m.cache_read_nusd_per_million || m.input_nusd_per_million}
                <span class="mp-price">
                  {#if m.cache_read_nusd_per_million}
                    <span class="mp-price-cache">缓存 {fmtPrice(m.cache_read_nusd_per_million)}</span>
                  {/if}
                  <span class="mp-price-io">
                    {fmtPrice(m.input_nusd_per_million)} / {fmtPrice(m.output_nusd_per_million)}
                  </span>
                </span>
              {/if}
            </button>
          {/each}

          {#if canUseCustom}
            <button
              type="button"
              class="mp-row"
              class:focused={activeIndex === filtered.length}
              onmouseenter={() => (activeIndex = filtered.length)}
              onclick={() => pick(query.trim())}
            >
              <span class="mp-check" aria-hidden="true">+</span>
              <span class="mp-row-main">
                <span class="mp-row-name">使用模型 {query.trim()}</span>
                <span class="mp-row-id">手动指定模型 ID</span>
              </span>
            </button>
          {/if}
        {/if}
      </div>

      <div class="mp-foot">
        <span>右侧价格为缓存读 · 输入 / 输出，USD / 百万 token</span>
        <span class="mp-count">{mergedModels.length} 个模型</span>
      </div>
    </div>
  {/if}
</div>

<style>
  .mp {
    position: relative;
    min-width: 0;
  }

  .mp-trigger {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    height: 36px;
    max-width: 340px;
    padding: 0 10px 0 12px;
    border-radius: 8px;
    border: 1px solid var(--line);
    background: var(--field);
    color: var(--fg);
    font-family: var(--font-sans);
    font-size: var(--text-sm);
    cursor: pointer;
    min-width: 0;
    transition: border-color 150ms var(--ease-out), box-shadow 150ms var(--ease-out);
  }

  .mp-trigger:hover:not(:disabled),
  .mp-trigger.open {
    border-color: var(--line-strong);
  }

  .mp-trigger:focus-visible {
    outline: none;
    border-color: var(--accent);
    box-shadow: 0 0 0 3px color-mix(in srgb, var(--accent) 18%, transparent);
  }

  .mp-title-wrap {
    display: inline-flex;
    align-items: baseline;
    gap: 4px;
    min-width: 0;
    overflow: hidden;
  }

  .mp-name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .mp-author {
    color: var(--fg-3);
    white-space: nowrap;
    flex-shrink: 0;
  }

  .mp-chip {
    font-size: var(--text-xs);
    padding: 0 6px;
    height: 20px;
    display: inline-flex;
    align-items: center;
    border-radius: 6px;
    background: var(--accent-soft);
    color: var(--accent);
    flex-shrink: 0;
  }

  .mp-id-pill {
    font-family: var(--font-mono);
    font-size: var(--text-xs);
    color: var(--fg-3);
    max-width: 130px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    flex-shrink: 1;
  }

  .mp-chev {
    width: 15px;
    height: 15px;
    flex-shrink: 0;
    color: var(--fg-3);
    transition: transform 200ms var(--ease-spring), color 150ms;
  }

  .mp-chev.rot {
    color: var(--fg);
    transform: scale(1.06);
  }

  .mp-popover {
    position: absolute;
    top: calc(100% + 6px);
    left: 0;
    width: min(520px, calc(100vw - 24px));
    z-index: 90;
    overflow: hidden;
    border-radius: 14px;
    border: 1px solid var(--line);
    background: var(--panel);
    box-shadow: var(--shadow-pop);
    transform-origin: top left;
    animation: pop-in 220ms var(--ease-spring);
  }

  .mp-search-bar {
    display: flex;
    align-items: center;
    gap: 8px;
    height: 46px;
    padding: 0 12px;
    border-bottom: 1px solid var(--line);
  }

  .mp-search-ico {
    width: 16px;
    height: 16px;
    color: var(--fg-3);
    flex-shrink: 0;
  }

  .mp-search-input {
    flex: 1;
    min-width: 0;
    height: 100%;
    border: 0;
    background: transparent;
    color: var(--fg);
    font-family: var(--font-sans);
    font-size: var(--text-base);
    outline: none;
  }

  .mp-search-input::placeholder {
    color: var(--fg-3);
  }

  .mp-refresh {
    width: 30px;
    height: 30px;
    display: grid;
    place-items: center;
    border: 0;
    border-radius: 8px;
    background: transparent;
    color: var(--fg-2);
    cursor: pointer;
    transition: background-color 140ms, color 140ms;
  }

  .mp-refresh:hover {
    background: var(--raised);
    color: var(--fg);
  }

  .mp-refresh svg {
    width: 15px;
    height: 15px;
  }

  .mp-refresh.spin svg {
    animation: spin 750ms linear infinite;
  }

  .mp-list {
    max-height: 340px;
    overflow-y: auto;
    overscroll-behavior: contain;
    padding: 6px;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  .mp-empty {
    padding: 28px 12px;
    text-align: center;
    font-size: var(--text-sm);
    color: var(--fg-3);
  }

  .mp-row {
    width: 100%;
    display: flex;
    align-items: flex-start;
    gap: 10px;
    padding: 9px 10px;
    border: 0;
    border-radius: 10px;
    background: transparent;
    color: var(--fg);
    font-family: var(--font-sans);
    text-align: left;
    cursor: pointer;
    transition: background-color 120ms var(--ease-out);
  }

  .mp-row.focused,
  .mp-row:hover {
    background: var(--raised);
  }

  .mp-row.selected {
    background: var(--mark-soft);
  }

  .mp-check {
    width: 16px;
    height: 20px;
    display: grid;
    place-items: center;
    flex-shrink: 0;
    color: var(--accent);
  }

  .mp-check svg {
    width: 15px;
    height: 15px;
  }

  .mp-row-main {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  .mp-row-head {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 6px;
  }

  .mp-row-name {
    font-size: var(--text-base);
    color: var(--fg);
  }

  .mp-row-author {
    font-size: var(--text-sm);
    color: var(--fg-3);
  }

  .mp-tag {
    font-size: var(--text-xs);
    padding: 0 6px;
    border-radius: 6px;
    background: var(--sunken);
    color: var(--fg-2);
    border: 1px solid var(--line);
  }

  .mp-tag.tag-think {
    background: var(--accent-soft);
    color: var(--accent);
    border-color: transparent;
  }

  .mp-row-id {
    font-family: var(--font-mono);
    font-size: var(--text-xs);
    color: var(--fg-3);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .mp-row-desc {
    font-size: var(--text-xs);
    color: var(--fg-2);
    display: -webkit-box;
    -webkit-line-clamp: 2;
    line-clamp: 2;
    -webkit-box-orient: vertical;
    overflow: hidden;
    margin-top: 2px;
  }

  .mp-price {
    flex-shrink: 0;
    text-align: right;
    display: flex;
    flex-direction: column;
    gap: 2px;
    font-variant-numeric: tabular-nums;
  }

  .mp-price-cache {
    font-size: var(--text-xs);
    color: var(--ok);
    background: var(--ok-soft);
    padding: 1px 6px;
    border-radius: 6px;
    white-space: nowrap;
  }

  .mp-price-io {
    font-size: var(--text-xs);
    color: var(--fg-3);
    white-space: nowrap;
  }

  .mp-foot {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    padding: 8px 14px;
    border-top: 1px solid var(--line);
    background: var(--sunken);
    font-size: var(--text-xs);
    color: var(--fg-3);
  }

  .mp-count {
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
  }

  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }
</style>
