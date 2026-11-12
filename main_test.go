package main

import (
	"context"
	"encoding/json"
	"html/template"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func testCatalog() *catalog {
	return &catalog{
		artists: []Artist{
			{
				ID: 1, Name: "Queen", Image: "/queen.jpg",
				Members: []string{"Freddie Mercury", "Brian May"}, CreationDate: 1970,
				FirstAlbum: "14-12-1973",
			},
			{
				ID: 2, Name: "Scorpions", Image: "/scorpions.jpg",
				Members: []string{"Klaus Meine"}, CreationDate: 1965,
				FirstAlbum: "01-01-1972",
			},
			{
				ID: 3, Name: "Example Band", Image: "/example.jpg",
				Members: []string{"Scott Example"}, CreationDate: 2000,
				FirstAlbum: "01-01-2001",
			},
		},
		locations: map[int][]string{1: {"london-uk"}, 2: {"berlin-germany"}, 3: {"scotland-uk"}},
		dates:     map[int][]string{1: {"10-05-2007"}, 2: {"01-02-2020"}, 3: {"04-05-2020"}},
		relations: map[int]map[string][]string{
			1: {"london-uk": {"10-05-2007"}},
			2: {"berlin-germany": {"01-02-2020"}},
			3: {"scotland-uk": {"04-05-2020"}},
		},
	}
}

func testServer(t *testing.T) (*server, *http.ServeMux) {
	t.Helper()
	templates, err := template.ParseFS(assets, "templates/*.html")
	if err != nil {
		t.Fatal(err)
	}
	app := &server{state: &appState{data: testCatalog()}, templates: templates, apiBase: "https://api.example.test"}
	mux := http.NewServeMux()
	mux.HandleFunc("/", app.handleHome)
	mux.HandleFunc("/about", app.handleAbout)
	mux.HandleFunc("/docs", app.handleDocs)
	mux.HandleFunc("/healthz", app.handleHealth)
	mux.HandleFunc("/api/artists", app.handleArtists)
	mux.HandleFunc("/api/filter-options", app.handleFilterOptions)
	mux.HandleFunc("/api/artists/", app.handleArtistDetail)
	mux.HandleFunc("/api/refresh", app.handleRefresh)
	mux.HandleFunc("/api/status", app.handleStatus)
	return app, mux
}

func TestHomeRendersArtistData(t *testing.T) {
	_, mux := testServer(t)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	for _, expected := range []string{"Queen", "1970", "14-12-1973", "FILTERS", "Refine artists", "Formation year", "Concert locations", `<details class="filter-panel">`, "leaflet.css", "leaflet.js"} {
		if !strings.Contains(response.Body.String(), expected) {
			t.Errorf("home page does not contain %q", expected)
		}
	}
	if strings.Contains(response.Body.String(), `<details class="filter-panel" open`) {
		t.Error("filter panel should be collapsed by default")
	}
}

func TestInformationPagesAndFooterAPIlinks(t *testing.T) {
	_, mux := testServer(t)
	for _, test := range []struct {
		path     string
		title    string
		contains []string
	}{
		{"/about", "About — Groupie Tracker", []string{"What it does", "by Mohammad, Ghazal, Milutin. 2026", "https://api.example.test/artists", "https://api.example.test/dates", "https://api.example.test/locations"}},
		{"/docs", "Docs — Groupie Tracker", []string{"Search the catalog", "Server endpoints", "persists successful coordinates", "one-request-per-second", "https://api.example.test/artists", "https://api.example.test/dates", "https://api.example.test/locations", "https://api.example.test/relation"}},
		{"/", "Groupie Tracker — Artists & live dates", []string{"GROUPIE TRACKER", "by Mohammad, Ghazal, Milutin. 2026", "https://api.example.test/artists", "https://api.example.test/dates", "https://api.example.test/locations"}},
	} {
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, test.path, nil))
		if response.Code != http.StatusOK {
			t.Errorf("GET %s status = %d, want %d", test.path, response.Code, http.StatusOK)
			continue
		}
		body := response.Body.String()
		if !strings.Contains(body, test.title) {
			t.Errorf("GET %s body does not contain title %q", test.path, test.title)
		}
		for _, expected := range test.contains {
			if !strings.Contains(body, expected) {
				t.Errorf("GET %s body does not contain %q", test.path, expected)
			}
		}
	}
}

func TestHealthEndpoint(t *testing.T) {
	_, mux := testServer(t)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if response.Code != http.StatusOK || response.Body.String() != "ok\n" {
		t.Errorf("health endpoint returned status %d and body %q", response.Code, response.Body.String())
	}
}

func TestUnknownPageRendersBrandedNotFound(t *testing.T) {
	_, mux := testServer(t)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/this-page-does-not-exist", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
	}
	for _, expected := range []string{
		"404 — Groupie Tracker",
		"This page",
		"missed its cue.",
		"Back to the artist directory",
		"https://api.example.test/artists",
	} {
		if !strings.Contains(response.Body.String(), expected) {
			t.Errorf("not found page does not contain %q", expected)
		}
	}
	if response.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q, want HTML", response.Header().Get("Content-Type"))
	}
}

func TestArtistSearchSeparatesMatchCategories(t *testing.T) {
	_, mux := testServer(t)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/artists?q=sc", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	var results artistSearchResults
	if err := json.Unmarshal(response.Body.Bytes(), &results); err != nil {
		t.Fatal(err)
	}
	if len(results.Artists) != 1 || results.Artists[0].Name != "Scorpions" {
		t.Errorf("artist-name matches = %#v, want only Scorpions", results.Artists)
	}
	if len(results.Members) != 1 || results.Members[0].Artist.Name != "Example Band" || !reflect.DeepEqual(results.Members[0].Matches, []string{"Scott Example"}) {
		t.Errorf("member matches = %#v, want Example Band / Scott Example", results.Members)
	}
	if len(results.Locations) != 1 || results.Locations[0].Artist.Name != "Example Band" || !reflect.DeepEqual(results.Locations[0].Matches, []string{"scotland-uk"}) {
		t.Errorf("location matches = %#v, want Example Band / scotland-uk", results.Locations)
	}
}

func TestArtistSearchMatchesDatesAndDetails(t *testing.T) {
	_, mux := testServer(t)
	for query, want := range map[string]struct {
		date   string
		detail string
	}{
		"2020":       {"01-02-2020", ""},
		"1970":       {"", "Started in 1970"},
		"14-12-1973": {"", "First album 14-12-1973"},
	} {
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/artists?q="+query, nil))
		var results artistSearchResults
		if err := json.Unmarshal(response.Body.Bytes(), &results); err != nil {
			t.Fatal(err)
		}
		if want.date != "" && (len(results.Dates) == 0 || !contains(results.Dates[0].Matches, want.date)) {
			t.Errorf("search %q date matches = %#v, want %q", query, results.Dates, want.date)
		}
		if want.detail != "" && (len(results.Details) == 0 || !contains(results.Details[0].Matches, want.detail)) {
			t.Errorf("search %q detail matches = %#v, want %q", query, results.Details, want.detail)
		}
	}
}

func TestArtistFilters(t *testing.T) {
	_, mux := testServer(t)
	tests := []struct {
		name  string
		query string
		want  []string
	}{
		{"creation year range", "creationMin=1970&creationMax=2000", []string{"Queen", "Example Band"}},
		{"first album year range", "albumMin=1972&albumMax=1973", []string{"Queen", "Scorpions"}},
		{"exact member count", "membersMin=1&membersMax=1", []string{"Scorpions", "Example Band"}},
		{"case-insensitive location", "location=SCOTLAND-UK", []string{"Example Band"}},
		{"combined filters", "creationMin=2000&membersMax=1&location=scotland-uk", []string{"Example Band"}},
		{"clear filters returns all artists", "", []string{"Queen", "Scorpions", "Example Band"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/artists?"+test.query, nil))
			if response.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d; body: %s", response.Code, http.StatusOK, response.Body.String())
			}
			var results artistSearchResults
			if err := json.Unmarshal(response.Body.Bytes(), &results); err != nil {
				t.Fatal(err)
			}
			got := make([]string, 0, len(results.Artists))
			for _, artist := range results.Artists {
				got = append(got, artist.Name)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Errorf("artists = %v, want %v", got, test.want)
			}
			if results.TotalArtists != len(test.want) {
				t.Errorf("totalArtists = %d, want %d", results.TotalArtists, len(test.want))
			}
		})
	}
}

func TestArtistFiltersRejectInvalidRanges(t *testing.T) {
	_, mux := testServer(t)
	for _, query := range []string{
		"creationMin=not-a-year",
		"creationMin=2001&creationMax=2000",
		"membersMin=3&membersMax=two",
		"creationMin=1990&creationMin=2000",
	} {
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/artists?"+query, nil))
		if response.Code != http.StatusBadRequest {
			t.Errorf("query %q returned status %d, want %d", query, response.Code, http.StatusBadRequest)
		}
	}
}

func TestFirstAlbumYearRequiresValidDate(t *testing.T) {
	if year, ok := firstAlbumYear("05-08-1967"); !ok || year != 1967 {
		t.Errorf("firstAlbumYear(valid date) = %d, %t; want 1967, true", year, ok)
	}
	if _, ok := firstAlbumYear("99-99-1967"); ok {
		t.Error("firstAlbumYear accepted an invalid date")
	}
}

func TestFilterOptions(t *testing.T) {
	_, mux := testServer(t)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/filter-options", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	var options filterOptions
	if err := json.Unmarshal(response.Body.Bytes(), &options); err != nil {
		t.Fatal(err)
	}
	if options.CreationYearMin != 1965 || options.CreationYearMax != 2000 {
		t.Errorf("creation year bounds = %d-%d, want 1965-2000", options.CreationYearMin, options.CreationYearMax)
	}
	if options.FirstAlbumYearMin != 1972 || options.FirstAlbumYearMax != 2001 {
		t.Errorf("first album year bounds = %d-%d, want 1972-2001", options.FirstAlbumYearMin, options.FirstAlbumYearMax)
	}
	if options.MemberCountMin != 1 || options.MemberCountMax != 2 {
		t.Errorf("member count bounds = %d-%d, want 1-2", options.MemberCountMin, options.MemberCountMax)
	}
	if !reflect.DeepEqual(options.Locations, []string{"berlin-germany", "london-uk", "scotland-uk"}) {
		t.Errorf("locations = %v, want sorted distinct catalog locations", options.Locations)
	}
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func TestArtistDetailIncludesRelatedConcertData(t *testing.T) {
	_, mux := testServer(t)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/artists/1", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	for _, expected := range []string{"Freddie Mercury", "london-uk", "10-05-2007"} {
		if !strings.Contains(response.Body.String(), expected) {
			t.Errorf("artist detail does not contain %q", expected)
		}
	}
	var details artistDetail
	if err := json.Unmarshal(response.Body.Bytes(), &details); err != nil {
		t.Fatal(err)
	}
	if len(details.Locations) != 1 || details.Locations[0].GeocodingStatus != "unavailable" {
		t.Errorf("location geocoding fallback = %#v; want concert details with unavailable map status", details.Locations)
	}
}

func TestEmbeddedMapUsesMarkersAndHistoricalRoute(t *testing.T) {
	script, err := assets.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		`L.marker([place.latitude, place.longitude])`,
		`L.polyline(points`,
		`maxZoom: 5`,
		`Concert map · historical route`,
		`OpenStreetMap Nominatim`,
	} {
		if !strings.Contains(string(script), expected) {
			t.Errorf("map client does not include %q", expected)
		}
	}
}

func TestFirstConcertDateAndChronologicalOrdering(t *testing.T) {
	first, ok := firstConcertDate([]string{"*10-05-2007", "01-01-2005"})
	if !ok || first.Format("02-01-2006") != "01-01-2005" {
		t.Errorf("firstConcertDate returned %v, %t; want 01-01-2005, true", first, ok)
	}
	if _, ok := firstConcertDate([]string{"TBA", "invalid"}); ok {
		t.Error("firstConcertDate accepted invalid date values")
	}
}

func TestGeocoderLookup(t *testing.T) {
	requests := make(chan *http.Request, 1)
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r.Clone(context.Background())
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"lat":"51.5072","lon":"-0.1276"}]`))
	}))
	defer service.Close()

	geo := newGeocoder(service.URL, 0)
	latitude, longitude, err := geo.lookup(context.Background(), "london-uk")
	if err != nil {
		t.Fatalf("lookup returned error: %v", err)
	}
	if latitude != 51.5072 || longitude != -0.1276 {
		t.Errorf("coordinates = %f,%f; want 51.5072,-0.1276", latitude, longitude)
	}
	request := <-requests
	if request.URL.Query().Get("q") != "london, uk" ||
		request.URL.Query().Get("format") != "jsonv2" ||
		request.URL.Query().Get("limit") != "1" {
		t.Errorf("geocoding query = %v; want normalized one-result search", request.URL.Query())
	}
	if request.Header.Get("User-Agent") == "" {
		t.Error("geocoding request has no identifying User-Agent")
	}
}

func TestGeocoderPersistsSuccessfulResults(t *testing.T) {
	requests := make(chan struct{}, 1)
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests <- struct{}{}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"lat":"51.5072","lon":"-0.1276"}]`))
	}))
	defer service.Close()

	cachePath := filepath.Join(t.TempDir(), "geocodes.json")
	geo := newGeocoder(service.URL, 0)
	geo.cachePath = cachePath
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		geo.run(ctx)
		close(done)
	}()
	defer func() {
		cancel()
		<-done
	}()

	if _, err := geo.getOrQueue("london-uk"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		result, err := geo.getOrQueue("london-uk")
		if err != nil {
			t.Fatal(err)
		}
		if result.status == "ready" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("geocoding did not complete before timeout")
		}
		time.Sleep(5 * time.Millisecond)
	}
	for {
		if _, err := os.Stat(cachePath); err == nil {
			break
		} else if !os.IsNotExist(err) {
			t.Fatalf("check persisted cache: %v", err)
		}
		if time.Now().After(deadline) {
			t.Fatal("geocode cache was not written before timeout")
		}
		time.Sleep(5 * time.Millisecond)
	}

	reloaded := newGeocoder(service.URL, 0)
	reloaded.cachePath = cachePath
	if err := reloaded.loadCache(); err != nil {
		t.Fatalf("load persisted cache: %v", err)
	}
	result, err := reloaded.getOrQueue("london-uk")
	if err != nil {
		t.Fatal(err)
	}
	if result.status != "ready" || result.latitude != 51.5072 || result.longitude != -0.1276 {
		t.Errorf("reloaded result = %#v; want cached London coordinates", result)
	}
	if len(reloaded.jobs) != 0 {
		t.Error("cached location was queued for another lookup")
	}
	select {
	case <-requests:
	default:
		t.Error("geocoding service was not called")
	}
}

func TestGeocoderLookupRejectsInvalidResponses(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
	}{
		{"service error", http.StatusBadGateway, `{}`},
		{"invalid JSON", http.StatusOK, `not json`},
		{"empty results", http.StatusOK, `[]`},
		{"invalid latitude", http.StatusOK, `[{"lat":"91","lon":"0"}]`},
		{"invalid longitude", http.StatusOK, `[{"lat":"0","lon":"181"}]`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.statusCode)
				_, _ = w.Write([]byte(test.body))
			}))
			defer service.Close()
			geo := newGeocoder(service.URL, 0)
			if _, _, err := geo.lookup(context.Background(), "somewhere"); err == nil {
				t.Fatal("lookup succeeded for invalid geocoding response")
			}
		})
	}
}

func TestArtistDetailGeocodesLocationsInHistoricalOrder(t *testing.T) {
	requests := make(chan string, 2)
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			t.Error("geocoding request has no User-Agent")
		}
		requests <- r.URL.Query().Get("q")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"lat":"40.0","lon":"-70.0"}]`))
	}))
	defer service.Close()

	app, mux := testServer(t)
	app.state.data.locations[1] = append(app.state.data.locations[1], "berlin-germany")
	app.state.data.relations[1]["berlin-germany"] = []string{"*05-01-2004"}
	geo := newGeocoder(service.URL, 0)
	app.geocoder = geo
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go geo.run(ctx)

	requestDetail := func() artistDetail {
		t.Helper()
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/artists/1", nil))
		if response.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
		}
		var details artistDetail
		if err := json.Unmarshal(response.Body.Bytes(), &details); err != nil {
			t.Fatal(err)
		}
		return details
	}

	details := requestDetail()
	deadline := time.Now().Add(2 * time.Second)
	for details.GeocodingPending && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
		details = requestDetail()
	}
	if details.GeocodingPending {
		t.Fatal("geocoding did not complete before timeout")
	}
	if len(details.Locations) != 2 || details.Locations[0].Location != "berlin-germany" || details.Locations[1].Location != "london-uk" {
		t.Errorf("locations = %#v; want Berlin before London by concert date", details.Locations)
	}
	for _, place := range details.Locations {
		if place.GeocodingStatus != "ready" || place.Latitude == nil || place.Longitude == nil {
			t.Errorf("location was not geocoded: %#v", place)
		}
	}
	queried := map[string]bool{<-requests: true, <-requests: true}
	if !queried["berlin, germany"] || !queried["london, uk"] {
		t.Errorf("geocoding requests = %v; want both artist locations", queried)
	}
	_ = requestDetail()
	select {
	case query := <-requests:
		t.Errorf("cached location was geocoded again: %q", query)
	default:
	}
}

func TestHandlersRejectWrongMethodsAndUnknownArtists(t *testing.T) {
	_, mux := testServer(t)
	for _, test := range []struct {
		path   string
		method string
		status int
	}{
		{"/api/artists", http.MethodPost, http.StatusMethodNotAllowed},
		{"/api/filter-options", http.MethodPost, http.StatusMethodNotAllowed},
		{"/api/artists/99", http.MethodGet, http.StatusNotFound},
		{"/api/artists/nope", http.MethodGet, http.StatusNotFound},
		{"/", http.MethodPost, http.StatusMethodNotAllowed},
		{"/about", http.MethodPost, http.StatusMethodNotAllowed},
		{"/docs", http.MethodPost, http.StatusMethodNotAllowed},
		{"/healthz", http.MethodPost, http.StatusMethodNotAllowed},
	} {
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(test.method, test.path, nil))
		if response.Code != test.status {
			t.Errorf("%s %s returned %d, want %d", test.method, test.path, response.Code, test.status)
		}
	}
}
