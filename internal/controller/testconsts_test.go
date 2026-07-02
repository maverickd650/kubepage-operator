package controller

// Shared literals reused across multiple *_test.go files in this package
// (goconst flags duplicate string literals package-wide, not just per-file).
const (
	testDoesNotExistInstanceName = "does-not-exist"
	testDashboardHost            = "dashboard.example.com"
	testOtherHost                = "other.example.com"
	testInstanceObjName          = "main"
	testRefInstanceName          = "inst"
	testAnnotationKey            = "example.com/note"
	testWidgetTypePrometheus     = "prometheus"
	testWidgetTypeGrafana        = "grafana"
	testWidgetTypeOpenMeteo      = "openmeteo"
	testInfoWidgetNameMetrics    = "metrics"
	policyTestGroup              = "media"
	testForeignAnnotationKey     = "foo.io/bar"
	testForeignAnnotationValue   = "baz"
	testWidgetTypeDatetime       = "datetime"
	testOtherInstanceName        = "some-other-instance"
	testPortNameHTTP             = "http"
	testPortNameHTTPS            = "https"
	testSecretRefName            = "api-secret"
	testServiceEntryObjName      = "svc"
	testValueTrue                = "true"

	// Shared table-test case names across the equal*Ptr nil-handling tests
	// in instance_network_test.go.
	testCaseBothNil         = "both nil"
	testCaseOneNil          = "one nil"
	testCaseEqualValues     = "equal values"
	testCaseDifferentValues = "different values"
)
