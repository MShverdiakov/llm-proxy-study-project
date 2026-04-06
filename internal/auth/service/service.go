package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/mshve/llm-proxy-system/internal/auth/store"
	"github.com/mshve/llm-proxy-system/pkg/client"
	"golang.org/x/crypto/bcrypt"
)

type Store interface {
	CreateUser(ctx context.Context, email, passwordHash, role string) (*store.User, error)
	GetUserByEmail(ctx context.Context, email string) (*store.User, error)
	GetUserByID(ctx context.Context, userID string) (*store.User, error)
	CreateAPIKey(ctx context.Context, userID, keyHash, name string) (*store.APIKey, error)
	GetAPIKeyByHash(ctx context.Context, keyHash string) (*store.APIKey, error)
	RevokeAPIKey(ctx context.Context, keyID string) error
	ListAPIKeysByUser(ctx context.Context, userID string) ([]*store.APIKey, error)
}

type RegisterResult struct {
	User   *store.User
	APIKey string
}

type Service struct {
	store     Store
	jwtSecret string
}

func NewService(store Store, jwtSecret string) *Service {
	return &Service{store: store, jwtSecret: jwtSecret}
}

func (s *Service) Register(ctx context.Context, email, password string) (*RegisterResult, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}
	user, err := s.store.CreateUser(ctx, email, string(hash), "user")
	if err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}
	apiKey, err := s.generateAndStoreAPIKey(ctx, user.ID, "default")
	if err != nil {
		return nil, fmt.Errorf("create api key: %w", err)
	}
	return &RegisterResult{User: user, APIKey: apiKey}, nil
}

func (s *Service) Login(ctx context.Context, email, password string) (string, error) {
	user, err := s.store.GetUserByEmail(ctx, email)
	if err != nil {
		return "", fmt.Errorf("user not found")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return "", fmt.Errorf("invalid credentials")
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": user.ID,
		"role":    user.Role,
		"exp":     time.Now().Add(24 * time.Hour).Unix(),
	})
	return token.SignedString([]byte(s.jwtSecret))
}

func (s *Service) ValidateAPIKey(ctx context.Context, apiKey string) (*client.UserInfo, error) {
	h := sha256.Sum256([]byte(apiKey))
	keyHash := hex.EncodeToString(h[:])
	k, err := s.store.GetAPIKeyByHash(ctx, keyHash)
	if err != nil {
		return nil, fmt.Errorf("invalid api key")
	}
	if k.Revoked {
		return nil, fmt.Errorf("api key revoked")
	}
	user, err := s.store.GetUserByID(ctx, k.UserID)
	if err != nil {
		return nil, fmt.Errorf("user not found")
	}
	return &client.UserInfo{UserID: user.ID, Role: user.Role}, nil
}

func (s *Service) CreateAPIKey(ctx context.Context, userID, name string) (string, error) {
	return s.generateAndStoreAPIKey(ctx, userID, name)
}

func (s *Service) RevokeAPIKey(ctx context.Context, keyID string) error {
	return s.store.RevokeAPIKey(ctx, keyID)
}

func (s *Service) GetMe(ctx context.Context, userID string) (*store.User, error) {
	return s.store.GetUserByID(ctx, userID)
}

func (s *Service) ParseJWT(tokenStr string) (string, string, error) {
	token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return []byte(s.jwtSecret), nil
	})
	if err != nil || !token.Valid {
		return "", "", fmt.Errorf("invalid token")
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return "", "", fmt.Errorf("invalid claims")
	}
	userID, _ := claims["user_id"].(string)
	role, _ := claims["role"].(string)
	return userID, role, nil
}

func (s *Service) generateAndStoreAPIKey(ctx context.Context, userID, name string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	apiKey := hex.EncodeToString(raw)
	h := sha256.Sum256([]byte(apiKey))
	keyHash := hex.EncodeToString(h[:])
	if _, err := s.store.CreateAPIKey(ctx, userID, keyHash, name); err != nil {
		return "", err
	}
	return apiKey, nil
}
