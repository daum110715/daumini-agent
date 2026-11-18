package tools

import (
	"context"
	"encoding/json"
	"sort"
	"sync"

	"github.com/daum110715/daumini-agent/internal/sandbox"
	"github.com/daum110715/daumini-agent/internal/types"
)

type ToolSpec struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type ToolResult struct {
	Content       string
	IsError       bool
	UpdatedTodos  []types.TodoItem
	SubagentTrace string
}

type ExecContext struct {
	SessionID string
	RunID     string
	Sandbox   *sandbox.Sandbox
	Reads     *ReadTracker
	Todos     []types.TodoItem
	Depth     int
}

type Tool interface {
	Spec() ToolSpec
	IsMutating() bool
	Execute(ctx context.Context, ec *ExecContext, args json.RawMessage) ToolResult
}

type ReadTracker struct {
	mu    sync.Mutex
	files map[string]map[string]bool
}

func NewReadTracker() *ReadTracker {
	return &ReadTracker{files: make(map[string]map[string]bool)}
}

func (r *ReadTracker) MarkRead(sessionID, absPath string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.files[sessionID] == nil {
		r.files[sessionID] = make(map[string]bool)
	}
	r.files[sessionID][absPath] = true
}

func (r *ReadTracker) WasRead(sessionID, absPath string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.files[sessionID][absPath]
}

// Registry maintains a canonically sorted tool map so prompt prefixes remain byte-stable across turns.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]Tool)}
}

func (r *Registry) Register(t Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[t.Spec().Name] = t
}

func (r *Registry) UnregisterPrefix(prefix string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for name := range r.tools {
		if len(name) >= len(prefix) && name[:len(prefix)] == prefix {
			delete(r.tools, name)
		}
	}
}

func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

func (r *Registry) Specs() []ToolSpec {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	sort.Strings(names)
	specs := make([]ToolSpec, 0, len(names))
	for _, name := range names {
		s := r.tools[name].Spec()
		if canon, err := types.CanonicalJSON(s.Parameters); err == nil {
			s.Parameters = canon
		}
		specs = append(specs, s)
	}
	return specs
}

func (r *Registry) CloneWithout(exclude ...string) *Registry {
	skip := make(map[string]bool, len(exclude))
	for _, name := range exclude {
		skip[name] = true
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	next := NewRegistry()
	for name, t := range r.tools {
		if !skip[name] {
			next.tools[name] = t
		}
	}
	return next
}
