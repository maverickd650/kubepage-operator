package dashboard

import (
	"fmt"
	"strconv"
	"strings"
)

func gridStyle(columns *int32) string {
	return fmt.Sprintf("grid-template-columns: repeat(%d, 1fr);", *columns)
}

// cardTarget resolves a card's link target: its own override, else the
// site default.
func cardTarget(c Card, siteTarget string) string {
	if c.Target != "" {
		return c.Target
	}
	return siteTarget
}

// statusWithLatency formats a monitor status for display, e.g. "Up · 12ms".
func statusWithLatency(status, latency string) string {
	if latency != "" {
		return status + " · " + latency
	}
	return status
}

// statusPillText prefers latency over the bare status word, matching the
// status-pill markup's previous {{if .Latency}}{{.Latency}}{{else}}{{.Status}}{{end}}.
func statusPillText(c Card) string {
	if c.Latency != "" {
		return c.Latency
	}
	return c.Status
}

// intToStr is a small formatting helper for use inside .templ attribute
// expressions, which can't call strconv.Itoa with a `+` concatenation
// against string literals directly.
func intToStr(n int) string {
	return strconv.Itoa(n)
}

// langOrDefault mirrors the page shell's {{if .Site.Language}}...{{else}}en{{end}}.
func langOrDefault(lang string) string {
	if lang != "" {
		return lang
	}
	return "en"
}

// rootVarsCSS computes the page's dynamic CSS custom properties (palette
// ramp, accent, card blur/opacity) as a single inline-style string set on
// <html>. Every value here is server-computed from a fixed lookup table or
// enum (AccentHex/PaletteRamp/blurPx) or a plain integer percentage, never
// free-form user text, so no CSS-escaping is needed — unlike backgroundStyle
// below, which does interpolate a CRD-supplied URL.
func rootVarsCSS(accentHex string, ramp Ramp, cardBlur string, background *Background) string {
	var b strings.Builder
	fmt.Fprintf(&b, "--accent: %s;", accentHex)
	fmt.Fprintf(&b, "--c50: %s; --c100: %s; --c200: %s; --c300: %s; --c400: %s; --c500: %s; --c600: %s; --c700: %s; --c800: %s; --c900: %s;",
		ramp.C50, ramp.C100, ramp.C200, ramp.C300, ramp.C400, ramp.C500, ramp.C600, ramp.C700, ramp.C800, ramp.C900)
	if cardBlur != "" {
		fmt.Fprintf(&b, "--card-blur: %s;", cardBlur)
	}
	if background != nil && background.Opacity != nil {
		fmt.Fprintf(&b, "--card-opacity: %d%%;", *background.Opacity)
	}
	return b.String()
}

// cssStringEscape escapes a value for safe embedding inside a double-quoted
// CSS string literal (e.g. url("...")). Background.Image is the one CSS
// value in this page that comes from CRD-supplied free text rather than a
// fixed lookup table, so it's the one value that needs this.
func cssStringEscape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}

// backgroundStyle returns a complete "<style>body { ... }</style>" element
// setting the background image, for emission via @templ.Raw as ordinary
// element content (a sibling node, not text typed inside a literal <style>
// tag in the .templ source — templ treats a <style> tag's own text content
// as raw/opaque and won't evaluate an @templ.Raw call written inside it).
// It's a full <style> tag rather than a style="" attribute value because
// the quoted url("...") it needs would otherwise go through templ's
// HTML-attribute escaping twice — once for the quotes templ.SafeCSS itself
// encodes, again when the attribute value as a whole is serialized —
// corrupting the URL. Raw element content only passes through escaping
// once (none, since this is explicitly Raw), so it's the correct place for
// a value containing literal quote characters.
func backgroundStyle(bg *Background) string {
	if bg == nil {
		return ""
	}
	return fmt.Sprintf(`<style>body { background-image: url("%s"); background-size: cover; background-position: center; background-attachment: fixed; }</style>`, cssStringEscape(bg.Image))
}
