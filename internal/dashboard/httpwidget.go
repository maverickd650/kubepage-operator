package dashboard

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// maxWidgetResponseBytes caps how many bytes of an upstream's response body
// a widget will read before decoding. Without this, a slow or malicious
// upstream streaming data for the duration of the shared HTTP client's
// timeout could still push an unbounded amount of data into memory per
// poll, multiplied by maxConcurrentPolls concurrent polls.
const maxWidgetResponseBytes = 2 << 20 // 2 MiB

// doJSONRequest executes req via httpClient and decodes a 2xx JSON response
// body into out, capped at maxWidgetResponseBytes. On a transport failure or
// non-2xx response it returns the widget-standard "Unreachable"/"HTTP <code>"
// status Field and a nil error — by this package's convention (see e.g.
// grafana.go), a widget reports those as a displayable Field rather than a
// Go error so the card still renders a status; poller.go's metricErr is what
// makes sure that convention doesn't also hide the failure from poll
// metrics. Callers should return immediately when the returned fields or err
// is non-nil; a nil, nil result means out was populated and the caller
// should build its own success Fields from it.
func doJSONRequest(httpClient *http.Client, req *http.Request, out any) ([]Field, error) {
	resp, err := httpClient.Do(req)
	if err != nil {
		return []Field{{Label: labelStatus, Value: statusUnreach}}, nil
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return []Field{{Label: labelStatus, Value: fmt.Sprintf("HTTP %d", resp.StatusCode)}}, nil
	}

	body := io.LimitReader(resp.Body, maxWidgetResponseBytes)
	if err := json.NewDecoder(body).Decode(out); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return nil, nil
}
