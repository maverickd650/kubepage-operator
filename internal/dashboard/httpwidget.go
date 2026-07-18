package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// maxWidgetResponseBytes caps how many bytes of an upstream's response body
// a widget will read before decoding. Without this, a slow or malicious
// upstream streaming data for the duration of the shared HTTP client's
// timeout could still push an unbounded amount of data into memory per
// poll, multiplied by maxConcurrentPolls concurrent polls.
const maxWidgetResponseBytes = 2 << 20 // 2 MiB

// doJSONRequest executes req via httpClient and decodes a 2xx JSON response
// body into out, capped at maxWidgetResponseBytes. On a transport failure or
// non-2xx response it returns the widget-standard "Unreachable"/"HTTP <code>"
// status Field and a nil error — by this package's convention (see e.g.
// grafana.go), a widget reports those as a displayable Field rather than a
// Go error so the card still renders a status; poller.go's metricErr is what
// makes sure that convention doesn't also hide the failure from poll
// metrics. Callers should return immediately when the returned fields or err
// is non-nil; a nil, nil result means out was populated and the caller
// should build its own success Fields from it.
func doJSONRequest(httpClient *http.Client, req *http.Request, out any) ([]Field, error) {
	resp, err := httpClient.Do(req)
	if err != nil {
		return []Field{{Label: labelStatus, Value: statusUnreach}}, nil
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return []Field{{Label: labelStatus, Value: fmt.Sprintf("HTTP %d", resp.StatusCode)}}, nil
	}

	body := io.LimitReader(resp.Body, maxWidgetResponseBytes)
	if err := json.NewDecoder(body).Decode(out); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return nil, nil
}

// buildJSONRequest builds a GET request against cfg.URL+path, returning the
// package-standard "<name> widget: url is required" error when cfg.URL is
// unset. name is the registered widget type (e.g. "plex"), used only for
// that error message.
func buildJSONRequest(ctx context.Context, cfg WidgetConfig, name, path string) (*http.Request, error) {
	if cfg.URL == "" {
		return nil, fmt.Errorf("%s widget: url is required", name)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(cfg.URL, "/")+path, nil)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	return req, nil
}

// fetchJSON builds a GET request against cfg.URL+path (see buildJSONRequest),
// sets headers on it, executes it, and decodes a 2xx JSON response into out
// (see doJSONRequest for the returned Field/error convention). Only headers
// present in the map are set — a caller that wants a header omitted when its
// backing secret is empty should simply not add that entry, rather than
// relying on fetchJSON to skip empty values.
func fetchJSON(ctx context.Context, httpClient *http.Client, cfg WidgetConfig, name, path string, headers map[string]string, out any) ([]Field, error) {
	req, err := buildJSONRequest(ctx, cfg, name, path)
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return doJSONRequest(httpClient, req, out)
}

// fetchJSONBasicAuth is fetchJSON for widgets (adguard, ...) whose upstream
// authenticates via HTTP Basic auth instead of a header/token. Basic auth is
// only set when username is non-empty, matching every hand-rolled widget
// this replaces.
func fetchJSONBasicAuth(ctx context.Context, httpClient *http.Client, cfg WidgetConfig, name, path, username, password string, out any) ([]Field, error) {
	req, err := buildJSONRequest(ctx, cfg, name, path)
	if err != nil {
		return nil, err
	}
	if username != "" {
		req.SetBasicAuth(username, password)
	}
	return doJSONRequest(httpClient, req, out)
}
