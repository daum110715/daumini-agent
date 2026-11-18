package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/daum110715/daumini-agent/internal/config"
	"github.com/daum110715/daumini-agent/internal/sandbox"
	"github.com/daum110715/daumini-agent/internal/tools"
)

const protocolVersion = "2025-11-25"

type RemoteTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

type ServerStatus struct {
	Name      string       `json:"name"`
	Transport string       `json:"transport"`
	Connected bool         `json:"connected"`
	Error     string       `json:"error,omitempty"`
	Tools     []RemoteTool `json:"tools,omitempty"`
}

type Manager struct {
	mu       sync.Mutex
	clients  map[string]*clientConn
	statuses map[string]ServerStatus
}

func NewManager() *Manager {
	return &Manager{
		clients:  make(map[string]*clientConn),
		statuses: make(map[string]ServerStatus),
	}
}

func (m *Manager) SyncAndRegister(ctx context.Context, servers []config.MCPServerConfig, reg *tools.Registry) []ServerStatus {
	m.CloseAll()
	reg.UnregisterPrefix("mcp__")

	var out []ServerStatus
	for _, srv := range servers {
		if !srv.Enabled || strings.TrimSpace(srv.Name) == "" {
			continue
		}
		conn, st := m.connectOne(ctx, srv)
		m.mu.Lock()
		m.statuses[srv.Name] = st
		if conn != nil {
			m.clients[srv.Name] = conn
		}
		m.mu.Unlock()
		if conn != nil {
			for _, rt := range st.Tools {
				reg.Register(&mcpToolAdapter{
					serverName: srv.Name,
					remote:     rt,
					conn:       conn,
					timeout:    timeoutOr(srv.TimeoutS, 45*time.Second),
				})
			}
		}
		out = append(out, st)
	}
	return out
}

func (m *Manager) Statuses() []ServerStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]ServerStatus, 0, len(m.statuses))
	for _, st := range m.statuses {
		out = append(out, st)
	}
	return out
}

func (m *Manager) CloseAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.clients {
		c.close()
	}
	m.clients = make(map[string]*clientConn)
	m.statuses = make(map[string]ServerStatus)
}

func (m *Manager) connectOne(ctx context.Context, srv config.MCPServerConfig) (*clientConn, ServerStatus) {
	st := ServerStatus{Name: srv.Name, Transport: string(srv.Transport)}
	c := &clientConn{
		cfg:     srv,
		pending: make(map[int64]chan rpcResponse),
		httpClient: &http.Client{
			Timeout: timeoutOr(srv.TimeoutS, 45*time.Second),
		},
	}
	if srv.Transport == config.MCPStdio || srv.Transport == "" {
		if err := c.startStdio(); err != nil {
			st.Error = err.Error()
			return nil, st
		}
	}
	initCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	_, err := c.call(initCtx, "initialize", map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "daumini-agent",
			"version": "0.1.0",
		},
	})
	if err != nil {
		c.close()
		st.Error = "initialize failed: " + err.Error()
		return nil, st
	}
	_ = c.notify(initCtx, "notifications/initialized", map[string]any{})

	listRes, err := c.call(initCtx, "tools/list", map[string]any{})
	if err != nil {
		c.close()
		st.Error = "tools/list failed: " + err.Error()
		return nil, st
	}
	var parsed struct {
		Tools []RemoteTool `json:"tools"`
	}
	if err := json.Unmarshal(listRes, &parsed); err != nil {
		c.close()
		st.Error = "decode tools/list: " + err.Error()
		return nil, st
	}
	st.Connected = true
	st.Tools = parsed.Tools
	return c, st
}

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      *int64 `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type clientConn struct {
	cfg        config.MCPServerConfig
	nextID     atomic.Int64
	mu         sync.Mutex
	cmd        *exec.Cmd
	stdin      io.WriteCloser
	pending    map[int64]chan rpcResponse
	sessionID  string
	httpClient *http.Client
	closed     bool
}

func (c *clientConn) startStdio() error {
	cmd := exec.Command(c.cfg.Command, c.cfg.Args...)
	env := append([]string(nil), os.Environ()...)
	for k, v := range c.cfg.Env {
		env = append(env, k+"="+v)
	}
	cmd.Env = env
	sandbox.ConfigureCommand(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return err
	}
	c.cmd = cmd
	c.stdin = stdin
	go c.readStdioLoop(stdout)
	go func() { _ = cmd.Wait() }()
	return nil
}

func (c *clientConn) readStdioLoop(r io.Reader) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var resp rpcResponse
		if err := json.Unmarshal([]byte(line), &resp); err != nil || resp.ID == nil {
			continue
		}
		c.mu.Lock()
		ch := c.pending[*resp.ID]
		delete(c.pending, *resp.ID)
		c.mu.Unlock()
		if ch != nil {
			ch <- resp
		}
	}
	c.mu.Lock()
	for id, ch := range c.pending {
		delete(c.pending, id)
		close(ch)
	}
	c.mu.Unlock()
}

func (c *clientConn) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := c.nextID.Add(1)
	req := rpcRequest{JSONRPC: "2.0", ID: &id, Method: method, Params: params}
	if c.cfg.Transport == config.MCPStreamableHTTP {
		return c.callHTTP(ctx, req)
	}

	raw, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	ch := make(chan rpcResponse, 1)
	c.mu.Lock()
	if c.closed || c.stdin == nil {
		c.mu.Unlock()
		return nil, fmt.Errorf("mcp stdio closed")
	}
	c.pending[id] = ch
	_, wErr := c.stdin.Write(append(raw, '\n'))
	if wErr != nil {
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, wErr
	}
	c.mu.Unlock()

	select {
	case resp, ok := <-ch:
		if !ok {
			return nil, fmt.Errorf("mcp server disconnected")
		}
		if resp.Error != nil {
			return nil, fmt.Errorf("mcp error %d: %s", resp.Error.Code, resp.Error.Message)
		}
		return resp.Result, nil
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, ctx.Err()
	}
}

func (c *clientConn) notify(ctx context.Context, method string, params any) error {
	req := rpcRequest{JSONRPC: "2.0", Method: method, Params: params}
	if c.cfg.Transport == config.MCPStreamableHTTP {
		_, _ = c.callHTTP(ctx, req)
		return nil
	}
	raw, _ := json.Marshal(req)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stdin == nil || c.closed {
		return nil
	}
	_, err := c.stdin.Write(append(raw, '\n'))
	return err
}

func (c *clientConn) callHTTP(ctx context.Context, req rpcRequest) (json.RawMessage, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.URL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json, text/event-stream")
	if c.sessionID != "" {
		httpReq.Header.Set("Mcp-Session-Id", c.sessionID)
	}
	for k, v := range c.cfg.Headers {
		httpReq.Header.Set(k, v)
	}
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
		c.sessionID = sid
	}
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	if req.ID == nil {
		return nil, nil
	}
	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	if strings.Contains(ct, "text/event-stream") {
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		var dataLines []string
		for sc.Scan() {
			line := sc.Text()
			if strings.HasPrefix(line, "data:") {
				dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
				continue
			}
			if line == "" && len(dataLines) > 0 {
				raw := strings.Join(dataLines, "\n")
				dataLines = nil
				var rr rpcResponse
				if json.Unmarshal([]byte(raw), &rr) == nil && rr.ID != nil && *rr.ID == *req.ID {
					if rr.Error != nil {
						return nil, fmt.Errorf("mcp error %d: %s", rr.Error.Code, rr.Error.Message)
					}
					return rr.Result, nil
				}
			}
		}
		return nil, fmt.Errorf("no matching response event from streamable-http server")
	}

	rawResp, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if err != nil {
		return nil, err
	}
	var rr rpcResponse
	if err := json.Unmarshal(rawResp, &rr); err != nil {
		return nil, err
	}
	if rr.Error != nil {
		return nil, fmt.Errorf("mcp error %d: %s", rr.Error.Code, rr.Error.Message)
	}
	return rr.Result, nil
}

func (c *clientConn) close() {
	c.mu.Lock()
	c.closed = true
	if c.stdin != nil {
		_ = c.stdin.Close()
		c.stdin = nil
	}
	cmd := c.cmd
	c.cmd = nil
	c.mu.Unlock()
	if cmd != nil {
		sandbox.KillProcessTree(cmd)
	}
}

type mcpToolAdapter struct {
	serverName string
	remote     RemoteTool
	conn       *clientConn
	timeout    time.Duration
}

func (t *mcpToolAdapter) IsMutating() bool { return true }

func (t *mcpToolAdapter) Spec() tools.ToolSpec {
	params := t.remote.InputSchema
	if len(params) == 0 {
		params = json.RawMessage(`{"type":"object","properties":{}}`)
	}
	return tools.ToolSpec{
		Name:        fmt.Sprintf("mcp__%s__%s", sanitizeMCPName(t.serverName), sanitizeMCPName(t.remote.Name)),
		Description: fmt.Sprintf("[MCP:%s] %s", t.serverName, t.remote.Description),
		Parameters:  params,
	}
}

func (t *mcpToolAdapter) Execute(ctx context.Context, _ *tools.ExecContext, args json.RawMessage) tools.ToolResult {
	var parsedArgs any = map[string]any{}
	if len(bytes.TrimSpace(args)) > 0 {
		_ = json.Unmarshal(args, &parsedArgs)
	}
	callCtx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()
	raw, err := t.conn.call(callCtx, "tools/call", map[string]any{
		"name":      t.remote.Name,
		"arguments": parsedArgs,
	})
	if err != nil {
		return tools.ToolResult{Content: err.Error(), IsError: true}
	}
	var res struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return tools.ToolResult{Content: string(raw)}
	}
	var parts []string
	for _, c := range res.Content {
		if c.Text != "" {
			parts = append(parts, c.Text)
		}
	}
	if len(parts) == 0 {
		parts = append(parts, string(raw))
	}
	return tools.ToolResult{
		Content: strings.Join(parts, "\n"),
		IsError: res.IsError,
	}
}

func sanitizeMCPName(s string) string {
	s = strings.ReplaceAll(s, " ", "_")
	s = strings.ReplaceAll(s, "-", "_")
	return s
}

func timeoutOr(sec int, fallback time.Duration) time.Duration {
	if sec > 0 {
		return time.Duration(sec) * time.Second
	}
	return fallback
}
