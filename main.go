package main

import (
	"context"
	"errors"
	"html/template"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

// main configures the application and starts the HTTP server.
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
		geocoder:    newGeocoder("https://nominatim.openstreetmap.org/search", time.Second),
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
