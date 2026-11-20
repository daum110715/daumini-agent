package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/daum110715/daumini-agent/internal/compaction"
	"github.com/daum110715/daumini-agent/internal/config"
	"github.com/daum110715/daumini-agent/internal/event"
	"github.com/daum110715/daumini-agent/internal/llm"
	"github.com/daum110715/daumini-agent/internal/mcp"
	"github.com/daum110715/daumini-agent/internal/sandbox"
	"github.com/daum110715/daumini-agent/internal/session"
	"github.com/daum110715/daumini-agent/internal/skills"
	"github.com/daum110715/daumini-agent/internal/tools"
	"github.com/daum110715/daumini-agent/internal/types"
)

const staticSystemPrompt = `Local coding agent. Call fs_read before fs_edit/fs_write; pass exact line#hash anchors to fs_edit. Keep edits minimal. Report only verified tool output.`

type Manager struct {
	cfgMgr   *config.Manager
	store    *session.Store
	bus      *event.Bus
	doom     *event.DoomLoopHook
	llm      *llm.Client
	reg      *tools.Registry
	reads    *tools.ReadTracker
	skills   *skills.Manager
	mcpMgr   *mcp.Manager

	mu        sync.Mutex
	active    map[string]*activeRun
	approvals map[string]chan types.ApprovalDecision
	subs      map[string]map[chan types.SessionEvent]struct{}
	queues    map[string][]queuedPrompt
	steers    map[string][]string
}

type activeRun struct {
	runID  string
	cancel context.CancelFunc
	done   chan struct{}
}

type queuedPrompt struct {
	prompt     string
	providerID string
	modelID    string
}

func NewManager(cfgMgr *config.Manager, store *session.Store) *Manager {
	cfg := cfgMgr.Get()
	bus := event.NewBus(cfg.Hooks)
	doom := event.NewDoomLoopHook(3)
	bus.RegisterHook(doom)

	reg := tools.NewRegistry()
	tools.RegisterBuiltins(reg)
	sk := skills.NewManager(cfgMgr.DataDir())
	reg.Register(skills.NewReadTool(sk))

	m := &Manager{
		cfgMgr:    cfgMgr,
		store:     store,
		bus:       bus,
		doom:      doom,
		llm:       llm.NewClient(),
		reg:       reg,
		reads:     tools.NewReadTracker(),
		skills:    sk,
		mcpMgr:    mcp.NewManager(),
		active:    make(map[string]*activeRun),
		approvals: make(map[string]chan types.ApprovalDecision),
		subs:      make(map[string]map[chan types.SessionEvent]struct{}),
		queues:    make(map[string][]queuedPrompt),
		steers:    make(map[string][]string),
	}
	reg.Register(&subagentTool{mgr: m})
	m.registerPlugins(cfg.Plugins)
	return m
}

func (m *Manager) RecoverAndInit(ctx context.Context) {
	if repaired, err := m.store.RecoverOrphans(); err == nil && repaired > 0 {
		m.store.Log(types.LogEntry{
			Level:     "info",
			Component: "recovery",
			Message:   fmt.Sprintf("repaired %d interrupted sessions on startup", repaired),
		})
	}
	cfg := m.cfgMgr.Get()
	if len(cfg.MCPServers) > 0 {
		m.mcpMgr.SyncAndRegister(ctx, cfg.MCPServers, m.reg)
	}
}

func (m *Manager) Shutdown() {
	m.mu.Lock()
	runs := make([]*activeRun, 0, len(m.active))
	for _, r := range m.active {
		runs = append(runs, r)
	}
	m.mu.Unlock()
	for _, r := range runs {
		r.cancel()
		select {
		case <-r.done:
		case <-time.After(4 * time.Second):
		}
	}
	m.mcpMgr.CloseAll()
}

func (m *Manager) ReloadExtensions(ctx context.Context) []mcp.ServerStatus {
	cfg := m.cfgMgr.Get()
	m.bus.SetExternalHooks(cfg.Hooks)
	m.reg.UnregisterPrefix("plugin__")
	m.registerPlugins(cfg.Plugins)
	return m.mcpMgr.SyncAndRegister(ctx, cfg.MCPServers, m.reg)
}

func (m *Manager) MCPStatuses() []mcp.ServerStatus {
	return m.mcpMgr.Statuses()
}

func (m *Manager) DiscoverSkills(workspaceDir string) []skills.Skill {
	return m.skills.Discover(workspaceDir)
}

func (m *Manager) ToolSpecs() []tools.ToolSpec {
	return m.reg.Specs()
}

func (m *Manager) ListModels(ctx context.Context, providerID string) ([]llm.ModelInfo, error) {
	p, ok := m.cfgMgr.FindProvider(providerID)
	if !ok {
		return nil, fmt.Errorf("provider %q not found", providerID)
	}
	return m.llm.ListModels(ctx, p)
}

func (m *Manager) CreateSession(workspaceDir, title, providerID, modelID string, mode types.ApprovalMode) (types.SessionMeta, error) {
	cfg := m.cfgMgr.Get()
	if strings.TrimSpace(workspaceDir) == "" {
		workspaceDir = "."
	}
	absDir, err := filepath.Abs(workspaceDir)
	if err != nil {
		return types.SessionMeta{}, err
	}
	if _, err := sandbox.New(absDir); err != nil {
		return types.SessionMeta{}, err
	}
	if providerID == "" {
		if p, ok := m.cfgMgr.PickActiveProvider(""); ok {
			providerID = p.ID
			if modelID == "" {
				modelID = p.DefaultModel
			}
		}
	}
	if modelID == "" {
		modelID = cfg.DefaultModel
	}
	if mode == "" {
		mode = cfg.ApprovalMode
	}
	if strings.TrimSpace(title) == "" {
		title = filepath.Base(absDir)
	}
	m.cfgMgr.AddRecentWorkspace(absDir)
	return m.store.CreateSession(types.SessionMeta{
		Title:        title,
		WorkspaceDir: absDir,
		ProviderID:   providerID,
		ModelID:      modelID,
		ApprovalMode: mode,
	})
}

func (m *Manager) ListSessions(includeArchived bool) ([]types.SessionMeta, error) {
	return m.store.ListSessions(includeArchived)
}

func (m *Manager) GetSnapshot(sessionID string) (types.SessionSnapshot, error) {
	return m.store.GetSnapshot(sessionID)
}

func (m *Manager) DeleteSession(sessionID string) error {
	m.mu.Lock()
	ar := m.active[sessionID]
	m.mu.Unlock()
	if ar != nil {
		ar.cancel()
		select {
		case <-ar.done:
		case <-time.After(2 * time.Second):
		}
	}
	m.mu.Lock()
	if subs := m.subs[sessionID]; subs != nil {
		for ch := range subs {
			delete(subs, ch)
			close(ch)
		}
		delete(m.subs, sessionID)
	}
	delete(m.active, sessionID)
	delete(m.queues, sessionID)
	delete(m.steers, sessionID)
	m.mu.Unlock()
	return m.store.DeleteSession(sessionID)
}

func (m *Manager) UpdateSessionSettings(sessionID string, providerID, modelID string, reasoningEffort *string, mode types.ApprovalMode, workspaceDir string, archived *bool, title string) (types.SessionMeta, error) {
	if strings.TrimSpace(workspaceDir) != "" {
		absDir, err := filepath.Abs(workspaceDir)
		if err != nil {
			return types.SessionMeta{}, err
		}
		if _, err := sandbox.New(absDir); err != nil {
			return types.SessionMeta{}, err
		}
		workspaceDir = absDir
		m.cfgMgr.AddRecentWorkspace(absDir)
	}
	snap, err := m.store.UpdateSnapshot(sessionID, func(s *types.SessionSnapshot) error {
		if providerID != "" {
			s.Meta.ProviderID = providerID
		}
		if modelID != "" {
			s.Meta.ModelID = modelID
		}
		if reasoningEffort != nil {
			s.Meta.ReasoningEffort = strings.TrimSpace(*reasoningEffort)
		}
		if mode != "" {
			s.Meta.ApprovalMode = mode
		}
		if workspaceDir != "" {
			s.Meta.WorkspaceDir = workspaceDir
		}
		if archived != nil {
			s.Meta.Archived = *archived
		}
		if strings.TrimSpace(title) != "" {
			s.Meta.Title = strings.TrimSpace(title)
		}
		return nil
	})
	if err != nil {
		return types.SessionMeta{}, err
	}
	m.emit(sessionID, "", "session.updated", snap.Meta)
	return snap.Meta, nil
}

func (m *Manager) ForkSession(sessionID string, upToSeq int64, title string) (types.SessionSnapshot, error) {
	return m.store.ForkSession(sessionID, upToSeq, title)
}

func (m *Manager) ReadEvents(sessionID string, afterSeq int64) ([]types.SessionEvent, error) {
	return m.store.ReadEventsAfter(sessionID, afterSeq)
}

func (m *Manager) Subscribe(sessionID string, afterSeq int64) (<-chan types.SessionEvent, []types.SessionEvent, types.SessionSnapshot, func(), error) {
	snap, err := m.store.GetSnapshot(sessionID)
	if err != nil {
		return nil, nil, types.SessionSnapshot{}, nil, err
	}
	missed, err := m.store.ReadEventsAfter(sessionID, afterSeq)
	if err != nil {
		return nil, nil, types.SessionSnapshot{}, nil, err
	}
	ch := make(chan types.SessionEvent, 128)
	m.mu.Lock()
	if m.subs[sessionID] == nil {
		m.subs[sessionID] = make(map[chan types.SessionEvent]struct{})
	}
	m.subs[sessionID][ch] = struct{}{}
	m.mu.Unlock()

	unsub := func() {
		var shouldClose bool
		m.mu.Lock()
		if set := m.subs[sessionID]; set != nil {
			if _, ok := set[ch]; ok {
				delete(set, ch)
				shouldClose = true
			}
		}
		m.mu.Unlock()
		if shouldClose {
			close(ch)
		}
	}
	return ch, missed, snap, unsub, nil
}

// SendPrompt starts a new run or queues/steers if a run is already active (mode: "queue" | "steer" | "interrupt").
func (m *Manager) SendPrompt(ctx context.Context, sessionID, prompt, providerID, modelID, mode string) (types.Run, error) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return types.Run{}, fmt.Errorf("prompt cannot be empty")
	}
	snap, err := m.store.GetSnapshot(sessionID)
	if err != nil {
		return types.Run{}, err
	}

	m.mu.Lock()
	cur := m.active[sessionID]
	if cur != nil {
		switch mode {
		case "steer":
			m.steers[sessionID] = append(m.steers[sessionID], prompt)
			m.mu.Unlock()
			m.emit(sessionID, cur.runID, "run.steered", map[string]string{"text": prompt})
			return types.Run{ID: cur.runID, SessionID: sessionID, Status: types.RunRunning}, nil
		case "interrupt":
			m.mu.Unlock()
			m.CancelRun(sessionID)
			select {
			case <-cur.done:
			case <-time.After(4 * time.Second):
			}
			return m.startRun(ctx, snap, prompt, providerID, modelID)
		default:
			m.queues[sessionID] = append(m.queues[sessionID], queuedPrompt{
				prompt:     prompt,
				providerID: providerID,
				modelID:    modelID,
			})
			qLen := len(m.queues[sessionID])
			m.mu.Unlock()
			m.emit(sessionID, cur.runID, "run.queued", map[string]any{"queue_len": qLen, "prompt": prompt})
			return types.Run{ID: cur.runID, SessionID: sessionID, Status: types.RunQueued}, nil
		}
	}
	m.mu.Unlock()
	return m.startRun(ctx, snap, prompt, providerID, modelID)
}

func (m *Manager) CancelRun(sessionID string) bool {
	m.mu.Lock()
	cur := m.active[sessionID]
	m.mu.Unlock()
	if cur == nil {
		return false
	}
	cur.cancel()
	return true
}

func (m *Manager) ResolveApproval(sessionID, approvalID string, decision types.ApprovalDecision) error {
	now := time.Now().UTC()
	var resolved types.Approval
	_, err := m.store.UpdateSnapshot(sessionID, func(s *types.SessionSnapshot) error {
		found := false
		for i := range s.PendingApprovals {
			if s.PendingApprovals[i].ID == approvalID {
				s.PendingApprovals[i].Decision = decision
				s.PendingApprovals[i].ResolvedAt = &now
				resolved = s.PendingApprovals[i]
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("approval %q not found", approvalID)
		}
		if decision == types.DecisionApproveAlways && resolved.ToolName != "" {
			has := false
			for _, t := range s.Meta.AllowedTools {
				if t == resolved.ToolName {
					has = true
					break
				}
			}
			if !has {
				s.Meta.AllowedTools = append(s.Meta.AllowedTools, resolved.ToolName)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	m.mu.Lock()
	ch := m.approvals[approvalID]
	delete(m.approvals, approvalID)
	m.mu.Unlock()
	if ch != nil {
		select {
		case ch <- decision:
		default:
		}
	}
	m.emit(sessionID, resolved.RunID, "approval.resolved", resolved)
	return nil
}

func (m *Manager) startRun(_ context.Context, snap types.SessionSnapshot, prompt, providerID, modelID string) (types.Run, error) {
	cfg := m.cfgMgr.Get()
	if providerID == "" {
		providerID = snap.Meta.ProviderID
	}
	prov, ok := m.cfgMgr.PickActiveProvider(providerID)
	if !ok {
		return types.Run{}, fmt.Errorf("no enabled LLM provider found")
	}
	if modelID == "" {
		if snap.Meta.ModelID != "" && snap.Meta.ProviderID == prov.ID {
			modelID = snap.Meta.ModelID
		} else {
			modelID = prov.DefaultModel
		}
	}
	if modelID == "" {
		modelID = cfg.DefaultModel
	}

	run := types.Run{
		ID:         types.NewID("run"),
		SessionID:  snap.Meta.ID,
		Prompt:     prompt,
		ProviderID: prov.ID,
		ModelID:    modelID,
		Status:     types.RunRunning,
		MaxTurns:   cfg.MaxTurns,
		StartedAt:  time.Now().UTC(),
	}

	runCtx, cancel := context.WithCancel(context.Background())
	ar := &activeRun{
		runID:  run.ID,
		cancel: cancel,
		done:   make(chan struct{}),
	}

	m.mu.Lock()
	if m.active[snap.Meta.ID] != nil {
		m.mu.Unlock()
		cancel()
		return types.Run{}, fmt.Errorf("session already has an active run")
	}
	m.active[snap.Meta.ID] = ar
	m.mu.Unlock()

	_, err := m.store.UpdateSnapshot(snap.Meta.ID, func(s *types.SessionSnapshot) error {
		s.Meta.ActiveRunID = run.ID
		s.Meta.ProviderID = prov.ID
		s.Meta.ModelID = modelID
		s.Meta.LastSeq++
		s.Messages = append(s.Messages, types.Message{
			ID:        types.NewID("msg"),
			SessionID: snap.Meta.ID,
			RunID:     run.ID,
			Seq:       s.Meta.LastSeq,
			Role:      types.RoleUser,
			Blocks:    []types.Block{{Type: types.BlockText, Text: prompt}},
			CreatedAt: time.Now().UTC(),
		})
		s.Runs = append(s.Runs, run)
		return nil
	})
	if err != nil {
		m.mu.Lock()
		delete(m.active, snap.Meta.ID)
		m.mu.Unlock()
		close(ar.done)
		cancel()
		return types.Run{}, err
	}

	m.doom.ResetSession(snap.Meta.ID)
	m.emit(snap.Meta.ID, run.ID, "run.started", run)

	go m.executeLoop(runCtx, ar, snap.Meta.ID, run, prov)
	return run, nil
}

func (m *Manager) executeLoop(ctx context.Context, ar *activeRun, sessionID string, run types.Run, prov config.ProviderConfig) {
	defer func() {
		cancel := ar.cancel
		cancel()
		var next *queuedPrompt
		m.mu.Lock()
		delete(m.active, sessionID)
		if q := m.queues[sessionID]; len(q) > 0 {
			item := q[0]
			m.queues[sessionID] = q[1:]
			next = &item
		}
		m.mu.Unlock()
		close(ar.done)

		if next != nil {
			if snap, err := m.store.GetSnapshot(sessionID); err == nil {
				_, _ = m.startRun(context.Background(), snap, next.prompt, next.providerID, next.modelID)
			}
		}
	}()

	rc := event.RunContext{
		SessionID: sessionID,
		RunID:     run.ID,
		Prompt:    run.Prompt,
	}
	if bd := m.bus.BeforeRun(ctx, rc); !bd.Allow {
		m.finishRun(sessionID, &run, types.RunFailed, bd.Reason)
		return
	}

	sb, err := m.sandboxForSession(sessionID)
	if err != nil {
		m.finishRun(sessionID, &run, types.RunFailed, err.Error())
		return
	}

	var lastInputTokens int64
	for turn := 1; turn <= run.MaxTurns; turn++ {
		if ctx.Err() != nil {
			m.finishRun(sessionID, &run, types.RunCancelled, "cancelled by user")
			return
		}
		run.Turns = turn
		m.drainSteers(sessionID, run.ID)

		snap, err := m.store.GetSnapshot(sessionID)
		if err != nil {
			m.finishRun(sessionID, &run, types.RunFailed, err.Error())
			return
		}

		// Apply infrequent batch observation masking without breaking prefix every turn.
		maskedMsgs, maskedCount := compaction.MaskObservationsBatch(snap.Messages, 128000, lastInputTokens, false)
		if maskedCount > 0 {
			_, _ = m.store.UpdateSnapshot(sessionID, func(s *types.SessionSnapshot) error {
				s.Messages = maskedMsgs
				return nil
			})
			m.emit(sessionID, run.ID, "compaction.masked", map[string]int{"count": maskedCount})
			snap.Messages = maskedMsgs
		}

		llmMsgs := m.appendTailReminder(snap, run.MaxTurns-turn)
		var textStream strings.Builder
		var thinkStream strings.Builder
		resp, err := m.llm.Generate(ctx, llm.GenerateRequest{
			Provider:        prov,
			ModelID:         run.ModelID,
			ReasoningEffort: snap.Meta.ReasoningEffort,
			SystemPrompt:    staticSystemPrompt + extraSystem(m.cfgMgr.Get().SystemExtra),
			Messages:        llmMsgs,
			Tools:           m.reg.Specs(),
			PromptCacheKey:  sessionID,
			Callbacks: llm.StreamCallbacks{
				OnTextDelta: func(d string) {
					textStream.WriteString(d)
					m.emit(sessionID, run.ID, "assistant.text_delta", map[string]string{"delta": d})
				},
				OnThinkingDelta: func(d string) {
					thinkStream.WriteString(d)
					m.emit(sessionID, run.ID, "assistant.thinking_delta", map[string]string{"delta": d})
				},
			},
		})
		if err != nil {
			if ctx.Err() != nil {
				m.finishRun(sessionID, &run, types.RunCancelled, "cancelled by user")
				return
			}
			m.finishRun(sessionID, &run, types.RunFailed, err.Error())
			return
		}

		run.Usage.Add(resp.Usage)
		lastInputTokens = resp.Usage.TotalInput()

		var assistantMsg types.Message
		_, err = m.store.UpdateSnapshot(sessionID, func(s *types.SessionSnapshot) error {
			s.Meta.LastSeq++
			s.Meta.TotalUsage.Add(resp.Usage)
			assistantMsg = types.Message{
				ID:         types.NewID("msg"),
				SessionID:  sessionID,
				RunID:      run.ID,
				Seq:        s.Meta.LastSeq,
				Role:       types.RoleAssistant,
				Blocks:     resp.Blocks,
				ModelID:    run.ModelID,
				ProviderID: prov.ID,
				StopReason: resp.StopReason,
				Usage:      &resp.Usage,
				CreatedAt:  time.Now().UTC(),
			}
			s.Messages = append(s.Messages, assistantMsg)
			return nil
		})
		if err != nil {
			m.finishRun(sessionID, &run, types.RunFailed, err.Error())
			return
		}
		m.emit(sessionID, run.ID, "assistant.message", assistantMsg)

		// Handle Anthropic pause_turn continuation before ending.
		if resp.StopReason == "pause_turn" {
			continue
		}

		var toolUses []types.Block
		for _, b := range resp.Blocks {
			if b.Type == types.BlockToolUse {
				toolUses = append(toolUses, b)
			}
		}
		if len(toolUses) == 0 {
			// Guard against Anthropic refusal / OpenAI incomplete / empty output or StopCheck hook.
			if resp.StopReason == "refusal" {
				m.finishRun(sessionID, &run, types.RunFailed, "model refused the request")
				return
			}
			stopDec := m.bus.StopCheck(ctx, event.RunContext{
				SessionID: sessionID,
				RunID:     run.ID,
				Workspace: sb.Root(),
				Prompt:    run.Prompt,
				Turns:     turn,
			})
			if !stopDec.Allow {
				m.appendNoticeMessage(sessionID, run.ID, "Stop hook prevented completion:\n"+stopDec.Reason)
				continue
			}
			m.finishRun(sessionID, &run, types.RunCompleted, "")
			return
		}

		// Execute requested tools sequentially inside the session context, preserving strict tool_use -> tool_result pairing.
		var results []types.Block
		for _, tu := range toolUses {
			resBlock := m.runSingleTool(ctx, sessionID, run.ID, sb, tu)
			results = append(results, resBlock)
			if ctx.Err() != nil {
				break
			}
		}
		// Ensure every tool_use has a matching tool_result even on cancellation.
		if len(results) < len(toolUses) {
			for i := len(results); i < len(toolUses); i++ {
				results = append(results, types.Block{
					Type:      types.BlockToolResult,
					ToolUseID: toolUses[i].ID,
					Text:      "tool execution cancelled",
					IsError:   true,
				})
			}
		}

		var toolMsg types.Message
		_, err = m.store.UpdateSnapshot(sessionID, func(s *types.SessionSnapshot) error {
			s.Meta.LastSeq++
			toolMsg = types.Message{
				ID:        types.NewID("msg"),
				SessionID: sessionID,
				RunID:     run.ID,
				Seq:       s.Meta.LastSeq,
				Role:      types.RoleUser,
				Blocks:    results,
				CreatedAt: time.Now().UTC(),
			}
			s.Messages = append(s.Messages, toolMsg)
			return nil
		})
		if err != nil {
			m.finishRun(sessionID, &run, types.RunFailed, err.Error())
			return
		}
		m.emit(sessionID, run.ID, "tool.results", toolMsg)
	}

	m.finishRun(sessionID, &run, types.RunMaxTurns, fmt.Sprintf("reached max_turns limit (%d)", run.MaxTurns))
}

func (m *Manager) runSingleTool(ctx context.Context, sessionID, runID string, sb *sandbox.Sandbox, tu types.Block) types.Block {
	start := time.Now()
	m.emit(sessionID, runID, "tool.started", map[string]any{
		"tool_use_id": tu.ID,
		"name":        tu.Name,
		"input":       tu.Input,
	})

	toolImpl, ok := m.reg.Get(tu.Name)
	if !ok {
		res := types.Block{
			Type:      types.BlockToolResult,
			ToolUseID: tu.ID,
			Text:      fmt.Sprintf("unknown tool %q", tu.Name),
			IsError:   true,
		}
		m.emit(sessionID, runID, "tool.finished", map[string]any{"tool_use_id": tu.ID, "name": tu.Name, "is_error": true, "output": res.Text})
		return res
	}

	tc := event.ToolCallContext{
		SessionID: sessionID,
		RunID:     runID,
		ToolName:  tu.Name,
		ArgsJSON:  tu.Input,
	}
	hd := m.bus.BeforeTool(ctx, tc)
	if !hd.Allow {
		res := types.Block{
			Type:      types.BlockToolResult,
			ToolUseID: tu.ID,
			Text:      hd.Reason,
			IsError:   true,
		}
		m.emit(sessionID, runID, "tool.finished", map[string]any{"tool_use_id": tu.ID, "name": tu.Name, "is_error": true, "output": res.Text})
		return res
	}

	if err := m.awaitApprovalIfNeeded(ctx, sessionID, runID, toolImpl, tu); err != nil {
		res := types.Block{
			Type:      types.BlockToolResult,
			ToolUseID: tu.ID,
			Text:      err.Error(),
			IsError:   true,
		}
		m.emit(sessionID, runID, "tool.finished", map[string]any{"tool_use_id": tu.ID, "name": tu.Name, "is_error": true, "output": res.Text})
		return res
	}

	snap, _ := m.store.GetSnapshot(sessionID)
	ec := &tools.ExecContext{
		SessionID: sessionID,
		RunID:     runID,
		Sandbox:   sb,
		Reads:     m.reads,
		Todos:     snap.Meta.Todos,
		Depth:     0,
	}
	out := toolImpl.Execute(ctx, ec, tu.Input)
	if out.UpdatedTodos != nil {
		_, _ = m.store.UpdateSnapshot(sessionID, func(s *types.SessionSnapshot) error {
			s.Meta.Todos = out.UpdatedTodos
			return nil
		})
		m.emit(sessionID, runID, "todo.updated", out.UpdatedTodos)
	}

	tc.Result = out.Content
	tc.IsError = out.IsError
	tc.Duration = time.Since(start)
	m.bus.AfterTool(ctx, tc)

	m.emit(sessionID, runID, "tool.finished", map[string]any{
		"tool_use_id": tu.ID,
		"name":        tu.Name,
		"is_error":    out.IsError,
		"duration_ms": tc.Duration.Milliseconds(),
		"output":      out.Content,
	})
	return types.Block{
		Type:      types.BlockToolResult,
		ToolUseID: tu.ID,
		Text:      out.Content,
		IsError:   out.IsError,
	}
}

func (m *Manager) awaitApprovalIfNeeded(ctx context.Context, sessionID, runID string, impl tools.Tool, tu types.Block) error {
	snap, err := m.store.GetSnapshot(sessionID)
	if err != nil {
		return err
	}
	if snap.Meta.ApprovalMode == types.ApprovalYolo {
		return nil
	}
	if snap.Meta.ApprovalMode == types.ApprovalAutoRead && !impl.IsMutating() {
		return nil
	}
	for _, allowed := range snap.Meta.AllowedTools {
		if allowed == tu.Name {
			return nil
		}
	}

	canon, err := types.CanonicalJSON(tu.Input)
	if err != nil {
		canon = tu.Input
	}
	ap := types.Approval{
		ID:        types.NewID("apr"),
		SessionID: sessionID,
		RunID:     runID,
		ToolUseID: tu.ID,
		ToolName:  tu.Name,
		ArgsHash:  types.HashText(string(canon)),
		ArgsJSON:  tu.Input,
		Decision:  types.DecisionPending,
		CreatedAt: time.Now().UTC(),
	}
	ch := make(chan types.ApprovalDecision, 1)
	m.mu.Lock()
	m.approvals[ap.ID] = ch
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.approvals, ap.ID)
		m.mu.Unlock()
	}()

	_, _ = m.store.UpdateSnapshot(sessionID, func(s *types.SessionSnapshot) error {
		s.PendingApprovals = append(s.PendingApprovals, ap)
		for i := range s.Runs {
			if s.Runs[i].ID == runID {
				s.Runs[i].Status = types.RunWaitingApproval
			}
		}
		return nil
	})
	m.emit(sessionID, runID, "approval.requested", ap)

	select {
	case dec := <-ch:
		_, _ = m.store.UpdateSnapshot(sessionID, func(s *types.SessionSnapshot) error {
			for i := range s.Runs {
				if s.Runs[i].ID == runID && s.Runs[i].Status == types.RunWaitingApproval {
					s.Runs[i].Status = types.RunRunning
				}
			}
			return nil
		})
		if dec == types.DecisionApproveOnce || dec == types.DecisionApproveAlways {
			return nil
		}
		return fmt.Errorf("tool %q denied by user", tu.Name)
	case <-ctx.Done():
		return fmt.Errorf("approval cancelled")
	}
}

func (m *Manager) finishRun(sessionID string, run *types.Run, status types.RunStatus, errMsg string) {
	now := time.Now().UTC()
	run.Status = status
	run.Error = errMsg
	run.EndedAt = &now

	_, _ = m.store.UpdateSnapshot(sessionID, func(s *types.SessionSnapshot) error {
		s.Meta.ActiveRunID = ""
		for i := range s.Runs {
			if s.Runs[i].ID == run.ID {
				s.Runs[i] = *run
				break
			}
		}
		return nil
	})

	m.bus.AfterRun(context.Background(), event.RunContext{
		SessionID: sessionID,
		RunID:     run.ID,
		Prompt:    run.Prompt,
		Turns:     run.Turns,
		Error:     errMsg,
	})
	m.emit(sessionID, run.ID, "run.finished", run)
}

func (m *Manager) sandboxForSession(sessionID string) (*sandbox.Sandbox, error) {
	snap, err := m.store.GetSnapshot(sessionID)
	if err != nil {
		return nil, err
	}
	return sandbox.New(snap.Meta.WorkspaceDir)
}

func (m *Manager) drainSteers(sessionID, runID string) {
	m.mu.Lock()
	steers := m.steers[sessionID]
	delete(m.steers, sessionID)
	m.mu.Unlock()
	for _, s := range steers {
		m.appendNoticeMessage(sessionID, runID, "[User steer instruction]\n"+s)
	}
}

func (m *Manager) appendNoticeMessage(sessionID, runID, text string) {
	var msg types.Message
	_, err := m.store.UpdateSnapshot(sessionID, func(s *types.SessionSnapshot) error {
		s.Meta.LastSeq++
		msg = types.Message{
			ID:        types.NewID("msg"),
			SessionID: sessionID,
			RunID:     runID,
			Seq:       s.Meta.LastSeq,
			Role:      types.RoleUser,
			Blocks:    []types.Block{{Type: types.BlockSystemNotice, Text: text}},
			CreatedAt: time.Now().UTC(),
		}
		s.Messages = append(s.Messages, msg)
		return nil
	})
	if err == nil {
		m.emit(sessionID, runID, "user.notice", msg)
	}
}

func (m *Manager) appendTailReminder(snap types.SessionSnapshot, remainingTurns int) []types.Message {
	out := make([]types.Message, len(snap.Messages))
	copy(out, snap.Messages)
	if len(out) == 0 {
		return out
	}

	var notes []string
	notes = append(notes, fmt.Sprintf("cwd=%s mode=%s", snap.Meta.WorkspaceDir, snap.Meta.ApprovalMode))
	if remainingTurns <= 2 {
		notes = append(notes, fmt.Sprintf("turns_left=%d wrap_up", remainingTurns))
	}
	if len(snap.Meta.Todos) > 0 {
		var lines []string
		for _, td := range snap.Meta.Todos {
			lines = append(lines, fmt.Sprintf("- [%s] %s (%s)", td.Status, td.Content, td.ID))
		}
		notes = append(notes, "todos:\n"+strings.Join(lines, "\n"))
	}
	if skIndex := skills.FormatIndex(m.skills.Discover(snap.Meta.WorkspaceDir)); skIndex != "" {
		notes = append(notes, skIndex)
	}

	reminder := "\n<system-reminder>\n" + strings.Join(notes, "\n") + "\n</system-reminder>"
	lastIdx := len(out) - 1
	if out[lastIdx].Role == types.RoleUser {
		blocks := append([]types.Block(nil), out[lastIdx].Blocks...)
		blocks = append(blocks, types.Block{Type: types.BlockText, Text: reminder})
		out[lastIdx].Blocks = blocks
	}
	return out
}

func (m *Manager) emit(sessionID, runID, evType string, payload any) {
	ev, err := m.store.AppendEvent(sessionID, runID, evType, payload)
	if err != nil {
		return
	}
	m.bus.Emit(ev)
	m.mu.Lock()
	set := m.subs[sessionID]
	chans := make([]chan types.SessionEvent, 0, len(set))
	for ch := range set {
		chans = append(chans, ch)
	}
	m.mu.Unlock()
	for _, ch := range chans {
		select {
		case ch <- ev:
		default:
		}
	}
}

func extraSystem(extra string) string {
	extra = strings.TrimSpace(extra)
	if extra == "" {
		return ""
	}
	return "\n" + extra
}

// subagentTool runs an isolated depth-1 leaf agent without recursive subagent spawning or parent KV-cache pollution.
type subagentTool struct {
	mgr *Manager
}

func (t *subagentTool) IsMutating() bool { return false }
func (t *subagentTool) Spec() tools.ToolSpec {
	return tools.ToolSpec{
		Name:        "subagent",
		Description: "Run an isolated subagent on a bounded task; returns a brief summary.",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"task":{"type":"string"},"read_only":{"type":"boolean"},"max_turns":{"type":"integer"}},"required":["task"]}`),
	}
}

func (t *subagentTool) Execute(ctx context.Context, ec *tools.ExecContext, args json.RawMessage) tools.ToolResult {
	if ec.Depth > 0 {
		return tools.ToolResult{Content: "nested subagents are disabled; solve directly", IsError: true}
	}
	var in struct {
		Task     string `json:"task"`
		ReadOnly *bool  `json:"read_only"`
		MaxTurns int    `json:"max_turns"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return tools.ToolResult{Content: "invalid json: " + err.Error(), IsError: true}
	}
	if strings.TrimSpace(in.Task) == "" {
		return tools.ToolResult{Content: "task is required", IsError: true}
	}
	readOnly := true
	if in.ReadOnly != nil {
		readOnly = *in.ReadOnly
	}
	maxTurns := in.MaxTurns
	if maxTurns <= 0 || maxTurns > 12 {
		maxTurns = 8
	}

	snap, err := t.mgr.store.GetSnapshot(ec.SessionID)
	if err != nil {
		return tools.ToolResult{Content: err.Error(), IsError: true}
	}
	prov, ok := t.mgr.cfgMgr.PickActiveProvider(snap.Meta.ProviderID)
	if !ok {
		return tools.ToolResult{Content: "no active LLM provider configured", IsError: true}
	}
	modelID := snap.Meta.ModelID
	if modelID == "" {
		modelID = prov.DefaultModel
	}

	subReg := t.mgr.reg.CloneWithout("subagent", "todo_update")
	if readOnly {
		subReg = subReg.CloneWithout("fs_edit", "fs_write", "shell_exec")
	}

	msgs := []types.Message{{
		ID:        types.NewID("msg"),
		SessionID: ec.SessionID,
		Role:      types.RoleUser,
		Blocks:    []types.Block{{Type: types.BlockText, Text: in.Task + "\n\nWorkspace root: " + ec.Sandbox.Root()}},
		CreatedAt: time.Now().UTC(),
	}}

	var totalUsage types.Usage
	var finalText string
	for turn := 1; turn <= maxTurns; turn++ {
		if ctx.Err() != nil {
			return tools.ToolResult{Content: "subagent cancelled", IsError: true}
		}
		resp, err := t.mgr.llm.Generate(ctx, llm.GenerateRequest{
			Provider:     prov,
			ModelID:      modelID,
			SystemPrompt: "Concise research subagent. Use tools and return a brief factual summary with file paths.",
			Messages:     msgs,
			Tools:        subReg.Specs(),
		})
		if err != nil {
			return tools.ToolResult{Content: "subagent failed: " + err.Error(), IsError: true}
		}
		totalUsage.Add(resp.Usage)
		msgs = append(msgs, types.Message{
			ID:        types.NewID("msg"),
			SessionID: ec.SessionID,
			Role:      types.RoleAssistant,
			Blocks:    resp.Blocks,
			CreatedAt: time.Now().UTC(),
		})

		var toolUses []types.Block
		var textParts []string
		for _, b := range resp.Blocks {
			switch b.Type {
			case types.BlockText:
				if strings.TrimSpace(b.Text) != "" {
					textParts = append(textParts, b.Text)
				}
			case types.BlockToolUse:
				toolUses = append(toolUses, b)
			}
		}
		if len(toolUses) == 0 {
			finalText = strings.Join(textParts, "\n")
			break
		}

		var results []types.Block
		for _, tu := range toolUses {
			impl, ok := subReg.Get(tu.Name)
			if !ok {
				results = append(results, types.Block{Type: types.BlockToolResult, ToolUseID: tu.ID, Text: "tool unavailable in subagent", IsError: true})
				continue
			}
			subEC := &tools.ExecContext{
				SessionID: ec.SessionID,
				RunID:     ec.RunID,
				Sandbox:   ec.Sandbox,
				Reads:     ec.Reads,
				Depth:     ec.Depth + 1,
			}
			res := impl.Execute(ctx, subEC, tu.Input)
			results = append(results, types.Block{
				Type:      types.BlockToolResult,
				ToolUseID: tu.ID,
				Text:      res.Content,
				IsError:   res.IsError,
			})
		}
		msgs = append(msgs, types.Message{
			ID:        types.NewID("msg"),
			SessionID: ec.SessionID,
			Role:      types.RoleUser,
			Blocks:    results,
			CreatedAt: time.Now().UTC(),
		})
	}
	if strings.TrimSpace(finalText) == "" {
		finalText = "[subagent finished without final text summary]"
	}
	return tools.ToolResult{
		Content: fmt.Sprintf("[Subagent summary | in=%d cache=%d out=%d]\n%s",
			totalUsage.TotalInput(), totalUsage.CacheReadTokens, totalUsage.OutputTokens, finalText),
	}
}

func (m *Manager) registerPlugins(plugins []config.PluginExecConfig) {
	for _, p := range plugins {
		if !p.Enabled || strings.TrimSpace(p.Name) == "" || strings.TrimSpace(p.Command) == "" {
			continue
		}
		m.reg.Register(&execPluginTool{cfg: p})
	}
}

type execPluginTool struct {
	cfg config.PluginExecConfig
}

func (p *execPluginTool) IsMutating() bool { return true }
func (p *execPluginTool) Spec() tools.ToolSpec {
	return tools.ToolSpec{
		Name:        "plugin__" + p.cfg.Name,
		Description: fmt.Sprintf("External executable plugin tool %q", p.cfg.Name),
		Parameters:  json.RawMessage(`{"type":"object","properties":{"input":{"type":"object","description":"Plugin input payload"}}}`),
	}
}

func (p *execPluginTool) Execute(ctx context.Context, ec *tools.ExecContext, args json.RawMessage) tools.ToolResult {
	pCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(pCtx, p.cfg.Command, p.cfg.Args...)
	cmd.Dir = ec.Sandbox.Root()
	cmd.Stdin = bytes.NewReader(args)
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	err := cmd.Run()
	if err != nil {
		msg := strings.TrimSpace(errBuf.String())
		if msg == "" {
			msg = err.Error()
		}
		return tools.ToolResult{Content: msg, IsError: true}
	}
	txt := strings.TrimSpace(out.String())
	if txt == "" {
		txt = "[plugin exited 0 with no output]"
	}
	return tools.ToolResult{Content: txt}
}
