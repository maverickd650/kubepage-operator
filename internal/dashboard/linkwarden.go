package dashboard

import (
	"context"
	"fmt"
	"net/http"
)

func init() {
	Register("linkwarden", &linkwardenWidget{})
}

// linkwardenWidget polls Linkwarden's /api/v1/collections endpoint and sums
// each collection's link count. The /api/v1/links endpoint is paginated
// (capped around 50 results per response), so counting saved links requires
// summing per-collection counts instead of len()-ing a single links page.
// Secrets["token"] is a Linkwarden API token, sent as a Bearer token.
type linkwardenWidget struct{}

type linkwardenCollectionsResponse struct {
	Response []struct {
		Count struct {
			Links int `json:"links"`
		} `json:"_count"`
	} `json:"response"`
}

func (linkwardenWidget) Poll(ctx context.Context, httpClient *http.Client, cfg WidgetConfig) ([]Field, error) {
	headers := map[string]string{}
	if token := cfg.Secrets["token"]; token != "" {
		headers["Authorization"] = "Bearer " + token
	}

	var parsed linkwardenCollectionsResponse
	if fields, err := fetchJSON(ctx, httpClient, cfg, "linkwarden", "/api/v1/collections", headers, &parsed); fields != nil || err != nil {
		return fields, err
	}

	var links int
	for _, c := range parsed.Response {
		links += c.Count.Links
	}

	return []Field{
		{Label: labelLinks, Value: fmt.Sprintf("%d", links)},
		{Label: labelCollections, Value: fmt.Sprintf("%d", len(parsed.Response))},
	}, nil
}

func (linkwardenWidget) Sample(WidgetConfig) []Field {
	return []Field{
		{Label: labelLinks, Value: "128"},
		{Label: labelCollections, Value: "6"},
	}
}
