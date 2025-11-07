package main

import (
	"encoding/json"
	"net/http"
	"strings"
)

func setupRouter() http.Handler {
	mux := http.NewServeMux()

	// WebSocket endpoint
	mux.HandleFunc("/ws", wsHandler)

	// REST endpoints
	mux.HandleFunc("/health", healthHandler)
	mux.HandleFunc("/stats", statsHandler)
	mux.HandleFunc("/topics", topicsCollectionHandler) // POST /topics, GET /topics
	mux.HandleFunc("/topics/", topicsItemHandler)      // DELETE /topics/{name}

	// default
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})


	protected := requireAPIKey(mux)
	return loggingMiddleware(corsMiddleware(protected))
}

func topicsCollectionHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var req struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Name) == "" {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "name is required")
			return
		}
		if err := globalTopics.CreateTopic(req.Name); err != nil {
			if err == ErrTopicExists {
				writeError(w, http.StatusConflict, "CONFLICT", "topic already exists")
				return
			}
			writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"status": "created", "topic": req.Name})
	case http.MethodGet:
		topics := globalTopics.ListTopics()
		writeJSON(w, http.StatusOK, map[string]any{"topics": topics})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func topicsItemHandler(w http.ResponseWriter, r *http.Request) {
	// /topics/{name}
	if r.Method != http.MethodDelete {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/topics/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "topic name required")
		return
	}
	name := parts[0]
	if err := globalTopics.DeleteTopic(name); err != nil {
		if err == ErrTopicNotFound {
			writeError(w, http.StatusNotFound, "TOPIC_NOT_FOUND", "topic not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "deleted", "topic": name})
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, globalTopics.Health())
}

func statsHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, globalTopics.Stats())
}

func requireAPIKey(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

        // Allow unauthenticated health & root requests (needed for Render)
        if r.URL.Path == "/health" || r.URL.Path == "/" {
            next.ServeHTTP(w, r)
            return
        }

        expected := getEnv("API_KEY", "")
        if expected != "" && r.Header.Get("X-API-Key") != expected {
            http.Error(w, "unauthorized", http.StatusUnauthorized)
            return
        }

        next.ServeHTTP(w, r)
    })
}

