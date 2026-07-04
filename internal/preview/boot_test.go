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

// TestPreviewServesConfigSamples boots the dashboard against this repo's own
// config/samples the same way `mise run preview` does, and asserts GET /
// renders successfully — a regression guard that the shipped samples stay
// loadable by preview mode as the CRD types evolve.
func TestPreviewServesConfigSamples(t *testing.T) {
	result, err := Load(Config{Scheme: testScheme(t), Paths: []string{"../../config/samples"}})
	if err != nil {
		t.Fatalf("Load(config/samples): %v", err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	errCh := make(chan error, 1)
	go func() {
		errCh <- dashboard.RunPreview(ctx, dashboard.PreviewOptions{
			Reader:        result.Reader,
			Namespace:     result.Namespace,
			DashboardName: result.DashboardName,
			Addr:          addr,
			MetricsAddr:   "127.0.0.1:0",
			PollInterval:  time.Hour,
			Version:       "test",
			Commit:        "test",
		})
	}()

	baseURL := "http://" + addr
	var resp *http.Response
	deadline := time.Now().Add(5 * time.Second)
	for {
		resp, err = http.Get(baseURL + "/") //nolint:gosec,noctx // fixed loopback test address, no external input
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("dashboard never became reachable at %s: %v", baseURL, err)
		}
		time.Sleep(50 * time.Millisecond)
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
