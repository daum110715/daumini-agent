package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/daum110715/daumini-agent/internal/agent"
	"github.com/daum110715/daumini-agent/internal/config"
	"github.com/daum110715/daumini-agent/internal/server"
	"github.com/daum110715/daumini-agent/internal/session"
	"github.com/daum110715/daumini-agent/internal/webui"
)

func TestFSBrowseAndPlatformLedger(t *testing.T) {
	mockDaumini := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/v1/generation":
			_, _ = w.Write([]byte(`{"data":{"id":"gen_test1","resolved_model":"deepseek/deepseek-v4.1flash","charged_nusd":120000,"prompt_tokens":10000,"completion_tokens":400,"cache_usage":{"cache_read_tokens":9500},"attempts":[{"id":"att_1","sequence":1,"endpoint_id":"ep_1","state":"succeeded","http_status":200,"response_committed":true}]}}`))
		case r.URL.Path == "/api/v1/generation/gen_test1/reconciliation":
			_, _ = w.Write([]byte(`{"data":{"status":"settled"}}`))
		case r.URL.Path == "/api/v1/currencies":
			_, _ = w.Write([]byte(`{"data":[{"code":"CNY","rate_per_usd":7.25}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer mockDaumini.Close()

	dataDir := t.TempDir()
	ws := t.TempDir()
	cfgMgr, err := config.NewManager(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = cfgMgr.Update(func(c *config.Config) error {
		for i := range c.Providers {
			if c.Providers[i].ID == "daumini" {
				c.Providers[i].BaseURL = mockDaumini.URL
				c.Providers[i].APIKey = "sk-dm-test"
			}
		}
		return nil
	})
	store, err := session.NewStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	mgr := agent.NewManager(cfgMgr, store)
	defer mgr.Shutdown()

	srv := server.New(cfgMgr, mgr, webui.FS())
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	parentJSON, _ := json.Marshal(ws)
	mkResp, err := http.Post(ts.URL+"/api/fs/mkdir", "application/json", strings.NewReader(`{"parent":`+string(parentJSON)+`,"name":"subproj"}`))
	if err != nil {
		t.Fatal(err)
	}
	mkResp.Body.Close()
	if mkResp.StatusCode != http.StatusOK {
		t.Fatalf("mkdir status=%d", mkResp.StatusCode)
	}

	brResp, err := http.Get(ts.URL + "/api/fs/browse?path=" + url.QueryEscape(ws))
	if err != nil {
		t.Fatal(err)
	}
	defer brResp.Body.Close()
	var br struct {
		Dirs []struct {
			Name string `json:"name"`
		} `json:"dirs"`
	}
	if err := json.NewDecoder(brResp.Body).Decode(&br); err != nil {
		t.Fatal(err)
	}
	if len(br.Dirs) != 1 || br.Dirs[0].Name != "subproj" {
		t.Fatalf("unexpected browse dirs: %+v", br.Dirs)
	}

	genResp, err := http.Get(ts.URL + "/api/daumini/generation/gen_test1")
	if err != nil {
		t.Fatal(err)
	}
	defer genResp.Body.Close()
	if genResp.StatusCode != http.StatusOK {
		t.Fatalf("generation proxy status=%d", genResp.StatusCode)
	}
	var genPayload struct {
		Generation struct {
			ID       string `json:"id"`
			Attempts []any  `json:"attempts"`
		} `json:"generation"`
		Reconciliation map[string]any `json:"reconciliation"`
	}
	if err := json.NewDecoder(genResp.Body).Decode(&genPayload); err != nil {
		t.Fatal(err)
	}
	if genPayload.Generation.ID != "gen_test1" || len(genPayload.Generation.Attempts) != 1 || genPayload.Reconciliation["status"] != "settled" {
		t.Fatalf("unexpected generation proxy payload: %+v", genPayload)
	}

	ledResp, err := http.Get(ts.URL + "/api/daumini/usage-ledger")
	if err != nil {
		t.Fatal(err)
	}
	ledResp.Body.Close()
	if ledResp.StatusCode != http.StatusOK {
		t.Fatalf("usage-ledger status=%d", ledResp.StatusCode)
	}
}
