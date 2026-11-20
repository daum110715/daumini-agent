package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/daum110715/daumini-agent/internal/types"
)

type Protocol string

const (
	ProtocolOpenAIChat      Protocol = "openai_chat"
	ProtocolOpenAIResponses Protocol = "openai_responses"
	ProtocolAnthropic       Protocol = "anthropic"
	ProtocolDeepSeek        Protocol = "deepseek"
	ProtocolDaumini         Protocol = "daumini"
)

type ProviderConfig struct {
	ID             string            `json:"id"`
	Name           string            `json:"name"`
	Protocol       Protocol          `json:"protocol"`
	BaseURL        string            `json:"base_url"`
	APIKey         string            `json:"api_key,omitempty"`
	APIKeyEnv      string            `json:"api_key_env,omitempty"`
	DefaultModel   string            `json:"default_model,omitempty"`
	Models         []string          `json:"models,omitempty"`
	Headers        map[string]string `json:"headers,omitempty"`
	Enabled        bool              `json:"enabled"`
	UserEmail      string            `json:"user_email,omitempty"`
}

func (p ProviderConfig) ResolveAPIKey() string {
	if strings.TrimSpace(p.APIKey) != "" {
		return strings.TrimSpace(p.APIKey)
	}
	if strings.TrimSpace(p.APIKeyEnv) != "" {
		return strings.TrimSpace(os.Getenv(strings.TrimSpace(p.APIKeyEnv)))
	}
	return ""
}

type MCPTransport string

const (
	MCPStdio          MCPTransport = "stdio"
	MCPStreamableHTTP MCPTransport = "http"
)

type MCPServerConfig struct {
	Name      string            `json:"name"`
	Transport MCPTransport      `json:"transport"`
	Command   string            `json:"command,omitempty"`
	Args      []string          `json:"args,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	URL       string            `json:"url,omitempty"`
	Headers   map[string]string `json:"headers,omitempty"`
	Enabled   bool              `json:"enabled"`
	TimeoutS  int               `json:"timeout_s,omitempty"`
}

type HookConfig struct {
	Name      string   `json:"name"`
	Event     string   `json:"event"` // pre_tool, post_tool, pre_run, post_run, stop_check
	MatchTool string   `json:"match_tool,omitempty"`
	Command   string   `json:"command"`
	Args      []string `json:"args,omitempty"`
	TimeoutS  int      `json:"timeout_s,omitempty"`
	Enabled   bool     `json:"enabled"`
}

type PluginExecConfig struct {
	Name    string   `json:"name"`
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
	Enabled bool     `json:"enabled"`
}

type Config struct {
	Version          int                `json:"version"`
	DefaultProvider  string             `json:"default_provider"`
	DefaultModel     string             `json:"default_model"`
	ApprovalMode     types.ApprovalMode `json:"approval_mode"`
	MaxTurns         int                `json:"max_turns"`
	Theme            string             `json:"theme"`    // auto | light | dark
	UIStyle          string             `json:"ui_style"` // atelier | sketch
	WebAddr          string             `json:"web_addr"`
	RecentWorkspaces []string           `json:"recent_workspaces,omitempty"`
	Providers        []ProviderConfig   `json:"providers"`
	MCPServers       []MCPServerConfig  `json:"mcp_servers,omitempty"`
	Hooks            []HookConfig       `json:"hooks,omitempty"`
	Plugins          []PluginExecConfig `json:"plugins,omitempty"`
	SystemExtra      string             `json:"system_extra,omitempty"`
}

func DefaultDataDir() string {
	if v := strings.TrimSpace(os.Getenv("DAUMINI_AGENT_HOME")); v != "" {
		return v
	}
	if base, err := os.UserConfigDir(); err == nil && base != "" {
		return filepath.Join(base, "daumini-agent")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "daumini-agent")
}

func DefaultConfig() Config {
	return Config{
		Version:         1,
		DefaultProvider: "daumini",
		DefaultModel:    "deepseek/deepseek-v4.1flash",
		ApprovalMode:    types.ApprovalAutoRead,
		MaxTurns:        40,
		Theme:           "auto",
		UIStyle:         "atelier",
		WebAddr:         "127.0.0.1:3847",
		Providers: []ProviderConfig{
			{
				ID:           "daumini",
				Name:         "daumini 中转",
				Protocol:     ProtocolDaumini,
				BaseURL:      "https://mini.daum.pw",
				APIKeyEnv:    "DAUMINI_API_KEY",
				DefaultModel: "deepseek/deepseek-v4.1flash",
				Models:       []string{"deepseek/deepseek-v4.1flash"},
				Enabled:      true,
			},
			{
				ID:           "deepseek",
				Name:         "DeepSeek",
				Protocol:     ProtocolDeepSeek,
				BaseURL:      "https://api.deepseek.com",
				APIKeyEnv:    "DEEPSEEK_API_KEY",
				DefaultModel: "deepseek-chat",
				Models:       []string{"deepseek-chat", "deepseek-reasoner"},
				Enabled:      true,
			},
			{
				ID:           "openai",
				Name:         "OpenAI",
				Protocol:     ProtocolOpenAIResponses,
				BaseURL:      "https://api.openai.com/v1",
				APIKeyEnv:    "OPENAI_API_KEY",
				DefaultModel: "gpt-5.2",
				Models:       []string{"gpt-5.2", "gpt-5-mini", "o4-mini"},
				Enabled:      true,
			},
			{
				ID:           "anthropic",
				Name:         "Anthropic",
				Protocol:     ProtocolAnthropic,
				BaseURL:      "https://api.anthropic.com",
				APIKeyEnv:    "ANTHROPIC_API_KEY",
				DefaultModel: "claude-sonnet-4-6",
				Models:       []string{"claude-sonnet-4-6", "claude-opus-4-6", "claude-haiku-4-5"},
				Enabled:      true,
			},
		},
	}
}

type Manager struct {
	mu      sync.RWMutex
	dataDir string
	cfg     Config
}

func NewManager(dataDir string) (*Manager, error) {
	if strings.TrimSpace(dataDir) == "" {
		dataDir = DefaultDataDir()
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, err
	}
	m := &Manager{dataDir: dataDir}
	if err := m.Load(); err != nil {
		return nil, err
	}
	return m, nil
}

func (m *Manager) DataDir() string { return m.dataDir }

func (m *Manager) ConfigPath() string {
	return filepath.Join(m.dataDir, "config.json")
}

func (m *Manager) Load() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	raw, err := os.ReadFile(m.ConfigPath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			cfg := DefaultConfig()
			m.cfg = cfg
			return writeAtomicJSON(m.ConfigPath(), cfg, 0o600)
		}
		return err
	}
	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return err
	}
	migrated := false
	def := DefaultConfig()
	if cfg.Version == 0 {
		cfg.Version = 1
		migrated = true
	}
	if len(cfg.Providers) == 0 {
		cfg.Providers = def.Providers
		var legacy struct {
			GatewayURL string `json:"gateway_url"`
			APIKey     string `json:"api_key"`
			UserEmail  string `json:"user_email"`
		}
		if json.Unmarshal(raw, &legacy) == nil && legacy.GatewayURL != "" {
			for i := range cfg.Providers {
				if cfg.Providers[i].ID == "daumini" {
					cfg.Providers[i].BaseURL = legacy.GatewayURL
					if legacy.APIKey != "" {
						cfg.Providers[i].APIKey = legacy.APIKey
					}
					if legacy.UserEmail != "" {
						cfg.Providers[i].UserEmail = legacy.UserEmail
					}
					break
				}
			}
		}
		migrated = true
	}
	if cfg.DefaultProvider == "" {
		cfg.DefaultProvider = def.DefaultProvider
		migrated = true
	}
	if cfg.DefaultModel == "" {
		cfg.DefaultModel = def.DefaultModel
		migrated = true
	}
	if cfg.MaxTurns <= 0 {
		cfg.MaxTurns = 40
	}
	if cfg.ApprovalMode == "" {
		cfg.ApprovalMode = types.ApprovalAutoRead
	}
	if cfg.Theme == "" {
		cfg.Theme = "auto"
	}
	if cfg.UIStyle == "" {
		cfg.UIStyle = "atelier"
	}
	if cfg.WebAddr == "" {
		cfg.WebAddr = "127.0.0.1:3847"
	}
	m.cfg = cfg
	if migrated {
		_ = writeAtomicJSON(m.ConfigPath(), cfg, 0o600)
	}
	return nil
}

func (m *Manager) Get() Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return cloneConfig(m.cfg)
}

func (m *Manager) Update(fn func(*Config) error) (Config, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	next := cloneConfig(m.cfg)
	if err := fn(&next); err != nil {
		return Config{}, err
	}
	if err := writeAtomicJSON(m.ConfigPath(), next, 0o600); err != nil {
		return Config{}, err
	}
	m.cfg = next
	return cloneConfig(next), nil
}

func (m *Manager) AddRecentWorkspace(dir string) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return
	}
	abs, err := filepath.Abs(dir)
	if err == nil {
		dir = abs
	}
	_, _ = m.Update(func(c *Config) error {
		next := make([]string, 0, len(c.RecentWorkspaces)+1)
		next = append(next, dir)
		for _, w := range c.RecentWorkspaces {
			if w == "" || strings.EqualFold(w, dir) {
				continue
			}
			next = append(next, w)
			if len(next) >= 12 {
				break
			}
		}
		c.RecentWorkspaces = next
		return nil
	})
}

func (m *Manager) FindProvider(id string) (ProviderConfig, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, p := range m.cfg.Providers {
		if p.ID == id {
			return p, true
		}
	}
	return ProviderConfig{}, false
}

// PickActiveProvider returns the explicitly requested provider or the first provider that actually has a key.
func (m *Manager) PickActiveProvider(preferredID string) (ProviderConfig, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if preferredID != "" {
		for _, p := range m.cfg.Providers {
			if p.ID == preferredID && p.Enabled {
				return p, true
			}
		}
	}
	for _, p := range m.cfg.Providers {
		if p.ID == m.cfg.DefaultProvider && p.Enabled && p.ResolveAPIKey() != "" {
			return p, true
		}
	}
	for _, p := range m.cfg.Providers {
		if p.Enabled && p.ResolveAPIKey() != "" {
			return p, true
		}
	}
	for _, p := range m.cfg.Providers {
		if p.ID == m.cfg.DefaultProvider && p.Enabled {
			return p, true
		}
	}
	if len(m.cfg.Providers) > 0 {
		return m.cfg.Providers[0], true
	}
	return ProviderConfig{}, false
}

func RedactedConfig(cfg Config) Config {
	out := cloneConfig(cfg)
	for i := range out.Providers {
		k := out.Providers[i].ResolveAPIKey()
		if k != "" {
			out.Providers[i].APIKey = maskSecret(k)
		}
	}
	return out
}

func maskSecret(s string) string {
	if len(s) <= 8 {
		return "****"
	}
	return s[:4] + "..." + s[len(s)-4:]
}

func cloneConfig(c Config) Config {
	raw, _ := json.Marshal(c)
	var out Config
	_ = json.Unmarshal(raw, &out)
	return out
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
