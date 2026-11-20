package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/daum110715/daumini-agent/internal/config"
	"github.com/daum110715/daumini-agent/internal/types"
)

func (s *Server) dauminiProvider() (config.ProviderConfig, string, string) {
	p, ok := s.cfgMgr.FindProvider("daumini")
	if !ok {
		return config.ProviderConfig{}, "https://mini.daum.pw", ""
	}
	base := strings.TrimRight(strings.TrimSpace(p.BaseURL), "/")
	if base == "" {
		base = "https://mini.daum.pw"
	}
	return p, base, p.ResolveAPIKey()
}

func dauminiProxyGET(ctx context.Context, targetURL, apiKey string) (int, json.RawMessage, error) {
	reqCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, targetURL, nil)
	if err != nil {
		return 0, nil, err
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return resp.StatusCode, nil, err
	}
	if resp.StatusCode >= 400 {
		return resp.StatusCode, nil, parseDauminiError(resp.StatusCode, body)
	}
	return resp.StatusCode, json.RawMessage(body), nil
}

func (s *Server) handleDauminiStatus(w http.ResponseWriter, r *http.Request) {
	prov, base, key := s.dauminiProvider()

	var (
		wg         sync.WaitGroup
		keyData    any
		keyErr     string
		currencies any
	)

	wg.Add(1)
	go func() {
		defer wg.Done()
		if _, raw, err := dauminiProxyGET(r.Context(), base+"/api/v1/currencies", ""); err == nil {
			var parsed any
			if json.Unmarshal(raw, &parsed) == nil {
				currencies = parsed
			}
		}
	}()

	if key != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, raw, err := dauminiProxyGET(r.Context(), base+"/api/v1/key", key)
			if err != nil {
				keyErr = err.Error()
				return
			}
			var env struct {
				Data any `json:"data"`
			}
			if json.Unmarshal(raw, &env) == nil && env.Data != nil {
				keyData = env.Data
			}
		}()
	}

	wg.Wait()
	writeJSON(w, http.StatusOK, map[string]any{
		"connected":  key != "",
		"base_url":   base,
		"user_email": prov.UserEmail,
		"key":        keyData,
		"key_error":  keyErr,
		"currencies": currencies,
	})
}

func (s *Server) handleDauminiGeneration(w http.ResponseWriter, r *http.Request) {
	genID := strings.TrimSpace(r.PathValue("id"))
	if genID == "" {
		writeErr(w, http.StatusBadRequest, "missing generation id")
		return
	}
	_, base, key := s.dauminiProvider()
	if key == "" {
		writeErr(w, http.StatusUnauthorized, "未连接 daumini 账号")
		return
	}

	var (
		wg         sync.WaitGroup
		genData    any
		genErr     error
		reconData  any
		currencies any
	)

	wg.Add(3)
	go func() {
		defer wg.Done()
		_, raw, err := dauminiProxyGET(r.Context(), base+"/api/v1/generation?id="+url.QueryEscape(genID), key)
		if err != nil {
			genErr = err
			return
		}
		var env struct {
			Data any `json:"data"`
		}
		if json.Unmarshal(raw, &env) == nil && env.Data != nil {
			genData = env.Data
		} else {
			var fallback any
			_ = json.Unmarshal(raw, &fallback)
			genData = fallback
		}
	}()

	go func() {
		defer wg.Done()
		_, raw, err := dauminiProxyGET(r.Context(), base+"/api/v1/generation/"+url.PathEscape(genID)+"/reconciliation", key)
		if err != nil {
			return
		}
		var env struct {
			Data any `json:"data"`
		}
		if json.Unmarshal(raw, &env) == nil && env.Data != nil {
			reconData = env.Data
		} else {
			var fallback any
			_ = json.Unmarshal(raw, &fallback)
			reconData = fallback
		}
	}()

	go func() {
		defer wg.Done()
		if _, raw, err := dauminiProxyGET(r.Context(), base+"/api/v1/currencies", ""); err == nil {
			var parsed any
			if json.Unmarshal(raw, &parsed) == nil {
				currencies = parsed
			}
		}
	}()

	wg.Wait()
	if genErr != nil {
		writeErr(w, http.StatusBadGateway, genErr.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"generation":     genData,
		"reconciliation": reconData,
		"currencies":     currencies,
		"base_url":       base,
	})
}

func (s *Server) handleDauminiCatalog(w http.ResponseWriter, r *http.Request) {
	_, base, key := s.dauminiProvider()
	var (
		wg         sync.WaitGroup
		catalogRaw json.RawMessage
		catalogErr error
		currencies any
	)

	wg.Add(2)
	go func() {
		defer wg.Done()
		_, raw, err := dauminiProxyGET(r.Context(), base+"/api/v1/models", key)
		catalogRaw = raw
		catalogErr = err
	}()
	go func() {
		defer wg.Done()
		if _, raw, err := dauminiProxyGET(r.Context(), base+"/api/v1/currencies", ""); err == nil {
			var parsed any
			if json.Unmarshal(raw, &parsed) == nil {
				currencies = parsed
			}
		}
	}()
	wg.Wait()

	if catalogErr != nil {
		writeErr(w, http.StatusBadGateway, catalogErr.Error())
		return
	}
	var parsed map[string]any
	if err := json.Unmarshal(catalogRaw, &parsed); err != nil {
		writeErr(w, http.StatusBadGateway, "invalid catalog payload")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"data":       parsed["data"],
		"currencies": currencies,
		"base_url":   base,
	})
}

func (s *Server) handleDauminiModelEndpoints(w http.ResponseWriter, r *http.Request) {
	modelID := strings.TrimSpace(r.URL.Query().Get("id"))
	if modelID == "" {
		writeErr(w, http.StatusBadRequest, "missing model id")
		return
	}
	_, base, key := s.dauminiProvider()
	var path string
	if author, slug, ok := strings.Cut(modelID, "/"); ok {
		path = fmt.Sprintf("/api/v1/models/%s/%s/endpoints", url.PathEscape(author), url.PathEscape(slug))
	} else {
		path = fmt.Sprintf("/api/v1/models/%s/endpoints", url.PathEscape(modelID))
	}
	_, raw, err := dauminiProxyGET(r.Context(), base+path, key)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	var parsed any
	_ = json.Unmarshal(raw, &parsed)
	writeJSON(w, http.StatusOK, parsed)
}

type ledgerEntry struct {
	SessionID    string      `json:"session_id"`
	SessionTitle string      `json:"session_title"`
	MessageID    string      `json:"message_id"`
	ProviderID   string      `json:"provider_id"`
	ModelID      string      `json:"model_id"`
	GenerationID string      `json:"generation_id,omitempty"`
	RequestID    string      `json:"request_id,omitempty"`
	Usage        types.Usage `json:"usage"`
	CreatedAt    time.Time   `json:"created_at"`
}

type ledgerModelSummary struct {
	ModelID       string      `json:"model_id"`
	ProviderID    string      `json:"provider_id"`
	RequestCount  int         `json:"request_count"`
	Usage         types.Usage `json:"usage"`
	CacheHitRatio float64     `json:"cache_hit_ratio"`
}

func (s *Server) handleDauminiUsageLedger(w http.ResponseWriter, _ *http.Request) {
	sessions, err := s.mgr.ListSessions(true)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	var totals types.Usage
	var requestCount int
	byModelMap := make(map[string]*ledgerModelSummary)
	var recent []ledgerEntry

	for _, sm := range sessions {
		snap, err := s.mgr.GetSnapshot(sm.ID)
		if err != nil {
			continue
		}
		for _, msg := range snap.Messages {
			if msg.Role != types.RoleAssistant || msg.Usage == nil {
				continue
			}
			u := *msg.Usage
			totals.Add(u)
			requestCount++

			pid := msg.ProviderID
			if pid == "" {
				pid = snap.Meta.ProviderID
			}
			mid := msg.ModelID
			if mid == "" {
				mid = snap.Meta.ModelID
			}
			key := pid + "::" + mid
			ms, ok := byModelMap[key]
			if !ok {
				ms = &ledgerModelSummary{ModelID: mid, ProviderID: pid}
				byModelMap[key] = ms
			}
			ms.RequestCount++
			ms.Usage.Add(u)

			recent = append(recent, ledgerEntry{
				SessionID:    sm.ID,
				SessionTitle: sm.Title,
				MessageID:    msg.ID,
				ProviderID:   pid,
				ModelID:      mid,
				GenerationID: u.GenerationID,
				RequestID:    u.RequestID,
				Usage:        u,
				CreatedAt:    msg.CreatedAt,
			})
		}
	}

	sort.Slice(recent, func(i, j int) bool {
		return recent[i].CreatedAt.After(recent[j].CreatedAt)
	})
	if len(recent) > 80 {
		recent = recent[:80]
	}

	byModel := make([]ledgerModelSummary, 0, len(byModelMap))
	for _, ms := range byModelMap {
		if tin := ms.Usage.TotalInput(); tin > 0 {
			ms.CacheHitRatio = float64(ms.Usage.CacheReadTokens) / float64(tin)
		}
		byModel = append(byModel, *ms)
	}
	sort.Slice(byModel, func(i, j int) bool {
		if byModel[i].Usage.TotalCostNUSD != byModel[j].Usage.TotalCostNUSD {
			return byModel[i].Usage.TotalCostNUSD > byModel[j].Usage.TotalCostNUSD
		}
		return byModel[i].RequestCount > byModel[j].RequestCount
	})

	var hitRatio float64
	if tin := totals.TotalInput(); tin > 0 {
		hitRatio = float64(totals.CacheReadTokens) / float64(tin)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"totals":          totals,
		"request_count":   requestCount,
		"session_count":   len(sessions),
		"cache_hit_ratio": hitRatio,
		"by_model":        byModel,
		"recent":          recent,
	})
}
