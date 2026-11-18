package cli

import (
	"os"
	"strconv"
	"strings"
)

type ThemeMode string

const (
	ThemeAuto  ThemeMode = "auto"
	ThemeLight ThemeMode = "light"
	ThemeDark  ThemeMode = "dark"
)

// Palette keeps body text on the terminal's native default foreground and uses high-contrast 256-color accents for light & dark terminals.
type Palette struct {
	Mode      ThemeMode
	IsLight   bool
	Color     bool
	Reset     string
	Bold      string
	Dim       string
	Italic    string
	Accent    string
	Success   string
	Warn      string
	Error     string
	Muted     string
	ToolTag   string
	PanelLine string
}

func DetectPalette(pref string) Palette {
	enableVirtualTerminal()

	if os.Getenv("NO_COLOR") != "" || strings.EqualFold(os.Getenv("TERM"), "dumb") || !isTerminal(os.Stdout) {
		return Palette{Mode: ThemeLight, Color: false}
	}

	var isLight bool
	switch strings.ToLower(strings.TrimSpace(pref)) {
	case "light":
		isLight = true
	case "dark":
		isLight = false
	default:
		if env := strings.ToLower(strings.TrimSpace(os.Getenv("DAUMINI_AGENT_THEME"))); env == "light" || env == "dark" {
			isLight = env == "light"
		} else {
			isLight = detectLightTerminal()
		}
	}

	if isLight {
		return Palette{
			Mode:      ThemeLight,
			IsLight:   true,
			Color:     true,
			Reset:     "\x1b[0m",
			Bold:      "\x1b[1m",
			Dim:       "\x1b[38;5;242m",
			Italic:    "\x1b[3m\x1b[38;5;242m",
			Accent:    "\x1b[1m\x1b[38;5;25m",  // deep cobalt blue on white/cream
			Success:   "\x1b[38;5;28m",         // deep forest green
			Warn:      "\x1b[1m\x1b[38;5;130m", // deep amber-brown (readable on light bg)
			Error:     "\x1b[1m\x1b[38;5;160m", // crimson
			Muted:     "\x1b[38;5;242m",        // charcoal gray
			ToolTag:   "\x1b[48;5;254m\x1b[38;5;236m",
			PanelLine: "\x1b[38;5;250m",
		}
	}
	return Palette{
		Mode:      ThemeDark,
		IsLight:   false,
		Color:     true,
		Reset:     "\x1b[0m",
		Bold:      "\x1b[1m",
		Dim:       "\x1b[38;5;245m",
		Italic:    "\x1b[3m\x1b[38;5;245m",
		Accent:    "\x1b[1m\x1b[38;5;75m",  // sky blue
		Success:   "\x1b[38;5;78m",         // mint green
		Warn:      "\x1b[1m\x1b[38;5;214m", // amber
		Error:     "\x1b[1m\x1b[38;5;203m", // coral red
		Muted:     "\x1b[38;5;245m",
		ToolTag:   "\x1b[48;5;236m\x1b[38;5;252m",
		PanelLine: "\x1b[38;5;239m",
	}
}

func (p Palette) Paint(style, s string) string {
	if !p.Color || style == "" {
		return s
	}
	return style + s + p.Reset
}

func detectLightTerminal() bool {
	if resp, ok := queryOSC11Background(); ok {
		if luma, parsed := parseOSC11Luma(resp); parsed {
			return luma > 0.5
		}
	}
	if cfgbg := strings.TrimSpace(os.Getenv("COLORFGBG")); cfgbg != "" {
		parts := strings.Split(cfgbg, ";")
		last := parts[len(parts)-1]
		if bg, err := strconv.Atoi(last); err == nil {
			// Standard ANSI background palette: 7 (white/light gray) or 9..15 except 8 (dark gray).
			return bg == 7 || (bg >= 9 && bg <= 15)
		}
	}
	return false
}

func parseOSC11Luma(resp string) (float64, bool) {
	idx := strings.Index(resp, "rgb:")
	if idx < 0 {
		return 0, false
	}
	rest := resp[idx+4:]
	end := strings.IndexAny(rest, "\x07\x1b")
	if end > 0 {
		rest = rest[:end]
	}
	parts := strings.Split(rest, "/")
	if len(parts) != 3 {
		return 0, false
	}
	r, ok1 := parseHexNorm(parts[0])
	g, ok2 := parseHexNorm(parts[1])
	b, ok3 := parseHexNorm(parts[2])
	if !ok1 || !ok2 || !ok3 {
		return 0, false
	}
	return 0.2126*r + 0.7152*g + 0.0722*b, true
}

func parseHexNorm(h string) (float64, bool) {
	if h == "" || len(h) > 4 {
		return 0, false
	}
	v, err := strconv.ParseUint(h, 16, 32)
	if err != nil {
		return 0, false
	}
	maxVal := float64((uint64(1) << (4 * len(h))) - 1)
	if maxVal <= 0 {
		return 0, false
	}
	return float64(v) / maxVal, true
}
