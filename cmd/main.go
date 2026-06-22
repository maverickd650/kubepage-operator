package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"os"
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
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(pagev1alpha1.AddToScheme(scheme))
	// Registered unconditionally so the client can decode HTTPRoute objects
	// when Gateway API is available; gatewayAPIAvailable (checked at startup)
	// decides whether InstanceReconciler ever watches or creates one.
	utilruntime.Must(gatewayv1.Install(scheme))
	// Registered so the dashboard's cluster client can decode NodeMetrics for
	// the kubemetrics InfoWidget (internal/dashboard/kubemetrics.go).
	utilruntime.Must(metricsv1beta1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "dashboard" {
		runDashboard(os.Args[2:])
		return
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
	var tlsOpts []func(*tls.Config)
	flag.StringVar(&metricsAddr, "metrics-bind-address", "0", "The address the metrics endpoint binds to. "+
		"Use :8443 for HTTPS or :8080 for HTTP, or leave as 0 to disable the metrics service.")
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

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
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
	})
	if err != nil {
		setupLog.Error(err, "Failed to start manager")
		os.Exit(1)
	}

	// The dashboard Deployment InstanceReconciler creates per Instance (D11 /
	// Phase 6.4) runs the same binary's `dashboard` subcommand, so it should
	// always be the exact image this manager process is itself running as —
	// there's no separately-pinned operand image to read from an env var
	// anymore. Kubernetes has no downward-API field for "my own container's
	// image", so this looks itself up via the API server instead, using the
	// POD_NAME/POD_NAMESPACE downward-API env vars set in
	// config/manager/manager.yaml. A direct (uncached) client is used since
	// this runs once before mgr.Start() brings up the cache.
	dashboardImage, err := ownDashboardImage(context.Background(), mgr.GetConfig(), scheme)
	if err != nil {
		setupLog.Error(err, "Failed to determine the manager's own image for the dashboard Deployment")
		os.Exit(1)
	}

	// Gateway API is an optional, separately-installed CRD set (unlike
	// Ingress, which every cluster has built in), so InstanceReconciler only
	// watches/manages HTTPRoute if it's actually present — see
	// gatewayAPIAvailable's doc comment.
	gatewayAPIEnabled, err := gatewayAPIAvailable(mgr.GetConfig())
	if err != nil {
		setupLog.Error(err, "Failed to check for Gateway API CRDs; continuing with Gateway API support disabled")
	}
	setupLog.Info("Gateway API support", "enabled", gatewayAPIEnabled)

	if err := (&controller.InstanceReconciler{
		Client:            mgr.GetClient(),
		Scheme:            mgr.GetScheme(),
		Recorder:          mgr.GetEventRecorder("instance-controller"),
		DashboardImage:    dashboardImage,
		GatewayAPIEnabled: gatewayAPIEnabled,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "Failed to create controller", "controller", "instance")
		os.Exit(1)
	}
	if err := (&controller.ConfigurationReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "Failed to create controller", "controller", "configuration")
		os.Exit(1)
	}
	if err := (&controller.ServiceEntryReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "Failed to create controller", "controller", "serviceentry")
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

	setupLog.Info("Starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "Failed to run manager")
		os.Exit(1)
	}
}

// ownDashboardImage looks up the running manager Pod (named by the
// POD_NAME/POD_NAMESPACE downward-API env vars) and returns its
// managerContainerName container's image, for InstanceReconciler to reuse
// when it builds each Instance's dashboard Deployment.
func ownDashboardImage(ctx context.Context, cfg *rest.Config, scheme *runtime.Scheme) (string, error) {
	podName := os.Getenv("POD_NAME")
	podNamespace := os.Getenv("POD_NAMESPACE")
	if podName == "" || podNamespace == "" {
		return "", fmt.Errorf("POD_NAME and POD_NAMESPACE env vars must be set (via the downward API) " +
			"to determine the manager's own image")
	}

	c, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		return "", fmt.Errorf("building direct client: %w", err)
	}

	pod := &corev1.Pod{}
	if err := c.Get(ctx, client.ObjectKey{Name: podName, Namespace: podNamespace}, pod); err != nil {
		return "", fmt.Errorf("getting own Pod %s/%s: %w", podNamespace, podName, err)
	}

	for _, container := range pod.Spec.Containers {
		if container.Name == managerContainerName {
			return container.Image, nil
		}
	}
	return "", fmt.Errorf("container %q not found in own Pod %s/%s", managerContainerName, podNamespace, podName)
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
// Instance: a per-Instance Deployment runs the same image with `dashboard`
// as its first argument instead of the manager's reconcile loop.
func runDashboard(args []string) {
	fs := flag.NewFlagSet("dashboard", flag.ExitOnError)
	var namespace, instanceName, addr string
	var pollInterval time.Duration
	fs.StringVar(&namespace, "namespace", "", "Namespace of the Instance to serve a dashboard for.")
	fs.StringVar(&instanceName, "instance-name", "", "Name of the Instance to serve a dashboard for.")
	fs.StringVar(&addr, "addr", ":8080", "The address the dashboard HTTP server binds to.")
	fs.DurationVar(&pollInterval, "poll-interval", 15*time.Second, "How often to poll each widget's upstream.")
	opts := zap.Options{Development: true}
	opts.BindFlags(fs)
	_ = fs.Parse(args)

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	if namespace == "" || instanceName == "" {
		setupLog.Error(nil, "--namespace and --instance-name are required")
		os.Exit(1)
	}

	if err := dashboard.Run(ctrl.SetupSignalHandler(), dashboard.Options{
		RestConfig:   ctrl.GetConfigOrDie(),
		Scheme:       scheme,
		Namespace:    namespace,
		InstanceName: instanceName,
		Addr:         addr,
		PollInterval: pollInterval,
	}); err != nil {
		setupLog.Error(err, "Failed to run dashboard")
		os.Exit(1)
	}
}
