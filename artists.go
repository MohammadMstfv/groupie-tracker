package main

import (
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// handleArtists returns the artists matching the request's filters and search query.
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

// handleArtistDetail returns an artist's concert and geocoding details.
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

// firstConcertDate finds the earliest parseable date in a list.
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
