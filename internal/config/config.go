package config

import (
	"fmt"
	"os"
	"strings"
)

type Config struct {
	Port               string
	DatabaseURL        string
	JWTSecret          string
	SchemaPath         string
	CORSAllowedOrigins []string
	FrontendBaseURL    string
	AppTimezone        string
	KRAPIN             string
	MLBaseURL          string
	SMTPHost           string
	SMTPPort           string
	SMTPUsername       string
	SMTPPassword       string
	FromEmail          string
	FromName           string
}

func Load() (Config, error) {
	cfg := Config{
		Port:               getEnvOrDefault("PORT", "8080"),
		DatabaseURL:        os.Getenv("DATABASE_URL"),
		JWTSecret:          os.Getenv("JWT_SECRET"),
		SchemaPath:         getEnvOrDefault("DB_SCHEMA_PATH", "db/schema.sql"),
		CORSAllowedOrigins: splitCSVEnv(getEnvOrDefault("CORS_ALLOWED_ORIGINS", "http://localhost:5173,http://127.0.0.1:5173")),
		FrontendBaseURL:    getEnvOrDefault("FRONTEND_BASE_URL", "http://localhost:5173"),
		AppTimezone:        getEnvOrDefault("APP_TIMEZONE", "Africa/Nairobi"),
		KRAPIN:             strings.TrimSpace(os.Getenv("KRA_PIN")),
		MLBaseURL:          strings.TrimSpace(os.Getenv("ML_BASE_URL")),
		SMTPHost:           strings.TrimSpace(os.Getenv("SMTP_HOST")),
		SMTPPort:           getEnvOrDefault("SMTP_PORT", "587"),
		SMTPUsername:       strings.TrimSpace(os.Getenv("SMTP_USERNAME")),
		SMTPPassword:       normalizeSMTPPassword(firstNonEmpty(os.Getenv("SMTP_PASSWORD"), os.Getenv("smtp_password"))),
		FromEmail:          getEnvOrDefault("FROM_EMAIL", "noreply@farmpro.com"),
		FromName:           getEnvOrDefault("FROM_NAME", "FarmPro"),
	}

	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("missing required environment variable: DATABASE_URL")
	}
	if cfg.JWTSecret == "" {
		return Config{}, fmt.Errorf("missing required environment variable: JWT_SECRET")
	}

	return cfg, nil
}

func getEnvOrDefault(key, fallback string) string {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	return v
}

func splitCSVEnv(v string) []string {
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		item := strings.TrimSpace(p)
		if item == "" {
			continue
		}
		out = append(out, item)
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		t := strings.TrimSpace(v)
		if t != "" {
			return t
		}
	}
	return ""
}

func normalizeSMTPPassword(v string) string {
	return strings.ReplaceAll(strings.TrimSpace(v), " ", "")
}
