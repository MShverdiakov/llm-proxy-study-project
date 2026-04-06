package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/redis/go-redis/v9"
	"github.com/mshve/llm-proxy-system/internal/llmproxy/provider"
	"github.com/mshve/llm-proxy-system/pkg/client"
)

type Service struct {
	p             provider.LLMProvider
	authClient    client.AuthClient
	billingClient client.BillingClient
	redis         *redis.Client
	rabbitCh      *amqp.Channel
	cacheTTL      time.Duration
}

func NewService(
	p provider.LLMProvider,
	auth client.AuthClient,
	billing client.BillingClient,
	redisClient *redis.Client,
	rabbitCh *amqp.Channel,
	cacheTTL time.Duration,
) *Service {
	return &Service{
		p:             p,
		authClient:    auth,
		billingClient: billing,
		redis:         redisClient,
		rabbitCh:      rabbitCh,
		cacheTTL:      cacheTTL,
	}
}

const minBalance int64 = 10

func (s *Service) Complete(ctx context.Context, apiKey string, req *provider.CompletionRequest) (*provider.CompletionResponse, error) {
	userInfo, err := s.authClient.Validate(ctx, apiKey)
	if err != nil {
		return nil, fmt.Errorf("auth: %w", err)
	}

	balance, err := s.billingClient.Balance(ctx, userInfo.UserID)
	if err != nil {
		return nil, fmt.Errorf("billing: %w", err)
	}
	if balance < minBalance {
		return nil, fmt.Errorf("insufficient balance: %w", errInsufficientBalance)
	}

	cacheKey := cacheKey(req)
	if cached, err := s.redis.Get(ctx, cacheKey).Bytes(); err == nil {
		var resp provider.CompletionResponse
		if err := json.Unmarshal(cached, &resp); err == nil {
			return &resp, nil
		}
	}

	resp, err := s.p.Complete(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("provider: %w", err)
	}

	if data, err := json.Marshal(resp); err == nil {
		s.redis.Set(ctx, cacheKey, data, s.cacheTTL)
	}

	if err := s.publishUsage(ctx, userInfo.UserID, req.Model, resp.PromptTokens, resp.CompletionTokens); err != nil {
		// Non-fatal: log but don't fail the request
		_ = err
	}

	return resp, nil
}

var errInsufficientBalance = fmt.Errorf("payment required")

func (s *Service) ListModels(ctx context.Context) ([]provider.Model, error) {
	return s.p.ListModels(ctx)
}

func (s *Service) ClearCache(ctx context.Context) error {
	return s.redis.FlushDB(ctx).Err()
}

func (s *Service) publishUsage(ctx context.Context, userID, model string, promptTokens, completionTokens int) error {
	event := map[string]any{
		"user_id":           userID,
		"model":             model,
		"prompt_tokens":     promptTokens,
		"completion_tokens": completionTokens,
		"timestamp":         time.Now().UTC().Format(time.RFC3339),
	}
	body, err := json.Marshal(event)
	if err != nil {
		return err
	}
	return s.rabbitCh.PublishWithContext(ctx, "", "usage.recorded", false, false, amqp.Publishing{
		ContentType: "application/json",
		Body:        body,
	})
}

func cacheKey(req *provider.CompletionRequest) string {
	data, _ := json.Marshal(struct {
		Model    string
		Messages []provider.Message
	}{req.Model, req.Messages})
	h := sha256.Sum256(data)
	return "llm:" + hex.EncodeToString(h[:])
}
