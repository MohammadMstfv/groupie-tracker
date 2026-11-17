package main

import (
	"encoding/json"
	"log"
	"net/http"
)

// recoverPanics converts handler panics into internal server errors.
func recoverPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				log.Printf("panic serving %s %s: %v", r.Method, r.URL.Path, recovered)
				http.Error(w, "Internal server error", http.StatusInternalServerError) //HTTP 500
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// methodNotAllowed rejects a request and advertises the allowed method.
func methodNotAllowed(w http.ResponseWriter, allowed string) {
	w.Header().Set("Allow", allowed)
	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

// writeJSON encodes value as a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("write JSON response: %v", err)
	}
}
