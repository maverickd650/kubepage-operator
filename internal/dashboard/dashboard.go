package dashboard

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
)

var setupLog = ctrl.Log.WithName("dashboard")

// Options configures a Run of the dashboard subcommand.
type Options struct {
	RestConfig *rest.Config
	Scheme     *runtime.Scheme

	// Namespace and InstanceName select the single Instance this process
	// serves a dashboard for (D11: one dashboard Deployment per Instance).
	Namespace    string
	InstanceName string

	Addr         string
	PollInterval time.Duration
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
		Reader:       clu.GetClient(),
		SecretReader: secretClient,
		KubeReader:   kubeClient,
		Namespace:    opts.Namespace,
		InstanceName: opts.InstanceName,
		Interval:     opts.PollInterval,
		HTTPClient:   &http.Client{Timeout: 10 * time.Second},
		Store:        store,
	}
	go poller.Run(ctx)

	srv := &Server{
		Store:          store,
		Reader:         clu.GetClient(),
		Namespace:      opts.Namespace,
		InstanceName:   opts.InstanceName,
		RefreshSeconds: int(opts.PollInterval.Seconds()),
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

	setupLog.Info("Starting dashboard", "addr", opts.Addr, "namespace", opts.Namespace, "instance", opts.InstanceName)
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}
