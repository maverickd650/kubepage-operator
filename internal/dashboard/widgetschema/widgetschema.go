// Package widgetschema declares the known config keys for every
// widget type, so that internal/controller's reconcilers can validate a
// ServiceWidget.Config or InfoWidgetEntry.Config block without importing
// internal/dashboard's widget implementations (their polling behavior, HTTP
// clients, etc.) — this package has no dependency beyond the standard
// library, and is the only widget-schema knowledge internal/controller needs.
package widgetschema

import "slices"

// Config key names shared by more than one schema entry (and their
// unit tests), pulled out as constants purely to satisfy goconst.
const (
	keyAccountID = "accountId"
	keyTunnelID  = "tunnelId"
	keyQuery     = "query"
	keyLabel     = "label"
)

// ConfigSchema declares one widget type's config contract: which
// keys must be present, and which are recognized but not required. Any key
// present that isn't in either list is "unknown" — not fatal (forward
// compatibility with an older operator reading a newer config), but worth
// surfacing.
type ConfigSchema struct {
	Required []string
	Optional []string
}

// ConfigSchemas maps a widget type (ServiceWidget.Type or
// InfoWidgetEntry.Type) to its config contract. Populated from each
// widget implementation's own config struct in internal/dashboard/*.go (the
// authoritative source — see each file's json tags) and from the doc
// comments on ServiceWidget.Config/InfoWidgetEntry.Config
// (api/v1alpha1/servicecard_types.go, api/v1alpha1/infowidget_types.go).
//
// Every widget type registered in internal/dashboard (dashboard.RegisteredTypes)
// must have an entry here — TestConfigSchemasCoverRegisteredWidgets in
// internal/dashboard guards this. "greeting", "datetime", and "logo" are
// InfoWidget types that render statically and never call dashboard.Register,
// so they're not covered by that guard, but are still declared below since
// they too accept (or reject) config keys.
var ConfigSchemas = map[string]ConfigSchema{
	// Service widget types with no known Config keys: any key present is
	// unknown. Poll implementations for these read only the typed URL/
	// Secrets fields, never cfg.Config.
	"plex":          {},
	"stash":         {},
	"paperlessngx":  {},
	"grafana":       {},
	"prometheus":    {},
	"truenas":       {},
	"linkwarden":    {},
	"homeassistant": {},
	"mealie":        {},
	"sonarr":        {},
	"radarr":        {},
	"jellyfin":      {},
	"jellyseerr":    {},
	"immich":        {},
	"adguard":       {},
	"pihole":        {},
	"uptime-kuma":   {},
	"portainer":     {},
	"argocd":        {},
	"gitea":         {},
	"tautulli":      {},
	"netdata":       {},
	"gatus":         {},
	"nextcloud":     {},

	// Service widget types with known Config keys (internal/dashboard/*.go).
	"cloudflared":      {Required: []string{keyAccountID, keyTunnelID}},
	"customapi":        {Required: []string{"mappings"}},
	"prometheusmetric": {Required: []string{keyQuery}, Optional: []string{keyLabel}},
	"unifi":            {Optional: []string{"site", "insecureTLS"}},
	"iframe":           {Optional: []string{"height"}},
	"proxmox":          {Optional: []string{"node", "insecureTLS"}},
	"opnsense":         {Optional: []string{"wan"}},
	"speedtest":        {Optional: []string{"version"}},

	// InfoWidget static types (rendered by internal/dashboard/server.go,
	// never polled).
	"greeting": {Optional: []string{"text"}},
	"datetime": {Optional: []string{"format"}},
	"logo":     {},

	// InfoWidget pollable types (internal/dashboard/*.go). glances/longhorn
	// require the typed InfoWidgetEntry.url field (enforced by CEL on the
	// CRD, not by this config schema).
	"openmeteo":      {Required: []string{"latitude", "longitude"}, Optional: []string{"units", keyLabel}},
	"openweathermap": {Required: []string{"latitude", "longitude"}, Optional: []string{"units", keyLabel}},
	"kubemetrics":    {Optional: []string{"cpuLabel", "memoryLabel"}},
	"glances":        {Optional: []string{"apiVersion"}},
	"longhorn":       {},
}

// ValidateConfig checks configKeys — the key set of a widget's parsed
// config JSON object — against schema, returning the required keys that are
// missing and the keys present that are neither required nor optional. Both
// are returned sorted for deterministic messages.
func ValidateConfig(configKeys map[string]any, schema ConfigSchema) (missing, unknown []string) {
	for _, key := range schema.Required {
		if _, ok := configKeys[key]; !ok {
			missing = append(missing, key)
		}
	}

	known := make(map[string]bool, len(schema.Required)+len(schema.Optional))
	for _, key := range schema.Required {
		known[key] = true
	}
	for _, key := range schema.Optional {
		known[key] = true
	}
	for key := range configKeys {
		if !known[key] {
			unknown = append(unknown, key)
		}
	}

	slices.Sort(missing)
	slices.Sort(unknown)
	return missing, unknown
}
