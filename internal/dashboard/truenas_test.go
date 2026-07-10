package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

const truenasTestToken = "nastok"

// truenasMockServer stands up a JSON-RPC 2.0 WebSocket server answering
// auth.login_with_api_key and system.info exactly like truenas.go's Poll
// expects: one request per method, in order. loginResult controls whether
// the mock reports a successful login; systemInfoResult is written verbatim
// as system.info's JSON-RPC result (skipped if loginResult is false, since a
// real TrueNAS wouldn't accept further calls on a failed login either).
func truenasMockServer(t *testing.T, loginResult bool, systemInfoResult string) (*httptest.Server, *string) {
	t.Helper()
	gotToken := new(string)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()
		ctx := r.Context()

		var loginReq jsonrpcRequest
		if err := wsjson.Read(ctx, conn, &loginReq); err != nil {
			return
		}
		if len(loginReq.Params) > 0 {
			if s, ok := loginReq.Params[0].(string); ok {
				*gotToken = s
			}
		}
		loginResultJSON, _ := json.Marshal(loginResult)
		if err := wsjson.Write(ctx, conn, jsonrpcResponse{ID: loginReq.ID, Result: loginResultJSON}); err != nil {
			return
		}
		if !loginResult {
			return
		}

		var infoReq jsonrpcRequest
		if err := wsjson.Read(ctx, conn, &infoReq); err != nil {
			return
		}
		_ = wsjson.Write(ctx, conn, jsonrpcResponse{ID: infoReq.ID, Result: json.RawMessage(systemInfoResult)})

		_ = conn.Close(websocket.StatusNormalClosure, "")
	}))
	return srv, gotToken
}

func TestTruenasWidgetPoll(t *testing.T) {
	tests := map[string]struct {
		systemInfoResult string
		wantVersion      string
		wantUptime       string
	}{
		"multi-day uptime": {
			systemInfoResult: `{"version":"TrueNAS-SCALE-23.10.1","uptime_seconds":266461}`,
			wantVersion:      "TrueNAS-SCALE-23.10.1",
			wantUptime:       "3d 2h",
		},
		"sub-day uptime": {
			systemInfoResult: `{"version":"TrueNAS-SCALE-23.10.1","uptime_seconds":5400}`,
			wantVersion:      "TrueNAS-SCALE-23.10.1",
			wantUptime:       "1h 30m",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			srv, gotToken := truenasMockServer(t, true, tc.systemInfoResult)
			defer srv.Close()

			got, err := (truenasWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{
				URL:     srv.URL,
				Secrets: map[string]string{testSecretField: truenasTestToken},
			})
			if err != nil {
				t.Fatalf("Poll() unexpected error: %v", err)
			}
			want := []Field{
				{Label: labelVersion, Value: tc.wantVersion},
				{Label: labelUptime, Value: tc.wantUptime},
			}
			if !reflect.DeepEqual(want, got) {
				t.Errorf("Poll() = %+v, want %+v", got, want)
			}
			if *gotToken != truenasTestToken {
				t.Errorf("auth.login_with_api_key param = %q, want %q", *gotToken, truenasTestToken)
			}
		})
	}
}

func TestTruenasWidgetPollAuthFailure(t *testing.T) {
	srv, _ := truenasMockServer(t, false, "")
	defer srv.Close()

	got, err := (truenasWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{
		URL:     srv.URL,
		Secrets: map[string]string{testSecretField: "wrong"},
	})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{{Label: labelStatus, Value: statusUnreach}}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}

// truenasMockServerSystemInfoError logs in successfully but answers
// system.info with a JSON-RPC error object, exercising truenasCall's
// resp.Error != nil branch.
func truenasMockServerSystemInfoError(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()
		ctx := r.Context()

		var loginReq jsonrpcRequest
		if err := wsjson.Read(ctx, conn, &loginReq); err != nil {
			return
		}
		loginOK, _ := json.Marshal(true)
		if err := wsjson.Write(ctx, conn, jsonrpcResponse{ID: loginReq.ID, Result: loginOK}); err != nil {
			return
		}
		var infoReq jsonrpcRequest
		if err := wsjson.Read(ctx, conn, &infoReq); err != nil {
			return
		}
		_ = wsjson.Write(ctx, conn, jsonrpcResponse{
			ID:    infoReq.ID,
			Error: &jsonrpcError{Code: -32000, Message: "boom"},
		})
	}))
}

func TestTruenasWidgetPollSystemInfoError(t *testing.T) {
	srv := truenasMockServerSystemInfoError(t)
	defer srv.Close()

	got, err := (truenasWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{
		URL:     srv.URL,
		Secrets: map[string]string{testSecretField: truenasTestToken},
	})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{{Label: labelStatus, Value: statusUnreach}}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}

func TestTruenasWidgetPollMalformedResult(t *testing.T) {
	// system.info returns a JSON string where Poll expects an object, so
	// truenasCall's json.Unmarshal into the result struct fails.
	srv, _ := truenasMockServer(t, true, `"not-an-object"`)
	defer srv.Close()

	got, err := (truenasWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{
		URL:     srv.URL,
		Secrets: map[string]string{testSecretField: truenasTestToken},
	})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{{Label: labelStatus, Value: statusUnreach}}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}

func TestTruenasWidgetPollMissingURL(t *testing.T) {
	if _, err := (truenasWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{}); err == nil {
		t.Fatal("Poll() expected error for missing URL, got nil")
	}
}

func TestTruenasWidgetPollUnreachable(t *testing.T) {
	got, err := (truenasWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{URL: testUnreachableAddr})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{{Label: labelStatus, Value: statusUnreach}}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}

func TestTruenasWidgetPollNonWebSocketServer(t *testing.T) {
	// A plain HTTP server that never upgrades the connection: the dial
	// itself should fail cleanly rather than hang or panic.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	got, err := (truenasWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{
		URL:     srv.URL,
		Secrets: map[string]string{testSecretField: truenasTestToken},
	})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{{Label: labelStatus, Value: statusUnreach}}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}

func TestTruenasWebSocketURL(t *testing.T) {
	tests := map[string]string{
		"http://truenas.local":       "ws://truenas.local/api/current",
		"https://truenas.local":      "wss://truenas.local/api/current",
		"https://truenas.local:444/": "wss://truenas.local:444/api/current",
	}
	for in, want := range tests {
		got, err := truenasWebSocketURL(in)
		if err != nil {
			t.Fatalf("truenasWebSocketURL(%q) unexpected error: %v", in, err)
		}
		if got != want {
			t.Errorf("truenasWebSocketURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTruenasWebSocketURLInvalid(t *testing.T) {
	if _, err := truenasWebSocketURL("http://[::1"); err == nil {
		t.Fatal("truenasWebSocketURL() expected error for malformed URL, got nil")
	}
}

func TestTruenasWidgetSample(t *testing.T) {
	got := (truenasWidget{}).Sample(WidgetConfig{})
	if len(got) != 2 || got[0].Label != labelVersion || got[1].Label != labelUptime {
		t.Errorf("Sample() = %+v, want Version/Uptime fields", got)
	}
	assertSampleDeterministic(t, truenasWidget{})
}
