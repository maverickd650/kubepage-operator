package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/discovery"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	metricsv1beta1 "k8s.io/metrics/pkg/apis/metrics/v1beta1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
	"github.com/maverickd650/kubepage-operator/internal/controller"
	"github.com/maverickd650/kubepage-operator/internal/dashboard"
	"github.com/maverickd650/kubepage-operator/internal/preview"
	// +kubebuilder:scaffold:imports
)

// managerContainerName is the manager Deployment's container name, set in
// config/manager/manager.yaml and dist/chart/templates/manager/manager.yaml.
// ownDashboardImage looks this container up by name in the manager's own
// running Pod to find the image value to reuse for dashboard Deployments.
const managerContainerName = "manager"

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")

	// version and commit are stamped at build time via -ldflags (see
	// Dockerfile's VERSION/REVISION build args); "dev" is the fallback for
	// `go run`/`go build` without those flags.
	version = "dev"
	commit  = "dev"
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(pagev1alpha1.AddToScheme(scheme))
	// Registered unconditionally so the client can decode HTTPRoute objects
	// when Gateway API is available; gatewayAPIAvailable (checked at startup)
	// decides whether DashboardReconciler ever watches or creates one.
	utilruntime.Must(gatewayv1.Install(scheme))
	// Registered so the dashboard's cluster client can decode NodeMetrics for
	// the kubemetrics InfoWidget (internal/dashboard/kubemetrics.go).
	utilruntime.Must(metricsv1beta1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "dashboard":
			runDashboard(os.Args[2:])
			return
		case "preview":
			runPreview(os.Args[2:])
			return
		}
	}
	runManager()
}

// nolint:gocyclo
func runManager() {
	var metricsAddr string
	var metricsCertPath, metricsCertName, metricsCertKey string
	var webhookCertPath, webhookCertName, webhookCertKey string
	var enableLeaderElection bool
	var probeAddr string
	var secureMetrics bool
	var enableHTTP2 bool
	var watchNamespaces string
	var tlsOpts []func(*tls.Config)
	flag.StringVar(&metricsAddr, "metrics-bind-address", "0", "The address the metrics endpoint binds to. "+
		"Use :8443 for HTTPS or :8080 for HTTP, or leave as 0 to disable the metrics service.")
	flag.StringVar(&watchNamespaces, "watch-namespaces", "", "Comma-separated list of namespaces to watch. "+
		"Empty (default) watches all namespaces cluster-wide. Set this for a namespace-scoped install "+
		"(see config/namespace-scoped/) so the manager's cache and RBAC needs are limited to specific "+
		"namespaces instead of cluster-wide — see SECURITY.md's P2.3 trade-off note.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&secureMetrics, "metrics-secure", true,
		"If set, the metrics endpoint is served securely via HTTPS. Use --metrics-secure=false to use HTTP instead.")
	flag.StringVar(&webhookCertPath, "webhook-cert-path", "", "The directory that contains the webhook certificate.")
	flag.StringVar(&webhookCertName, "webhook-cert-name", "tls.crt", "The name of the webhook certificate file.")
	flag.StringVar(&webhookCertKey, "webhook-cert-key", "tls.key", "The name of the webhook key file.")
	flag.StringVar(&metricsCertPath, "metrics-cert-path", "",
		"The directory that contains the metrics server certificate.")
	flag.StringVar(&metricsCertName, "metrics-cert-name", "tls.crt", "The name of the metrics server certificate file.")
	flag.StringVar(&metricsCertKey, "metrics-cert-key", "tls.key", "The name of the metrics server key file.")
	flag.BoolVar(&enableHTTP2, "enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers")
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	// if the enable-http2 flag is false (the default), http/2 should be disabled
	// due to its vulnerabilities. More specifically, disabling http/2 will
	// prevent from being vulnerable to the HTTP/2 Stream Cancellation and
	// Rapid Reset CVEs. For more information see:
	// - https://github.com/advisories/GHSA-qppj-fm5r-hxr3
	// - https://github.com/advisories/GHSA-4374-p667-p6c8
	disableHTTP2 := func(c *tls.Config) {
		setupLog.Info("Disabling HTTP/2")
		c.NextProtos = []string{"http/1.1"}
	}

	if !enableHTTP2 {
		tlsOpts = append(tlsOpts, disableHTTP2)
	}

	// Initial webhook TLS options
	webhookTLSOpts := tlsOpts
	webhookServerOptions := webhook.Options{
		TLSOpts: webhookTLSOpts,
	}

	if len(webhookCertPath) > 0 {
		setupLog.Info("Initializing webhook certificate watcher using provided certificates",
			"webhook-cert-path", webhookCertPath, "webhook-cert-name", webhookCertName, "webhook-cert-key", webhookCertKey)

		webhookServerOptions.CertDir = webhookCertPath
		webhookServerOptions.CertName = webhookCertName
		webhookServerOptions.KeyName = webhookCertKey
	}

	webhookServer := webhook.NewServer(webhookServerOptions)

	// Metrics endpoint is enabled in 'config/default/kustomization.yaml'. The Metrics options configure the server.
	// More info:
	// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.24.1/pkg/metrics/server
	// - https://book.kubebuilder.io/reference/metrics.html
	metricsServerOptions := metricsserver.Options{
		BindAddress:   metricsAddr,
		SecureServing: secureMetrics,
		TLSOpts:       tlsOpts,
	}

	if secureMetrics {
		// FilterProvider is used to protect the metrics endpoint with authn/authz.
		// These configurations ensure that only authorized users and service accounts
		// can access the metrics endpoint. The RBAC are configured in 'config/rbac/kustomization.yaml'. More info:
		// https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.24.1/pkg/metrics/filters#WithAuthenticationAndAuthorization
		metricsServerOptions.FilterProvider = filters.WithAuthenticationAndAuthorization
	}

	// If the certificate is not specified, controller-runtime will automatically
	// generate self-signed certificates for the metrics server. While convenient for development and testing,
	// this setup is not recommended for production.
	//
	// TODO(user): If you enable certManager, uncomment the following lines:
	// - [METRICS-WITH-CERTS] at config/default/kustomization.yaml to generate and use certificates
	// managed by cert-manager for the metrics server.
	// - [PROMETHEUS-WITH-CERTS] at config/prometheus/kustomization.yaml for TLS certification.
	if len(metricsCertPath) > 0 {
		setupLog.Info("Initializing metrics certificate watcher using provided certificates",
			"metrics-cert-path", metricsCertPath, "metrics-cert-name", metricsCertName, "metrics-cert-key", metricsCertKey)

		metricsServerOptions.CertDir = metricsCertPath
		metricsServerOptions.CertName = metricsCertName
		metricsServerOptions.KeyName = metricsCertKey
	}

	mgrOptions := ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsServerOptions,
		WebhookServer:          webhookServer,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "6db42ddf.kubepage.dev",
		// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
		// when the Manager ends. This requires the binary to immediately end when the
		// Manager is stopped, otherwise, this setting is unsafe. Setting this significantly
		// speeds up voluntary leader transitions as the new leader don't have to wait
		// LeaseDuration time first.
		//
		// In the default scaffold provided, the program ends immediately after
		// the manager stops, so would be fine to enable this option. However,
		// if you are doing or is intended to do any operation such as perform cleanups
		// after the manager stops then its usage might be unsafe.
		// LeaderElectionReleaseOnCancel: true,
	}
	if namespaces := parseWatchNamespaces(watchNamespaces); len(namespaces) > 0 {
		setupLog.Info("Restricting manager cache to specific namespaces", "namespaces", namespaces)
		mgrOptions.Cache = cache.Options{DefaultNamespaces: namespaceCacheConfigs(namespaces)}
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), mgrOptions)
	if err != nil {
		setupLog.Error(err, "Failed to start manager")
		os.Exit(1)
	}

	// The dashboard Deployment DashboardReconciler creates per Dashboard (D11 /
	// Phase 6.4) runs the same binary's `dashboard` subcommand, so it should
	// always be the exact image this manager process is itself running as —
	// there's no separately-pinned operand image to read from an env var
	// anymore. Kubernetes has no downward-API field for "my own container's
	// image", so this looks itself up via the API server instead, using the
	// POD_NAME/POD_NAMESPACE downward-API env vars set in
	// config/manager/manager.yaml. A direct (uncached) client is used since
	// this runs once before mgr.Start() brings up the cache.
	directClient, err := client.New(mgr.GetConfig(), client.Options{Scheme: scheme})
	if err != nil {
		setupLog.Error(err, "Failed to build direct (uncached) client")
		os.Exit(1)
	}

	dashboardImage, err := ownDashboardImage(context.Background(), directClient)
	if err != nil {
		setupLog.Error(err, "Failed to determine the manager's own image for the dashboard Deployment")
		os.Exit(1)
	}

	// Gateway API is an optional, separately-installed CRD set (unlike
	// Ingress, which every cluster has built in), so DashboardReconciler only
	// watches/manages HTTPRoute if it's actually present — see
	// gatewayAPIAvailable's doc comment.
	gatewayAPIEnabled, err := gatewayAPIAvailable(mgr.GetConfig())
	if err != nil {
		setupLog.Error(err, "Failed to check for Gateway API CRDs; continuing with Gateway API support disabled")
	}
	setupLog.Info("Gateway API support", "enabled", gatewayAPIEnabled)

	if err := (&controller.DashboardReconciler{
		Client:            mgr.GetClient(),
		Scheme:            mgr.GetScheme(),
		Recorder:          mgr.GetEventRecorder("dashboard-controller"),
		DashboardImage:    dashboardImage,
		GatewayAPIEnabled: gatewayAPIEnabled,
		DirectReader:      directClient,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "Failed to create controller", "controller", "dashboard")
		os.Exit(1)
	}
	if err := (&controller.DashboardStyleReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "Failed to create controller", "controller", "dashboardstyle")
		os.Exit(1)
	}
	if err := (&controller.ServiceCardReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "Failed to create controller", "controller", "servicecard")
		os.Exit(1)
	}
	if err := (&controller.BookmarkReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "Failed to create controller", "controller", "bookmark")
		os.Exit(1)
	}
	if err := (&controller.InfoWidgetReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "Failed to create controller", "controller", "infowidget")
		os.Exit(1)
	}
	// +kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "Failed to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "Failed to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("Starting manager", "version", version, "commit", commit)
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "Failed to run manager")
		os.Exit(1)
	}
}

// parseWatchNamespaces splits s (the --watch-namespaces flag value) on
// commas, trims whitespace, and drops empty entries — so "" (the default),
// ",", and " " all yield no namespaces (cluster-wide watch), matching the
// flag's documented default.
func parseWatchNamespaces(s string) []string {
	var namespaces []string
	for ns := range strings.SplitSeq(s, ",") {
		ns = strings.TrimSpace(ns)
		if ns != "" {
			namespaces = append(namespaces, ns)
		}
	}
	return namespaces
}

// namespaceCacheConfigs builds the cache.Options.DefaultNamespaces map
// ctrl.Options.Cache expects from parseWatchNamespaces' output — every
// listed namespace gets the zero-value cache.Config (no further per-
// namespace restriction, e.g. label/field selectors).
func namespaceCacheConfigs(namespaces []string) map[string]cache.Config {
	configs := make(map[string]cache.Config, len(namespaces))
	for _, ns := range namespaces {
		configs[ns] = cache.Config{}
	}
	return configs
}

// ownDashboardImage looks up the running manager Pod (named by the
// POD_NAME/POD_NAMESPACE downward-API env vars) and returns the image
// reference for DashboardReconciler to reuse when it builds each Dashboard's
// dashboard Deployment: c is expected to be a direct (uncached) client, used
// once before mgr.Start() brings up the cache.
//
// Prefers the container's running digest (status.containerStatuses[].imageID,
// e.g. "registry/repo@sha256:...") over its spec image (typically a mutable
// tag): on a multi-node cluster, a tag that's since been repointed or a
// locally-loaded image can otherwise cause dashboard pods to run different
// bits than the manager itself, even though both reference "the same"
// image string. Falls back to the spec image when the runtime hasn't
// populated a usable digest yet (e.g. a container reporting no imageID, or a
// locally-loaded kind/minikube image whose imageID isn't a resolvable
// pull reference) — nothing else to fall back to, and refusing to start the
// manager over it would make image-less-digest environments unusable.
func ownDashboardImage(ctx context.Context, c client.Reader) (string, error) {
	podName := os.Getenv("POD_NAME")
	podNamespace := os.Getenv("POD_NAMESPACE")
	if podName == "" || podNamespace == "" {
		return "", fmt.Errorf("POD_NAME and POD_NAMESPACE env vars must be set (via the downward API) " +
			"to determine the manager's own image")
	}

	pod := &corev1.Pod{}
	if err := c.Get(ctx, client.ObjectKey{Name: podName, Namespace: podNamespace}, pod); err != nil {
		return "", fmt.Errorf("getting own Pod %s/%s: %w", podNamespace, podName, err)
	}

	var specImage string
	for _, container := range pod.Spec.Containers {
		if container.Name == managerContainerName {
			specImage = container.Image
			break
		}
	}
	if specImage == "" {
		return "", fmt.Errorf("container %q not found in own Pod %s/%s", managerContainerName, podNamespace, podName)
	}

	if digestImage, ok := digestPinnedImage(specImage, pod.Status.ContainerStatuses); ok {
		return digestImage, nil
	}
	return specImage, nil
}

// digestPinnedImage returns specImage repointed at its running digest, taken
// from statuses' entry for managerContainerName's ImageID (e.g.
// "docker.io/library/registry@sha256:abcd..." or, on some runtimes, a bare
// "sha256:abcd..."). ok is false when no usable digest is available (no
// matching status yet, an empty ImageID, one that doesn't carry a
// "sha256:..." digest, or one whose repository doesn't match specImage's —
// see below) — see ownDashboardImage's doc comment for why that's an
// accepted fallback rather than an error.
//
// The repository match check matters because ImageID's repository isn't
// always specImage's: an image loaded locally without a real registry pull
// (e.g. `kind load docker-image`, common for self-built/self-hosted
// deployments and this project's own e2e tests) gets reported under a
// synthetic repository the runtime invents for the import, distinct from
// the repo the Deployment spec actually names. Pairing that digest with
// specImage's repo would construct a reference the runtime can't resolve
// (that exact repo@digest was never itself pulled/tagged), so such cases
// fall back to the plain tag reference instead.
func digestPinnedImage(specImage string, statuses []corev1.ContainerStatus) (image string, ok bool) {
	repo := imageRepo(specImage)
	for _, status := range statuses {
		if status.Name != managerContainerName {
			continue
		}
		idx := strings.Index(status.ImageID, "@sha256:")
		if idx == -1 {
			return "", false
		}
		if status.ImageID[:idx] != repo {
			return "", false
		}
		digest := status.ImageID[idx+1:]
		return repo + "@" + digest, true
	}
	return "", false
}

// imageRepo strips the tag or digest suffix from an image reference, e.g.
// "example.com/foo:v1" and "example.com/foo@sha256:abcd" both become
// "example.com/foo".
func imageRepo(image string) string {
	if at := strings.LastIndex(image, "@"); at != -1 {
		return image[:at]
	}
	if colon := strings.LastIndex(image, ":"); colon != -1 && !strings.Contains(image[colon:], "/") {
		return image[:colon]
	}
	return image
}

// gatewayAPIAvailable reports whether the cluster has Gateway API's HTTPRoute
// resource registered, via the discovery API (not the controller-runtime
// RESTMapper, since that's cache-backed and tied to mgr.Start() timing). A
// discovery error returns (false, err) rather than panicking: the caller
// logs it and falls back to "disabled" so an unrelated, possibly-transient
// discovery hiccup never blocks manager startup over an optional feature.
func gatewayAPIAvailable(cfg *rest.Config) (bool, error) {
	dc, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return false, fmt.Errorf("building discovery client: %w", err)
	}

	resources, err := dc.ServerResourcesForGroupVersion(gatewayv1.GroupVersion.String())
	if err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("listing %s resources: %w", gatewayv1.GroupVersion, err)
	}

	for _, res := range resources.APIResources {
		if res.Kind == "HTTPRoute" {
			return true, nil
		}
	}
	return false, nil
}

// runDashboard serves the native dashboard (D11 / Phase 6.0) for a single
// Dashboard: a per-Dashboard Deployment runs the same image with `dashboard`
// as its first argument instead of the manager's reconcile loop.
func runDashboard(args []string) {
	fs := flag.NewFlagSet("dashboard", flag.ExitOnError)
	var namespace, dashboardName, addr, metricsAddr string
	var pollInterval time.Duration
	fs.StringVar(&namespace, "namespace", "", "Namespace of the Dashboard to serve a dashboard for.")
	fs.StringVar(&dashboardName, "dashboard-name", "", "Name of the Dashboard to serve a dashboard for.")
	fs.StringVar(&addr, "addr", ":8080", "The address the dashboard HTTP server binds to.")
	metricsAddrUsage := "The address the /metrics endpoint binds to, kept separate from --addr " +
		"so Prometheus metrics aren't reachable through the Dashboard's public Ingress/Gateway."
	fs.StringVar(&metricsAddr, "metrics-addr", ":9090", metricsAddrUsage)
	fs.DurationVar(&pollInterval, "poll-interval", 15*time.Second, "How often to poll each widget's upstream.")
	opts := zap.Options{Development: true}
	opts.BindFlags(fs)
	_ = fs.Parse(args)

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	if namespace == "" || dashboardName == "" {
		setupLog.Error(nil, "--namespace and --dashboard-name are required")
		os.Exit(1)
	}

	setupLog.Info("Starting dashboard", "version", version, "commit", commit,
		"namespace", namespace, "dashboardName", dashboardName)

	restConfig := ctrl.GetConfigOrDie()
	gatewayAPIEnabled, err := gatewayAPIAvailable(restConfig)
	if err != nil {
		setupLog.Error(err, "Failed to check for Gateway API CRDs; continuing with HTTPRoute discovery disabled")
	}
	setupLog.Info("Gateway API support", "enabled", gatewayAPIEnabled)

	if err := dashboard.Run(ctrl.SetupSignalHandler(), dashboard.Options{
		RestConfig:        restConfig,
		Scheme:            scheme,
		Namespace:         namespace,
		DashboardName:     dashboardName,
		Addr:              addr,
		MetricsAddr:       metricsAddr,
		PollInterval:      pollInterval,
		Version:           version,
		Commit:            commit,
		GatewayAPIEnabled: gatewayAPIEnabled,
	}); err != nil {
		setupLog.Error(err, "Failed to run dashboard")
		os.Exit(1)
	}
}

// stringSliceFlag implements flag.Value to collect a repeatable -f flag
// (preview's manifest paths) into a slice, the same way `kubectl apply -f`
// accepts more than one path.
type stringSliceFlag []string

func (s *stringSliceFlag) String() string { return strings.Join(*s, ",") }

func (s *stringSliceFlag) Set(v string) error {
	*s = append(*s, v)
	return nil
}

// runPreview serves the dashboard against manifests loaded from local files
// instead of a live cluster, for previewing what a Dashboard renders as
// without installing the operator anywhere. See internal/preview for how
// -f's paths are turned into a Dashboard/DashboardStyle/ServiceCard/
// Bookmark/InfoWidget/Secret set and internal/dashboard.RunPreview for how
// that's served.
func runPreview(args []string) {
	fs := flag.NewFlagSet("preview", flag.ExitOnError)
	var paths stringSliceFlag
	var namespace, dashboardName, addr, metricsAddr string
	var pollInterval time.Duration
	var recursive bool
	fs.Var(&paths, "f", "Path to a YAML file or directory of manifests to preview (repeatable).")
	fs.BoolVar(&recursive, "recursive", false, "Recurse into directories passed via -f.")
	fs.StringVar(&namespace, "namespace", "",
		"Namespace of the Dashboard to preview; only needed if -f contains more than one Dashboard.")
	fs.StringVar(&dashboardName, "dashboard-name", "",
		"Name of the Dashboard to preview; only needed if -f contains more than one Dashboard.")
	fs.StringVar(&addr, "addr", "127.0.0.1:8080", "The address the preview HTTP server binds to.")
	fs.StringVar(&metricsAddr, "metrics-addr", "127.0.0.1:0", "The address the /metrics endpoint binds to.")
	fs.DurationVar(&pollInterval, "poll-interval", 15*time.Second, "How often to poll each widget's upstream.")
	opts := zap.Options{Development: true}
	opts.BindFlags(fs)
	_ = fs.Parse(args)

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	if len(paths) == 0 {
		setupLog.Error(nil, "-f is required (path to a YAML file or directory of manifests)")
		os.Exit(1)
	}

	result, err := preview.Load(preview.Config{
		Scheme:        scheme,
		Paths:         paths,
		Recursive:     recursive,
		Namespace:     namespace,
		DashboardName: dashboardName,
	})
	if err != nil {
		setupLog.Error(err, "Failed to load preview manifests", "paths", []string(paths))
		os.Exit(1)
	}

	setupLog.Info("Starting preview", "version", version, "commit", commit,
		"namespace", result.Namespace, "dashboardName", result.DashboardName, "addr", addr)

	if err := dashboard.RunPreview(ctrl.SetupSignalHandler(), dashboard.PreviewOptions{
		Reader:        result.Reader,
		Namespace:     result.Namespace,
		DashboardName: result.DashboardName,
		Addr:          addr,
		MetricsAddr:   metricsAddr,
		PollInterval:  pollInterval,
		Version:       version,
		Commit:        commit,
	}); err != nil {
		setupLog.Error(err, "Failed to run preview")
		os.Exit(1)
	}
}
