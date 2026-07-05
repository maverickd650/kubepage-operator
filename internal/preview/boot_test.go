package preview

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/maverickd650/kubepage-operator/internal/dashboard"
)

// bootPreview starts dashboard.RunPreview with opts (Reader/Namespace/
// DashboardName/Version/Commit are filled in by the caller; Addr/MetricsAddr
// are always assigned here) against an OS-assigned loopback port, blocks
// until GET / responds, and returns the base URL plus a shutdown func that
// cancels the context and waits for RunPreview to return (or fails the test
// if it doesn't within 5s). Shared by every boot test in this file so the
// listener/goroutine/readiness-poll/shutdown protocol can't drift between
// them.
func bootPreview(t *testing.T, opts dashboard.PreviewOptions) (baseURL string, shutdown func()) {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	opts.Addr = addr
	opts.MetricsAddr = "127.0.0.1:0"

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	errCh := make(chan error, 1)
	go func() { errCh <- dashboard.RunPreview(ctx, opts) }()

	baseURL = "http://" + addr
	deadline := time.Now().Add(5 * time.Second)
	for {
		resp, err := http.Get(baseURL + "/") //nolint:gosec,noctx // fixed loopback test address, no external input
		if err == nil {
			_ = resp.Body.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("dashboard never became reachable at %s: %v", baseURL, err)
		}
		time.Sleep(50 * time.Millisecond)
	}

	return baseURL, func() {
		cancel()
		select {
		case err := <-errCh:
			if err != nil {
				t.Errorf("RunPreview returned an error after shutdown: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("RunPreview did not shut down within 5s of context cancellation")
		}
	}
}

// TestPreviewServesConfigSamples boots the dashboard against this repo's own
// config/samples the same way `mise run preview` does, and asserts GET /
// renders successfully — a regression guard that the shipped samples stay
// loadable by preview mode as the CRD types evolve.
func TestPreviewServesConfigSamples(t *testing.T) {
	result, err := Load(Config{Scheme: testScheme(t), Paths: []string{"../../config/samples"}})
	if err != nil {
		t.Fatalf("Load(config/samples): %v", err)
	}

	baseURL, shutdown := bootPreview(t, dashboard.PreviewOptions{
		Reader:        result.Reader,
		Namespace:     result.Namespace,
		DashboardName: result.DashboardName,
		PollInterval:  time.Hour,
		Version:       testVersion,
		Commit:        testVersion,
	})

	resp, err := http.Get(baseURL + "/") //nolint:gosec,noctx // fixed loopback test address, no external input
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET / status = %d, want 200", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	// Bookmarks render straight from LoadSite (a cached-reader read),
	// independent of the Poller's own ticker — unlike a ServiceCard's
	// widget/siteMonitor fields, which only appear once the Poller's first
	// cycle actually completes its outbound probes. Asserting on the sample
	// Bookmark keeps this test deterministic instead of racing a real
	// network round-trip to config/samples' (nonexistent) plex.example.com.
	if !strings.Contains(string(body), "Github") {
		t.Errorf("GET / body missing the sample Bookmark's name %q", "Github")
	}

	shutdown()
}

// TestPreviewSampleDataServesConfigSamples is TestPreviewServesConfigSamples'
// --sample-data counterpart: boots preview against config/samples with
// SampleData set and asserts the sample-data banner renders and at least one
// widget's placeholder Fields show up in /fragment — proving --sample-data
// works end to end against the repo's own shipped samples, not just against
// synthetic fixtures in internal/dashboard's own tests.
func TestPreviewSampleDataServesConfigSamples(t *testing.T) {
	result, err := Load(Config{Scheme: testScheme(t), Paths: []string{"../../config/samples"}})
	if err != nil {
		t.Fatalf("Load(config/samples): %v", err)
	}

	baseURL, shutdown := bootPreview(t, dashboard.PreviewOptions{
		Reader:        result.Reader,
		Namespace:     result.Namespace,
		DashboardName: result.DashboardName,
		PollInterval:  100 * time.Millisecond,
		Version:       testVersion,
		Commit:        testVersion,
		SampleData:    true,
	})

	resp, err := http.Get(baseURL + "/") //nolint:gosec,noctx // fixed loopback test address, no external input
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET / status = %d, want 200", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "Sample data") {
		t.Error("GET / body missing the sample-data banner")
	}

	// Give the Poller at least one sample cycle before checking /fragment.
	deadline := time.Now().Add(5 * time.Second)
	var fragmentBody string
	for {
		fragResp, err := http.Get(baseURL + "/fragment") //nolint:gosec,noctx // fixed loopback test address
		if err == nil {
			b, _ := io.ReadAll(fragResp.Body)
			_ = fragResp.Body.Close()
			fragmentBody = string(b)
			if strings.Contains(fragmentBody, statusHealthyText) {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("GET /fragment never showed sample widget data; last body:\n%s", fragmentBody)
		}
		time.Sleep(50 * time.Millisecond)
	}

	shutdown()
}

// statusHealthyText is the literal "Status: Healthy" rendering config/
// samples' plex ServiceCard's widget shows under --sample-data (see
// internal/dashboard's plexWidget.Sample) — plain text, not a Go constant
// import, since internal/preview intentionally has no dependency on
// internal/dashboard's unexported widget internals.
const statusHealthyText = "Healthy"

// testVersion is the placeholder dashboard.PreviewOptions.Version/Commit
// this file's boot tests pass; the value itself is never asserted on.
const testVersion = "test"
