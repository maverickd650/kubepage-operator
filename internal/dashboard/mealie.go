package dashboard

import (
	"context"
	"encoding/json"
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

	resp, err := httpClient.Do(req)
	if err != nil {
		return []Field{{Label: labelStatus, Value: statusUnreach}}, nil
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return []Field{{Label: labelStatus, Value: fmt.Sprintf("HTTP %d", resp.StatusCode)}}, nil
	}

	var parsed mealieRecipesResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("decoding recipes response: %w", err)
	}

	return []Field{
		{Label: labelRecipes, Value: fmt.Sprintf("%d", parsed.Total)},
	}, nil
}
