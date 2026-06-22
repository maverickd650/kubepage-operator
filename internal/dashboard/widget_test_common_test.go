package dashboard

// Shared fixtures for the per-widget table-driven Poll tests: every widget
// test exercises the same "upstream returns a non-2xx" and "upstream is
// unreachable" cases, so the literals are pulled out once here instead of
// being retyped (and flagged by goconst) in each _test.go file.
const (
	testCaseNon200      = "non-200"
	testHTTP500         = "HTTP 500"
	testHTTP401         = "HTTP 401"
	testUnreachableAddr = "http://127.0.0.1:1"
)
