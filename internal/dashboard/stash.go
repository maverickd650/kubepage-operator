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
	Register("stash", &stashWidget{})
}

// stashWidget polls Stash's GraphQL API for library stats. Secrets["token"]
// is Stash's API key, sent via the "ApiKey" header Stash expects.
type stashWidget struct{}

const stashStatsQuery = `{"query":"{ stats { scene_count image_count gallery_count } }"}`

type stashStatsResponse struct {
	Data struct {
		Stats struct {
			SceneCount   int `json:"scene_count"`
			ImageCount   int `json:"image_count"`
			GalleryCount int `json:"gallery_count"`
		} `json:"stats"`
	} `json:"data"`
}

func (stashWidget) Poll(ctx context.Context, httpClient *http.Client, cfg WidgetConfig) ([]Field, error) {
	if cfg.URL == "" {
		return nil, errors.New("stash widget: url is required")
	}

	endpoint := strings.TrimRight(cfg.URL, "/") + "/graphql"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewBufferString(stashStatsQuery))
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token := cfg.Secrets["token"]; token != "" {
		req.Header.Set("ApiKey", token)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return []Field{{Label: labelStatus, Value: statusUnreach}}, nil
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return []Field{{Label: labelStatus, Value: fmt.Sprintf("HTTP %d", resp.StatusCode)}}, nil
	}

	var parsed stashStatsResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("decoding stats response: %w", err)
	}

	return []Field{
		{Label: labelScenes, Value: fmt.Sprintf("%d", parsed.Data.Stats.SceneCount)},
		{Label: labelImages, Value: fmt.Sprintf("%d", parsed.Data.Stats.ImageCount)},
		{Label: labelGalleries, Value: fmt.Sprintf("%d", parsed.Data.Stats.GalleryCount)},
	}, nil
}
