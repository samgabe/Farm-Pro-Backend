package api

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func (s *Server) authRequired(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			respondJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing bearer token"})
			return
		}
		tokenString := strings.TrimPrefix(header, "Bearer ")
		claims := jwt.MapClaims{}
		token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (any, error) {
			if token.Method.Alg() != jwt.SigningMethodHS256.Alg() {
				return nil, errors.New("unexpected signing method")
			}
			return s.jwtSecret, nil
		})
		if err != nil || !token.Valid {
			respondJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid token"})
			return
		}

		uid, err := parseTokenUserID(claims["sub"])
		if err != nil {
			respondJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid token subject"})
			return
		}

		dbCtx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
		defer cancel()

		role, permissions, err := s.loadAuthContext(dbCtx, uid)
		if err != nil {
			respondJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid user session"})
			return
		}

		authCtx := context.WithValue(r.Context(), userIDContextKey, uid)
		authCtx = context.WithValue(authCtx, userRoleContextKey, role)
		authCtx = context.WithValue(authCtx, userPermissionsContextKey, permissions)
		next.ServeHTTP(w, r.WithContext(authCtx))
	})
}

func (s *Server) permissionRequired(next http.Handler, requiredPermission string) http.Handler {
	requiredPermission = strings.TrimSpace(requiredPermission)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requiredPermission == "" {
			next.ServeHTTP(w, r)
			return
		}

		permissions, ok := r.Context().Value(userPermissionsContextKey).(map[string]struct{})
		if !ok {
			respondJSON(w, http.StatusForbidden, map[string]string{"error": "missing permissions in auth context"})
			return
		}
		if _, allowed := permissions[requiredPermission]; !allowed {
			respondJSON(w, http.StatusForbidden, map[string]string{"error": "insufficient permissions"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			_, allowed := s.allowedOrigins[origin]
			if s.allowAnyOrigin || allowed {
				w.Header().Set("Access-Control-Allow-Origin", origin)
			}
		}
		w.Header().Set("Vary", "Origin")
		w.Header().Set("Vary", "Access-Control-Request-Method")
		w.Header().Set("Vary", "Access-Control-Request-Headers")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Accept, Origin")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Max-Age", "600")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func parseTokenUserID(raw any) (int64, error) {
	switch v := raw.(type) {
	case float64:
		if v != math.Trunc(v) {
			return 0, errors.New("non-integer subject")
		}
		return int64(v), nil
	case int64:
		return v, nil
	case string:
		return strconv.ParseInt(strings.TrimSpace(v), 10, 64)
	default:
		uidStr := fmt.Sprint(raw)
		return strconv.ParseInt(uidStr, 10, 64)
	}
}

func clientIP(r *http.Request) string {
	forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For"))
	if forwarded != "" {
		parts := strings.Split(forwarded, ",")
		if len(parts) > 0 {
			candidate := strings.TrimSpace(parts[0])
			if candidate != "" {
				return candidate
			}
		}
	}

	hostPort := strings.TrimSpace(r.RemoteAddr)
	if hostPort == "" {
		return "unknown"
	}
	if addr, err := netip.ParseAddrPort(hostPort); err == nil {
		return addr.Addr().String()
	}
	if addr, err := netip.ParseAddr(hostPort); err == nil {
		return addr.String()
	}
	return hostPort
}
