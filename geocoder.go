package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// newGeocoder creates a geocoder with a rate-limited lookup queue.
func newGeocoder(endpoint string, interval time.Duration) *geocoder {
	return &geocoder{
		client:   &http.Client{Timeout: 10 * time.Second},
		endpoint: endpoint,
		interval: interval,
		results:  make(map[string]geocodeResult),
		jobs:     make(chan string, 2048),
	}
}

// getOrQueue returns a cached result or queues a location for lookup.
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

// run processes queued geocoding lookups until the context is cancelled.
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
		}
	}
}

// waitForRateLimit delays until the next geocoding request is allowed.
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

// lookup requests and validates coordinates for a location.
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
