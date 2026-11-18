package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/daum110715/daumini-agent/internal/agent"
	"github.com/daum110715/daumini-agent/internal/config"
	"github.com/daum110715/daumini-agent/internal/server"
	"github.com/daum110715/daumini-agent/internal/session"
	"github.com/daum110715/daumini-agent/internal/types"
)

const Version = "0.1.0"

func Run(args []string, webFS fs.FS) int {
	if len(args) == 0 {
		if !isTerminal(os.Stdin) {
			return cmdRun([]string{}, webFS)
		}
		return cmdInteractive([]string{}, webFS)
	}
	sub := args[0]
	rest := args[1:]
	switch sub {
	case "web":
		return cmdWeb(rest, webFS)
	case "run":
		return cmdRun(rest, webFS)
	case "chat", "tui", "repl":
		return cmdInteractive(rest, webFS)
	case "sessions":
		return cmdSessions(rest)
	case "models":
		return cmdModels(rest)
	case "mcp":
		return cmdMCP(rest)
	case "skills":
		return cmdSkills(rest)
	case "config":
		return cmdConfig(rest)
	case "version", "-v", "--version":
		fmt.Printf("daumini-agent %s\n", Version)
		return 0
	case "help", "-h", "--help":
		printHelp()
		return 0
	default:
		if strings.HasPrefix(sub, "-") {
			return cmdInteractive(args, webFS)
		}
		fmt.Fprintf(os.Stderr, "未知命令: %s\n\n", sub)
		printHelp()
		return 2
	}
}

func printHelp() {
	fmt.Print(`daumini-agent - 本地独立运行的纯种 Agent 工具

用法:
  daumini-agent                    启动交互式终端会话 (支持浅色/深色终端自适应)
  daumini-agent web                启动本地 Web 工作台并在浏览器自动打开
  daumini-agent run [选项] [提示]   无头单次执行 (支持管道 stdin 与 --json)
  daumini-agent config             查看或设置 Provider / Key / 主题 / 审批策略
  daumini-agent models [provider]  列出可用模型
  daumini-agent sessions           列出或分叉历史会话
  daumini-agent mcp                查看已配置的 MCP 服务器状态
  daumini-agent skills             查看发现的 SKILL.md 技能
  daumini-agent version            输出版本号

常用选项:
  --dir <path>                     工作区目录 (默认当前目录)
  --provider <id>                  指定 Provider (如 deepseek, openai, anthropic, daumini)
  --model <id>                     指定模型 ID
  --theme <auto|light|dark>        终端配色模式 (默认自动探测浅色/深色背景)
  --yolo                           跳过工具审批确认
`)
}

func initRuntime() (*config.Manager, *session.Store, *agent.Manager, error) {
	cfgMgr, err := config.NewManager("")
	if err != nil {
		return nil, nil, nil, err
	}
	store, err := session.NewStore(cfgMgr.DataDir())
	if err != nil {
		return nil, nil, nil, err
	}
	mgr := agent.NewManager(cfgMgr, store)
	mgr.RecoverAndInit(context.Background())
	return cfgMgr, store, mgr, nil
}

func cmdWeb(args []string, webFS fs.FS) int {
	fsFlags := flag.NewFlagSet("web", flag.ContinueOnError)
	addr := fsFlags.String("addr", "", "listen address (default from config, e.g. 127.0.0.1:3847)")
	dir := fsFlags.String("dir", ".", "default workspace directory")
	noOpen := fsFlags.Bool("no-open", false, "do not auto-open browser")
	themePref := fsFlags.String("theme", "", "terminal theme: auto|light|dark")
	if err := fsFlags.Parse(args); err != nil {
		return 2
	}

	cfgMgr, _, mgr, err := initRuntime()
	if err != nil {
		fmt.Fprintln(os.Stderr, "初始化失败:", err)
		return 1
	}
	defer mgr.Shutdown()

	cfg := cfgMgr.Get()
	pal := DetectPalette(firstNonEmpty(*themePref, cfg.Theme))
	listenAddr := firstNonEmpty(*addr, cfg.WebAddr, "127.0.0.1:3847")

	if sessions, err := mgr.ListSessions(false); err == nil && len(sessions) == 0 {
		absDir, _ := filepath.Abs(*dir)
		_, _ = mgr.CreateSession(absDir, filepath.Base(absDir), "", "", "")
	}

	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		fmt.Fprintln(os.Stderr, pal.Paint(pal.Error, "端口监听失败: "+err.Error()))
		return 1
	}
	url := "http://" + ln.Addr().String()
	srv := server.New(cfgMgr, mgr, webFS)
	httpSrv := &http.Server{
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	fmt.Println()
	fmt.Println(pal.Paint(pal.Accent, "  daumini-agent 本地工作台已启动"))
	fmt.Println("  " + pal.Paint(pal.Muted, "地址: ") + url)
	fmt.Println("  " + pal.Paint(pal.Muted, "配置: ") + cfgMgr.ConfigPath())
	fmt.Println("  " + pal.Paint(pal.Muted, "按 Ctrl+C 停止"))
	fmt.Println()

	if !*noOpen {
		_ = openBrowser(url)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(shCtx)
	}()

	if err := httpSrv.Serve(ln); err != nil && err != http.ErrServerClosed {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func cmdRun(args []string, _ fs.FS) int {
	fsFlags := flag.NewFlagSet("run", flag.ContinueOnError)
	dir := fsFlags.String("dir", ".", "workspace directory")
	provider := fsFlags.String("provider", "", "provider id")
	model := fsFlags.String("model", "", "model id")
	yolo := fsFlags.Bool("yolo", false, "skip approval prompts")
	jsonOut := fsFlags.Bool("json", false, "emit JSONL session events")
	themePref := fsFlags.String("theme", "", "auto|light|dark")
	if err := fsFlags.Parse(args); err != nil {
		return 2
	}

	prompt := strings.TrimSpace(strings.Join(fsFlags.Args(), " "))
	if prompt == "" && !isTerminal(os.Stdin) {
		raw, _ := io.ReadAll(io.LimitReader(os.Stdin, 512*1024))
		prompt = strings.TrimSpace(string(raw))
	}
	if prompt == "" {
		fmt.Fprintln(os.Stderr, "请传入提示词或通过标准输入提供内容")
		return 2
	}

	cfgMgr, _, mgr, err := initRuntime()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer mgr.Shutdown()

	pal := DetectPalette(firstNonEmpty(*themePref, cfgMgr.Get().Theme))
	mode := cfgMgr.Get().ApprovalMode
	if *yolo {
		mode = types.ApprovalYolo
	}

	meta, err := mgr.CreateSession(*dir, "cli-run", *provider, *model, mode)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	ch, _, _, unsub, err := mgr.Subscribe(meta.ID, 0)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer unsub()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if _, err := mgr.SendPrompt(ctx, meta.ID, prompt, *provider, *model, ""); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	for {
		select {
		case <-ctx.Done():
			mgr.CancelRun(meta.ID)
			return 130
		case ev := <-ch:
			if *jsonOut {
				b, _ := json.Marshal(ev)
				fmt.Println(string(b))
			} else {
				renderEventHeadless(pal, mgr, meta.ID, ev, *yolo)
			}
			if ev.Type == "run.finished" {
				var run types.Run
				_ = json.Unmarshal(ev.Payload, &run)
				if !*jsonOut {
					fmt.Println()
					printUsageLine(pal, run.Usage)
				}
				if run.Status == types.RunFailed {
					return 1
				}
				return 0
			}
		}
	}
}

func cmdInteractive(args []string, webFS fs.FS) int {
	fsFlags := flag.NewFlagSet("chat", flag.ContinueOnError)
	dir := fsFlags.String("dir", ".", "workspace directory")
	provider := fsFlags.String("provider", "", "provider id")
	model := fsFlags.String("model", "", "model id")
	yolo := fsFlags.Bool("yolo", false, "yolo approval mode")
	themePref := fsFlags.String("theme", "", "auto|light|dark")
	sessionIDFlag := fsFlags.String("session", "", "resume session id")
	if err := fsFlags.Parse(args); err != nil {
		return 2
	}

	cfgMgr, _, mgr, err := initRuntime()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer mgr.Shutdown()

	cfg := cfgMgr.Get()
	pal := DetectPalette(firstNonEmpty(*themePref, cfg.Theme))
	mode := cfg.ApprovalMode
	if *yolo {
		mode = types.ApprovalYolo
	}

	var curMeta types.SessionMeta
	if *sessionIDFlag != "" {
		snap, err := mgr.GetSnapshot(*sessionIDFlag)
		if err != nil {
			fmt.Fprintln(os.Stderr, pal.Paint(pal.Error, "找不到会话: "+err.Error()))
			return 1
		}
		curMeta = snap.Meta
	} else {
		curMeta, err = mgr.CreateSession(*dir, "", *provider, *model, mode)
		if err != nil {
			fmt.Fprintln(os.Stderr, pal.Paint(pal.Error, err.Error()))
			return 1
		}
	}

	printBanner(pal, cfgMgr, curMeta)

	// Single stdin reader goroutine so prompt input and HITL approval input never race.
	lineCh := make(chan string, 16)
	eofCh := make(chan struct{})
	go func() {
		sc := bufio.NewScanner(os.Stdin)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			lineCh <- sc.Text()
		}
		close(eofCh)
	}()

	var webSrv *http.Server
	var webURL string
	defer func() {
		if webSrv != nil {
			_ = webSrv.Close()
		}
	}()

	for {
		fmt.Print(pal.Paint(pal.Accent, "you> "))
		var line string
		select {
		case l := <-lineCh:
			line = strings.TrimSpace(l)
		case <-eofCh:
			fmt.Println()
			return 0
		}
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "/") {
			handled, quit, nextMeta, nextPal := handleSlash(line, curMeta, pal, cfgMgr, mgr, webFS, &webSrv, &webURL)
			curMeta = nextMeta
			pal = nextPal
			if quit {
				return 0
			}
			if handled {
				continue
			}
		}

		if err := runInteractiveTurn(pal, mgr, curMeta.ID, line, lineCh, eofCh); err != nil {
			fmt.Println(pal.Paint(pal.Error, "错误: "+err.Error()))
		}
		if snap, err := mgr.GetSnapshot(curMeta.ID); err == nil {
			curMeta = snap.Meta
		}
	}
}

func runInteractiveTurn(pal Palette, mgr *agent.Manager, sessionID, prompt string, lineCh <-chan string, eofCh <-chan struct{}) error {
	ch, _, _, unsub, err := mgr.Subscribe(sessionID, 0)
	if err != nil {
		return err
	}
	defer unsub()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	defer signal.Stop(sigCh)

	if _, err := mgr.SendPrompt(context.Background(), sessionID, prompt, "", "", ""); err != nil {
		return err
	}

	var inThinking bool
	var inText bool
	var mu sync.Mutex

	flushNewline := func() {
		mu.Lock()
		defer mu.Unlock()
		if inThinking || inText {
			fmt.Println()
			inThinking = false
			inText = false
		}
	}

	for {
		select {
		case <-sigCh:
			flushNewline()
			fmt.Println(pal.Paint(pal.Warn, "[中断当前运行...]"))
			mgr.CancelRun(sessionID)
		case <-eofCh:
			mgr.CancelRun(sessionID)
			return nil
		case ev := <-ch:
			switch ev.Type {
			case "assistant.thinking_delta":
				var p struct{ Delta string `json:"delta"` }
				_ = json.Unmarshal(ev.Payload, &p)
				mu.Lock()
				if !inThinking {
					fmt.Print(pal.Paint(pal.Muted, "thinking> "))
					inThinking = true
				}
				fmt.Print(pal.Paint(pal.Italic, p.Delta))
				mu.Unlock()
			case "assistant.text_delta":
				var p struct{ Delta string `json:"delta"` }
				_ = json.Unmarshal(ev.Payload, &p)
				mu.Lock()
				if inThinking {
					fmt.Println()
					inThinking = false
				}
				if !inText {
					fmt.Print(pal.Paint(pal.Accent, "agent> "))
					inText = true
				}
				// Primary body text stays on default terminal foreground for crisp readability on light and dark backgrounds.
				fmt.Print(p.Delta)
				mu.Unlock()
			case "tool.started":
				flushNewline()
				var p struct {
					Name  string          `json:"name"`
					Input json.RawMessage `json:"input"`
				}
				_ = json.Unmarshal(ev.Payload, &p)
				fmt.Printf("%s %s\n", pal.Paint(pal.ToolTag, " "+p.Name+" "), pal.Paint(pal.Muted, compactText(string(p.Input), 110)))
			case "tool.finished":
				var p struct {
					Name       string `json:"name"`
					IsError    bool   `json:"is_error"`
					DurationMs int64  `json:"duration_ms"`
					Output     string `json:"output"`
				}
				_ = json.Unmarshal(ev.Payload, &p)
				tag := pal.Paint(pal.Success, "ok")
				if p.IsError {
					tag = pal.Paint(pal.Error, "err")
				}
				fmt.Printf("  [%s %dms] %s\n", tag, p.DurationMs, pal.Paint(pal.Muted, compactText(p.Output, 140)))
			case "approval.requested":
				flushNewline()
				var ap types.Approval
				_ = json.Unmarshal(ev.Payload, &ap)
				fmt.Println(pal.Paint(pal.Warn, fmt.Sprintf("\n需要确认工具 %s: %s", ap.ToolName, compactText(string(ap.ArgsJSON), 160))))
				fmt.Print(pal.Paint(pal.Warn, "[y] 允许一次  [a] 本会话同类放行  [n] 拒绝: "))
				var ans string
				select {
				case l := <-lineCh:
					ans = strings.ToLower(strings.TrimSpace(l))
				case <-sigCh:
					mgr.CancelRun(sessionID)
					continue
				case <-eofCh:
					mgr.CancelRun(sessionID)
					return nil
				}
				dec := types.DecisionDeny
				if ans == "y" || ans == "yes" {
					dec = types.DecisionApproveOnce
				} else if ans == "a" || ans == "always" {
					dec = types.DecisionApproveAlways
				}
				_ = mgr.ResolveApproval(sessionID, ap.ID, dec)
			case "run.finished":
				flushNewline()
				var run types.Run
				_ = json.Unmarshal(ev.Payload, &run)
				if run.Status == types.RunFailed && run.Error != "" {
					fmt.Println(pal.Paint(pal.Error, "失败: "+run.Error))
				}
				printUsageLine(pal, run.Usage)
				return nil
			}
		}
	}
}

func handleSlash(line string, meta types.SessionMeta, pal Palette, cfgMgr *config.Manager, mgr *agent.Manager, webFS fs.FS, webSrv **http.Server, webURL *string) (handled bool, quit bool, nextMeta types.SessionMeta, nextPal Palette) {
	fields := strings.Fields(line)
	cmd := strings.ToLower(fields[0])
	arg := ""
	if len(fields) > 1 {
		arg = strings.TrimSpace(strings.TrimPrefix(line, fields[0]))
	}
	nextMeta = meta
	nextPal = pal

	switch cmd {
	case "/exit", "/quit", "/q":
		return true, true, nextMeta, nextPal
	case "/help":
		fmt.Println(pal.Paint(pal.Muted, `斜杠命令:
  /new                 新建会话
  /sessions            查看会话列表
  /switch <id>         切换会话
  /fork [标题]         从当前消息分叉新会话
  /model <prov/model>  切换 Provider 与模型 (例如 deepseek/deepseek-chat)
  /mode <ask|auto_read|yolo>  切换工具审批策略
  /mcp                 查看 MCP 连接与工具
  /skills              查看可用 SKILL.md
  /tools               查看已注册工具列表
  /web                 启动并在浏览器打开本地 Web 工作台
  /theme <auto|light|dark>    切换浅色/深色终端配色
  /clear               清屏
  /exit                退出`))
		return true, false, nextMeta, nextPal
	case "/clear":
		fmt.Print("\x1b[2J\x1b[H")
		return true, false, nextMeta, nextPal
	case "/new":
		m, err := mgr.CreateSession(meta.WorkspaceDir, "", meta.ProviderID, meta.ModelID, meta.ApprovalMode)
		if err != nil {
			fmt.Println(pal.Paint(pal.Error, err.Error()))
		} else {
			nextMeta = m
			fmt.Println(pal.Paint(pal.Success, "已新建会话: "+m.ID))
		}
		return true, false, nextMeta, nextPal
	case "/sessions":
		list, _ := mgr.ListSessions(false)
		for _, s := range list {
			mark := " "
			if s.ID == meta.ID {
				mark = "*"
			}
			fmt.Printf(" %s %s  %s/%s  (%d msgs)  %s\n", mark, s.ID, s.ProviderID, s.ModelID, s.MessageCount, pal.Paint(pal.Muted, s.Title))
		}
		return true, false, nextMeta, nextPal
	case "/switch":
		if arg == "" {
			fmt.Println(pal.Paint(pal.Warn, "用法: /switch <session_id>"))
			return true, false, nextMeta, nextPal
		}
		snap, err := mgr.GetSnapshot(arg)
		if err != nil {
			fmt.Println(pal.Paint(pal.Error, err.Error()))
		} else {
			nextMeta = snap.Meta
			fmt.Println(pal.Paint(pal.Success, "已切换至会话: "+nextMeta.ID))
		}
		return true, false, nextMeta, nextPal
	case "/fork":
		snap, err := mgr.ForkSession(meta.ID, 0, arg)
		if err != nil {
			fmt.Println(pal.Paint(pal.Error, err.Error()))
		} else {
			nextMeta = snap.Meta
			fmt.Println(pal.Paint(pal.Success, "已分叉并切换至: "+nextMeta.ID))
		}
		return true, false, nextMeta, nextPal
	case "/model":
		if arg == "" {
			fmt.Printf("当前: %s / %s\n", meta.ProviderID, meta.ModelID)
			return true, false, nextMeta, nextPal
		}
		provID := meta.ProviderID
		modID := arg
		if p, m, ok := strings.Cut(arg, "/"); ok {
			if _, exists := cfgMgr.FindProvider(p); exists {
				provID = p
				modID = m
			}
		}
		m, err := mgr.UpdateSessionSettings(meta.ID, provID, modID, "", "", nil, "")
		if err != nil {
			fmt.Println(pal.Paint(pal.Error, err.Error()))
		} else {
			nextMeta = m
			fmt.Println(pal.Paint(pal.Success, fmt.Sprintf("已切换模型: %s / %s", m.ProviderID, m.ModelID)))
		}
		return true, false, nextMeta, nextPal
	case "/mode":
		mode := types.ApprovalMode(arg)
		if mode != types.ApprovalAsk && mode != types.ApprovalAutoRead && mode != types.ApprovalYolo {
			fmt.Println(pal.Paint(pal.Warn, "可选模式: ask, auto_read, yolo"))
			return true, false, nextMeta, nextPal
		}
		m, err := mgr.UpdateSessionSettings(meta.ID, "", "", mode, "", nil, "")
		if err != nil {
			fmt.Println(pal.Paint(pal.Error, err.Error()))
		} else {
			nextMeta = m
			fmt.Println(pal.Paint(pal.Success, "审批模式已切换为: "+string(mode)))
		}
		return true, false, nextMeta, nextPal
	case "/mcp":
		sts := mgr.MCPStatuses()
		if len(sts) == 0 {
			fmt.Println(pal.Paint(pal.Muted, "暂未配置或启用 MCP 服务器"))
		}
		for _, s := range sts {
			fmt.Printf(" - %s (%s) connected=%v tools=%d %s\n", s.Name, s.Transport, s.Connected, len(s.Tools), s.Error)
		}
		return true, false, nextMeta, nextPal
	case "/skills":
		sks := mgr.DiscoverSkills(meta.WorkspaceDir)
		if len(sks) == 0 {
			fmt.Println(pal.Paint(pal.Muted, "未发现本地 SKILL.md"))
		}
		for _, s := range sks {
			fmt.Printf(" - %s (%s): %s\n", s.Name, s.Scope, s.Description)
		}
		return true, false, nextMeta, nextPal
	case "/tools":
		for _, ts := range mgr.ToolSpecs() {
			fmt.Printf(" - %s: %s\n", ts.Name, compactText(ts.Description, 90))
		}
		return true, false, nextMeta, nextPal
	case "/theme":
		if arg == "light" || arg == "dark" || arg == "auto" {
			nextPal = DetectPalette(arg)
			_, _ = cfgMgr.Update(func(c *config.Config) error {
				c.Theme = arg
				return nil
			})
			fmt.Println(nextPal.Paint(nextPal.Success, fmt.Sprintf("终端配色已切为 %s (isLight=%v)", arg, nextPal.IsLight)))
		} else {
			fmt.Printf("当前配色: %s (isLight=%v)\n", pal.Mode, pal.IsLight)
		}
		return true, false, nextMeta, nextPal
	case "/web":
		if *webSrv == nil {
			addr := cfgMgr.Get().WebAddr
			ln, err := net.Listen("tcp", addr)
			if err != nil {
				ln, err = net.Listen("tcp", "127.0.0.1:0")
			}
			if err != nil {
				fmt.Println(pal.Paint(pal.Error, "启动 Web 失败: "+err.Error()))
				return true, false, nextMeta, nextPal
			}
			*webURL = "http://" + ln.Addr().String()
			s := server.New(cfgMgr, mgr, webFS)
			*webSrv = &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second}
			go func(srv *http.Server, l net.Listener) { _ = srv.Serve(l) }(*webSrv, ln)
		}
		fmt.Println(pal.Paint(pal.Success, "本地 Web 工作台: "+*webURL))
		_ = openBrowser(*webURL)
		return true, false, nextMeta, nextPal
	default:
		return false, false, nextMeta, nextPal
	}
}

func renderEventHeadless(pal Palette, mgr *agent.Manager, sessionID string, ev types.SessionEvent, autoDenyOrYolo bool) {
	switch ev.Type {
	case "assistant.text_delta":
		var p struct{ Delta string `json:"delta"` }
		_ = json.Unmarshal(ev.Payload, &p)
		fmt.Print(p.Delta)
	case "tool.started":
		var p struct{ Name string `json:"name"` }
		_ = json.Unmarshal(ev.Payload, &p)
		fmt.Fprintf(os.Stderr, "\n%s\n", pal.Paint(pal.Muted, "[tool] "+p.Name))
	case "approval.requested":
		var ap types.Approval
		_ = json.Unmarshal(ev.Payload, &ap)
		if autoDenyOrYolo {
			_ = mgr.ResolveApproval(sessionID, ap.ID, types.DecisionApproveOnce)
		} else {
			fmt.Fprintf(os.Stderr, "\n%s\n", pal.Paint(pal.Warn, "无头模式下未开启 --yolo，自动拒绝变更工具: "+ap.ToolName))
			_ = mgr.ResolveApproval(sessionID, ap.ID, types.DecisionDeny)
		}
	}
}

func cmdSessions(args []string) int {
	_, _, mgr, err := initRuntime()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer mgr.Shutdown()

	if len(args) >= 2 && args[0] == "fork" {
		snap, err := mgr.ForkSession(args[1], 0, strings.Join(args[2:], " "))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Println(snap.Meta.ID)
		return 0
	}
	list, err := mgr.ListSessions(true)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	for _, s := range list {
		fmt.Printf("%s\t%s/%s\t%d msgs\t%s\t%s\n", s.ID, s.ProviderID, s.ModelID, s.MessageCount, s.WorkspaceDir, s.Title)
	}
	return 0
}

func cmdModels(args []string) int {
	cfgMgr, _, mgr, err := initRuntime()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer mgr.Shutdown()
	pid := ""
	if len(args) > 0 {
		pid = args[0]
	} else {
		pid = cfgMgr.Get().DefaultProvider
	}
	models, err := mgr.ListModels(context.Background(), pid)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	for _, m := range models {
		fmt.Println(m.ID)
	}
	return 0
}

func cmdMCP(_ []string) int {
	_, _, mgr, err := initRuntime()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer mgr.Shutdown()
	for _, st := range mgr.MCPStatuses() {
		fmt.Printf("%s\t%s\tconnected=%v\ttools=%d\t%s\n", st.Name, st.Transport, st.Connected, len(st.Tools), st.Error)
	}
	return 0
}

func cmdSkills(args []string) int {
	_, _, mgr, err := initRuntime()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer mgr.Shutdown()
	dir := "."
	if len(args) > 0 {
		dir = args[0]
	}
	for _, sk := range mgr.DiscoverSkills(dir) {
		fmt.Printf("%s\t(%s)\t%s\t%s\n", sk.Name, sk.Scope, sk.Description, sk.Path)
	}
	return 0
}

func cmdConfig(args []string) int {
	cfgMgr, err := config.NewManager("")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if len(args) == 0 || args[0] == "show" {
		b, _ := json.MarshalIndent(config.RedactedConfig(cfgMgr.Get()), "", "  ")
		fmt.Println(string(b))
		return 0
	}
	switch args[0] {
	case "path":
		fmt.Println(cfgMgr.ConfigPath())
		return 0
	case "set-key":
		if len(args) < 3 {
			fmt.Fprintln(os.Stderr, "用法: daumini-agent config set-key <provider_id> <api_key> [model]")
			return 2
		}
		pid, key := args[1], args[2]
		model := ""
		if len(args) >= 4 {
			model = args[3]
		}
		_, err := cfgMgr.Update(func(c *config.Config) error {
			for i := range c.Providers {
				if c.Providers[i].ID == pid {
					c.Providers[i].APIKey = key
					c.Providers[i].Enabled = true
					if model != "" {
						c.Providers[i].DefaultModel = model
					}
					c.DefaultProvider = pid
					if c.Providers[i].DefaultModel != "" {
						c.DefaultModel = c.Providers[i].DefaultModel
					}
					return nil
				}
			}
			return fmt.Errorf("未找到 provider %q", pid)
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Println("已保存 API Key 到", cfgMgr.ConfigPath())
		return 0
	case "login":
		base := "https://mini.daum.pw"
		if len(args) > 1 {
			base = strings.TrimRight(args[1], "/")
		}
		reader := bufio.NewReader(os.Stdin)
		fmt.Print("daumini 邮箱: ")
		email, _ := reader.ReadString('\n')
		fmt.Print("密码: ")
		pass, _ := reader.ReadString('\n')
		key, err := server.LoginAndMintDauminiKey(context.Background(), base, strings.TrimSpace(email), strings.TrimRight(pass, "\r\n"), "")
		if err != nil {
			fmt.Fprintln(os.Stderr, "登录失败:", err)
			return 1
		}
		_, err = cfgMgr.Update(func(c *config.Config) error {
			for i := range c.Providers {
				if c.Providers[i].ID == "daumini" {
					c.Providers[i].BaseURL = base
					c.Providers[i].APIKey = key
					c.Providers[i].UserEmail = strings.TrimSpace(email)
					c.Providers[i].Enabled = true
				}
			}
			c.DefaultProvider = "daumini"
			return nil
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Println("已绑定 daumini 中转账号")
		return 0
	default:
		fmt.Fprintln(os.Stderr, "支持子命令: show, path, set-key, login")
		return 2
	}
}

func printBanner(pal Palette, cfgMgr *config.Manager, meta types.SessionMeta) {
	p, _ := cfgMgr.FindProvider(meta.ProviderID)
	keyHint := "未设密钥"
	if p.ResolveAPIKey() != "" {
		keyHint = "密钥就绪"
	}
	modeLabel := "dark"
	if pal.IsLight {
		modeLabel = "light"
	}
	fmt.Println()
	fmt.Printf("%s  %s\n", pal.Paint(pal.Accent, "daumini-agent "+Version), pal.Paint(pal.Muted, "("+modeLabel+" 终端适配)"))
	fmt.Printf("%s %s\n", pal.Paint(pal.Muted, "工作区:"), meta.WorkspaceDir)
	fmt.Printf("%s %s / %s (%s)\n", pal.Paint(pal.Muted, "模型:  "), meta.ProviderID, meta.ModelID, keyHint)
	fmt.Printf("%s 输入 /help 查看命令，输入 /web 打开图形界面\n\n", pal.Paint(pal.Muted, "提示:  "))
}

func printUsageLine(pal Palette, u types.Usage) {
	if u.TotalInput() == 0 && u.OutputTokens == 0 {
		return
	}
	pct := u.CacheHitRate() * 100
	costStr := ""
	if u.TotalCostNUSD > 0 {
		costStr = fmt.Sprintf(" | $%.4f", float64(u.TotalCostNUSD)/1e9)
	}
	line := fmt.Sprintf("cache %.0f%% | in %d (read %d) | out %d%s", pct, u.TotalInput(), u.CacheReadTokens, u.OutputTokens, costStr)
	fmt.Println(pal.Paint(pal.Muted, line))
}

func compactText(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
