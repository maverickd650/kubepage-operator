package dashboard

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

const statusDown = "Down"

// probeHTTP performs an HTTP reachability check of url, returning whether the
// endpoint is up (responded with a 2xx/3xx status) and how long the request
// took. It is the implementation behind ServiceEntry.Monitor — deliberately
// HTTP-only so the dashboard pod needs
// no raw-socket / NET_RAW capability for ICMP. A transport error (DNS failure,
// connection refused, timeout) returns up=false with the error; an HTTP
// response of any status returns no error, with up reflecting the status code.
//
// statusCode is the status the endpoint answered with, or 0 when the request
// never got that far. The two "down" cases — no response at all, and a
// response the probe considers a failure — are otherwise indistinguishable to
// the caller, and they call for completely different fixes (fix the network
// vs. fix the URL), so monitorResult needs both to say which happened.
func probeHTTP(ctx context.Context, httpClient *http.Client, url string) (up bool, statusCode int, latency time.Duration, err error) {
	start := time.Now()

	// Prefer HEAD (cheaper); fall back to GET when the server rejects HEAD
	// outright (405) or doesn't implement it at all (501 — seen from some
	// upstreams that only ever implement GET/POST). Deliberately not
	// extended to any other status (e.g. 404): that would double every
	// genuinely-down probe's request count for no benefit.
	resp, err := doProbe(ctx, httpClient, http.MethodHead, url)
	if err != nil {
		return false, 0, time.Since(start), err
	}
	if resp.StatusCode == http.StatusMethodNotAllowed || resp.StatusCode == http.StatusNotImplemented {
		_ = resp.Body.Close()
		resp, err = doProbe(ctx, httpClient, http.MethodGet, url)
		if err != nil {
			return false, 0, time.Since(start), err
		}
	}
	_ = resp.Body.Close()

	latency = time.Since(start)
	up = resp.StatusCode >= 200 && resp.StatusCode < 400
	return up, resp.StatusCode, latency, nil
}

func doProbe(ctx context.Context, httpClient *http.Client, method, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return nil, fmt.Errorf("building %s request: %w", method, err)
	}
	return httpClient.Do(req)
}

// monitorResult fills a Card's Status/Latency from an HTTP probe of url.
//
// A "Down" pill carries no reason and, unlike a widget's card error, has
// nowhere on the page to put one — so the reason is logged instead
// (deduplicated per URL, see logPollFailure). Which of the two failures it
// was matters: nothing answered at all is a network/DNS/TLS problem, while a
// status the probe rejects means the URL is reachable but wrong (a 404 path,
// or a 401/403 login redirect on a URL that needs auth).
func monitorResult(ctx context.Context, httpClient *http.Client, url string) (status, latency string) {
	up, statusCode, took, err := probeHTTP(ctx, httpClient, url)
	switch {
	case err != nil:
		logPollFailure(url, err, "monitor probe failed", "url", url)
		return statusDown, ""
	case !up:
		logPollFailure(url, nil, "monitor probe returned a status treated as down",
			"url", url, "status", statusCode)
		return statusDown, ""
	}
	clearPollFailure(url)
	return "Up", took.Round(time.Millisecond).String()
}
