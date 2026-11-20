import type {
  AgentConfig,
  ApprovalDecision,
  ApprovalMode,
  ColorTheme,
  DauminiGenerationResponse,
  DauminiStatus,
  DauminiUsageLedger,
  FSBrowseResponse,
  LogEntry,
  MCPServerStatus,
  ModelInfo,
  SessionMeta,
  SessionSnapshot,
  SkillInfo,
  UIStyle
} from './types';
import { isValidUIStyle } from './themes';

async function apiJSON<T>(url: string, init?: RequestInit): Promise<T> {
  const resp = await fetch(url, {
    ...init,
    headers: {
      'Content-Type': 'application/json',
      ...(init?.headers ?? {})
    }
  });
  const data = await resp.json().catch(() => ({}));
  if (!resp.ok) {
    throw new Error((data as { error?: string }).error || `HTTP ${resp.status}`);
  }
  return data as T;
}

function normalizeSnapshot(snap: SessionSnapshot): SessionSnapshot {
  return {
    ...snap,
    meta: {
      ...snap.meta,
      todos: snap.meta?.todos ?? []
    },
    messages: snap.messages ?? [],
    runs: snap.runs ?? [],
    pending_approvals: snap.pending_approvals ?? []
  };
}

const CATALOG_CACHE_KEY = 'daumini.catalog.v1';

function readCachedCatalog(): Record<string, ModelInfo[]> {
  try {
    const raw = localStorage.getItem(CATALOG_CACHE_KEY);
    if (!raw) return {};
    const parsed = JSON.parse(raw) as Record<string, ModelInfo[]>;
    return parsed && typeof parsed === 'object' ? parsed : {};
  } catch {
    return {};
  }
}

function writeCachedCatalog(map: Record<string, ModelInfo[]>) {
  try {
    localStorage.setItem(CATALOG_CACHE_KEY, JSON.stringify(map));
  } catch {}
}

export class AgentController {
  config = $state<AgentConfig | null>(null);
  sessions = $state<SessionMeta[]>([]);
  current = $state<SessionSnapshot | null>(null);
  activeStreamText = $state('');
  activeStreamThinking = $state('');
  activeTools = $state<Record<string, { name: string; input: unknown; output?: string; is_error?: boolean; duration_ms?: number }>>({});
  mcpServers = $state<MCPServerStatus[]>([]);
  skills = $state<SkillInfo[]>([]);
  tools = $state<{ name: string; description: string }[]>([]);
  logs = $state<LogEntry[]>([]);
  files = $state<string[]>([]);
  filePreview = $state<{ path: string; content: string } | null>(null);
  providerModels = $state<Record<string, string[]>>({});
  providerCatalog = $state<Record<string, ModelInfo[]>>(readCachedCatalog());
  loadingModels = $state<Record<string, boolean>>({});
  error = $state('');
  busy = $state(false);
  theme = $state<ColorTheme>('light');
  uiStyle = $state<UIStyle>('atelier');
  currency = $state<'USD' | 'CNY'>('USD');
  cnyRate = $state<number>(7.25);
  dauminiStatus = $state<DauminiStatus | null>(null);
  dauminiLedger = $state<DauminiUsageLedger | null>(null);

  private es: EventSource | null = null;
  private lastSeq = 0;

  constructor() {
    // Hydrate providerModels from cached catalog immediately so first paint has model labels
    const cached = this.providerCatalog;
    const idMap: Record<string, string[]> = {};
    for (const [k, list] of Object.entries(cached)) {
      idMap[k] = (list ?? []).map((m) => m.id);
    }
    this.providerModels = idMap;
    try {
      const savedCur = localStorage.getItem('daumini.currency');
      if (savedCur === 'CNY' || savedCur === 'USD') this.currency = savedCur;
    } catch {}
  }

  initAppearance(themePref: string, stylePref?: string) {
    if (themePref === 'light' || themePref === 'dark') {
      this.theme = themePref;
    } else {
      const stored = typeof localStorage !== 'undefined' ? localStorage.getItem('daumini.theme') : null;
      if (stored === 'light' || stored === 'dark') {
        this.theme = stored;
      } else {
        this.theme = window.matchMedia?.('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
      }
    }

    const storedStyle = typeof localStorage !== 'undefined' ? localStorage.getItem('daumini.style') : null;
    if (isValidUIStyle(stylePref)) {
      this.uiStyle = stylePref;
    } else if (isValidUIStyle(storedStyle)) {
      this.uiStyle = storedStyle;
    } else {
      this.uiStyle = 'atelier';
    }

    document.documentElement.dataset.theme = this.theme;
    document.documentElement.dataset.style = this.uiStyle;
    try {
      localStorage.setItem('daumini.theme', this.theme);
      localStorage.setItem('daumini.style', this.uiStyle);
    } catch {}
  }

  setTheme(next: ColorTheme) {
    this.theme = next;
    document.documentElement.dataset.theme = next;
    try { localStorage.setItem('daumini.theme', next); } catch {}
    if (this.config) {
      void this.saveConfig({ ...this.config, theme: next });
    }
  }

  toggleTheme() {
    this.setTheme(this.theme === 'light' ? 'dark' : 'light');
  }

  setUIStyle(next: UIStyle) {
    this.uiStyle = next;
    document.documentElement.dataset.style = next;
    try { localStorage.setItem('daumini.style', next); } catch {}
    if (this.config) {
      void this.saveConfig({ ...this.config, ui_style: next });
    }
  }

  setCurrency(next: 'USD' | 'CNY') {
    this.currency = next;
    try { localStorage.setItem('daumini.currency', next); } catch {}
  }

  formatNUSD(nusd?: number, digits = 4): string {
    if (!nusd) return this.currency === 'CNY' ? '¥0.0000' : '$0.0000';
    const usd = nusd / 1e9;
    if (this.currency === 'CNY') {
      return `¥${(usd * this.cnyRate).toFixed(digits)}`;
    }
    return `$${usd.toFixed(digits)}`;
  }

  formatPricePerM(nusdPerMillion?: number): string {
    if (!nusdPerMillion || nusdPerMillion <= 0) return '—';
    const usd = nusdPerMillion / 1e9;
    const val = this.currency === 'CNY' ? usd * this.cnyRate : usd;
    const sym = this.currency === 'CNY' ? '¥' : '$';
    return val >= 0.01 ? `${sym}${val.toFixed(2)}` : `${sym}${val.toFixed(3)}`;
  }

  async bootstrap() {
    this.error = '';
    const [cfg, sesRes, mcpRes, toolsRes] = await Promise.all([
      apiJSON<AgentConfig>('/api/config'),
      apiJSON<{ sessions: SessionMeta[] }>('/api/sessions'),
      apiJSON<{ servers: MCPServerStatus[] }>('/api/mcp'),
      apiJSON<{ tools: { name: string; description: string }[] }>('/api/tools')
    ]);
    this.config = cfg;
    this.initAppearance(cfg.theme, cfg.ui_style);
    this.sessions = sesRes.sessions ?? [];
    this.mcpServers = mcpRes.servers ?? [];
    this.tools = toolsRes.tools ?? [];

    if (this.sessions.length > 0) {
      await this.selectSession(this.sessions[0].id);
    } else {
      await this.createSession('.');
    }
    if (cfg.providers.some((p) => p.id === 'daumini' && p.enabled)) {
      void this.fetchProviderModels('daumini').catch(() => {});
      void this.loadDauminiStatus().catch(() => {});
    }
  }

  async refreshSessions() {
    const res = await apiJSON<{ sessions: SessionMeta[] }>('/api/sessions');
    this.sessions = res.sessions ?? [];
  }

  async createSession(workspaceDir: string, providerId?: string, modelId?: string) {
    this.error = '';
    const meta = await apiJSON<SessionMeta>('/api/sessions', {
      method: 'POST',
      body: JSON.stringify({
        workspace_dir: workspaceDir || '.',
        provider_id: providerId || this.config?.default_provider || '',
        model_id: modelId || this.config?.default_model || ''
      })
    });
    await this.refreshSessions();
    await this.selectSession(meta.id);
    // Refresh config to pick up updated recent_workspaces
    void apiJSON<AgentConfig>('/api/config').then((c) => (this.config = c)).catch(() => {});
  }

  async selectSession(id: string) {
    this.closeSSE();
    this.activeStreamText = '';
    this.activeStreamThinking = '';
    this.activeTools = {};
    this.filePreview = null;
    const snap = normalizeSnapshot(await apiJSON<SessionSnapshot>(`/api/sessions/${encodeURIComponent(id)}`));
    this.current = snap;
    this.lastSeq = snap.meta.last_seq;
    this.connectSSE(id, snap.meta.last_seq);
    void this.loadWorkspaceFiles(id);
    void this.loadSkills(snap.meta.workspace_dir);
  }

  async deleteSession(id: string) {
    this.error = '';
    const wasCurrent = this.current?.meta.id === id;
    if (wasCurrent) {
      this.closeSSE();
    }
    await apiJSON(`/api/sessions/${encodeURIComponent(id)}`, { method: 'DELETE' });
    await this.refreshSessions();
    if (wasCurrent) {
      if (this.sessions.length > 0) {
        await this.selectSession(this.sessions[0].id);
      } else {
        await this.createSession('.');
      }
    }
  }

  async renameSession(id: string, title: string) {
    const trimmed = title.trim();
    if (!trimmed) return;
    const meta = await apiJSON<SessionMeta>(`/api/sessions/${encodeURIComponent(id)}`, {
      method: 'PATCH',
      body: JSON.stringify({ title: trimmed })
    });
    if (this.current?.meta.id === id) {
      this.current = { ...this.current, meta };
    }
    await this.refreshSessions();
  }

  async startDauminiOAuth(baseUrl?: string) {
    this.error = '';
    const res = await apiJSON<{ authorize_url: string; state: string }>('/api/auth/daumini/start', {
      method: 'POST',
      body: JSON.stringify({ base_url: baseUrl || '' })
    });
    window.location.assign(res.authorize_url);
  }

  async syncAfterOAuthCallback() {
    await this.afterDauminiConnected();
  }

  async forkSession(upToSeq = 0) {
    if (!this.current) return;
    const snap = await apiJSON<SessionSnapshot>(`/api/sessions/${encodeURIComponent(this.current.meta.id)}/fork`, {
      method: 'POST',
      body: JSON.stringify({ up_to_seq: upToSeq })
    });
    await this.refreshSessions();
    await this.selectSession(snap.meta.id);
  }

  async updateSession(patch: Partial<{ provider_id: string; model_id: string; reasoning_effort: string; approval_mode: ApprovalMode; workspace_dir: string; archived: boolean; title: string }>) {
    if (!this.current) return;
    const meta = await apiJSON<SessionMeta>(`/api/sessions/${encodeURIComponent(this.current.meta.id)}`, {
      method: 'PATCH',
      body: JSON.stringify(patch)
    });
    this.current = { ...this.current, meta };
    await this.refreshSessions();
    if (patch.workspace_dir) {
      this.filePreview = null;
      void this.loadWorkspaceFiles(meta.id);
      void this.loadSkills(meta.workspace_dir);
      void apiJSON<AgentConfig>('/api/config').then((c) => (this.config = c)).catch(() => {});
    }
  }

  async sendMessage(prompt: string, mode: 'queue' | 'steer' | 'interrupt' = 'queue') {
    if (!this.current || !prompt.trim()) return;
    this.error = '';
    await apiJSON(`/api/sessions/${encodeURIComponent(this.current.meta.id)}/messages`, {
      method: 'POST',
      body: JSON.stringify({
        prompt,
        provider_id: this.current.meta.provider_id,
        model_id: this.current.meta.model_id,
        mode
      })
    });
  }

  async cancelRun() {
    if (!this.current) return;
    await apiJSON(`/api/sessions/${encodeURIComponent(this.current.meta.id)}/cancel`, { method: 'POST' });
  }

  async resolveApproval(approvalId: string, decision: ApprovalDecision) {
    if (!this.current) return;
    await apiJSON(`/api/sessions/${encodeURIComponent(this.current.meta.id)}/approvals/${encodeURIComponent(approvalId)}`, {
      method: 'POST',
      body: JSON.stringify({ decision })
    });
  }

  async saveConfig(next: AgentConfig) {
    this.error = '';
    const saved = await apiJSON<AgentConfig>('/api/config', {
      method: 'PUT',
      body: JSON.stringify(next)
    });
    this.config = saved;
    const mcpRes = await apiJSON<{ servers: MCPServerStatus[] }>('/api/mcp');
    this.mcpServers = mcpRes.servers ?? [];
  }

  async loginDaumini(baseUrl: string, email: string, password: string, inviteCode = '') {
    this.error = '';
    const saved = await apiJSON<AgentConfig>('/api/providers/daumini/login', {
      method: 'POST',
      body: JSON.stringify({ base_url: baseUrl, email, password, invite_code: inviteCode })
    });
    this.config = saved;
    await this.afterDauminiConnected();
  }

  async saveDauminiKey(baseUrl: string, apiKey: string) {
    this.error = '';
    const saved = await apiJSON<AgentConfig>('/api/providers/daumini/key', {
      method: 'POST',
      body: JSON.stringify({ base_url: baseUrl, api_key: apiKey })
    });
    this.config = saved;
    await this.afterDauminiConnected();
  }

  async logoutDaumini() {
    this.error = '';
    const saved = await apiJSON<AgentConfig>('/api/providers/daumini/logout', {
      method: 'POST'
    });
    this.config = saved;
    this.dauminiStatus = null;
  }

  private async afterDauminiConnected() {
    let preferredModel = '';
    try {
      await Promise.all([
        this.fetchProviderModels('daumini'),
        this.loadDauminiStatus().catch(() => {})
      ]);
      const models = this.providerModels['daumini'] ?? [];
      if (models.length > 0) {
        preferredModel =
          models.find((m) => m.includes('deepseek')) ||
          models.find((m) => !m.includes('mock')) ||
          models[0];
      }
    } catch {
      // ignore model fetch error after login
    }
    if (this.current) {
      await this.updateSession({
        provider_id: 'daumini',
        model_id: preferredModel || this.current.meta.model_id || 'deepseek/deepseek-v4.1flash'
      });
    }
  }

  async fetchProviderModels(providerId: string) {
    this.loadingModels = { ...this.loadingModels, [providerId]: true };
    try {
      const res = await apiJSON<{ models: ModelInfo[] }>(`/api/providers/${encodeURIComponent(providerId)}/models`);
      const list = res.models ?? [];
      this.providerCatalog = {
        ...this.providerCatalog,
        [providerId]: list
      };
      this.providerModels = {
        ...this.providerModels,
        [providerId]: list.map((m) => m.id)
      };
      writeCachedCatalog(this.providerCatalog);
    } finally {
      this.loadingModels = { ...this.loadingModels, [providerId]: false };
    }
  }

  // ── Graphical Filesystem Picker APIs ──────────────────────────────────────

  async browseFS(path = '', hidden = false): Promise<FSBrowseResponse> {
    const q = new URLSearchParams();
    if (path) q.set('path', path);
    if (hidden) q.set('hidden', '1');
    return apiJSON<FSBrowseResponse>(`/api/fs/browse?${q.toString()}`);
  }

  async mkdirFS(parent: string, name: string): Promise<{ path: string; name: string }> {
    return apiJSON<{ path: string; name: string }>('/api/fs/mkdir', {
      method: 'POST',
      body: JSON.stringify({ parent, name })
    });
  }

  async pickNativeFolder(initialDir = ''): Promise<{ path: string; cancelled: boolean }> {
    return apiJSON<{ path: string; cancelled: boolean }>('/api/fs/pick-folder', {
      method: 'POST',
      body: JSON.stringify({ initial_dir: initialDir, title: '选择工作区文件夹' })
    });
  }

  // ── In-Agent daumini Platform APIs ────────────────────────────────────────

  async loadDauminiStatus(): Promise<DauminiStatus> {
    const st = await apiJSON<DauminiStatus>('/api/daumini/status');
    this.dauminiStatus = st;
    const cny = st.currencies?.data?.find((c) => c.code === 'CNY');
    const rate = cny?.rate_per_usd || cny?.rate;
    if (rate && rate > 0) this.cnyRate = rate;
    return st;
  }

  async loadDauminiGeneration(genId: string): Promise<DauminiGenerationResponse> {
    return apiJSON<DauminiGenerationResponse>(`/api/daumini/generation/${encodeURIComponent(genId)}`);
  }

  async loadDauminiCatalogRaw(): Promise<{ data: any[]; currencies?: DauminiStatus['currencies']; base_url?: string }> {
    const res = await apiJSON<{ data: any[]; currencies?: DauminiStatus['currencies']; base_url?: string }>('/api/daumini/models');
    const cny = res.currencies?.data?.find((c) => c.code === 'CNY');
    const rate = cny?.rate_per_usd || cny?.rate;
    if (rate && rate > 0) this.cnyRate = rate;
    return res;
  }

  async loadDauminiModelEndpoints(modelId: string): Promise<any> {
    return apiJSON<any>(`/api/daumini/model-endpoints?id=${encodeURIComponent(modelId)}`);
  }

  async loadDauminiLedger(): Promise<DauminiUsageLedger> {
    const res = await apiJSON<DauminiUsageLedger>('/api/daumini/usage-ledger');
    this.dauminiLedger = res;
    return res;
  }

  // ── Workspace & Extensions ────────────────────────────────────────────────

  async reloadMCP() {
    const res = await apiJSON<{ servers: MCPServerStatus[] }>('/api/mcp/reload', { method: 'POST' });
    this.mcpServers = res.servers ?? [];
    const toolsRes = await apiJSON<{ tools: { name: string; description: string }[] }>('/api/tools');
    this.tools = toolsRes.tools ?? [];
  }

  async loadWorkspaceFiles(sessionId: string) {
    try {
      const res = await apiJSON<{ files: string[] }>(`/api/workspace/files?session_id=${encodeURIComponent(sessionId)}`);
      this.files = res.files ?? [];
    } catch {
      this.files = [];
    }
  }

  async readFile(path: string) {
    if (!this.current) return;
    const res = await apiJSON<{ path: string; content: string }>(
      `/api/workspace/file?session_id=${encodeURIComponent(this.current.meta.id)}&path=${encodeURIComponent(path)}`
    );
    this.filePreview = res;
  }

  async loadSkills(dir: string) {
    try {
      const res = await apiJSON<{ skills: SkillInfo[] }>(`/api/skills?dir=${encodeURIComponent(dir)}`);
      this.skills = res.skills ?? [];
    } catch {
      this.skills = [];
    }
  }

  async loadLogs() {
    const res = await apiJSON<{ logs: LogEntry[] }>('/api/logs');
    this.logs = res.logs ?? [];
  }

  private connectSSE(sessionId: string, afterSeq: number) {
    const es = new EventSource(`/api/sessions/${encodeURIComponent(sessionId)}/events?after_seq=${afterSeq}`);
    this.es = es;

    const refreshSnap = async () => {
      try {
        const snap = normalizeSnapshot(await apiJSON<SessionSnapshot>(`/api/sessions/${encodeURIComponent(sessionId)}`));
        if (this.current?.meta.id === sessionId) {
          this.current = snap;
          this.lastSeq = snap.meta.last_seq;
        }
        void this.refreshSessions();
      } catch {}
    };

    es.addEventListener('session.snapshot', (e) => {
      try {
        const snap = normalizeSnapshot(JSON.parse((e as MessageEvent).data) as SessionSnapshot);
        if (this.current?.meta.id === sessionId) {
          this.current = snap;
          this.lastSeq = snap.meta.last_seq;
        }
      } catch {}
    });

    es.addEventListener('run.started', () => {
      this.activeStreamText = '';
      this.activeStreamThinking = '';
      this.activeTools = {};
      void refreshSnap();
    });

    es.addEventListener('assistant.text_delta', (e) => {
      const ev = JSON.parse((e as MessageEvent).data) as { seq: number; payload: { delta: string } };
      this.lastSeq = Math.max(this.lastSeq, ev.seq);
      this.activeStreamText += ev.payload.delta || '';
    });

    es.addEventListener('assistant.thinking_delta', (e) => {
      const ev = JSON.parse((e as MessageEvent).data) as { seq: number; payload: { delta: string } };
      this.lastSeq = Math.max(this.lastSeq, ev.seq);
      this.activeStreamThinking += ev.payload.delta || '';
    });

    es.addEventListener('assistant.message', () => {
      this.activeStreamText = '';
      this.activeStreamThinking = '';
      void refreshSnap();
    });

    es.addEventListener('tool.started', (e) => {
      const ev = JSON.parse((e as MessageEvent).data) as { payload: { tool_use_id: string; name: string; input: unknown } };
      this.activeTools = {
        ...this.activeTools,
        [ev.payload.tool_use_id]: { name: ev.payload.name, input: ev.payload.input }
      };
    });

    es.addEventListener('tool.finished', (e) => {
      const ev = JSON.parse((e as MessageEvent).data) as {
        payload: { tool_use_id: string; name: string; is_error: boolean; duration_ms: number; output: string };
      };
      const prev = this.activeTools[ev.payload.tool_use_id] ?? { name: ev.payload.name, input: {} };
      this.activeTools = {
        ...this.activeTools,
        [ev.payload.tool_use_id]: {
          ...prev,
          output: ev.payload.output,
          is_error: ev.payload.is_error,
          duration_ms: ev.payload.duration_ms
        }
      };
      void this.loadWorkspaceFiles(sessionId);
    });

    es.addEventListener('approval.requested', () => {
      void refreshSnap();
    });

    es.addEventListener('approval.resolved', () => {
      void refreshSnap();
    });

    es.addEventListener('run.finished', () => {
      this.activeStreamText = '';
      this.activeStreamThinking = '';
      this.activeTools = {};
      void refreshSnap();
    });

    es.onerror = () => {
      // Browser EventSource reconnects automatically.
    };
  }

  private closeSSE() {
    if (this.es) {
      this.es.close();
      this.es = null;
    }
  }
}
