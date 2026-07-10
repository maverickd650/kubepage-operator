package dashboard

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

func init() {
	Register("pihole", &piholeWidget{})
}

// piholeWidget polls a Pi-hole v6 controller. Pi-hole v6's API (a full
// rewrite from v5's static-token query string) is session-based: a POST to
// /api/auth with a password (a regular Pi-hole web-UI password, or an "app
// password" generated in Settings > API for scripting, either works)
// returns a short-lived session id, sent as the "X-FTL-SID" header on the
// stats request that follows. Secrets["password"] holds it.
//
// Unlike unifi.go's session, this one is never cached across polls: Pi-hole
// sessions are cheap to establish (a single POST) and short-lived by design
// (the "validity" the auth response returns is commonly a few minutes), so
// the simplest-documented flow — log in, make the one stats request, done —
// is used every poll rather than adding cross-poll session state that would
// need its own TTL/eviction bookkeeping for a savings that doesn't matter
// here.
type piholeWidget struct{}

const labelBlockPercent = "Block %"

type piholeAuthRequest struct {
	Password string `json:"password"`
}

type piholeAuthResponse struct {
	Session struct {
		Valid bool   `json:"valid"`
		SID   string `json:"sid"`
	} `json:"session"`
}

type piholeStatsSummaryResponse struct {
	Queries struct {
		Total          int     `json:"total"`
		Blocked        int     `json:"blocked"`
		PercentBlocked float64 `json:"percent_blocked"`
	} `json:"queries"`
}

func (piholeWidget) Poll(ctx context.Context, httpClient *http.Client, cfg WidgetConfig) ([]Field, error) {
	if cfg.URL == "" {
		return nil, errors.New("pihole widget: url is required")
	}
	password := cfg.Secrets[secretPassword]
	if password == "" {
		return nil, errors.New("pihole widget: secrets.password is required")
	}

	base := strings.TrimRight(cfg.URL, "/")

	authBody, err := json.Marshal(piholeAuthRequest{Password: password})
	if err != nil {
		return nil, fmt.Errorf("encoding auth request: %w", err)
	}
	authReq, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/auth", bytes.NewReader(authBody))
	if err != nil {
		return nil, fmt.Errorf("building auth request: %w", err)
	}
	authReq.Header.Set("Content-Type", "application/json")

	var auth piholeAuthResponse
	if fields, err := doJSONRequest(httpClient, authReq, &auth); fields != nil || err != nil {
		return fields, err
	}
	if !auth.Session.Valid || auth.Session.SID == "" {
		return []Field{{Label: labelStatus, Value: statusUnreach}}, nil
	}

	statsReq, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/stats/summary", nil)
	if err != nil {
		return nil, fmt.Errorf("building stats request: %w", err)
	}
	statsReq.Header.Set("X-FTL-SID", auth.Session.SID)

	var stats piholeStatsSummaryResponse
	if fields, err := doJSONRequest(httpClient, statsReq, &stats); fields != nil || err != nil {
		return fields, err
	}

	return []Field{
		{Label: labelQueries, Value: fmt.Sprintf("%d", stats.Queries.Total)},
		{Label: labelBlocked, Value: fmt.Sprintf("%d", stats.Queries.Blocked)},
		{Label: labelBlockPercent, Value: fmt.Sprintf("%.1f%%", stats.Queries.PercentBlocked)},
	}, nil
}

func (piholeWidget) Sample(WidgetConfig) []Field {
	return []Field{
		{Label: labelQueries, Value: "63482"},
		{Label: labelBlocked, Value: "12904"},
		{Label: labelBlockPercent, Value: "20.3%"},
	}
}
