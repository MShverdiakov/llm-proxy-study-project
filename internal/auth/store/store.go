package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type User struct {
	ID           string
	Email        string
	PasswordHash string
	Role         string
	CreatedAt    time.Time
}

type APIKey struct {
	ID        string
	UserID    string
	KeyHash   string
	Name      string
	Revoked   bool
	CreatedAt time.Time
}

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (s *Store) CreateUser(ctx context.Context, email, passwordHash, role string) (*User, error) {
	row := s.pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash, role) VALUES ($1, $2, $3)
		 RETURNING id, email, password_hash, role, created_at`,
		email, passwordHash, role,
	)
	u := &User{}
	err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Role, &u.CreatedAt)
	return u, err
}

func (s *Store) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT id, email, password_hash, role, created_at FROM users WHERE email = $1`, email)
	u := &User{}
	err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Role, &u.CreatedAt)
	return u, err
}

func (s *Store) GetUserByID(ctx context.Context, userID string) (*User, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT id, email, password_hash, role, created_at FROM users WHERE id = $1`, userID)
	u := &User{}
	err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Role, &u.CreatedAt)
	return u, err
}

func (s *Store) CreateAPIKey(ctx context.Context, userID, keyHash, name string) (*APIKey, error) {
	row := s.pool.QueryRow(ctx,
		`INSERT INTO api_keys (user_id, key_hash, name) VALUES ($1, $2, $3)
		 RETURNING id, user_id, key_hash, name, revoked, created_at`,
		userID, keyHash, name,
	)
	k := &APIKey{}
	err := row.Scan(&k.ID, &k.UserID, &k.KeyHash, &k.Name, &k.Revoked, &k.CreatedAt)
	return k, err
}

func (s *Store) GetAPIKeyByHash(ctx context.Context, keyHash string) (*APIKey, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT id, user_id, key_hash, name, revoked, created_at FROM api_keys WHERE key_hash = $1`, keyHash)
	k := &APIKey{}
	err := row.Scan(&k.ID, &k.UserID, &k.KeyHash, &k.Name, &k.Revoked, &k.CreatedAt)
	return k, err
}

func (s *Store) RevokeAPIKey(ctx context.Context, keyID string) error {
	_, err := s.pool.Exec(ctx, `UPDATE api_keys SET revoked = TRUE WHERE id = $1`, keyID)
	return err
}

func (s *Store) ListAPIKeysByUser(ctx context.Context, userID string) ([]*APIKey, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, user_id, key_hash, name, revoked, created_at FROM api_keys WHERE user_id = $1 ORDER BY created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []*APIKey
	for rows.Next() {
		k := &APIKey{}
		if err := rows.Scan(&k.ID, &k.UserID, &k.KeyHash, &k.Name, &k.Revoked, &k.CreatedAt); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}
