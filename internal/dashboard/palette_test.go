package dashboard

import "testing"

func TestPaletteRampKnownColor(t *testing.T) {
	r := PaletteRamp(testColor)
	if r.C500 != blue500 {
		t.Errorf("PaletteRamp(blue).C500 = %q, want %q", r.C500, blue500)
	}
	if r.C900 != "#1e3a8a" {
		t.Errorf("PaletteRamp(blue).C900 = %q, want #1e3a8a", r.C900)
	}
	// The 500 shade must equal the accent for the same color, so a card's
	// hover border and its heading accent stay in sync.
	if r.C500 != AccentHex(testColor) {
		t.Errorf("PaletteRamp(blue).C500 = %q, AccentHex(blue) = %q; want equal", r.C500, AccentHex(testColor))
	}
}

func TestPaletteRampFallsBackToSlate(t *testing.T) {
	slate := PaletteRamp(defaultColor)
	for _, color := range []string{"", "white", "not-a-color"} {
		if got := PaletteRamp(color); got != slate {
			t.Errorf("PaletteRamp(%q) = %+v, want slate fallback %+v", color, got, slate)
		}
	}
}
