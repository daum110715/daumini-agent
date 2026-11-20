package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/daum110715/daumini-agent/internal/sandbox"
	"github.com/daum110715/daumini-agent/internal/types"
)

func RegisterBuiltins(reg *Registry) {
	reg.Register(&fsListTool{})
	reg.Register(&fsSearchTool{})
	reg.Register(&fsReadTool{})
	reg.Register(&fsEditTool{})
	reg.Register(&fsWriteTool{})
	reg.Register(&shellExecTool{})
	reg.Register(&todoUpdateTool{})
	reg.Register(&webFetchTool{})
}

type fsListTool struct{}

func (t *fsListTool) IsMutating() bool { return false }
func (t *fsListTool) Spec() ToolSpec {
	return ToolSpec{
		Name:        "fs_list",
		Description: "List workspace files/dirs.",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"limit":{"type":"integer"}}}`),
	}
}

func (t *fsListTool) Execute(_ context.Context, ec *ExecContext, args json.RawMessage) ToolResult {
	var in struct {
		Path  string `json:"path"`
		Limit int    `json:"limit"`
	}
	_ = json.Unmarshal(args, &in)
	items, err := ec.Sandbox.List(in.Path, in.Limit)
	if err != nil {
		return ToolResult{Content: err.Error(), IsError: true}
	}
	if len(items) == 0 {
		return ToolResult{Content: "[directory is empty]"}
	}
	return ToolResult{Content: strings.Join(items, "\n")}
}

type fsSearchTool struct{}

func (t *fsSearchTool) IsMutating() bool { return false }
func (t *fsSearchTool) Spec() ToolSpec {
	return ToolSpec{
		Name:        "fs_search",
		Description: "Search text or regex in workspace files.",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"},"path":{"type":"string"},"regex":{"type":"boolean"},"max_matches":{"type":"integer"}},"required":["query"]}`),
	}
}

func (t *fsSearchTool) Execute(_ context.Context, ec *ExecContext, args json.RawMessage) ToolResult {
	var in struct {
		Query      string `json:"query"`
		Path       string `json:"path"`
		Regex      bool   `json:"regex"`
		MaxMatches int    `json:"max_matches"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return ToolResult{Content: "invalid json arguments: " + err.Error(), IsError: true}
	}
	if strings.TrimSpace(in.Query) == "" {
		return ToolResult{Content: "query is required", IsError: true}
	}
	if in.MaxMatches <= 0 || in.MaxMatches > 300 {
		in.MaxMatches = 80
	}
	absDir, _, err := ec.Sandbox.Resolve(in.Path)
	if err != nil {
		return ToolResult{Content: err.Error(), IsError: true}
	}
	var re *regexp.Regexp
	if in.Regex {
		re, err = regexp.Compile(in.Query)
		if err != nil {
			return ToolResult{Content: "invalid regex: " + err.Error(), IsError: true}
		}
	}
	var matches []string
	_ = filepath.WalkDir(absDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if path != absDir && (name == ".git" || name == "node_modules" || name == "dist" || name == ".svelte-kit" || name == ".agent-logs" || name == "target") {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil || info.Size() > 1024*1024 {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil || bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data) {
			return nil
		}
		rel, err := filepath.Rel(ec.Sandbox.Root(), path)
		if err != nil {
			return nil
		}
		relSlash := filepath.ToSlash(rel)
		lines := strings.Split(string(data), "\n")
		for idx, line := range lines {
			matched := false
			if re != nil {
				matched = re.MatchString(line)
			} else {
				matched = strings.Contains(line, in.Query)
			}
			if matched {
				text := line
				if len(text) > 220 {
					text = text[:220] + "..."
				}
				matches = append(matches, fmt.Sprintf("%s:%d#%s|%s", relSlash, idx+1, types.LineHash4(line), text))
				if len(matches) >= in.MaxMatches {
					return fs.SkipAll
				}
			}
		}
		return nil
	})
	if len(matches) == 0 {
		return ToolResult{Content: "[no matches found]"}
	}
	return ToolResult{Content: strings.Join(matches, "\n")}
}

type fsReadTool struct{}

func (t *fsReadTool) IsMutating() bool { return false }
func (t *fsReadTool) Spec() ToolSpec {
	return ToolSpec{
		Name:        "fs_read",
		Description: "Read file with line#hash anchors.",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"start_line":{"type":"integer"},"end_line":{"type":"integer"}},"required":["path"]}`),
	}
}

func (t *fsReadTool) Execute(_ context.Context, ec *ExecContext, args json.RawMessage) ToolResult {
	var in struct {
		Path      string `json:"path"`
		StartLine int    `json:"start_line"`
		EndLine   int    `json:"end_line"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return ToolResult{Content: "invalid json arguments: " + err.Error(), IsError: true}
	}
	absPath, rel, err := ec.Sandbox.Resolve(in.Path)
	if err != nil {
		return ToolResult{Content: err.Error(), IsError: true}
	}
	data, err := os.ReadFile(absPath)
	if err != nil {
		return ToolResult{Content: err.Error(), IsError: true}
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return ToolResult{Content: fmt.Sprintf("binary file (%d bytes), refusing raw read", len(data)), IsError: true}
	}
	ec.Reads.MarkRead(ec.SessionID, absPath)

	normalized := strings.ReplaceAll(string(data), "\r\n", "\n")
	lines := strings.Split(normalized, "\n")
	start := in.StartLine
	if start <= 0 {
		start = 1
	}
	end := in.EndLine
	if end <= 0 || end < start {
		end = start + 199
	}
	if start > len(lines) {
		return ToolResult{Content: fmt.Sprintf("%s has %d lines; start_line %d out of range", rel, len(lines), start), IsError: true}
	}
	if end > len(lines) {
		end = len(lines)
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "[%s lines %d-%d of %d]\n", rel, start, end, len(lines))
	for i := start; i <= end; i++ {
		line := lines[i-1]
		fmt.Fprintf(&sb, "%d#%s|%s\n", i, types.LineHash4(line), line)
	}
	return ToolResult{Content: sb.String()}
}

type fsEditTool struct{}

func (t *fsEditTool) IsMutating() bool { return true }
func (t *fsEditTool) Spec() ToolSpec {
	return ToolSpec{
		Name:        "fs_edit",
		Description: "Replace line span using line#hash anchors from fs_read.",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"start_anchor":{"type":"string"},"end_anchor":{"type":"string"},"replacement":{"type":"string"}},"required":["path","start_anchor","replacement"]}`),
	}
}

func (t *fsEditTool) Execute(_ context.Context, ec *ExecContext, args json.RawMessage) ToolResult {
	var in struct {
		Path        string `json:"path"`
		StartAnchor string `json:"start_anchor"`
		EndAnchor   string `json:"end_anchor"`
		Replacement string `json:"replacement"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return ToolResult{Content: "invalid json arguments: " + err.Error(), IsError: true}
	}
	absPath, rel, err := ec.Sandbox.Resolve(in.Path)
	if err != nil {
		return ToolResult{Content: err.Error(), IsError: true}
	}
	if !ec.Reads.WasRead(ec.SessionID, absPath) {
		return ToolResult{Content: fmt.Sprintf("must call fs_read on %s before fs_edit", rel), IsError: true}
	}
	startLine, startHash, err := parseAnchor(in.StartAnchor)
	if err != nil {
		return ToolResult{Content: "invalid start_anchor: " + err.Error(), IsError: true}
	}
	endAnchor := strings.TrimSpace(in.EndAnchor)
	if endAnchor == "" {
		endAnchor = in.StartAnchor
	}
	endLine, endHash, err := parseAnchor(endAnchor)
	if err != nil {
		return ToolResult{Content: "invalid end_anchor: " + err.Error(), IsError: true}
	}
	if endLine < startLine {
		return ToolResult{Content: "end_anchor line must be >= start_anchor line", IsError: true}
	}

	raw, err := os.ReadFile(absPath)
	if err != nil {
		return ToolResult{Content: err.Error(), IsError: true}
	}
	useCRLF := bytes.Contains(raw, []byte("\r\n"))
	lines := strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")
	if startLine < 1 || endLine > len(lines) {
		return ToolResult{Content: fmt.Sprintf("anchor range %d-%d out of file bounds (%d lines)", startLine, endLine, len(lines)), IsError: true}
	}
	actualStart := types.LineHash4(lines[startLine-1])
	if actualStart != startHash {
		return ToolResult{Content: fmt.Sprintf("hash mismatch at line %d: expected %s got %s (re-read file)", startLine, startHash, actualStart), IsError: true}
	}
	actualEnd := types.LineHash4(lines[endLine-1])
	if actualEnd != endHash {
		return ToolResult{Content: fmt.Sprintf("hash mismatch at line %d: expected %s got %s (re-read file)", endLine, endHash, actualEnd), IsError: true}
	}

	repNorm := strings.ReplaceAll(in.Replacement, "\r\n", "\n")
	var repLines []string
	if repNorm == "" {
		repLines = nil
	} else {
		repLines = strings.Split(repNorm, "\n")
	}

	outLines := make([]string, 0, len(lines)-(endLine-startLine+1)+len(repLines))
	outLines = append(outLines, lines[:startLine-1]...)
	outLines = append(outLines, repLines...)
	outLines = append(outLines, lines[endLine:]...)
	joined := strings.Join(outLines, "\n")
	if useCRLF {
		joined = strings.ReplaceAll(joined, "\n", "\r\n")
	}
	if err := os.WriteFile(absPath, []byte(joined), 0o644); err != nil {
		return ToolResult{Content: err.Error(), IsError: true}
	}
	return ToolResult{Content: fmt.Sprintf("edited %s lines %d-%d (%d new lines)", rel, startLine, endLine, len(repLines))}
}

func parseAnchor(anchor string) (int, string, error) {
	parts := strings.SplitN(strings.TrimSpace(anchor), "#", 2)
	if len(parts) != 2 || len(parts[1]) != 4 {
		return 0, "", fmt.Errorf("expected format <line>#<4hex>, got %q", anchor)
	}
	n, err := strconv.Atoi(parts[0])
	if err != nil || n <= 0 {
		return 0, "", fmt.Errorf("invalid line number in %q", anchor)
	}
	return n, strings.ToLower(parts[1]), nil
}

type fsWriteTool struct{}

func (t *fsWriteTool) IsMutating() bool { return true }
func (t *fsWriteTool) Spec() ToolSpec {
	return ToolSpec{
		Name:        "fs_write",
		Description: "Create file or overwrite read file.",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"}},"required":["path","content"]}`),
	}
}

func (t *fsWriteTool) Execute(_ context.Context, ec *ExecContext, args json.RawMessage) ToolResult {
	var in struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return ToolResult{Content: "invalid json arguments: " + err.Error(), IsError: true}
	}
	absPath, rel, err := ec.Sandbox.Resolve(in.Path)
	if err != nil {
		return ToolResult{Content: err.Error(), IsError: true}
	}
	if _, statErr := os.Stat(absPath); statErr == nil && !ec.Reads.WasRead(ec.SessionID, absPath) {
		return ToolResult{Content: fmt.Sprintf("file %s exists; call fs_read first before overwriting", rel), IsError: true}
	}
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return ToolResult{Content: err.Error(), IsError: true}
	}
	if err := os.WriteFile(absPath, []byte(in.Content), 0o644); err != nil {
		return ToolResult{Content: err.Error(), IsError: true}
	}
	ec.Reads.MarkRead(ec.SessionID, absPath)
	return ToolResult{Content: fmt.Sprintf("wrote %s (%d bytes)", rel, len(in.Content))}
}

type shellExecTool struct{}

func (t *shellExecTool) IsMutating() bool { return true }
func (t *shellExecTool) Spec() ToolSpec {
	return ToolSpec{
		Name:        "shell_exec",
		Description: "Run shell command in workspace.",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"command":{"type":"string"},"timeout_sec":{"type":"integer"}},"required":["command"]}`),
	}
}

func (t *shellExecTool) Execute(ctx context.Context, ec *ExecContext, args json.RawMessage) ToolResult {
	var in struct {
		Command    string `json:"command"`
		TimeoutSec int    `json:"timeout_sec"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return ToolResult{Content: "invalid json arguments: " + err.Error(), IsError: true}
	}
	cmdStr := strings.TrimSpace(in.Command)
	if cmdStr == "" {
		return ToolResult{Content: "command is required", IsError: true}
	}
	timeout := 60 * time.Second
	if in.TimeoutSec > 0 {
		if in.TimeoutSec > 300 {
			in.TimeoutSec = 300
		}
		timeout = time.Duration(in.TimeoutSec) * time.Second
	}

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", cmdStr)
	} else {
		cmd = exec.Command("sh", "-c", cmdStr)
	}
	cmd.Dir = ec.Sandbox.Root()
	cmd.Env = sandbox.SanitizedEnv()
	cmd.Stdin = nil
	sandbox.ConfigureCommand(cmd)

	var combined bytes.Buffer
	cmd.Stdout = &combined
	cmd.Stderr = &combined

	if err := cmd.Start(); err != nil {
		return ToolResult{Content: "start failed: " + err.Error(), IsError: true}
	}
	waitCh := make(chan error, 1)
	go func() { waitCh <- cmd.Wait() }()

	var runErr error
	timedOut := false
	select {
	case runErr = <-waitCh:
	case <-runCtx.Done():
		timedOut = true
		sandbox.KillProcessTree(cmd)
		select {
		case runErr = <-waitCh:
		case <-time.After(2 * time.Second):
		}
	}

	exitCode := 0
	if timedOut {
		exitCode = 124
	} else if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
		}
	}

	rawOut := combined.String()
	if strings.TrimSpace(rawOut) == "" {
		if exitCode == 0 {
			return ToolResult{Content: "[Command exited 0 with no output]", IsError: false}
		}
		return ToolResult{Content: fmt.Sprintf("[Command exited %d with no output]", exitCode), IsError: true}
	}

	out := capShellOutput(ec.Sandbox.Root(), rawOut, exitCode)
	if timedOut {
		out = fmt.Sprintf("[Command timed out after %s and process group was killed]\n%s", timeout, out)
	}
	return ToolResult{Content: out, IsError: exitCode != 0}
}

func capShellOutput(workspaceRoot, raw string, exitCode int) string {
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	if len(raw) <= 24*1024 && len(lines) <= 220 {
		return fmt.Sprintf("[exit_code=%d]\n%s", exitCode, raw)
	}
	logDir := filepath.Join(workspaceRoot, ".agent-logs")
	_ = os.MkdirAll(logDir, 0o755)
	logFile := fmt.Sprintf("cmd-%s.log", types.NewID("sh"))
	logPath := filepath.Join(logDir, logFile)
	_ = os.WriteFile(logPath, []byte(raw), 0o644)

	headCount := 45
	tailCount := 80
	if len(lines) <= headCount+tailCount {
		headCount = len(lines) / 3
		tailCount = len(lines) / 3
	}
	head := strings.Join(lines[:headCount], "\n")
	tail := strings.Join(lines[len(lines)-tailCount:], "\n")
	return fmt.Sprintf("[exit_code=%d; output truncated (%d lines, %d bytes); full log saved to .agent-logs/%s]\n%s\n...\n[omitted middle]\n...\n%s",
		exitCode, len(lines), len(raw), logFile, head, tail)
}

type todoUpdateTool struct{}

func (t *todoUpdateTool) IsMutating() bool { return false }
func (t *todoUpdateTool) Spec() ToolSpec {
	return ToolSpec{
		Name:        "todo_update",
		Description: "Update active task checklist.",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"items":{"type":"array","items":{"type":"object","properties":{"id":{"type":"string"},"content":{"type":"string"},"status":{"type":"string","enum":["pending","in_progress","completed"]}},"required":["id","content","status"]}}},"required":["items"]}`),
	}
}

func (t *todoUpdateTool) Execute(_ context.Context, _ *ExecContext, args json.RawMessage) ToolResult {
	var in struct {
		Items []types.TodoItem `json:"items"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return ToolResult{Content: "invalid json arguments: " + err.Error(), IsError: true}
	}
	return ToolResult{
		Content:      fmt.Sprintf("updated %d todo items", len(in.Items)),
		UpdatedTodos: in.Items,
	}
}

type webFetchTool struct{}

func (t *webFetchTool) IsMutating() bool { return false }
func (t *webFetchTool) Spec() ToolSpec {
	return ToolSpec{
		Name:        "web_fetch",
		Description: "Fetch public HTTP(S) URL text.",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"url":{"type":"string"}},"required":["url"]}`),
	}
}

var (
	reScript = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	reStyle  = regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	reTags   = regexp.MustCompile(`(?s)<[^>]+>`)
	reSpaces = regexp.MustCompile(`\n{3,}`)
)

func (t *webFetchTool) Execute(ctx context.Context, _ *ExecContext, args json.RawMessage) ToolResult {
	var in struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return ToolResult{Content: "invalid json: " + err.Error(), IsError: true}
	}
	parsed, err := url.Parse(strings.TrimSpace(in.URL))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" {
		return ToolResult{Content: "invalid public http/https URL", IsError: true}
	}
	if err := checkPublicHost(parsed.Hostname()); err != nil {
		return ToolResult{Content: err.Error(), IsError: true}
	}

	reqCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return ToolResult{Content: err.Error(), IsError: true}
	}
	req.Header.Set("User-Agent", "daumini-agent/0.1")

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return ToolResult{Content: err.Error(), IsError: true}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		return ToolResult{Content: err.Error(), IsError: true}
	}
	text := string(body)
	if strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "html") {
		text = reScript.ReplaceAllString(text, " ")
		text = reStyle.ReplaceAllString(text, " ")
		text = reTags.ReplaceAllString(text, "\n")
		text = html.UnescapeString(text)
		text = reSpaces.ReplaceAllString(text, "\n\n")
	}
	text = strings.TrimSpace(text)
	if len(text) > 16000 {
		text = text[:16000] + "\n...[truncated]"
	}
	if resp.StatusCode >= 400 {
		return ToolResult{Content: fmt.Sprintf("HTTP %d:\n%s", resp.StatusCode, text), IsError: true}
	}
	return ToolResult{Content: fmt.Sprintf("[HTTP %d %s]\n%s", resp.StatusCode, parsed.String(), text)}
}

func checkPublicHost(host string) error {
	if strings.EqualFold(host, "localhost") {
		return fmt.Errorf("refusing loopback host %q", host)
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return err
	}
	for _, ip := range ips {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
			return fmt.Errorf("refusing non-public IP %s for %q", ip.String(), host)
		}
	}
	return nil
}
