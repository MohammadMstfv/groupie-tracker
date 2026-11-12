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
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
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
	Location        string   `json:"location"`
	Dates           []string `json:"dates"`
	Latitude        *float64 `json:"latitude,omitempty"`
	Longitude       *float64 `json:"longitude,omitempty"`
	GeocodingStatus string   `json:"geocodingStatus"`
}

type artistDetail struct {
	Artist
	Locations        []concert `json:"locations"`
	Dates            []string  `json:"dates"`
	GeocodingPending bool      `json:"geocodingPending"`
}

type geocodeResult struct {
	latitude  float64
	longitude float64
	status    string
}

type geocoder struct {
	client    *http.Client
	endpoint  string
	interval  time.Duration
	mu        sync.RWMutex
	results   map[string]geocodeResult
	jobs      chan string
	lastCall  time.Time
	cachePath string
}

type persistedGeocodeResult struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
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
	geocoder    *geocoder
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
	cachePath := strings.TrimSpace(os.Getenv("GEOCODE_CACHE_PATH"))
	if cachePath == "" {
		cachePath = "data/geocode-cache.json"
	}

	templates, err := template.ParseFS(assets, "templates/*.html")
	if err != nil {
		log.Fatalf("parse page templates: %v", err)
	}

	geocoder := newGeocoder("https://nominatim.openstreetmap.org/search", time.Second)
	geocoder.cachePath = cachePath
	if err := geocoder.loadCache(); err != nil {
		log.Printf("load geocode cache %q: %v", cachePath, err)
	}
	app := &server{
		state:       &appState{},
		apiBase:     apiBase,
		client:      &http.Client{Timeout: 15 * time.Second},
		templates:   templates,
		refreshJobs: make(chan struct{}, 1),
		geocoder:    geocoder,
	}
	go app.geocoder.run(context.Background())
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
			place := concert{
				Location:        location,
				Dates:           append([]string(nil), data.relations[id][location]...),
				GeocodingStatus: "pending",
			}
			if s.geocoder != nil {
				geocoded, err := s.geocoder.getOrQueue(location)
				if err != nil {
					log.Printf("queue geocode for %q: %v", location, err)
					place.GeocodingStatus = "unavailable"
				} else {
					place.GeocodingStatus = geocoded.status
					if geocoded.status == "ready" {
						latitude, longitude := geocoded.latitude, geocoded.longitude
						place.Latitude, place.Longitude = &latitude, &longitude
					}
				}
			} else {
				place.GeocodingStatus = "unavailable"
			}
			result.Locations = append(result.Locations, place)
		}
		sort.SliceStable(result.Locations, func(i, j int) bool {
			left, leftOK := firstConcertDate(result.Locations[i].Dates)
			right, rightOK := firstConcertDate(result.Locations[j].Dates)
			if leftOK != rightOK {
				return leftOK
			}
			return leftOK && left.Before(right)
		})
		for _, place := range result.Locations {
			if place.GeocodingStatus == "pending" {
				result.GeocodingPending = true
				break
			}
		}
		writeJSON(w, http.StatusOK, result)
		return
	}
	http.NotFound(w, r)
}

func firstConcertDate(dates []string) (time.Time, bool) {
	var earliest time.Time
	found := false
	for _, value := range dates {
		date, err := time.Parse("02-01-2006", strings.TrimPrefix(value, "*"))
		if err != nil {
			continue
		}
		if !found || date.Before(earliest) {
			earliest, found = date, true
		}
	}
	return earliest, found
}

func newGeocoder(endpoint string, interval time.Duration) *geocoder {
	return &geocoder{
		client:   &http.Client{Timeout: 10 * time.Second},
		endpoint: endpoint,
		interval: interval,
		results:  make(map[string]geocodeResult),
		jobs:     make(chan string, 2048),
	}
}

func (g *geocoder) getOrQueue(location string) (geocodeResult, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if cached, exists := g.results[location]; exists {
		return cached, nil
	}
	pending := geocodeResult{status: "pending"}
	select {
	case g.jobs <- location:
		g.results[location] = pending
		return pending, nil
	default:
		return geocodeResult{}, errors.New("geocoding queue is full")
	}
}

func (g *geocoder) run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case location := <-g.jobs:
			if err := g.waitForRateLimit(ctx); err != nil {
				return
			}
			latitude, longitude, err := g.lookup(ctx, location)
			result := geocodeResult{status: "ready", latitude: latitude, longitude: longitude}
			if err != nil {
				log.Printf("geocode %q: %v", location, err)
				result.status = "unavailable"
			}
			g.mu.Lock()
			g.results[location] = result
			g.mu.Unlock()
			if result.status == "ready" {
				if err := g.saveCache(); err != nil {
					log.Printf("save geocode cache %q: %v", g.cachePath, err)
				}
			}
		}
	}
}

func (g *geocoder) loadCache() error {
	if g.cachePath == "" {
		return nil
	}
	data, err := os.ReadFile(g.cachePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read cache: %w", err)
	}
	var cached map[string]persistedGeocodeResult
	if err := json.Unmarshal(data, &cached); err != nil {
		return fmt.Errorf("decode cache: %w", err)
	}
	results := make(map[string]geocodeResult, len(cached))
	for location, coordinates := range cached {
		if strings.TrimSpace(location) == "" ||
			math.IsNaN(coordinates.Latitude) || math.IsInf(coordinates.Latitude, 0) ||
			coordinates.Latitude < -90 || coordinates.Latitude > 90 ||
			math.IsNaN(coordinates.Longitude) || math.IsInf(coordinates.Longitude, 0) ||
			coordinates.Longitude < -180 || coordinates.Longitude > 180 {
			return fmt.Errorf("cache contains invalid coordinates for %q", location)
		}
		results[location] = geocodeResult{
			latitude: coordinates.Latitude, longitude: coordinates.Longitude, status: "ready",
		}
	}
	g.mu.Lock()
	for location, result := range results {
		if _, exists := g.results[location]; !exists {
			g.results[location] = result
		}
	}
	g.mu.Unlock()
	return nil
}

func (g *geocoder) saveCache() error {
	if g.cachePath == "" {
		return nil
	}
	g.mu.RLock()
	cached := make(map[string]persistedGeocodeResult)
	for location, result := range g.results {
		if result.status == "ready" {
			cached[location] = persistedGeocodeResult{
				Latitude: result.latitude, Longitude: result.longitude,
			}
		}
	}
	g.mu.RUnlock()

	directory := filepath.Dir(g.cachePath)
	if err := os.MkdirAll(directory, 0755); err != nil {
		return fmt.Errorf("create cache directory: %w", err)
	}
	file, err := os.CreateTemp(directory, filepath.Base(g.cachePath)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary cache: %w", err)
	}
	temporaryPath := file.Name()
	defer os.Remove(temporaryPath)
	if err := file.Chmod(0600); err != nil {
		_ = file.Close()
		return fmt.Errorf("set cache permissions: %w", err)
	}
	if err := json.NewEncoder(file).Encode(cached); err != nil {
		_ = file.Close()
		return fmt.Errorf("encode cache: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync cache: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close cache: %w", err)
	}
	if err := os.Rename(temporaryPath, g.cachePath); err != nil {
		return fmt.Errorf("replace cache: %w", err)
	}
	return nil
}

func (g *geocoder) waitForRateLimit(ctx context.Context) error {
	g.mu.Lock()
	delay := time.Until(g.lastCall.Add(g.interval))
	if delay < 0 {
		delay = 0
	}
	g.mu.Unlock()
	if delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}
	g.mu.Lock()
	g.lastCall = time.Now()
	g.mu.Unlock()
	return nil
}

func (g *geocoder) lookup(ctx context.Context, location string) (float64, float64, error) {
	endpoint, err := url.Parse(g.endpoint)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid geocoding endpoint: %w", err)
	}
	query := endpoint.Query()
	query.Set("q", strings.ReplaceAll(strings.ReplaceAll(location, "_", " "), "-", ", "))
	query.Set("format", "jsonv2")
	query.Set("limit", "1")
	endpoint.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return 0, 0, fmt.Errorf("create geocoding request: %w", err)
	}
	request.Header.Set("User-Agent", "GroupieTracker/1.0 (educational concert map)")
	response, err := g.client.Do(request)
	if err != nil {
		return 0, 0, fmt.Errorf("request geocoding service: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return 0, 0, fmt.Errorf("geocoding service returned HTTP %d", response.StatusCode)
	}
	var results []struct {
		Latitude  string `json:"lat"`
		Longitude string `json:"lon"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&results); err != nil {
		return 0, 0, fmt.Errorf("decode geocoding response: %w", err)
	}
	if len(results) == 0 {
		return 0, 0, errors.New("geocoding service found no matching location")
	}
	latitude, err := strconv.ParseFloat(results[0].Latitude, 64)
	if err != nil || math.IsNaN(latitude) || math.IsInf(latitude, 0) || latitude < -90 || latitude > 90 {
		return 0, 0, errors.New("geocoding service returned an invalid latitude")
	}
	longitude, err := strconv.ParseFloat(results[0].Longitude, 64)
	if err != nil || math.IsNaN(longitude) || math.IsInf(longitude, 0) || longitude < -180 || longitude > 180 {
		return 0, 0, errors.New("geocoding service returned an invalid longitude")
	}
	return latitude, longitude, nil
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
