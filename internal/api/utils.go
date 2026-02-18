package api

import (
	"encoding/json"
	"math"
	"net/http"
	"strconv"
	"strings"
)

func respondJSON(w http.ResponseWriter, code int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(payload)
}

func trimZero(v float64) string {
	if math.Mod(v, 1) == 0 {
		return strconv.Itoa(int(v))
	}
	return strconv.FormatFloat(v, 'f', 2, 64)
}

func capitalizeRole(role string) string {
	if role == "" {
		return ""
	}
	return strings.ToUpper(role[:1]) + role[1:]
}

func parsePagination(r *http.Request) (page int, pageSize int) {
	page = 1
	pageSize = 10

	if v := strings.TrimSpace(r.URL.Query().Get("page")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			page = n
		}
	}
	if v := strings.TrimSpace(r.URL.Query().Get("pageSize")); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			if n < 1 {
				n = 1
			}
			if n > 100 {
				n = 100
			}
			pageSize = n
		}
	}
	return page, pageSize
}

func parseSearch(r *http.Request) string {
	return strings.TrimSpace(r.URL.Query().Get("q"))
}
