package dashboard

import (
	"bytes"
	"context"
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
//
// Field set and GraphQL stats names mirror homepage's stash widget
// (https://gethomepage.dev/widgets/services/stash/,
// https://github.com/gethomepage/homepage's src/widgets/stash/{widget,proxy,component}.js):
// homepage's `fields` allowlist entries are the camelCase keys documented
// there (e.g. "sceneSize", "oCount"); this widget's Field.Label values are
// human-readable ("Scene Size", "O Count") whose normalized form
// (highlight.go's filterFields/normalizeFieldKey — lowercase, punctuation
// stripped) matches those keys, so a homepage-vocabulary `fields` list works
// unmodified. Unlike homepage, which defaults to showing only scenes+images
// when `fields` is unset and caps display at 4, this package has no
// per-widget default-fields mechanism, so an unfiltered ServiceWidget shows
// every field below; users restrict via `fields`.
type stashWidget struct{}

const (
	labelScenesPlayed  = "Scenes Played"
	labelPlayCount     = "Play Count"
	labelPlayDuration  = "Play Duration"
	labelSceneSize     = "Scene Size"
	labelSceneDuration = "Scene Duration"
	labelImageSize     = "Image Size"
	labelPerformers    = "Performers"
	labelStudios       = "Studios"
	labelTags          = "Tags"
	labelOCount        = "O Count"
)

const stashStatsQuery = `{"query":"{ stats { ` +
	`scene_count scenes_size scenes_duration scenes_played ` +
	`image_count images_size gallery_count ` +
	`performer_count studio_count movie_count tag_count ` +
	`total_o_count total_play_count total_play_duration ` +
	`} }"}`

// stashStats is Stash's GraphQL StatsResultType, decoded from the response
// and, for Sample, built directly with deterministic placeholder values —
// stashFields turns either into the widget's display Fields, so Poll and
// Sample can't drift apart on field set or order.
type stashStats struct {
	SceneCount        int     `json:"scene_count"`
	ScenesSize        float64 `json:"scenes_size"`
	ScenesDuration    float64 `json:"scenes_duration"`
	ScenesPlayed      int     `json:"scenes_played"`
	ImageCount        int     `json:"image_count"`
	ImagesSize        float64 `json:"images_size"`
	GalleryCount      int     `json:"gallery_count"`
	PerformerCount    int     `json:"performer_count"`
	StudioCount       int     `json:"studio_count"`
	MovieCount        int     `json:"movie_count"`
	TagCount          int     `json:"tag_count"`
	TotalOCount       int     `json:"total_o_count"`
	TotalPlayCount    int     `json:"total_play_count"`
	TotalPlayDuration float64 `json:"total_play_duration"`
}

type stashStatsResponse struct {
	Data struct {
		Stats stashStats `json:"stats"`
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

	var parsed stashStatsResponse
	if fields, err := doJSONRequest(httpClient, req, &parsed); fields != nil || err != nil {
		return fields, err
	}

	return stashFields(parsed.Data.Stats), nil
}

// stashFields builds the widget's display Fields from decoded stats, in the
// homepage-parity field set/order documented on stashWidget. Sizes are
// rendered human-readably (formatBytesHumanized, shared with opnsense.go)
// and durations as "Xd Xh"/"Xh Xm" (formatUptime, shared with truenas.go) —
// Stash reports both scenes_size/images_size in bytes and
// scenes_duration/total_play_duration in seconds.
func stashFields(s stashStats) []Field {
	return []Field{
		{Label: labelScenes, Value: fmt.Sprintf("%d", s.SceneCount)},
		{Label: labelSceneSize, Value: formatBytesHumanized(s.ScenesSize)},
		{Label: labelSceneDuration, Value: formatUptime(int64(s.ScenesDuration))},
		{Label: labelScenesPlayed, Value: fmt.Sprintf("%d", s.ScenesPlayed)},
		{Label: labelImages, Value: fmt.Sprintf("%d", s.ImageCount)},
		{Label: labelImageSize, Value: formatBytesHumanized(s.ImagesSize)},
		{Label: labelGalleries, Value: fmt.Sprintf("%d", s.GalleryCount)},
		{Label: labelPerformers, Value: fmt.Sprintf("%d", s.PerformerCount)},
		{Label: labelStudios, Value: fmt.Sprintf("%d", s.StudioCount)},
		{Label: labelMovies, Value: fmt.Sprintf("%d", s.MovieCount)},
		{Label: labelTags, Value: fmt.Sprintf("%d", s.TagCount)},
		{Label: labelOCount, Value: fmt.Sprintf("%d", s.TotalOCount)},
		{Label: labelPlayCount, Value: fmt.Sprintf("%d", s.TotalPlayCount)},
		{Label: labelPlayDuration, Value: formatUptime(int64(s.TotalPlayDuration))},
	}
}

func (stashWidget) Sample(WidgetConfig) []Field {
	return stashFields(stashStats{
		SceneCount:        512,
		ScenesSize:        128849018880, // ~120 GB
		ScenesDuration:    3672000,      // ~1020h
		ScenesPlayed:      301,
		ImageCount:        10234,
		ImagesSize:        21474836480, // ~20 GB
		GalleryCount:      87,
		PerformerCount:    142,
		StudioCount:       23,
		MovieCount:        9,
		TagCount:          64,
		TotalOCount:       58,
		TotalPlayCount:    890,
		TotalPlayDuration: 4320000, // ~1200h
	})
}
