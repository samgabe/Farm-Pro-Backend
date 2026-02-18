package api

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

var emailRe = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name     string `json:"name"`
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}

	in.Name = strings.TrimSpace(in.Name)
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	if in.Name == "" || in.Email == "" || len(in.Password) < 6 {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "name, email and password(min 6) are required"})
		return
	}
	if !emailRe.MatchString(in.Email) {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid email format"})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "password processing failed"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	var id int64
	err = s.db.QueryRow(ctx, `
		INSERT INTO users(name, email, password_hash, role, phone, status)
		VALUES ($1, $2, $3, 'owner', '', 'active')
		RETURNING id
	`, in.Name, in.Email, string(hash)).Scan(&id)
	if err != nil {
		if strings.Contains(err.Error(), "duplicate key") {
			respondJSON(w, http.StatusConflict, map[string]string{"error": "email already registered"})
			return
		}
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create user"})
		return
	}

	token, err := s.signToken(id, in.Email, "owner")
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to sign token"})
		return
	}

	respondJSON(w, http.StatusCreated, map[string]any{
		"token": token,
		"user": map[string]any{"id": id, "name": in.Name, "email": in.Email, "role": "owner"},
	})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	var id int64
	var name, email, role, passwordHash string
	err := s.db.QueryRow(ctx, `
		SELECT id, name, email, role, password_hash
		FROM users
		WHERE email = $1 AND status = 'active'
	`, strings.ToLower(strings.TrimSpace(in.Email))).Scan(&id, &name, &email, &role, &passwordHash)
	if err != nil {
		respondJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid email or password"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(in.Password)); err != nil {
		respondJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid email or password"})
		return
	}

	token, err := s.signToken(id, email, role)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to sign token"})
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"token": token,
		"user": map[string]any{"id": id, "name": name, "email": email, "role": role},
	})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	userID, ok := r.Context().Value(userIDContextKey).(int64)
	if !ok {
		respondJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid auth context"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	var out struct {
		ID     int64  `json:"id"`
		Name   string `json:"name"`
		Email  string `json:"email"`
		Role   string `json:"role"`
		Phone  string `json:"phone"`
		Status string `json:"status"`
	}
	err := s.db.QueryRow(ctx, `SELECT id, name, email, role, phone, status FROM users WHERE id = $1`, userID).
		Scan(&out.ID, &out.Name, &out.Email, &out.Role, &out.Phone, &out.Status)
	if err != nil {
		respondJSON(w, http.StatusUnauthorized, map[string]string{"error": "user not found"})
		return
	}

	respondJSON(w, http.StatusOK, out)
}
