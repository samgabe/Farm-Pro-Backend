package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"
)

var emailRe = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if !s.registerLimiter.allow("register:" + ip) {
		respondJSON(w, http.StatusTooManyRequests, map[string]string{"error": "too many registration attempts"})
		return
	}

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

	roleID, roleName, err := s.resolveRole(ctx, publicRegistrationRole)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "registration role is not configured"})
		return
	}

	var id int64
	var role string
	verifyToken, verifyTokenHash, err := generateTokenPair(32)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to prepare verification token"})
		return
	}
	verifyExpiry := s.now().Add(24 * time.Hour)

	err = s.db.QueryRow(ctx, `
		INSERT INTO users(name, email, password_hash, role_id, role, phone, status, email_verified, email_verify_token_hash, email_verify_expires_at)
		VALUES ($1, $2, $3, $4, $5, '', 'active', false, $6, $7)
		RETURNING id, role
	`, in.Name, in.Email, string(hash), roleID, roleName, verifyTokenHash, verifyExpiry).Scan(&id, &role)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "duplicate key") {
			respondJSON(w, http.StatusConflict, map[string]string{"error": "email already registered"})
			return
		}
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create user"})
		return
	}

	if s.mailer != nil {
		verifyURL := s.frontendURL("/verify-email?token=" + verifyToken)
		body := fmt.Sprintf("Hi %s,\n\nVerify your FarmPro account by opening this link:\n%s\n\nThis link expires in 24 hours.", in.Name, verifyURL)
		if mailErr := s.mailer.send(in.Email, "Verify your FarmPro account", body); mailErr != nil {
			log.Printf("verify email send failed for %s: %v", in.Email, mailErr)
			respondJSON(w, http.StatusCreated, map[string]any{
				"user":   map[string]any{"id": id, "name": in.Name, "email": in.Email, "role": role},
				"notice": "account created, but verification email could not be sent",
			})
			return
		}
	}

	respondJSON(w, http.StatusCreated, map[string]any{
		"user":                 map[string]any{"id": id, "name": in.Name, "email": in.Email, "role": role},
		"verificationRequired": true,
		"notice":               "account created, check your email to verify your account",
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

	loginEmail := strings.ToLower(strings.TrimSpace(in.Email))
	ip := clientIP(r)
	if !s.loginLimiter.allow("login:" + ip + ":" + loginEmail) {
		respondJSON(w, http.StatusTooManyRequests, map[string]string{"error": "too many login attempts"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	var id int64
	var name, email, role, passwordHash string
	err := s.db.QueryRow(ctx, `
		SELECT u.id, u.name, u.email, r.name, u.password_hash
		FROM users u
		JOIN roles r ON r.id = u.role_id
		WHERE u.email = $1 AND u.status = 'active' AND u.email_verified = true
		`, loginEmail).Scan(&id, &name, &email, &role, &passwordHash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid email or password"})
			return
		}
		respondJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid email or password"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(in.Password)); err != nil {
		respondJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid email or password"})
		return
	}

	token, err := s.signToken(id, email)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to sign token"})
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"token": token,
		"user":  map[string]any{"id": id, "name": name, "email": email, "role": role},
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
	err := s.db.QueryRow(ctx, `
		SELECT u.id, u.name, u.email, r.name, u.phone, u.status
		FROM users u
		JOIN roles r ON r.id = u.role_id
		WHERE u.id = $1
	`, userID).
		Scan(&out.ID, &out.Name, &out.Email, &out.Role, &out.Phone, &out.Status)
	if err != nil {
		respondJSON(w, http.StatusUnauthorized, map[string]string{"error": "user not found"})
		return
	}

	respondJSON(w, http.StatusOK, out)
}

func (s *Server) handleVerifyEmail(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}
	token := strings.TrimSpace(in.Token)
	if token == "" {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "token is required"})
		return
	}

	tokenHash := hashToken(token)
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	var id int64
	var email string
	err := s.db.QueryRow(ctx, `
		UPDATE users
		SET email_verified = true, email_verify_token_hash = NULL, email_verify_expires_at = NULL
		WHERE email_verify_token_hash = $1
			AND email_verify_expires_at IS NOT NULL
			AND email_verify_expires_at > NOW()
		RETURNING id, email
	`, tokenHash).Scan(&id, &email)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid or expired token"})
			return
		}
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to verify email"})
		return
	}

	authCtx, authCancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer authCancel()

	var name, role string
	err = s.db.QueryRow(authCtx, `
		SELECT u.name, r.name
		FROM users u
		JOIN roles r ON r.id = u.role_id
		WHERE u.id = $1
	`, id).Scan(&name, &role)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load user"})
		return
	}

	tokenOut, err := s.signToken(id, email)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to sign token"})
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"token": tokenOut,
		"user":  map[string]any{"id": id, "name": name, "email": email, "role": role},
	})
}

func (s *Server) handleForgotPassword(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}
	email := strings.ToLower(strings.TrimSpace(in.Email))
	if email == "" || !emailRe.MatchString(email) {
		respondJSON(w, http.StatusOK, map[string]string{"message": "if the email exists, reset instructions were sent"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	var id int64
	err := s.db.QueryRow(ctx, `
		SELECT id
		FROM users
		WHERE email = $1 AND status = 'active' AND email_verified = true
	`, email).Scan(&id)
	if err != nil {
		respondJSON(w, http.StatusOK, map[string]string{"message": "if the email exists, reset instructions were sent"})
		return
	}

	if s.mailer == nil {
		respondJSON(w, http.StatusOK, map[string]string{"message": "if the email exists, reset instructions were sent"})
		return
	}

	resetToken, resetTokenHash, err := generateTokenPair(32)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to prepare reset token"})
		return
	}
	expiry := s.now().Add(30 * time.Minute)
	_, err = s.db.Exec(ctx, `
		UPDATE users
		SET reset_token_hash = $1, reset_token_expires_at = $2
		WHERE id = $3
	`, resetTokenHash, expiry, id)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to start reset flow"})
		return
	}

	resetURL := s.frontendURL("/reset-password?token=" + resetToken)
	body := fmt.Sprintf("Use this link to reset your FarmPro password:\n%s\n\nThis link expires in 30 minutes.", resetURL)
	if err := s.mailer.send(email, "FarmPro password reset", body); err != nil {
		log.Printf("password reset email send failed for %s: %v", email, err)
	}

	respondJSON(w, http.StatusOK, map[string]string{"message": "if the email exists, reset instructions were sent"})
}

func (s *Server) handleResetPassword(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token       string `json:"token"`
		NewPassword string `json:"newPassword"`
		Password    string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}

	token := strings.TrimSpace(in.Token)
	password := strings.TrimSpace(in.NewPassword)
	if password == "" {
		password = strings.TrimSpace(in.Password)
	}
	if token == "" || len(password) < 6 {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "token and password(min 6) are required"})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "password processing failed"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	res, err := s.db.Exec(ctx, `
		UPDATE users
		SET password_hash = $1, reset_token_hash = NULL, reset_token_expires_at = NULL
		WHERE reset_token_hash = $2
			AND reset_token_expires_at IS NOT NULL
			AND reset_token_expires_at > NOW()
	`, string(hash), hashToken(token))
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to reset password"})
		return
	}
	if res.RowsAffected() == 0 {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid or expired token"})
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func generateTokenPair(n int) (string, string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", "", err
	}
	token := hex.EncodeToString(buf)
	return token, hashToken(token), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}
