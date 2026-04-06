package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mshve/llm-proxy-system/internal/auth/handler"
	authservice "github.com/mshve/llm-proxy-system/internal/auth/service"
	authstore "github.com/mshve/llm-proxy-system/internal/auth/store"
	"github.com/mshve/llm-proxy-system/pkg/stats"
	"github.com/mshve/llm-proxy-system/pkg/telemetry"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	port := getenv("PORT", "8001")
	dbURL := getenv("DB_URL", "postgres://llm:llm@localhost:5432/llm")
	jwtSecret := getenv("JWT_SECRET", "secret")
	otlpEndpoint := getenv("OTEL_EXPORTER_OTLP_ENDPOINT", "localhost:4317")
	version := getenv("SERVICE_VERSION", "1.0.0")

	ctx := context.Background()

	shutdown, err := telemetry.Init(ctx, "auth-service", version, otlpEndpoint)
	if err != nil {
		logger.Warn("telemetry init failed", "err", err)
	} else {
		defer func() { _ = shutdown(ctx) }()
	}

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		logger.Error("db connect failed", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		logger.Error("db ping failed", "err", err)
		os.Exit(1)
	}

	st := authstore.NewStore(pool)
	svc := authservice.NewService(st, jwtSecret)
	s := stats.NewStats("auth-service", version)
	h := handler.New(svc, s)

	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      h.Router(),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
	}

	logger.Info("auth-service starting", "port", port)
	if err := srv.ListenAndServe(); err != nil {
		logger.Error("server error", "err", err)
	}
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
