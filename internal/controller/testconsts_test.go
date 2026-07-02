package controller

// Shared literals reused across multiple *_test.go files in this package
// (goconst flags duplicate string literals package-wide, not just per-file).
const (
	testDoesNotExistDashboardName = "does-not-exist"
	testDashboardHost             = "dashboard.example.com"
	testOtherHost                 = "other.example.com"
	testDashboardObjName          = "main"
	testRefDashboardName          = "inst"
	testAnnotationKey             = "example.com/note"
	testWidgetTypePrometheus      = "prometheus"
	testWidgetTypeGrafana         = "grafana"
	testWidgetTypeOpenMeteo       = "openmeteo"
	testInfoWidgetNameMetrics     = "metrics"
	policyTestGroup               = "media"
	testForeignAnnotationKey      = "foo.io/bar"
	testForeignAnnotationValue    = "baz"
	testWidgetTypeDatetime        = "datetime"
	testOtherDashboardName        = "some-other-instance"
	testPortNameHTTP              = "http"
	testPortNameHTTPS             = "https"
	testSecretRefName             = "api-secret"
	testServiceCardObjName        = "svc"
	testValueTrue                 = "true"

	// Shared table-test case names across the equal*Ptr nil-handling tests
	// in instance_network_test.go.
	testCaseBothNil         = "both nil"
	testCaseOneNil          = "one nil"
	testCaseEqualValues     = "equal values"
	testCaseDifferentValues = "different values"
)
