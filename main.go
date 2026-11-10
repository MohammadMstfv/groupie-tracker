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

type searchMatch struct {
	Artist  Artist   `json:"artist"`
	Matches []string `json:"matches"`
}

type artistSearchResults struct {
	Artists      []Artist      `json:"artists"`
	Members      []searchMatch `json:"members"`
	Locations    []searchMatch `json:"locations"`
	Dates        []searchMatch `json:"dates"`
	Details      []searchMatch `json:"details"`
	TotalArtists int           `json:"totalArtists"`
}

type filterOptions struct {
	CreationYearMin   int      `json:"creationYearMin"`
	CreationYearMax   int      `json:"creationYearMax"`
	FirstAlbumYearMin int      `json:"firstAlbumYearMin"`
	FirstAlbumYearMax int      `json:"firstAlbumYearMax"`
	MemberCountMin    int      `json:"memberCountMin"`
	MemberCountMax    int      `json:"memberCountMax"`
	Locations         []string `json:"locations"`
}

type intRange struct {
	min *int
	max *int
}

type artistFilters struct {
	creationYears intRange
	albumYears    intRange
	memberCounts  intRange
	locations     map[string]struct{}
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
	APIBase string
	Title   string
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
	mux.HandleFunc("/about", app.handleAbout)
	mux.HandleFunc("/docs", app.handleDocs)
	mux.HandleFunc("/healthz", app.handleHealth)
	mux.HandleFunc("/api/artists", app.handleArtists)
	mux.HandleFunc("/api/filter-options", app.handleFilterOptions)
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
		s.renderNotFound(w)
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
	if err := s.templates.ExecuteTemplate(w, "index.html", pageData{
		Artists: artists,
		Error:   errMsg,
		APIBase: s.apiBase,
	}); err != nil {
		log.Printf("render home page: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

func (s *server) renderNotFound(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	if err := s.templates.ExecuteTemplate(w, "not-found.html", pageData{APIBase: s.apiBase}); err != nil {
		log.Printf("render not found page: %v", err)
	}
}

func (s *server) handleAbout(w http.ResponseWriter, r *http.Request) {
	s.renderInfoPage(w, r, "about.html", "About")
}

func (s *server) handleDocs(w http.ResponseWriter, r *http.Request) {
	s.renderInfoPage(w, r, "docs.html", "Docs")
}

func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}

func (s *server) renderInfoPage(w http.ResponseWriter, r *http.Request, templateName, title string) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.templates.ExecuteTemplate(w, templateName, pageData{
		APIBase: s.apiBase,
		Title:   title,
	}); err != nil {
		log.Printf("render %s page: %v", title, err)
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

	filters, err := parseArtistFilters(r.URL.Query())
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	filteredData := *data
	filteredData.artists = filterArtists(data, filters)
	writeJSON(w, http.StatusOK, searchArtists(&filteredData, strings.TrimSpace(r.URL.Query().Get("q"))))
}

func (s *server) handleFilterOptions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	data := s.currentCatalog(w)
	if data == nil {
		return
	}
	writeJSON(w, http.StatusOK, getFilterOptions(data))
}

func getFilterOptions(data *catalog) filterOptions {
	options := filterOptions{
		Locations:         make([]string, 0),
		MemberCountMin:    int(^uint(0) >> 1),
		CreationYearMin:   int(^uint(0) >> 1),
		FirstAlbumYearMin: int(^uint(0) >> 1),
	}
	locationSet := make(map[string]struct{})
	for _, artist := range data.artists {
		if artist.CreationDate < options.CreationYearMin {
			options.CreationYearMin = artist.CreationDate
		}
		if artist.CreationDate > options.CreationYearMax {
			options.CreationYearMax = artist.CreationDate
		}
		memberCount := len(artist.Members)
		if memberCount < options.MemberCountMin {
			options.MemberCountMin = memberCount
		}
		if memberCount > options.MemberCountMax {
			options.MemberCountMax = memberCount
		}
		if year, ok := firstAlbumYear(artist.FirstAlbum); ok {
			if year < options.FirstAlbumYearMin {
				options.FirstAlbumYearMin = year
			}
			if year > options.FirstAlbumYearMax {
				options.FirstAlbumYearMax = year
			}
		}
		for _, location := range data.locations[artist.ID] {
			locationSet[location] = struct{}{}
		}
	}
	for location := range locationSet {
		options.Locations = append(options.Locations, location)
	}
	sort.Strings(options.Locations)
	if len(data.artists) == 0 {
		options.CreationYearMin = 0
		options.FirstAlbumYearMin = 0
		options.MemberCountMin = 0
	}
	if options.FirstAlbumYearMin == int(^uint(0)>>1) {
		options.FirstAlbumYearMin = 0
	}
	return options
}

func parseArtistFilters(query map[string][]string) (artistFilters, error) {
	creationYears, err := parseIntRange(query, "creationMin", "creationMax", "creation year")
	if err != nil {
		return artistFilters{}, err
	}
	albumYears, err := parseIntRange(query, "albumMin", "albumMax", "first album year")
	if err != nil {
		return artistFilters{}, err
	}
	memberCounts, err := parseIntRange(query, "membersMin", "membersMax", "member count")
	if err != nil {
		return artistFilters{}, err
	}
	locations := make(map[string]struct{})
	for _, location := range query["location"] {
		location = normalizeSearch(location)
		if location != "" {
			locations[location] = struct{}{}
		}
	}
	return artistFilters{
		creationYears: creationYears,
		albumYears:    albumYears,
		memberCounts:  memberCounts,
		locations:     locations,
	}, nil
}

func parseIntRange(query map[string][]string, minKey, maxKey, label string) (intRange, error) {
	min, err := parseOptionalInt(query, minKey, label+" minimum")
	if err != nil {
		return intRange{}, err
	}
	max, err := parseOptionalInt(query, maxKey, label+" maximum")
	if err != nil {
		return intRange{}, err
	}
	if min != nil && max != nil && *min > *max {
		return intRange{}, fmt.Errorf("%s minimum cannot be greater than its maximum", label)
	}
	return intRange{min: min, max: max}, nil
}

func parseOptionalInt(query map[string][]string, key, label string) (*int, error) {
	values := query[key]
	if len(values) > 1 {
		return nil, fmt.Errorf("%s must only be specified once", label)
	}
	if len(values) == 0 || strings.TrimSpace(values[0]) == "" {
		return nil, nil
	}
	value, err := strconv.Atoi(strings.TrimSpace(values[0]))
	if err != nil {
		return nil, fmt.Errorf("%s must be a whole number", label)
	}
	return &value, nil
}

func filterArtists(data *catalog, filters artistFilters) []Artist {
	results := make([]Artist, 0, len(data.artists))
	for _, artist := range data.artists {
		if !inRange(artist.CreationDate, filters.creationYears) ||
			!inRange(len(artist.Members), filters.memberCounts) {
			continue
		}
		if filters.albumYears.min != nil || filters.albumYears.max != nil {
			year, ok := firstAlbumYear(artist.FirstAlbum)
			if !ok || !inRange(year, filters.albumYears) {
				continue
			}
		}
		if len(filters.locations) > 0 && !hasAnyLocation(data.locations[artist.ID], filters.locations) {
			continue
		}
		results = append(results, artist)
	}
	return results
}

func inRange(value int, bounds intRange) bool {
	return (bounds.min == nil || value >= *bounds.min) &&
		(bounds.max == nil || value <= *bounds.max)
}

func hasAnyLocation(locations []string, selected map[string]struct{}) bool {
	for _, location := range locations {
		if _, ok := selected[normalizeSearch(location)]; ok {
			return true
		}
	}
	return false
}

func firstAlbumYear(firstAlbum string) (int, bool) {
	date, err := time.Parse("02-01-2006", firstAlbum)
	if err != nil {
		return 0, false
	}
	return date.Year(), true
}

func searchArtists(data *catalog, query string) artistSearchResults {
	results := artistSearchResults{
		Artists:      make([]Artist, 0),
		Members:      make([]searchMatch, 0),
		Locations:    make([]searchMatch, 0),
		Dates:        make([]searchMatch, 0),
		Details:      make([]searchMatch, 0),
		TotalArtists: len(data.artists),
	}
	if query == "" {
		results.Artists = append(results.Artists, data.artists...)
		return results
	}

	for _, artist := range data.artists {
		if searchContains(artist.Name, query) {
			results.Artists = append(results.Artists, artist)
		}

		members := make([]string, 0)
		for _, member := range artist.Members {
			if searchContains(member, query) {
				members = append(members, member)
			}
		}
		if len(members) > 0 {
			results.Members = append(results.Members, searchMatch{Artist: artist, Matches: members})
		}

		locations := make([]string, 0)
		for _, location := range data.locations[artist.ID] {
			if searchContains(location, query) {
				locations = append(locations, location)
			}
		}
		if len(locations) > 0 {
			results.Locations = append(results.Locations, searchMatch{Artist: artist, Matches: locations})
		}

		dates := make([]string, 0)
		for _, date := range data.dates[artist.ID] {
			if searchContains(strings.TrimPrefix(date, "*"), query) {
				dates = append(dates, date)
			}
		}
		if len(dates) > 0 {
			results.Dates = append(results.Dates, searchMatch{Artist: artist, Matches: dates})
		}

		details := make([]string, 0, 2)
		creationDate := strconv.Itoa(artist.CreationDate)
		if searchContains(creationDate, query) {
			details = append(details, "Started in "+creationDate)
		}
		if searchContains(artist.FirstAlbum, query) {
			details = append(details, "First album "+artist.FirstAlbum)
		}
		if len(details) > 0 {
			results.Details = append(results.Details, searchMatch{Artist: artist, Matches: details})
		}
	}
	return results
}

func searchContains(value, query string) bool {
	return strings.Contains(normalizeSearch(value), normalizeSearch(query))
}

func normalizeSearch(text string) string {
	text = strings.ToLower(strings.TrimSpace(text))
	text = strings.ReplaceAll(text, "_", " ")
	return strings.ReplaceAll(text, "-", " ")
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
