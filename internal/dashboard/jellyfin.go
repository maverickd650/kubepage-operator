package dashboard

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

func init() {
	Register("jellyfin", &jellyfinWidget{})
}

// jellyfinWidget polls Jellyfin's /System/Info (version) and /Sessions
// (active playback count) endpoints. Secrets["token"] is a Jellyfin API key,
// sent as the "X-Emby-Token" header — Jellyfin's server API still speaks
// Emby's original header name for backward compatibility.
type jellyfinWidget struct{}

const headerXEmbyToken = "X-Emby-Token"

type jellyfinSystemInfoResponse struct {
	Version string `json:"Version"`
}

type jellyfinSession struct {
	NowPlayingItem *struct{} `json:"NowPlayingItem"`
}

func (jellyfinWidget) Poll(ctx context.Context, httpClient *http.Client, cfg WidgetConfig) ([]Field, error) {
	if cfg.URL == "" {
		return nil, errors.New("jellyfin widget: url is required")
	}
	token := cfg.Secrets["token"]

	base := strings.TrimRight(cfg.URL, "/")

	infoReq, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/System/Info", nil)
	if err != nil {
		return nil, fmt.Errorf("building system info request: %w", err)
	}
	infoReq.Header.Set(headerXEmbyToken, token)

	var info jellyfinSystemInfoResponse
	if fields, err := doJSONRequest(httpClient, infoReq, &info); fields != nil || err != nil {
		return fields, err
	}

	sessionsReq, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/Sessions", nil)
	if err != nil {
		return nil, fmt.Errorf("building sessions request: %w", err)
	}
	sessionsReq.Header.Set(headerXEmbyToken, token)

	var sessions []jellyfinSession
	if fields, err := doJSONRequest(httpClient, sessionsReq, &sessions); fields != nil || err != nil {
		return fields, err
	}

	streams := 0
	for _, s := range sessions {
		if s.NowPlayingItem != nil {
			streams++
		}
	}

	return []Field{
		{Label: labelVersion, Value: info.Version},
		{Label: labelStreams, Value: fmt.Sprintf("%d", streams)},
	}, nil
}

func (jellyfinWidget) Sample(WidgetConfig) []Field {
	return []Field{
		{Label: labelVersion, Value: "10.9.7"},
		{Label: labelStreams, Value: "1"},
	}
}
