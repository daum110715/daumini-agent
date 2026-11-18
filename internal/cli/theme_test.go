package cli

import "testing"

func TestParseOSC11Luma(t *testing.T) {
	lightResp := "\x1b]11;rgb:ffff/fafa/f0f0\x07"
	luma, ok := parseOSC11Luma(lightResp)
	if !ok || luma <= 0.8 {
		t.Fatalf("expected high luminance for white/ivory terminal, got luma=%v ok=%v", luma, ok)
	}

	darkResp := "\x1b]11;rgb:1111/1313/1a1a\x1b\\"
	lumaDark, okDark := parseOSC11Luma(darkResp)
	if !okDark || lumaDark >= 0.2 {
		t.Fatalf("expected low luminance for dark terminal, got luma=%v ok=%v", lumaDark, okDark)
	}
}
