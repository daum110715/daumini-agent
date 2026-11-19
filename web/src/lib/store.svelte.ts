import type {
  AgentConfig,
  ApprovalDecision,
  ApprovalMode,
  LogEntry,
  MCPServerStatus,
  SessionMeta,
  SessionSnapshot,
  SkillInfo
} from './types';

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
    pending_approvals: snap.pending_approvals ?? [],
    queued_messages: snap.queued_messages ?? []
  };
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
  error = $state('');
  busy = $state(false);
  theme = $state<'light' | 'dark'>('light');

  private es: EventSource | null = null;
  private lastSeq = 0;

  initTheme(pref: string) {
    if (pref === 'light' || pref === 'dark') {
      this.theme = pref;
    } else {
      this.theme = window.matchMedia?.('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
    }
    document.documentElement.dataset.theme = this.theme;
    try { localStorage.setItem('daumini.theme', this.theme); } catch {}
  }

  toggleTheme() {
    this.theme = this.theme === 'light' ? 'dark' : 'light';
    document.documentElement.dataset.theme = this.theme;
    try { localStorage.setItem('daumini.theme', this.theme); } catch {}
    if (this.config) {
      void this.saveConfig({ ...this.config, theme: this.theme });
    }
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
    this.initTheme(cfg.theme);
    this.sessions = sesRes.sessions ?? [];
    this.mcpServers = mcpRes.servers ?? [];
    this.tools = toolsRes.tools ?? [];

    if (this.sessions.length > 0) {
      await this.selectSession(this.sessions[0].id);
    } else {
      await this.createSession('.');
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

  async forkSession(upToSeq = 0) {
    if (!this.current) return;
    const snap = await apiJSON<SessionSnapshot>(`/api/sessions/${encodeURIComponent(this.current.meta.id)}/fork`, {
      method: 'POST',
      body: JSON.stringify({ up_to_seq: upToSeq })
    });
    await this.refreshSessions();
    await this.selectSession(snap.meta.id);
  }

  async updateSession(patch: Partial<{ provider_id: string; model_id: string; approval_mode: ApprovalMode; workspace_dir: string; archived: boolean; title: string }>) {
    if (!this.current) return;
    const meta = await apiJSON<SessionMeta>(`/api/sessions/${encodeURIComponent(this.current.meta.id)}`, {
      method: 'PATCH',
      body: JSON.stringify(patch)
    });
    this.current = { ...this.current, meta };
    await this.refreshSessions();
    if (patch.workspace_dir) {
      void this.loadWorkspaceFiles(meta.id);
      void this.loadSkills(meta.workspace_dir);
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

  async loginDaumini(baseUrl: string, email: string, password: string) {
    this.error = '';
    const saved = await apiJSON<AgentConfig>('/api/providers/daumini/login', {
      method: 'POST',
      body: JSON.stringify({ base_url: baseUrl, email, password })
    });
    this.config = saved;
  }

  async fetchProviderModels(providerId: string) {
    const res = await apiJSON<{ models: { id: string }[] }>(`/api/providers/${encodeURIComponent(providerId)}/models`);
    this.providerModels = {
      ...this.providerModels,
      [providerId]: (res.models ?? []).map((m) => m.id)
    };
  }

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
      } catch {
        // ignore transient errors
      }
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

    es.addEventListener('tool.results', () => {
      this.activeTools = {};
      void refreshSnap();
    });

    es.addEventListener('approval.requested', () => {
      void refreshSnap();
    });

    es.addEventListener('approval.resolved', () => {
      void refreshSnap();
    });

    es.addEventListener('todo.updated', () => {
      void refreshSnap();
    });

    es.addEventListener('run.finished', (e) => {
      const ev = JSON.parse((e as MessageEvent).data) as { payload: { status: string; error?: string } };
      if (ev.payload.status === 'failed' && ev.payload.error) {
        this.error = ev.payload.error;
      }
      this.activeStreamText = '';
      this.activeStreamThinking = '';
      this.activeTools = {};
      void refreshSnap();
    });
  }

  closeSSE() {
    if (this.es) {
      this.es.close();
      this.es = null;
    }
  }
}
