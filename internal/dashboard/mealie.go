package dashboard

import (
	"context"
	"fmt"
	"net/http"
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
	headers := map[string]string{}
	if token := cfg.Secrets["token"]; token != "" {
		headers["Authorization"] = "Bearer " + token
	}

	var parsed mealieRecipesResponse
	if fields, err := fetchJSON(ctx, httpClient, cfg, "mealie", "/api/recipes?page=1&perPage=1", headers, &parsed); fields != nil || err != nil {
		return fields, err
	}

	return []Field{
		{Label: labelRecipes, Value: fmt.Sprintf("%d", parsed.Total)},
	}, nil
}

func (mealieWidget) Sample(WidgetConfig) []Field {
	return []Field{{Label: labelRecipes, Value: "87"}}
}
