package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func init() {
	Register("truenas", &truenasWidget{})
}

// truenasWidget polls TrueNAS over its JSON-RPC 2.0 WebSocket API
// (wss://<host>/api/current, or ws:// when cfg.URL is http://) instead of
// the REST API (/api/v2.0/...) this widget used before: TrueNAS SCALE 25.04+
// introduced the JSON-RPC/WebSocket API as the go-forward interface and is
// deprecating v2.0 REST, so new installs increasingly don't serve it at
// all. Secrets["token"] is a TrueNAS API key, sent as the sole argument to
// the "auth.login_with_api_key" JSON-RPC method — there is no header-based
// auth over this transport, unlike every REST-based widget in this package.
// "system.info" supplies Load/Uptime, matching gethomepage/homepage's
// default truenas fields; "alert.list" then supplies the count of
// undismissed alerts.
type truenasWidget struct{}

// truenasMaxFrameBytes caps a single WebSocket message the same way
// maxWidgetResponseBytes caps a REST widget's response body — system.info's
// result is small, but an upstream (or a compromised/misbehaving one)
// shouldn't be able to push unbounded data into memory per poll.
const truenasMaxFrameBytes = maxWidgetResponseBytes

const (
	labelLoad   = "Load"
	labelAlerts = "Alerts"
)

// truenasDefaultTimeout is used for the WebSocket dial+call round trip when
// the shared httpClient carries no timeout of its own (Timeout == 0, e.g. an
// http.Client{} built for a test) — a truenas Poll must still not hang
// forever.
const truenasDefaultTimeout = 10 * time.Second

type jsonrpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  []any  `json:"params"`
	ID      int    `json:"id"`
}

type jsonrpcResponse struct {
	ID     int             `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *jsonrpcError   `json:"error"`
}

type jsonrpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type truenasSystemInfoResult struct {
	UptimeSeconds int64     `json:"uptime_seconds"`
	LoadAvg       []float64 `json:"loadavg"`
}

// truenasAlert is the subset of alert.list's per-alert response fields this
// widget needs: whether the alert has been dismissed.
type truenasAlert struct {
	Dismissed bool `json:"dismissed"`
}

func (truenasWidget) Poll(ctx context.Context, httpClient *http.Client, cfg WidgetConfig) ([]Field, error) {
	if cfg.URL == "" {
		return nil, errors.New("truenas widget: url is required")
	}

	wsURL, err := truenasWebSocketURL(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("building websocket url: %w", err)
	}

	timeout := httpClient.Timeout
	if timeout <= 0 {
		timeout = truenasDefaultTimeout
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	conn, _, err := websocket.Dial(callCtx, wsURL, &websocket.DialOptions{HTTPClient: httpClient})
	if err != nil {
		logPollFailure(wsURL, err, "truenas websocket dial failed", "url", wsURL)
		return []Field{{Label: labelStatus, Value: statusUnreach}}, nil
	}
	defer func() { _ = conn.CloseNow() }()
	conn.SetReadLimit(truenasMaxFrameBytes)

	var loggedIn bool
	if err := truenasCall(callCtx, conn, "auth.login_with_api_key", []any{cfg.Secrets["token"]}, 1, &loggedIn); err != nil {
		logPollFailure(wsURL, err, "truenas login call failed", "url", wsURL)
		return []Field{{Label: labelStatus, Value: statusUnreach}}, nil
	}
	if !loggedIn {
		// TrueNAS answers a rejected API key with a plain `"result": false`
		// and no JSON-RPC error object, so there is no reason to report
		// beyond "the stored token is not accepted" — which is still far
		// more actionable than the "Unreachable" this used to render, since
		// it rules out DNS/TLS/connectivity entirely.
		logPollFailure(wsURL, nil, "truenas rejected the API key: check secrets.token holds the full key", "url", wsURL)
		return []Field{{Label: labelStatus, Value: statusUnauth}}, nil
	}

	var info truenasSystemInfoResult
	if err := truenasCall(callCtx, conn, "system.info", []any{}, 2, &info); err != nil {
		logPollFailure(wsURL, err, "truenas system.info call failed", "url", wsURL)
		return []Field{{Label: labelStatus, Value: statusUnreach}}, nil
	}

	var alerts []truenasAlert
	if err := truenasCall(callCtx, conn, "alert.list", []any{}, 3, &alerts); err != nil {
		logPollFailure(wsURL, err, "truenas alert.list call failed", "url", wsURL)
		return []Field{{Label: labelStatus, Value: statusUnreach}}, nil
	}
	clearPollFailure(wsURL)

	_ = conn.Close(websocket.StatusNormalClosure, "")

	load := "0.00"
	if len(info.LoadAvg) > 0 {
		load = fmt.Sprintf("%.2f", info.LoadAvg[0])
	}

	active := 0
	for _, a := range alerts {
		if !a.Dismissed {
			active++
		}
	}

	return []Field{
		{Label: labelLoad, Value: load},
		{Label: labelUptime, Value: formatUptime(info.UptimeSeconds)},
		{Label: labelAlerts, Value: fmt.Sprintf("%d", active)},
	}, nil
}

func (truenasWidget) Sample(WidgetConfig) []Field {
	return []Field{
		{Label: labelLoad, Value: "0.42"},
		{Label: labelUptime, Value: formatUptime(370000)},
		{Label: labelAlerts, Value: "0"},
	}
}

// truenasWebSocketURL derives the JSON-RPC WebSocket URL from cfg.URL's
// http(s) scheme (ws for http, wss for https), mounted at TrueNAS's
// "/api/current" path.
func truenasWebSocketURL(rawURL string) (string, error) {
	u, err := url.Parse(strings.TrimRight(rawURL, "/"))
	if err != nil {
		return "", fmt.Errorf("parsing url: %w", err)
	}
	switch u.Scheme {
	case schemeHTTPS:
		u.Scheme = "wss"
	case schemeHTTP:
		u.Scheme = "ws"
	}
	u.Path = "/api/current"
	u.RawQuery = ""
	return u.String(), nil
}

// truenasMaxSkippedFrames bounds how many non-matching frames truenasCall
// will discard while waiting for its own response (see its doc comment). A
// server that only ever sends frames this call isn't waiting for would
// otherwise spin here until callCtx's deadline; failing after a small fixed
// number turns that into a prompt, reported error instead.
const truenasMaxSkippedFrames = 8

// truenasCall sends one JSON-RPC 2.0 request over conn and decodes its
// result into out (skipped if out is nil, e.g. auth.login_with_api_key's
// boolean result is decoded by the caller directly). Returns an error on a
// transport failure, a malformed response, or a JSON-RPC-level error object.
//
// Frames arriving with a different id (or none — JSON-RPC notifications carry
// no id) are discarded rather than decoded as this call's response: the
// middleware is free to push a message at any point, and treating the first
// frame off the wire as the answer would misread it and then leave every
// subsequent call reading one response behind.
func truenasCall(ctx context.Context, conn *websocket.Conn, method string, params []any, id int, out any) error {
	req := jsonrpcRequest{JSONRPC: "2.0", Method: method, Params: params, ID: id}
	if err := wsjson.Write(ctx, conn, req); err != nil {
		return fmt.Errorf("writing %s request: %w", method, err)
	}

	var resp jsonrpcResponse
	for skipped := 0; ; skipped++ {
		if skipped > truenasMaxSkippedFrames {
			return fmt.Errorf("reading %s response: no reply after %d unrelated frames", method, truenasMaxSkippedFrames)
		}
		resp = jsonrpcResponse{}
		if err := wsjson.Read(ctx, conn, &resp); err != nil {
			return fmt.Errorf("reading %s response: %w", method, err)
		}
		if resp.ID == id {
			break
		}
	}
	if resp.Error != nil {
		return fmt.Errorf("%s: jsonrpc error %d: %s", method, resp.Error.Code, resp.Error.Message)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(resp.Result, out); err != nil {
		return fmt.Errorf("decoding %s result: %w", method, err)
	}
	return nil
}

func formatUptime(seconds int64) string {
	days := seconds / 86400
	hours := (seconds % 86400) / 3600
	if days > 0 {
		return fmt.Sprintf("%dd %dh", days, hours)
	}
	minutes := (seconds % 3600) / 60
	return fmt.Sprintf("%dh %dm", hours, minutes)
}
