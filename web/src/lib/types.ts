export type ApprovalMode = 'ask' | 'auto_read' | 'yolo';

export interface Block {
  type: 'text' | 'thinking' | 'tool_use' | 'tool_result' | 'system_notice' | 'subagent_trace';
  text?: string;
  thinking?: string;
  id?: string;
  name?: string;
  input?: unknown;
  tool_use_id?: string;
  is_error?: boolean;
  masked?: boolean;
}

export interface Usage {
  input_tokens: number;
  cache_read_tokens: number;
  cache_write_tokens: number;
  output_tokens: number;
  reasoning_tokens: number;
  total_cost_nusd?: number;
  generation_id?: string;
  request_id?: string;
  latency_ms?: number;
  reconciled?: boolean;
}

export interface ModelInfo {
  id: string;
  name?: string;
  author?: string;
  description?: string;
  context_window?: number;
  max_output?: number;
  supported_protocols?: string[];
  supported_parameters?: string[];
  reasoning_levels?: string[];
  supports_images?: boolean;
  supports_tools?: boolean;
  supports_thinking?: boolean;
  input_nusd_per_million?: number;
  output_nusd_per_million?: number;
  cache_read_nusd_per_million?: number;
  cache_write_nusd_per_million?: number;
  request_nusd?: number;
  latency_ms?: number;
}

export interface Message {
  id: string;
  session_id: string;
  run_id?: string;
  seq: number;
  role: 'user' | 'assistant' | 'system';
  blocks: Block[];
  model_id?: string;
  provider_id?: string;
  stop_reason?: string;
  usage?: Usage;
  created_at: string;
}

export interface TodoItem {
  id: string;
  content: string;
  status: 'pending' | 'in_progress' | 'completed';
}

export interface Approval {
  id: string;
  session_id: string;
  run_id: string;
  tool_use_id: string;
  tool_name: string;
  args_hash: string;
  args_json: unknown;
  decision: 'pending' | 'approve_once' | 'approve_always' | 'deny';
  created_at: string;
}

export interface Run {
  id: string;
  session_id: string;
  prompt: string;
  provider_id: string;
  model_id: string;
  status: string;
  turns: number;
  max_turns: number;
  error?: string;
  usage: Usage;
}

export interface SessionMeta {
  id: string;
  title: string;
  workspace_dir: string;
  provider_id: string;
  model_id: string;
  reasoning_effort?: string;
  approval_mode: ApprovalMode;
  parent_session_id?: string;
  forked_from_seq?: number;
  todos?: TodoItem[];
  total_usage: Usage;
  last_seq: number;
  message_count: number;
  active_run_id?: string;
  archived?: boolean;
  created_at: string;
  updated_at: string;
}

export interface SessionSnapshot {
  meta: SessionMeta;
  messages: Message[];
  runs: Run[];
  pending_approvals: Approval[];
}

export interface ProviderConfig {
  id: string;
  name: string;
  protocol: 'openai_chat' | 'openai_responses' | 'anthropic' | 'deepseek' | 'daumini';
  base_url: string;
  api_key?: string;
  api_key_env?: string;
  default_model?: string;
  models?: string[];
  enabled: boolean;
  user_email?: string;
}

export interface MCPServerConfig {
  name: string;
  transport: 'stdio' | 'http';
  command?: string;
  args?: string[];
  url?: string;
  enabled: boolean;
}

export interface HookConfig {
  name: string;
  event: string;
  match_tool?: string;
  command: string;
  args?: string[];
  enabled: boolean;
}

export interface PluginConfig {
  name: string;
  command: string;
  args?: string[];
  enabled: boolean;
}

export interface AgentConfig {
  version: number;
  default_provider: string;
  default_model: string;
  approval_mode: ApprovalMode;
  max_turns: number;
  theme: 'auto' | 'light' | 'dark';
  web_addr: string;
  providers: ProviderConfig[];
  mcp_servers?: MCPServerConfig[];
  hooks?: HookConfig[];
  plugins?: PluginConfig[];
  system_extra?: string;
}

export interface MCPServerStatus {
  name: string;
  transport: string;
  connected: boolean;
  error?: string;
  tools?: { name: string; description: string }[];
}

export interface SkillInfo {
  name: string;
  description: string;
  path: string;
  scope: string;
}

export interface LogEntry {
  ts: string;
  level: string;
  component: string;
  msg: string;
}
