package main

import (
	"bufio"
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"farmpro/backend/internal/api"
	"farmpro/backend/internal/config"
	"farmpro/backend/internal/database"
)

func main() {
	loadEnvFiles(".env", "backend/.env")

	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := database.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()

	migrateCtx, migrateCancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer migrateCancel()
	if err := database.EnsureSchema(migrateCtx, pool, cfg.SchemaPath); err != nil {
		log.Fatal(err)
	}

	mailer := api.NewSMTPMailer(cfg.SMTPHost, cfg.SMTPPort, cfg.SMTPUsername, cfg.SMTPPassword, cfg.FromName, cfg.FromEmail)
	srv := api.NewServer(pool, cfg.JWTSecret, cfg.CORSAllowedOrigins, mailer, cfg.FrontendBaseURL, cfg.AppTimezone, cfg.KRAPIN)
	httpServer := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      srv.Mux(),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 20 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	stopCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		<-stopCtx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			log.Printf("graceful shutdown failed: %v", err)
		}
	}()

	log.Printf("FarmPro backend running on :%s", cfg.Port)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func loadEnvFiles(paths ...string) {
	for _, p := range paths {
		if p == "" {
			continue
		}
		if err := loadEnvFile(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Printf("warning: failed to load %s: %v", p, err)
		}
	}
}

func loadEnvFile(path string) error {
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}

		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		val = strings.Trim(val, "\"'")
		if key == "" {
			continue
		}

		if _, exists := os.LookupEnv(key); !exists {
			_ = os.Setenv(key, val)
		}
	}

	return scanner.Err()
}
