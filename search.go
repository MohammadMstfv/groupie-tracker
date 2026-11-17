package main

import (
	"strconv"
	"strings"
)

// searchArtists groups catalog matches by artist name, members, locations, dates, and details.
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

// searchContains checks for a query within normalized text.
func searchContains(value, query string) bool {
	return strings.Contains(normalizeSearch(value), normalizeSearch(query))
}

// normalizeSearch standardizes text for case- and separator-insensitive matching.
func normalizeSearch(text string) string {
	text = strings.ToLower(strings.TrimSpace(text))
	text = strings.ReplaceAll(text, "_", " ")
	return strings.ReplaceAll(text, "-", " ")
}
