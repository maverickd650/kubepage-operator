package dashboard

import (
	"context"
	"fmt"
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

	return serve(ctx, serveParams{
		Namespace:         opts.Namespace,
		DashboardName:     opts.DashboardName,
		Addr:              opts.Addr,
		MetricsAddr:       opts.MetricsAddr,
		PollInterval:      opts.PollInterval,
		Version:           opts.Version,
		Commit:            opts.Commit,
		GatewayAPIEnabled: opts.GatewayAPIEnabled,
	}, clu.GetClient(), secretClient, kubeClient)
}

// serveParams is the subset of Options/PreviewOptions needed once the three
// client.Readers (CRD cache, secrets, cluster-scoped) already exist — shared
// by Run, which builds them from a live rest.Config, and RunPreview
// (preview.go), which builds them from local YAML manifests (see
// internal/preview) instead.
type serveParams struct {
	Namespace     string
	DashboardName string

	Addr         string
	MetricsAddr  string
	PollInterval time.Duration

	Version string
	Commit  string

	GatewayAPIEnabled bool
}

// serve wires Store/Poller/Server together and blocks serving the dashboard
// HTTP and metrics servers until ctx is done.
func serve(ctx context.Context, p serveParams, reader, secretReader, kubeReader client.Reader) error {
	store := NewStore()
	poller := &Poller{
		Reader:            reader,
		SecretReader:      secretReader,
		KubeReader:        kubeReader,
		Namespace:         p.Namespace,
		DashboardName:     p.DashboardName,
		Interval:          p.PollInterval,
		HTTPClient:        newGuardedHTTPClient(10 * time.Second),
		Store:             store,
		GatewayAPIEnabled: p.GatewayAPIEnabled,
	}
	go poller.Run(ctx)

	srv := &Server{
		Store:          store,
		Reader:         reader,
		SecretReader:   secretReader,
		Namespace:      p.Namespace,
		DashboardName:  p.DashboardName,
		RefreshSeconds: int(p.PollInterval.Seconds()),
		Version:        p.Version,
		Commit:         p.Commit,
	}
	httpServer := &http.Server{
		Addr:              p.Addr,
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

	metricsAddr := p.MetricsAddr
	if metricsAddr == "" {
		metricsAddr = defaultMetricsAddr
	}
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

	setupLog.Info("Starting dashboard", "addr", p.Addr, "namespace", p.Namespace, "instance", p.DashboardName)
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}
