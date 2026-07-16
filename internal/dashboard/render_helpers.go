package dashboard

import (
	"cmp"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// percentBarStyle returns the inline width style for a usage bar's fill,
// clamping p to [0, 100] since a widget-computed percentage could round
// slightly outside that range.
func percentBarStyle(p int) string {
	switch {
	case p < 0:
		p = 0
	case p > 100:
		p = 100
	}
	return fmt.Sprintf("width: %d%%;", p)
}

func gridStyle(columns *int32) string {
	return fmt.Sprintf("grid-template-columns: repeat(%d, 1fr);", *columns)
}

// isHTTPURL reports whether s has an http(s) scheme. Used to defensively
// re-check DashboardStyle.Spec.Search.URL before it's passed into a
// client-side window.open()/href — see the call site in site.go.
func isHTTPURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

// cardTarget resolves a card's link target: its own override, else the
// site default.
func cardTarget(c Card, siteTarget string) string {
	if c.Target != "" {
		return c.Target
	}
	return siteTarget
}

// bookmarkTarget resolves a bookmark's link target: its own override, else
// the site default. Mirrors cardTarget for BookmarkCard.
func bookmarkTarget(b BookmarkCard, siteTarget string) string {
	if b.Target != "" {
		return b.Target
	}
	return siteTarget
}

// targetSelf and targetTop are HTML link targets that stay in the current
// browsing context (targetSelf) or navigate the topmost one (targetTop),
// pulled out as constants since each is referenced from both
// isNewTabTarget and its tests.
const (
	targetSelf = "_self"
	targetTop  = "_top"
)

// isNewTabTarget reports whether target opens a new browsing context
// ("_blank" or a named target other than "_self"/"_parent"/"_top"), in which
// case the link should carry rel="noopener noreferrer": without it, the
// opened page's window.opener can navigate this dashboard tab to an
// arbitrary URL (reverse tabnabbing), and it also leaks the dashboard's own
// URL to every linked service via the Referer header.
func isNewTabTarget(target string) bool {
	switch target {
	case "", targetSelf, "_parent", targetTop:
		return false
	default:
		return true
	}
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

// statusWithReadyText formats a pod monitor status for display, e.g.
// "Partial (2/3 ready)" — mirrors statusWithLatency's " · " join with
// parens instead, so the pod monitor's ready-count detail reads distinctly
// from the HTTP monitor's latency in a combined tooltip/basic-text line.
func statusWithReadyText(status, readyText string) string {
	if readyText != "" {
		return status + " (" + readyText + ")"
	}
	return status
}

// statusLine renders c's "basic" style status field: both monitors when
// both are configured (e.g. "Up (12ms) · 2/3 ready"), else whichever one is.
func statusLine(c Card) string {
	var parts []string
	if c.Status != "" {
		if c.Latency != "" {
			parts = append(parts, c.Status+" ("+c.Latency+")")
		} else {
			parts = append(parts, c.Status)
		}
	}
	if c.PodStatus != "" {
		if c.PodReadyText != "" {
			parts = append(parts, c.PodReadyText)
		} else {
			parts = append(parts, c.PodStatus)
		}
	}
	return strings.Join(parts, " · ")
}

// tabID and panelID derive stable, index-based ids for a tab button and its
// associated panel (e.g. "tab-0"/"panel-0"), linked by aria-controls/
// aria-labelledby per the WAI-ARIA tabs pattern. Index-based rather than
// name-based since a tab's Name isn't guaranteed unique or slug-safe.
func tabID(i int) string {
	return "tab-" + strconv.Itoa(i)
}

func panelID(i int) string {
	return "panel-" + strconv.Itoa(i)
}

// ariaSelectedAttr and tabIndexAttr render a tab button's initial
// aria-selected/tabindex state server-side (see cards.templ's Cards), so the
// default-active tab is correct before any client-side JS runs. index.templ's
// showTab() keeps both in sync with the client-selected tab afterward.
func ariaSelectedAttr(selected bool) string {
	return strconv.FormatBool(selected)
}

func tabIndexAttr(selected bool) string {
	if selected {
		return "0"
	}
	return "-1"
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

// themeColorHex picks the color for the <meta name="theme-color"> tag,
// which browsers/OSes use to tint native UI chrome (mobile address bar,
// task switcher, etc.) around the page. It mirrors the page's own
// [data-theme="dark"/"light"] --bg custom property (see index.templ) so the
// native chrome matches the page background rather than a fixed color that
// looks wrong on the theme currently in effect.
func themeColorHex(theme string, ramp Ramp) string {
	if theme == themeLight {
		return ramp.C50
	}
	return ramp.C900
}

// cssStringEscape escapes a value for safe embedding both inside a
// double-quoted CSS string literal (e.g. url("...")) and inside the raw,
// unescaped <style> element backgroundStyle emits via @templ.Raw.
// Background.Image is the one CSS value in this page that comes from
// CRD-supplied free text rather than a fixed lookup table, so it's the one
// value that needs this. Backslash/quote escaping alone is enough for a CSS
// string literal, but templ.Raw means the surrounding HTML is never escaped
// either: without also escaping '<' and '>', a value containing
// `"></style><script>...</script>` would close the <style> tag early and
// inject arbitrary markup into the page, regardless of how the quotes
// inside it are escaped.
func cssStringEscape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, `<`, `&lt;`)
	s = strings.ReplaceAll(s, `>`, `&gt;`)
	return s
}

// backgroundStyle returns a complete "<style>body::before { ... }</style>"
// element setting the background image on a viewport-fixed pseudo-element,
// for emission via @templ.Raw as ordinary element content (a sibling node,
// not text typed inside a literal <style> tag in the .templ source — templ
// treats a <style> tag's own text content as raw/opaque and won't evaluate
// an @templ.Raw call written inside it). The image is applied via
// `position: fixed` on body::before rather than `background-attachment:
// fixed` on body itself: iOS Safari doesn't support background-attachment:
// fixed and instead sizes/positions the image against the whole scrollable
// document, producing a hugely zoomed-in image. z-index: -1 keeps the
// pseudo-element behind all page content (checked: the only other z-indexed
// elements in this page are the quick-launch overlay at 50 and the color
// menu at 40, both far above).
// It's a full <style> tag rather than a style="" attribute value because
// the quoted url("...") it needs would otherwise go through templ's
// HTML-attribute escaping twice — once for the quotes templ.SafeCSS itself
// encodes, again when the attribute value as a whole is serialized —
// corrupting the URL. Raw element content only passes through escaping
// once (none, since this is explicitly Raw), so it's the correct place for
// a value containing literal quote characters.
//
// nonce is the per-request CSP nonce (see server.go's securityHeaders):
// with script-src/style-src locked to 'nonce-...' instead of
// 'unsafe-inline', every inline <style>/<script> — including this
// @templ.Raw one, which templ's own automatic nonce handling (used by
// @templ.JSONScript and literal <style>/<script> tags parsed from .templ
// source) doesn't reach — must carry it to render at all. nonce is always
// server-generated (see server.go's generateNonce), never derived from CRD
// input, so it needs no HTML-attribute escaping of its own.
func backgroundStyle(nonce string, bg *Background) string {
	if bg == nil {
		return ""
	}
	return fmt.Sprintf(`<style nonce="%s">body::before { content: ""; position: fixed; inset: 0; z-index: -1; background-image: url("%s"); background-size: cover; background-position: center; }</style>`,
		nonce, cssStringEscape(bg.Image))
}

// customStyle returns a complete "<style>...</style>" element wrapping the
// DashboardStyle's CustomCSS, nonce-carrying like backgroundStyle above (same
// reasoning: emitted via @templ.Raw, so templ's automatic nonce handling
// doesn't reach it). Returns "" when css is empty, so the caller's
// @templ.Raw call renders nothing.
func customStyle(nonce, css string) string {
	if css == "" {
		return ""
	}
	return fmt.Sprintf(`<style nonce="%s">%s</style>`, nonce, cssStringEscape(css))
}

// customScript returns a complete "<script>...</script>" element wrapping
// the DashboardStyle's CustomJS, nonce-carrying like backgroundStyle/
// customStyle above. Returns "" when js is empty.
func customScript(nonce, js string) string {
	if js == "" {
		return ""
	}
	return fmt.Sprintf(`<script nonce="%s">%s</script>`, nonce, jsStringEscape(js))
}

// scriptCloseTag matches a case-insensitive "</script" anywhere in a string,
// the one sequence jsStringEscape must neutralize: CustomJS is emitted
// verbatim inside a literal <script> element via @templ.Raw (see index.templ),
// so a value containing it would otherwise close that tag early and let
// whatever follows execute/render as ordinary page markup instead of script
// text, regardless of the JavaScript inside being otherwise well-formed.
var scriptCloseTag = regexp.MustCompile(`(?i)</script`)

// jsStringEscape escapes CustomJS for safe embedding as the raw text content
// of a literal <script> block (see CustomCSS's cssStringEscape for the same
// concern applied to <style>).
func jsStringEscape(s string) string {
	return scriptCloseTag.ReplaceAllString(s, "<\\/script")
}

// versionFooterText formats the dashboard's version/commit footer text,
// e.g. "v0.4.0 (abc1234)". commit is omitted when empty, "dev" (the
// ldflags-unset fallback — see cmd/main.go), or identical to version.
func versionFooterText(version, commit string) string {
	version = cmp.Or(version, "dev")
	if commit == "" || commit == "dev" || commit == version {
		return version
	}
	return version + " (" + commit + ")"
}
