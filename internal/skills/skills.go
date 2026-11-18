package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/daum110715/daumini-agent/internal/tools"
)

type Skill struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Path        string `json:"path"`
	Scope       string `json:"scope"` // workspace | user
}

type Manager struct {
	dataDir string
}

func NewManager(dataDir string) *Manager {
	return &Manager{dataDir: dataDir}
}

func (m *Manager) Discover(workspaceDir string) []Skill {
	seen := make(map[string]Skill)
	roots := []struct {
		dir   string
		scope string
	}{
		{filepath.Join(m.dataDir, "skills"), "user"},
		{filepath.Join(workspaceDir, ".agents", "skills"), "workspace"},
		{filepath.Join(workspaceDir, ".daumini-agent", "skills"), "workspace"},
	}
	for _, r := range roots {
		entries, err := os.ReadDir(r.dir)
		if err != nil {
			continue
		}
		for _, ent := range entries {
			if !ent.IsDir() {
				continue
			}
			p := filepath.Join(r.dir, ent.Name(), "SKILL.md")
			raw, err := os.ReadFile(p)
			if err != nil {
				continue
			}
			name, desc := parseFrontmatter(string(raw), ent.Name())
			seen[name] = Skill{
				Name:        name,
				Description: desc,
				Path:        p,
				Scope:       r.scope,
			}
		}
	}
	out := make([]Skill, 0, len(seen))
	for _, s := range seen {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (m *Manager) LoadBody(workspaceDir, name string) (Skill, string, error) {
	for _, s := range m.Discover(workspaceDir) {
		if s.Name == name {
			raw, err := os.ReadFile(s.Path)
			if err != nil {
				return Skill{}, "", err
			}
			return s, string(raw), nil
		}
	}
	return Skill{}, "", fmt.Errorf("skill %q not found", name)
}

func FormatIndex(skills []Skill) string {
	if len(skills) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("Available skills (call skill_read with name when relevant):\n")
	for _, s := range skills {
		if s.Description != "" {
			fmt.Fprintf(&sb, "- %s: %s\n", s.Name, s.Description)
		} else {
			fmt.Fprintf(&sb, "- %s\n", s.Name)
		}
	}
	return strings.TrimSpace(sb.String())
}

func parseFrontmatter(src, fallbackName string) (string, string) {
	norm := strings.ReplaceAll(src, "\r\n", "\n")
	name := fallbackName
	var desc string
	if !strings.HasPrefix(norm, "---\n") {
		return name, desc
	}
	end := strings.Index(norm[4:], "\n---")
	if end < 0 {
		return name, desc
	}
	fm := norm[4 : 4+end]
	for _, line := range strings.Split(fm, "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.Trim(strings.TrimSpace(v), `"'`)
		switch k {
		case "name":
			if v != "" {
				name = v
			}
		case "description":
			desc = v
		}
	}
	return name, desc
}

type SkillReadTool struct {
	mgr *Manager
}

func NewReadTool(mgr *Manager) tools.Tool {
	return &SkillReadTool{mgr: mgr}
}

func (t *SkillReadTool) IsMutating() bool { return false }
func (t *SkillReadTool) Spec() tools.ToolSpec {
	return tools.ToolSpec{
		Name:        "skill_read",
		Description: "Load the full SKILL.md instructions for a named skill discovered in the workspace or user config.",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"name":{"type":"string","description":"Skill name from available skills list"}},"required":["name"]}`),
	}
}

func (t *SkillReadTool) Execute(_ context.Context, ec *tools.ExecContext, args json.RawMessage) tools.ToolResult {
	var in struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return tools.ToolResult{Content: "invalid json: " + err.Error(), IsError: true}
	}
	s, body, err := t.mgr.LoadBody(ec.Sandbox.Root(), strings.TrimSpace(in.Name))
	if err != nil {
		return tools.ToolResult{Content: err.Error(), IsError: true}
	}
	return tools.ToolResult{
		Content: fmt.Sprintf("[Skill %s loaded from %s]\n%s", s.Name, s.Path, body),
	}
}
