package sandbox

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Sandbox enforces strict path containment inside the workspace root, including symlink resolution checks.
type Sandbox struct {
	rootAbs  string
	rootReal string
}

func New(workspaceDir string) (*Sandbox, error) {
	abs, err := filepath.Abs(workspaceDir)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("workspace path %q is not a directory", abs)
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		real = abs
	}
	return &Sandbox{rootAbs: abs, rootReal: real}, nil
}

func (s *Sandbox) Root() string { return s.rootAbs }

// Resolve validates that a relative or absolute path stays inside the workspace root.
func (s *Sandbox) Resolve(rawPath string) (string, string, error) {
	p := strings.TrimSpace(rawPath)
	if p == "" || p == "." {
		return s.rootAbs, ".", nil
	}
	candidate := p
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(s.rootAbs, candidate)
	}
	candidate = filepath.Clean(candidate)

	rel, err := filepath.Rel(s.rootAbs, candidate)
	if err != nil || isOutsideRel(rel) {
		return "", "", fmt.Errorf("path escapes workspace root: %s", rawPath)
	}

	resolved := resolveNearestReal(candidate)
	realRel, err := filepath.Rel(s.rootReal, resolved)
	if err != nil || isOutsideRel(realRel) {
		return "", "", fmt.Errorf("symlink target escapes workspace root: %s", rawPath)
	}

	return candidate, filepath.ToSlash(rel), nil
}

func (s *Sandbox) List(relDir string, maxEntries int) ([]string, error) {
	if maxEntries <= 0 {
		maxEntries = 200
	}
	absDir, _, err := s.Resolve(relDir)
	if err != nil {
		return nil, err
	}
	var out []string
	err = filepath.WalkDir(absDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		name := d.Name()
		if d.IsDir() && shouldSkipDir(name) && path != absDir {
			return filepath.SkipDir
		}
		if path == absDir {
			return nil
		}
		rel, err := filepath.Rel(s.rootAbs, path)
		if err != nil || isOutsideRel(rel) {
			return nil
		}
		item := filepath.ToSlash(rel)
		if d.IsDir() {
			item += "/"
		}
		out = append(out, item)
		if len(out) >= maxEntries {
			return fs.SkipAll
		}
		return nil
	})
	sort.Strings(out)
	return out, err
}

func resolveNearestReal(candidate string) string {
	cur := candidate
	var tail []string
	for {
		real, err := filepath.EvalSymlinks(cur)
		if err == nil {
			for i := len(tail) - 1; i >= 0; i-- {
				real = filepath.Join(real, tail[i])
			}
			return real
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return candidate
		}
		tail = append(tail, filepath.Base(cur))
		cur = parent
	}
}

func isOutsideRel(rel string) bool {
	return rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func shouldSkipDir(name string) bool {
	switch name {
	case ".git", "node_modules", ".svelte-kit", "dist", "build", ".agent-logs", "__pycache__", ".venv", "target":
		return true
	default:
		return false
	}
}

func SanitizedEnv() []string {
	deny := map[string]bool{
		"DAUMINI_API_KEY":    true,
		"OPENAI_API_KEY":     true,
		"ANTHROPIC_API_KEY":  true,
		"DEEPSEEK_API_KEY":   true,
		"CF_API_TOKEN":       true,
		"CLOUDFLARE_API_TOKEN": true,
		"GITHUB_TOKEN":       true,
		"GH_TOKEN":           true,
	}
	var env []string
	for _, kv := range os.Environ() {
		k, _, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		if deny[strings.ToUpper(k)] {
			continue
		}
		env = append(env, kv)
	}
	env = append(env,
		"CI=1",
		"GIT_TERMINAL_PROMPT=0",
		"PAGER=cat",
		"TERM=dumb",
	)
	return env
}
