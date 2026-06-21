package dashboard

// accentPalette maps a Configuration.Color value (homepage's documented
// palette enum) to that Tailwind color's 500-shade hex, used as the
// dashboard's --accent CSS variable. Lifted from homepage's palette (D11:
// "close enough" fidelity, not a pixel-exact port) rather than the whole
// Tailwind build, since this is the no-build native renderer.
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
	"blue":       "#3b82f6",
	"indigo":     "#6366f1",
	"violet":     "#8b5cf6",
	"purple":     "#a855f7",
	"fuchsia":    "#d946ef",
	"pink":       "#ec4899",
	"rose":       "#f43f5e",
	"white":      "#94a3b8",
}

const defaultAccentHex = "#64748b"

// AccentHex returns the hex color for a Configuration.Color value, falling
// back to the slate default for an empty or unrecognized value (e.g. a
// color added to homepage's enum after this palette was written).
func AccentHex(color string) string {
	if hex, ok := accentPalette[color]; ok {
		return hex
	}
	return defaultAccentHex
}
