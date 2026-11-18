package event

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/daum110715/daumini-agent/internal/config"
	"github.com/daum110715/daumini-agent/internal/types"
)

type HookDecision struct {
	Allow       bool
	Message     string
	Reason      string
	InjectTail  string
}

type ToolCallContext struct {
	SessionID string
	RunID     string
	ToolName  string
	ArgsJSON  json.RawMessage
	Result    string
	IsError   bool
	Duration  time.Duration
}

type RunContext struct {
	SessionID string
	RunID     string
	Workspace string
	Prompt    string
	Turns     int
	Error     string
}

// Hook intercepts lifecycle phases cleanly without hiding core control flow inside Cordis-style proxies.
type Hook interface {
	Name() string
	BeforeRun(ctx context.Context, rc RunContext) HookDecision
	AfterRun(ctx context.Context, rc RunContext)
	BeforeTool(ctx context.Context, tc ToolCallContext) HookDecision
	AfterTool(ctx context.Context, tc ToolCallContext)
	StopCheck(ctx context.Context, rc RunContext) HookDecision
}

type BaseHook struct{ HookName string }

func (b BaseHook) Name() string                                               { return b.HookName }
func (b BaseHook) BeforeRun(context.Context, RunContext) HookDecision         { return HookDecision{Allow: true} }
func (b BaseHook) AfterRun(context.Context, RunContext)                       {}
func (b BaseHook) BeforeTool(context.Context, ToolCallContext) HookDecision   { return HookDecision{Allow: true} }
func (b BaseHook) AfterTool(context.Context, ToolCallContext)                 {}
func (b BaseHook) StopCheck(context.Context, RunContext) HookDecision         { return HookDecision{Allow: true} }

// Listener observes domain events emitted by the core loop.
type Listener func(types.SessionEvent)

type Bus struct {
	mu        sync.RWMutex
	hooks     []Hook
	listeners []Listener
	extHooks  []config.HookConfig
}

func NewBus(ext []config.HookConfig) *Bus {
	return &Bus{extHooks: append([]config.HookConfig(nil), ext...)}
}

func (b *Bus) SetExternalHooks(ext []config.HookConfig) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.extHooks = append([]config.HookConfig(nil), ext...)
}

func (b *Bus) RegisterHook(h Hook) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.hooks = append(b.hooks, h)
}

func (b *Bus) Subscribe(fn Listener) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.listeners = append(b.listeners, fn)
}

func (b *Bus) Emit(ev types.SessionEvent) {
	b.mu.RLock()
	ls := append([]Listener(nil), b.listeners...)
	b.mu.RUnlock()
	for _, fn := range ls {
		fn(ev)
	}
}

func (b *Bus) BeforeRun(ctx context.Context, rc RunContext) HookDecision {
	b.mu.RLock()
	hooks := append([]Hook(nil), b.hooks...)
	ext := append([]config.HookConfig(nil), b.extHooks...)
	b.mu.RUnlock()
	for _, h := range hooks {
		if d := h.BeforeRun(ctx, rc); !d.Allow {
			return d
		}
	}
	return runExternalHooks(ctx, ext, "pre_run", "", rc)
}

func (b *Bus) AfterRun(ctx context.Context, rc RunContext) {
	b.mu.RLock()
	hooks := append([]Hook(nil), b.hooks...)
	ext := append([]config.HookConfig(nil), b.extHooks...)
	b.mu.RUnlock()
	for _, h := range hooks {
		h.AfterRun(ctx, rc)
	}
	_ = runExternalHooks(ctx, ext, "post_run", "", rc)
}

func (b *Bus) BeforeTool(ctx context.Context, tc ToolCallContext) HookDecision {
	b.mu.RLock()
	hooks := append([]Hook(nil), b.hooks...)
	ext := append([]config.HookConfig(nil), b.extHooks...)
	b.mu.RUnlock()
	for _, h := range hooks {
		if d := h.BeforeTool(ctx, tc); !d.Allow {
			return d
		}
	}
	return runExternalHooks(ctx, ext, "pre_tool", tc.ToolName, tc)
}

func (b *Bus) AfterTool(ctx context.Context, tc ToolCallContext) {
	b.mu.RLock()
	hooks := append([]Hook(nil), b.hooks...)
	ext := append([]config.HookConfig(nil), b.extHooks...)
	b.mu.RUnlock()
	for _, h := range hooks {
		h.AfterTool(ctx, tc)
	}
	_ = runExternalHooks(ctx, ext, "post_tool", tc.ToolName, tc)
}

func (b *Bus) StopCheck(ctx context.Context, rc RunContext) HookDecision {
	b.mu.RLock()
	hooks := append([]Hook(nil), b.hooks...)
	ext := append([]config.HookConfig(nil), b.extHooks...)
	b.mu.RUnlock()
	for _, h := range hooks {
		if d := h.StopCheck(ctx, rc); !d.Allow {
			return d
		}
	}
	return runExternalHooks(ctx, ext, "stop_check", "", rc)
}

type extHookOutput struct {
	Allow      *bool  `json:"allow"`
	Reason     string `json:"reason"`
	Message    string `json:"message"`
	InjectTail string `json:"inject_tail"`
}

func runExternalHooks(ctx context.Context, hooks []config.HookConfig, eventName, toolName string, payload any) HookDecision {
	for _, hc := range hooks {
		if !hc.Enabled || hc.Event != eventName {
			continue
		}
		if hc.MatchTool != "" && toolName != "" && !strings.Contains(toolName, hc.MatchTool) {
			continue
		}
		timeout := 15 * time.Second
		if hc.TimeoutS > 0 {
			timeout = time.Duration(hc.TimeoutS) * time.Second
		}
		hCtx, cancel := context.WithTimeout(ctx, timeout)
		inBytes, _ := json.Marshal(map[string]any{
			"event":   eventName,
			"payload": payload,
		})
		cmd := exec.CommandContext(hCtx, hc.Command, hc.Args...)
		cmd.Stdin = bytes.NewReader(inBytes)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err := cmd.Run()
		cancel()

		outStr := strings.TrimSpace(stdout.String())
		if err != nil {
			msg := strings.TrimSpace(stderr.String())
			if msg == "" {
				msg = err.Error()
			}
			return HookDecision{
				Allow:   false,
				Reason:  fmt.Sprintf("hook %s blocked (%s)", hc.Name, msg),
				Message: outStr,
			}
		}
		if strings.HasPrefix(outStr, "{") {
			var parsed extHookOutput
			if json.Unmarshal([]byte(outStr), &parsed) == nil {
				if parsed.Allow != nil && !*parsed.Allow {
					return HookDecision{
						Allow:      false,
						Reason:     parsed.Reason,
						Message:    parsed.Message,
						InjectTail: parsed.InjectTail,
					}
				}
				if parsed.InjectTail != "" {
					return HookDecision{Allow: true, InjectTail: parsed.InjectTail}
				}
			}
		}
	}
	return HookDecision{Allow: true}
}

// DoomLoopHook breaks silent repetition when the same tool + canonical args repeats N times.
type DoomLoopHook struct {
	BaseHook
	mu        sync.Mutex
	threshold int
	recent    map[string][]string
}

func NewDoomLoopHook(threshold int) *DoomLoopHook {
	if threshold <= 1 {
		threshold = 3
	}
	return &DoomLoopHook{
		BaseHook:  BaseHook{HookName: "doom_loop"},
		threshold: threshold,
		recent:    make(map[string][]string),
	}
}

func (d *DoomLoopHook) BeforeTool(_ context.Context, tc ToolCallContext) HookDecision {
	canon, err := types.CanonicalJSON(tc.ArgsJSON)
	if err != nil {
		canon = tc.ArgsJSON
	}
	sig := tc.ToolName + ":" + types.HashText(string(canon))

	d.mu.Lock()
	defer d.mu.Unlock()
	list := append(d.recent[tc.SessionID], sig)
	if len(list) > d.threshold+4 {
		list = list[len(list)-(d.threshold+4):]
	}
	d.recent[tc.SessionID] = list

	if len(list) >= d.threshold {
		allSame := true
		for i := len(list) - d.threshold; i < len(list); i++ {
			if list[i] != sig {
				allSame = false
				break
			}
		}
		if allSame {
			return HookDecision{
				Allow:  false,
				Reason: fmt.Sprintf("doom loop detected: tool %q called %d consecutive times with identical arguments; change approach or explain the blocker", tc.ToolName, d.threshold),
			}
		}
	}
	return HookDecision{Allow: true}
}

func (d *DoomLoopHook) ResetSession(sessionID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.recent, sessionID)
}
