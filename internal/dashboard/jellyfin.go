package dashboard

import (
	"context"
	"fmt"
	"net/http"
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
	headers := map[string]string{headerXEmbyToken: cfg.Secrets["token"]}

	var info jellyfinSystemInfoResponse
	if fields, err := fetchJSON(ctx, httpClient, cfg, "jellyfin", "/System/Info", headers, &info); fields != nil || err != nil {
		return fields, err
	}

	var sessions []jellyfinSession
	if fields, err := fetchJSON(ctx, httpClient, cfg, "jellyfin", "/Sessions", headers, &sessions); fields != nil || err != nil {
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
