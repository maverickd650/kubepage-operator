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
// auth.login_with_api_key, system.info, and alert.list exactly like
// truenas.go's Poll expects: one request per method, in order. loginResult
// controls whether the mock reports a successful login; systemInfoResult and
// alertListResult are written verbatim as each call's JSON-RPC result
// (skipped if loginResult is false, since a real TrueNAS wouldn't accept
// further calls on a failed login either).
func truenasMockServer(t *testing.T, loginResult bool, systemInfoResult, alertListResult string) (*httptest.Server, *string) {
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
		if err := wsjson.Write(ctx, conn, jsonrpcResponse{ID: infoReq.ID, Result: json.RawMessage(systemInfoResult)}); err != nil {
			return
		}

		var alertReq jsonrpcRequest
		if err := wsjson.Read(ctx, conn, &alertReq); err != nil {
			return
		}
		_ = wsjson.Write(ctx, conn, jsonrpcResponse{ID: alertReq.ID, Result: json.RawMessage(alertListResult)})

		_ = conn.Close(websocket.StatusNormalClosure, "")
	}))
	return srv, gotToken
}

func TestTruenasWidgetPoll(t *testing.T) {
	tests := map[string]struct {
		systemInfoResult string
		alertListResult  string
		wantLoad         string
		wantUptime       string
		wantAlerts       string
	}{
		"multi-day uptime, no alerts": {
			systemInfoResult: `{"uptime_seconds":266461,"loadavg":[1.23,0.98,0.75]}`,
			alertListResult:  `[]`,
			wantLoad:         "1.23",
			wantUptime:       "3d 2h",
			wantAlerts:       "0",
		},
		"sub-day uptime, mixed alerts": {
			systemInfoResult: `{"uptime_seconds":5400,"loadavg":[0.5,0.4,0.3]}`,
			alertListResult:  `[{"dismissed":false},{"dismissed":true},{"dismissed":false}]`,
			wantLoad:         "0.50",
			wantUptime:       "1h 30m",
			wantAlerts:       "2",
		},
		"missing loadavg": {
			systemInfoResult: `{"uptime_seconds":100,"loadavg":[]}`,
			alertListResult:  `[]`,
			wantLoad:         "0.00",
			wantUptime:       "0h 1m",
			wantAlerts:       "0",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			srv, gotToken := truenasMockServer(t, true, tc.systemInfoResult, tc.alertListResult)
			defer srv.Close()

			got, err := (truenasWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{
				URL:     srv.URL,
				Secrets: map[string]string{testSecretField: truenasTestToken},
			})
			if err != nil {
				t.Fatalf("Poll() unexpected error: %v", err)
			}
			want := []Field{
				{Label: labelLoad, Value: tc.wantLoad},
				{Label: labelUptime, Value: tc.wantUptime},
				{Label: labelAlerts, Value: tc.wantAlerts},
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
	srv, _ := truenasMockServer(t, false, "", "")
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

// truenasMockServerAlertListError logs in and answers system.info
// successfully, but answers alert.list with a JSON-RPC error object.
func truenasMockServerAlertListError(t *testing.T) *httptest.Server {
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
		if err := wsjson.Write(ctx, conn, jsonrpcResponse{ID: infoReq.ID, Result: json.RawMessage(`{"uptime_seconds":100,"loadavg":[1,1,1]}`)}); err != nil {
			return
		}
		var alertReq jsonrpcRequest
		if err := wsjson.Read(ctx, conn, &alertReq); err != nil {
			return
		}
		_ = wsjson.Write(ctx, conn, jsonrpcResponse{
			ID:    alertReq.ID,
			Error: &jsonrpcError{Code: -32000, Message: "boom"},
		})
	}))
}

func TestTruenasWidgetPollAlertListError(t *testing.T) {
	srv := truenasMockServerAlertListError(t)
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
	srv, _ := truenasMockServer(t, true, `"not-an-object"`, `[]`)
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
	if len(got) != 3 || got[0].Label != labelLoad || got[1].Label != labelUptime || got[2].Label != labelAlerts {
		t.Errorf("Sample() = %+v, want Load/Uptime/Alerts fields", got)
	}
	assertSampleDeterministic(t, truenasWidget{})
}
