package dashboard

import (
	"context"
	"net"
	"path/filepath"
	"testing"
	"time"

	"go.uber.org/goleak"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

// TestRunStopsAllGoroutinesOnContextCancel exercises Run's shutdown path: the
// cache-cluster, poller, and shutdown-watcher goroutines it spawns (see
// dashboard.go) must all actually exit once ctx is canceled, not just the
// foreground call to Run itself. This is the one function in the package
// that's goroutine-dense enough for a regression (e.g. a goroutine that
// stops honoring ctx, or shutdown ordering that hangs Shutdown waiting on an
// in-flight poll) to leak — goleak.VerifyNone after Run returns is what
// would actually catch that, where merely asserting Run() returned would not.
//
// This needs a real API server (cluster.New issues List/Watch against
// opts.RestConfig, which a fake client.Client can't serve), so it uses
// envtest rather than the fake client the rest of this package's tests use.
func TestRunStopsAllGoroutinesOnContextCancel(t *testing.T) {
	testEnv := &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}
	cfg, err := testEnv.Start()
	if err != nil {
		t.Fatalf("starting envtest: %v", err)
	}
	t.Cleanup(func() {
		if err := testEnv.Stop(); err != nil {
			t.Errorf("stopping envtest: %v", err)
		}
	})

	scheme := testScheme(t)
	setupClient, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatalf("building setup client: %v", err)
	}
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "dashboard-run-"}}
	if err := setupClient.Create(context.Background(), ns); err != nil {
		t.Fatalf("creating namespace: %v", err)
	}

	addr := freeTCPAddr(t)
	ctx, cancel := context.WithCancel(context.Background())

	// Anything started by the test binary, envtest's own client machinery,
	// etc. before this point is not part of what we're checking — only
	// goroutines Run itself spawns and fails to tear down.
	leakOpt := goleak.IgnoreCurrent()

	runErr := make(chan error, 1)
	go func() {
		runErr <- Run(ctx, Options{
			RestConfig:   cfg,
			Scheme:       scheme,
			Namespace:    ns.Name,
			InstanceName: "main",
			Addr:         addr,
			PollInterval: 50 * time.Millisecond,
		})
	}()

	waitForListening(t, addr, 10*time.Second)

	cancel()

	select {
	case err := <-runErr:
		if err != nil {
			t.Errorf("Run() returned error %v, want nil after context cancel", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run() did not return within 10s of context cancellation")
	}

	// http2's per-connection read loop is a known goleak false positive for
	// any code using a Kubernetes REST client: client-go's transport pools
	// HTTP/2 connections and keeps their read loop goroutine alive on an
	// idle timeout rather than tearing it down on context cancellation
	// (neither client-go nor controller-runtime expose a way to force-close
	// a rest.Config's underlying transport). That's an intentional
	// keep-alive/connection-reuse tradeoff, not a leak Run introduced — it
	// would still be there with a correctly-implemented Run.
	ignoreHTTP2ReadLoop := goleak.IgnoreTopFunction("golang.org/x/net/http2.(*clientConnReadLoop).run")

	if err := goleak.Find(leakOpt, ignoreHTTP2ReadLoop); err != nil {
		t.Errorf("goroutines leaked after Run() returned: %v", err)
	}
}

// freeTCPAddr returns a "127.0.0.1:port" address on an OS-assigned free
// port, suitable for handing to Options.Addr.
func freeTCPAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("finding a free port: %v", err)
	}
	addr := l.Addr().String()
	if err := l.Close(); err != nil {
		t.Fatalf("releasing port: %v", err)
	}
	return addr
}

// waitForListening polls addr until something accepts a TCP connection,
// confirming Run's httpServer.ListenAndServe has actually bound it (i.e.
// cluster cache sync and poller startup already happened) before the test
// cancels the context — canceling too early would only exercise the
// early-return path, not the steady-state shutdown.
func waitForListening(t *testing.T, addr string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("dashboard server never started listening on %s within %s", addr, timeout)
}
