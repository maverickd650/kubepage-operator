package dashboard

import (
	"context"
	"fmt"
	"maps"
	"net/http"
)

func init() {
	Register("plex", &plexWidget{})
}

// plexWidget polls Plex to match gethomepage/homepage's default plex fields:
// Streams (active sessions), Albums, Movies, and TV — the latter three summed
// across every library section of the matching type. Secrets["token"] is
// Plex's X-Plex-Token, sent as a header (not a query param, so it never lands
// in server access logs).
type plexWidget struct{}

type plexSessionsResponse struct {
	MediaContainer struct {
		Size int `json:"size"`
	} `json:"MediaContainer"`
}

// plexSectionsResponse is /library/sections: one Directory entry per library,
// each carrying the section key used to query its item count and the type
// (movie/show/artist) deciding which count it contributes to.
type plexSectionsResponse struct {
	MediaContainer struct {
		Directory []plexSection `json:"Directory"`
	} `json:"MediaContainer"`
}

type plexSection struct {
	Key  string `json:"key"`
	Type string `json:"type"`
}

// plexSectionCount is a library section listing (/library/sections/{key}/all
// or .../albums). TotalSize is the full item count when the response is
// paginated, which it always is here since this widget requests a zero-size
// page (see plexCountHeaders) — so a large library reports its count without
// streaming every item through each poll. Size is only a fallback for the
// theoretical case of Plex omitting totalSize; with a zero-size page it is
// itself 0, so an absent totalSize surfaces as 0 rather than a wrong count.
type plexSectionCount struct {
	MediaContainer struct {
		Size      int `json:"size"`
		TotalSize int `json:"totalSize"`
	} `json:"MediaContainer"`
}

const (
	plexSectionTypeMovie  = "movie"
	plexSectionTypeShow   = "show"
	plexSectionTypeArtist = "artist"
)

// plexCountHeaders adds Plex's pagination headers to the base request headers,
// asking for a zero-size page so /all and /albums return just MediaContainer's
// totalSize without the (potentially large) item list — see plexSectionCount.
func plexCountHeaders(base map[string]string) map[string]string {
	headers := maps.Clone(base)
	headers["X-Plex-Container-Start"] = "0"
	headers["X-Plex-Container-Size"] = "0"
	return headers
}

func (plexWidget) Poll(ctx context.Context, httpClient *http.Client, cfg WidgetConfig) ([]Field, error) {
	headers := map[string]string{"Accept": "application/json"}
	if token := cfg.Secrets["token"]; token != "" {
		headers["X-Plex-Token"] = token
	}

	var sessions plexSessionsResponse
	if fields, err := fetchJSON(ctx, httpClient, cfg, "plex", "/status/sessions", headers, &sessions); fields != nil || err != nil {
		return fields, err
	}

	var sections plexSectionsResponse
	if fields, err := fetchJSON(ctx, httpClient, cfg, "plex", "/library/sections", headers, &sections); fields != nil || err != nil {
		return fields, err
	}

	countHeaders := plexCountHeaders(headers)
	var albums, movies, tv int
	for _, section := range sections.MediaContainer.Directory {
		var path string
		switch section.Type {
		case plexSectionTypeMovie, plexSectionTypeShow:
			path = "/library/sections/" + section.Key + "/all"
		case plexSectionTypeArtist:
			path = "/library/sections/" + section.Key + "/albums"
		default:
			continue
		}

		var count plexSectionCount
		if fields, err := fetchJSON(ctx, httpClient, cfg, "plex", path, countHeaders, &count); fields != nil || err != nil {
			return fields, err
		}

		size := count.MediaContainer.Size
		if count.MediaContainer.TotalSize > 0 {
			size = count.MediaContainer.TotalSize
		}
		switch section.Type {
		case plexSectionTypeMovie:
			movies += size
		case plexSectionTypeShow:
			tv += size
		case plexSectionTypeArtist:
			albums += size
		}
	}

	return []Field{
		{Label: labelStreams, Value: fmt.Sprintf("%d", sessions.MediaContainer.Size)},
		{Label: labelAlbums, Value: fmt.Sprintf("%d", albums)},
		{Label: labelMovies, Value: fmt.Sprintf("%d", movies)},
		{Label: labelTV, Value: fmt.Sprintf("%d", tv)},
	}, nil
}

func (plexWidget) Sample(WidgetConfig) []Field {
	return []Field{
		{Label: labelStreams, Value: "3"},
		{Label: labelAlbums, Value: "1240"},
		{Label: labelMovies, Value: "842"},
		{Label: labelTV, Value: "120"},
	}
}
