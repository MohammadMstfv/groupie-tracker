package main

import (
	"log"
	"net/http"
)

// handleHome renders the artist home page.
func (s *server) handleHome(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		s.renderNotFound(w)
		return //HTTP 404
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return //HTTP 405
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

// renderNotFound displays the not-found page HTTP 404.
func (s *server) renderNotFound(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	if err := s.templates.ExecuteTemplate(w, "not-found.html", pageData{APIBase: s.apiBase}); err != nil {
		log.Printf("render not found page: %v", err)
	}
}

// handleAbout renders the about page.
func (s *server) handleAbout(w http.ResponseWriter, r *http.Request) {
	s.renderInfoPage(w, r, "about.html", "About")
}

// handleDocs renders the API documentation page.
func (s *server) handleDocs(w http.ResponseWriter, r *http.Request) {
	s.renderInfoPage(w, r, "docs.html", "Docs")
}

// handleHealth reports whether the server is responding.
func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}

// renderInfoPage renders a named informational page.
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
