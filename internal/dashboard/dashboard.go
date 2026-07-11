package dashboard

import (
	"cmp"
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
)

var setupLog = ctrl.Log.WithName("dashboard")

// defaultMetricsAddr is used when Options.MetricsAddr is unset.
const defaultMetricsAddr = ":9090"

// Options configures a Run of the dashboard subcommand.
type Options struct {
	RestConfig *rest.Config
	Scheme     *runtime.Scheme

	// Namespace and DashboardName select the single Dashboard this process
	// serves a dashboard for (D11: one dashboard Deployment per Dashboard).
	Namespace     string
	DashboardName string

	Addr string
	// MetricsAddr is the address /metrics is served on, deliberately a
	// separate listener from Addr: the dashboard's main port is commonly
	// exposed via the Dashboard's Ingress/HTTPRoute, and Prometheus metrics
	// (per-widget-type poll counts/latencies, per-service up/down) shouldn't
	// be reachable by anyone who can reach the dashboard's public URL.
	// instance_controller.go's Deployment/Service expose this port
	// separately from spec.ingress/spec.gateway, which only ever target
	// Addr's port.
	MetricsAddr  string
	PollInterval time.Duration

	// Version/Commit are stamped at build time (cmd/main.go's ldflags-set
	// package vars), shown in the page shell's footer unless the bound
	// DashboardStyle sets HideVersion.
	Version string
	Commit  string

	// GatewayAPIEnabled reports whether the cluster has Gateway API CRDs
	// installed, checked once at this process's own startup (cmd/main.go's
	// gatewayAPIAvailable, the same helper the manager uses for
	// spec.gateway) — this is a separate pod from the manager, so it can't
	// just read the manager's own in-memory result. Gates whether the
	// Poller attempts HTTPRoute discovery at all: the per-Dashboard Role
	// only grants httproutes RBAC when discovery is enabled and the
	// controller made this same determination (instance_rbac.go), so
	// without it a List would just fail on missing RBAC or, if Gateway API
	// truly isn't installed, on a nonexistent Kind.
	GatewayAPIEnabled bool

	// Ready, if set, is called once the main HTTP listener is bound (before
	// it starts serving) with the actual resolved address — e.g.
	// "127.0.0.1:53214" for an Addr of "127.0.0.1:0". cmd/main.go's preview
	// --open uses this to open the real bound port rather than guessing at
	// the configured Addr string, which can't be dialed directly when it
	// asks for an OS-assigned port.
	Ready func(addr string)

	// SampleData is always false for in-cluster dashboard mode; only
	// RunPreview's PreviewOptions.SampleData ever sets it. See
	// Poller.SampleData's doc comment.
	SampleData bool
}

// Run wires the CRD cache, secret-resolving client, background poller, and
// HTTP server together, and blocks serving until ctx is done.
func Run(ctx context.Context, opts Options) error {
	clu, err := cluster.New(opts.RestConfig, func(o *cluster.Options) {
		o.Scheme = opts.Scheme
		o.Cache.DefaultNamespaces = map[string]cache.Config{opts.Namespace: {}}
	})
	if err != nil {
		return fmt.Errorf("building cluster cache: %w", err)
	}

	go func() {
		if err := clu.Start(ctx); err != nil {
			setupLog.Error(err, "cluster cache stopped")
		}
	}()
	if !clu.GetCache().WaitForCacheSync(ctx) {
		return fmt.Errorf("waiting for cache sync: %w", ctx.Err())
	}

	// Secrets are read through a direct (uncached) client deliberately: D11
	// requires secret values never sit in an informer's in-memory store for
	// the life of the process, only flow through per-poll.
	secretClient, err := client.New(opts.RestConfig, client.Options{Scheme: opts.Scheme})
	if err != nil {
		return fmt.Errorf("building direct client: %w", err)
	}

	// Cluster-scoped, uncached client for ClusterWidget types (kubemetrics):
	// metrics.k8s.io doesn't support watch and nodes are cluster-scoped, so
	// neither fits the namespace-scoped cache above.
	kubeClient, err := client.New(opts.RestConfig, client.Options{Scheme: opts.Scheme})
	if err != nil {
		return fmt.Errorf("building cluster client: %w", err)
	}

	return serve(ctx, opts, clu.GetClient(), secretClient, kubeClient)
}

// serve wires Store/Poller/Server together and blocks serving the dashboard
// HTTP and metrics servers until ctx is done. opts.RestConfig/opts.Scheme are
// unused here — Run has already consumed them to build reader/secretReader/
// kubeReader by this point, and RunPreview (preview.go) never sets them.
func serve(ctx context.Context, opts Options, reader, secretReader, kubeReader client.Reader) error {
	store := NewStore()
	broadcast := NewBroadcaster()
	poller := &Poller{
		Reader:            reader,
		SecretReader:      secretReader,
		KubeReader:        kubeReader,
		Namespace:         opts.Namespace,
		DashboardName:     opts.DashboardName,
		Interval:          opts.PollInterval,
		HTTPClient:        newGuardedHTTPClient(10 * time.Second),
		Store:             store,
		GatewayAPIEnabled: opts.GatewayAPIEnabled,
		SampleData:        opts.SampleData,
		Broadcast:         broadcast,
	}
	go poller.Run(ctx)

	srv := &Server{
		Store:          store,
		Reader:         reader,
		SecretReader:   secretReader,
		Namespace:      opts.Namespace,
		DashboardName:  opts.DashboardName,
		RefreshSeconds: int(opts.PollInterval.Seconds()),
		Version:        opts.Version,
		Commit:         opts.Commit,
		SampleData:     opts.SampleData,
		Broadcast:      broadcast,
	}

	ln, err := net.Listen("tcp", opts.Addr)
	if err != nil {
		return fmt.Errorf("binding %s: %w", opts.Addr, err)
	}

	httpServer := &http.Server{
		Handler:           srv.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()

	metricsAddr := cmp.Or(opts.MetricsAddr, defaultMetricsAddr)
	metricsServer := &http.Server{
		Addr:              metricsAddr,
		Handler:           promhttp.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = metricsServer.Shutdown(shutdownCtx)
	}()
	go func() {
		setupLog.Info("Starting metrics server", "addr", metricsAddr)
		if err := metricsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			setupLog.Error(err, "Metrics server stopped")
		}
	}()

	boundAddr := ln.Addr().String()
	setupLog.Info("Starting dashboard", "addr", boundAddr, "namespace", opts.Namespace, "instance", opts.DashboardName)
	if opts.Ready != nil {
		opts.Ready(boundAddr)
	}
	if err := httpServer.Serve(ln); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}
