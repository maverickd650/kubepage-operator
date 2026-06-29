package dashboard

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

func init() {
	Register("mealie", &mealieWidget{})
}

// mealieWidget polls Mealie's paginated recipes endpoint and reads its
// "total" field. Secrets["token"] is a Mealie API token, sent as a Bearer
// token.
type mealieWidget struct{}

type mealieRecipesResponse struct {
	Total int `json:"total"`
}

func (mealieWidget) Poll(ctx context.Context, httpClient *http.Client, cfg WidgetConfig) ([]Field, error) {
	if cfg.URL == "" {
		return nil, errors.New("mealie widget: url is required")
	}

	endpoint := strings.TrimRight(cfg.URL, "/") + "/api/recipes?page=1&perPage=1"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	if token := cfg.Secrets["token"]; token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	var parsed mealieRecipesResponse
	if fields, err := doJSONRequest(httpClient, req, &parsed); fields != nil || err != nil {
		return fields, err
	}

	return []Field{
		{Label: labelRecipes, Value: fmt.Sprintf("%d", parsed.Total)},
	}, nil
}
