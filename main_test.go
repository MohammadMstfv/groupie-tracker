package main

import (
	"html/template"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testCatalog() *catalog {
	return &catalog{
		artists: []Artist{
			{
				ID: 1, Name: "Queen", Image: "/queen.jpg",
				Members: []string{"Freddie Mercury", "Brian May"}, CreationDate: 1970,
				FirstAlbum: "14-12-1973",
			},
		},
		locations: map[int][]string{1: {"london-uk"}},
		dates:     map[int][]string{1: {"10-05-2007"}},
		relations: map[int]map[string][]string{1: {"london-uk": {"10-05-2007"}}},
	}
}

func testServer(t *testing.T) (*server, *http.ServeMux) {
	t.Helper()
	templates, err := template.ParseFS(assets, "templates/*.html")
	if err != nil {
		t.Fatal(err)
	}
	app := &server{state: &appState{data: testCatalog()}, templates: templates}
	mux := http.NewServeMux()
	mux.HandleFunc("/", app.handleHome)
	mux.HandleFunc("/api/artists", app.handleArtists)
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
	for _, expected := range []string{"Queen", "1970", "14-12-1973"} {
		if !strings.Contains(response.Body.String(), expected) {
			t.Errorf("home page does not contain %q", expected)
		}
	}
}

func TestArtistSearchMatchesMembersAndLocations(t *testing.T) {
	_, mux := testServer(t)
	for _, query := range []string{"Freddie", "london"} {
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/artists?q="+query, nil))
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Queen") {
			t.Errorf("search %q returned status %d and body %s", query, response.Code, response.Body.String())
		}
	}
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
}

func TestHandlersRejectWrongMethodsAndUnknownArtists(t *testing.T) {
	_, mux := testServer(t)
	for _, test := range []struct {
		path   string
		method string
		status int
	}{
		{"/api/artists", http.MethodPost, http.StatusMethodNotAllowed},
		{"/api/artists/2", http.MethodGet, http.StatusNotFound},
		{"/api/artists/nope", http.MethodGet, http.StatusNotFound},
		{"/", http.MethodPost, http.StatusMethodNotAllowed},
	} {
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(test.method, test.path, nil))
		if response.Code != test.status {
			t.Errorf("%s %s returned %d, want %d", test.method, test.path, response.Code, test.status)
		}
	}
}
