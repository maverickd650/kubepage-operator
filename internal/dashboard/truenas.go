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
// "system.info" then supplies Version/Uptime.
type truenasWidget struct{}

// truenasMaxFrameBytes caps a single WebSocket message the same way
// maxWidgetResponseBytes caps a REST widget's response body — system.info's
// result is small, but an upstream (or a compromised/misbehaving one)
// shouldn't be able to push unbounded data into memory per poll.
const truenasMaxFrameBytes = maxWidgetResponseBytes

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
	Version       string `json:"version"`
	UptimeSeconds int64  `json:"uptime_seconds"`
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
		return []Field{{Label: labelStatus, Value: statusUnreach}}, nil
	}
	defer func() { _ = conn.CloseNow() }()
	conn.SetReadLimit(truenasMaxFrameBytes)

	var loggedIn bool
	if err := truenasCall(callCtx, conn, "auth.login_with_api_key", []any{cfg.Secrets["token"]}, 1, &loggedIn); err != nil || !loggedIn {
		return []Field{{Label: labelStatus, Value: statusUnreach}}, nil
	}

	var info truenasSystemInfoResult
	if err := truenasCall(callCtx, conn, "system.info", []any{}, 2, &info); err != nil {
		return []Field{{Label: labelStatus, Value: statusUnreach}}, nil
	}

	_ = conn.Close(websocket.StatusNormalClosure, "")

	return []Field{
		{Label: labelVersion, Value: info.Version},
		{Label: labelUptime, Value: formatUptime(info.UptimeSeconds)},
	}, nil
}

func (truenasWidget) Sample(WidgetConfig) []Field {
	return []Field{
		{Label: labelVersion, Value: "TrueNAS-24.04.0"},
		{Label: labelUptime, Value: formatUptime(370000)},
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

// truenasCall sends one JSON-RPC 2.0 request over conn and decodes its
// result into out (skipped if out is nil, e.g. auth.login_with_api_key's
// boolean result is decoded by the caller directly). Returns an error on a
// transport failure, a malformed response, or a JSON-RPC-level error object.
func truenasCall(ctx context.Context, conn *websocket.Conn, method string, params []any, id int, out any) error {
	req := jsonrpcRequest{JSONRPC: "2.0", Method: method, Params: params, ID: id}
	if err := wsjson.Write(ctx, conn, req); err != nil {
		return fmt.Errorf("writing %s request: %w", method, err)
	}

	var resp jsonrpcResponse
	if err := wsjson.Read(ctx, conn, &resp); err != nil {
		return fmt.Errorf("reading %s response: %w", method, err)
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
