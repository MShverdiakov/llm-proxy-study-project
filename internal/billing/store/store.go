package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Transaction struct {
	ID        string
	UserID    string
	Type      string
	Amount    int64
	Model     string
	Tokens    int64
	CreatedAt time.Time
}

type UsageSummary struct {
	UserID      string
	Period      string
	TotalTokens int64
	TotalAmount int64
	Requests    int64
}

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (s *Store) GetBalance(ctx context.Context, userID string) (int64, error) {
	row := s.pool.QueryRow(ctx, `SELECT amount FROM balances WHERE user_id = $1`, userID)
	var amount int64
	err := row.Scan(&amount)
	if err != nil {
		// No row = zero balance
		return 0, nil
	}
	return amount, nil
}

func (s *Store) UpdateBalance(ctx context.Context, userID string, delta int64) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO balances (user_id, amount) VALUES ($1, $2)
		 ON CONFLICT (user_id) DO UPDATE SET amount = balances.amount + $2, updated_at = NOW()`,
		userID, delta,
	)
	return err
}

func (s *Store) CreateTransaction(ctx context.Context, userID, txType string, amount int64, model string, tokens int64) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO transactions (user_id, type, amount, model, tokens) VALUES ($1, $2, $3, $4, $5)`,
		userID, txType, amount, model, tokens,
	)
	return err
}

func (s *Store) GetTransactions(ctx context.Context, userID string) ([]*Transaction, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, user_id, type, amount, model, tokens, created_at FROM transactions WHERE user_id = $1 ORDER BY created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var txs []*Transaction
	for rows.Next() {
		t := &Transaction{}
		if err := rows.Scan(&t.ID, &t.UserID, &t.Type, &t.Amount, &t.Model, &t.Tokens, &t.CreatedAt); err != nil {
			return nil, err
		}
		txs = append(txs, t)
	}
	return txs, rows.Err()
}

func (s *Store) GetUsage(ctx context.Context, userID, period string) (*UsageSummary, error) {
	var interval string
	switch period {
	case "week":
		interval = "7 days"
	case "month":
		interval = "30 days"
	default:
		interval = "1 day"
	}
	row := s.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(tokens),0), COALESCE(SUM(amount),0), COUNT(*) FROM transactions
		 WHERE user_id = $1 AND type = 'usage' AND created_at >= NOW() - INTERVAL '`+interval+`'`,
		userID,
	)
	summary := &UsageSummary{UserID: userID, Period: period}
	err := row.Scan(&summary.TotalTokens, &summary.TotalAmount, &summary.Requests)
	return summary, err
}
