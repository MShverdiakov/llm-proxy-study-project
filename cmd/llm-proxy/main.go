package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/redis/go-redis/v9"
	"github.com/mshve/llm-proxy-system/internal/llmproxy/handler"
	llmservice "github.com/mshve/llm-proxy-system/internal/llmproxy/service"
	"github.com/mshve/llm-proxy-system/internal/llmproxy/provider"
	"github.com/mshve/llm-proxy-system/pkg/client"
	"github.com/mshve/llm-proxy-system/pkg/stats"
	"github.com/mshve/llm-proxy-system/pkg/telemetry"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	port := getenv("PORT", "8000")
	redisURL := getenv("REDIS_URL", "redis://localhost:6379")
	rabbitURL := getenv("RABBITMQ_URL", "amqp://llm:llm@localhost:5672/")
	authServiceURL := getenv("AUTH_SERVICE_URL", "http://localhost:8001")
	billingServiceURL := getenv("BILLING_SERVICE_URL", "http://localhost:8002")
	llmProviderType := getenv("LLM_PROVIDER", "mock")
	otlpEndpoint := getenv("OTEL_EXPORTER_OTLP_ENDPOINT", "localhost:4317")
	version := getenv("SERVICE_VERSION", "1.0.0")

	cacheTTLSec, _ := strconv.Atoi(getenv("CACHE_TTL", "300"))
	cacheTTL := time.Duration(cacheTTLSec) * time.Second

	ctx := context.Background()

	shutdown, err := telemetry.Init(ctx, "llm-proxy", version, otlpEndpoint)
	if err != nil {
		logger.Warn("telemetry init failed", "err", err)
	} else {
		defer func() { _ = shutdown(ctx) }()
	}

	// Redis
	rOpts, err := redis.ParseURL(redisURL)
	if err != nil {
		logger.Error("redis url parse failed", "err", err)
		os.Exit(1)
	}
	redisClient := redis.NewClient(rOpts)
	defer redisClient.Close()

	if err := redisClient.Ping(ctx).Err(); err != nil {
		logger.Error("redis ping failed", "err", err)
		os.Exit(1)
	}

	// RabbitMQ
	rabbitConn, err := amqp.Dial(rabbitURL)
	if err != nil {
		logger.Error("rabbitmq connect failed", "err", err)
		os.Exit(1)
	}
	defer rabbitConn.Close()

	rabbitCh, err := rabbitConn.Channel()
	if err != nil {
		logger.Error("rabbitmq channel failed", "err", err)
		os.Exit(1)
	}
	defer rabbitCh.Close()

	if _, err := rabbitCh.QueueDeclare("usage.recorded", true, false, false, false, nil); err != nil {
		logger.Error("queue declare failed", "err", err)
		os.Exit(1)
	}

	// LLM Provider
	prov, err := provider.NewProvider(llmProviderType)
	if err != nil {
		logger.Error("provider init failed", "err", err)
		os.Exit(1)
	}

	// HTTP clients
	authClient := client.NewHTTPAuthClient(authServiceURL)
	billingClient := client.NewHTTPBillingClient(billingServiceURL)

	svc := llmservice.NewService(prov, authClient, billingClient, redisClient, rabbitCh, cacheTTL)
	s := stats.NewStats("llm-proxy", version)
	h := handler.New(svc, s)

	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      h.Router(),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
	}

	logger.Info("llm-proxy starting", "port", port, "provider", llmProviderType)
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
