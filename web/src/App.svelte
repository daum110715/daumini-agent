<script lang="ts">
  import { onMount, tick } from 'svelte';
  import { AgentController } from './lib/store.svelte';
  import { renderMarkdown } from './lib/markdown';
  import Segmented from './lib/components/Segmented.svelte';
  import Wordmark from './lib/components/Wordmark.svelte';
  import type { AgentConfig, ApprovalMode, Block, MCPServerConfig, ProviderConfig, Run, Usage } from './lib/types';

  const ctrl = new AgentController();

  let tab = $state<'work' | 'providers' | 'extensions'>('work');
  let showInspector = $state(true);
  let promptText = $state('');
  let newWorkspace = $state('.');
  let showNewBox = $state(false);
  let toastText = $state('');
  let toastTimer: ReturnType<typeof setTimeout> | null = null;

  // Session inline rename & delete confirm state
  let renamingId = $state<string | null>(null);
  let renameDraft = $state('');
  let deletingId = $state<string | null>(null);

  // One-click daumini login modal state (fallback / account switcher)
  let showLoginModal = $state(false);
  let loginMode = $state<'oauth' | 'account' | 'key'>('oauth');
  let dmBase = $state('https://mini.daum.pw');
  let dmEmail = $state('');
  let dmPass = $state('');
  let dmInvite = $state('');
  let dmShowInvite = $state(false);
  let dmShowBase = $state(false);
  let dmKey = $state('');
  let dmBusy = $state(false);
  let dmErr = $state('');
  let dmOk = $state('');

  // Custom provider & MCP form state
  let newProvName = $state('');
  let newProvUrl = $state('');
  let newProvKey = $state('');
  let newProvModel = $state('');

  let mcpName = $state('');
  let mcpTransport = $state<'stdio' | 'http'>('stdio');
  let mcpCommand = $state('');
  let mcpArgs = $state('');
  let mcpURL = $state('');

  let feedEl = $state<HTMLDivElement | null>(null);
  let composerEl = $state<HTMLTextAreaElement | null>(null);
  let userScrolledUp = false;

  // Gliding marker for sidebar navigation
  const navItems: { id: 'work' | 'providers' | 'extensions'; label: string }[] = [
    { id: 'work', label: '工作台' },
    { id: 'providers', label: '模型' },
    { id: 'extensions', label: '扩展' }
  ];
  let navButtons = $state<HTMLButtonElement[]>([]);
  let navMarker = $state({ y: 0, h: 0, glide: false });
  let navPlaced = false;

  $effect(() => {
    const idx = navItems.findIndex((item) => item.id === tab);
    const el = navButtons[idx];
    if (!el) return;
    navMarker = { y: el.offsetTop, h: el.offsetHeight, glide: navPlaced };
    if (!navPlaced) {
      navPlaced = true;
      void tick().then(() => {
        navMarker = { ...navMarker, glide: true };
      });
    }
  });

  // Auto-grow composer textarea up to 220px (matching daumini Composer.svelte)
  $effect(() => {
    void promptText;
    if (!composerEl) return;
    composerEl.style.height = 'auto';
    composerEl.style.height = `${Math.min(Math.max(composerEl.scrollHeight, 52), 220)}px`;
  });

  // Smart auto-scroll when messages or stream text update
  $effect(() => {
    const msgCount = ctrl.current?.messages?.length ?? 0;
    const streamLen = ctrl.activeStreamText.length + ctrl.activeStreamThinking.length;
    const toolCount = Object.keys(ctrl.activeTools).length;
    void msgCount;
    void streamLen;
    void toolCount;
    if (!feedEl || userScrolledUp) return;
    void tick().then(() => {
      if (feedEl && !userScrolledUp) {
        feedEl.scrollTo({ top: feedEl.scrollHeight, behavior: isRunning ? 'auto' : 'smooth' });
      }
    });
  });

  function showToast(msg: string) {
    toastText = msg;
    if (toastTimer) clearTimeout(toastTimer);
    toastTimer = setTimeout(() => {
      toastText = '';
    }, 2600);
  }

  onMount(() => {
    const params = new URLSearchParams(window.location.search);
    const authStatus = params.get('auth');
    const authError = params.get('auth_error');
    if (authStatus || authError) {
      window.history.replaceState({}, '', window.location.pathname);
    }

    void ctrl.bootstrap().then(async () => {
      const dm = ctrl.config?.providers.find((p) => p.id === 'daumini');
      if (dm?.base_url) dmBase = dm.base_url;
      if (dm?.user_email) dmEmail = dm.user_email;
      if (authStatus === 'ok') {
        await ctrl.syncAfterOAuthCallback();
        showToast(`已通过 daumini 授权登录${dm?.user_email ? ` ${dm.user_email}` : ''}`);
      } else if (authError) {
        ctrl.error = authError === 'access_denied' ? '已取消 daumini 授权' : authError;
      } else if (dm?.api_key) {
        void ctrl.fetchProviderModels('daumini').catch(() => {});
      }
    });

    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        showLoginModal = false;
        showNewBox = false;
        renamingId = null;
        deletingId = null;
        if (ctrl.filePreview) ctrl.filePreview = null;
      }
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault();
        composerEl?.focus();
      }
    };
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  });

  const dauminiProvider = $derived(ctrl.config?.providers.find((p) => p.id === 'daumini'));
  const dauminiConnected = $derived(Boolean(dauminiProvider?.api_key));
  const currentProvider = $derived(
    ctrl.config?.providers.find((p) => p.id === ctrl.current?.meta.provider_id)
  );
  const currentProviderReady = $derived(Boolean(currentProvider?.api_key));
  const currentModelSuggestions = $derived(
    ctrl.current ? (ctrl.providerModels[ctrl.current.meta.provider_id] ?? []) : []
  );

  const isRunning = $derived(Boolean(ctrl.current?.meta.active_run_id));
  const pendingApprovals = $derived(
    (ctrl.current?.pending_approvals ?? []).filter((a) => a.decision === 'pending')
  );

  const toolResultsMap = $derived.by(() => {
    const map: Record<string, Block> = {};
    for (const m of ctrl.current?.messages ?? []) {
      for (const b of m.blocks ?? []) {
        if (b.type === 'tool_result' && b.tool_use_id) {
          map[b.tool_use_id] = b;
        }
      }
    }
    return map;
  });

  const failedRunByRunID = $derived.by(() => {
    const map: Record<string, Run> = {};
    for (const r of ctrl.current?.runs ?? []) {
      if (r.status === 'failed' && r.error) {
        map[r.id] = r;
      }
    }
    return map;
  });

  const latestOrphanFailedRun = $derived.by(() => {
    const runs = ctrl.current?.runs ?? [];
    if (runs.length === 0) return null;
    const last = runs[runs.length - 1];
    if (last.status !== 'failed' || !last.error) return null;
    const hasAttachedMsg = (ctrl.current?.messages ?? []).some((m) => m.run_id === last.id);
    return hasAttachedMsg ? null : last;
  });

  const starters = ['梳理当前项目结构', '检查最近代码改动', '帮我定位并修复问题'];

  function withTransition(fn: () => void) {
    const doc = document as Document & { startViewTransition?: (cb: () => void) => void };
    if (doc.startViewTransition && !window.matchMedia('(prefers-reduced-motion: reduce)').matches) {
      doc.startViewTransition(fn);
    } else {
      fn();
    }
  }

  function switchTab(next: 'work' | 'providers' | 'extensions') {
    withTransition(() => {
      tab = next;
      if (next === 'extensions') void ctrl.loadLogs();
    });
  }

  function openLoginModal(mode: 'oauth' | 'account' | 'key' = 'oauth') {
    loginMode = mode;
    dmErr = '';
    dmOk = '';
    showLoginModal = true;
  }

  async function triggerOneClickOAuth() {
    dmBusy = true;
    dmErr = '';
    try {
      await ctrl.startDauminiOAuth(dmBase);
    } catch (e) {
      dmBusy = false;
      ctrl.error = e instanceof Error ? e.message : String(e);
    }
  }

  function cachePct(u?: Usage | null): string {
    if (!u) return '0%';
    const total = (u.input_tokens || 0) + (u.cache_read_tokens || 0) + (u.cache_write_tokens || 0);
    if (total <= 0) return '0%';
    return `${Math.round(((u.cache_read_tokens || 0) / total) * 100)}%`;
  }

  function totalIn(u?: Usage | null): number {
    if (!u) return 0;
    return (u.input_tokens || 0) + (u.cache_read_tokens || 0) + (u.cache_write_tokens || 0);
  }

  function fmtNum(n?: number): string {
    if (!n) return '0';
    if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
    if (n >= 1_000) return `${(n / 1_000).toFixed(1)}k`;
    return String(n);
  }

  function fmtCost(nusd?: number): string {
    if (!nusd) return '';
    return `$${(nusd / 1e9).toFixed(4)}`;
  }

  function fmtJSON(v: unknown): string {
    if (typeof v === 'string') return v;
    try {
      return JSON.stringify(v, null, 2);
    } catch {
      return String(v ?? '');
    }
  }

  function looksLikeAuthError(msg?: string): boolean {
    if (!msg) return false;
    const s = msg.toLowerCase();
    return s.includes('api_key') || s.includes('api key') || s.includes('missing') || s.includes('401') || s.includes('unauth') || s.includes('密钥');
  }

  function handleFeedScroll() {
    if (!feedEl) return;
    const dist = feedEl.scrollHeight - feedEl.scrollTop - feedEl.clientHeight;
    userScrolledUp = dist > 72;
  }

  function handleFeedClick(e: MouseEvent) {
    const target = (e.target as HTMLElement)?.closest<HTMLButtonElement>('.md-copy-btn');
    if (!target) return;
    const encoded = target.dataset.copy;
    if (!encoded) return;
    void copyText(decodeURIComponent(encoded), '已复制代码');
  }

  async function copyText(text: string, label = '已复制') {
    try {
      await navigator.clipboard.writeText(text);
      showToast(label);
    } catch {
      showToast('复制失败');
    }
  }

  function messagePlainText(blocks?: Block[]): string {
    return (blocks ?? [])
      .filter((b) => b.type === 'text' && b.text)
      .map((b) => b.text)
      .join('\n\n');
  }

  async function handleSend(mode: 'queue' | 'steer' | 'interrupt' = 'queue', overrideText?: string) {
    const txt = (overrideText ?? promptText).trim();
    if (!txt) return;
    if (!overrideText) promptText = '';
    userScrolledUp = false;
    try {
      await ctrl.sendMessage(txt, mode);
    } catch (e) {
      ctrl.error = e instanceof Error ? e.message : String(e);
    }
  }

  async function handleQuickNewSession() {
    tab = 'work';
    showNewBox = false;
    const dir = ctrl.current?.meta.workspace_dir || '.';
    await ctrl.createSession(dir);
    showToast('已新建会话');
    void tick().then(() => composerEl?.focus());
  }

  async function handleCreateSessionWithDir() {
    showNewBox = false;
    await ctrl.createSession(newWorkspace.trim() || '.');
    showToast('已新建会话');
    void tick().then(() => composerEl?.focus());
  }

  async function commitRename(id: string) {
    const val = renameDraft.trim();
    renamingId = null;
    if (!val) return;
    try {
      await ctrl.renameSession(id, val);
      showToast('已重命名会话');
    } catch (e) {
      ctrl.error = e instanceof Error ? e.message : String(e);
    }
  }

  async function confirmDelete(id: string) {
    deletingId = null;
    try {
      await ctrl.deleteSession(id);
      showToast('已删除会话');
    } catch (e) {
      ctrl.error = e instanceof Error ? e.message : String(e);
    }
  }

  function updateProviderField(idx: number, patch: Partial<ProviderConfig>) {
    if (!ctrl.config) return;
    const next: AgentConfig = structuredClone($state.snapshot(ctrl.config));
    next.providers[idx] = { ...next.providers[idx], ...patch };
    ctrl.config = next;
  }

  async function saveProviders() {
    if (!ctrl.config) return;
    await ctrl.saveConfig($state.snapshot(ctrl.config));
    showToast('已保存渠道配置');
  }

  async function addCustomProvider() {
    if (!ctrl.config || !newProvName.trim() || !newProvUrl.trim()) return;
    const id = newProvName.trim().toLowerCase().replace(/[^a-z0-9_-]+/g, '-');
    const next: AgentConfig = structuredClone($state.snapshot(ctrl.config));
    next.providers.push({
      id,
      name: newProvName.trim(),
      protocol: 'openai_chat',
      base_url: newProvUrl.trim(),
      api_key: newProvKey.trim(),
      default_model: newProvModel.trim(),
      enabled: true
    });
    next.default_provider = id;
    if (newProvModel.trim()) next.default_model = newProvModel.trim();
    await ctrl.saveConfig(next);
    newProvName = '';
    newProvUrl = '';
    newProvKey = '';
    newProvModel = '';
    showToast('已添加自定义渠道');
  }

  async function doDauminiLogin() {
    if (!dmEmail.trim() || !dmPass) {
      dmErr = '请输入邮箱和密码';
      return;
    }
    dmBusy = true;
    dmErr = '';
    dmOk = '';
    try {
      await ctrl.loginDaumini(dmBase, dmEmail.trim(), dmPass, dmInvite.trim());
      dmPass = '';
      dmOk = '已登录并自动签发密钥';
      showToast('已接入 daumini 账号');
      setTimeout(() => {
        showLoginModal = false;
      }, 450);
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      if (msg.includes('invalid_invite_code') || msg.includes('beta_invite_required')) {
        dmShowInvite = true;
      }
      dmErr = msg;
    } finally {
      dmBusy = false;
    }
  }

  async function doDauminiKeySave() {
    if (!dmKey.trim()) {
      dmErr = '请粘贴 daumini API 密钥';
      return;
    }
    dmBusy = true;
    dmErr = '';
    dmOk = '';
    try {
      await ctrl.saveDauminiKey(dmBase, dmKey.trim());
      dmKey = '';
      dmOk = '密钥已保存并接入';
      showToast('已保存 daumini 密钥');
      setTimeout(() => {
        showLoginModal = false;
      }, 400);
    } catch (e) {
      dmErr = e instanceof Error ? e.message : String(e);
    } finally {
      dmBusy = false;
    }
  }

  async function addMCP() {
    if (!ctrl.config || !mcpName.trim()) return;
    const next: AgentConfig = structuredClone($state.snapshot(ctrl.config));
    const entry: MCPServerConfig = {
      name: mcpName.trim(),
      transport: mcpTransport,
      enabled: true
    };
    if (mcpTransport === 'stdio') {
      entry.command = mcpCommand.trim();
      entry.args = mcpArgs.trim() ? mcpArgs.trim().split(/\s+/) : [];
    } else {
      entry.url = mcpURL.trim();
    }
    next.mcp_servers = [...(next.mcp_servers ?? []), entry];
    await ctrl.saveConfig(next);
    mcpName = '';
    mcpCommand = '';
    mcpArgs = '';
    mcpURL = '';
    showToast('已添加 MCP 服务');
  }

  async function removeMCP(idx: number) {
    if (!ctrl.config) return;
    const next: AgentConfig = structuredClone($state.snapshot(ctrl.config));
    next.mcp_servers = (next.mcp_servers ?? []).filter((_, i) => i !== idx);
    await ctrl.saveConfig(next);
    showToast('已移除 MCP 服务');
  }
</script>

<div class="shell {tab === 'work' && showInspector ? 'with-inspector' : ''}">
  <aside class="sidebar">
    <div class="brand-row">
      <span style="font-size: 20px;">
        <Wordmark live={isRunning} />
      </span>
      <button
        type="button"
        class="wobble btn btn-icon btn-ghost"
        title={ctrl.theme === 'light' ? '切换深色' : '切换浅色'}
        aria-label={ctrl.theme === 'light' ? '切换深色' : '切换浅色'}
        onclick={() => ctrl.toggleTheme()}
      >
        {#if ctrl.theme === 'light'}
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M12 3a6 6 0 0 0 9 9 9 9 0 1 1-9-9Z"/></svg>
        {:else}
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="4"/><path d="M12 2v2m0 16v2M4.93 4.93l1.41 1.41m11.32 11.32 1.41 1.41M2 12h2m16 0h2M6.34 17.66l-1.41 1.41M19.07 4.93l-1.41 1.41"/></svg>
        {/if}
      </button>
    </div>

    <nav class="side-nav" aria-label="主导航">
      <span
        aria-hidden="true"
        class="side-nav-marker border-line {navMarker.glide ? 'glide' : ''}"
        style:translate="0 {navMarker.y}px"
        style:height="{navMarker.h}px"
        style:opacity={navMarker.h ? 1 : 0}
      ></span>
      {#each navItems as item, i (item.id)}
        <button
          bind:this={navButtons[i]}
          type="button"
          class="side-nav-btn {tab === item.id ? 'active' : ''}"
          onclick={() => switchTab(item.id)}
        >
          {#if item.id === 'work'}
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z"/></svg>
          {:else if item.id === 'providers'}
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><rect width="18" height="18" x="3" y="3" rx="2"/><path d="M7 7h10M7 12h10M7 17h6"/></svg>
          {:else}
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M12 2v4m0 12v4M4.93 4.93l2.83 2.83m8.48 8.48 2.83 2.83M2 12h4m12 0h4M4.93 19.07l2.83-2.83m8.48-8.48 2.83-2.83"/></svg>
          {/if}
          <span>{item.label}</span>
        </button>
      {/each}
    </nav>

    <div class="new-session-row">
      <button
        type="button"
        class="wobble new-session-btn border-line"
        onclick={() => void handleQuickNewSession()}
      >
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M12 5v14M5 12h14"/></svg>
        <span>新建会话</span>
      </button>
      <button
        type="button"
        class="wobble btn btn-icon btn-quiet border-line"
        style="height: 38px; width: 38px;"
        title="指定目录新建会话"
        aria-label="指定目录新建会话"
        onclick={() => {
          newWorkspace = ctrl.current?.meta.workspace_dir || '.';
          showNewBox = !showNewBox;
        }}
      >
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M20 20a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.9a2 2 0 0 1-1.69-.9L9.6 3.9A2 2 0 0 0 7.93 3H4a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2Z"/></svg>
      </button>
    </div>

    {#if showNewBox}
      <div class="border-line" style="padding: 11px; border-radius: 10px; background: var(--sunken); display: flex; flex-direction: column; gap: 8px; animation: pop-in 0.22s var(--ease-out);">
        <div class="field-col">
          <span class="field-label">工作区路径</span>
          <input
            class="field border-line"
            bind:value={newWorkspace}
            placeholder="."
            onkeydown={(e) => { if (e.key === 'Enter') void handleCreateSessionWithDir(); }}
          />
        </div>
        <button type="button" class="wobble btn btn-sm btn-primary hatch-blue hatch-label border-line-strong" onclick={() => void handleCreateSessionWithDir()}>
          创建并进入
        </button>
      </div>
    {/if}

    <div class="side-section-label">会话 ({ctrl.sessions.length})</div>
    <div class="session-list">
      {#each ctrl.sessions as s (s.id)}
        {@const isActive = ctrl.current?.meta.id === s.id}
        <div class="session-row {isActive ? 'active border-line' : ''} {deletingId === s.id ? 'confirming' : ''}">
          {#if renamingId === s.id}
            <div class="session-inline-box">
              <input
                class="field border-line"
                style="padding: 5px 8px; font-size: var(--text-sm);"
                bind:value={renameDraft}
                onkeydown={(e) => {
                  if (e.key === 'Enter') void commitRename(s.id);
                  if (e.key === 'Escape') renamingId = null;
                }}
              />
              <div style="display: flex; gap: 6px; justify-content: flex-end;">
                <button type="button" class="wobble btn btn-xs btn-ghost" onclick={() => (renamingId = null)}>取消</button>
                <button type="button" class="wobble btn btn-xs btn-primary hatch-blue hatch-label border-line-strong" onclick={() => void commitRename(s.id)}>保存</button>
              </div>
            </div>
          {:else if deletingId === s.id}
            <div class="session-inline-box">
              <span style="font-size: var(--text-sm); color: var(--bad);">确认删除此会话</span>
              <div style="display: flex; gap: 6px; justify-content: flex-end;">
                <button type="button" class="wobble btn btn-xs btn-ghost" onclick={() => (deletingId = null)}>取消</button>
                <button type="button" class="wobble btn btn-xs btn-danger hatch-red hatch-label border-line-strong" onclick={() => void confirmDelete(s.id)}>删除</button>
              </div>
            </div>
          {:else}
            <button
              type="button"
              class="session-select"
              onclick={() => {
                withTransition(() => {
                  tab = 'work';
                  userScrolledUp = false;
                  void ctrl.selectSession(s.id);
                });
              }}
            >
              <div class="session-head">
                <span class="session-title">{s.title || s.id}</span>
              </div>
              <div class="session-meta">
                <span>{s.model_id || s.provider_id}</span>
                <span>缓存 {cachePct(s.total_usage)}</span>
              </div>
            </button>
            <div class="session-actions">
              <button
                type="button"
                class="wobble btn btn-icon-sm btn-ghost"
                title="重命名"
                aria-label="重命名会话"
                onclick={(e) => {
                  e.stopPropagation();
                  deletingId = null;
                  renamingId = s.id;
                  renameDraft = s.title || '';
                }}
              >
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M12 20h9"/><path d="M16.376 3.622a1 1 0 0 1 3.002 3.002L7.368 18.635a2 2 0 0 1-.855.506l-2.872.838a.5.5 0 0 1-.62-.62l.838-2.872a2 2 0 0 1 .506-.854z"/></svg>
              </button>
              <button
                type="button"
                class="wobble btn btn-icon-sm btn-danger-ghost"
                title="删除会话"
                aria-label="删除会话"
                onclick={(e) => {
                  e.stopPropagation();
                  renamingId = null;
                  deletingId = s.id;
                }}
              >
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M3 6h18"/><path d="M19 6v14c0 1-1 2-2 2H7c-1 0-2-1-2-2V6"/><path d="M8 6V4c0-1 1-2 2-2h4c1 0 2 1 2 2v2"/></svg>
              </button>
            </div>
          {/if}
        </div>
      {/each}
    </div>

    <div class="sidebar-footer">
      {#if dauminiConnected}
        <div class="account-pill border-line">
          <div class="account-info">
            <span class="status-dot" title="daumini 已连接"></span>
            <span class="account-email" title={dauminiProvider?.user_email || 'daumini 已连接'}>
              {dauminiProvider?.user_email || 'daumini 已连接'}
            </span>
          </div>
          <button
            type="button"
            class="wobble btn btn-icon-sm btn-ghost"
            title="账号设置"
            aria-label="账号设置"
            onclick={() => openLoginModal('oauth')}
          >
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4"/><polyline points="16 17 21 12 16 7"/><line x1="21" x2="9" y1="12" y2="12"/></svg>
          </button>
        </div>
      {:else}
        <div style="padding: 0 2px;">
          <div style="font-size: var(--text-sm); color: var(--fg);">daumini 账号</div>
          <div style="font-size: var(--text-xs); color: var(--fg-3); margin-top: 2px;">跳转授权自动签发密钥</div>
        </div>
        <button
          type="button"
          class="wobble btn btn-sm btn-primary hatch-blue hatch-label border-line-strong"
          style="width: 100%;"
          disabled={dmBusy}
          onclick={() => void triggerOneClickOAuth()}
        >
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M15 3h4a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2h-4"/><polyline points="10 17 15 12 10 7"/><line x1="15" x2="3" y1="12" y2="12"/></svg>
          <span>{dmBusy ? '正在跳转授权' : '一键登录 daumini'}</span>
        </button>
        <button type="button" class="wobble btn btn-xs btn-ghost" style="width: 100%;" onclick={() => openLoginModal('account')}>
          账号密码或粘贴密钥
        </button>
      {/if}
    </div>
  </aside>

  <main class="main-pane paper">
    {#if ctrl.error}
      <div style="max-width: 780px; width: 100%; margin: 12px auto 0; padding: 0 24px;">
        <div class="notice notice-error border-line">
          <div style="display: flex; justify-content: space-between; align-items: center; gap: 12px;">
            <span>{ctrl.error}</span>
            <div style="display: flex; gap: 6px;">
              {#if looksLikeAuthError(ctrl.error)}
                <button type="button" class="wobble btn btn-sm btn-primary hatch-blue hatch-label border-line-strong" onclick={() => void triggerOneClickOAuth()}>
                  一键登录 daumini
                </button>
              {/if}
              <button type="button" class="wobble btn btn-sm btn-ghost" onclick={() => (ctrl.error = '')}>关闭</button>
            </div>
          </div>
        </div>
      </div>
    {/if}

    {#if tab === 'work'}
      <header class="topbar glass">
        <div class="topbar-left">
          <span class="topbar-title">{ctrl.current?.meta.title || '未选择会话'}</span>
          {#if ctrl.current}
            <span class="topbar-path" title={ctrl.current.meta.workspace_dir}>{ctrl.current.meta.workspace_dir}</span>
            {#if ctrl.current.meta.parent_session_id}
              <span class="tag mark">分叉</span>
            {/if}
          {/if}
        </div>

        {#if ctrl.current}
          <div class="topbar-actions">
            <select
              class="field-quiet border-line"
              value={ctrl.current.meta.provider_id}
              onchange={(e) => {
                const pid = (e.target as HTMLSelectElement).value;
                const prov = ctrl.config?.providers.find((p) => p.id === pid);
                void ctrl.updateSession({ provider_id: pid, model_id: prov?.default_model || ctrl.current?.meta.model_id });
                if (prov?.api_key) void ctrl.fetchProviderModels(pid).catch(() => {});
              }}
            >
              {#each ctrl.config?.providers ?? [] as p (p.id)}
                <option value={p.id} selected={p.id === ctrl.current.meta.provider_id}>{p.name}</option>
              {/each}
            </select>

            <input
              class="field-quiet border-line"
              style="width: 210px; font-family: var(--font-mono); font-size: 13px;"
              list="model-suggestions"
              value={ctrl.current.meta.model_id}
              onchange={(e) => void ctrl.updateSession({ model_id: (e.target as HTMLInputElement).value })}
              placeholder="模型 ID"
            />
            <datalist id="model-suggestions">
              {#each currentModelSuggestions as m}
                <option value={m}></option>
              {/each}
            </datalist>

            <Segmented
              size="sm"
              label="工具权限"
              value={ctrl.current.meta.approval_mode}
              options={[
                { value: 'ask', label: '询问' },
                { value: 'auto_read', label: '只读' },
                { value: 'yolo', label: '放行' }
              ]}
              onchange={(v) => void ctrl.updateSession({ approval_mode: v as ApprovalMode })}
            />

            <button type="button" class="wobble btn btn-sm btn-ghost" title="分叉会话" onclick={() => void ctrl.forkSession(0)}>
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="18" r="3"/><circle cx="6" cy="6" r="3"/><circle cx="18" cy="6" r="3"/><path d="M18 9v2c0 .6-.4 1-1 1H7c-.6 0-1-.4-1-1V9"/><path d="M12 12v3"/></svg>
              <span>分叉</span>
            </button>
            <button
              type="button"
              class="wobble btn btn-sm btn-ghost"
              title={showInspector ? '收起工作区面板' : '展开工作区面板'}
              onclick={() => withTransition(() => { showInspector = !showInspector; })}
            >
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><rect width="18" height="18" x="3" y="3" rx="2"/><path d="M15 3v18"/></svg>
              <span>{showInspector ? '收起' : '工作区'}</span>
            </button>
          </div>
        {/if}
      </header>

      {#if (ctrl.current?.meta.todos?.length ?? 0) > 0}
        <div class="todo-strip">
          {#each ctrl.current?.meta.todos ?? [] as td}
            <span class="tag {td.status === 'completed' ? 'ok' : td.status === 'in_progress' ? 'mark' : ''}">
              {td.status === 'completed' ? '完成' : td.status === 'in_progress' ? '进行中' : '待办'} {td.content}
            </span>
          {/each}
        </div>
      {/if}

      <!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
      <div class="feed" bind:this={feedEl} onscroll={handleFeedScroll} onclick={handleFeedClick}>
        <div class="feed-inner">
          {#if ctrl.current && (ctrl.current.messages?.length ?? 0) === 0 && !ctrl.activeStreamText && !ctrl.activeStreamThinking && !latestOrphanFailedRun}
            <div class="empty-feed">
              <span style="font-size: 32px;"><Wordmark live={false} suffix="" /></span>
              <div style="font-size: var(--text-lg); margin-top: 2px;" class="scribble">{ctrl.current.meta.title}</div>
              <div style="font-family: var(--font-mono); font-size: var(--text-xs); color: var(--fg-3); margin-top: 8px;">
                {ctrl.current.meta.provider_id} / {ctrl.current.meta.model_id}
              </div>

              {#if !currentProviderReady}
                <div style="margin-top: 16px; display: flex; flex-direction: column; align-items: center; gap: 12px;">
                  <span style="font-size: var(--text-base); color: var(--fg-2);">当前渠道尚未配置密钥</span>
                  <div style="display: flex; gap: 10px;">
                    <button type="button" class="wobble btn btn-primary hatch-blue hatch-label border-line-strong" onclick={() => void triggerOneClickOAuth()}>
                      一键登录 daumini
                    </button>
                    <button type="button" class="wobble btn btn-quiet border-line" onclick={() => switchTab('providers')}>
                      配置其他模型
                    </button>
                  </div>
                </div>
              {:else}
                <div class="starter-row">
                  {#each starters as s}
                    <button
                      type="button"
                      class="wobble starter-chip border-line"
                      onclick={() => {
                        promptText = s;
                        composerEl?.focus();
                      }}
                    >
                      {s}
                    </button>
                  {/each}
                </div>
              {/if}
            </div>
          {/if}

          {#if ctrl.current}
            {@const msgs = ctrl.current.messages ?? []}
            {#each msgs as msg, idx (msg.id)}
              {@const nextMsg = msgs[idx + 1]}
              {@const failedRun = msg.role === 'user' && msg.run_id && (!nextMsg || nextMsg.role === 'user') ? failedRunByRunID[msg.run_id] : undefined}
              {#if msg.role === 'user'}
                {@const textBlocks = (msg.blocks ?? []).filter((b) => b.type === 'text' || b.type === 'system_notice')}
                {#if textBlocks.length > 0}
                  <div class="turn-user">
                    <span class="turn-rail-user" aria-hidden="true"></span>
                    <div class="msg-user border-line">
                      {#each textBlocks as tb}{tb.text}{/each}
                    </div>
                  </div>
                {/if}
                {#if failedRun}
                  <div class="turn-assistant {idx === msgs.length - 1 && !ctrl.activeStreamText ? 'is-last' : ''}">
                    <span class="turn-rail-tail" aria-hidden="true"></span>
                    <span class="knot-wrap" aria-hidden="true">
                      <span class="knot-dot bad"></span>
                    </span>
                    <div class="assistant-body">
                      <div class="assistant-speaker">{failedRun.model_id || ctrl.current.meta.model_id}</div>
                      <div class="notice notice-error border-line">
                        <span>{failedRun.error}</span>
                        <div style="display: flex; gap: 8px; flex-wrap: wrap;">
                          {#if looksLikeAuthError(failedRun.error)}
                            <button type="button" class="wobble btn btn-sm btn-primary hatch-blue hatch-label border-line-strong" onclick={() => void triggerOneClickOAuth()}>
                              一键登录 daumini
                            </button>
                          {/if}
                          {#if failedRun.prompt}
                            <button type="button" class="wobble btn btn-sm btn-quiet border-line" onclick={() => void handleSend('queue', failedRun.prompt)}>
                              重试
                            </button>
                          {/if}
                        </div>
                      </div>
                    </div>
                  </div>
                {/if}
              {:else if msg.role === 'assistant'}
                {@const plainText = messagePlainText(msg.blocks)}
                <div class="turn-assistant {idx === msgs.length - 1 && !ctrl.activeStreamText && !ctrl.activeStreamThinking ? 'is-last' : ''}">
                  <span class="turn-rail-tail" aria-hidden="true"></span>
                  <span class="knot-wrap" aria-hidden="true">
                    <span class="knot-dot"></span>
                  </span>
                  <div class="assistant-body">
                    <div class="assistant-speaker">{msg.model_id || ctrl.current.meta.model_id}</div>
                    {#each msg.blocks ?? [] as b}
                      {#if b.type === 'thinking' && b.thinking}
                        <details class="think-box border-line">
                          <summary>思考过程 {b.thinking.length} 字</summary>
                          <div style="margin-top: 8px; white-space: pre-wrap; line-height: 1.7; color: var(--fg-2);">{b.thinking}</div>
                        </details>
                      {:else if b.type === 'text' && b.text}
                        <div class="md-prose">{@html renderMarkdown(b.text)}</div>
                      {:else if b.type === 'tool_use'}
                        {@const res = b.id ? toolResultsMap[b.id] : undefined}
                        <details class="tool-card border-line">
                          <summary class="tool-head">
                            <span class="tool-name">{b.name}</span>
                            <span class="tag {res?.is_error ? 'bad' : res ? 'ok' : 'mark'}">
                              {res ? (res.is_error ? '报错' : '完成') : '执行中'}
                            </span>
                          </summary>
                          <pre class="code-surface">{fmtJSON(b.input)}</pre>
                          {#if res}
                            <pre class="code-surface">{res.text}</pre>
                          {/if}
                        </details>
                      {/if}
                    {/each}
                    {#if msg.usage || plainText}
                      <div class="usage-line">
                        {#if msg.usage}
                          <span class="marked">缓存 {cachePct(msg.usage)}</span>
                          <span>输入 {fmtNum(totalIn(msg.usage))}</span>
                          <span>输出 {fmtNum(msg.usage.output_tokens)}</span>
                          {#if msg.usage.total_cost_nusd}
                            <span>{fmtCost(msg.usage.total_cost_nusd)}</span>
                          {/if}
                        {/if}
                        {#if plainText}
                          <button type="button" class="wobble btn btn-xs btn-ghost" onclick={() => void copyText(plainText, '已复制回复')}>
                            复制
                          </button>
                        {/if}
                      </div>
                    {/if}
                  </div>
                </div>
              {/if}
            {/each}

            {#if latestOrphanFailedRun}
              <div class="turn-assistant is-last">
                <span class="turn-rail-tail" aria-hidden="true"></span>
                <span class="knot-wrap" aria-hidden="true">
                  <span class="knot-dot bad"></span>
                </span>
                <div class="assistant-body">
                  <div class="assistant-speaker">{latestOrphanFailedRun.model_id || ctrl.current.meta.model_id}</div>
                  <div class="notice notice-error border-line">
                    <span>{latestOrphanFailedRun.error}</span>
                    <div style="display: flex; gap: 8px; flex-wrap: wrap;">
                      {#if looksLikeAuthError(latestOrphanFailedRun.error)}
                        <button type="button" class="wobble btn btn-sm btn-primary hatch-blue hatch-label border-line-strong" onclick={() => void triggerOneClickOAuth()}>
                          一键登录 daumini
                        </button>
                      {/if}
                      {#if latestOrphanFailedRun.prompt}
                        <button type="button" class="wobble btn btn-sm btn-quiet border-line" onclick={() => void handleSend('queue', latestOrphanFailedRun.prompt)}>
                          重试
                        </button>
                      {/if}
                    </div>
                  </div>
                </div>
              </div>
            {/if}

            {#if isRunning || ctrl.activeStreamThinking || ctrl.activeStreamText || Object.keys(ctrl.activeTools).length > 0}
              <div class="turn-assistant is-last">
                <span class="turn-rail-tail" aria-hidden="true"></span>
                <span class="knot-wrap" aria-hidden="true">
                  <span class="knot-ping"></span>
                  <span class="knot-dot"></span>
                </span>
                <div class="assistant-body">
                  <div class="assistant-speaker">{ctrl.current.meta.model_id}</div>
                  {#if ctrl.activeStreamThinking}
                    <details class="think-box border-line" open>
                      <summary>正在思考</summary>
                      <div style="margin-top: 8px; white-space: pre-wrap; line-height: 1.7; color: var(--fg-2);">{ctrl.activeStreamThinking}</div>
                    </details>
                  {/if}
                  {#if ctrl.activeStreamText}
                    <div class="md-prose">{@html renderMarkdown(ctrl.activeStreamText)}<span class="caret" aria-hidden="true"></span></div>
                  {:else if !ctrl.activeStreamThinking && Object.keys(ctrl.activeTools).length === 0}
                    <div style="font-size: var(--text-base); color: var(--fg-3);">正在响应</div>
                  {/if}
                  {#each Object.entries(ctrl.activeTools) as [tid, t] (tid)}
                    <div class="tool-card border-line">
                      <div class="tool-head">
                        <span class="tool-name">{t.name}</span>
                        <span class="tag {t.output ? (t.is_error ? 'bad' : 'ok') : 'mark'}">
                          {t.output ? `${t.duration_ms ?? 0}ms` : '运行中'}
                        </span>
                      </div>
                      <pre class="code-surface">{fmtJSON(t.input)}</pre>
                      {#if t.output}
                        <pre class="code-surface">{t.output}</pre>
                      {/if}
                    </div>
                  {/each}
                </div>
              </div>
            {/if}
          {/if}
        </div>
      </div>

      {#if pendingApprovals.length > 0}
        <div class="approval-wrap">
          {#each pendingApprovals as ap (ap.id)}
            <div class="approval-bar border-line">
              <div style="display: flex; justify-content: space-between; align-items: center;">
                <span style="font-size: var(--text-base);">需要确认工具 <code style="font-family: var(--font-mono);">{ap.tool_name}</code></span>
                <span class="tag">Hash {ap.args_hash.slice(0, 8)}</span>
              </div>
              <pre class="code-surface">{fmtJSON(ap.args_json)}</pre>
              <div style="display: flex; gap: 8px;">
                <button type="button" class="wobble btn btn-sm btn-primary hatch-green hatch-label border-line-strong" onclick={() => void ctrl.resolveApproval(ap.id, 'approve_once')}>允许一次</button>
                <button type="button" class="wobble btn btn-sm btn-accent hatch-yellow hatch-label border-line-strong" onclick={() => void ctrl.resolveApproval(ap.id, 'approve_always')}>本会话同类放行</button>
                <button type="button" class="wobble btn btn-sm btn-danger-ghost" onclick={() => void ctrl.resolveApproval(ap.id, 'deny')}>拒绝</button>
              </div>
            </div>
          {/each}
        </div>
      {/if}

      <div class="composer-area">
        <div class="composer-box border-line-strong">
          {#if isRunning}
            <span class="composer-scan" aria-hidden="true"><span class="composer-scan-bar"></span></span>
          {/if}
          <textarea
            bind:this={composerEl}
            class="composer-input"
            bind:value={promptText}
            placeholder={currentProviderReady ? '输入任务或指令' : '输入任务或指令（先登录 daumini 或配置密钥）'}
            onkeydown={(e) => {
              if (e.key === 'Enter' && !e.shiftKey) {
                e.preventDefault();
                void handleSend('queue');
              }
            }}
          ></textarea>
          <div class="composer-bar">
            <div class="composer-stats">
              {#if ctrl.current}
                <span class="marked">缓存 {cachePct(ctrl.current.meta.total_usage)}</span>
                <span>输入 {fmtNum(totalIn(ctrl.current.meta.total_usage))}</span>
                <span>输出 {fmtNum(ctrl.current.meta.total_usage.output_tokens)}</span>
              {/if}
            </div>
            <div style="display: flex; align-items: center; gap: 8px;">
              <span class="kbd">Enter</span>
              {#if isRunning}
                <button type="button" class="wobble btn btn-sm btn-quiet border-line" onclick={() => void handleSend('steer')}>插队</button>
                <button type="button" class="wobble btn btn-sm btn-danger hatch-red hatch-label border-line-strong" onclick={() => void ctrl.cancelRun()}>
                  <svg viewBox="0 0 24 24" fill="currentColor"><rect x="6" y="6" width="12" height="12" rx="2"/></svg>
                  <span>停止</span>
                </button>
              {:else}
                <button type="button" class="wobble btn btn-sm btn-primary hatch-blue hatch-label border-line-strong" disabled={!promptText.trim()} onclick={() => void handleSend('queue')}>
                  <span>发送</span>
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="m5 12 7-7 7 7"/><path d="M12 19V5"/></svg>
                </button>
              {/if}
            </div>
          </div>
        </div>
      </div>

    {:else if tab === 'providers'}
      <div class="page-scroll">
        <div class="page-container">
          <div style="display: flex; align-items: center; justify-content: space-between; gap: 12px;">
            <h1 class="page-title scribble">模型与渠道</h1>
            <button
              type="button"
              class="wobble btn btn-sm btn-primary hatch-blue hatch-label border-line-strong"
              onclick={() => (dauminiConnected ? openLoginModal('oauth') : void triggerOneClickOAuth())}
            >
              {dauminiConnected ? '管理 daumini 账号' : '一键登录 daumini'}
            </button>
          </div>

          <div class="card-section border-line">
            <div style="display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 8px;">
              <span style="font-size: var(--text-lg);">已配置渠道</span>
              <span style="font-size: var(--text-sm); color: var(--fg-3);">支持 daumini DeepSeek OpenAI Anthropic 及自定义兼容端点</span>
            </div>
            {#each ctrl.config?.providers ?? [] as p, idx (p.id)}
              <div style="padding-top: 14px; border-top: 1px solid var(--line); display: flex; flex-direction: column; gap: 10px;">
                <div style="display: flex; justify-content: space-between; align-items: center; gap: 8px;">
                  <div style="display: flex; align-items: center; gap: 8px;">
                    <span style="font-size: var(--text-base);">{p.name}</span>
                    <span class="tag">{p.protocol}</span>
                    {#if p.api_key}
                      <span class="tag ok">已配置</span>
                    {/if}
                  </div>
                  <div style="display: flex; gap: 6px;">
                    {#if p.id === 'daumini'}
                      <button type="button" class="wobble btn btn-sm btn-ghost" onclick={() => void triggerOneClickOAuth()}>
                        {p.api_key ? '重新授权' : '一键授权登录'}
                      </button>
                    {/if}
                    <button type="button" class="wobble btn btn-sm btn-ghost" onclick={() => void ctrl.fetchProviderModels(p.id)}>拉取模型</button>
                  </div>
                </div>
                <div class="form-grid">
                  <div class="field-col">
                    <span class="field-label">Base URL</span>
                    <input class="field border-line" value={p.base_url} oninput={(e) => updateProviderField(idx, { base_url: (e.target as HTMLInputElement).value })} />
                  </div>
                  <div class="field-col">
                    <span class="field-label">API Key</span>
                    <input class="field border-line" type="password" value={p.api_key || ''} placeholder={p.api_key_env ? `环境变量 ${p.api_key_env}` : '填入密钥'} oninput={(e) => updateProviderField(idx, { api_key: (e.target as HTMLInputElement).value })} />
                  </div>
                  <div class="field-col">
                    <span class="field-label">默认模型</span>
                    <input class="field border-line" value={p.default_model} oninput={(e) => updateProviderField(idx, { default_model: (e.target as HTMLInputElement).value })} />
                  </div>
                </div>
                {#if ctrl.providerModels[p.id]?.length}
                  <div style="display: flex; flex-wrap: wrap; gap: 6px;">
                    {#each ctrl.providerModels[p.id].slice(0, 18) as mid}
                      <button
                        type="button"
                        class="wobble tag"
                        style="cursor: pointer;"
                        onclick={() => updateProviderField(idx, { default_model: mid })}
                      >
                        {mid}
                      </button>
                    {/each}
                  </div>
                {/if}
              </div>
            {/each}
            <div>
              <button type="button" class="wobble btn btn-primary hatch-blue hatch-label border-line-strong" onclick={() => void saveProviders()}>保存渠道配置</button>
            </div>
          </div>

          <div class="card-section border-line">
            <span style="font-size: var(--text-lg);">添加自定义 OpenAI 兼容渠道</span>
            <div class="form-grid">
              <input class="field border-line" bind:value={newProvName} placeholder="名称" />
              <input class="field border-line" bind:value={newProvUrl} placeholder="Base URL" />
              <input class="field border-line" type="password" bind:value={newProvKey} placeholder="API Key" />
              <input class="field border-line" bind:value={newProvModel} placeholder="默认模型 ID" />
            </div>
            <div>
              <button type="button" class="wobble btn btn-quiet border-line" onclick={() => void addCustomProvider()}>添加并设为默认</button>
            </div>
          </div>
        </div>
      </div>

    {:else}
      <div class="page-scroll">
        <div class="page-container">
          <h1 class="page-title scribble">扩展与诊断</h1>

          <div class="card-section border-line">
            <div style="display: flex; justify-content: space-between; align-items: center;">
              <span style="font-size: var(--text-lg);">MCP 服务</span>
              <button type="button" class="wobble btn btn-sm btn-ghost" onclick={() => void ctrl.reloadMCP()}>重载</button>
            </div>
            <div class="form-grid">
              <input class="field border-line" bind:value={mcpName} placeholder="服务名称" />
              <select class="field border-line" bind:value={mcpTransport}>
                <option value="stdio">Stdio 本地进程</option>
                <option value="http">Streamable HTTP</option>
              </select>
              {#if mcpTransport === 'stdio'}
                <input class="field border-line" bind:value={mcpCommand} placeholder="可执行命令" />
                <input class="field border-line" bind:value={mcpArgs} placeholder="参数 空格分隔" />
              {:else}
                <input class="field border-line" bind:value={mcpURL} placeholder="MCP URL" />
              {/if}
            </div>
            <div>
              <button type="button" class="wobble btn btn-primary hatch-blue hatch-label border-line-strong" onclick={() => void addMCP()}>添加服务</button>
            </div>
            {#each ctrl.config?.mcp_servers ?? [] as ms, idx}
              <div style="display: flex; justify-content: space-between; align-items: center; padding: 9px 12px; border-radius: 8px; background: var(--sunken);">
                <div>
                  <span>{ms.name}</span>
                  <span class="tag" style="margin-left: 6px;">{ms.transport}</span>
                  <span style="font-size: var(--text-xs); color: var(--fg-3); margin-left: 8px;">{ms.command || ms.url}</span>
                </div>
                <button type="button" class="wobble btn btn-sm btn-danger-ghost" onclick={() => void removeMCP(idx)}>移除</button>
              </div>
            {/each}
            {#each ctrl.mcpServers as st}
              <div style="font-size: var(--text-sm); color: var(--fg-2);">
                {st.name} {st.connected ? `已连接 ${st.tools?.length ?? 0} 个工具` : `连接失败 ${st.error}`}
              </div>
            {/each}
          </div>

          <div class="card-section border-line">
            <span style="font-size: var(--text-lg);">技能 ({ctrl.skills.length})</span>
            {#if ctrl.skills.length === 0}
              <span style="font-size: var(--text-sm); color: var(--fg-3);">暂无已加载技能</span>
            {:else}
              {#each ctrl.skills as sk}
                <div style="font-size: var(--text-sm);">
                  <span>{sk.name}</span> <span class="tag">{sk.scope}</span>
                  <span style="color: var(--fg-2); margin-left: 6px;">{sk.description}</span>
                </div>
              {/each}
            {/if}
          </div>

          <div class="card-section border-line">
            <span style="font-size: var(--text-lg);">工具 ({ctrl.tools.length})</span>
            <div style="display: flex; flex-wrap: wrap; gap: 6px;">
              {#each ctrl.tools as tl}
                <span class="tag">{tl.name}</span>
              {/each}
            </div>
          </div>

          <div class="card-section border-line">
            <div style="display: flex; justify-content: space-between; align-items: center;">
              <span style="font-size: var(--text-lg);">诊断日志</span>
              <button type="button" class="wobble btn btn-sm btn-ghost" onclick={() => void ctrl.loadLogs()}>刷新</button>
            </div>
            <pre class="code-surface">{ctrl.logs.map((l) => `${l.ts} [${l.level}] ${l.component} ${l.msg}`).join('\n') || '暂无日志'}</pre>
          </div>
        </div>
      </div>
    {/if}
  </main>

  {#if tab === 'work' && showInspector && ctrl.current}
    <aside class="inspector">
      <div class="inspector-section">
        <div class="inspector-head">
          <span>缓存与用量</span>
          <span class="tnum marked" style="font-size: var(--text-xl); color: var(--fg);">{cachePct(ctrl.current.meta.total_usage)}</span>
        </div>
        <div class="stat-grid border-line">
          <div class="stat-cell">
            <span class="stat-label">总输入</span>
            <span class="stat-val">{fmtNum(totalIn(ctrl.current.meta.total_usage))}</span>
          </div>
          <div class="stat-cell">
            <span class="stat-label">缓存读取</span>
            <span class="stat-val">{fmtNum(ctrl.current.meta.total_usage.cache_read_tokens)}</span>
          </div>
          <div class="stat-cell">
            <span class="stat-label">缓存写入</span>
            <span class="stat-val">{fmtNum(ctrl.current.meta.total_usage.cache_write_tokens)}</span>
          </div>
          <div class="stat-cell">
            <span class="stat-label">总输出</span>
            <span class="stat-val">{fmtNum(ctrl.current.meta.total_usage.output_tokens)}</span>
          </div>
        </div>
      </div>

      <div class="inspector-section-grow">
        <div class="inspector-head">
          <span>工作区文件</span>
          <button
            type="button"
            class="wobble btn btn-icon-sm btn-ghost"
            title="刷新文件列表"
            aria-label="刷新文件列表"
            onclick={() => ctrl.current && void ctrl.loadWorkspaceFiles(ctrl.current.meta.id)}
          >
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M21 12a9 9 0 0 0-9-9 9.75 9.75 0 0 0-6.74 2.74L3 8"/><path d="M3 3v5h5"/><path d="M3 12a9 9 0 0 0 9 9 9.75 9.75 0 0 0 6.74-2.74L21 16"/><path d="M16 16h5v5"/></svg>
          </button>
        </div>

        <div class="inspector-files">
          {#each ctrl.files as f}
            <button type="button" class="file-row" onclick={() => void ctrl.readFile(f)}>
              {#if f.endsWith('/')}
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M20 20a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.9a2 2 0 0 1-1.69-.9L9.6 3.9A2 2 0 0 0 7.93 3H4a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2Z"/></svg>
              {:else}
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M15 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7Z"/><path d="M14 2v4a2 2 0 0 0 2 2h4"/></svg>
              {/if}
              <span>{f}</span>
            </button>
          {/each}
        </div>

        {#if ctrl.filePreview}
          <div class="border-line" style="padding: 10px; border-radius: 10px; background: var(--card); display: flex; flex-direction: column; gap: 6px; animation: pop-in 0.22s var(--ease-out);">
            <div style="display: flex; justify-content: space-between; align-items: center; gap: 6px;">
              <span style="font-family: var(--font-mono); font-size: var(--text-xs); overflow: hidden; text-overflow: ellipsis; white-space: nowrap;">{ctrl.filePreview.path}</span>
              <button type="button" class="wobble btn btn-xs btn-ghost" onclick={() => (ctrl.filePreview = null)}>关闭</button>
            </div>
            <pre class="code-surface" style="max-height: 210px;">{ctrl.filePreview.content}</pre>
          </div>
        {/if}
      </div>
    </aside>
  {/if}
</div>

{#if showLoginModal}
  <div
    class="modal-backdrop"
    role="presentation"
    onclick={(e) => { if (e.target === e.currentTarget) showLoginModal = false; }}
  >
    <div class="modal-card sketch" role="dialog" aria-modal="true" aria-label="登录 daumini 账号">
      <div class="modal-head">
        <div style="display: flex; align-items: center; gap: 8px;">
          <span style="font-size: 20px;"><Wordmark live={dmBusy} suffix="" /></span>
          <span style="font-size: var(--text-base); color: var(--fg-2);">账号接入</span>
        </div>
        <button type="button" class="wobble btn btn-icon-sm btn-ghost" aria-label="关闭" onclick={() => (showLoginModal = false)}>
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M18 6 6 18M6 6l12 12"/></svg>
        </button>
      </div>

      <Segmented
        label="登录方式"
        full={true}
        value={loginMode}
        options={[
          { value: 'oauth', label: '一键授权' },
          { value: 'account', label: '邮箱密码' },
          { value: 'key', label: '粘贴密钥' }
        ]}
        onchange={(v) => { loginMode = v; dmErr = ''; dmOk = ''; }}
      />

      {#if loginMode === 'oauth'}
        <div style="display: flex; flex-direction: column; gap: 12px;">
          <div style="font-size: var(--text-sm); color: var(--fg-2); line-height: 1.65;">
            跳转至 <span class="marked">{dmBase}</span> 确认授权，复用浏览器已有登录态并自动签发专属密钥
          </div>
          {#if dmShowBase}
            <div class="field-col">
              <span class="field-label">中转地址</span>
              <input class="field border-line" bind:value={dmBase} placeholder="https://mini.daum.pw" />
            </div>
          {:else}
            <div style="display: flex; justify-content: space-between; align-items: center; font-size: var(--text-xs); color: var(--fg-3);">
              <span>中转 {dmBase}</span>
              <button type="button" class="btn btn-xs btn-ghost" onclick={() => (dmShowBase = true)}>修改地址</button>
            </div>
          {/if}
          <button
            type="button"
            class="wobble btn btn-primary hatch-blue hatch-label border-line-strong"
            style="width: 100%;"
            disabled={dmBusy}
            onclick={() => void triggerOneClickOAuth()}
          >
            {dmBusy ? '正在跳转授权' : '前往 daumini 一键授权'}
          </button>
          {#if dauminiConnected}
            <button
              type="button"
              class="wobble btn btn-sm btn-danger-ghost"
              style="width: 100%;"
              onclick={async () => {
                await ctrl.logoutDaumini();
                showToast('已退出当前 daumini 账号');
                showLoginModal = false;
              }}
            >
              退出当前账号
            </button>
          {/if}
        </div>
      {:else if loginMode === 'account'}
        <form
          style="display: flex; flex-direction: column; gap: 12px;"
          onsubmit={(e) => { e.preventDefault(); void doDauminiLogin(); }}
        >
          <div class="field-col">
            <span class="field-label">邮箱</span>
            <input class="field border-line" type="email" autocomplete="username" bind:value={dmEmail} placeholder="you@example.com" required />
          </div>
          <div class="field-col">
            <span class="field-label">密码</span>
            <input class="field border-line" type="password" autocomplete="current-password" bind:value={dmPass} placeholder="输入 daumini 密码" required />
          </div>
          {#if dmShowInvite}
            <div class="field-col">
              <span class="field-label">内测邀请码</span>
              <input class="field border-line" bind:value={dmInvite} placeholder="输入邀请码" />
            </div>
          {/if}
          {#if dmErr}
            <div class="notice notice-error border-line">{dmErr}</div>
          {/if}
          {#if dmOk}
            <div class="notice notice-ok border-line">{dmOk}</div>
          {/if}
          <button type="submit" class="wobble btn btn-primary hatch-blue hatch-label border-line-strong" style="width: 100%;" disabled={dmBusy}>
            {dmBusy ? '正在登录并签发密钥' : '登录并签发密钥'}
          </button>
        </form>
      {:else}
        <form
          style="display: flex; flex-direction: column; gap: 12px;"
          onsubmit={(e) => { e.preventDefault(); void doDauminiKeySave(); }}
        >
          <div style="font-size: var(--text-sm); color: var(--fg-2); line-height: 1.65;">
            前往 <a href="{dmBase}/console/keys" target="_blank" rel="noreferrer">{dmBase}/console/keys</a> 复制推理密钥粘贴即可
          </div>
          <div class="field-col">
            <span class="field-label">API 密钥</span>
            <input class="field border-line" type="password" bind:value={dmKey} placeholder="sk-dm-..." required />
          </div>
          {#if dmErr}
            <div class="notice notice-error border-line">{dmErr}</div>
          {/if}
          {#if dmOk}
            <div class="notice notice-ok border-line">{dmOk}</div>
          {/if}
          <button type="submit" class="wobble btn btn-primary hatch-blue hatch-label border-line-strong" style="width: 100%;" disabled={dmBusy}>
            {dmBusy ? '正在保存' : '保存并接入'}
          </button>
        </form>
      {/if}
    </div>
  </div>
{/if}

{#if toastText}
  <div class="toast border-line-strong" role="status">{toastText}</div>
{/if}
