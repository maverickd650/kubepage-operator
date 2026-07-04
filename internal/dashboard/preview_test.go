package dashboard

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

func TestNoopClusterReaderAlwaysErrors(t *testing.T) {
	r := noopClusterReader{}
	if err := r.Get(context.Background(), client.ObjectKey{}, &pagev1alpha1.Dashboard{}); !errors.Is(err, errNoCluster) {
		t.Errorf("Get() error = %v, want errNoCluster", err)
	}
	if err := r.List(context.Background(), &pagev1alpha1.DashboardList{}); !errors.Is(err, errNoCluster) {
		t.Errorf("List() error = %v, want errNoCluster", err)
	}
}

// TestRunPreviewServesAndShutsDown is RunPreview's counterpart to
// dashboard_test.go's envtest-backed TestRunStopsAllGoroutinesOnContextCancel:
// since RunPreview never touches a real cluster, it can run as a plain fake-
// client test instead, exercising the same serve() wiring Run uses.
func TestRunPreviewServesAndShutsDown(t *testing.T) {
	scheme := testScheme(t)
	style := &pagev1alpha1.DashboardStyle{
		ObjectMeta: metav1.ObjectMeta{Name: testDashboardName, Namespace: testNamespace},
		Spec:       pagev1alpha1.DashboardStyleSpec{DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName}},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(style).Build()

	addr := freeTCPAddr(t)
	ctx, cancel := context.WithCancel(context.Background())

	runErr := make(chan error, 1)
	go func() {
		runErr <- RunPreview(ctx, PreviewOptions{
			Reader:        cl,
			Namespace:     testNamespace,
			DashboardName: testDashboardName,
			Addr:          addr,
			MetricsAddr:   testEphemeralAddr,
			PollInterval:  50 * time.Millisecond,
			Version:       "test",
			Commit:        "test",
		})
	}()

	waitForListening(t, addr, 10*time.Second)

	resp, err := http.Get("http://" + addr + "/") //nolint:gosec,noctx // fixed loopback test address
	if err != nil {
		cancel()
		t.Fatalf("GET /: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET / status = %d, want 200", resp.StatusCode)
	}
	_ = resp.Body.Close()

	cancel()
	select {
	case err := <-runErr:
		if err != nil {
			t.Errorf("RunPreview() returned error %v, want nil after context cancel", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("RunPreview() did not return within 5s of context cancellation")
	}
}

// TestRunPreviewReadyReceivesActualBoundAddress verifies the fix for
// --open breaking on a ":0" (OS-assigned) Addr: Ready must fire with the
// real resolved address (dialable), not the literal configured Addr string
// — which for ":0" can never be dialed directly.
func TestRunPreviewReadyReceivesActualBoundAddress(t *testing.T) {
	scheme := testScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).Build()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	readyAddr := make(chan string, 1)
	runErr := make(chan error, 1)
	go func() {
		runErr <- RunPreview(ctx, PreviewOptions{
			Reader:        cl,
			Namespace:     testNamespace,
			DashboardName: testDashboardName,
			Addr:          testEphemeralAddr,
			MetricsAddr:   testEphemeralAddr,
			PollInterval:  time.Hour,
			Ready:         func(addr string) { readyAddr <- addr },
		})
	}()

	var addr string
	select {
	case addr = <-readyAddr:
	case <-time.After(5 * time.Second):
		t.Fatal("Ready was never called")
	}
	if addr == testEphemeralAddr || strings.HasSuffix(addr, ":0") {
		t.Fatalf("Ready received %q, want a real resolved port, not the literal :0 Addr", addr)
	}

	resp, err := http.Get("http://" + addr + "/") //nolint:gosec,noctx // fixed loopback test address
	if err != nil {
		t.Fatalf("GET %s: %v", addr, err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET %s status = %d, want 200", addr, resp.StatusCode)
	}

	cancel()
	select {
	case <-runErr:
	case <-time.After(5 * time.Second):
		t.Fatal("RunPreview() did not return within 5s of context cancellation")
	}
}

// TestKubeMetricsWidgetUsesNoopClusterReaderErrorPath exercises the
// production wiring RunPreview relies on for cluster-only widgets: with a
// noopClusterReader standing in for the cluster, kubemetrics' PollCluster
// must degrade to its normal "unreachable" status rather than erroring or
// panicking.
func TestKubeMetricsWidgetUsesNoopClusterReaderErrorPath(t *testing.T) {
	fields, err := (kubeMetricsWidget{}).PollCluster(context.Background(), noopClusterReader{}, WidgetConfig{})
	if err != nil {
		t.Fatalf("PollCluster() error = %v, want nil (degrade to unreachable status)", err)
	}
	if len(fields) != 1 || fields[0].Value != statusUnreach {
		t.Errorf("PollCluster() fields = %+v, want a single %q field", fields, statusUnreach)
	}
}
