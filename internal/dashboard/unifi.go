package dashboard

import (
	"cmp"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

func init() {
	Register("unifi", &unifiWidget{})
}

// unifiWidget polls a UniFi Network controller's Network Integration API
// (a stateless REST API introduced for third-party integrations, distinct
// from the private/UI-facing API the classic session-cookie login talked
// to). Auth is a single static "X-API-KEY" header — no login call, no
// session cookie, no CSRF token, no UniFi-OS-vs-classic-controller branching
// — since the Integration API is only ever mounted at
// "/proxy/network/integration/v1/..." regardless of controller hardware.
//
// Secrets: "apiKey" — an Integration API key generated in the controller's
// Settings > Control Plane > Integrations (or Settings > System > Advanced
// on older UI). Config: {"site": "default", "insecureTLS": false} — site is
// the site's configured *name* (not its internal id, which this widget
// resolves via the /sites endpoint on every poll, matching how the site was
// already looked up under the old session-based login flow); insecureTLS is
// an explicit opt-in for self-hosted controllers using a self-signed
// certificate (common for Cloud Key / on-prem installs).
type unifiWidget struct{}

type unifiConfig struct {
	Site        string `json:"site"`
	InsecureTLS bool   `json:"insecureTLS"`
}

const unifiIntegrationBasePath = "/proxy/network/integration/v1"

type unifiIntegrationSite struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type unifiSitesResponse struct {
	Data []unifiIntegrationSite `json:"data"`
}

type unifiIntegrationDevice struct {
	// State mirrors the Integration API's device lifecycle values (e.g.
	// "ONLINE", "OFFLINE", "PENDING_ADOPTION", "UPDATING", ...); compared
	// case-insensitively since casing isn't consistently documented across
	// controller versions.
	State string `json:"state"`
}

type unifiDevicesResponse struct {
	Data       []unifiIntegrationDevice `json:"data"`
	TotalCount int                      `json:"totalCount"`
}

type unifiClientsResponse struct {
	Data       []json.RawMessage `json:"data"`
	TotalCount int               `json:"totalCount"`
}

func (unifiWidget) Poll(ctx context.Context, httpClient *http.Client, cfg WidgetConfig) ([]Field, error) {
	if cfg.URL == "" {
		return nil, errors.New("unifi widget: url is required")
	}
	apiKey := cfg.Secrets["apiKey"]
	if apiKey == "" {
		return nil, errors.New("unifi widget: secrets.apiKey is required")
	}

	var unifiCfg unifiConfig
	if len(cfg.Config) > 0 {
		if err := json.Unmarshal(cfg.Config, &unifiCfg); err != nil {
			return nil, fmt.Errorf("decoding widget config: %w", err)
		}
	}
	site := cmp.Or(unifiCfg.Site, "default")

	baseURL := strings.TrimRight(cfg.URL, "/")
	client := unifiHTTPClient(httpClient, baseURL, unifiCfg.InsecureTLS)

	sitesReq, err := unifiIntegrationRequest(ctx, baseURL+unifiIntegrationBasePath+"/sites", apiKey)
	if err != nil {
		return nil, err
	}
	var sites unifiSitesResponse
	if fields, err := doJSONRequest(client, sitesReq, &sites); fields != nil || err != nil {
		return fields, err
	}

	var siteID string
	for _, s := range sites.Data {
		if s.Name == site {
			siteID = s.ID
			break
		}
	}
	if siteID == "" {
		// A named site that doesn't exist (typo, or a site never adopted by
		// this API key) is a poll failure to report, not a Go programmer
		// error: the same widget config might start working again once the
		// site/permissions are fixed controller-side.
		return []Field{{Label: labelStatus, Value: statusUnreach}}, nil
	}

	devicesReq, err := unifiIntegrationRequest(ctx, fmt.Sprintf("%s%s/sites/%s/devices", baseURL, unifiIntegrationBasePath, siteID), apiKey)
	if err != nil {
		return nil, err
	}
	var devices unifiDevicesResponse
	if fields, err := doJSONRequest(client, devicesReq, &devices); fields != nil || err != nil {
		return fields, err
	}

	clientsReq, err := unifiIntegrationRequest(ctx, fmt.Sprintf("%s%s/sites/%s/clients", baseURL, unifiIntegrationBasePath, siteID), apiKey)
	if err != nil {
		return nil, err
	}
	var clients unifiClientsResponse
	if fields, err := doJSONRequest(client, clientsReq, &clients); fields != nil || err != nil {
		return fields, err
	}

	status := statusUnknown
	if len(devices.Data) > 0 {
		status = statusHealthy
		for _, d := range devices.Data {
			if !strings.EqualFold(d.State, "online") {
				status = statusDegraded
				break
			}
		}
	}

	clientCount := clients.TotalCount
	if clientCount == 0 {
		clientCount = len(clients.Data)
	}

	return []Field{
		{Label: labelStatus, Value: status},
		{Label: labelClients, Value: fmt.Sprintf("%d", clientCount)},
	}, nil
}

func (unifiWidget) Sample(WidgetConfig) []Field {
	return []Field{
		{Label: labelStatus, Value: statusHealthy},
		{Label: labelClients, Value: "24"},
	}
}

// unifiIntegrationRequest builds a GET request against the Integration API
// with the X-API-KEY header set — every request this widget makes uses the
// same auth, so this is the one place that sets it.
func unifiIntegrationRequest(ctx context.Context, endpoint, apiKey string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("X-API-KEY", apiKey)
	return req, nil
}

// unifiInsecureClientCache holds one *http.Client per baseURL for
// insecureTLS controllers, so unifiHTTPClient builds (and keeps open
// connections in) a fresh *http.Transport once per target rather than on
// every poll — an insecureTLS controller was otherwise paying full TLS
// handshake cost each cycle instead of reusing keep-alive connections like
// every other widget. Bounded (see boundedClientCache) so editing a
// controller's baseURL over the dashboard pod's indefinite lifetime doesn't
// leak *http.Client/*http.Transport entries forever.
var unifiInsecureClientCache = newBoundedClientCache()

// unifiHTTPClient returns client unchanged unless insecureTLS is set, in
// which case it returns a cached (or newly built and cached) client for
// baseURL with certificate verification disabled — separate from client
// since that's the single *http.Client shared by every widget poll and must
// stay safe for the controllers that don't need this opt-out. The returned
// client still goes through newGuardedTransport's link-local dial guard,
// same as client itself.
func unifiHTTPClient(client *http.Client, baseURL string, insecureTLS bool) *http.Client {
	if !insecureTLS {
		return client
	}

	return unifiInsecureClientCache.getOrCreate(baseURL, func() *http.Client {
		transport := newGuardedTransport(nil)
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // explicit per-widget opt-in, see unifiConfig.InsecureTLS
		return &http.Client{Timeout: client.Timeout, Transport: transport}
	})
}
