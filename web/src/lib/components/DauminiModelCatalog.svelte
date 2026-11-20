<script lang="ts">
  import { onMount } from 'svelte';
  import type { AgentController } from '../store.svelte';

  let {
    ctrl,
    onSelectModel,
    onSetDefaultModel
  }: {
    ctrl: AgentController;
    onSelectModel: (modelId: string) => void;
    onSetDefaultModel: (modelId: string) => void;
  } = $props();

  let catalogModels = $state<any[]>([]);
  let loading = $state(false);
  let filter = $state('');
  let calcHit = $state(95);
  let expandedId = $state<string | null>(null);
  let epCache = $state<Record<string, any>>({});
  let loadingEpId = $state<string | null>(null);

  const filtered = $derived.by(() => {
    const q = filter.trim().toLowerCase();
    if (!q) return catalogModels;
    return catalogModels.filter((m) =>
      `${m.id || ''} ${m.name || ''} ${m.author || ''}`.toLowerCase().includes(q)
    );
  });

  function extractPricing(m: any): { read: number; input: number; output: number } {
    const eps: any[] = Array.isArray(m.endpoints) ? m.endpoints : [];
    const p = eps[0]?.pricing || m.pricing || {};
    return {
      read: Number(p.cache_read_nusd_per_million || m.cache_read_nusd_per_million || 0),
      input: Number(p.input_nusd_per_million || m.input_nusd_per_million || 0),
      output: Number(p.output_nusd_per_million || m.output_nusd_per_million || 0)
    };
  }

  function blendedInputPerM(m: any): number {
    const pr = extractPricing(m);
    if (!pr.input) return 0;
    const hit = calcHit / 100;
    const readPrice = pr.read > 0 ? pr.read : pr.input;
    return Math.round(readPrice * hit + pr.input * (1 - hit));
  }

  async function loadCatalog() {
    loading = true;
    try {
      const res = await ctrl.loadDauminiCatalogRaw();
      if (Array.isArray(res?.data)) catalogModels = res.data;
    } catch {
      // Fallback to already-fetched ModelInfo catalog
      catalogModels = ctrl.providerCatalog['daumini'] ?? [];
    } finally {
      loading = false;
    }
  }

  async function toggleEndpoints(modelId: string) {
    if (expandedId === modelId) {
      expandedId = null;
      return;
    }
    expandedId = modelId;
    if (epCache[modelId]) return;
    loadingEpId = modelId;
    try {
      const res = await ctrl.loadDauminiModelEndpoints(modelId);
      epCache = { ...epCache, [modelId]: res?.data ?? res };
    } catch {
      epCache = { ...epCache, [modelId]: { error: '查询失败' } };
    } finally {
      loadingEpId = null;
    }
  }

  onMount(() => {
    void loadCatalog();
  });
</script>

<div class="dmc-wrap">
  <div class="dmc-bar">
    <input
      class="field border-line dmc-search"
      bind:value={filter}
      placeholder="筛选 daumini 可用模型"
    />
    <div class="dmc-calc">
      <span class="dmc-calc-label">缓存命中 <strong class="tnum">{calcHit}%</strong></span>
      <input type="range" min="0" max="99" step="1" bind:value={calcHit} class="dmc-slider" />
    </div>
    <button
      type="button"
      class="btn btn-xs btn-quiet border-line"
      onclick={() => ctrl.setCurrency(ctrl.currency === 'USD' ? 'CNY' : 'USD')}
    >
      {ctrl.currency === 'USD' ? 'USD $' : 'CNY ¥'}
    </button>
    <button type="button" class="btn btn-xs btn-ghost" disabled={loading} onclick={() => void loadCatalog()}>
      {loading ? '同步中' : '刷新'}
    </button>
  </div>

  {#if filtered.length === 0}
    <div class="dmc-empty">{loading ? '正在拉取在线模型与路由' : '暂无匹配模型'}</div>
  {:else}
    <div class="dmc-list">
      {#each filtered as m (m.id)}
        {@const pr = extractPricing(m)}
        {@const blended = blendedInputPerM(m)}
        {@const caps = m.capabilities || {}}
        {@const endpoints = Array.isArray(m.endpoints) ? m.endpoints : []}
        {@const isCurrent = ctrl.current?.meta.provider_id === 'daumini' && ctrl.current?.meta.model_id === m.id}
        <div class="dmc-item" class:active={isCurrent}>
          <div class="dmc-row-top">
            <div class="dmc-id-col">
              <div class="dmc-name-row">
                <span class="dmc-name">{m.name || m.id}</span>
                {#if isCurrent}<span class="tag ok">当前</span>{/if}
                {#if caps.reasoning_levels?.length}
                  <span class="tag">思考 {caps.reasoning_levels.join('/')}</span>
                {/if}
                {#if m.context_length || caps.context_window}
                  {@const ctx = m.context_length || caps.context_window}
                  <span class="tag">{ctx >= 1_000_000 ? `${(ctx / 1_000_000).toFixed(0)}M` : `${Math.round(ctx / 1000)}K`}</span>
                {/if}
              </div>
              <code class="dmc-slug">{m.id}</code>
            </div>
            <div class="dmc-actions">
              <button type="button" class="btn btn-xs btn-ghost" onclick={() => void toggleEndpoints(m.id)}>
                {expandedId === m.id ? '收起路由' : `路由 (${endpoints.length || 1})`}
              </button>
              <button type="button" class="btn btn-xs btn-quiet border-line" onclick={() => onSetDefaultModel(m.id)}>
                设为默认
              </button>
              <button type="button" class="btn btn-xs btn-primary hatch-blue hatch-label border-line-strong" onclick={() => onSelectModel(m.id)}>
                选用
              </button>
            </div>
          </div>

          <div class="dmc-prices tnum">
            <span class="dmc-price-chip hi">缓存读 <strong>{ctrl.formatPricePerM(pr.read)}/M</strong></span>
            <span class="dmc-price-chip">{calcHit}% 综合 <strong>{ctrl.formatPricePerM(blended)}/M</strong></span>
            <span class="dmc-price-chip">输入 <strong>{ctrl.formatPricePerM(pr.input)}/M</strong></span>
            <span class="dmc-price-chip">输出 <strong>{ctrl.formatPricePerM(pr.output)}/M</strong></span>
          </div>

          {#if expandedId === m.id}
            <div class="dmc-eps">
              {#if loadingEpId === m.id}
                <span class="dmc-ep-hint">查询上游端点中</span>
              {:else}
                {@const detail = epCache[m.id]}
                {@const liveEps = Array.isArray(detail?.endpoints) ? detail.endpoints : endpoints}
                {#each liveEps as ep, idx (ep.id || idx)}
                  <div class="dmc-ep-row">
                    <span class="tag">{ep.provider_name || ep.provider_id || 'upstream'}</span>
                    <code>{ep.id || ep.upstream_model || m.id}</code>
                    {#if ep.supported_protocols?.length}
                      <span class="dmc-ep-hint">{ep.supported_protocols.join(' · ')}</span>
                    {/if}
                    {#if ep.latency_ms}
                      <span class="dmc-ep-hint tnum" style="margin-left: auto;">{ep.latency_ms}ms</span>
                    {/if}
                  </div>
                {/each}
              {/if}
            </div>
          {/if}
        </div>
      {/each}
    </div>
  {/if}
</div>

<style>
  .dmc-wrap {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding-top: 6px;
  }

  .dmc-bar {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
  }

  .dmc-search {
    flex: 1;
    min-width: 170px;
    height: 30px;
    padding: 0 9px;
    font-size: var(--text-xs);
  }

  .dmc-calc {
    display: inline-flex;
    align-items: center;
    gap: 7px;
  }

  .dmc-calc-label {
    font-size: var(--text-xs);
    color: var(--fg-2);
    white-space: nowrap;
  }

  .dmc-slider {
    width: 96px;
    accent-color: var(--accent);
    cursor: pointer;
  }

  .dmc-empty {
    font-size: var(--text-xs);
    color: var(--fg-3);
    padding: 10px 4px;
  }

  .dmc-list {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .dmc-item {
    padding: 9px 11px;
    border-radius: 9px;
    background: var(--sunken);
    border: 1px solid var(--line);
    display: flex;
    flex-direction: column;
    gap: 6px;
    transition: border-color 150ms var(--ease-out);
  }
  .dmc-item.active {
    border-color: color-mix(in srgb, var(--accent) 45%, var(--line));
  }

  .dmc-row-top {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 8px;
    flex-wrap: wrap;
  }

  .dmc-id-col {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
  }

  .dmc-name-row {
    display: flex;
    align-items: center;
    gap: 6px;
    flex-wrap: wrap;
  }

  .dmc-name {
    font-size: var(--text-sm);
    font-weight: 500;
    color: var(--fg);
  }

  .dmc-slug {
    font-family: var(--font-mono);
    font-size: var(--text-xs);
    color: var(--fg-3);
  }

  .dmc-actions {
    display: flex;
    align-items: center;
    gap: 5px;
    flex-shrink: 0;
  }

  .dmc-prices {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }

  .dmc-price-chip {
    padding: 3px 8px;
    border-radius: 6px;
    background: var(--panel);
    border: 1px solid var(--line);
    font-size: var(--text-xs);
    color: var(--fg-3);
    display: inline-flex;
    gap: 5px;
  }
  .dmc-price-chip strong {
    font-family: var(--font-mono);
    color: var(--fg);
    font-weight: 500;
  }
  .dmc-price-chip.hi {
    background: var(--ok-soft);
    border-color: color-mix(in srgb, var(--ok) 26%, transparent);
    color: var(--ok);
  }
  .dmc-price-chip.hi strong {
    color: var(--ok);
    font-weight: 600;
  }

  .dmc-eps {
    padding-top: 6px;
    border-top: 1px solid var(--line);
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .dmc-ep-row {
    display: flex;
    align-items: center;
    gap: 7px;
    padding: 4px 8px;
    border-radius: 6px;
    background: var(--panel);
    font-size: var(--text-xs);
    flex-wrap: wrap;
  }
  .dmc-ep-row code {
    font-family: var(--font-mono);
    color: var(--fg);
  }

  .dmc-ep-hint {
    font-size: var(--text-xs);
    color: var(--fg-3);
  }
</style>
