package server_test

import (
	"crypto/sha256"
	"encoding/base64"
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
	"github.com/daum110715/daumini-agent/internal/types"
	"github.com/daum110715/daumini-agent/internal/webui"
)

func TestDeleteSessionAndPKCEOAuthFlow(t *testing.T) {
	var issuedChallenge string
	var issuedRedirect string
	mockDaumini := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/auth/app-exchange" && r.URL.Path != "/backend/auth/app-exchange" {
			http.NotFound(w, r)
			return
		}
		var in struct {
			Code         string `json:"code"`
			CodeVerifier string `json:"code_verifier"`
			RedirectURI  string `json:"redirect_uri"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		sum := sha256.Sum256([]byte(in.CodeVerifier))
		gotChallenge := base64.RawURLEncoding.EncodeToString(sum[:])
		if in.Code != "dmac_test_123" || gotChallenge != issuedChallenge || in.RedirectURI != issuedRedirect {
			http.Error(w, `{"error":{"code":"invalid_grant","message":"bad pkce"}}`, 400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"key":"sk-dm-pkce-test","key_id":"key_1","prefix":"sk-dm-pkce","name":"daumini-agent","email":"admin@example.test","workspace_id":"ws_1"}`))
	}))
	defer mockDaumini.Close()

	dataDir := t.TempDir()
	ws := t.TempDir()
	cfgMgr, err := config.NewManager(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	store, err := session.NewStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	mgr := agent.NewManager(cfgMgr, store)
	defer mgr.Shutdown()

	srv := server.New(cfgMgr, mgr, webui.FS())
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// 1. Create and delete a session.
	meta, err := mgr.CreateSession(ws, "to-delete", "daumini", "deepseek/deepseek-v4.1flash", types.ApprovalAutoRead)
	if err != nil {
		t.Fatal(err)
	}
	delReq, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/sessions/"+meta.ID, nil)
	delResp, err := http.DefaultClient.Do(delReq)
	if err != nil {
		t.Fatal(err)
	}
	delResp.Body.Close()
	if delResp.StatusCode != http.StatusOK {
		t.Fatalf("DELETE session status=%d", delResp.StatusCode)
	}
	getResp, err := http.Get(ts.URL + "/api/sessions/" + meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	getResp.Body.Close()
	if getResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 after delete, got %d", getResp.StatusCode)
	}

	// 2. PKCE start + callback stores key and email.
	startResp, err := http.Post(ts.URL+"/api/auth/daumini/start", "application/json", strings.NewReader(`{"base_url":"`+mockDaumini.URL+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer startResp.Body.Close()
	if startResp.StatusCode != http.StatusOK {
		t.Fatalf("start auth status=%d", startResp.StatusCode)
	}
	var startData struct {
		AuthorizeURL string `json:"authorize_url"`
		State        string `json:"state"`
	}
	if err := json.NewDecoder(startResp.Body).Decode(&startData); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(startData.AuthorizeURL)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	issuedChallenge = q.Get("code_challenge")
	issuedRedirect = q.Get("redirect_uri")

	noFollowClient := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	cbURL := issuedRedirect + "?code=dmac_test_123&state=" + url.QueryEscape(startData.State)
	cbResp, err := noFollowClient.Get(cbURL)
	if err != nil {
		t.Fatal(err)
	}
	cbResp.Body.Close()
	if cbResp.StatusCode != http.StatusFound || cbResp.Header.Get("Location") != "/?auth=ok" {
		t.Fatalf("unexpected callback redirect: status=%d location=%q", cbResp.StatusCode, cbResp.Header.Get("Location"))
	}

	dm, ok := cfgMgr.FindProvider("daumini")
	if !ok || dm.APIKey != "sk-dm-pkce-test" || dm.UserEmail != "admin@example.test" {
		t.Fatalf("unexpected stored daumini config: ok=%v provider=%+v", ok, dm)
	}
}
