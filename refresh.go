package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"time"
)

// handleRefresh starts an asynchronous catalog refresh.
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

// handleStatus returns the catalog readiness and refresh status.
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

// currentCatalog returns the current catalog or writes an unavailable response.
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

// enqueueRefresh schedules a catalog refresh unless one is already running.
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

// runRefreshWorker processes catalog refresh jobs and updates application state.
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

// fetchCatalog loads and validates the artist API datasets.
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

// fetchJSON fetches a bounded API response and decodes its JSON into destination.
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
