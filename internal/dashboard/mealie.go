package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

func init() {
	Register("mealie", &mealieWidget{})
}

// mealieWidget polls Mealie's statistics endpoint, matching
// gethomepage/homepage's mealie widget. Mealie v2+ (the "household" model)
// serves statistics at /api/households/statistics; Mealie v1 (the older
// "group" model) serves the equivalent at /api/groups/statistics. config:
// {"version": 1} selects the v1 endpoint; version unset or 2 uses v2, the
// current Mealie release line. Secrets["token"] is a Mealie API token, sent
// as a Bearer token.
type mealieWidget struct{}

// mealieStatisticsPathV2/V1 are the household- and group-model statistics
// endpoints (see mealieWidget's doc comment); pulled into constants since
// mealie_test.go also asserts against them per test case.
const (
	mealieStatisticsPathV2 = "/api/households/statistics"
	mealieStatisticsPathV1 = "/api/groups/statistics"
)

type mealieConfig struct {
	Version int `json:"version"`
}

type mealieStatisticsResponse struct {
	TotalRecipes    int `json:"totalRecipes"`
	TotalUsers      int `json:"totalUsers"`
	TotalCategories int `json:"totalCategories"`
	TotalTags       int `json:"totalTags"`
}

func (mealieWidget) Poll(ctx context.Context, httpClient *http.Client, cfg WidgetConfig) ([]Field, error) {
	if cfg.URL == "" {
		return nil, errors.New("mealie widget: url is required")
	}

	var mealieCfg mealieConfig
	if len(cfg.Config) > 0 {
		if err := json.Unmarshal(cfg.Config, &mealieCfg); err != nil {
			return nil, fmt.Errorf("decoding widget config: %w", err)
		}
	}

	path := mealieStatisticsPathV2
	if mealieCfg.Version == 1 {
		path = mealieStatisticsPathV1
	}

	headers := map[string]string{}
	if token := cfg.Secrets["token"]; token != "" {
		headers["Authorization"] = "Bearer " + token
	}

	var parsed mealieStatisticsResponse
	if fields, err := fetchJSON(ctx, httpClient, cfg, "mealie", path, headers, &parsed); fields != nil || err != nil {
		return fields, err
	}

	return []Field{
		{Label: labelRecipes, Value: fmt.Sprintf("%d", parsed.TotalRecipes)},
		{Label: labelUsers, Value: fmt.Sprintf("%d", parsed.TotalUsers)},
		{Label: labelCategories, Value: fmt.Sprintf("%d", parsed.TotalCategories)},
		{Label: labelTags, Value: fmt.Sprintf("%d", parsed.TotalTags)},
	}, nil
}

func (mealieWidget) Sample(WidgetConfig) []Field {
	return []Field{
		{Label: labelRecipes, Value: "87"},
		{Label: labelUsers, Value: "3"},
		{Label: labelCategories, Value: "12"},
		{Label: labelTags, Value: "20"},
	}
}
