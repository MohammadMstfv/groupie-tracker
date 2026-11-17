package main

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// handleFilterOptions returns the available artist filter values.
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

// getFilterOptions derives filter ranges and locations from the catalog.
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

// parseArtistFilters converts URL query parameters into artist filters.
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

// parseIntRange parses and validates an optional integer range.
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

// parseOptionalInt parses a query parameter as an optional integer.
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

// filterArtists returns artists that satisfy all supplied filters.
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

// inRange reports whether value falls within the optional bounds.
func inRange(value int, bounds intRange) bool {
	return (bounds.min == nil || value >= *bounds.min) &&
		(bounds.max == nil || value <= *bounds.max)
}

// hasAnyLocation reports whether any location is among the selected values.
func hasAnyLocation(locations []string, selected map[string]struct{}) bool {
	for _, location := range locations {
		if _, ok := selected[normalizeSearch(location)]; ok {
			return true
		}
	}
	return false
}

// firstAlbumYear extracts the year from an album date.
func firstAlbumYear(firstAlbum string) (int, bool) {
	date, err := time.Parse("02-01-2006", firstAlbum)
	if err != nil {
		return 0, false
	}
	return date.Year(), true
}
