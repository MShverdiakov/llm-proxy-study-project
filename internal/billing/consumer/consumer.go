package consumer

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

type BillingService interface {
	RecordUsage(ctx context.Context, userID, model string, tokens int64) error
}

type Consumer struct {
	url     string
	svc     BillingService
	logger  *slog.Logger
}

type usageEvent struct {
	UserID           string    `json:"user_id"`
	Model            string    `json:"model"`
	PromptTokens     int64     `json:"prompt_tokens"`
	CompletionTokens int64     `json:"completion_tokens"`
	Timestamp        time.Time `json:"timestamp"`
}

func NewConsumer(url string, svc BillingService, logger *slog.Logger) *Consumer {
	return &Consumer{url: url, svc: svc, logger: logger}
}

func (c *Consumer) Start(ctx context.Context) error {
	for {
		if err := c.run(ctx); err != nil {
			c.logger.Error("rabbitmq consumer error", "err", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
			c.logger.Info("reconnecting to rabbitmq")
		}
	}
}

func (c *Consumer) run(ctx context.Context) error {
	conn, err := amqp.Dial(c.url)
	if err != nil {
		return err
	}
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		return err
	}
	defer ch.Close()

	_, err = ch.QueueDeclare("usage.recorded", true, false, false, false, nil)
	if err != nil {
		return err
	}

	msgs, err := ch.Consume("usage.recorded", "", false, false, false, false, nil)
	if err != nil {
		return err
	}

	connClose := make(chan *amqp.Error, 1)
	conn.NotifyClose(connClose)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-connClose:
			return err
		case msg, ok := <-msgs:
			if !ok {
				return nil
			}
			var event usageEvent
			if err := json.Unmarshal(msg.Body, &event); err != nil {
				c.logger.Error("failed to decode usage event", "err", err)
				msg.Nack(false, false)
				continue
			}
			tokens := event.PromptTokens + event.CompletionTokens
			if err := c.svc.RecordUsage(ctx, event.UserID, event.Model, tokens); err != nil {
				c.logger.Error("failed to record usage", "err", err)
				msg.Nack(false, true)
				continue
			}
			msg.Ack(false)
		}
	}
}
