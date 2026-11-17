package main

import (
	"html/template"
	"net/http"
	"sync"
	"time"
)

const maxAPIResponseBytes = 16 << 20

// Artist holds the core data for a band or musician.
type Artist struct {
	ID           int      `json:"id"`
	Image        string   `json:"image"`
	Name         string   `json:"name"`
	Members      []string `json:"members"`
	CreationDate int      `json:"creationDate"`
	FirstAlbum   string   `json:"firstAlbum"`
}

// indexedLocations maps artist IDs to their concert locations.
type indexedLocations struct {
	Index []struct {
		ID        int      `json:"id"`
		Locations []string `json:"locations"`
	} `json:"index"`
}

// indexedDates maps artist IDs to their concert dates.
type indexedDates struct {
	Index []struct {
		ID    int      `json:"id"`
		Dates []string `json:"dates"`
	} `json:"index"`
}

// indexedRelations maps artist IDs to locations and their concert dates.
type indexedRelations struct {
	Index []struct {
		ID             int                 `json:"id"`
		DatesLocations map[string][]string `json:"datesLocations"`
	} `json:"index"`
}

// concert combines a location's dates with its geocoding data.
type concert struct {
	Location        string   `json:"location"`
	Dates           []string `json:"dates"`
	Latitude        *float64 `json:"latitude,omitempty"`
	Longitude       *float64 `json:"longitude,omitempty"`
	GeocodingStatus string   `json:"geocodingStatus"`
}

// artistDetail combines an artist with concert locations and dates.
type artistDetail struct {
	Artist
	Locations        []concert `json:"locations"`
	Dates            []string  `json:"dates"`
	GeocodingPending bool      `json:"geocodingPending"`
}

// geocodeResult stores the coordinates and status for a geocoded location.
type geocodeResult struct {
	latitude  float64
	longitude float64
	status    string
}

// geocoder looks up location coordinates and caches the results.
type geocoder struct {
	client   *http.Client
	endpoint string
	interval time.Duration
	mu       sync.RWMutex
	results  map[string]geocodeResult
	jobs     chan string
	lastCall time.Time
}

// searchMatch pairs an artist with the fields matching a search query.
type searchMatch struct {
	Artist  Artist   `json:"artist"`
	Matches []string `json:"matches"`
}

// artistSearchResults groups artist search matches by category.
type artistSearchResults struct {
	Artists      []Artist      `json:"artists"`
	Members      []searchMatch `json:"members"`
	Locations    []searchMatch `json:"locations"`
	Dates        []searchMatch `json:"dates"`
	Details      []searchMatch `json:"details"`
	TotalArtists int           `json:"totalArtists"`
}

// filterOptions describes the available ranges and locations for filtering artists.
type filterOptions struct {
	CreationYearMin   int      `json:"creationYearMin"`
	CreationYearMax   int      `json:"creationYearMax"`
	FirstAlbumYearMin int      `json:"firstAlbumYearMin"`
	FirstAlbumYearMax int      `json:"firstAlbumYearMax"`
	MemberCountMin    int      `json:"memberCountMin"`
	MemberCountMax    int      `json:"memberCountMax"`
	Locations         []string `json:"locations"`
}

// intRange represents an optional inclusive range of integers.
type intRange struct {
	min *int
	max *int
}

// artistFilters stores the criteria applied when selecting artists.
type artistFilters struct {
	creationYears intRange
	albumYears    intRange
	memberCounts  intRange
	locations     map[string]struct{}
}

// catalog stores artist data and its associated locations, dates, and relations.
type catalog struct {
	artists   []Artist
	locations map[int][]string
	dates     map[int][]string
	relations map[int]map[string][]string
}

// appState holds the catalog and its refresh status.
type appState struct {
	mu         sync.RWMutex
	data       *catalog
	refreshing bool
	lastError  string
}

// pageData provides the values rendered by the page templates.
type pageData struct {
	Artists []Artist
	Error   string
	APIBase string
	Title   string
}

// server handles HTTP requests and coordinates application services.
type server struct {
	state       *appState
	apiBase     string
	client      *http.Client
	templates   *template.Template
	refreshJobs chan struct{}
	geocoder    *geocoder
}
