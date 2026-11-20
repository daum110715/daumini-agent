<script lang="ts">
  import { onMount } from 'svelte';
  import { fade } from 'svelte/transition';
  import { dialogIn, popOut } from '../motion';
  import type { AgentController } from '../store.svelte';
  import type { DauminiGenerationResponse, ModelInfo } from '../types';

  let {
    ctrl,
    generationId,
    onClose,
    onCopy
  }: {
    ctrl: AgentController;
    generationId: string;
    onClose: () => void;
    onCopy: (text: string, label: string) => void;
  } = $props();

  let loading = $state(true);
  let err = $state('');
  let detail = $state<DauminiGenerationResponse | null>(null);

  const gen = $derived(detail?.generation);
  const cacheRead = $derived(gen?.cache_usage?.cache_read_tokens ?? 0);
  const cacheWrite = $derived(gen?.cache_usage?.cache_write_tokens ?? 0);
  const totalPrompt = $derived(gen?.prompt_tokens ?? 0);
  const uncachedPrompt = $derived(Math.max(0, totalPrompt - cacheRead - cacheWrite));
  const hitPct = $derived(totalPrompt > 0 ? Math.min(100, Math.round((cacheRead / totalPrompt) * 100)) : 0);

  const modelMeta = $derived.by((): ModelInfo | undefined => {
    const id = gen?.resolved_model || gen?.requested_model;
    if (!id) return undefined;
    return (ctrl.providerCatalog['daumini'] ?? []).find((m) => m.id === id);
  });

  const savedNUSD = $derived.by(() => {
    if (!modelMeta?.input_nusd_per_million || cacheRead <= 0) return 0;
    const readRate = modelMeta.cache_read_nusd_per_million ?? 0;
    const diffPerM = Math.max(0, modelMeta.input_nusd_per_million - readRate);
    return Math.round((cacheRead / 1_000_000) * diffPerM);
  });

  function fmtNum(n?: number): string {
    if (!n) return '0';
    return n.toLocaleString('en-US');
  }

  async function fetchDetail() {
    loading = true;
    err = '';
    try {
      detail = await ctrl.loadDauminiGeneration(generationId);
    } catch (e) {
      err = e instanceof Error ? e.message : String(e);
    } finally {
      loading = false;
    }
  }

  onMount(() => {
    void fetchDetail();
  });
</script>

<!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
<div
  class="modal-backdrop"
  role="presentation"
  transition:fade={{ duration: 160 }}
  onclick={(e) => { if (e.target === e.currentTarget) onClose(); }}
>
  <div
    class="gi-modal border-line-strong"
    role="dialog"
    aria-modal="true"
    aria-label="路由与费用明细"
    in:dialogIn
    out:popOut={{ y: 4, end: 0.97, duration: 130 }}
  >
    <div class="gi-head">
      <div class="gi-head-left">
        <span class="gi-title">路由与费用明细</span>
        <button
          type="button"
          class="gi-id-pill"
          title="复制 Generation ID"
          onclick={() => onCopy(generationId, '已复制 Generation ID')}
        >
          <code>{generationId}</code>
        </button>
      </div>
      <div class="gi-head-actions">
        <button
          type="button"
          class="btn btn-xs btn-quiet border-line"
          onclick={() => ctrl.setCurrency(ctrl.currency === 'USD' ? 'CNY' : 'USD')}
        >
          {ctrl.currency === 'USD' ? 'USD 美元' : 'CNY 人民币'}
        </button>
        <button type="button" class="wobble btn btn-icon-sm btn-ghost" aria-label="关闭" onclick={onClose}>
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M18 6 6 18M6 6l12 12"/></svg>
        </button>
      </div>
    </div>

    <div class="gi-body">
      {#if loading}
        <div class="gi-state">正在向 daumini 查询路由与结算记录</div>
      {:else if err}
        <div class="notice notice-error border-line">
          <span>{err}</span>
          <div>
            <button type="button" class="btn btn-xs btn-quiet border-line" onclick={() => void fetchDetail()}>重试</button>
          </div>
        </div>
      {:else if gen}
        <div class="gi-hero">
          <div class="gi-hero-card">
            <span class="gi-kpi-label">缓存命中率</span>
            <span class="gi-kpi-val ok tnum">{hitPct}%</span>
            <div class="cache-meter" style="margin-top: 6px;">
              <span class="cache-meter-fill" style:width={`${hitPct}%`}></span>
            </div>
          </div>
          <div class="gi-hero-card">
            <span class="gi-kpi-label">实际结算费用</span>
            <span class="gi-kpi-val tnum">{ctrl.formatNUSD(gen.charged_nusd, 5)}</span>
            <span class="gi-kpi-sub">
              {#if savedNUSD > 0}
                缓存节省 {ctrl.formatNUSD(savedNUSD, 5)}
              {:else}
                预占 {ctrl.formatNUSD(gen.reserved_nusd, 5)}
              {/if}
            </span>
          </div>
          <div class="gi-hero-card">
            <span class="gi-kpi-label">端到端耗时</span>
            <span class="gi-kpi-val tnum">{gen.latency_ms ? `${(gen.latency_ms / 1000).toFixed(2)}s` : '—'}</span>
            <span class="gi-kpi-sub">{gen.settlement_state || gen.state || 'completed'} · {gen.finish_reason || 'stop'}</span>
          </div>
        </div>

        <div class="gi-sec">
          <div class="gi-sec-title">Token 构成</div>
          <div class="gi-token-grid">
            <div class="gi-tok">
              <span class="gi-tok-label">缓存读取</span>
              <span class="gi-tok-val ok tnum">{fmtNum(cacheRead)}</span>
            </div>
            <div class="gi-tok">
              <span class="gi-tok-label">缓存写入</span>
              <span class="gi-tok-val tnum">{fmtNum(cacheWrite)}</span>
            </div>
            <div class="gi-tok">
              <span class="gi-tok-label">未命中输入</span>
              <span class="gi-tok-val tnum">{fmtNum(uncachedPrompt)}</span>
            </div>
            <div class="gi-tok">
              <span class="gi-tok-label">输出生成</span>
              <span class="gi-tok-val tnum">{fmtNum(gen.completion_tokens)}</span>
            </div>
            <div class="gi-tok">
              <span class="gi-tok-label">推理思考</span>
              <span class="gi-tok-val tnum">{fmtNum(gen.reasoning_tokens)}</span>
            </div>
          </div>
        </div>

        <div class="gi-sec">
          <div class="gi-sec-title">上游路由尝试 ({gen.attempts?.length ?? 0})</div>
          {#if (gen.attempts?.length ?? 0) === 0}
            <div class="gi-empty-sub">无上游尝试明细</div>
          {:else}
            <div class="gi-attempts">
              {#each gen.attempts ?? [] as att (att.id)}
                <div class="gi-attempt">
                  <span class="gi-att-seq">#{att.sequence}</span>
                  <code class="gi-att-ep">{att.endpoint_id}</code>
                  <span class="tag {att.http_status >= 200 && att.http_status < 300 ? 'ok' : 'bad'}">
                    HTTP {att.http_status || '—'}
                  </span>
                  <span class="tag">{att.state}</span>
                  {#if att.response_committed}
                    <span class="tag ok">已提交响应</span>
                  {/if}
                  {#if att.error_class}
                    <span class="tag bad">{att.error_class}</span>
                  {/if}
                  {#if att.cost_nusd}
                    <span class="gi-att-cost tnum">{ctrl.formatNUSD(att.cost_nusd, 5)}</span>
                  {/if}
                </div>
              {/each}
            </div>
          {/if}
        </div>

        <div class="gi-sec">
          <div class="gi-sec-title">请求元数据与结算</div>
          <div class="gi-kv-grid">
            <div class="gi-kv">
              <span>请求模型</span>
              <code>{gen.requested_model || '—'}</code>
            </div>
            <div class="gi-kv">
              <span>实际路由模型</span>
              <code>{gen.resolved_model || '—'}</code>
            </div>
            <div class="gi-kv">
              <span>供应商</span>
              <code>{gen.provider_id || '—'}</code>
            </div>
            <div class="gi-kv">
              <span>用量质量</span>
              <code>{gen.usage_quality || '—'}</code>
            </div>
            {#if gen.request_id}
              <div class="gi-kv">
                <span>Request ID</span>
                <button type="button" class="gi-copy-inline" onclick={() => onCopy(gen.request_id || '', '已复制 Request ID')}>
                  <code>{gen.request_id}</code>
                </button>
              </div>
            {/if}
            {#if gen.created_at}
              <div class="gi-kv">
                <span>记录时间</span>
                <code>{new Date(gen.created_at).toLocaleString()}</code>
              </div>
            {/if}
          </div>
        </div>

        {#if detail?.reconciliation}
          <div class="gi-sec">
            <div class="gi-sec-title">对账详情</div>
            <pre class="code-surface" style="max-height: 150px;">{JSON.stringify(detail.reconciliation, null, 2)}</pre>
          </div>
        {/if}
      {/if}
    </div>
  </div>
</div>

<style>
  .gi-modal {
    width: min(680px, 94vw);
    max-height: 86vh;
    border-radius: 14px;
    background: var(--panel);
    box-shadow: var(--shadow-pop);
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }

  .gi-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
    padding: 13px 16px;
    border-bottom: 1px solid var(--line);
    flex-shrink: 0;
  }

  .gi-head-left {
    display: flex;
    align-items: center;
    gap: 10px;
    min-width: 0;
  }

  .gi-title {
    font-size: var(--text-md);
    font-weight: 600;
    color: var(--fg);
    white-space: nowrap;
  }

  .gi-id-pill {
    padding: 2px 8px;
    border-radius: 6px;
    background: var(--sunken);
    border: 1px solid var(--line);
    font-family: var(--font-mono);
    font-size: var(--text-xs);
    color: var(--fg-2);
    max-width: 240px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .gi-id-pill:hover {
    background: var(--raised);
    color: var(--accent);
  }

  .gi-head-actions {
    display: flex;
    align-items: center;
    gap: 6px;
    flex-shrink: 0;
  }

  .gi-body {
    padding: 14px 16px 18px;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: 14px;
  }

  .gi-state {
    padding: 36px 12px;
    text-align: center;
    color: var(--fg-3);
    font-size: var(--text-sm);
  }

  .gi-hero {
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
    gap: 10px;
  }

  .gi-hero-card {
    padding: 11px 12px;
    border-radius: 10px;
    background: var(--sunken);
    border: 1px solid var(--line);
    display: flex;
    flex-direction: column;
    gap: 3px;
  }

  .gi-kpi-label {
    font-size: var(--text-xs);
    color: var(--fg-3);
  }

  .gi-kpi-val {
    font-size: var(--text-xl);
    font-weight: 600;
    color: var(--fg);
    line-height: 1.2;
  }
  .gi-kpi-val.ok {
    color: var(--ok);
  }

  .gi-kpi-sub {
    font-size: var(--text-xs);
    color: var(--fg-3);
    margin-top: 2px;
  }

  .gi-sec {
    display: flex;
    flex-direction: column;
    gap: 7px;
  }

  .gi-sec-title {
    font-size: var(--text-xs);
    color: var(--fg-2);
    font-weight: 500;
  }

  .gi-token-grid {
    display: grid;
    grid-template-columns: repeat(5, minmax(0, 1fr));
    gap: 6px;
  }

  .gi-tok {
    padding: 8px 9px;
    border-radius: 8px;
    background: var(--sunken);
    border: 1px solid var(--line);
    display: flex;
    flex-direction: column;
    gap: 3px;
  }

  .gi-tok-label {
    font-size: 11px;
    color: var(--fg-3);
  }

  .gi-tok-val {
    font-family: var(--font-mono);
    font-size: var(--text-sm);
    color: var(--fg);
  }
  .gi-tok-val.ok {
    color: var(--ok);
    font-weight: 600;
  }

  .gi-attempts {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .gi-attempt {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 7px;
    padding: 8px 10px;
    border-radius: 8px;
    background: var(--sunken);
    border: 1px solid var(--line);
    font-size: var(--text-xs);
  }

  .gi-att-seq {
    font-family: var(--font-mono);
    color: var(--fg-3);
    font-weight: 600;
  }

  .gi-att-ep {
    font-family: var(--font-mono);
    color: var(--fg);
  }

  .gi-att-cost {
    margin-left: auto;
    font-family: var(--font-mono);
    color: var(--fg-2);
  }

  .gi-empty-sub {
    font-size: var(--text-xs);
    color: var(--fg-3);
    padding: 8px 10px;
    border-radius: 8px;
    background: var(--sunken);
  }

  .gi-kv-grid {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 6px;
  }

  .gi-kv {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    padding: 7px 10px;
    border-radius: 8px;
    background: var(--sunken);
    border: 1px solid var(--line);
    font-size: var(--text-xs);
  }
  .gi-kv span {
    color: var(--fg-3);
    flex-shrink: 0;
  }
  .gi-kv code {
    font-family: var(--font-mono);
    color: var(--fg);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .gi-copy-inline {
    padding: 0;
    color: var(--accent);
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .gi-copy-inline:hover code {
    color: var(--accent);
    text-decoration: underline;
  }

  @media (max-width: 640px) {
    .gi-hero { grid-template-columns: 1fr; }
    .gi-token-grid { grid-template-columns: repeat(2, 1fr); }
    .gi-kv-grid { grid-template-columns: 1fr; }
  }
</style>
