package dashboard

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// probeHTTP performs an HTTP reachability check of url, returning whether the
// endpoint is up (responded with a 2xx/3xx status) and how long the request
// took. It is the shared implementation behind both ServiceEntry.Ping and
// ServiceEntry.SiteMonitor — deliberately HTTP-only so the dashboard pod needs
// no raw-socket / NET_RAW capability for ICMP. A transport error (DNS failure,
// connection refused, timeout) returns up=false with the error; an HTTP
// response of any status returns no error, with up reflecting the status code.
func probeHTTP(ctx context.Context, httpClient *http.Client, url string) (up bool, latency time.Duration, err error) {
	start := time.Now()

	// Prefer HEAD (cheaper); fall back to GET when the server rejects HEAD.
	resp, err := doProbe(ctx, httpClient, http.MethodHead, url)
	if err != nil {
		return false, time.Since(start), err
	}
	if resp.StatusCode == http.StatusMethodNotAllowed {
		_ = resp.Body.Close()
		resp, err = doProbe(ctx, httpClient, http.MethodGet, url)
		if err != nil {
			return false, time.Since(start), err
		}
	}
	_ = resp.Body.Close()

	latency = time.Since(start)
	up = resp.StatusCode >= 200 && resp.StatusCode < 400
	return up, latency, nil
}

func doProbe(ctx context.Context, httpClient *http.Client, method, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return nil, fmt.Errorf("building %s request: %w", method, err)
	}
	return httpClient.Do(req)
}

// monitorResult fills a Card's Status/Latency from an HTTP probe of url.
func monitorResult(ctx context.Context, httpClient *http.Client, url string) (status, latency string) {
	up, took, err := probeHTTP(ctx, httpClient, url)
	if err != nil || !up {
		return "Down", ""
	}
	return "Up", took.Round(time.Millisecond).String()
}
