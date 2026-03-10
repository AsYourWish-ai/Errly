package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg := loadConfig()

	storage, err := NewStorage(cfg.DBPath, logger)
	if err != nil {
		logger.Error("init storage", "error", err)
		os.Exit(1)
	}
	defer storage.Close()

	handler := NewHandlerWithOptions(storage, logger, cfg.APIKey, cfg.RatePerMin)

	srv := &http.Server{
		Addr:         cfg.Addr,
		Handler:      handler.Routes(),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		logger.Info("shutting down server")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		srv.Shutdown(ctx)
	}()

	logger.Info("errly server started", "addr", cfg.Addr, "db", cfg.DBPath)
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		logger.Error("server error", "error", err)
		os.Exit(1)
	}
}

type Config struct {
	Addr       string
	DBPath     string
	APIKey     string
	RatePerMin int
}

func loadConfig() Config {
	ratePerMin := 100
	if v := getEnv("ERRLY_RATE_LIMIT", ""); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			ratePerMin = n
		}
	}
	cfg := Config{
		Addr:       getEnv("ERRLY_ADDR", ":5080"),
		DBPath:     getEnv("ERRLY_DB_PATH", "./errly.db"),
		APIKey:     getEnv("ERRLY_API_KEY", ""),
		RatePerMin: ratePerMin,
	}
	if cfg.APIKey == "" {
		fmt.Fprintln(os.Stderr, "[WARN] ERRLY_API_KEY not set — server is unprotected!")
	}
	return cfg
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
