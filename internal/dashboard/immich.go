package dashboard

import (
	"context"
	"fmt"
	"net/http"
)

func init() {
	Register("immich", &immichWidget{})
}

// immichWidget polls Immich's /api/server/statistics endpoint for library
// size. Secrets["apiKey"] is an Immich API key (needs the server.statistics
// permission), sent as the "x-api-key" header.
type immichWidget struct{}

const (
	headerXAPIKeyLower = "x-api-key"
	labelPhotos        = "Photos"
	labelVideos        = "Videos"
)

type immichStatisticsResponse struct {
	Photos int64 `json:"photos"`
	Videos int64 `json:"videos"`
}

func (immichWidget) Poll(ctx context.Context, httpClient *http.Client, cfg WidgetConfig) ([]Field, error) {
	headers := map[string]string{headerXAPIKeyLower: cfg.Secrets[secretAPIKey]}

	var parsed immichStatisticsResponse
	if fields, err := fetchJSON(ctx, httpClient, cfg, "immich", "/api/server/statistics", headers, &parsed); fields != nil || err != nil {
		return fields, err
	}

	return []Field{
		{Label: labelPhotos, Value: fmt.Sprintf("%d", parsed.Photos)},
		{Label: labelVideos, Value: fmt.Sprintf("%d", parsed.Videos)},
	}, nil
}

func (immichWidget) Sample(WidgetConfig) []Field {
	return []Field{
		{Label: labelPhotos, Value: "18234"},
		{Label: labelVideos, Value: "412"},
	}
}
