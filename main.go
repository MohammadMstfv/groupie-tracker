package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed templates/*.html static/*
var assets embed.FS

const maxAPIResponseBytes = 16 << 20

type Artist struct {
	ID           int      `json:"id"`
	Image        string   `json:"image"`
	Name         string   `json:"name"`
	Members      []string `json:"members"`
	CreationDate int      `json:"creationDate"`
	FirstAlbum   string   `json:"firstAlbum"`
}

type indexedLocations struct {
	Index []struct {
		ID        int      `json:"id"`
		Locations []string `json:"locations"`
	} `json:"index"`
}

type indexedDates struct {
	Index []struct {
		ID    int      `json:"id"`
		Dates []string `json:"dates"`
	} `json:"index"`
}

type indexedRelations struct {
	Index []struct {
		ID             int                 `json:"id"`
		DatesLocations map[string][]string `json:"datesLocations"`
	} `json:"index"`
}

type concert struct {
	Location string   `json:"location"`
	Dates    []string `json:"dates"`
}

type artistDetail struct {
	Artist
	Locations []concert `json:"locations"`
	Dates     []string  `json:"dates"`
}

type catalog struct {
	artists   []Artist
	locations map[int][]string
	dates     map[int][]string
	relations map[int]map[string][]string
}

type appState struct {
	mu         sync.RWMutex
	data       *catalog
	refreshing bool
	lastError  string
}

type pageData struct {
	Artists []Artist
	Error   string
}

type server struct {
	state       *appState
	apiBase     string
	client      *http.Client
	templates   *template.Template
	refreshJobs chan struct{}
}

func main() {
	apiBase := strings.TrimRight(os.Getenv("GROUPIE_API_BASE"), "/")
	if apiBase == "" {
		apiBase = "https://groupietrackers.01edu.ai/api"
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	templates, err := template.ParseFS(assets, "templates/*.html")
	if err != nil {
		log.Fatalf("parse page templates: %v", err)
	}

	app := &server{
		state:       &appState{},
		apiBase:     apiBase,
		client:      &http.Client{Timeout: 15 * time.Second},
		templates:   templates,
		refreshJobs: make(chan struct{}, 1),
	}
	go app.runRefreshWorker()
	app.enqueueRefresh()

	mux := http.NewServeMux()
	mux.HandleFunc("/", app.handleHome)
	mux.HandleFunc("/api/artists", app.handleArtists)
	mux.HandleFunc("/api/artists/", app.handleArtistDetail)
	mux.HandleFunc("/api/refresh", app.handleRefresh)
	mux.HandleFunc("/api/status", app.handleStatus)
	mux.Handle("/static/", http.FileServer(http.FS(assets)))

	httpServer := &http.Server{
		Addr:              ":" + port,
		Handler:           recoverPanics(mux),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      20 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	log.Printf("Groupie Tracker listening on http://localhost:%s", port)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("start HTTP server: %v", err)
	}
}

func recoverPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				log.Printf("panic serving %s %s: %v", r.Method, r.URL.Path, recovered)
				http.Error(w, "Internal server error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *server) handleHome(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}

	s.state.mu.RLock()
	var artists []Artist
	errMsg := s.state.lastError
	if s.state.data != nil {
		artists = s.state.data.artists
		errMsg = ""
	}
	s.state.mu.RUnlock()

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.templates.ExecuteTemplate(w, "index.html", pageData{Artists: artists, Error: errMsg}); err != nil {
		log.Printf("render home page: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

func (s *server) handleArtists(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	data := s.currentCatalog(w)
	if data == nil {
		return
	}

	query := strings.TrimSpace(r.URL.Query().Get("q"))
	results := make([]Artist, 0, len(data.artists))
	for _, artist := range data.artists {
		if query == "" || artistMatches(data, artist, query) {
			results = append(results, artist)
		}
	}
	writeJSON(w, http.StatusOK, results)
}

func artistMatches(data *catalog, artist Artist, query string) bool {
	query = strings.ToLower(query)
	if strings.Contains(strings.ToLower(artist.Name), query) {
		return true
	}
	for _, member := range artist.Members {
		if strings.Contains(strings.ToLower(member), query) {
			return true
		}
	}
	for _, location := range data.locations[artist.ID] {
		if strings.Contains(strings.ToLower(location), query) {
			return true
		}
	}
	return false
}

func (s *server) handleArtistDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	rawID := strings.TrimPrefix(r.URL.Path, "/api/artists/")
	if rawID == "" || strings.Contains(rawID, "/") {
		http.NotFound(w, r)
		return
	}
	id, err := strconv.Atoi(rawID)
	if err != nil || id < 1 {
		http.NotFound(w, r)
		return
	}
	data := s.currentCatalog(w)
	if data == nil {
		return
	}

	for _, artist := range data.artists {
		if artist.ID != id {
			continue
		}
		result := artistDetail{
			Artist: artist,
			Dates:  append([]string(nil), data.dates[id]...),
		}
		for _, location := range data.locations[id] {
			result.Locations = append(result.Locations, concert{
				Location: location,
				Dates:    append([]string(nil), data.relations[id][location]...),
			})
		}
		writeJSON(w, http.StatusOK, result)
		return
	}
	http.NotFound(w, r)
}

func (s *server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	if !s.enqueueRefresh() {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "A data refresh is already in progress."})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"message": "Data refresh started."})
}

func (s *server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	s.state.mu.RLock()
	ready, refreshing, lastError := s.state.data != nil, s.state.refreshing, s.state.lastError
	s.state.mu.RUnlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"ready": ready, "refreshing": refreshing, "error": lastError,
	})
}

func (s *server) currentCatalog(w http.ResponseWriter) *catalog {
	s.state.mu.RLock()
	data, lastError := s.state.data, s.state.lastError
	s.state.mu.RUnlock()
	if data == nil {
		message := "Artist data is temporarily unavailable."
		if lastError != "" {
			message = message + " " + lastError
		}
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": message})
	}
	return data
}

func (s *server) enqueueRefresh() bool {
	s.state.mu.Lock()
	defer s.state.mu.Unlock()
	if s.state.refreshing {
		return false
	}
	s.state.refreshing = true
	s.state.lastError = ""
	s.refreshJobs <- struct{}{}
	return true
}

func (s *server) runRefreshWorker() {
	for range s.refreshJobs {
		data, err := s.fetchCatalog()
		s.state.mu.Lock()
		if err != nil {
			s.state.lastError = err.Error()
		} else {
			s.state.data = data
			s.state.lastError = ""
		}
		s.state.refreshing = false
		s.state.mu.Unlock()
		if err != nil {
			log.Printf("refresh artist data: %v", err)
		} else {
			log.Printf("loaded %d artists", len(data.artists))
		}
	}
}

func (s *server) fetchCatalog() (*catalog, error) {
	type result struct {
		name string
		data any
		err  error
	}
	endpoints := []struct {
		name string
		dest any
	}{
		{name: "artists", dest: new([]Artist)},
		{name: "locations", dest: new(indexedLocations)},
		{name: "dates", dest: new(indexedDates)},
		{name: "relation", dest: new(indexedRelations)},
	}
	results := make(chan result, len(endpoints))
	for _, endpoint := range endpoints {
		go func(name string, dest any) {
			err := s.fetchJSON(name, dest)
			results <- result{name: name, data: dest, err: err}
		}(endpoint.name, endpoint.dest)
	}

	loaded := make(map[string]any, len(endpoints))
	for range endpoints {
		result := <-results
		if result.err != nil {
			return nil, result.err
		}
		loaded[result.name] = result.data
	}

	artists := *loaded["artists"].(*[]Artist)
	locations := make(map[int][]string)
	for _, item := range loaded["locations"].(*indexedLocations).Index {
		locations[item.ID] = item.Locations
	}
	dates := make(map[int][]string)
	for _, item := range loaded["dates"].(*indexedDates).Index {
		dates[item.ID] = item.Dates
	}
	relations := make(map[int]map[string][]string)
	for _, item := range loaded["relation"].(*indexedRelations).Index {
		relations[item.ID] = item.DatesLocations
	}
	for _, artist := range artists {
		if _, ok := locations[artist.ID]; !ok {
			return nil, fmt.Errorf("locations missing artist ID %d", artist.ID)
		}
		if _, ok := dates[artist.ID]; !ok {
			return nil, fmt.Errorf("dates missing artist ID %d", artist.ID)
		}
		if _, ok := relations[artist.ID]; !ok {
			return nil, fmt.Errorf("relations missing artist ID %d", artist.ID)
		}
	}
	sort.Slice(artists, func(i, j int) bool { return artists[i].Name < artists[j].Name })
	return &catalog{artists: artists, locations: locations, dates: dates, relations: relations}, nil
}

func (s *server) fetchJSON(endpoint string, destination any) error {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, s.apiBase+"/"+endpoint, nil)
	if err != nil {
		return fmt.Errorf("create %s API request: %w", endpoint, err)
	}
	response, err := s.client.Do(request)
	if err != nil {
		return fmt.Errorf("fetch %s: %w", endpoint, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("fetch %s: upstream returned HTTP %d", endpoint, response.StatusCode)
	}
	limited := io.LimitReader(response.Body, maxAPIResponseBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return fmt.Errorf("read %s response: %w", endpoint, err)
	}
	if len(body) > maxAPIResponseBytes {
		return fmt.Errorf("%s response exceeds %d bytes", endpoint, maxAPIResponseBytes)
	}
	if err := json.Unmarshal(body, destination); err != nil {
		return fmt.Errorf("decode %s response: %w", endpoint, err)
	}
	return nil
}

func methodNotAllowed(w http.ResponseWriter, allowed string) {
	w.Header().Set("Allow", allowed)
	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("write JSON response: %v", err)
	}
}
