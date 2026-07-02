package dashboard

import (
	"context"
	"errors"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Shared fixtures for the per-widget table-driven Poll tests: every widget
// test exercises the same "upstream returns a non-2xx" and "upstream is
// unreachable" cases, so the literals are pulled out once here instead of
// being retyped (and flagged by goconst) in each _test.go file.
const (
	testCaseNon200      = "non-200"
	testHTTP500         = "HTTP 500"
	testHTTP401         = "HTTP 401"
	testUnreachableAddr = "http://127.0.0.1:1"

	// Shared literals across the look/monitor/header tests.
	testCfgName       = "cfg"
	testSvcName       = "svc"
	testHeaderWeather = "weather"
	testStatusBasic   = "basic"

	// Shared literals across poller/server/site fixtures.
	testSecretName     = "creds"
	testSvcDisplayName = "Svc"
	testPodSvcName     = "podsvc"
	testAppLabelKey    = "app"
	testAppLabelValue  = "demo"
	testOpenMeteoType  = "openmeteo"
	testClockName      = "clock"
	testInfraGroup     = "Infra"

	// Shared literals across customapi/discovery fixtures.
	testLabelFirst        = "First"
	testLabelDisk         = "disk"
	testDiscoveryGroup    = "Apps"
	testDiscoveredAppName = "App"
	testCustomName        = "Custom"
	testNameOther         = "other"
	testValueAlpha        = "alpha"
	testExampleURL        = "http://example.invalid"

	// testDiscoveryEnabledAnnotation is the full annotation key an Ingress
	// carries to opt into discovery.go's discovery, built from the same
	// constants the production code uses so it can't drift from them.
	testDiscoveryEnabledAnnotation = defaultDiscoveryPrefix + discoveryAnnEnabled

	// Shared literals across openmeteo/openweathermap tests.
	testCoordsConfig = `{"latitude":1,"longitude":1}`
	testAPIKey       = "abc123"

	// Shared literals across iframe widget/render tests.
	testIframeURL    = "https://grafana.example.com/d/abc"
	testIframeHeight = "40vh"

	// testJSSchemeURL is a non-http(s) URL used to exercise scheme rejection
	// (iframe widget URLs, Favicon/link hrefs, isHTTPURL itself).
	testJSSchemeURL = "javascript:alert(1)"

	// testFragmentCardKey is a Store card key shared by the /fragment
	// ETag/ revalidation and ordering fixtures.
	testFragmentCardKey = "ns/prom/0"

	// Shared literal across bookmark fixtures.
	testBookmarkHrefA = "https://example.invalid/a"

	// Shared literals across header-widget alignment/kubemetrics fixtures.
	testKubeMetricsType = "kubemetrics"
	testCPUName         = "cpu"
	testGreetName       = "greet"

	// Shared literals across Ingress/HTTPRoute discovery fixtures.
	testDiscoveredAppKey        = "app"
	testDiscoveredBareKey       = "bare"
	testDiscoverySkipKey        = "skip"
	testMyAppDisplayName        = "My App"
	testAnAppDescription        = "An app"
	testGrafanaIconSlug         = "grafana"
	testAppExampleHost          = "app.example.invalid"
	testKubepageNameAnnotation  = "kubepage.io/name"
	testDiscoveredAppCardName   = "Discovered App"
	testDiscoveredRouteCardName = "Discovered Route"
)

// errBoom is returned by errInjectingReader when a predicate matches.
var errBoom = errors.New("boom")

// errInjectingReader wraps a client.Reader, forcing chosen List/Get calls to
// fail — used to exercise the error-handling branches of code paths that
// would otherwise never see a List/Get error against the fake client (e.g.
// LoadSite's or Poller's own informer reads).
type errInjectingReader struct {
	client.Reader
	failList func(list client.ObjectList) bool
	failGet  func(key client.ObjectKey, obj client.Object) bool
}

func (r errInjectingReader) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	if r.failList != nil && r.failList(list) {
		return errBoom
	}
	return r.Reader.List(ctx, list, opts...)
}

func (r errInjectingReader) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if r.failGet != nil && r.failGet(key, obj) {
		return errBoom
	}
	return r.Reader.Get(ctx, key, obj, opts...)
}
