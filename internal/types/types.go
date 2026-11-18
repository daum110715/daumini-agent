package types

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleSystem    Role = "system"
)

type BlockType string

const (
	BlockText          BlockType = "text"
	BlockThinking      BlockType = "thinking"
	BlockToolUse       BlockType = "tool_use"
	BlockToolResult    BlockType = "tool_result"
	BlockSystemNotice  BlockType = "system_notice"
	BlockSubagentTrace BlockType = "subagent_trace"
)

// Block is the canonical, lossless multi-modal/tool/reasoning content unit across providers.
type Block struct {
	Type       BlockType       `json:"type"`
	Text       string          `json:"text,omitempty"`
	Thinking   string          `json:"thinking,omitempty"`
	Signature  string          `json:"signature,omitempty"`
	Encrypted  string          `json:"encrypted,omitempty"`
	ID         string          `json:"id,omitempty"`
	Name       string          `json:"name,omitempty"`
	Input      json.RawMessage `json:"input,omitempty"`
	ToolUseID  string          `json:"tool_use_id,omitempty"`
	IsError    bool            `json:"is_error,omitempty"`
	Masked     bool            `json:"masked,omitempty"`
	Raw        json.RawMessage `json:"raw,omitempty"`
}

// Usage tracks cache-first token accounting on every turn.
type Usage struct {
	InputTokens      int64  `json:"input_tokens"`
	CacheReadTokens  int64  `json:"cache_read_tokens"`
	CacheWriteTokens int64  `json:"cache_write_tokens"`
	OutputTokens     int64  `json:"output_tokens"`
	ReasoningTokens  int64  `json:"reasoning_tokens"`
	TotalCostNUSD    int64  `json:"total_cost_nusd,omitempty"`
	GenerationID     string `json:"generation_id,omitempty"`
	Reconciled       bool   `json:"reconciled,omitempty"`
}

func (u Usage) TotalInput() int64 {
	return u.InputTokens + u.CacheReadTokens + u.CacheWriteTokens
}

func (u Usage) CacheHitRate() float64 {
	total := u.TotalInput()
	if total <= 0 {
		return 0
	}
	return float64(u.CacheReadTokens) / float64(total)
}

func (u *Usage) Add(o Usage) {
	u.InputTokens += o.InputTokens
	u.CacheReadTokens += o.CacheReadTokens
	u.CacheWriteTokens += o.CacheWriteTokens
	u.OutputTokens += o.OutputTokens
	u.ReasoningTokens += o.ReasoningTokens
	u.TotalCostNUSD += o.TotalCostNUSD
}

type Message struct {
	ID         string    `json:"id"`
	SessionID  string    `json:"session_id"`
	RunID      string    `json:"run_id,omitempty"`
	Seq        int64     `json:"seq"`
	Role       Role      `json:"role"`
	Blocks     []Block   `json:"blocks"`
	ModelID    string    `json:"model_id,omitempty"`
	ProviderID string    `json:"provider_id,omitempty"`
	StopReason string    `json:"stop_reason,omitempty"`
	Usage      *Usage    `json:"usage,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

type TodoStatus string

const (
	TodoPending    TodoStatus = "pending"
	TodoInProgress TodoStatus = "in_progress"
	TodoCompleted  TodoStatus = "completed"
)

type TodoItem struct {
	ID      string     `json:"id"`
	Content string     `json:"content"`
	Status  TodoStatus `json:"status"`
}

type ApprovalMode string

const (
	ApprovalAsk      ApprovalMode = "ask"
	ApprovalAutoRead ApprovalMode = "auto_read"
	ApprovalYolo     ApprovalMode = "yolo"
)

type ApprovalDecision string

const (
	DecisionPending      ApprovalDecision = "pending"
	DecisionApproveOnce  ApprovalDecision = "approve_once"
	DecisionApproveAlways ApprovalDecision = "approve_always"
	DecisionDeny         ApprovalDecision = "deny"
)

type Approval struct {
	ID        string           `json:"id"`
	SessionID string           `json:"session_id"`
	RunID     string           `json:"run_id"`
	ToolUseID string           `json:"tool_use_id"`
	ToolName  string           `json:"tool_name"`
	ArgsHash  string           `json:"args_hash"`
	ArgsJSON  json.RawMessage  `json:"args_json"`
	Reason    string           `json:"reason,omitempty"`
	Decision  ApprovalDecision `json:"decision"`
	CreatedAt time.Time        `json:"created_at"`
	ResolvedAt *time.Time      `json:"resolved_at,omitempty"`
}

type RunStatus string

const (
	RunQueued          RunStatus = "queued"
	RunRunning         RunStatus = "running"
	RunWaitingApproval RunStatus = "waiting_approval"
	RunCompleted       RunStatus = "completed"
	RunFailed          RunStatus = "failed"
	RunCancelled       RunStatus = "cancelled"
	RunMaxTurns        RunStatus = "max_turns"
)

type Run struct {
	ID         string    `json:"id"`
	SessionID  string    `json:"session_id"`
	ParentID   string    `json:"parent_id,omitempty"`
	Prompt     string    `json:"prompt"`
	ProviderID string    `json:"provider_id"`
	ModelID    string    `json:"model_id"`
	Status     RunStatus `json:"status"`
	Turns      int       `json:"turns"`
	MaxTurns   int       `json:"max_turns"`
	Error      string    `json:"error,omitempty"`
	Usage      Usage     `json:"usage"`
	StartedAt  time.Time `json:"started_at"`
	EndedAt    *time.Time `json:"ended_at,omitempty"`
}

type SessionMeta struct {
	ID               string       `json:"id"`
	Title            string       `json:"title"`
	WorkspaceDir     string       `json:"workspace_dir"`
	ProviderID       string       `json:"provider_id"`
	ModelID          string       `json:"model_id"`
	ApprovalMode     ApprovalMode `json:"approval_mode"`
	ParentSessionID  string       `json:"parent_session_id,omitempty"`
	ForkedFromSeq    int64        `json:"forked_from_seq,omitempty"`
	AllowedTools     []string     `json:"allowed_tools,omitempty"`
	Todos            []TodoItem   `json:"todos,omitempty"`
	TotalUsage       Usage        `json:"total_usage"`
	LastSeq          int64        `json:"last_seq"`
	MessageCount     int          `json:"message_count"`
	ActiveRunID      string       `json:"active_run_id,omitempty"`
	Archived         bool         `json:"archived,omitempty"`
	CreatedAt        time.Time    `json:"created_at"`
	UpdatedAt        time.Time    `json:"updated_at"`
}

// SessionEvent is the durable append-only event record in events.jsonl and over SSE.
type SessionEvent struct {
	Seq       int64           `json:"seq"`
	SessionID string          `json:"session_id"`
	RunID     string          `json:"run_id,omitempty"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
	Timestamp time.Time       `json:"timestamp"`
}

type SessionSnapshot struct {
	Meta             SessionMeta `json:"meta"`
	Messages         []Message   `json:"messages"`
	Runs             []Run       `json:"runs"`
	PendingApprovals []Approval  `json:"pending_approvals"`
}

type LogEntry struct {
	Timestamp time.Time      `json:"ts"`
	Level     string         `json:"level"`
	Component string         `json:"component"`
	SessionID string         `json:"session_id,omitempty"`
	RunID     string         `json:"run_id,omitempty"`
	Message   string         `json:"msg"`
	Fields    map[string]any `json:"fields,omitempty"`
}

func NewID(prefix string) string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("%s_%s", prefix, hex.EncodeToString(b[:]))
}

func HashText(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:8])
}

func LineHash4(line string) string {
	sum := sha256.Sum256([]byte(line))
	return hex.EncodeToString(sum[:2])
}

// CanonicalJSON produces deterministic key-sorted JSON so prompt prefixes and doom-loop hashes never jitter.
func CanonicalJSON(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var decoded any
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	if err := dec.Decode(&decoded); err != nil {
		return raw, nil
	}
	return json.Marshal(normalizeJSONValue(decoded))
}

func normalizeJSONValue(v any) any {
	switch x := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out := make(map[string]any, len(x))
		for _, k := range keys {
			out[k] = normalizeJSONValue(x[k])
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = normalizeJSONValue(item)
		}
		return out
	default:
		return v
	}
}
