<script lang="ts">
  import { onMount } from 'svelte';
  import { AgentController } from './lib/store.svelte';
  import type { AgentConfig, ApprovalMode, Block, MCPServerConfig, ProviderConfig, Usage } from './lib/types';

  const ctrl = new AgentController();

  let tab = $state<'work' | 'providers' | 'extensions'>('work');
  let showInspector = $state(true);
  let promptText = $state('');
  let newWorkspace = $state('.');
  let showNewBox = $state(false);

  // daumini quick login form
  let dmBase = $state('https://mini.daum.pw');
  let dmEmail = $state('');
  let dmPass = $state('');
  let dmStatus = $state('');

  // new custom provider form
  let newProvName = $state('');
  let newProvUrl = $state('');
  let newProvKey = $state('');
  let newProvModel = $state('');

  // new MCP server form
  let mcpName = $state('');
  let mcpTransport = $state<'stdio' | 'http'>('stdio');
  let mcpCommand = $state('');
  let mcpArgs = $state('');
  let mcpURL = $state('');

  let feedEl = $state<HTMLDivElement | null>(null);

  onMount(() => {
    void ctrl.bootstrap();
  });

  const isRunning = $derived(Boolean(ctrl.current?.meta.active_run_id));
  const pendingApprovals = $derived(
    (ctrl.current?.pending_approvals ?? []).filter((a) => a.decision === 'pending')
  );

  // Pair tool_use blocks with matching tool_result blocks across subsequent user messages.
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

  async function handleSend(mode: 'queue' | 'steer' | 'interrupt' = 'queue') {
    const txt = promptText.trim();
    if (!txt) return;
    promptText = '';
    try {
      await ctrl.sendMessage(txt, mode);
    } catch (e) {
      ctrl.error = e instanceof Error ? e.message : String(e);
    }
  }

  async function handleCreateSession() {
    showNewBox = false;
    await ctrl.createSession(newWorkspace || '.');
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
  }

  async function doDauminiLogin() {
    dmStatus = '正在登录并签发密钥';
    try {
      await ctrl.loginDaumini(dmBase, dmEmail, dmPass);
      dmPass = '';
      dmStatus = '已绑定 daumini 中转密钥';
    } catch (e) {
      dmStatus = e instanceof Error ? e.message : String(e);
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
  }

  async function removeMCP(idx: number) {
    if (!ctrl.config) return;
    const next: AgentConfig = structuredClone($state.snapshot(ctrl.config));
    next.mcp_servers = (next.mcp_servers ?? []).filter((_, i) => i !== idx);
    await ctrl.saveConfig(next);
  }
</script>

<div class="shell {tab === 'work' && showInspector ? 'with-inspector' : ''}">
  <aside class="sidebar">
    <div class="brand-row">
      <span class="brand-title">daumini-agent</span>
      <button class="icon-btn" onclick={() => ctrl.toggleTheme()}>
        {ctrl.theme === 'light' ? '深色' : '浅色'}
      </button>
    </div>

    <div class="nav-tabs">
      <button class="nav-tab {tab === 'work' ? 'active' : ''}" onclick={() => (tab = 'work')}>工作台</button>
      <button class="nav-tab {tab === 'providers' ? 'active' : ''}" onclick={() => (tab = 'providers')}>模型</button>
      <button class="nav-tab {tab === 'extensions' ? 'active' : ''}" onclick={() => { tab = 'extensions'; void ctrl.loadLogs(); }}>扩展</button>
    </div>

    <button class="btn-primary" onclick={() => { tab = 'work'; showNewBox = !showNewBox; }}>
      新建会话
    </button>

    {#if showNewBox}
      <div class="layer-card" style="padding: 10px;">
        <div class="field-col">
          <span class="field-label">工作区路径</span>
          <input class="input-flat" bind:value={newWorkspace} placeholder="." />
        </div>
        <button class="btn-primary" onclick={() => void handleCreateSession()}>创建并进入</button>
      </div>
    {/if}

    <div class="session-list">
      {#each ctrl.sessions as s (s.id)}
        <button
          class="session-item {ctrl.current?.meta.id === s.id ? 'active' : ''}"
          onclick={() => { tab = 'work'; void ctrl.selectSession(s.id); }}
        >
          <div class="session-title">{s.title || s.id}</div>
          <div class="session-meta">
            <span>{s.model_id || s.provider_id}</span>
            <span>缓存 {cachePct(s.total_usage)}</span>
          </div>
        </button>
      {/each}
    </div>

    <div style="font-size: 11px; color: var(--fg-muted); padding: 4px 6px;">
      纯本地独立引擎 零云端依赖
    </div>
  </aside>

  <main class="main-pane">
    {#if ctrl.error}
      <div class="approval-bar" style="margin-top: 10px; background: var(--bg-err-soft); color: var(--fg-err);">
        <div style="display: flex; justify-content: space-between; align-items: center;">
          <span>{ctrl.error}</span>
          <button class="icon-btn" onclick={() => (ctrl.error = '')}>关闭</button>
        </div>
      </div>
    {/if}

    {#if tab === 'work'}
      <header class="topbar">
        <div class="topbar-left">
          <strong>{ctrl.current?.meta.title || '未选择会话'}</strong>
          {#if ctrl.current}
            <span class="pill">{ctrl.current.meta.workspace_dir}</span>
            {#if ctrl.current.meta.parent_session_id}
              <span class="pill accent">分叉会话</span>
            {/if}
          {/if}
        </div>

        {#if ctrl.current}
          <div class="topbar-actions">
            <select
              class="select-flat"
              value={ctrl.current.meta.provider_id}
              onchange={(e) => {
                const pid = (e.target as HTMLSelectElement).value;
                const prov = ctrl.config?.providers.find((p) => p.id === pid);
                void ctrl.updateSession({ provider_id: pid, model_id: prov?.default_model || ctrl.current?.meta.model_id });
              }}
            >
              {#each ctrl.config?.providers ?? [] as p}
                <option value={p.id}>{p.name}</option>
              {/each}
            </select>

            <input
              class="input-flat"
              style="width: 170px;"
              value={ctrl.current.meta.model_id}
              onchange={(e) => void ctrl.updateSession({ model_id: (e.target as HTMLInputElement).value })}
              placeholder="模型 ID"
            />

            <div class="segmented">
              {#each [['ask', '询问'], ['auto_read', '只读放行'], ['yolo', '全部放行']] as [modeVal, label]}
                <button
                  class={ctrl.current.meta.approval_mode === modeVal ? 'active' : ''}
                  onclick={() => void ctrl.updateSession({ approval_mode: modeVal as ApprovalMode })}
                >
                  {label}
                </button>
              {/each}
            </div>

            <button class="btn-flat" onclick={() => void ctrl.forkSession(0)}>分叉</button>
            <button class="btn-flat" onclick={() => (showInspector = !showInspector)}>
              {showInspector ? '收起侧栏' : '工作区'}
            </button>
          </div>
        {/if}
      </header>

      {#if (ctrl.current?.meta.todos?.length ?? 0) > 0}
        <div class="todo-strip">
          {#each ctrl.current?.meta.todos ?? [] as td}
            <span class="pill {td.status === 'completed' ? 'ok' : td.status === 'in_progress' ? 'accent' : ''}">
              {td.status === 'completed' ? '已完成' : td.status === 'in_progress' ? '进行中' : '待办'} {td.content}
            </span>
          {/each}
        </div>
      {/if}

      <div class="feed" bind:this={feedEl}>
        {#if ctrl.current}
          {#each ctrl.current.messages as msg (msg.id)}
            {#if msg.role === 'user'}
              {@const textBlocks = (msg.blocks ?? []).filter((b) => b.type === 'text' || b.type === 'system_notice')}
              {#if textBlocks.length > 0}
                <div class="msg-wrap">
                  <div class="msg-user">
                    {#each textBlocks as tb}{tb.text}{/each}
                  </div>
                </div>
              {/if}
            {:else if msg.role === 'assistant'}
              <div class="msg-wrap">
                <div class="msg-assistant">
                  {#each msg.blocks ?? [] as b}
                    {#if b.type === 'thinking' && b.thinking}
                      <details class="think-box">
                        <summary>思考过程 {b.thinking.length} 字</summary>
                        <div class="msg-text" style="margin-top: 6px;">{b.thinking}</div>
                      </details>
                    {:else if b.type === 'text' && b.text}
                      <div class="msg-text">{b.text}</div>
                    {:else if b.type === 'tool_use'}
                      {@const res = b.id ? toolResultsMap[b.id] : undefined}
                      <details class="tool-card">
                        <summary class="tool-head">
                          <span class="tool-name">{b.name}</span>
                          <span class="pill {res?.is_error ? 'warn' : res ? 'ok' : ''}">
                            {res ? (res.is_error ? '报错' : '完成') : '执行中'}
                          </span>
                        </summary>
                        <pre class="tool-pre">{fmtJSON(b.input)}</pre>
                        {#if res}
                          <pre class="tool-pre">{res.text}</pre>
                        {/if}
                      </details>
                    {/if}
                  {/each}
                  {#if msg.usage}
                    <div style="font-size: 11px; color: var(--fg-muted); display: flex; gap: 12px;">
                      <span>缓存命中 {cachePct(msg.usage)}</span>
                      <span>输入 {totalIn(msg.usage)}</span>
                      <span>输出 {msg.usage.output_tokens}</span>
                      {#if msg.usage.total_cost_nusd}
                        <span>{fmtCost(msg.usage.total_cost_nusd)}</span>
                      {/if}
                    </div>
                  {/if}
                </div>
              </div>
            {/if}
          {/each}

          {#if ctrl.activeStreamThinking || ctrl.activeStreamText || Object.keys(ctrl.activeTools).length > 0}
            <div class="msg-wrap">
              <div class="msg-assistant">
                {#if ctrl.activeStreamThinking}
                  <details class="think-box" open>
                    <summary>正在思考</summary>
                    <div class="msg-text" style="margin-top: 6px;">{ctrl.activeStreamThinking}</div>
                  </details>
                {/if}
                {#if ctrl.activeStreamText}
                  <div class="msg-text">{ctrl.activeStreamText}</div>
                {/if}
                {#each Object.entries(ctrl.activeTools) as [tid, t]}
                  <div class="tool-card">
                    <div class="tool-head">
                      <span class="tool-name">{t.name}</span>
                      <span class="pill {t.output ? (t.is_error ? 'warn' : 'ok') : 'accent'}">
                        {t.output ? `${t.duration_ms ?? 0}ms` : '运行中'}
                      </span>
                    </div>
                    <pre class="tool-pre">{fmtJSON(t.input)}</pre>
                    {#if t.output}
                      <pre class="tool-pre">{t.output}</pre>
                    {/if}
                  </div>
                {/each}
              </div>
            </div>
          {/if}
        {/if}
      </div>

      {#if pendingApprovals.length > 0}
        {#each pendingApprovals as ap (ap.id)}
          <div class="approval-bar">
            <div style="display: flex; justify-content: space-between; align-items: center;">
              <strong>需要确认执行工具 {ap.tool_name}</strong>
              <span class="pill">Hash {ap.args_hash.slice(0, 8)}</span>
            </div>
            <pre class="tool-pre" style="margin: 0;">{fmtJSON(ap.args_json)}</pre>
            <div style="display: flex; gap: 8px;">
              <button class="btn-flat btn-ok" onclick={() => void ctrl.resolveApproval(ap.id, 'approve_once')}>允许一次</button>
              <button class="btn-flat btn-ok" onclick={() => void ctrl.resolveApproval(ap.id, 'approve_always')}>本会话同类放行</button>
              <button class="btn-flat btn-err" onclick={() => void ctrl.resolveApproval(ap.id, 'deny')}>拒绝</button>
            </div>
          </div>
        {/each}
      {/if}

      <div class="composer-area">
        <div class="composer-box">
          <textarea
            class="composer-input"
            bind:value={promptText}
            placeholder="输入任务或指令 Enter 发送 Shift+Enter 换行"
            onkeydown={(e) => {
              if (e.key === 'Enter' && !e.shiftKey) {
                e.preventDefault();
                void handleSend(isRunning ? 'queue' : 'queue');
              }
            }}
          ></textarea>
          <div class="composer-foot">
            <div style="font-size: 12px; color: var(--fg-muted); display: flex; gap: 12px;">
              {#if ctrl.current}
                <span>会话累计缓存命中 {cachePct(ctrl.current.meta.total_usage)}</span>
                <span>总输入 {totalIn(ctrl.current.meta.total_usage)}</span>
                <span>总输出 {ctrl.current.meta.total_usage.output_tokens}</span>
              {/if}
            </div>
            <div style="display: flex; gap: 8px;">
              {#if isRunning}
                <button class="btn-flat btn-warn" onclick={() => void ctrl.cancelRun()}>停止</button>
                <button class="btn-flat" onclick={() => void handleSend('steer')}>插队引导</button>
                <button class="btn-primary" onclick={() => void handleSend('queue')}>排队发送</button>
              {:else}
                <button class="btn-primary" disabled={!promptText.trim()} onclick={() => void handleSend('queue')}>发送</button>
              {/if}
            </div>
          </div>
        </div>
      </div>

    {:else if tab === 'providers' && ctrl.config}
      <div class="panel-page">
        <div class="panel-inner">
          <div style="display: flex; justify-content: space-between; align-items: center;">
            <h2 style="margin: 0;">模型与凭据</h2>
            <button class="btn-primary" onclick={() => void saveProviders()}>保存全部配置</button>
          </div>

          <div class="layer-card">
            <strong>默认行为</strong>
            <div class="grid-2">
              <div class="field-col">
                <span class="field-label">默认 Provider</span>
                <select class="select-flat" bind:value={ctrl.config.default_provider}>
                  {#each ctrl.config.providers as p}
                    <option value={p.id}>{p.name}</option>
                  {/each}
                </select>
              </div>
              <div class="field-col">
                <span class="field-label">默认模型</span>
                <input class="input-flat" bind:value={ctrl.config.default_model} />
              </div>
              <div class="field-col">
                <span class="field-label">默认审批模式</span>
                <select class="select-flat" bind:value={ctrl.config.approval_mode}>
                  <option value="ask">每次询问</option>
                  <option value="auto_read">只读放行</option>
                  <option value="yolo">全部放行</option>
                </select>
              </div>
              <div class="field-col">
                <span class="field-label">单轮最大步数</span>
                <input class="input-flat" type="number" bind:value={ctrl.config.max_turns} />
              </div>
            </div>
            <div class="field-col">
              <span class="field-label">附加系统指令</span>
              <textarea class="input-flat" rows="2" bind:value={ctrl.config.system_extra} placeholder="可选 追加到静态系统提示尾部"></textarea>
            </div>
          </div>

          <div class="layer-card">
            <strong>daumini 中转一键登录</strong>
            <div class="grid-2">
              <div class="field-col">
                <span class="field-label">中转地址</span>
                <input class="input-flat" bind:value={dmBase} />
              </div>
              <div class="field-col">
                <span class="field-label">账号邮箱</span>
                <input class="input-flat" bind:value={dmEmail} placeholder="user@example.com" />
              </div>
              <div class="field-col">
                <span class="field-label">密码</span>
                <input class="input-flat" type="password" bind:value={dmPass} />
              </div>
              <div class="field-col" style="justify-content: flex-end;">
                <button class="btn-primary" onclick={() => void doDauminiLogin()}>登录并写入本地密钥</button>
              </div>
            </div>
            {#if dmStatus}
              <div class="pill accent">{dmStatus}</div>
            {/if}
          </div>

          {#each ctrl.config.providers as p, idx (p.id)}
            <div class="layer-card">
              <div style="display: flex; justify-content: space-between; align-items: center;">
                <strong>{p.name} ({p.id})</strong>
                <div style="display: flex; gap: 8px; align-items: center;">
                  <button class="btn-flat" onclick={() => void ctrl.fetchProviderModels(p.id)}>拉取模型</button>
                  <label style="font-size: 12px; display: flex; gap: 4px; align-items: center;">
                    <input type="checkbox" checked={p.enabled} onchange={(e) => updateProviderField(idx, { enabled: (e.target as HTMLInputElement).checked })} />
                    启用
                  </label>
                </div>
              </div>
              <div class="grid-2">
                <div class="field-col">
                  <span class="field-label">协议</span>
                  <select class="select-flat" value={p.protocol} onchange={(e) => updateProviderField(idx, { protocol: (e.target as HTMLSelectElement).value as ProviderConfig['protocol'] })}>
                    <option value="deepseek">DeepSeek</option>
                    <option value="openai_chat">OpenAI Chat</option>
                    <option value="openai_responses">OpenAI Responses</option>
                    <option value="anthropic">Anthropic Messages</option>
                    <option value="daumini">daumini 中转</option>
                  </select>
                </div>
                <div class="field-col">
                  <span class="field-label">Base URL</span>
                  <input class="input-flat" value={p.base_url} oninput={(e) => updateProviderField(idx, { base_url: (e.target as HTMLInputElement).value })} />
                </div>
                <div class="field-col">
                  <span class="field-label">API Key 留空保留旧值</span>
                  <input class="input-flat" type="password" value={p.api_key ?? ''} oninput={(e) => updateProviderField(idx, { api_key: (e.target as HTMLInputElement).value })} placeholder={p.api_key_env ? `环境变量 ${p.api_key_env}` : 'sk-...'} />
                </div>
                <div class="field-col">
                  <span class="field-label">默认模型</span>
                  <input class="input-flat" value={p.default_model ?? ''} oninput={(e) => updateProviderField(idx, { default_model: (e.target as HTMLInputElement).value })} />
                </div>
              </div>
              {#if (ctrl.providerModels[p.id]?.length ?? 0) > 0}
                <div style="display: flex; flex-wrap: wrap; gap: 6px;">
                  {#each ctrl.providerModels[p.id].slice(0, 24) as mid}
                    <button class="pill" onclick={() => updateProviderField(idx, { default_model: mid })}>{mid}</button>
                  {/each}
                </div>
              {/if}
            </div>
          {/each}

          <div class="layer-card">
            <strong>添加自定义 OpenAI 兼容端点</strong>
            <div class="grid-2">
              <input class="input-flat" bind:value={newProvName} placeholder="名称 例如 Local vLLM" />
              <input class="input-flat" bind:value={newProvUrl} placeholder="Base URL 例如 http://127.0.0.1:8000/v1" />
              <input class="input-flat" type="password" bind:value={newProvKey} placeholder="API Key" />
              <input class="input-flat" bind:value={newProvModel} placeholder="默认模型名" />
            </div>
            <div>
              <button class="btn-primary" onclick={() => void addCustomProvider()}>添加并保存</button>
            </div>
          </div>
        </div>
      </div>

    {:else if tab === 'extensions'}
      <div class="panel-page">
        <div class="panel-inner">
          <div style="display: flex; justify-content: space-between; align-items: center;">
            <h2 style="margin: 0;">MCP 技能 插件与日志</h2>
            <button class="btn-primary" onclick={() => void ctrl.reloadMCP()}>重载全部扩展</button>
          </div>

          <div class="layer-card">
            <strong>MCP 服务器</strong>
            <div class="grid-2">
              <input class="input-flat" bind:value={mcpName} placeholder="服务名 例如 filesystem" />
              <select class="select-flat" bind:value={mcpTransport}>
                <option value="stdio">Stdio 子进程</option>
                <option value="http">Streamable HTTP</option>
              </select>
              {#if mcpTransport === 'stdio'}
                <input class="input-flat" bind:value={mcpCommand} placeholder="可执行命令 例如 npx" />
                <input class="input-flat" bind:value={mcpArgs} placeholder="参数 空格分隔" />
              {:else}
                <input class="input-flat" bind:value={mcpURL} placeholder="http(s)://... MCP 地址" />
              {/if}
            </div>
            <div>
              <button class="btn-primary" onclick={() => void addMCP()}>添加 MCP 服务</button>
            </div>
            {#each ctrl.config?.mcp_servers ?? [] as ms, idx}
              <div style="display: flex; justify-content: space-between; align-items: center; background: var(--bg-raised); padding: 8px 12px; border-radius: 8px;">
                <div>
                  <strong>{ms.name}</strong>
                  <span class="pill" style="margin-left: 6px;">{ms.transport}</span>
                  <span style="font-size: 12px; color: var(--fg-muted); margin-left: 8px;">{ms.command || ms.url}</span>
                </div>
                <button class="btn-flat btn-err" onclick={() => void removeMCP(idx)}>移除</button>
              </div>
            {/each}
            {#each ctrl.mcpServers as st}
              <div style="font-size: 12px; color: var(--fg-secondary);">
                {st.name} 状态 {st.connected ? `已连接 ${st.tools?.length ?? 0} 个工具` : `失败 ${st.error}`}
              </div>
            {/each}
          </div>

          <div class="layer-card">
            <strong>已发现技能 ({ctrl.skills.length})</strong>
            {#if ctrl.skills.length === 0}
              <span style="font-size: 12px; color: var(--fg-muted);">在工作区 .daumini-agent/skills 或用户配置目录放入 SKILL.md 即可自动加载</span>
            {:else}
              {#each ctrl.skills as sk}
                <div style="font-size: 13px;">
                  <strong>{sk.name}</strong> <span class="pill">{sk.scope}</span>
                  <span style="color: var(--fg-secondary); margin-left: 6px;">{sk.description}</span>
                </div>
              {/each}
            {/if}
          </div>

          <div class="layer-card">
            <strong>已注册工具 ({ctrl.tools.length})</strong>
            <div style="display: flex; flex-wrap: wrap; gap: 6px;">
              {#each ctrl.tools as tl}
                <span class="pill">{tl.name}</span>
              {/each}
            </div>
          </div>

          <div class="layer-card">
            <div style="display: flex; justify-content: space-between; align-items: center;">
              <strong>诊断日志</strong>
              <button class="btn-flat" onclick={() => void ctrl.loadLogs()}>刷新</button>
            </div>
            <pre class="tool-pre">{ctrl.logs.map((l) => `${l.ts} [${l.level}] ${l.component} ${l.msg}`).join('\n') || '暂无日志'}</pre>
          </div>
        </div>
      </div>
    {/if}
  </main>

  {#if tab === 'work' && showInspector && ctrl.current}
    <aside class="inspector">
      <div style="display: flex; justify-content: space-between; align-items: center;">
        <strong>工作区文件</strong>
        <button class="icon-btn" onclick={() => ctrl.current && void ctrl.loadWorkspaceFiles(ctrl.current.meta.id)}>刷新</button>
      </div>
      <div style="max-height: 280px; overflow-y: auto;">
        {#each ctrl.files as f}
          <button class="file-row" onclick={() => void ctrl.readFile(f)}>{f}</button>
        {/each}
      </div>

      {#if ctrl.filePreview}
        <div class="layer-card" style="padding: 10px;">
          <div style="display: flex; justify-content: space-between; align-items: center;">
            <span style="font-family: var(--font-mono); font-size: 12px;">{ctrl.filePreview.path}</span>
            <button class="icon-btn" onclick={() => (ctrl.filePreview = null)}>关闭</button>
          </div>
          <pre class="tool-pre" style="max-height: 260px;">{ctrl.filePreview.content}</pre>
        </div>
      {/if}

      <div class="layer-card" style="padding: 12px;">
        <strong>缓存与用量</strong>
        <div style="font-size: 12px; color: var(--fg-secondary); display: flex; flex-direction: column; gap: 4px;">
          <div>缓存命中率 {cachePct(ctrl.current.meta.total_usage)}</div>
          <div>总输入 {totalIn(ctrl.current.meta.total_usage)}</div>
          <div>缓存读取 {ctrl.current.meta.total_usage.cache_read_tokens}</div>
          <div>缓存写入 {ctrl.current.meta.total_usage.cache_write_tokens}</div>
          <div>总输出 {ctrl.current.meta.total_usage.output_tokens}</div>
          <div>事件序列 {ctrl.current.meta.last_seq}</div>
        </div>
      </div>
    </aside>
  {/if}
</div>
