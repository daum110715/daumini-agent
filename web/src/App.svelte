<script lang="ts">
  import { onMount, tick } from 'svelte';
  import { flip } from 'svelte/animate';
  import { cubicOut } from 'svelte/easing';
  import { fade } from 'svelte/transition';
  import { AgentController } from './lib/store.svelte';
  import { renderMarkdown } from './lib/markdown';
  import { dialogIn, drawerIn, drawerOut, pop, popOut, turnIn } from './lib/motion';
  import Collapsible from './lib/components/Collapsible.svelte';
  import DauminiModelCatalog from './lib/components/DauminiModelCatalog.svelte';
  import GenerationInspectorModal from './lib/components/GenerationInspectorModal.svelte';
  import ModelPicker from './lib/components/ModelPicker.svelte';
  import Picker from './lib/components/Picker.svelte';
  import Segmented from './lib/components/Segmented.svelte';
  import ThemePopover from './lib/components/ThemePopover.svelte';
  import Wordmark from './lib/components/Wordmark.svelte';
  import WorkspacePickerModal from './lib/components/WorkspacePickerModal.svelte';
  import type { AgentConfig, ApprovalMode, Block, MCPServerConfig, ModelInfo, ProviderConfig, Run, Usage } from './lib/types';

  const ctrl = new AgentController();

  type MainTab = 'work' | 'providers' | 'extensions';
  let tab = $state<MainTab>('work');
  let showInspector = $state(true);
  let promptText = $state('');
  let toastText = $state('');
  let toastTimer: ReturnType<typeof setTimeout> | null = null;

  // Modals & Inspectors
  let workspaceModal = $state<{ open: boolean; mode: 'create' | 'switch' }>({ open: false, mode: 'create' });
  let inspectGenerationId = $state<string | null>(null);

  // Session inline rename & delete confirm state
  let renamingId = $state<string | null>(null);
  let renameDraft = $state('');
  let deletingId = $state<string | null>(null);

  // One-click daumini login modal state
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
  let userScrolledUp = $state(false);
  let fileFilter = $state('');
  let slashIndex = $state(0);
  let mentionIndex = $state(0);
  let promptHistory = $state<string[]>([]);
  let historyCursor = $state(-1);

  // Gliding marker for sidebar navigation
  const navItems: { id: MainTab; label: string }[] = [
    { id: 'work', label: '工作台' },
    { id: 'providers', label: '模型' },
    { id: 'extensions', label: '扩展' }
  ];
  let navButtons = $state<HTMLButtonElement[]>([]);
  let navMarker = $state({ y: 0, h: 0, glide: false });
  let navPlaced = false;

  // Gliding marker for active session in sidebar
  let sessionEls = $state<Record<string, HTMLDivElement>>({});
  let sessionMarker = $state({ y: 0, h: 0, glide: false });
  let sessionPlaced = false;

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

  $effect(() => {
    const activeId = ctrl.current?.meta.id;
    const count = ctrl.sessions.length;
    void count;
    void tick().then(() => {
      const el = activeId ? sessionEls[activeId] : undefined;
      if (!el) {
        sessionMarker = { y: 0, h: 0, glide: sessionPlaced };
        return;
      }
      sessionMarker = { y: el.offsetTop, h: el.offsetHeight, glide: sessionPlaced };
      if (!sessionPlaced) {
        sessionPlaced = true;
        requestAnimationFrame(() => {
          sessionMarker = { ...sessionMarker, glide: true };
        });
      }
    });
  });

  // Auto-grow composer textarea up to 200px
  $effect(() => {
    void promptText;
    if (!composerEl) return;
    composerEl.style.height = 'auto';
    composerEl.style.height = `${Math.min(Math.max(composerEl.scrollHeight, 36), 200)}px`;
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
        if (inspectGenerationId) {
          inspectGenerationId = null;
          return;
        }
        if (workspaceModal.open) {
          workspaceModal = { ...workspaceModal, open: false };
          return;
        }
        if (showLoginModal) {
          showLoginModal = false;
          return;
        }
        if (renamingId || deletingId) {
          renamingId = null;
          deletingId = null;
          return;
        }
        if (ctrl.filePreview) {
          ctrl.filePreview = null;
          return;
        }
      }
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault();
        tab = 'work';
        void tick().then(() => composerEl?.focus());
      }
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'n') {
        e.preventDefault();
        workspaceModal = { open: true, mode: 'create' };
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
  const currentCatalog = $derived<ModelInfo[]>(
    ctrl.current ? (ctrl.providerCatalog[ctrl.current.meta.provider_id] ?? []) : []
  );
  const currentFallbackModels = $derived<string[]>(
    currentProvider?.models ?? (currentProvider?.default_model ? [currentProvider.default_model] : [])
  );
  const activeModelInfo = $derived<ModelInfo | undefined>(
    ctrl.current ? currentCatalog.find((m) => m.id === ctrl.current?.meta.model_id) : undefined
  );
  const providerOptions = $derived(
    (ctrl.config?.providers ?? []).map((p) => ({
      value: p.id,
      label: p.name,
      sub: p.protocol === 'daumini' ? (p.user_email || p.base_url) : p.base_url,
      badge: p.api_key ? '已连' : undefined
    }))
  );
  const reasoningOptions = $derived.by(() => {
    const levels = activeModelInfo?.reasoning_levels?.length
      ? activeModelInfo.reasoning_levels
      : activeModelInfo?.supports_thinking
        ? ['low', 'medium', 'high']
        : [];
    if (levels.length === 0) return [];
    const labelMap: Record<string, string> = {
      low: '思考 低',
      medium: '思考 中',
      high: '思考 高',
      max: '思考 极高'
    };
    return [
      { value: '', label: '思考 默认', sub: '跟随模型默认' },
      { value: 'off', label: '思考 关闭', sub: '跳过推理过程' },
      ...levels.map((lv) => ({ value: lv, label: labelMap[lv] || `思考 ${lv}`, sub: `reasoning_effort: ${lv}` }))
    ];
  });
  const filteredFiles = $derived.by(() => {
    const q = fileFilter.trim().toLowerCase();
    if (!q) return ctrl.files;
    return ctrl.files.filter((f) => f.toLowerCase().includes(q));
  });

  // @ file mention detection at the end of promptText
  const mentionQuery = $derived.by(() => {
    const m = promptText.match(/(?:^|\s)@([^\s@]*)$/);
    return m ? m[1] : null;
  });

  const mentionMatches = $derived.by(() => {
    if (mentionQuery === null) return [];
    const q = mentionQuery.toLowerCase();
    const onlyFiles = ctrl.files.filter((f) => !f.endsWith('/'));
    const hits = q ? onlyFiles.filter((f) => f.toLowerCase().includes(q)) : onlyFiles;
    return hits.slice(0, 10);
  });

  interface SlashCommand {
    cmd: string;
    label: string;
    desc: string;
    run: () => void;
  }

  const allSlashCommands: SlashCommand[] = [
    { cmd: '/new', label: '/new', desc: '在当前目录新建会话', run: () => void handleQuickNewSession() },
    { cmd: '/workspace', label: '/workspace', desc: '图形化选择工作区目录', run: () => (workspaceModal = { open: true, mode: 'switch' }) },
    { cmd: '/models', label: '/models', desc: '查看在线模型与路由定价', run: () => switchTab('providers') },
    { cmd: '/style', label: '/style', desc: '切换静谧工作台与手绘白板主题', run: () => withTransition(() => ctrl.setUIStyle(ctrl.uiStyle === 'atelier' ? 'sketch' : 'atelier')) },
    { cmd: '/theme', label: '/theme', desc: '切换浅色深色模式', run: () => withTransition(() => ctrl.toggleTheme()) },
    { cmd: '/fork', label: '/fork', desc: '分叉当前会话', run: () => void ctrl.forkSession(0) },
    { cmd: '/login', label: '/login', desc: '一键授权登录 daumini', run: () => void triggerOneClickOAuth() },
    { cmd: '/extensions', label: '/extensions', desc: '查看 MCP 技能与诊断', run: () => switchTab('extensions') }
  ];

  const slashMatches = $derived.by(() => {
    if (!promptText.startsWith('/') || promptText.includes('\n') || promptText.includes(' ')) return [];
    const q = promptText.toLowerCase();
    return allSlashCommands.filter((c) => c.cmd.startsWith(q));
  });

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

  const sessionGenerations = $derived.by(() => {
    const list: { id: string; model: string; usage: Usage }[] = [];
    for (const m of ctrl.current?.messages ?? []) {
      if (m.role === 'assistant' && m.provider_id === 'daumini' && m.usage?.generation_id?.startsWith('gen_')) {
        list.push({ id: m.usage.generation_id, model: m.model_id || '', usage: m.usage });
      }
    }
    return list.reverse().slice(0, 6);
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

  function switchTab(next: MainTab) {
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
    void ctrl.loadDauminiStatus().catch(() => {});
    void ctrl.loadDauminiLedger().catch(() => {});
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

  function cacheRatio(u?: Usage | null): number {
    if (!u) return 0;
    const total = (u.input_tokens || 0) + (u.cache_read_tokens || 0) + (u.cache_write_tokens || 0);
    if (total <= 0) return 0;
    return Math.min(100, Math.round(((u.cache_read_tokens || 0) / total) * 100));
  }

  function cachePct(u?: Usage | null): string {
    return `${cacheRatio(u)}%`;
  }

  function speakerName(modelId?: string): string {
    const id = modelId || ctrl.current?.meta.model_id || '';
    const info = currentCatalog.find((m) => m.id === id);
    return info?.name || id;
  }

  function toolFilePath(name?: string, input?: unknown): string {
    if (!input || typeof input !== 'object') return '';
    if (name !== 'fs_read' && name !== 'fs_edit' && name !== 'fs_write') return '';
    const p = (input as Record<string, unknown>).path;
    return typeof p === 'string' ? p : '';
  }

  function toolSummary(name?: string, input?: unknown): string {
    if (!input || typeof input !== 'object') return '';
    const obj = input as Record<string, unknown>;
    if (typeof obj.path === 'string' && obj.path) {
      const sl = typeof obj.start_line === 'number' ? `:${obj.start_line}` : '';
      return `${obj.path}${sl}`;
    }
    if (typeof obj.command === 'string' && obj.command) {
      return obj.command.length > 52 ? `${obj.command.slice(0, 52)}...` : obj.command;
    }
    if (typeof obj.query === 'string' && obj.query) return obj.query;
    if (typeof obj.pattern === 'string' && obj.pattern) return obj.pattern;
    if (name === 'todo_update' && Array.isArray(obj.items)) return `${obj.items.length} 项任务`;
    return '';
  }

  function insertFileRef(filePath: string) {
    const ref = `\`${filePath}\``;
    promptText = promptText.trim() ? `${promptText.trimEnd()} ${ref} ` : `请查看 ${ref} `;
    composerEl?.focus();
    showToast(`已引用 ${filePath}`);
  }

  function applyMention(filePath: string) {
    promptText = promptText.replace(/(^|\s)@([^\s@]*)$/, `$1\`${filePath}\` `);
    mentionIndex = 0;
    void tick().then(() => composerEl?.focus());
  }

  function runSlash(cmd: SlashCommand) {
    promptText = '';
    slashIndex = 0;
    cmd.run();
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

  function scrollToBottom() {
    userScrolledUp = false;
    feedEl?.scrollTo({ top: feedEl.scrollHeight, behavior: 'smooth' });
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
    if (!overrideText) {
      promptText = '';
      if (promptHistory[promptHistory.length - 1] !== txt) {
        promptHistory = [...promptHistory.slice(-29), txt];
      }
      historyCursor = -1;
    }
    userScrolledUp = false;
    try {
      await ctrl.sendMessage(txt, mode);
    } catch (e) {
      ctrl.error = e instanceof Error ? e.message : String(e);
    }
  }

  async function handleQuickNewSession() {
    tab = 'work';
    const dir = ctrl.current?.meta.workspace_dir || '.';
    await ctrl.createSession(dir);
    showToast('已新建会话');
    void tick().then(() => composerEl?.focus());
  }

  async function handleWorkspaceConfirm(path: string, action: 'create' | 'switch') {
    workspaceModal = { ...workspaceModal, open: false };
    tab = 'work';
    try {
      if (action === 'switch' && ctrl.current) {
        await ctrl.updateSession({ workspace_dir: path });
        showToast('已切换工作区目录');
      } else {
        await ctrl.createSession(path);
        showToast('已在新工作区创建会话');
      }
      void tick().then(() => composerEl?.focus());
    } catch (e) {
      ctrl.error = e instanceof Error ? e.message : String(e);
    }
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

  async function handlePlatformSelectModel(modelId: string) {
    if (ctrl.current) {
      await ctrl.updateSession({ provider_id: 'daumini', model_id: modelId });
      showToast(`当前会话已切换至 ${modelId}`);
      switchTab('work');
    }
  }

  async function handlePlatformSetDefaultModel(modelId: string) {
    if (!ctrl.config) return;
    const next: AgentConfig = structuredClone($state.snapshot(ctrl.config));
    next.default_provider = 'daumini';
    next.default_model = modelId;
    const dm = next.providers.find((p) => p.id === 'daumini');
    if (dm) dm.default_model = modelId;
    await ctrl.saveConfig(next);
    showToast(`默认模型已设为 ${modelId}`);
  }
</script>

<div class="shell {tab === 'work' && showInspector ? 'with-inspector' : ''}">
  <aside class="sidebar">
    <div class="brand-row">
      <span style="font-size: 19px;">
        <Wordmark live={isRunning} />
      </span>
      <ThemePopover
        uiStyle={ctrl.uiStyle}
        theme={ctrl.theme}
        onSelectStyle={(st) => withTransition(() => ctrl.setUIStyle(st))}
        onSelectTheme={(th) => withTransition(() => ctrl.setTheme(th))}
      />
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
        style="height: 30px; width: 30px;"
        title="选择工作区目录"
        aria-label="选择工作区目录"
        onclick={() => (workspaceModal = { open: true, mode: 'create' })}
      >
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M20 20a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.9a2 2 0 0 1-1.69-.9L9.6 3.9A2 2 0 0 0 7.93 3H4a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2Z"/></svg>
      </button>
    </div>

    <div class="side-section-label">会话 ({ctrl.sessions.length})</div>
    <div class="session-list">
      <span
        aria-hidden="true"
        class="session-marker border-line {sessionMarker.glide ? 'glide' : ''}"
        style:translate="0 {sessionMarker.y}px"
        style:height="{sessionMarker.h}px"
        style:opacity={sessionMarker.h ? 1 : 0}
      ></span>
      {#each ctrl.sessions as s (s.id)}
        {@const isActive = ctrl.current?.meta.id === s.id}
        <div
          bind:this={sessionEls[s.id]}
          animate:flip={{ duration: 240, easing: cubicOut }}
          in:turnIn={{ y: 5, duration: 190 }}
          out:popOut={{ y: -2, duration: 115 }}
          class="session-row {isActive ? 'active' : ''} {deletingId === s.id ? 'confirming' : ''}"
        >
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
          <button
            type="button"
            class="account-info"
            title="查看账号状态与累计用量"
            onclick={() => openLoginModal('oauth')}
          >
            <span class="status-dot" title="daumini 已连接"></span>
            <span class="account-email" title={dauminiProvider?.user_email || 'daumini 已连接'}>
              {dauminiProvider?.user_email || 'daumini 已连接'}
            </span>
          </button>
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
      <div style="max-width: 780px; width: 100%; margin: 10px auto 0; padding: 0 20px;">
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
            <button
              type="button"
              class="topbar-path-btn"
              title={`切换工作区 ${ctrl.current.meta.workspace_dir}`}
              onclick={() => (workspaceModal = { open: true, mode: 'switch' })}
            >
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M20 20a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.9a2 2 0 0 1-1.69-.9L9.6 3.9A2 2 0 0 0 7.93 3H4a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2Z"/></svg>
              <span>{ctrl.current.meta.workspace_dir}</span>
            </button>
            {#if ctrl.current.meta.parent_session_id}
              <span class="tag mark">分叉</span>
            {/if}
          {/if}
        </div>

        {#if ctrl.current}
          <div class="topbar-actions">
            <Picker
              ariaLabel="切换渠道"
              value={ctrl.current.meta.provider_id}
              options={providerOptions}
              onChange={(pid) => {
                const prov = ctrl.config?.providers.find((p) => p.id === pid);
                const cachedModels = ctrl.providerCatalog[pid] ?? [];
                const nextModel = prov?.default_model || cachedModels[0]?.id || ctrl.current?.meta.model_id || '';
                void ctrl.updateSession({ provider_id: pid, model_id: nextModel, reasoning_effort: '' });
                if (pid === 'daumini' || prov?.api_key) {
                  void ctrl.fetchProviderModels(pid).catch(() => {});
                }
              }}
            />

            <ModelPicker
              models={currentCatalog}
              fallbackIds={currentFallbackModels}
              value={ctrl.current.meta.model_id}
              loading={Boolean(ctrl.loadingModels[ctrl.current.meta.provider_id])}
              currency={ctrl.currency}
              cnyRate={ctrl.cnyRate}
              onChange={(mid) => void ctrl.updateSession({ model_id: mid })}
              onRefresh={() => ctrl.current && void ctrl.fetchProviderModels(ctrl.current.meta.provider_id).catch((e) => (ctrl.error = String(e)))}
            />

            {#if reasoningOptions.length > 0}
              <Picker
                compact
                ariaLabel="推理强度"
                value={ctrl.current.meta.reasoning_effort || ''}
                options={reasoningOptions}
                onChange={(eff) => void ctrl.updateSession({ reasoning_effort: eff })}
              />
            {/if}

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
              <span style="font-size: 30px;"><Wordmark live={false} suffix="" /></span>
              <div style="font-size: var(--text-lg); margin-top: 2px;" class="scribble">{ctrl.current.meta.title}</div>
              <button
                type="button"
                class="empty-ws-pill"
                onclick={() => (workspaceModal = { open: true, mode: 'switch' })}
              >
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M20 20a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.9a2 2 0 0 1-1.69-.9L9.6 3.9A2 2 0 0 0 7.93 3H4a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2Z"/></svg>
                <span>{ctrl.current.meta.workspace_dir}</span>
                <span class="empty-ws-act">切换目录</span>
              </button>

              {#if !currentProviderReady}
                <div style="margin-top: 14px; display: flex; flex-direction: column; align-items: center; gap: 10px;">
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
                    <div class="assistant-speaker">{speakerName(msg.model_id)}</div>
                    {#each msg.blocks ?? [] as b}
                      {#if b.type === 'thinking' && b.thinking}
                        <Collapsible variant="think" title="思考过程" badge={`${b.thinking.length} 字`}>
                          <div class="col-body-think">{b.thinking}</div>
                        </Collapsible>
                      {:else if b.type === 'text' && b.text}
                        <div class="md-prose">{@html renderMarkdown(b.text)}</div>
                      {:else if b.type === 'tool_use'}
                        {@const res = b.id ? toolResultsMap[b.id] : undefined}
                        {@const fp = toolFilePath(b.name, b.input)}
                        <Collapsible
                          variant="tool"
                          title={b.name || 'tool'}
                          subtitle={toolSummary(b.name, b.input)}
                          badge={res ? (res.is_error ? '报错' : '完成') : '执行中'}
                          badgeTone={res ? (res.is_error ? 'bad' : 'ok') : 'warn'}
                          streaming={!res}
                        >
                          <div class="col-body-tool">
                            <div style="display: flex; align-items: center; justify-content: space-between; gap: 8px;">
                              <div class="col-sec-label">参数</div>
                              {#if fp}
                                <button
                                  type="button"
                                  class="usage-link-btn"
                                  onclick={() => {
                                    showInspector = true;
                                    void ctrl.readFile(fp);
                                  }}
                                >
                                  在工作区预览 {fp}
                                </button>
                              {/if}
                            </div>
                            <pre class="code-surface">{fmtJSON(b.input)}</pre>
                            {#if res}
                              <div class="col-sec-label">{res.is_error ? '错误输出' : '返回结果'}</div>
                              <pre class="code-surface">{res.text}</pre>
                            {/if}
                          </div>
                        </Collapsible>
                      {/if}
                    {/each}
                    {#if msg.usage || plainText}
                      <div class="usage-line">
                        {#if msg.usage}
                          <span class="marked">缓存 {cachePct(msg.usage)}</span>
                          <span>输入 {fmtNum(totalIn(msg.usage))}</span>
                          <span>输出 {fmtNum(msg.usage.output_tokens)}</span>
                          {#if msg.usage.total_cost_nusd}
                            <span class="usage-cost">{ctrl.formatNUSD(msg.usage.total_cost_nusd, 4)}</span>
                          {/if}
                          {#if msg.usage.latency_ms}
                            <span>{(msg.usage.latency_ms / 1000).toFixed(1)}s</span>
                          {/if}
                          {#if msg.provider_id === 'daumini' && msg.usage.generation_id && msg.usage.generation_id.startsWith('gen_')}
                            <button
                              type="button"
                              class="usage-link-btn"
                              onclick={() => (inspectGenerationId = msg.usage?.generation_id || null)}
                            >
                              路由与费用
                            </button>
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
                  <div class="assistant-speaker">{speakerName(ctrl.current.meta.model_id)}</div>
                  {#if ctrl.activeStreamThinking}
                    <Collapsible open={true} variant="think" title="正在思考" badge={`${ctrl.activeStreamThinking.length} 字`} streaming={true}>
                      <div class="col-body-think">{ctrl.activeStreamThinking}</div>
                    </Collapsible>
                  {/if}
                  {#if ctrl.activeStreamText}
                    <div class="md-prose">{@html renderMarkdown(ctrl.activeStreamText)}<span class="caret" aria-hidden="true"></span></div>
                  {:else if !ctrl.activeStreamThinking && Object.keys(ctrl.activeTools).length === 0}
                    <div class="streaming-wait">
                      <span class="wait-dot"></span>
                      <span>正在等待模型响应</span>
                    </div>
                  {/if}
                  {#each Object.entries(ctrl.activeTools) as [tid, t] (tid)}
                    <Collapsible
                      open={!t.output}
                      variant="tool"
                      title={t.name}
                      subtitle={toolSummary(t.name, t.input)}
                      badge={t.output ? `${t.duration_ms ?? 0}ms` : '运行中'}
                      badgeTone={t.output ? (t.is_error ? 'bad' : 'ok') : 'accent'}
                      streaming={!t.output}
                    >
                      <div class="col-body-tool">
                        <div class="col-sec-label">参数</div>
                        <pre class="code-surface">{fmtJSON(t.input)}</pre>
                        {#if t.output}
                          <div class="col-sec-label">{t.is_error ? '错误输出' : '返回结果'}</div>
                          <pre class="code-surface">{t.output}</pre>
                        {/if}
                      </div>
                    </Collapsible>
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
        <div class="composer-wrap">
          {#if userScrolledUp}
            <button
              type="button"
              class="scroll-bottom-btn border-line"
              in:pop={{ y: 6, duration: 170 }}
              out:popOut={{ y: 4, duration: 110 }}
              onclick={scrollToBottom}
            >
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="m6 9 6 6 6-6"/></svg>
              <span>回到底部</span>
            </button>
          {/if}

          {#if slashMatches.length > 0}
            <div
              class="slash-menu"
              role="listbox"
              aria-label="快捷命令"
              in:pop={{ y: 6, start: 0.96, blur: 4, duration: 180 }}
              out:popOut={{ y: 4, end: 0.97, blur: 2, duration: 110 }}
            >
              {#each slashMatches as item, idx (item.cmd)}
                <button
                  type="button"
                  class="slash-item"
                  class:focused={idx === slashIndex}
                  onmouseenter={() => (slashIndex = idx)}
                  onclick={() => runSlash(item)}
                >
                  <code class="slash-cmd">{item.label}</code>
                  <span class="slash-desc">{item.desc}</span>
                </button>
              {/each}
            </div>
          {:else if mentionMatches.length > 0}
            <div
              class="slash-menu"
              role="listbox"
              aria-label="引用工作区文件"
              in:pop={{ y: 6, start: 0.96, blur: 4, duration: 170 }}
              out:popOut={{ y: 4, end: 0.97, blur: 2, duration: 105 }}
            >
              {#each mentionMatches as fp, idx (fp)}
                <button
                  type="button"
                  class="slash-item"
                  class:focused={idx === mentionIndex}
                  onmouseenter={() => (mentionIndex = idx)}
                  onclick={() => applyMention(fp)}
                >
                  <code class="slash-cmd">{fp}</code>
                  <span class="slash-desc">引用文件</span>
                </button>
              {/each}
            </div>
          {/if}

          <div class="composer-box border-line-strong">
            {#if isRunning}
              <span class="composer-scan" aria-hidden="true"><span class="composer-scan-bar"></span></span>
            {/if}
            <textarea
              bind:this={composerEl}
              class="composer-input"
              bind:value={promptText}
              placeholder={currentProviderReady ? '输入任务 键入 @ 引用文件 或 / 执行快捷命令' : '输入任务（先一键登录 daumini 或配置渠道密钥）'}
              oninput={() => {
                slashIndex = 0;
                mentionIndex = 0;
                historyCursor = -1;
              }}
              onkeydown={(e) => {
                if (slashMatches.length > 0) {
                  if (e.key === 'ArrowDown') {
                    e.preventDefault();
                    slashIndex = (slashIndex + 1) % slashMatches.length;
                    return;
                  }
                  if (e.key === 'ArrowUp') {
                    e.preventDefault();
                    slashIndex = (slashIndex - 1 + slashMatches.length) % slashMatches.length;
                    return;
                  }
                  if (e.key === 'Enter' || e.key === 'Tab') {
                    e.preventDefault();
                    runSlash(slashMatches[slashIndex] ?? slashMatches[0]);
                    return;
                  }
                  if (e.key === 'Escape') {
                    e.preventDefault();
                    promptText = '';
                    return;
                  }
                }
                if (mentionMatches.length > 0) {
                  if (e.key === 'ArrowDown') {
                    e.preventDefault();
                    mentionIndex = (mentionIndex + 1) % mentionMatches.length;
                    return;
                  }
                  if (e.key === 'ArrowUp') {
                    e.preventDefault();
                    mentionIndex = (mentionIndex - 1 + mentionMatches.length) % mentionMatches.length;
                    return;
                  }
                  if (e.key === 'Enter' || e.key === 'Tab') {
                    e.preventDefault();
                    applyMention(mentionMatches[mentionIndex] ?? mentionMatches[0]);
                    return;
                  }
                  if (e.key === 'Escape') {
                    e.preventDefault();
                    promptText = promptText.replace(/(^|\s)@([^\s@]*)$/, '$1');
                    return;
                  }
                }
                if (e.key === 'ArrowUp' && promptText === '' && promptHistory.length > 0) {
                  e.preventDefault();
                  const nextIdx = historyCursor < 0 ? promptHistory.length - 1 : Math.max(0, historyCursor - 1);
                  historyCursor = nextIdx;
                  promptText = promptHistory[nextIdx];
                  return;
                }
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
                  {#if ctrl.current.meta.total_usage.total_cost_nusd}
                    <button
                      type="button"
                      class="usage-link-btn"
                      title={sessionGenerations.length ? '查看最近一次路由与费用' : '切换美元或人民币'}
                      onclick={() => {
                        if (sessionGenerations.length > 0) {
                          inspectGenerationId = sessionGenerations[0].id;
                        } else {
                          ctrl.setCurrency(ctrl.currency === 'USD' ? 'CNY' : 'USD');
                        }
                      }}
                    >
                      {ctrl.formatNUSD(ctrl.current.meta.total_usage.total_cost_nusd, 4)}
                    </button>
                  {/if}
                {/if}
              </div>
              <div style="display: flex; align-items: center; gap: 6px;">
                <span class="kbd">@</span>
                <span class="kbd">/</span>
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
              <span style="font-size: var(--text-lg);">daumini 在线模型与路由定价</span>
              <span style="font-size: var(--text-xs); color: var(--fg-3);">缓存读价格优先 展开可查看上游路由端点</span>
            </div>
            <DauminiModelCatalog
              {ctrl}
              onSelectModel={(mid) => void handlePlatformSelectModel(mid)}
              onSetDefaultModel={(mid) => void handlePlatformSetDefaultModel(mid)}
            />
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
              <Picker
                ariaLabel="传输方式"
                value={mcpTransport}
                options={[
                  { value: 'stdio', label: 'Stdio 本地进程', sub: '标准输入输出子进程' },
                  { value: 'http', label: 'Streamable HTTP', sub: '远程或本地 HTTP 端点' }
                ]}
                onChange={(v) => (mcpTransport = v as 'stdio' | 'http')}
              />
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
    <aside
      class="inspector"
      in:drawerIn={{ x: 24 }}
      out:drawerOut={{ x: 18 }}
    >
      <div class="inspector-section">
        <div class="inspector-head">
          <span>缓存与用量</span>
          <div style="display: flex; align-items: center; gap: 6px;">
            <button
              type="button"
              class="usage-link-btn tnum"
              title="切换美元或人民币"
              onclick={() => ctrl.setCurrency(ctrl.currency === 'USD' ? 'CNY' : 'USD')}
            >
              {ctrl.currency === 'USD' ? '$' : '¥'}
            </button>
            <span class="tnum marked" style="font-size: var(--text-sm);">{cachePct(ctrl.current.meta.total_usage)}</span>
          </div>
        </div>
        <div class="cache-meter" aria-label="缓存命中率进度条">
          <span class="cache-meter-fill" style:width={`${cacheRatio(ctrl.current.meta.total_usage)}%`}></span>
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
            <span class="stat-label">总输出</span>
            <span class="stat-val">{fmtNum(ctrl.current.meta.total_usage.output_tokens)}</span>
          </div>
          <div class="stat-cell">
            <span class="stat-label">累计费用</span>
            <span class="stat-val">{ctrl.formatNUSD(ctrl.current.meta.total_usage.total_cost_nusd, 4)}</span>
          </div>
        </div>
        {#if activeModelInfo}
          <div class="model-meta-box">
            <div class="model-meta-name">{activeModelInfo.name || activeModelInfo.id}</div>
            <div class="model-meta-tags">
              {#if activeModelInfo.context_window}
                <span class="tag">{activeModelInfo.context_window >= 1_000_000 ? `${(activeModelInfo.context_window / 1_000_000).toFixed(0)}M` : `${Math.round(activeModelInfo.context_window / 1000)}K`} 上下文</span>
              {/if}
              {#if activeModelInfo.supports_thinking}
                <span class="tag ok">支持思考</span>
              {/if}
              {#if activeModelInfo.cache_read_nusd_per_million}
                <span class="tag mark">缓存 {ctrl.formatPricePerM(activeModelInfo.cache_read_nusd_per_million)}/M</span>
              {/if}
            </div>
            {#if activeModelInfo.input_nusd_per_million}
              <div class="model-meta-price">
                输入 {ctrl.formatPricePerM(activeModelInfo.input_nusd_per_million)} · 输出 {ctrl.formatPricePerM(activeModelInfo.output_nusd_per_million)} / M
              </div>
            {/if}
          </div>
        {/if}
        {#if sessionGenerations.length > 0}
          <div class="gen-mini-list">
            <div class="gen-mini-head">最近路由与费用</div>
            {#each sessionGenerations.slice(0, 4) as g (g.id)}
              <button
                type="button"
                class="gen-mini-row"
                onclick={() => (inspectGenerationId = g.id)}
              >
                <span class="gen-mini-hit tnum">{cachePct(g.usage)}</span>
                <span class="gen-mini-cost tnum">{ctrl.formatNUSD(g.usage.total_cost_nusd, 4)}</span>
                <span class="gen-mini-lat tnum">{g.usage.latency_ms ? `${(g.usage.latency_ms / 1000).toFixed(1)}s` : ''}</span>
                <span class="gen-mini-act">明细</span>
              </button>
            {/each}
          </div>
        {/if}
      </div>

      <div class="inspector-section-grow">
        <div class="inspector-head">
          <span>工作区文件 ({filteredFiles.length})</span>
          <div style="display: flex; align-items: center; gap: 2px;">
            <button
              type="button"
              class="wobble btn btn-icon-sm btn-ghost"
              title="切换工作区目录"
              aria-label="切换工作区目录"
              onclick={() => (workspaceModal = { open: true, mode: 'switch' })}
            >
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M20 20a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.9a2 2 0 0 1-1.69-.9L9.6 3.9A2 2 0 0 0 7.93 3H4a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2Z"/></svg>
            </button>
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
        </div>

        <input
          class="file-filter-input"
          bind:value={fileFilter}
          placeholder="筛选工作区文件"
          aria-label="筛选工作区文件"
        />

        <div class="inspector-files">
          {#each filteredFiles as f}
            <div class="file-row-wrap">
              <button
                type="button"
                class="file-row"
                class:active={ctrl.filePreview?.path === f}
                onclick={() => !f.endsWith('/') && void ctrl.readFile(f)}
              >
                {#if f.endsWith('/')}
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M20 20a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.9a2 2 0 0 1-1.69-.9L9.6 3.9A2 2 0 0 0 7.93 3H4a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2Z"/></svg>
                {:else}
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M15 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7Z"/><path d="M14 2v4a2 2 0 0 0 2 2h4"/></svg>
                {/if}
                <span>{f}</span>
              </button>
              {#if !f.endsWith('/')}
                <button
                  type="button"
                  class="file-ref-btn"
                  title="引用文件路径到输入框"
                  onclick={() => insertFileRef(f)}
                >
                  @
                </button>
              {/if}
            </div>
          {/each}
        </div>

        {#if ctrl.filePreview}
          <div
            class="file-preview border-line"
            in:pop={{ y: 6, start: 0.97, blur: 3, duration: 200 }}
            out:popOut={{ y: 4, duration: 120 }}
          >
            <div class="file-preview-head">
              <span class="file-preview-path" title={ctrl.filePreview.path}>{ctrl.filePreview.path}</span>
              <div style="display: flex; gap: 4px; flex-shrink: 0;">
                <button type="button" class="wobble btn btn-xs btn-ghost" onclick={() => ctrl.filePreview && insertFileRef(ctrl.filePreview.path)}>引用</button>
                <button type="button" class="wobble btn btn-xs btn-ghost" onclick={() => ctrl.filePreview && void copyText(ctrl.filePreview.content, '已复制文件内容')}>复制</button>
                <button type="button" class="wobble btn btn-xs btn-ghost" onclick={() => (ctrl.filePreview = null)}>关闭</button>
              </div>
            </div>
            <pre class="code-surface file-preview-code">{ctrl.filePreview.content}</pre>
          </div>
        {/if}
      </div>
    </aside>
  {/if}
</div>

{#if workspaceModal.open}
  <WorkspacePickerModal
    {ctrl}
    initialPath={ctrl.current?.meta.workspace_dir || ''}
    mode={workspaceModal.mode}
    onClose={() => (workspaceModal = { ...workspaceModal, open: false })}
    onConfirm={handleWorkspaceConfirm}
  />
{/if}

{#if inspectGenerationId}
  <GenerationInspectorModal
    {ctrl}
    generationId={inspectGenerationId}
    onClose={() => (inspectGenerationId = null)}
    onCopy={(txt, label) => void copyText(txt, label)}
  />
{/if}

{#if showLoginModal}
  <!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
  <div
    class="modal-backdrop"
    role="presentation"
    transition:fade={{ duration: 160 }}
    onclick={(e) => { if (e.target === e.currentTarget) showLoginModal = false; }}
  >
    <div
      class="modal-card sketch"
      role="dialog"
      aria-modal="true"
      aria-label="登录 daumini 账号"
      in:dialogIn
      out:popOut={{ y: 4, end: 0.96, blur: 3, duration: 140 }}
    >
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
          {#if dauminiConnected}
            <div class="acc-summary">
              <div class="acc-summary-top">
                <div style="display: flex; align-items: center; gap: 7px; min-width: 0;">
                  <span class="status-dot"></span>
                  <span class="acc-email">{dauminiProvider?.user_email || 'daumini 已授权'}</span>
                </div>
                <button
                  type="button"
                  class="btn btn-xs btn-quiet border-line"
                  onclick={() => ctrl.setCurrency(ctrl.currency === 'USD' ? 'CNY' : 'USD')}
                >
                  {ctrl.currency === 'USD' ? 'USD $' : 'CNY ¥'}
                </button>
              </div>
              {#if ctrl.dauminiStatus?.key?.workspace_id}
                <div class="acc-meta-line">工作空间 {ctrl.dauminiStatus.key.workspace_id}</div>
              {/if}
              {#if ctrl.dauminiLedger}
                <div class="acc-kpi-row tnum">
                  <div>
                    <span>全局缓存</span>
                    <strong class="ok">{Math.round(ctrl.dauminiLedger.cache_hit_ratio * 100)}%</strong>
                  </div>
                  <div>
                    <span>累计费用</span>
                    <strong>{ctrl.formatNUSD(ctrl.dauminiLedger.totals.total_cost_nusd, 4)}</strong>
                  </div>
                  <div>
                    <span>调用次数</span>
                    <strong>{ctrl.dauminiLedger.request_count}</strong>
                  </div>
                </div>
                {#if ctrl.dauminiLedger.recent.filter((r) => r.provider_id === 'daumini' && r.generation_id?.startsWith('gen_')).length > 0}
                  <div class="acc-recent">
                    {#each ctrl.dauminiLedger.recent.filter((r) => r.provider_id === 'daumini' && r.generation_id?.startsWith('gen_')).slice(0, 3) as item (item.message_id)}
                      <button
                        type="button"
                        class="gen-mini-row"
                        onclick={() => {
                          showLoginModal = false;
                          inspectGenerationId = item.generation_id || null;
                        }}
                      >
                        <span class="acc-recent-model">{item.model_id}</span>
                        <span class="gen-mini-hit tnum">{cachePct(item.usage)}</span>
                        <span class="gen-mini-cost tnum">{ctrl.formatNUSD(item.usage.total_cost_nusd, 4)}</span>
                        <span class="gen-mini-act">路由</span>
                      </button>
                    {/each}
                  </div>
                {/if}
              {/if}
            </div>
          {:else}
            <div style="font-size: var(--text-sm); color: var(--fg-2); line-height: 1.65;">
              跳转至 <span class="marked">{dmBase}</span> 确认授权 复用浏览器已有登录态并自动签发专属密钥
            </div>
          {/if}
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
            {dmBusy ? '正在跳转授权' : (dauminiConnected ? '重新授权或切换账号' : '前往 daumini 一键授权')}
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
            粘贴 daumini 推理密钥直接接入
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
  <div
    class="toast border-line-strong"
    role="status"
    in:pop={{ y: 10, start: 0.94, blur: 4, duration: 210 }}
    out:popOut={{ y: 6, end: 0.96, blur: 2, duration: 130 }}
  >{toastText}</div>
{/if}
