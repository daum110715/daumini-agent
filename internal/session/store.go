package session

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/daum110715/daumini-agent/internal/types"
)

// Store persists durable append-only session events, materialized session snapshots, and diagnostic JSONL logs.
type Store struct {
	mu      sync.Mutex
	rootDir string
}

func NewStore(dataDir string) (*Store, error) {
	sessionsDir := filepath.Join(dataDir, "sessions")
	logsDir := filepath.Join(dataDir, "logs")
	if err := os.MkdirAll(sessionsDir, 0o700); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(logsDir, 0o700); err != nil {
		return nil, err
	}
	return &Store{rootDir: dataDir}, nil
}

func (s *Store) sessionDir(id string) string {
	return filepath.Join(s.rootDir, "sessions", sanitizeID(id))
}

func sanitizeID(id string) string {
	id = strings.ReplaceAll(id, "/", "_")
	id = strings.ReplaceAll(id, "\\", "_")
	id = strings.ReplaceAll(id, "..", "_")
	return id
}

func (s *Store) Log(entry types.LogEntry) {
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now().UTC()
	}
	line, err := json.Marshal(entry)
	if err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p := filepath.Join(s.rootDir, "logs", "agent-"+entry.Timestamp.Format("2006-01-02")+".jsonl")
	f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(line, '\n'))
}

func (s *Store) CreateSession(meta types.SessionMeta) (types.SessionMeta, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	if meta.ID == "" {
		meta.ID = types.NewID("ses")
	}
	if meta.CreatedAt.IsZero() {
		meta.CreatedAt = now
	}
	meta.UpdatedAt = now
	dir := s.sessionDir(meta.ID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return types.SessionMeta{}, err
	}
	snap := types.SessionSnapshot{Meta: meta}
	if err := writeAtomicJSON(filepath.Join(dir, "snapshot.json"), snap, 0o600); err != nil {
		return types.SessionMeta{}, err
	}
	return meta, nil
}

func (s *Store) ListSessions(includeArchived bool) ([]types.SessionMeta, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(filepath.Join(s.rootDir, "sessions"))
	if err != nil {
		return nil, err
	}
	out := make([]types.SessionMeta, 0, len(entries))
	for _, ent := range entries {
		if !ent.IsDir() {
			continue
		}
		snap, err := s.readSnapshotLocked(ent.Name())
		if err != nil {
			continue
		}
		if !includeArchived && snap.Meta.Archived {
			continue
		}
		out = append(out, snap.Meta)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	return out, nil
}

func (s *Store) GetSnapshot(sessionID string) (types.SessionSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readSnapshotLocked(sessionID)
}

func (s *Store) UpdateSnapshot(sessionID string, fn func(*types.SessionSnapshot) error) (types.SessionSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	snap, err := s.readSnapshotLocked(sessionID)
	if err != nil {
		return types.SessionSnapshot{}, err
	}
	if err := fn(&snap); err != nil {
		return types.SessionSnapshot{}, err
	}
	snap.Meta.UpdatedAt = time.Now().UTC()
	snap.Meta.MessageCount = len(snap.Messages)
	if err := writeAtomicJSON(filepath.Join(s.sessionDir(sessionID), "snapshot.json"), snap, 0o600); err != nil {
		return types.SessionSnapshot{}, err
	}
	return snap, nil
}

// AppendEvent durably appends one monotonic-seq SessionEvent to events.jsonl and advances snapshot.Meta.LastSeq.
func (s *Store) AppendEvent(sessionID, runID, eventType string, payload any) (types.SessionEvent, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return types.SessionEvent{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	snap, err := s.readSnapshotLocked(sessionID)
	if err != nil {
		return types.SessionEvent{}, err
	}
	snap.Meta.LastSeq++
	ev := types.SessionEvent{
		Seq:       snap.Meta.LastSeq,
		SessionID: sessionID,
		RunID:     runID,
		Type:      eventType,
		Payload:   raw,
		Timestamp: time.Now().UTC(),
	}
	line, err := json.Marshal(ev)
	if err != nil {
		return types.SessionEvent{}, err
	}
	evPath := filepath.Join(s.sessionDir(sessionID), "events.jsonl")
	f, err := os.OpenFile(evPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return types.SessionEvent{}, err
	}
	_, wErr := f.Write(append(line, '\n'))
	_ = f.Close()
	if wErr != nil {
		return types.SessionEvent{}, wErr
	}
	snap.Meta.UpdatedAt = ev.Timestamp
	if err := writeAtomicJSON(filepath.Join(s.sessionDir(sessionID), "snapshot.json"), snap, 0o600); err != nil {
		return types.SessionEvent{}, err
	}
	return ev, nil
}

func (s *Store) ReadEventsAfter(sessionID string, afterSeq int64) ([]types.SessionEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readEventsLocked(sessionID, afterSeq, 0)
}

func (s *Store) readEventsLocked(sessionID string, afterSeq, maxSeq int64) ([]types.SessionEvent, error) {
	evPath := filepath.Join(s.sessionDir(sessionID), "events.jsonl")
	f, err := os.Open(evPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var out []types.SessionEvent
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var ev types.SessionEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}
		if ev.Seq <= afterSeq {
			continue
		}
		if maxSeq > 0 && ev.Seq > maxSeq {
			break
		}
		out = append(out, ev)
	}
	return out, sc.Err()
}

// ForkSession creates an independent child session preserving message history up to upToSeq (or all if <= 0).
func (s *Store) ForkSession(sourceID string, upToSeq int64, title string) (types.SessionSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	src, err := s.readSnapshotLocked(sourceID)
	if err != nil {
		return types.SessionSnapshot{}, err
	}
	now := time.Now().UTC()
	if title == "" {
		title = src.Meta.Title + " (分支)"
	}
	newID := types.NewID("ses")
	var msgs []types.Message
	var maxMsgSeq int64
	for _, m := range src.Messages {
		if upToSeq > 0 && m.Seq > upToSeq {
			break
		}
		copyMsg := m
		copyMsg.SessionID = newID
		msgs = append(msgs, copyMsg)
		if m.Seq > maxMsgSeq {
			maxMsgSeq = m.Seq
		}
	}
	newMeta := types.SessionMeta{
		ID:              newID,
		Title:           title,
		WorkspaceDir:    src.Meta.WorkspaceDir,
		ProviderID:      src.Meta.ProviderID,
		ModelID:         src.Meta.ModelID,
		ApprovalMode:    src.Meta.ApprovalMode,
		ParentSessionID: sourceID,
		ForkedFromSeq:   maxMsgSeq,
		AllowedTools:    append([]string(nil), src.Meta.AllowedTools...),
		Todos:           append([]types.TodoItem(nil), src.Meta.Todos...),
		MessageCount:    len(msgs),
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	newSnap := types.SessionSnapshot{
		Meta:     newMeta,
		Messages: msgs,
	}
	dir := s.sessionDir(newID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return types.SessionSnapshot{}, err
	}
	if err := writeAtomicJSON(filepath.Join(dir, "snapshot.json"), newSnap, 0o600); err != nil {
		return types.SessionSnapshot{}, err
	}
	return newSnap, nil
}

// RecoverOrphans repairs interrupted runs and ensures no dangling tool_use breaks future LLM requests.
func (s *Store) RecoverOrphans() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(filepath.Join(s.rootDir, "sessions"))
	if err != nil {
		return 0, err
	}
	repaired := 0
	for _, ent := range entries {
		if !ent.IsDir() {
			continue
		}
		snap, err := s.readSnapshotLocked(ent.Name())
		if err != nil {
			continue
		}
		changed := false
		now := time.Now().UTC()
		for i := range snap.Runs {
			if snap.Runs[i].Status == types.RunRunning || snap.Runs[i].Status == types.RunQueued || snap.Runs[i].Status == types.RunWaitingApproval {
				snap.Runs[i].Status = types.RunFailed
				snap.Runs[i].Error = "daemon restarted before run completed"
				snap.Runs[i].EndedAt = &now
				changed = true
			}
		}
		if snap.Meta.ActiveRunID != "" {
			snap.Meta.ActiveRunID = ""
			changed = true
		}
		for i := range snap.PendingApprovals {
			if snap.PendingApprovals[i].Decision == types.DecisionPending {
				snap.PendingApprovals[i].Decision = types.DecisionDeny
				snap.PendingApprovals[i].ResolvedAt = &now
				changed = true
			}
		}
		if repairDanglingToolUse(&snap) {
			changed = true
		}
		if changed {
			repaired++
			snap.Meta.UpdatedAt = now
			snap.Meta.MessageCount = len(snap.Messages)
			_ = writeAtomicJSON(filepath.Join(s.sessionDir(ent.Name()), "snapshot.json"), snap, 0o600)
		}
	}
	return repaired, nil
}

func repairDanglingToolUse(snap *types.SessionSnapshot) bool {
	if len(snap.Messages) == 0 {
		return false
	}
	answered := make(map[string]bool)
	for _, m := range snap.Messages {
		for _, b := range m.Blocks {
			if b.Type == types.BlockToolResult && b.ToolUseID != "" {
				answered[b.ToolUseID] = true
			}
		}
	}
	var missing []types.Block
	for _, m := range snap.Messages {
		if m.Role != types.RoleAssistant {
			continue
		}
		for _, b := range m.Blocks {
			if b.Type == types.BlockToolUse && b.ID != "" && !answered[b.ID] {
				missing = append(missing, types.Block{
					Type:      types.BlockToolResult,
					ToolUseID: b.ID,
					Text:      "tool execution interrupted by process restart",
					IsError:   true,
				})
			}
		}
	}
	if len(missing) == 0 {
		return false
	}
	snap.Meta.LastSeq++
	snap.Messages = append(snap.Messages, types.Message{
		ID:        types.NewID("msg"),
		SessionID: snap.Meta.ID,
		Seq:       snap.Meta.LastSeq,
		Role:      types.RoleUser,
		Blocks:    missing,
		CreatedAt: time.Now().UTC(),
	})
	return true
}

func (s *Store) readSnapshotLocked(id string) (types.SessionSnapshot, error) {
	p := filepath.Join(s.sessionDir(id), "snapshot.json")
	raw, err := os.ReadFile(p)
	if err != nil {
		return types.SessionSnapshot{}, err
	}
	var snap types.SessionSnapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		return types.SessionSnapshot{}, err
	}
	return snap, nil
}

func writeAtomicJSON(path string, v any, perm os.FileMode) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp := path + ".tmp." + types.NewID("w")
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
