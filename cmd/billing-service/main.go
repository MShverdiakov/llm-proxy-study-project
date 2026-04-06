package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mshve/llm-proxy-system/internal/billing/consumer"
	"github.com/mshve/llm-proxy-system/internal/billing/handler"
	billingservice "github.com/mshve/llm-proxy-system/internal/billing/service"
	billingstore "github.com/mshve/llm-proxy-system/internal/billing/store"
	"github.com/mshve/llm-proxy-system/pkg/stats"
	"github.com/mshve/llm-proxy-system/pkg/telemetry"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	port := getenv("PORT", "8002")
	dbURL := getenv("DB_URL", "postgres://llm:llm@localhost:5432/llm")
	rabbitURL := getenv("RABBITMQ_URL", "amqp://llm:llm@localhost:5672/")
	otlpEndpoint := getenv("OTEL_EXPORTER_OTLP_ENDPOINT", "localhost:4317")
	version := getenv("SERVICE_VERSION", "1.0.0")

	priceGPT4, _ := strconv.ParseFloat(getenv("PRICE_GPT4", "0.03"), 64)

	ctx := context.Background()

	shutdown, err := telemetry.Init(ctx, "billing-service", version, otlpEndpoint)
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

	prices := map[string]float64{
		"gpt-4":       priceGPT4,
		"gpt-4-turbo": priceGPT4,
		"default":     priceGPT4,
	}

	st := billingstore.NewStore(pool)
	svc := billingservice.NewService(st, prices)
	s := stats.NewStats("billing-service", version)
	h := handler.New(svc, s)

	// Start RabbitMQ consumer
	c := consumer.NewConsumer(rabbitURL, svc, logger)
	go func() {
		if err := c.Start(ctx); err != nil {
			logger.Error("consumer stopped", "err", err)
		}
	}()

	// Verify RabbitMQ connection at startup (optional, consumer handles reconnects)
	conn, err := amqp.Dial(rabbitURL)
	if err != nil {
		logger.Warn("rabbitmq initial connect failed, consumer will retry", "err", err)
	} else {
		conn.Close()
	}

	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      h.Router(),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
	}

	logger.Info("billing-service starting", "port", port)
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
