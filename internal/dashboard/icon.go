package dashboard

import "strings"

// dashboardIconsBaseURL is the dashboard-icons project's jsdelivr CDN mirror
// (D11: "integrate dashboard-icons" for the curated widget set's look).
const dashboardIconsBaseURL = "https://cdn.jsdelivr.net/gh/homarr-labs/dashboard-icons/png/"

// IconURL resolves a ServiceEntry/Bookmark Icon field to a renderable image
// URL. A full URL passes through unchanged (homepage's own convention for
// self-hosted icons); anything else is treated as a dashboard-icons slug
// (e.g. "grafana", "prometheus") and resolved against the CDN. Returns ""
// for a nil/empty icon, which the template treats as "no icon".
func IconURL(icon *string) string {
	if icon == nil || *icon == "" {
		return ""
	}
	v := *icon
	if strings.HasPrefix(v, "http://") || strings.HasPrefix(v, "https://") {
		return v
	}
	slug := strings.TrimPrefix(strings.TrimPrefix(v, "mdi-"), "si-")
	return dashboardIconsBaseURL + strings.ToLower(slug) + ".png"
}
