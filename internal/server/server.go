package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/daum110715/daumini-agent/internal/agent"
	"github.com/daum110715/daumini-agent/internal/config"
	"github.com/daum110715/daumini-agent/internal/sandbox"
	"github.com/daum110715/daumini-agent/internal/types"
)

type Server struct {
	cfgMgr *config.Manager
	mgr    *agent.Manager
	webFS  fs.FS
}

func New(cfgMgr *config.Manager, mgr *agent.Manager, webFS fs.FS) *Server {
	return &Server{
		cfgMgr: cfgMgr,
		mgr:    mgr,
		webFS:  webFS,
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/healthz", s.handleHealth)
	mux.HandleFunc("GET /api/config", s.handleGetConfig)
	mux.HandleFunc("PUT /api/config", s.handlePutConfig)
	mux.HandleFunc("POST /api/providers/daumini/login", s.handleDauminiLogin)
	mux.HandleFunc("GET /api/providers/{id}/models", s.handleProviderModels)
	mux.HandleFunc("GET /api/mcp", s.handleGetMCP)
	mux.HandleFunc("POST /api/mcp/reload", s.handleReloadMCP)
	mux.HandleFunc("GET /api/skills", s.handleGetSkills)
	mux.HandleFunc("GET /api/tools", s.handleGetTools)
	mux.HandleFunc("GET /api/logs", s.handleGetLogs)

	mux.HandleFunc("GET /api/sessions", s.handleListSessions)
	mux.HandleFunc("POST /api/sessions", s.handleCreateSession)
	mux.HandleFunc("GET /api/sessions/{id}", s.handleGetSession)
	mux.HandleFunc("PATCH /api/sessions/{id}", s.handlePatchSession)
	mux.HandleFunc("POST /api/sessions/{id}/fork", s.handleForkSession)
	mux.HandleFunc("POST /api/sessions/{id}/messages", s.handleSendMessage)
	mux.HandleFunc("POST /api/sessions/{id}/cancel", s.handleCancelRun)
	mux.HandleFunc("POST /api/sessions/{id}/approvals/{aid}", s.handleResolveApproval)
	mux.HandleFunc("GET /api/sessions/{id}/events", s.handleSSE)

	mux.HandleFunc("GET /api/workspace/files", s.handleWorkspaceFiles)
	mux.HandleFunc("GET /api/workspace/file", s.handleWorkspaceFile)

	mux.HandleFunc("/", s.handleStatic)
	return s.withLocalGuard(mux)
}

func (s *Server) withLocalGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			if strings.HasPrefix(origin, "http://127.0.0.1:") ||
				strings.HasPrefix(origin, "http://localhost:") ||
				origin == "http://127.0.0.1" ||
				origin == "http://localhost" {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,PATCH,DELETE,OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			}
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	cfg := s.cfgMgr.Get()
	writeJSON(w, http.StatusOK, map[string]any{
		"status":           "ok",
		"service":          "daumini-agent",
		"version":          "0.1.0",
		"data_dir":         s.cfgMgr.DataDir(),
		"default_provider": cfg.DefaultProvider,
		"default_model":    cfg.DefaultModel,
		"theme":            cfg.Theme,
	})
}

func (s *Server) handleGetConfig(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, config.RedactedConfig(s.cfgMgr.Get()))
}

func (s *Server) handlePutConfig(w http.ResponseWriter, r *http.Request) {
	var in config.Config
	if err := json.NewDecoder(io.LimitReader(r.Body, 1024*1024)).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	prev := s.cfgMgr.Get()
	prevKeys := make(map[string]string, len(prev.Providers))
	for _, p := range prev.Providers {
		prevKeys[p.ID] = p.APIKey
	}
	// Preserve existing stored keys when UI sends back redacted masks (e.g. sk-1...abcd).
	for i := range in.Providers {
		k := strings.TrimSpace(in.Providers[i].APIKey)
		if k == "" || k == "****" || strings.Contains(k, "...") {
			if existing, ok := prevKeys[in.Providers[i].ID]; ok {
				in.Providers[i].APIKey = existing
			} else if strings.Contains(k, "...") || k == "****" {
				in.Providers[i].APIKey = ""
			}
		}
	}
	updated, err := s.cfgMgr.Update(func(c *config.Config) error {
		if in.DefaultProvider != "" {
			c.DefaultProvider = in.DefaultProvider
		}
		if in.DefaultModel != "" {
			c.DefaultModel = in.DefaultModel
		}
		if in.ApprovalMode != "" {
			c.ApprovalMode = in.ApprovalMode
		}
		if in.MaxTurns > 0 {
			c.MaxTurns = in.MaxTurns
		}
		if in.Theme != "" {
			c.Theme = in.Theme
		}
		if in.WebAddr != "" {
			c.WebAddr = in.WebAddr
		}
		if len(in.Providers) > 0 {
			c.Providers = in.Providers
		}
		if in.MCPServers != nil {
			c.MCPServers = in.MCPServers
		}
		if in.Hooks != nil {
			c.Hooks = in.Hooks
		}
		if in.Plugins != nil {
			c.Plugins = in.Plugins
		}
		c.SystemExtra = in.SystemExtra
		return nil
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = s.mgr.ReloadExtensions(r.Context())
	writeJSON(w, http.StatusOK, config.RedactedConfig(updated))
}

func (s *Server) handleDauminiLogin(w http.ResponseWriter, r *http.Request) {
	var in struct {
		BaseURL    string `json:"base_url"`
		Email      string `json:"email"`
		Password   string `json:"password"`
		InviteCode string `json:"invite_code"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	base := strings.TrimRight(strings.TrimSpace(in.BaseURL), "/")
	if base == "" {
		base = "https://mini.daum.pw"
	}
	key, err := LoginAndMintDauminiKey(r.Context(), base, in.Email, in.Password, in.InviteCode)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	updated, err := s.cfgMgr.Update(func(c *config.Config) error {
		found := false
		for i := range c.Providers {
			if c.Providers[i].ID == "daumini" {
				c.Providers[i].BaseURL = base
				c.Providers[i].APIKey = key
				c.Providers[i].UserEmail = in.Email
				c.Providers[i].Enabled = true
				found = true
				break
			}
		}
		if !found {
			c.Providers = append(c.Providers, config.ProviderConfig{
				ID:        "daumini",
				Name:      "daumini 中转",
				Protocol:  config.ProtocolDaumini,
				BaseURL:   base,
				APIKey:    key,
				UserEmail: in.Email,
				Enabled:   true,
			})
		}
		c.DefaultProvider = "daumini"
		return nil
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, config.RedactedConfig(updated))
}

func (s *Server) handleProviderModels(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	models, err := s.mgr.ListModels(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"models": models})
}

func (s *Server) handleGetMCP(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"servers": s.mgr.MCPStatuses()})
}

func (s *Server) handleReloadMCP(w http.ResponseWriter, r *http.Request) {
	statuses := s.mgr.ReloadExtensions(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{"servers": statuses})
}

func (s *Server) handleGetSkills(w http.ResponseWriter, r *http.Request) {
	dir := strings.TrimSpace(r.URL.Query().Get("dir"))
	if dir == "" {
		dir = "."
	}
	writeJSON(w, http.StatusOK, map[string]any{"skills": s.mgr.DiscoverSkills(dir)})
}

func (s *Server) handleGetTools(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"tools": s.mgr.ToolSpecs()})
}

func (s *Server) handleGetLogs(w http.ResponseWriter, _ *http.Request) {
	logsDir := filepath.Join(s.cfgMgr.DataDir(), "logs")
	entries, err := os.ReadDir(logsDir)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"logs": []types.LogEntry{}})
		return
	}
	var latest string
	for _, ent := range entries {
		if !ent.IsDir() && strings.HasSuffix(ent.Name(), ".jsonl") && ent.Name() > latest {
			latest = ent.Name()
		}
	}
	if latest == "" {
		writeJSON(w, http.StatusOK, map[string]any{"logs": []types.LogEntry{}})
		return
	}
	f, err := os.Open(filepath.Join(logsDir, latest))
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"logs": []types.LogEntry{}})
		return
	}
	defer f.Close()
	var items []types.LogEntry
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var le types.LogEntry
		if json.Unmarshal(sc.Bytes(), &le) == nil {
			items = append(items, le)
		}
	}
	if len(items) > 120 {
		items = items[len(items)-120:]
	}
	writeJSON(w, http.StatusOK, map[string]any{"logs": items})
}

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	inc := r.URL.Query().Get("archived") == "1"
	list, err := s.mgr.ListSessions(inc)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": list})
}

func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	var in struct {
		WorkspaceDir string             `json:"workspace_dir"`
		Title        string             `json:"title"`
		ProviderID   string             `json:"provider_id"`
		ModelID      string             `json:"model_id"`
		ApprovalMode types.ApprovalMode `json:"approval_mode"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&in)
	meta, err := s.mgr.CreateSession(in.WorkspaceDir, in.Title, in.ProviderID, in.ModelID, in.ApprovalMode)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, meta)
}

func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request) {
	snap, err := s.mgr.GetSnapshot(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, snap)
}

func (s *Server) handlePatchSession(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ProviderID   string             `json:"provider_id"`
		ModelID      string             `json:"model_id"`
		ApprovalMode types.ApprovalMode `json:"approval_mode"`
		WorkspaceDir string             `json:"workspace_dir"`
		Archived     *bool              `json:"archived"`
		Title        string             `json:"title"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	meta, err := s.mgr.UpdateSessionSettings(r.PathValue("id"), in.ProviderID, in.ModelID, in.ApprovalMode, in.WorkspaceDir, in.Archived, in.Title)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, meta)
}

func (s *Server) handleForkSession(w http.ResponseWriter, r *http.Request) {
	var in struct {
		UpToSeq int64  `json:"up_to_seq"`
		Title   string `json:"title"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 32*1024)).Decode(&in)
	snap, err := s.mgr.ForkSession(r.PathValue("id"), in.UpToSeq, in.Title)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, snap)
}

func (s *Server) handleSendMessage(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Prompt     string `json:"prompt"`
		ProviderID string `json:"provider_id"`
		ModelID    string `json:"model_id"`
		Mode       string `json:"mode"` // queue | steer | interrupt
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 256*1024)).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	run, err := s.mgr.SendPrompt(r.Context(), r.PathValue("id"), in.Prompt, in.ProviderID, in.ModelID, in.Mode)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, run)
}

func (s *Server) handleCancelRun(w http.ResponseWriter, r *http.Request) {
	ok := s.mgr.CancelRun(r.PathValue("id"))
	writeJSON(w, http.StatusOK, map[string]bool{"cancelled": ok})
}

func (s *Server) handleResolveApproval(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Decision types.ApprovalDecision `json:"decision"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 16*1024)).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.mgr.ResolveApproval(r.PathValue("id"), r.PathValue("aid"), in.Decision); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	afterSeq, _ := strconv.ParseInt(r.URL.Query().Get("after_seq"), 10, 64)
	ch, missed, snap, unsub, err := s.mgr.Subscribe(r.PathValue("id"), afterSeq)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	defer unsub()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	snapJSON, _ := json.Marshal(snap)
	fmt.Fprintf(w, "event: session.snapshot\ndata: %s\n\n", snapJSON)
	for _, ev := range missed {
		b, _ := json.Marshal(ev)
		fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", ev.Seq, ev.Type, b)
	}
	flusher.Flush()

	ping := time.NewTicker(15 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ping.C:
			_, _ = w.Write([]byte(": ping\n\n"))
			flusher.Flush()
		case ev, open := <-ch:
			if !open {
				return
			}
			b, _ := json.Marshal(ev)
			fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", ev.Seq, ev.Type, b)
			flusher.Flush()
		}
	}
}

func (s *Server) handleWorkspaceFiles(w http.ResponseWriter, r *http.Request) {
	sb, err := s.resolveSandbox(r.URL.Query().Get("session_id"), r.URL.Query().Get("dir"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	items, err := sb.List(r.URL.Query().Get("path"), 300)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"root": sb.Root(), "files": items})
}

func (s *Server) handleWorkspaceFile(w http.ResponseWriter, r *http.Request) {
	sb, err := s.resolveSandbox(r.URL.Query().Get("session_id"), r.URL.Query().Get("dir"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	abs, rel, err := sb.Resolve(r.URL.Query().Get("path"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	if len(data) > 256*1024 {
		data = data[:256*1024]
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": rel, "content": string(data)})
}

func (s *Server) resolveSandbox(sessionID, dir string) (*sandbox.Sandbox, error) {
	if sessionID != "" {
		snap, err := s.mgr.GetSnapshot(sessionID)
		if err != nil {
			return nil, err
		}
		return sandbox.New(snap.Meta.WorkspaceDir)
	}
	if dir == "" {
		dir = "."
	}
	return sandbox.New(dir)
}

func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	if s.webFS == nil {
		writeErr(w, http.StatusNotFound, "web UI bundle not embedded")
		return
	}
	clean := strings.TrimPrefix(filepath.ToSlash(filepath.Clean(r.URL.Path)), "/")
	if clean == "" || clean == "." {
		clean = "index.html"
	}
	data, err := fs.ReadFile(s.webFS, clean)
	if err != nil {
		data, err = fs.ReadFile(s.webFS, "index.html")
		if err != nil {
			writeErr(w, http.StatusNotFound, "not found")
			return
		}
		clean = "index.html"
	}
	switch {
	case strings.HasSuffix(clean, ".html"):
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	case strings.HasSuffix(clean, ".js"):
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	case strings.HasSuffix(clean, ".css"):
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
	case strings.HasSuffix(clean, ".svg"):
		w.Header().Set("Content-Type", "image/svg+xml")
	}
	_, _ = w.Write(data)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func LoginAndMintDauminiKey(ctx context.Context, baseURL, email, password, invite string) (string, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	loginBody, _ := json.Marshal(map[string]string{
		"email":    email,
		"password": password,
		"invite":   invite,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/backend/auth/login", bytes.NewReader(loginBody))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("login HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	cookies := resp.Cookies()

	keyBody, _ := json.Marshal(map[string]string{"name": "daumini-agent"})
	kReq, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/backend/console/v1/keys", bytes.NewReader(keyBody))
	if err != nil {
		return "", err
	}
	kReq.Header.Set("Content-Type", "application/json")
	for _, c := range cookies {
		kReq.AddCookie(c)
	}
	kResp, err := client.Do(kReq)
	if err != nil {
		return "", err
	}
	defer kResp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(kResp.Body, 16384))
	if kResp.StatusCode >= 400 {
		return "", fmt.Errorf("mint key HTTP %d: %s", kResp.StatusCode, strings.TrimSpace(string(b)))
	}
	var parsed struct {
		Key   string `json:"key"`
		Token string `json:"token"`
	}
	_ = json.Unmarshal(b, &parsed)
	if parsed.Key != "" {
		return parsed.Key, nil
	}
	if parsed.Token != "" {
		return parsed.Token, nil
	}
	return "", fmt.Errorf("unexpected key response from %s", baseURL)
}
