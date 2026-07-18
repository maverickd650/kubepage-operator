package dashboard

import (
	"context"
	"io"
	"net/http"
	"strings"
)

func init() {
	Register("gitea", &giteaWidget{})
}

// giteaWidget polls Gitea's /api/v1/version endpoint, plus a best-effort
// total-repository count from /api/v1/repos/search (Gitea returns the total
// in an X-Total-Count response header rather than the JSON body, so that
// second call is skipped rather than failing the whole poll if it errors).
// Secrets["token"] is a Gitea access token, sent as "Authorization: token
// <token>" — Gitea's own scheme, distinct from Bearer.
type giteaWidget struct{}

const labelRepos = "Repos"

// giteaSampleVersion is Sample's placeholder Gitea version, also referenced
// from gitea_test.go's mock server fixtures so the literal isn't retyped
// across both files.
const giteaSampleVersion = "1.22.3"

type giteaVersionResponse struct {
	Version string `json:"version"`
}

func (giteaWidget) Poll(ctx context.Context, httpClient *http.Client, cfg WidgetConfig) ([]Field, error) {
	token := cfg.Secrets["token"]
	base := strings.TrimRight(cfg.URL, "/")

	headers := map[string]string{}
	if token != "" {
		headers["Authorization"] = "token " + token
	}

	var version giteaVersionResponse
	if fields, err := fetchJSON(ctx, httpClient, cfg, "gitea", "/api/v1/version", headers, &version); fields != nil || err != nil {
		return fields, err
	}

	fields := []Field{{Label: labelVersion, Value: version.Version}}

	if repos, ok := giteaTotalRepos(ctx, httpClient, base, token); ok {
		fields = append(fields, Field{Label: labelRepos, Value: repos})
	}

	return fields, nil
}

// giteaTotalRepos best-effort fetches the total repository count via the
// X-Total-Count header of /api/v1/repos/search?limit=1, returning ok=false
// on any failure so a Gitea instance that rejects or doesn't support this
// call still renders its Version field.
func giteaTotalRepos(ctx context.Context, httpClient *http.Client, base, token string) (string, bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/v1/repos/search?limit=1", nil)
	if err != nil {
		return "", false
	}
	if token != "" {
		req.Header.Set("Authorization", "token "+token)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", false
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxWidgetResponseBytes))

	if resp.StatusCode != http.StatusOK {
		return "", false
	}
	total := resp.Header.Get("X-Total-Count")
	if total == "" {
		return "", false
	}
	return total, true
}

func (giteaWidget) Sample(WidgetConfig) []Field {
	return []Field{
		{Label: labelVersion, Value: giteaSampleVersion},
		{Label: labelRepos, Value: "57"},
	}
}
