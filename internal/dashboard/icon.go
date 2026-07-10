package dashboard

import "strings"

// dashboardIconsBaseURL is the dashboard-icons project's jsdelivr CDN mirror
// (D11: "integrate dashboard-icons" for the curated widget set's look) —
// used for bare slugs (app logos like "grafana", "prometheus").
const dashboardIconsBaseURL = "https://cdn.jsdelivr.net/gh/homarr-labs/dashboard-icons/png/"

// iconifyBaseURL serves generic icon glyphs (as opposed to app logos) via
// Iconify's SVG API, keyed by icon set: "mdi" for Material Design Icons,
// "simple-icons" for Simple Icons (note the set name differs from the
// homepage-style "si-" prefix below).
const iconifyBaseURL = "https://api.iconify.design/"

// selfhstIconsBaseURL is selfh.st/icons' own jsdelivr CDN mirror, a separate
// icon source from both dashboard-icons and Iconify.
const selfhstIconsBaseURL = "https://cdn.jsdelivr.net/gh/selfhst/icons/"

// IconURL resolves a ServiceCard/Bookmark/LayoutGroup Icon field to a
// renderable image URL, following homepage's icon prefix conventions
// (https://gethomepage.dev/configs/services/#icons):
//
//   - A full URL passes through unchanged.
//   - "mdi-X", "si-X", "lucide-X", "wi-X", and "fa6-solid-X" resolve to the
//     actual Material Design Icon / Simple Icon / Lucide / Weather Icon /
//     Font Awesome 6 Solid glyph via Iconify's SVG API (not the
//     dashboard-icons CDN: that CDN only has app logos, not generic icon
//     glyph names, so routing these there 404s). A trailing "-#hexcolor"
//     recolors the glyph via Iconify's ?color= query param.
//   - "sh-X" resolves to a selfh.st/icons glyph; X may end in .svg/.png/.webp
//     to pick a specific format, defaulting to .png.
//   - Anything else is treated as a dashboard-icons slug (e.g. "grafana").
//
// Returns "" for a nil/empty icon, which the template treats as "no icon".
func IconURL(icon *string) string {
	if icon == nil || *icon == "" {
		return ""
	}
	v := *icon
	if strings.HasPrefix(v, "http://") || strings.HasPrefix(v, "https://") {
		return v
	}

	if rest, ok := strings.CutPrefix(v, "mdi-"); ok {
		return iconifyIconURL("mdi", rest)
	}
	if rest, ok := strings.CutPrefix(v, "si-"); ok {
		return iconifyIconURL("simple-icons", rest)
	}
	if rest, ok := strings.CutPrefix(v, "lucide-"); ok {
		return iconifyIconURL("lucide", rest)
	}
	if rest, ok := strings.CutPrefix(v, "wi-"); ok {
		return iconifyIconURL("wi", rest)
	}
	if rest, ok := strings.CutPrefix(v, "fa6-solid-"); ok {
		return iconifyIconURL("fa6-solid", rest)
	}
	if rest, ok := strings.CutPrefix(v, "sh-"); ok {
		return selfhstIconURL(rest)
	}
	return dashboardIconsBaseURL + strings.ToLower(v) + ".png"
}

// iconifyIconURL builds an Iconify SVG API URL for setName/slug, splitting
// off an optional trailing "-#hexcolor" suffix into the API's ?color= param.
func iconifyIconURL(setName, slug string) string {
	name, color := splitColorSuffix(slug)
	url := iconifyBaseURL + setName + "/" + strings.ToLower(name) + ".svg"
	if color != "" {
		url += "?color=%23" + color
	}
	return url
}

// selfhstIconURL builds a selfh.st/icons CDN URL for slug, which may carry
// an explicit .svg/.png/.webp extension to pick a format; bare slugs default
// to png, matching homepage's "sh-XX to use the png version" convention.
func selfhstIconURL(slug string) string {
	for _, ext := range []string{".svg", ".png", ".webp"} {
		if strings.HasSuffix(slug, ext) {
			format := strings.TrimPrefix(ext, ".")
			name := strings.TrimSuffix(slug, ext)
			return selfhstIconsBaseURL + format + "/" + strings.ToLower(name) + ext
		}
	}
	return selfhstIconsBaseURL + "png/" + strings.ToLower(slug) + ".png"
}

// splitColorSuffix splits a trailing "-#hexcolor" off slug, e.g.
// "flask-outline-#f0d453" -> ("flask-outline", "f0d453"). Returns the
// original slug and an empty color when there's no such suffix.
func splitColorSuffix(slug string) (name, hexColor string) {
	i := strings.LastIndex(slug, "-#")
	if i < 0 {
		return slug, ""
	}
	return slug[:i], slug[i+2:]
}
