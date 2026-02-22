package api

import (
	"io"
	"net/http"
	"net/url"
	"time"
)

func (s *Server) handleMLInsights(w http.ResponseWriter, r *http.Request) {
	if s.mlBaseURL == "" {
		respondJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "ml service is not configured"})
		return
	}
	days := r.URL.Query().Get("days")
	targetURL := s.mlBaseURL + "/insights"
	if days != "" {
		targetURL += "?days=" + url.QueryEscape(days)
	}

	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Get(targetURL)
	if err != nil {
		respondJSON(w, http.StatusBadGateway, map[string]string{"error": "failed to reach ml service"})
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(body)
}

func (s *Server) handleMLTrain(w http.ResponseWriter, r *http.Request) {
	if s.mlBaseURL == "" {
		respondJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "ml service is not configured"})
		return
	}
	days := r.URL.Query().Get("days")
	targetURL := s.mlBaseURL + "/train"
	if days != "" {
		targetURL += "?days=" + url.QueryEscape(days)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest(http.MethodPost, targetURL, nil)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create request"})
		return
	}
	resp, err := client.Do(req)
	if err != nil {
		respondJSON(w, http.StatusBadGateway, map[string]string{"error": "failed to reach ml service"})
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(body)
}
