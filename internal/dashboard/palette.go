package dashboard

// Ramp is a Tailwind color's 50–900 shades. The chosen Style.Color
// tints the *whole* UI from this ramp (bg/panel/text/muted/border) the way
// homepage does, rather than only colouring an accent. The template wires
// these into CSS variables and the data-theme block selects which steps map
// to background/foreground (see index.html.tmpl).
type Ramp struct {
	C50, C100, C200, C300, C400, C500, C600, C700, C800, C900 string
}

// blue500 is Tailwind's blue-500 shade, shared between the blue ramp's mid
// step and the blue accent so the two stay in lockstep (the palette test
// asserts a ramp's 500 step equals its accent).
const blue500 = "#3b82f6"

// colorRamps maps a Style.Color value (homepage's documented palette
// enum) to that Tailwind color's full 50–900 ramp. Lifted from the Tailwind v3
// palette (D11: "close enough" fidelity for the no-build native renderer).
// "white" reuses the slate ramp for structural theming; its accent stays the
// lighter slate-400 shade defined in accentPalette.
var colorRamps = map[string]Ramp{
	defaultColor: {"#f8fafc", "#f1f5f9", "#e2e8f0", "#cbd5e1", "#94a3b8", "#64748b", "#475569", "#334155", "#1e293b", "#0f172a"},
	"gray":       {"#f9fafb", "#f3f4f6", "#e5e7eb", "#d1d5db", "#9ca3af", "#6b7280", "#4b5563", "#374151", "#1f2937", "#111827"},
	"zinc":       {"#fafafa", "#f4f4f5", "#e4e4e7", "#d4d4d8", "#a1a1aa", "#71717a", "#52525b", "#3f3f46", "#27272a", "#18181b"},
	"neutral":    {"#fafafa", "#f5f5f5", "#e5e5e5", "#d4d4d4", "#a3a3a3", "#737373", "#525252", "#404040", "#262626", "#171717"},
	"stone":      {"#fafaf9", "#f5f5f4", "#e7e5e4", "#d6d3d1", "#a8a29e", "#78716c", "#57534e", "#44403c", "#292524", "#1c1917"},
	"red":        {"#fef2f2", "#fee2e2", "#fecaca", "#fca5a5", "#f87171", "#ef4444", "#dc2626", "#b91c1c", "#991b1b", "#7f1d1d"},
	"orange":     {"#fff7ed", "#ffedd5", "#fed7aa", "#fdba74", "#fb923c", "#f97316", "#ea580c", "#c2410c", "#9a3412", "#7c2d12"},
	"amber":      {"#fffbeb", "#fef3c7", "#fde68a", "#fcd34d", "#fbbf24", "#f59e0b", "#d97706", "#b45309", "#92400e", "#78350f"},
	"yellow":     {"#fefce8", "#fef9c3", "#fef08a", "#fde047", "#facc15", "#eab308", "#ca8a04", "#a16207", "#854d0e", "#713f12"},
	"lime":       {"#f7fee7", "#ecfccb", "#d9f99d", "#bef264", "#a3e635", "#84cc16", "#65a30d", "#4d7c0f", "#3f6212", "#365314"},
	"green":      {"#f0fdf4", "#dcfce7", "#bbf7d0", "#86efac", "#4ade80", "#22c55e", "#16a34a", "#15803d", "#166534", "#14532d"},
	"emerald":    {"#ecfdf5", "#d1fae5", "#a7f3d0", "#6ee7b7", "#34d399", "#10b981", "#059669", "#047857", "#065f46", "#064e3b"},
	"teal":       {"#f0fdfa", "#ccfbf1", "#99f6e4", "#5eead4", "#2dd4bf", "#14b8a6", "#0d9488", "#0f766e", "#115e59", "#134e4a"},
	"cyan":       {"#ecfeff", "#cffafe", "#a5f3fc", "#67e8f9", "#22d3ee", "#06b6d4", "#0891b2", "#0e7490", "#155e75", "#164e63"},
	"sky":        {"#f0f9ff", "#e0f2fe", "#bae6fd", "#7dd3fc", "#38bdf8", "#0ea5e9", "#0284c7", "#0369a1", "#075985", "#0c4a6e"},
	"blue":       {"#eff6ff", "#dbeafe", "#bfdbfe", "#93c5fd", "#60a5fa", blue500, "#2563eb", "#1d4ed8", "#1e40af", "#1e3a8a"},
	"indigo":     {"#eef2ff", "#e0e7ff", "#c7d2fe", "#a5b4fc", "#818cf8", "#6366f1", "#4f46e5", "#4338ca", "#3730a3", "#312e81"},
	"violet":     {"#f5f3ff", "#ede9fe", "#ddd6fe", "#c4b5fd", "#a78bfa", "#8b5cf6", "#7c3aed", "#6d28d9", "#5b21b6", "#4c1d95"},
	"purple":     {"#faf5ff", "#f3e8ff", "#e9d5ff", "#d8b4fe", "#c084fc", "#a855f7", "#9333ea", "#7e22ce", "#6b21a8", "#581c87"},
	"fuchsia":    {"#fdf4ff", "#fae8ff", "#f5d0fe", "#f0abfc", "#e879f9", "#d946ef", "#c026d3", "#a21caf", "#86198f", "#701a75"},
	"pink":       {"#fdf2f8", "#fce7f3", "#fbcfe8", "#f9a8d4", "#f472b6", "#ec4899", "#db2777", "#be185d", "#9d174d", "#831843"},
	"rose":       {"#fff1f2", "#ffe4e6", "#fecdd3", "#fda4af", "#fb7185", "#f43f5e", "#e11d48", "#be123c", "#9f1239", "#881337"},
}

// accentPalette maps a Style.Color value to that Tailwind color's
// 500-shade hex, used as the dashboard's --accent CSS variable.
var accentPalette = map[string]string{
	defaultColor: "#64748b",
	"gray":       "#6b7280",
	"zinc":       "#71717a",
	"neutral":    "#737373",
	"stone":      "#78716c",
	"red":        "#ef4444",
	"orange":     "#f97316",
	"amber":      "#f59e0b",
	"yellow":     "#eab308",
	"lime":       "#84cc16",
	"green":      "#22c55e",
	"emerald":    "#10b981",
	"teal":       "#14b8a6",
	"cyan":       "#06b6d4",
	"sky":        "#0ea5e9",
	"blue":       blue500,
	"indigo":     "#6366f1",
	"violet":     "#8b5cf6",
	"purple":     "#a855f7",
	"fuchsia":    "#d946ef",
	"pink":       "#ec4899",
	"rose":       "#f43f5e",
	"white":      "#94a3b8",
}

const defaultAccentHex = "#64748b"

// AccentHex returns the hex color for a Style.Color value, falling
// back to the slate default for an empty or unrecognized value (e.g. a
// color added to homepage's enum after this palette was written).
func AccentHex(color string) string {
	if hex, ok := accentPalette[color]; ok {
		return hex
	}
	return defaultAccentHex
}

// PaletteRamp returns the full 50–900 ramp for a Style.Color value,
// falling back to the slate default for an empty or unrecognized value (and
// for "white", which has no chromatic ramp of its own).
func PaletteRamp(color string) Ramp {
	if r, ok := colorRamps[color]; ok {
		return r
	}
	return colorRamps[defaultColor]
}

// switcherConfig is the page shell's small JSON data island (index.templ)
// telling the client-side theme/color switcher script which axes it's
// allowed to override — homepage's "fixed theme/palette disables the
// switcher" behavior, applied per-axis.
type switcherConfig struct {
	ThemeFixed bool `json:"themeFixed"`
	ColorFixed bool `json:"colorFixed"`
}

// AllPalettes returns every color accentPalette has an entry for — the same
// set StyleSpec.Color's kubebuilder enum accepts, plus "orange"
// (defined here but not part of that enum; a harmless switcher-only bonus
// since it's never written back to a Dashboard) — keyed by name, with
// its ramp + accent, JSON-shaped for the client-side color switcher
// (index.templ): rather than reloading the page to pick a different
// palette, the switcher ships every palette to the browser once and flips
// CSS custom properties locally. Iterating accentPalette's own keys (rather
// than a second hardcoded name list) avoids restating each color name as a
// literal a third time.
func AllPalettes() map[string]map[string]string {
	out := make(map[string]map[string]string, len(accentPalette))
	for name := range accentPalette {
		r := PaletteRamp(name)
		out[name] = map[string]string{
			"c50": r.C50, "c100": r.C100, "c200": r.C200, "c300": r.C300, "c400": r.C400,
			"c500": r.C500, "c600": r.C600, "c700": r.C700, "c800": r.C800, "c900": r.C900,
			"accent": AccentHex(name),
		}
	}
	return out
}
