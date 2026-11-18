package agent_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daum110715/daumini-agent/internal/agent"
	"github.com/daum110715/daumini-agent/internal/compaction"
	"github.com/daum110715/daumini-agent/internal/config"
	"github.com/daum110715/daumini-agent/internal/event"
	"github.com/daum110715/daumini-agent/internal/sandbox"
	"github.com/daum110715/daumini-agent/internal/session"
	"github.com/daum110715/daumini-agent/internal/tools"
	"github.com/daum110715/daumini-agent/internal/types"
)

func TestHashlineReadEditAndSymlinkSandbox(t *testing.T) {
	ws := t.TempDir()
	outside := t.TempDir()
	_ = os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("nope"), 0o644)
	_ = os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(ws, "escape.txt"))

	target := filepath.Join(ws, "hello.txt")
	_ = os.WriteFile(target, []byte("alpha\nbeta\ngamma\n"), 0o644)

	sb, err := sandbox.New(ws)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := sb.Resolve("escape.txt"); err == nil {
		t.Fatal("expected symlink escape to be rejected")
	}

	reg := tools.NewRegistry()
	tools.RegisterBuiltins(reg)
	ec := &tools.ExecContext{SessionID: "s1", Sandbox: sb, Reads: tools.NewReadTracker()}

	editTool, _ := reg.Get("fs_edit")
	pre := editTool.Execute(context.Background(), ec, json.RawMessage(fmt.Sprintf(`{"path":"hello.txt","start_anchor":"2#%s","replacement":"BETA"}`, types.LineHash4("beta"))))
	if !pre.IsError || !strings.Contains(pre.Content, "fs_read") {
		t.Fatalf("expected read-before-edit failure, got %+v", pre)
	}

	readTool, _ := reg.Get("fs_read")
	readRes := readTool.Execute(context.Background(), ec, json.RawMessage(`{"path":"hello.txt"}`))
	if readRes.IsError || !strings.Contains(readRes.Content, "2#"+types.LineHash4("beta")+"|beta") {
		t.Fatalf("unexpected fs_read output: %+v", readRes)
	}

	badHash := editTool.Execute(context.Background(), ec, json.RawMessage(`{"path":"hello.txt","start_anchor":"2#0000","replacement":"BETA"}`))
	if !badHash.IsError || !strings.Contains(badHash.Content, "hash mismatch") {
		t.Fatalf("expected hash mismatch error, got %+v", badHash)
	}

	goodEdit := editTool.Execute(context.Background(), ec, json.RawMessage(fmt.Sprintf(`{"path":"hello.txt","start_anchor":"2#%s","replacement":"BETA"}`, types.LineHash4("beta"))))
	if goodEdit.IsError {
		t.Fatalf("unexpected edit error: %+v", goodEdit)
	}
	after, _ := os.ReadFile(target)
	if string(after) != "alpha\nBETA\ngamma\n" {
		t.Fatalf("unexpected file contents: %q", string(after))
	}
}

func TestDoomLoopAndBatchMasking(t *testing.T) {
	hook := event.NewDoomLoopHook(3)
	for i := 0; i < 2; i++ {
		d := hook.BeforeTool(context.Background(), event.ToolCallContext{
			SessionID: "s1",
			ToolName:  "fs_read",
			ArgsJSON:  json.RawMessage(`{"b":2,"a":1}`),
		})
		if !d.Allow {
			t.Fatalf("call %d should be allowed", i+1)
		}
	}
	d3 := hook.BeforeTool(context.Background(), event.ToolCallContext{
		SessionID: "s1",
		ToolName:  "fs_read",
		ArgsJSON:  json.RawMessage(`{"a":1,"b":2}`),
	})
	if d3.Allow {
		t.Fatal("3rd identical canonical call should trigger doom loop")
	}

	var msgs []types.Message
	big := strings.Repeat("x", 500)
	for i := 0; i < 24; i++ {
		role := types.RoleAssistant
		blk := types.Block{Type: types.BlockText, Text: "ok"}
		if i%2 == 1 {
			role = types.RoleUser
			blk = types.Block{Type: types.BlockToolResult, ToolUseID: fmt.Sprintf("t%d", i), Text: big}
		}
		msgs = append(msgs, types.Message{Role: role, Blocks: []types.Block{blk}})
	}
	masked, count := compaction.MaskObservationsBatch(msgs, 1000, 900, false)
	if count == 0 || !masked[1].Blocks[0].Masked {
		t.Fatalf("expected batch observation masking, got count=%d", count)
	}
}

func TestEndToEndLoopApprovalAndFork(t *testing.T) {
	var callNum atomic.Int32
	mockLLM := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" && r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		n := callNum.Add(1)
		if n == 1 {
			fmt.Fprint(w, "data: {\"id\":\"g1\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"function\":{\"name\":\"fs_write\",\"arguments\":\"{\\\"path\\\":\\\"note.txt\\\",\\\"content\\\":\\\"hi\\\"}\"}}]}}]}\n\n")
			fmt.Fprint(w, "data: {\"id\":\"g1\",\"choices\":[{\"finish_reason\":\"tool_calls\",\"delta\":{}}],\"usage\":{\"prompt_tokens\":100,\"completion_tokens\":12}}\n\n")
			fmt.Fprint(w, "data: [DONE]\n\n")
			flusher.Flush()
			return
		}
		fmt.Fprint(w, "data: {\"id\":\"g2\",\"choices\":[{\"delta\":{\"content\":\"done\"}}]}\n\n")
		fmt.Fprint(w, "data: {\"id\":\"g2\",\"choices\":[{\"finish_reason\":\"stop\",\"delta\":{}}],\"usage\":{\"prompt_tokens\":120,\"prompt_tokens_details\":{\"cached_tokens\":90},\"completion_tokens\":4}}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer mockLLM.Close()

	dataDir := t.TempDir()
	ws := t.TempDir()
	cfgMgr, err := config.NewManager(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	_, err = cfgMgr.Update(func(c *config.Config) error {
		c.DefaultProvider = "mock"
		c.DefaultModel = "mock-1"
		c.ApprovalMode = types.ApprovalAutoRead
		c.Providers = []config.ProviderConfig{{
			ID:           "mock",
			Name:         "Mock",
			Protocol:     config.ProtocolOpenAIChat,
			BaseURL:      mockLLM.URL,
			APIKey:       "test-key",
			DefaultModel: "mock-1",
			Enabled:      true,
		}}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	store, err := session.NewStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	mgr := agent.NewManager(cfgMgr, store)
	defer mgr.Shutdown()

	meta, err := mgr.CreateSession(ws, "test", "mock", "mock-1", types.ApprovalAutoRead)
	if err != nil {
		t.Fatal(err)
	}
	evCh, _, _, unsub, err := mgr.Subscribe(meta.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unsub()

	if _, err := mgr.SendPrompt(context.Background(), meta.ID, "write note.txt", "mock", "mock-1", ""); err != nil {
		t.Fatal(err)
	}

	deadline := time.After(5 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for mock agent loop")
		case ev := <-evCh:
			if ev.Type == "approval.requested" {
				var ap types.Approval
				_ = json.Unmarshal(ev.Payload, &ap)
				if err := mgr.ResolveApproval(meta.ID, ap.ID, types.DecisionApproveOnce); err != nil {
					t.Fatal(err)
				}
			}
			if ev.Type == "run.finished" {
				var run types.Run
				_ = json.Unmarshal(ev.Payload, &run)
				if run.Status != types.RunCompleted {
					t.Fatalf("unexpected run finish: %+v", run)
				}
				got, err := os.ReadFile(filepath.Join(ws, "note.txt"))
				if err != nil || string(got) != "hi" {
					t.Fatalf("expected note.txt=hi, got %q err=%v", string(got), err)
				}
				forked, err := mgr.ForkSession(meta.ID, 0, "branch")
				if err != nil || forked.Meta.ParentSessionID != meta.ID {
					t.Fatalf("fork failed: %+v err=%v", forked.Meta, err)
				}
				return
			}
		}
	}
}
