package service

import (
	"context"
	"fmt"
	"math"

	"github.com/mshve/llm-proxy-system/internal/billing/store"
)

type Store interface {
	GetBalance(ctx context.Context, userID string) (int64, error)
	UpdateBalance(ctx context.Context, userID string, delta int64) error
	CreateTransaction(ctx context.Context, userID, txType string, amount int64, model string, tokens int64) error
	GetTransactions(ctx context.Context, userID string) ([]*store.Transaction, error)
	GetUsage(ctx context.Context, userID, period string) (*store.UsageSummary, error)
}

type Service struct {
	store  Store
	prices map[string]float64
}

func NewService(s Store, prices map[string]float64) *Service {
	return &Service{store: s, prices: prices}
}

func (s *Service) GetBalance(ctx context.Context, userID string) (int64, error) {
	return s.store.GetBalance(ctx, userID)
}

func (s *Service) Deposit(ctx context.Context, userID string, amount int64) error {
	if err := s.store.UpdateBalance(ctx, userID, amount); err != nil {
		return err
	}
	return s.store.CreateTransaction(ctx, userID, "deposit", amount, "", 0)
}

func (s *Service) RecordUsage(ctx context.Context, userID, model string, tokens int64) error {
	price, ok := s.prices[model]
	if !ok {
		price = s.prices["default"]
		if price == 0 {
			price = 0.03
		}
	}
	cost := int64(math.Ceil(float64(tokens) / 1000.0 * price * 100)) // store as microcredits
	balance, err := s.store.GetBalance(ctx, userID)
	if err != nil {
		return err
	}
	if balance < cost {
		return fmt.Errorf("insufficient balance")
	}
	if err := s.store.UpdateBalance(ctx, userID, -cost); err != nil {
		return err
	}
	return s.store.CreateTransaction(ctx, userID, "usage", cost, model, tokens)
}

func (s *Service) GetTransactions(ctx context.Context, userID string) ([]*store.Transaction, error) {
	return s.store.GetTransactions(ctx, userID)
}

func (s *Service) GetUsage(ctx context.Context, userID, period string) (*store.UsageSummary, error) {
	return s.store.GetUsage(ctx, userID, period)
}
