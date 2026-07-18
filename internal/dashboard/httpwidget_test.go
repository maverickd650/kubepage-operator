package dashboard

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBuildJSONRequestMissingURL(t *testing.T) {
	_, err := buildJSONRequest(t.Context(), WidgetConfig{}, "widgettype", "/path")
	if err == nil {
		t.Fatal("buildJSONRequest() expected error for missing url, got nil")
	}
	if got, want := err.Error(), "widgettype widget: url is required"; got != want {
		t.Errorf("buildJSONRequest() error = %q, want %q", got, want)
	}
}

// TestBuildJSONRequestBuildError covers the http.NewRequestWithContext
// error path directly: every fetchJSON/fetchJSONBasicAuth/grafanaRequest
// call site forwards this error unchanged, but none of the per-widget
// table tests can trigger it (a widget's cfg.URL is CRD-validated), so it's
// otherwise unreachable from any widget's own tests.
func TestBuildJSONRequestBuildError(t *testing.T) {
	_, err := buildJSONRequest(t.Context(), WidgetConfig{URL: testExampleURL}, "widgettype", "/\x7f")
	if err == nil {
		t.Fatal("buildJSONRequest() expected error for an invalid request URL, got nil")
	}
	if !strings.Contains(err.Error(), "building request") {
		t.Errorf("buildJSONRequest() error = %q, want it to mention %q", err.Error(), "building request")
	}
}

// TestDoJSONRequestMalformedBody exercises doJSONRequest's decode-error path
// directly: a 200 response whose body isn't valid JSON. Every widget's Poll
// calls through this shared helper, but none of the per-widget table tests
// feeds a genuinely malformed body, so this path has only ever been hit
// incidentally (e.g. via a truncated/empty body), not on purpose — a
// regression here (e.g. swallowing the decode error instead of wrapping it)
// would silently affect every widget at once.
func TestDoJSONRequestMalformedBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{not valid json`))
	}))
	defer srv.Close()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("building request: %v", err)
	}

	var out struct {
		Field string `json:"field"`
	}
	fields, gotErr := doJSONRequest(srv.Client(), req, &out)

	if fields != nil {
		t.Errorf("doJSONRequest() fields = %+v, want nil on decode error", fields)
	}
	if gotErr == nil {
		t.Fatal("doJSONRequest() error = nil, want a decode error")
	}
	if !strings.Contains(gotErr.Error(), "decoding response") {
		t.Errorf("doJSONRequest() error = %q, want it to mention %q", gotErr.Error(), "decoding response")
	}
	var syntaxErr *json.SyntaxError
	if !errors.As(gotErr, &syntaxErr) {
		t.Errorf("doJSONRequest() error = %v, want it to wrap a *json.SyntaxError", gotErr)
	}
}
