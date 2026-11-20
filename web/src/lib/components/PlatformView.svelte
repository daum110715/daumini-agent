<script lang="ts">
  import { onMount } from 'svelte';
  import Segmented from './Segmented.svelte';
  import type { AgentController } from '../store.svelte';

  let {
    ctrl,
    onInspectGeneration,
    onSelectModel,
    onSetDefaultModel,
    onOpenLogin,
    onTriggerOAuth
  }: {
    ctrl: AgentController;
    onInspectGeneration: (genId: string) => void;
    onSelectModel: (modelId: string) => void;
    onSetDefaultModel: (modelId: string) => void;
    onOpenLogin: () => void;
    onTriggerOAuth: () => void;
  } = $props();

  let subTab = $state<'ledger' | 'catalog'>('ledger');
  let loading = $state(false);
  let catalogModels = $state<any[]>([]);
  let catalogFilter = $state('');
  let calcCacheHit = $state(95);
  let expandedModelId = $state<string | null>(null);
  let endpointsCache = $state<Record<string, any>>({});
  let loadingEndpointId = $state<string | null>(null);

  const filteredModels = $derived.by(() => {
    const q = catalogFilter.trim().toLowerCase();
    if (!q) return catalogModels;
    return catalogModels.filter((m) =>
      `${m.id || ''} ${m.name || ''} ${m.author || ''} ${m.description || ''}`.toLowerCase().includes(q)
    );
  });

  function totalIn(u?: { input_tokens?: number; cache_read_tokens?: number; cache_write_tokens?: number }): number {
    if (!u) return 0;
    return (u.input_tokens || 0) + (u.cache_read_tokens || 0) + (u.cache_write_tokens || 0);
  }

  function cachePct(u?: { input_tokens?: number; cache_read_tokens?: number; cache_write_tokens?: number }): string {
    const t = totalIn(u);
    if (t <= 0) return '0%';
    return `${Math.min(100, Math.round(((u?.cache_read_tokens || 0) / t) * 100))}%`;
  }

  function fmtCompact(n?: number): string {
    if (!n) return '0';
    if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(2)}M`;
    if (n >= 1_000) return `${(n / 1_000).toFixed(1)}k`;
    return String(n);
  }

  function extractPricing(m: any): { read: number; write: number; input: number; output: number } {
    const eps: any[] = Array.isArray(m.endpoints) ? m.endpoints : [];
    const first = eps[0] || {};
    const p = first.pricing || m.pricing || {};
    return {
      read: Number(p.cache_read_nusd_per_million || m.cache_read_nusd_per_million || 0),
      write: Number(p.cache_write_nusd_per_million || m.cache_write_nusd_per_million || 0),
      input: Number(p.input_nusd_per_million || m.input_nusd_per_million || 0),
      output: Number(p.output_nusd_per_million || m.output_nusd_per_million || 0)
    };
  }

  // Blended cost per 1M prompt tokens at current slider hit rate (e.g. 95% cache read + 5% uncached)
  function blendedInputPerM(m: any): number {
    const pr = extractPricing(m);
    if (!pr.input) return 0;
    const hit = calcCacheHit / 100;
    const readPrice = pr.read > 0 ? pr.read : pr.input;
    return Math.round(readPrice * hit + pr.input * (1 - hit));
  }

  async function refreshAll() {
    loading = true;
    try {
      const [, , cat] = await Promise.all([
        ctrl.loadDauminiStatus().catch(() => null),
        ctrl.loadDauminiLedger().catch(() => null),
        ctrl.loadDauminiCatalogRaw().catch(() => null)
      ]);
      if (cat && Array.isArray(cat.data)) {
        catalogModels = cat.data;
      }
    } finally {
      loading = false;
    }
  }

  async function toggleEndpoints(modelId: string) {
    if (expandedModelId === modelId) {
      expandedModelId = null;
      return;
    }
    expandedModelId = modelId;
    if (endpointsCache[modelId]) return;
    loadingEndpointId = modelId;
    try {
      const res = await ctrl.loadDauminiModelEndpoints(modelId);
      endpointsCache = { ...endpointsCache, [modelId]: res?.data ?? res };
    } catch {
      endpointsCache = { ...endpointsCache, [modelId]: { error: '端点查询失败' } };
    } finally {
      loadingEndpointId = null;
    }
  }

  onMount(() => {
    void refreshAll();
  });
</script>

<div class="page-scroll">
  <div class="page-container">
    <div class="pv-head">
      <div>
        <h1 class="page-title scribble">daumini 平台中心</h1>
        <div class="pv-sub">在 Agent 内直接查看路由明细 费用账单 在线模型与缓存定价</div>
      </div>
      <div class="pv-head-actions">
        <Segmented
          size="sm"
          label="货币单位"
          value={ctrl.currency}
          options={[
            { value: 'USD', label: 'USD $' },
            { value: 'CNY', label: 'CNY ¥' }
          ]}
          onchange={(v) => ctrl.setCurrency(v)}
        />
        <button type="button" class="wobble btn btn-sm btn-quiet border-line" disabled={loading} onclick={() => void refreshAll()}>
          {loading ? '同步中' : '刷新数据'}
        </button>
      </div>
    </div>

    <div class="card-section border-line pv-account-card">
      <div class="pv-account-row">
        <div class="pv-account-main">
          <span class="status-dot" style:background={ctrl.dauminiStatus?.connected ? 'var(--ok)' : 'var(--warn)'}></span>
          <div class="pv-account-text">
            <div class="pv-account-email">
              {ctrl.dauminiStatus?.connected ? (ctrl.dauminiStatus.user_email || 'daumini 已授权连接') : '尚未连接 daumini 中转'}
            </div>
            <div class="pv-account-meta">
              <span>{ctrl.dauminiStatus?.base_url || 'https://mini.daum.pw'}</span>
              {#if ctrl.dauminiStatus?.key?.workspace_id}
                <span>工作空间 {ctrl.dauminiStatus.key.workspace_id}</span>
              {/if}
              {#if ctrl.dauminiStatus?.key?.scopes?.length}
                <span>权限 {ctrl.dauminiStatus.key.scopes.join(' / ')}</span>
              {/if}
            </div>
          </div>
        </div>
        <div class="pv-account-btns">
          {#if ctrl.dauminiStatus?.connected}
            <button type="button" class="wobble btn btn-sm btn-quiet border-line" onclick={onOpenLogin}>切换或退出</button>
          {:else}
            <button type="button" class="wobble btn btn-sm btn-primary hatch-blue hatch-label border-line-strong" onclick={onTriggerOAuth}>
              一键登录 daumini
            </button>
          {/if}
        </div>
      </div>
    </div>

    <div class="pv-tabs">
      <Segmented
        label="平台功能"
        value={subTab}
        options={[
          { value: 'ledger', label: '路由与费用账单' },
          { value: 'catalog', label: `模型市场与路由 (${catalogModels.length})` }
        ]}
        onchange={(v) => (subTab = v)}
      />
    </div>

    {#if subTab === 'ledger'}
      <div class="pv-kpi-grid">
        <div class="card-section border-line pv-kpi">
          <span class="pv-kpi-label">全局缓存命中率</span>
          <span class="pv-kpi-val ok tnum">
            {Math.round((ctrl.dauminiLedger?.cache_hit_ratio ?? 0) * 100)}%
          </span>
          <div class="cache-meter">
            <span class="cache-meter-fill" style:width={`${Math.round((ctrl.dauminiLedger?.cache_hit_ratio ?? 0) * 100)}%`}></span>
          </div>
        </div>
        <div class="card-section border-line pv-kpi">
          <span class="pv-kpi-label">累计总费用</span>
          <span class="pv-kpi-val tnum">{ctrl.formatNUSD(ctrl.dauminiLedger?.totals?.total_cost_nusd, 4)}</span>
          <span class="pv-kpi-sub">{ctrl.dauminiLedger?.request_count ?? 0} 次调用 · {ctrl.dauminiLedger?.session_count ?? 0} 个会话</span>
        </div>
        <div class="card-section border-line pv-kpi">
          <span class="pv-kpi-label">缓存读取 Token</span>
          <span class="pv-kpi-val tnum">{fmtCompact(ctrl.dauminiLedger?.totals?.cache_read_tokens)}</span>
          <span class="pv-kpi-sub">总输入 {fmtCompact(totalIn(ctrl.dauminiLedger?.totals))}</span>
        </div>
        <div class="card-section border-line pv-kpi">
          <span class="pv-kpi-label">总输出 Token</span>
          <span class="pv-kpi-val tnum">{fmtCompact(ctrl.dauminiLedger?.totals?.output_tokens)}</span>
          <span class="pv-kpi-sub">推理 {fmtCompact(ctrl.dauminiLedger?.totals?.reasoning_tokens)}</span>
        </div>
      </div>

      {#if (ctrl.dauminiLedger?.by_model?.length ?? 0) > 0}
        <div class="card-section border-line">
          <span class="pv-sec-title">按模型汇总</span>
          <div class="md-table-wrap" style="margin: 0;">
            <table class="md-table">
              <thead>
                <tr>
                  <th>模型</th>
                  <th>调用次数</th>
                  <th>缓存命中</th>
                  <th>缓存读取</th>
                  <th>输出</th>
                  <th>累计费用</th>
                </tr>
              </thead>
              <tbody>
                {#each ctrl.dauminiLedger?.by_model ?? [] as row (row.provider_id + row.model_id)}
                  <tr>
                    <td><code>{row.model_id}</code></td>
                    <td class="tnum">{row.request_count}</td>
                    <td class="tnum"><span class="marked">{Math.round(row.cache_hit_ratio * 100)}%</span></td>
                    <td class="tnum">{fmtCompact(row.usage.cache_read_tokens)}</td>
                    <td class="tnum">{fmtCompact(row.usage.output_tokens)}</td>
                    <td class="tnum">{ctrl.formatNUSD(row.usage.total_cost_nusd, 4)}</td>
                  </tr>
                {/each}
              </tbody>
            </table>
          </div>
        </div>
      {/if}

      <div class="card-section border-line">
        <div class="pv-sec-head">
          <span class="pv-sec-title">最近调用记录</span>
          <span class="pv-sec-hint">点击任意 daumini 记录直接在 Agent 内查看上游路由与对账</span>
        </div>
        {#if (ctrl.dauminiLedger?.recent?.length ?? 0) === 0}
          <div class="pv-empty">暂无调用记录 发送一条对话后会在此汇总</div>
        {:else}
          <div class="pv-recent-list">
            {#each ctrl.dauminiLedger?.recent ?? [] as item (item.message_id)}
              {@const canInspect = item.provider_id === 'daumini' && item.generation_id?.startsWith('gen_')}
              <div class="pv-recent-row">
                <div class="pv-recent-main">
                  <span class="pv-recent-model">{item.model_id}</span>
                  <span class="pv-recent-sess">{item.session_title}</span>
                  <span class="pv-recent-time">{new Date(item.created_at).toLocaleString()}</span>
                </div>
                <div class="pv-recent-stats tnum">
                  <span class="marked">缓存 {cachePct(item.usage)}</span>
                  <span>入 {fmtCompact(totalIn(item.usage))}</span>
                  <span>出 {fmtCompact(item.usage.output_tokens)}</span>
                  <span class="pv-recent-cost">{ctrl.formatNUSD(item.usage.total_cost_nusd, 4)}</span>
                  {#if item.usage.latency_ms}
                    <span>{(item.usage.latency_ms / 1000).toFixed(1)}s</span>
                  {/if}
                  {#if canInspect && item.generation_id}
                    <button
                      type="button"
                      class="wobble btn btn-xs btn-quiet border-line"
                      onclick={() => item.generation_id && onInspectGeneration(item.generation_id)}
                    >
                      路由与费用
                    </button>
                  {/if}
                </div>
              </div>
            {/each}
          </div>
        {/if}
      </div>
    {:else}
      <div class="card-section border-line pv-calc-bar">
        <div class="pv-calc-top">
          <input
            class="field border-line pv-search"
            bind:value={catalogFilter}
            placeholder="搜索 daumini 在线模型 名称 作者或能力"
          />
          <div class="pv-slider-box">
            <span class="pv-slider-label">缓存命中测算 <strong class="tnum">{calcCacheHit}%</strong></span>
            <input type="range" min="0" max="99" step="1" bind:value={calcCacheHit} class="pv-slider" />
          </div>
        </div>
      </div>

      <div class="pv-model-list">
        {#each filteredModels as m (m.id)}
          {@const pr = extractPricing(m)}
          {@const blended = blendedInputPerM(m)}
          {@const caps = m.capabilities || {}}
          {@const endpoints = Array.isArray(m.endpoints) ? m.endpoints : []}
          {@const isActive = ctrl.current?.meta.model_id === m.id && ctrl.current?.meta.provider_id === 'daumini'}
          <div class="card-section border-line pv-model-card" class:is-active={isActive}>
            <div class="pv-model-top">
              <div class="pv-model-id-col">
                <div class="pv-model-name-row">
                  <span class="pv-model-name">{m.name || m.id}</span>
                  {#if isActive}<span class="tag ok">当前会话</span>{/if}
                  {#if caps.reasoning_levels?.length}
                    <span class="tag">思考 {caps.reasoning_levels.join('/')}</span>
                  {/if}
                  {#if m.context_length || caps.context_window}
                    {@const ctx = m.context_length || caps.context_window}
                    <span class="tag">{ctx >= 1_000_000 ? `${(ctx / 1_000_000).toFixed(0)}M` : `${Math.round(ctx / 1000)}K`} 上下文</span>
                  {/if}
                </div>
                <code class="pv-model-slug">{m.id}</code>
              </div>

              <div class="pv-model-actions">
                <button type="button" class="wobble btn btn-xs btn-ghost" onclick={() => void toggleEndpoints(m.id)}>
                  {expandedModelId === m.id ? '收起路由' : `路由端点 (${endpoints.length || 1})`}
                </button>
                <button type="button" class="wobble btn btn-xs btn-quiet border-line" onclick={() => onSetDefaultModel(m.id)}>
                  设为默认
                </button>
                <button type="button" class="wobble btn btn-xs btn-primary hatch-blue hatch-label border-line-strong" onclick={() => onSelectModel(m.id)}>
                  当前会话选用
                </button>
              </div>
            </div>

            <div class="pv-price-strip tnum">
              <div class="pv-price-item highlight">
                <span>缓存读</span>
                <strong>{ctrl.formatPricePerM(pr.read)}/M</strong>
              </div>
              <div class="pv-price-item">
                <span>{calcCacheHit}% 命中综合输入</span>
                <strong>{ctrl.formatPricePerM(blended)}/M</strong>
              </div>
              <div class="pv-price-item">
                <span>普通输入</span>
                <strong>{ctrl.formatPricePerM(pr.input)}/M</strong>
              </div>
              <div class="pv-price-item">
                <span>输出</span>
                <strong>{ctrl.formatPricePerM(pr.output)}/M</strong>
              </div>
            </div>

            {#if expandedModelId === m.id}
              <div class="pv-ep-panel">
                {#if loadingEndpointId === m.id}
                  <span class="pv-sec-hint">正在查询实时上游路由端点</span>
                {:else}
                  {@const detail = endpointsCache[m.id]}
                  {@const liveEps = Array.isArray(detail?.endpoints) ? detail.endpoints : endpoints}
                  {#if liveEps.length === 0}
                    <span class="pv-sec-hint">暂无公开端点明细</span>
                  {:else}
                    {#each liveEps as ep, idx (ep.id || idx)}
                      <div class="pv-ep-row">
                        <span class="tag">{ep.provider_name || ep.provider_id || 'upstream'}</span>
                        <code class="pv-ep-id">{ep.id || ep.upstream_model || m.id}</code>
                        {#if ep.supported_protocols?.length}
                          <span class="pv-sec-hint">{ep.supported_protocols.join(' · ')}</span>
                        {/if}
                        {#if ep.latency_ms}
                          <span class="pv-sec-hint tnum" style="margin-left: auto;">{ep.latency_ms}ms</span>
                        {/if}
                      </div>
                    {/each}
                  {/if}
                {/if}
              </div>
            {/if}
          </div>
        {/each}
      </div>
    {/if}
  </div>
</div>

<style>
  .pv-head {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 12px;
    flex-wrap: wrap;
  }

  .pv-sub {
    font-size: var(--text-xs);
    color: var(--fg-3);
    margin-top: 4px;
  }

  .pv-head-actions {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .pv-account-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    flex-wrap: wrap;
  }

  .pv-account-main {
    display: flex;
    align-items: center;
    gap: 10px;
    min-width: 0;
  }

  .pv-account-text {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
  }

  .pv-account-email {
    font-size: var(--text-base);
    font-weight: 500;
    color: var(--fg);
  }

  .pv-account-meta {
    display: flex;
    flex-wrap: wrap;
    gap: 10px;
    font-size: var(--text-xs);
    color: var(--fg-3);
    font-family: var(--font-mono);
  }

  .pv-tabs {
    display: flex;
    align-items: center;
  }

  .pv-kpi-grid {
    display: grid;
    grid-template-columns: repeat(4, minmax(0, 1fr));
    gap: 10px;
  }

  .pv-kpi {
    padding: 12px 14px;
    gap: 4px;
  }

  .pv-kpi-label {
    font-size: var(--text-xs);
    color: var(--fg-3);
  }

  .pv-kpi-val {
    font-size: var(--text-xl);
    font-weight: 600;
    color: var(--fg);
    line-height: 1.25;
  }
  .pv-kpi-val.ok {
    color: var(--ok);
  }

  .pv-kpi-sub {
    font-size: var(--text-xs);
    color: var(--fg-3);
  }

  .pv-sec-head {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 8px;
    flex-wrap: wrap;
  }

  .pv-sec-title {
    font-size: var(--text-base);
    font-weight: 600;
    color: var(--fg);
  }

  .pv-sec-hint {
    font-size: var(--text-xs);
    color: var(--fg-3);
  }

  .pv-empty {
    padding: 20px 8px;
    text-align: center;
    font-size: var(--text-sm);
    color: var(--fg-3);
  }

  .pv-recent-list {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .pv-recent-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
    padding: 8px 10px;
    border-radius: 9px;
    background: var(--sunken);
    border: 1px solid var(--line);
    flex-wrap: wrap;
  }

  .pv-recent-main {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
  }

  .pv-recent-model {
    font-family: var(--font-mono);
    font-size: var(--text-xs);
    color: var(--fg);
    font-weight: 500;
  }

  .pv-recent-sess {
    font-size: var(--text-xs);
    color: var(--fg-2);
    max-width: 180px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .pv-recent-time {
    font-size: 11px;
    color: var(--fg-3);
  }

  .pv-recent-stats {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: var(--text-xs);
    color: var(--fg-2);
    flex-wrap: wrap;
  }

  .pv-recent-cost {
    font-family: var(--font-mono);
    color: var(--fg);
    font-weight: 500;
  }

  .pv-calc-top {
    display: flex;
    align-items: center;
    gap: 14px;
    flex-wrap: wrap;
  }

  .pv-search {
    flex: 1;
    min-width: 220px;
    height: 34px;
    padding: 0 11px;
    font-size: var(--text-sm);
  }

  .pv-slider-box {
    display: flex;
    align-items: center;
    gap: 10px;
  }

  .pv-slider-label {
    font-size: var(--text-xs);
    color: var(--fg-2);
    white-space: nowrap;
  }

  .pv-slider {
    width: 130px;
    accent-color: var(--accent);
    cursor: pointer;
  }

  .pv-model-list {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }

  .pv-model-card.is-active {
    border-color: color-mix(in srgb, var(--accent) 45%, var(--line));
  }

  .pv-model-top {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 10px;
    flex-wrap: wrap;
  }

  .pv-model-id-col {
    display: flex;
    flex-direction: column;
    gap: 4px;
    min-width: 0;
  }

  .pv-model-name-row {
    display: flex;
    align-items: center;
    gap: 6px;
    flex-wrap: wrap;
  }

  .pv-model-name {
    font-size: var(--text-base);
    font-weight: 600;
    color: var(--fg);
  }

  .pv-model-slug {
    font-family: var(--font-mono);
    font-size: var(--text-xs);
    color: var(--fg-3);
  }

  .pv-model-actions {
    display: flex;
    align-items: center;
    gap: 6px;
    flex-wrap: wrap;
  }

  .pv-price-strip {
    display: grid;
    grid-template-columns: repeat(4, minmax(0, 1fr));
    gap: 6px;
  }

  .pv-price-item {
    padding: 7px 10px;
    border-radius: 8px;
    background: var(--sunken);
    border: 1px solid var(--line);
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 6px;
    font-size: var(--text-xs);
    color: var(--fg-3);
  }
  .pv-price-item strong {
    font-family: var(--font-mono);
    font-weight: 600;
    color: var(--fg);
  }
  .pv-price-item.highlight {
    background: var(--ok-soft);
    border-color: color-mix(in srgb, var(--ok) 28%, transparent);
    color: var(--ok);
  }
  .pv-price-item.highlight strong {
    color: var(--ok);
  }

  .pv-ep-panel {
    padding-top: 8px;
    border-top: 1px solid var(--line);
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .pv-ep-row {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 6px 9px;
    border-radius: 7px;
    background: var(--sunken);
    font-size: var(--text-xs);
    flex-wrap: wrap;
  }

  .pv-ep-id {
    font-family: var(--font-mono);
    color: var(--fg);
  }

  @media (max-width: 820px) {
    .pv-kpi-grid { grid-template-columns: repeat(2, 1fr); }
    .pv-price-strip { grid-template-columns: repeat(2, 1fr); }
  }
</style>
