package dashboard

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"testing"
)

const (
	plexSessionsPath  = "/status/sessions"
	plexSectionsPath  = "/library/sections"
	plexMovieAllPath  = "/library/sections/1/all"
	plexShowAllPath   = "/library/sections/2/all"
	plexArtistAllPath = "/library/sections/3/albums"
)

// plexMockHandler serves the endpoints Poll walks in order: /status/sessions,
// /library/sections, and each section's count listing (/all or /albums). The
// section keys are fixed: "1" movie, "2" show, "3" artist, "9" photo (an
// unsupported type that Poll must skip). It records every X-Plex-Token header
// and the query string seen on count requests.
func plexMockHandler(sessionsSize int, gotTokens *[]string, gotCountQueries *[]string) http.HandlerFunc {
	sectionBodies := map[string]string{
		plexMovieAllPath:  `{"MediaContainer":{"size":0,"totalSize":842}}`,
		plexShowAllPath:   `{"MediaContainer":{"size":0,"totalSize":120}}`,
		plexArtistAllPath: `{"MediaContainer":{"size":0,"totalSize":1240}}`,
	}
	return func(w http.ResponseWriter, r *http.Request) {
		*gotTokens = append(*gotTokens, r.Header.Get("X-Plex-Token"))
		switch r.URL.Path {
		case plexSessionsPath:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"MediaContainer":{"size":` + strconv.Itoa(sessionsSize) + `}}`))
		case plexSectionsPath:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"MediaContainer":{"Directory":[` +
				`{"key":"1","type":"movie"},` +
				`{"key":"2","type":"show"},` +
				`{"key":"3","type":"artist"},` +
				`{"key":"9","type":"photo"}]}}`))
		default:
			if body, ok := sectionBodies[r.URL.Path]; ok {
				if gotCountQueries != nil {
					*gotCountQueries = append(*gotCountQueries, r.URL.RawQuery)
				}
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(body))
				return
			}
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

func TestPlexWidgetPoll(t *testing.T) {
	var gotTokens []string
	srv := httptest.NewServer(plexMockHandler(3, &gotTokens, nil))
	defer srv.Close()

	got, err := (plexWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{
		URL:     srv.URL,
		Secrets: map[string]string{testSecretField: "plextok"},
	})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	// The photo section (key "9") is an unsupported type and must be skipped,
	// so it contributes to none of the counts.
	want := []Field{
		{Label: labelStreams, Value: "3"},
		{Label: labelAlbums, Value: "1240"},
		{Label: labelMovies, Value: "842"},
		{Label: labelTV, Value: "120"},
	}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
	for _, tok := range gotTokens {
		if tok != "plextok" {
			t.Errorf("X-Plex-Token header = %q, want %q", tok, "plextok")
		}
	}
}

// TestPlexWidgetPollCountsAcrossLibraries verifies sizes are summed across
// multiple sections of the same type, and that a section missing totalSize
// falls back to size.
func TestPlexWidgetPollCountsAcrossLibraries(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case plexSessionsPath:
			_, _ = w.Write([]byte(`{"MediaContainer":{"size":0}}`))
		case plexSectionsPath:
			_, _ = w.Write([]byte(`{"MediaContainer":{"Directory":[` +
				`{"key":"1","type":"movie"},` +
				`{"key":"2","type":"movie"}]}}`))
		case plexMovieAllPath:
			_, _ = w.Write([]byte(`{"MediaContainer":{"size":0,"totalSize":10}}`))
		case plexShowAllPath:
			// No totalSize: the size field is the fallback.
			_, _ = w.Write([]byte(`{"MediaContainer":{"size":5}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	got, err := (plexWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{URL: srv.URL})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{
		{Label: labelStreams, Value: "0"},
		{Label: labelAlbums, Value: "0"},
		{Label: labelMovies, Value: "15"},
		{Label: labelTV, Value: "0"},
	}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}

// TestPlexWidgetPollCountQuery verifies the count requests ask for a zero-size
// page so a large library is never streamed in full.
func TestPlexWidgetPollCountQuery(t *testing.T) {
	var gotTokens, gotCountQueries []string
	srv := httptest.NewServer(plexMockHandler(0, &gotTokens, &gotCountQueries))
	defer srv.Close()

	if _, err := (plexWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{URL: srv.URL}); err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	if len(gotCountQueries) != 3 {
		t.Fatalf("count requests = %d, want 3", len(gotCountQueries))
	}
	// The pagination is sent as headers, not query params, so the count
	// request URLs carry no query string.
	for _, q := range gotCountQueries {
		if q != "" {
			t.Errorf("count request query = %q, want empty (pagination is header-based)", q)
		}
	}
}

func TestPlexWidgetPollContainerHeaders(t *testing.T) {
	var gotStart, gotSize string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case plexSessionsPath:
			_, _ = w.Write([]byte(`{"MediaContainer":{"size":0}}`))
		case plexSectionsPath:
			_, _ = w.Write([]byte(`{"MediaContainer":{"Directory":[{"key":"1","type":"movie"}]}}`))
		case plexMovieAllPath:
			gotStart = r.Header.Get("X-Plex-Container-Start")
			gotSize = r.Header.Get("X-Plex-Container-Size")
			_, _ = w.Write([]byte(`{"MediaContainer":{"size":0,"totalSize":1}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	if _, err := (plexWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{URL: srv.URL}); err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	if gotStart != "0" || gotSize != "0" {
		t.Errorf("count request X-Plex-Container-Start/Size = %q/%q, want 0/0", gotStart, gotSize)
	}
}

// TestPlexWidgetPollSessionsError covers the first request (sessions) failing:
// Poll returns the standard Status field without walking any libraries.
func TestPlexWidgetPollSessionsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	got, err := (plexWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{URL: srv.URL})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{{Label: labelStatus, Value: testHTTP401}}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}

// TestPlexWidgetPollSectionsError covers a request mid-sequence (a library
// count) failing: Poll surfaces it as the Status field rather than an
// undercount.
func TestPlexWidgetPollSectionsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case plexSessionsPath:
			_, _ = w.Write([]byte(`{"MediaContainer":{"size":0}}`))
		case plexSectionsPath:
			_, _ = w.Write([]byte(`{"MediaContainer":{"Directory":[{"key":"1","type":"movie"}]}}`))
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	got, err := (plexWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{URL: srv.URL})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{{Label: labelStatus, Value: testHTTP500}}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}

func TestPlexWidgetPollMissingURL(t *testing.T) {
	if _, err := (plexWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{}); err == nil {
		t.Fatal("Poll() expected error for missing URL, got nil")
	}
}

func TestPlexWidgetPollUnreachable(t *testing.T) {
	got, err := (plexWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{URL: testUnreachableAddr})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{{Label: labelStatus, Value: statusUnreach}}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}

func TestPlexWidgetSample(t *testing.T) {
	got := (plexWidget{}).Sample(WidgetConfig{})
	wantLabels := []string{labelStreams, labelAlbums, labelMovies, labelTV}
	if len(got) != len(wantLabels) {
		t.Fatalf("Sample() returned %d fields, want %d", len(got), len(wantLabels))
	}
	for i, label := range wantLabels {
		if got[i].Label != label {
			t.Errorf("Sample()[%d].Label = %q, want %q", i, got[i].Label, label)
		}
	}
	assertSampleDeterministic(t, plexWidget{})
}
