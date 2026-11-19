package llm

import (
	"bufio"
	"bytes"
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
	"github.com/daum110715/daumini-agent/internal/tools"
	"github.com/daum110715/daumini-agent/internal/types"
)

type StreamCallbacks struct {
	OnTextDelta     func(string)
	OnThinkingDelta func(string)
}

type GenerateRequest struct {
	Provider        config.ProviderConfig
	ModelID         string
	ReasoningEffort string
	SystemPrompt    string
	Messages        []types.Message
	Tools           []tools.ToolSpec
	PromptCacheKey  string
	MaxTokens       int
	Callbacks       StreamCallbacks
}

type GenerateResponse struct {
	Blocks     []types.Block
	StopReason string
	Usage      types.Usage
	Protocol   string
}

type ModelInfo struct {
	ID                       string   `json:"id"`
	Name                     string   `json:"name,omitempty"`
	Author                   string   `json:"author,omitempty"`
	Description              string   `json:"description,omitempty"`
	ContextWindow            int64    `json:"context_window,omitempty"`
	MaxOutput                int      `json:"max_output,omitempty"`
	SupportedProtocols       []string `json:"supported_protocols,omitempty"`
	SupportedParameters      []string `json:"supported_parameters,omitempty"`
	ReasoningLevels          []string `json:"reasoning_levels,omitempty"`
	SupportsImages           bool     `json:"supports_images,omitempty"`
	SupportsTools            bool     `json:"supports_tools,omitempty"`
	SupportsThinking         bool     `json:"supports_thinking,omitempty"`
	InputNUSDPerMillion      int64    `json:"input_nusd_per_million,omitempty"`
	OutputNUSDPerMillion     int64    `json:"output_nusd_per_million,omitempty"`
	CacheReadNUSDPerMillion  int64    `json:"cache_read_nusd_per_million,omitempty"`
	CacheWriteNUSDPerMillion int64    `json:"cache_write_nusd_per_million,omitempty"`
	RequestNUSD              int64    `json:"request_nusd,omitempty"`
	LatencyMS                int64    `json:"latency_ms,omitempty"`
}

func (m ModelInfo) HasParam(name string) bool {
	for _, p := range m.SupportedParameters {
		if p == name {
			return true
		}
	}
	return false
}

func (m ModelInfo) HasProtocol(name string) bool {
	for _, p := range m.SupportedProtocols {
		if p == name {
			return true
		}
	}
	return false
}

type cachedCatalog struct {
	models    []ModelInfo
	byID      map[string]ModelInfo
	fetchedAt time.Time
}

type Client struct {
	http  *http.Client
	mu    sync.RWMutex
	cache map[string]cachedCatalog
}

func NewClient() *Client {
	return &Client{
		http:  &http.Client{Timeout: 0},
		cache: make(map[string]cachedCatalog),
	}
}

func (c *Client) Generate(ctx context.Context, req GenerateRequest) (GenerateResponse, error) {
	apiKey := req.Provider.ResolveAPIKey()
	if apiKey == "" {
		return GenerateResponse{}, fmt.Errorf("provider %q has no API key configured", req.Provider.ID)
	}
	modelID := strings.TrimSpace(req.ModelID)
	if modelID == "" {
		modelID = req.Provider.DefaultModel
	}
	if modelID == "" {
		return GenerateResponse{}, fmt.Errorf("no model specified for provider %q", req.Provider.ID)
	}

	var meta *ModelInfo
	proto := req.Provider.Protocol
	if proto == config.ProtocolDaumini {
		resolvedProto, info := c.resolveDauminiTarget(ctx, req.Provider, modelID)
		proto = resolvedProto
		meta = info
	}

	if req.MaxTokens <= 0 {
		req.MaxTokens = 16384
	}
	if meta != nil && meta.MaxOutput > 0 && req.MaxTokens > meta.MaxOutput {
		req.MaxTokens = meta.MaxOutput
	}

	switch proto {
	case config.ProtocolAnthropic:
		return c.streamAnthropic(ctx, req, apiKey, modelID, meta)
	case config.ProtocolOpenAIResponses:
		return c.streamOpenAIResponses(ctx, req, apiKey, modelID, meta)
	case config.ProtocolDeepSeek:
		return c.streamOpenAIChat(ctx, req, apiKey, modelID, true, meta)
	default:
		return c.streamOpenAIChat(ctx, req, apiKey, modelID, false, meta)
	}
}

func (c *Client) ListModels(ctx context.Context, p config.ProviderConfig) ([]ModelInfo, error) {
	apiKey := p.ResolveAPIKey()
	base := strings.TrimRight(strings.TrimSpace(p.BaseURL), "/")
	if base == "" {
		return nil, fmt.Errorf("empty base_url")
	}
	reqCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	var endpoint string
	switch p.Protocol {
	case config.ProtocolDaumini:
		endpoint = base + "/api/v1/models"
	case config.ProtocolAnthropic:
		endpoint = strings.TrimSuffix(base, "/v1") + "/v1/models"
	default:
		endpoint = base + "/models"
		if !strings.HasSuffix(base, "/v1") && p.Protocol == config.ProtocolOpenAIChat {
			endpoint = base + "/v1/models"
		}
	}

	httpReq, err := http.NewRequestWithContext(reqCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	if apiKey != "" {
		if p.Protocol == config.ProtocolAnthropic {
			httpReq.Header.Set("x-api-key", apiKey)
			httpReq.Header.Set("anthropic-version", "2023-06-01")
		} else {
			httpReq.Header.Set("Authorization", "Bearer "+apiKey)
		}
	}
	for k, v := range p.Headers {
		httpReq.Header.Set(k, v)
	}
	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var parsed struct {
		Data []struct {
			ID                  string   `json:"id"`
			Name                string   `json:"name"`
			Author              string   `json:"author"`
			Description         string   `json:"description"`
			ContextLength       int64    `json:"context_length"`
			InputModalities     []string `json:"input_modalities"`
			SupportedParameters []string `json:"supported_parameters"`
			Capabilities        *struct {
				MaxOutputTokens int      `json:"max_output_tokens"`
				ReasoningLevels []string `json:"reasoning_levels"`
			} `json:"capabilities"`
			Endpoints []struct {
				Status                   string   `json:"status"`
				SupportedProtocols       []string `json:"supported_protocols"`
				SupportedParameters      []string `json:"supported_parameters"`
				InputNUSDPerMillion      int64    `json:"input_nusd_per_million"`
				OutputNUSDPerMillion     int64    `json:"output_nusd_per_million"`
				CacheReadNUSDPerMillion  int64    `json:"cache_read_nusd_per_million"`
				CacheWriteNUSDPerMillion int64    `json:"cache_write_nusd_per_million"`
				RequestNUSD              int64    `json:"request_nusd"`
				MaxOutputTokens          int      `json:"max_output_tokens"`
				LatencyMS                int64    `json:"latency_ms"`
			} `json:"endpoints"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}
	out := make([]ModelInfo, 0, len(parsed.Data))
	byID := make(map[string]ModelInfo, len(parsed.Data))
	for _, item := range parsed.Data {
		name := item.Name
		if name == "" {
			name = item.ID
		}
		info := ModelInfo{
			ID:                  item.ID,
			Name:                name,
			Author:              item.Author,
			Description:         item.Description,
			ContextWindow:       item.ContextLength,
			SupportedParameters: append([]string(nil), item.SupportedParameters...),
		}
		if item.Capabilities != nil {
			info.MaxOutput = item.Capabilities.MaxOutputTokens
			info.ReasoningLevels = append([]string(nil), item.Capabilities.ReasoningLevels...)
		}
		for _, mod := range item.InputModalities {
			if mod == "image" {
				info.SupportsImages = true
			}
		}
		protoSeen := make(map[string]struct{})
		paramSeen := make(map[string]struct{})
		for _, sp := range info.SupportedParameters {
			paramSeen[sp] = struct{}{}
		}
		for _, ep := range item.Endpoints {
			if ep.Status != "" && ep.Status != "active" {
				continue
			}
			for _, pr := range ep.SupportedProtocols {
				if _, ok := protoSeen[pr]; !ok {
					protoSeen[pr] = struct{}{}
					info.SupportedProtocols = append(info.SupportedProtocols, pr)
				}
			}
			for _, pa := range ep.SupportedParameters {
				if _, ok := paramSeen[pa]; !ok {
					paramSeen[pa] = struct{}{}
					info.SupportedParameters = append(info.SupportedParameters, pa)
				}
			}
			if info.MaxOutput == 0 && ep.MaxOutputTokens > 0 {
				info.MaxOutput = ep.MaxOutputTokens
			}
			if info.InputNUSDPerMillion == 0 && ep.InputNUSDPerMillion > 0 {
				info.InputNUSDPerMillion = ep.InputNUSDPerMillion
				info.OutputNUSDPerMillion = ep.OutputNUSDPerMillion
				info.CacheReadNUSDPerMillion = ep.CacheReadNUSDPerMillion
				info.CacheWriteNUSDPerMillion = ep.CacheWriteNUSDPerMillion
				info.RequestNUSD = ep.RequestNUSD
				info.LatencyMS = ep.LatencyMS
			}
		}
		for _, pa := range info.SupportedParameters {
			switch pa {
			case "tools":
				info.SupportsTools = true
			case "thinking", "reasoning_effort", "enable_thinking", "reasoning":
				info.SupportsThinking = true
			}
		}
		if len(info.ReasoningLevels) > 0 {
			info.SupportsThinking = true
		}
		out = append(out, info)
		byID[info.ID] = info
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })

	if p.Protocol == config.ProtocolDaumini {
		c.mu.Lock()
		c.cache[base] = cachedCatalog{
			models:    out,
			byID:      byID,
			fetchedAt: time.Now(),
		}
		c.mu.Unlock()
	}
	return out, nil
}

func (c *Client) resolveDauminiTarget(ctx context.Context, p config.ProviderConfig, modelID string) (config.Protocol, *ModelInfo) {
	base := strings.TrimRight(strings.TrimSpace(p.BaseURL), "/")
	if base == "" {
		return config.ProtocolOpenAIChat, nil
	}

	c.mu.RLock()
	cat, ok := c.cache[base]
	c.mu.RUnlock()
	if !ok || time.Since(cat.fetchedAt) > 5*time.Minute {
		_, _ = c.ListModels(ctx, p)
		c.mu.RLock()
		cat, ok = c.cache[base]
		c.mu.RUnlock()
	}
	if ok {
		if info, found := cat.byID[modelID]; found {
			return pickDauminiProtocol(info), &info
		}
	}

	// Fallback: query the exact daumini endpoints route /api/v1/models/{author}/{slug}/endpoints.
	author, slug, hasSlash := strings.Cut(strings.TrimSpace(modelID), "/")
	if !hasSlash || author == "" || slug == "" {
		return config.ProtocolOpenAIChat, nil
	}
	u := fmt.Sprintf("%s/api/v1/models/%s/%s/endpoints", base, url.PathEscape(author), url.PathEscape(slug))
	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, u, nil)
	if err != nil {
		return config.ProtocolOpenAIChat, nil
	}
	if k := p.ResolveAPIKey(); k != "" {
		req.Header.Set("Authorization", "Bearer "+k)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return config.ProtocolOpenAIChat, nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return config.ProtocolOpenAIChat, nil
	}
	var detail struct {
		Data struct {
			Endpoints []struct {
				Status              string   `json:"status"`
				SupportedProtocols  []string `json:"supported_protocols"`
				SupportedParameters []string `json:"supported_parameters"`
				MaxOutputTokens     int      `json:"max_output_tokens"`
			} `json:"endpoints"`
		} `json:"data"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 512*1024)).Decode(&detail) != nil {
		return config.ProtocolOpenAIChat, nil
	}
	info := ModelInfo{ID: modelID}
	for _, ep := range detail.Data.Endpoints {
		if ep.Status != "" && ep.Status != "active" {
			continue
		}
		info.SupportedProtocols = append(info.SupportedProtocols, ep.SupportedProtocols...)
		info.SupportedParameters = append(info.SupportedParameters, ep.SupportedParameters...)
		if info.MaxOutput == 0 {
			info.MaxOutput = ep.MaxOutputTokens
		}
	}
	return pickDauminiProtocol(info), &info
}

func pickDauminiProtocol(info ModelInfo) config.Protocol {
	// Native Anthropic Messages gets explicit cache_control breakpoints.
	if info.HasProtocol("messages") {
		return config.ProtocolAnthropic
	}
	// Prefer chat.completions for DeepSeek / general models so reasoning_content stays intact across tool loops.
	if info.HasProtocol("chat.completions") {
		return config.ProtocolOpenAIChat
	}
	if info.HasProtocol("responses") {
		return config.ProtocolOpenAIResponses
	}
	return config.ProtocolOpenAIChat
}

func (c *Client) streamOpenAIChat(ctx context.Context, req GenerateRequest, apiKey, modelID string, isDeepSeek bool, meta *ModelInfo) (GenerateResponse, error) {
	base := strings.TrimRight(strings.TrimSpace(req.Provider.BaseURL), "/")
	endpoint := base + "/chat/completions"
	if req.Provider.Protocol == config.ProtocolDaumini {
		endpoint = base + "/api/v1/chat/completions"
	} else if !strings.HasSuffix(base, "/v1") && !isDeepSeek {
		endpoint = base + "/v1/chat/completions"
	}

	msgs := make([]map[string]any, 0, len(req.Messages)+1)
	if strings.TrimSpace(req.SystemPrompt) != "" {
		msgs = append(msgs, map[string]any{
			"role":    "system",
			"content": req.SystemPrompt,
		})
	}
	for _, m := range req.Messages {
		if m.Role == types.RoleAssistant {
			var textBuf strings.Builder
			var thinkBuf strings.Builder
			var toolCalls []map[string]any
			for _, b := range m.Blocks {
				switch b.Type {
				case types.BlockText:
					textBuf.WriteString(b.Text)
				case types.BlockThinking:
					thinkBuf.WriteString(b.Thinking)
				case types.BlockToolUse:
					argsStr := strings.TrimSpace(string(b.Input))
					if argsStr == "" {
						argsStr = "{}"
					}
					toolCalls = append(toolCalls, map[string]any{
						"id":   b.ID,
						"type": "function",
						"function": map[string]any{
							"name":      b.Name,
							"arguments": argsStr,
						},
					})
				}
			}
			item := map[string]any{"role": "assistant"}
			if textBuf.Len() > 0 {
				item["content"] = textBuf.String()
			} else if len(toolCalls) > 0 {
				item["content"] = nil
			} else {
				item["content"] = ""
			}
			if thinkBuf.Len() > 0 {
				// DeepSeek thinking models require reasoning_content back across tool turns.
				item["reasoning_content"] = thinkBuf.String()
			}
			if len(toolCalls) > 0 {
				item["tool_calls"] = toolCalls
			}
			msgs = append(msgs, item)
			continue
		}

		// User message may contain tool_result blocks + text blocks.
		var userTexts []string
		for _, b := range m.Blocks {
			switch b.Type {
			case types.BlockToolResult:
				content := b.Text
				if b.IsError && !strings.HasPrefix(content, "[error]") {
					content = "[error] " + content
				}
				msgs = append(msgs, map[string]any{
					"role":         "tool",
					"tool_call_id": b.ToolUseID,
					"content":      content,
				})
			case types.BlockText, types.BlockSystemNotice:
				if strings.TrimSpace(b.Text) != "" {
					userTexts = append(userTexts, b.Text)
				}
			}
		}
		if len(userTexts) > 0 {
			msgs = append(msgs, map[string]any{
				"role":    "user",
				"content": strings.Join(userTexts, "\n\n"),
			})
		}
	}

	payload := map[string]any{
		"model":      modelID,
		"messages":   msgs,
		"stream":     true,
		"max_tokens": req.MaxTokens,
		"stream_options": map[string]any{
			"include_usage": true,
		},
	}
	if !isDeepSeek && req.Provider.Protocol != config.ProtocolDaumini && req.PromptCacheKey != "" {
		payload["prompt_cache_key"] = req.PromptCacheKey
	}
	if effort := strings.TrimSpace(req.ReasoningEffort); effort != "" {
		if effort == "off" || effort == "none" {
			if meta == nil || meta.HasParam("thinking") {
				payload["thinking"] = map[string]any{"type": "disabled"}
			} else if meta.HasParam("enable_thinking") {
				payload["enable_thinking"] = false
			}
		} else {
			if meta == nil || meta.HasParam("reasoning_effort") {
				payload["reasoning_effort"] = effort
			} else if meta.HasParam("thinking") {
				payload["thinking"] = map[string]any{"type": "enabled"}
			} else if meta.HasParam("enable_thinking") {
				payload["enable_thinking"] = true
			}
		}
	}
	if len(req.Tools) > 0 {
		tList := make([]map[string]any, 0, len(req.Tools))
		for _, t := range req.Tools {
			var params any
			_ = json.Unmarshal(t.Parameters, &params)
			tList = append(tList, map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        t.Name,
					"description": t.Description,
					"parameters":  params,
				},
			})
		}
		payload["tools"] = tList
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return GenerateResponse{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return GenerateResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	for k, v := range req.Provider.Headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return GenerateResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		return GenerateResponse{}, fmt.Errorf("HTTP %d from %s: %s", resp.StatusCode, endpoint, strings.TrimSpace(string(errBody)))
	}

	type tcAcc struct {
		id   string
		name string
		args strings.Builder
	}
	var textAcc strings.Builder
	var thinkAcc strings.Builder
	tcs := make(map[int]*tcAcc)
	var tcOrder []int
	var stopReason string
	var usage types.Usage
	headerGenID := strings.TrimSpace(resp.Header.Get("X-Generation-Id"))
	usage.RequestID = strings.TrimSpace(resp.Header.Get("X-Request-Id"))
	genID := headerGenID

	err = readSSE(resp.Body, func(data string) error {
		if data == "[DONE]" {
			return io.EOF
		}
		var chunk struct {
			ID    string `json:"id"`
			Error *struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
			Choices []struct {
				FinishReason string `json:"finish_reason"`
				Delta        struct {
					Content          string `json:"content"`
					ReasoningContent string `json:"reasoning_content"`
					Reasoning        string `json:"reasoning"`
					ToolCalls        []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
			} `json:"choices"`
			Usage *struct {
				PromptTokens            int64 `json:"prompt_tokens"`
				CompletionTokens        int64 `json:"completion_tokens"`
				PromptCacheHitTokens    int64 `json:"prompt_cache_hit_tokens"`
				PromptCacheMissTokens   int64 `json:"prompt_cache_miss_tokens"`
				PromptTokensDetails     *struct {
					CachedTokens int64 `json:"cached_tokens"`
				} `json:"prompt_tokens_details"`
				CompletionTokensDetails *struct {
					ReasoningTokens int64 `json:"reasoning_tokens"`
				} `json:"completion_tokens_details"`
			} `json:"usage"`
		}
		if json.Unmarshal([]byte(data), &chunk) != nil {
			return nil
		}
		if chunk.Error != nil && chunk.Error.Message != "" {
			if chunk.Error.Code != "" {
				return fmt.Errorf("%s (%s)", chunk.Error.Message, chunk.Error.Code)
			}
			return fmt.Errorf("%s", chunk.Error.Message)
		}
		if headerGenID == "" && chunk.ID != "" {
			genID = chunk.ID
		}
		if chunk.Usage != nil {
			cached := chunk.Usage.PromptCacheHitTokens
			if cached == 0 && chunk.Usage.PromptTokensDetails != nil {
				cached = chunk.Usage.PromptTokensDetails.CachedTokens
			}
			uncached := chunk.Usage.PromptTokens - cached
			if uncached < 0 {
				uncached = 0
			}
			usage.InputTokens = uncached
			usage.CacheReadTokens = cached
			usage.OutputTokens = chunk.Usage.CompletionTokens
			if chunk.Usage.CompletionTokensDetails != nil {
				usage.ReasoningTokens = chunk.Usage.CompletionTokensDetails.ReasoningTokens
			}
		}
		for _, ch := range chunk.Choices {
			if ch.FinishReason != "" {
				stopReason = ch.FinishReason
			}
			think := ch.Delta.ReasoningContent
			if think == "" {
				think = ch.Delta.Reasoning
			}
			if think != "" {
				thinkAcc.WriteString(think)
				if req.Callbacks.OnThinkingDelta != nil {
					req.Callbacks.OnThinkingDelta(think)
				}
			}
			if ch.Delta.Content != "" {
				textAcc.WriteString(ch.Delta.Content)
				if req.Callbacks.OnTextDelta != nil {
					req.Callbacks.OnTextDelta(ch.Delta.Content)
				}
			}
			for _, tc := range ch.Delta.ToolCalls {
				acc, ok := tcs[tc.Index]
				if !ok {
					acc = &tcAcc{}
					tcs[tc.Index] = acc
					tcOrder = append(tcOrder, tc.Index)
				}
				if tc.ID != "" {
					acc.id = tc.ID
				}
				if tc.Function.Name != "" {
					acc.name = tc.Function.Name
				}
				if tc.Function.Arguments != "" {
					acc.args.WriteString(tc.Function.Arguments)
				}
			}
		}
		return nil
	})
	if err != nil && err != io.EOF {
		return GenerateResponse{}, err
	}

	var blocks []types.Block
	if thinkAcc.Len() > 0 {
		blocks = append(blocks, types.Block{Type: types.BlockThinking, Thinking: thinkAcc.String()})
	}
	if textAcc.Len() > 0 {
		blocks = append(blocks, types.Block{Type: types.BlockText, Text: textAcc.String()})
	}
	sort.Ints(tcOrder)
	for _, idx := range tcOrder {
		acc := tcs[idx]
		rawArgs := json.RawMessage("{}")
		if s := strings.TrimSpace(acc.args.String()); s != "" && json.Valid([]byte(s)) {
			rawArgs = json.RawMessage(s)
		}
		id := acc.id
		if id == "" {
			id = types.NewID("call")
		}
		blocks = append(blocks, types.Block{
			Type:  types.BlockToolUse,
			ID:    id,
			Name:  acc.name,
			Input: rawArgs,
		})
	}
	usage.GenerationID = genID
	if req.Provider.Protocol == config.ProtocolDaumini && genID != "" {
		c.reconcileDauminiGeneration(ctx, req.Provider, apiKey, genID, &usage)
	}
	protoName := "openai_chat"
	if isDeepSeek {
		protoName = "deepseek"
	}
	return GenerateResponse{
		Blocks:     blocks,
		StopReason: stopReason,
		Usage:      usage,
		Protocol:   protoName,
	}, nil
}

func (c *Client) streamOpenAIResponses(ctx context.Context, req GenerateRequest, apiKey, modelID string, meta *ModelInfo) (GenerateResponse, error) {
	base := strings.TrimRight(strings.TrimSpace(req.Provider.BaseURL), "/")
	endpoint := base + "/responses"
	if req.Provider.Protocol == config.ProtocolDaumini {
		endpoint = base + "/api/v1/responses"
	} else if !strings.HasSuffix(base, "/v1") {
		endpoint = base + "/v1/responses"
	}

	var input []map[string]any
	for _, m := range req.Messages {
		if m.Role == types.RoleAssistant {
			var txt strings.Builder
			for _, b := range m.Blocks {
				switch b.Type {
				case types.BlockText:
					txt.WriteString(b.Text)
				case types.BlockToolUse:
					if txt.Len() > 0 {
						input = append(input, map[string]any{"role": "assistant", "content": txt.String()})
						txt.Reset()
					}
					args := strings.TrimSpace(string(b.Input))
					if args == "" {
						args = "{}"
					}
					input = append(input, map[string]any{
						"type":      "function_call",
						"call_id":   b.ID,
						"name":      b.Name,
						"arguments": args,
					})
				}
			}
			if txt.Len() > 0 {
				input = append(input, map[string]any{"role": "assistant", "content": txt.String()})
			}
			continue
		}
		var userParts []string
		for _, b := range m.Blocks {
			switch b.Type {
			case types.BlockToolResult:
				content := b.Text
				if b.IsError && !strings.HasPrefix(content, "[error]") {
					content = "[error] " + content
				}
				input = append(input, map[string]any{
					"type":    "function_call_output",
					"call_id": b.ToolUseID,
					"output":  content,
				})
			case types.BlockText, types.BlockSystemNotice:
				if strings.TrimSpace(b.Text) != "" {
					userParts = append(userParts, b.Text)
				}
			}
		}
		if len(userParts) > 0 {
			input = append(input, map[string]any{
				"role":    "user",
				"content": strings.Join(userParts, "\n\n"),
			})
		}
	}

	payload := map[string]any{
		"model":             modelID,
		"instructions":      req.SystemPrompt,
		"input":             input,
		"stream":            true,
		"max_output_tokens": req.MaxTokens,
	}
	if req.PromptCacheKey != "" && (req.Provider.Protocol != config.ProtocolDaumini || (meta != nil && meta.HasParam("prompt_cache_key"))) {
		payload["prompt_cache_key"] = req.PromptCacheKey
	}
	if effort := strings.TrimSpace(req.ReasoningEffort); effort != "" && effort != "off" && effort != "none" {
		if meta == nil || meta.HasParam("reasoning") {
			payload["reasoning"] = map[string]any{"effort": effort}
		}
	}
	if len(req.Tools) > 0 {
		tList := make([]map[string]any, 0, len(req.Tools))
		for _, t := range req.Tools {
			var params any
			_ = json.Unmarshal(t.Parameters, &params)
			tList = append(tList, map[string]any{
				"type":        "function",
				"name":        t.Name,
				"description": t.Description,
				"parameters":  params,
			})
		}
		payload["tools"] = tList
	}

	body, _ := json.Marshal(payload)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return GenerateResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	for k, v := range req.Provider.Headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return GenerateResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		return GenerateResponse{}, fmt.Errorf("HTTP %d from %s: %s", resp.StatusCode, endpoint, strings.TrimSpace(string(errBody)))
	}

	var textAcc strings.Builder
	var thinkAcc strings.Builder
	type fcAcc struct {
		callID string
		name   string
		args   strings.Builder
	}
	fcs := make(map[string]*fcAcc)
	var fcOrder []string
	var stopReason string
	var usage types.Usage
	headerGenID := strings.TrimSpace(resp.Header.Get("X-Generation-Id"))
	usage.RequestID = strings.TrimSpace(resp.Header.Get("X-Request-Id"))
	genID := headerGenID

	err = readSSE(resp.Body, func(data string) error {
		if data == "[DONE]" {
			return io.EOF
		}
		var ev struct {
			Type   string `json:"type"`
			Delta  string `json:"delta"`
			ItemID string `json:"item_id"`
			Error  *struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
			Item   *struct {
				ID        string `json:"id"`
				Type      string `json:"type"`
				CallID    string `json:"call_id"`
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			} `json:"item"`
			Response *struct {
				ID     string `json:"id"`
				Status string `json:"status"`
				Usage  *struct {
					InputTokens        int64 `json:"input_tokens"`
					OutputTokens       int64 `json:"output_tokens"`
					InputTokensDetails *struct {
						CachedTokens int64 `json:"cached_tokens"`
					} `json:"input_tokens_details"`
					OutputTokensDetails *struct {
						ReasoningTokens int64 `json:"reasoning_tokens"`
					} `json:"output_tokens_details"`
				} `json:"usage"`
			} `json:"response"`
		}
		if json.Unmarshal([]byte(data), &ev) != nil {
			return nil
		}
		switch ev.Type {
		case "error", "response.failed":
			if ev.Error != nil && ev.Error.Message != "" {
				return fmt.Errorf("%s", ev.Error.Message)
			}
			return fmt.Errorf("upstream response failed")
		case "response.output_text.delta":
			textAcc.WriteString(ev.Delta)
			if req.Callbacks.OnTextDelta != nil {
				req.Callbacks.OnTextDelta(ev.Delta)
			}
		case "response.reasoning_summary_text.delta", "response.reasoning_text.delta":
			thinkAcc.WriteString(ev.Delta)
			if req.Callbacks.OnThinkingDelta != nil {
				req.Callbacks.OnThinkingDelta(ev.Delta)
			}
		case "response.output_item.added":
			if ev.Item != nil && ev.Item.Type == "function_call" {
				key := ev.Item.ID
				if key == "" {
					key = ev.Item.CallID
				}
				acc := &fcAcc{callID: ev.Item.CallID, name: ev.Item.Name}
				if ev.Item.Arguments != "" {
					acc.args.WriteString(ev.Item.Arguments)
				}
				fcs[key] = acc
				fcOrder = append(fcOrder, key)
			}
		case "response.function_call_arguments.delta":
			if acc, ok := fcs[ev.ItemID]; ok {
				acc.args.WriteString(ev.Delta)
			}
		case "response.completed", "response.incomplete":
			if ev.Response != nil {
				if headerGenID == "" && ev.Response.ID != "" {
					genID = ev.Response.ID
				}
				stopReason = ev.Response.Status
				if ev.Response.Usage != nil {
					cached := int64(0)
					if ev.Response.Usage.InputTokensDetails != nil {
						cached = ev.Response.Usage.InputTokensDetails.CachedTokens
					}
					uncached := ev.Response.Usage.InputTokens - cached
					if uncached < 0 {
						uncached = 0
					}
					usage.InputTokens = uncached
					usage.CacheReadTokens = cached
					usage.OutputTokens = ev.Response.Usage.OutputTokens
					if ev.Response.Usage.OutputTokensDetails != nil {
						usage.ReasoningTokens = ev.Response.Usage.OutputTokensDetails.ReasoningTokens
					}
				}
			}
		}
		return nil
	})
	if err != nil && err != io.EOF {
		return GenerateResponse{}, err
	}

	var blocks []types.Block
	if thinkAcc.Len() > 0 {
		blocks = append(blocks, types.Block{Type: types.BlockThinking, Thinking: thinkAcc.String()})
	}
	if textAcc.Len() > 0 {
		blocks = append(blocks, types.Block{Type: types.BlockText, Text: textAcc.String()})
	}
	for _, key := range fcOrder {
		acc := fcs[key]
		rawArgs := json.RawMessage("{}")
		if s := strings.TrimSpace(acc.args.String()); s != "" && json.Valid([]byte(s)) {
			rawArgs = json.RawMessage(s)
		}
		cid := acc.callID
		if cid == "" {
			cid = types.NewID("call")
		}
		blocks = append(blocks, types.Block{
			Type:  types.BlockToolUse,
			ID:    cid,
			Name:  acc.name,
			Input: rawArgs,
		})
	}
	usage.GenerationID = genID
	if req.Provider.Protocol == config.ProtocolDaumini && genID != "" {
		c.reconcileDauminiGeneration(ctx, req.Provider, apiKey, genID, &usage)
	}
	return GenerateResponse{
		Blocks:     blocks,
		StopReason: stopReason,
		Usage:      usage,
		Protocol:   "openai_responses",
	}, nil
}

func (c *Client) streamAnthropic(ctx context.Context, req GenerateRequest, apiKey, modelID string, meta *ModelInfo) (GenerateResponse, error) {
	base := strings.TrimRight(strings.TrimSpace(req.Provider.BaseURL), "/")
	endpoint := strings.TrimSuffix(base, "/v1") + "/v1/messages"
	if req.Provider.Protocol == config.ProtocolDaumini {
		endpoint = base + "/api/v1/messages"
	}

	var msgs []map[string]any
	for i, m := range req.Messages {
		role := "user"
		if m.Role == types.RoleAssistant {
			role = "assistant"
		}
		content := make([]map[string]any, 0, len(m.Blocks))
		for _, b := range m.Blocks {
			switch b.Type {
			case types.BlockText, types.BlockSystemNotice:
				if b.Text != "" {
					content = append(content, map[string]any{"type": "text", "text": b.Text})
				}
			case types.BlockThinking:
				if b.Thinking != "" && b.Signature != "" {
					content = append(content, map[string]any{
						"type":      "thinking",
						"thinking":  b.Thinking,
						"signature": b.Signature,
					})
				}
			case types.BlockToolUse:
				var inObj any = map[string]any{}
				if len(b.Input) > 0 {
					_ = json.Unmarshal(b.Input, &inObj)
				}
				content = append(content, map[string]any{
					"type":  "tool_use",
					"id":    b.ID,
					"name":  b.Name,
					"input": inObj,
				})
			case types.BlockToolResult:
				content = append(content, map[string]any{
					"type":        "tool_result",
					"tool_use_id": b.ToolUseID,
					"content":     b.Text,
					"is_error":    b.IsError,
				})
			}
		}
		if len(content) == 0 {
			continue
		}
		// Place explicit cache breakpoint on the second-to-last user turn to keep rolling prefix warm.
		if role == "user" && i >= len(req.Messages)-2 {
			content[len(content)-1]["cache_control"] = map[string]any{"type": "ephemeral"}
		}
		msgs = append(msgs, map[string]any{"role": role, "content": content})
	}

	payload := map[string]any{
		"model":      modelID,
		"max_tokens": req.MaxTokens,
		"messages":   msgs,
		"stream":     true,
	}
	if effort := strings.TrimSpace(req.ReasoningEffort); effort != "" {
		if effort == "off" || effort == "none" {
			payload["thinking"] = map[string]any{"type": "disabled"}
		} else if meta == nil || meta.HasParam("thinking") {
			budget := 4096
			switch effort {
			case "low":
				budget = 2048
			case "medium":
				budget = 6144
			case "high":
				budget = 12288
			case "max":
				budget = 24576
			}
			if req.MaxTokens <= budget {
				payload["max_tokens"] = budget + 4096
			}
			payload["thinking"] = map[string]any{"type": "enabled", "budget_tokens": budget}
		}
	}
	if strings.TrimSpace(req.SystemPrompt) != "" {
		payload["system"] = []map[string]any{{
			"type":          "text",
			"text":          req.SystemPrompt,
			"cache_control": map[string]any{"type": "ephemeral"},
		}}
	}
	if len(req.Tools) > 0 {
		tList := make([]map[string]any, 0, len(req.Tools))
		for idx, t := range req.Tools {
			var schema any
			_ = json.Unmarshal(t.Parameters, &schema)
			item := map[string]any{
				"name":         t.Name,
				"description":  t.Description,
				"input_schema": schema,
			}
			if idx == len(req.Tools)-1 {
				item["cache_control"] = map[string]any{"type": "ephemeral"}
			}
			tList = append(tList, item)
		}
		payload["tools"] = tList
	}

	body, _ := json.Marshal(payload)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return GenerateResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set("anthropic-version", "2023-06-01")
	if req.Provider.Protocol == config.ProtocolDaumini {
		httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	} else {
		httpReq.Header.Set("x-api-key", apiKey)
	}
	for k, v := range req.Provider.Headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return GenerateResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		return GenerateResponse{}, fmt.Errorf("HTTP %d from %s: %s", resp.StatusCode, endpoint, strings.TrimSpace(string(errBody)))
	}

	type blkAcc struct {
		btype     string
		id        string
		name      string
		text      strings.Builder
		thinking  strings.Builder
		signature strings.Builder
		jsonBuf   strings.Builder
	}
	accs := make(map[int]*blkAcc)
	var blkOrder []int
	var stopReason string
	var usage types.Usage
	headerGenID := strings.TrimSpace(resp.Header.Get("X-Generation-Id"))
	usage.RequestID = strings.TrimSpace(resp.Header.Get("X-Request-Id"))
	genID := headerGenID

	err = readSSE(resp.Body, func(data string) error {
		var ev struct {
			Type  string `json:"type"`
			Index int    `json:"index"`
			Error *struct {
				Type    string `json:"type"`
				Message string `json:"message"`
			} `json:"error"`
			Message *struct {
				ID    string `json:"id"`
				Usage *struct {
					InputTokens              int64 `json:"input_tokens"`
					CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
					CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
					OutputTokens             int64 `json:"output_tokens"`
				} `json:"usage"`
			} `json:"message"`
			ContentBlock *struct {
				Type string `json:"type"`
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"content_block"`
			Delta *struct {
				Type        string `json:"type"`
				Text        string `json:"text"`
				Thinking    string `json:"thinking"`
				Signature   string `json:"signature"`
				PartialJSON string `json:"partial_json"`
				StopReason  string `json:"stop_reason"`
			} `json:"delta"`
			Usage *struct {
				InputTokens              int64 `json:"input_tokens"`
				CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
				CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
				OutputTokens             int64 `json:"output_tokens"`
			} `json:"usage"`
		}
		if json.Unmarshal([]byte(data), &ev) != nil {
			return nil
		}
		switch ev.Type {
		case "error":
			if ev.Error != nil && ev.Error.Message != "" {
				return fmt.Errorf("%s", ev.Error.Message)
			}
			return fmt.Errorf("upstream stream error")
		case "message_start":
			if ev.Message != nil {
				if headerGenID == "" && ev.Message.ID != "" {
					genID = ev.Message.ID
				}
				if ev.Message.Usage != nil {
					usage.InputTokens = ev.Message.Usage.InputTokens
					usage.CacheWriteTokens = ev.Message.Usage.CacheCreationInputTokens
					usage.CacheReadTokens = ev.Message.Usage.CacheReadInputTokens
					usage.OutputTokens = ev.Message.Usage.OutputTokens
				}
			}
		case "content_block_start":
			if ev.ContentBlock != nil {
				accs[ev.Index] = &blkAcc{
					btype: ev.ContentBlock.Type,
					id:    ev.ContentBlock.ID,
					name:  ev.ContentBlock.Name,
				}
				blkOrder = append(blkOrder, ev.Index)
			}
		case "content_block_delta":
			acc := accs[ev.Index]
			if acc == nil || ev.Delta == nil {
				return nil
			}
			switch ev.Delta.Type {
			case "text_delta":
				acc.text.WriteString(ev.Delta.Text)
				if req.Callbacks.OnTextDelta != nil {
					req.Callbacks.OnTextDelta(ev.Delta.Text)
				}
			case "thinking_delta":
				acc.thinking.WriteString(ev.Delta.Thinking)
				if req.Callbacks.OnThinkingDelta != nil {
					req.Callbacks.OnThinkingDelta(ev.Delta.Thinking)
				}
			case "signature_delta":
				acc.signature.WriteString(ev.Delta.Signature)
			case "input_json_delta":
				acc.jsonBuf.WriteString(ev.Delta.PartialJSON)
			}
		case "message_delta":
			if ev.Delta != nil && ev.Delta.StopReason != "" {
				stopReason = ev.Delta.StopReason
			}
			if ev.Usage != nil && ev.Usage.OutputTokens > 0 {
				usage.OutputTokens = ev.Usage.OutputTokens
			}
		}
		return nil
	})
	if err != nil && err != io.EOF {
		return GenerateResponse{}, err
	}

	sort.Ints(blkOrder)
	var blocks []types.Block
	for _, idx := range blkOrder {
		acc := accs[idx]
		switch acc.btype {
		case "thinking":
			blocks = append(blocks, types.Block{
				Type:      types.BlockThinking,
				Thinking:  acc.thinking.String(),
				Signature: acc.signature.String(),
			})
		case "text":
			blocks = append(blocks, types.Block{
				Type: types.BlockText,
				Text: acc.text.String(),
			})
		case "tool_use":
			rawArgs := json.RawMessage("{}")
			if s := strings.TrimSpace(acc.jsonBuf.String()); s != "" && json.Valid([]byte(s)) {
				rawArgs = json.RawMessage(s)
			}
			blocks = append(blocks, types.Block{
				Type:  types.BlockToolUse,
				ID:    acc.id,
				Name:  acc.name,
				Input: rawArgs,
			})
		}
	}
	usage.GenerationID = genID
	if req.Provider.Protocol == config.ProtocolDaumini && genID != "" {
		c.reconcileDauminiGeneration(ctx, req.Provider, apiKey, genID, &usage)
	}
	return GenerateResponse{
		Blocks:     blocks,
		StopReason: stopReason,
		Usage:      usage,
		Protocol:   "anthropic",
	}, nil
}

func (c *Client) reconcileDauminiGeneration(ctx context.Context, p config.ProviderConfig, apiKey, genID string, usage *types.Usage) {
	base := strings.TrimRight(strings.TrimSpace(p.BaseURL), "/")
	u := fmt.Sprintf("%s/api/v1/generation?id=%s", base, url.QueryEscape(genID))
	reqCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, u, nil)
	if err != nil {
		return
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := c.http.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		if resp != nil {
			resp.Body.Close()
		}
		return
	}
	defer resp.Body.Close()
	var parsed struct {
		Data struct {
			ChargedNUSD      int64  `json:"charged_nusd"`
			TotalCostNUSD    int64  `json:"total_cost_nusd"`
			LatencyMS        int64  `json:"latency_ms"`
			PromptTokens     *int64 `json:"prompt_tokens"`
			CompletionTokens *int64 `json:"completion_tokens"`
			ReasoningTokens  *int64 `json:"reasoning_tokens"`
			CacheUsage       *struct {
				CacheReadTokens  int64 `json:"cache_read_tokens"`
				CacheWriteTokens int64 `json:"cache_write_tokens"`
			} `json:"cache_usage"`
		} `json:"data"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 128*1024)).Decode(&parsed) == nil {
		cost := parsed.Data.ChargedNUSD
		if cost == 0 {
			cost = parsed.Data.TotalCostNUSD
		}
		usage.TotalCostNUSD = cost
		if parsed.Data.LatencyMS > 0 {
			usage.LatencyMS = parsed.Data.LatencyMS
		}
		if parsed.Data.CacheUsage != nil {
			usage.CacheReadTokens = parsed.Data.CacheUsage.CacheReadTokens
			usage.CacheWriteTokens = parsed.Data.CacheUsage.CacheWriteTokens
		}
		if parsed.Data.PromptTokens != nil {
			uncached := *parsed.Data.PromptTokens - usage.CacheReadTokens - usage.CacheWriteTokens
			if uncached < 0 {
				uncached = 0
			}
			usage.InputTokens = uncached
		}
		if parsed.Data.CompletionTokens != nil {
			usage.OutputTokens = *parsed.Data.CompletionTokens
		}
		if parsed.Data.ReasoningTokens != nil {
			usage.ReasoningTokens = *parsed.Data.ReasoningTokens
		}
		usage.Reconciled = true
	}
}

func readSSE(r io.Reader, onData func(string) error) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	var lines []string
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			if len(lines) > 0 {
				if err := onData(strings.Join(lines, "\n")); err != nil {
					return err
				}
				lines = nil
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		if strings.HasPrefix(line, "data:") {
			lines = append(lines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	if len(lines) > 0 {
		if err := onData(strings.Join(lines, "\n")); err != nil {
			return err
		}
	}
	return sc.Err()
}
