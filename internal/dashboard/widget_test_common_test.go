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
	testTargetSelf     = "_self"
	testInfraGroup     = "Infra"
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
