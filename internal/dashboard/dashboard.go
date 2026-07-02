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

	// Namespace and InstanceName select the single Instance this process
	// serves a dashboard for (D11: one dashboard Deployment per Instance).
	Namespace    string
	InstanceName string

	Addr string
	// MetricsAddr is the address /metrics is served on, deliberately a
	// separate listener from Addr: the dashboard's main port is commonly
	// exposed via the Instance's Ingress/HTTPRoute, and Prometheus metrics
	// (per-widget-type poll counts/latencies, per-service up/down) shouldn't
	// be reachable by anyone who can reach the dashboard's public URL.
	// instance_controller.go's Deployment/Service expose this port
	// separately from spec.ingress/spec.gateway, which only ever target
	// Addr's port.
	MetricsAddr  string
	PollInterval time.Duration

	// Version/Commit are stamped at build time (cmd/main.go's ldflags-set
	// package vars), shown in the page shell's footer unless the bound
	// Configuration sets HideVersion.
	Version string
	Commit  string

	// GatewayAPIEnabled reports whether the cluster has Gateway API CRDs
	// installed, checked once at this process's own startup (cmd/main.go's
	// gatewayAPIAvailable, the same helper the manager uses for
	// spec.gateway) — this is a separate pod from the manager, so it can't
	// just read the manager's own in-memory result. Gates whether the
	// Poller attempts HTTPRoute discovery at all: the per-Instance Role
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

	store := NewStore()
	poller := &Poller{
		Reader:            clu.GetClient(),
		SecretReader:      secretClient,
		KubeReader:        kubeClient,
		Namespace:         opts.Namespace,
		InstanceName:      opts.InstanceName,
		Interval:          opts.PollInterval,
		HTTPClient:        newGuardedHTTPClient(10 * time.Second),
		Store:             store,
		GatewayAPIEnabled: opts.GatewayAPIEnabled,
	}
	go poller.Run(ctx)

	srv := &Server{
		Store:          store,
		Reader:         clu.GetClient(),
		SecretReader:   secretClient,
		Namespace:      opts.Namespace,
		InstanceName:   opts.InstanceName,
		RefreshSeconds: int(opts.PollInterval.Seconds()),
		Version:        opts.Version,
		Commit:         opts.Commit,
	}
	httpServer := &http.Server{
		Addr:              opts.Addr,
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

	metricsAddr := opts.MetricsAddr
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

	setupLog.Info("Starting dashboard", "addr", opts.Addr, "namespace", opts.Namespace, "instance", opts.InstanceName)
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}
