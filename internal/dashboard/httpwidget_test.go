package dashboard

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

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
