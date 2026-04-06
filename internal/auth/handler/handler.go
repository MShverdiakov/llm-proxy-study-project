package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	authservice "github.com/mshve/llm-proxy-system/internal/auth/service"
	"github.com/mshve/llm-proxy-system/internal/auth/store"
	"github.com/mshve/llm-proxy-system/pkg/client"
	"github.com/mshve/llm-proxy-system/pkg/stats"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Service interface {
	Register(ctx context.Context, email, password string) (*authservice.RegisterResult, error)
	Login(ctx context.Context, email, password string) (string, error)
	ValidateAPIKey(ctx context.Context, apiKey string) (*client.UserInfo, error)
	CreateAPIKey(ctx context.Context, userID, name string) (string, error)
	RevokeAPIKey(ctx context.Context, keyID string) error
	GetMe(ctx context.Context, userID string) (*store.User, error)
	ParseJWT(tokenStr string) (string, string, error)
}

type Handler struct {
	svc   Service
	stats *stats.Stats
}

func New(svc Service, s *stats.Stats) *Handler {
	return &Handler{svc: svc, stats: s}
}

type contextKey string

const (
	ctxUserID contextKey = "user_id"
	ctxRole   contextKey = "role"
)

func (h *Handler) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return stats.Middleware(h.stats, next)
	})

	r.Post("/auth/register", h.register)
	r.Post("/auth/login", h.login)
	r.Get("/auth/validate", h.validateAPIKey)

	r.Group(func(r chi.Router) {
		r.Use(h.jwtMiddleware)
		r.Post("/auth/keys", h.createAPIKey)
		r.Delete("/auth/keys/{key_id}", h.revokeAPIKey)
		r.Get("/auth/users/me", h.getMe)
	})

	r.Get("/stats", stats.Handler(h.stats))
	r.Get("/metrics", promhttp.Handler().ServeHTTP)
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	})

	return r
}

func (h *Handler) jwtMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			writeError(w, http.StatusUnauthorized, "missing token")
			return
		}
		token := strings.TrimPrefix(auth, "Bearer ")
		userID, role, err := h.svc.ParseJWT(token)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid token")
			return
		}
		ctx := context.WithValue(r.Context(), ctxUserID, userID)
		ctx = context.WithValue(ctx, ctxRole, role)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (h *Handler) register(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	res, err := h.svc.Register(r.Context(), req.Email, req.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"user":    res.User,
		"api_key": res.APIKey,
	})
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	token, err := h.svc.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"jwt_token": token})
}

func (h *Handler) validateAPIKey(w http.ResponseWriter, r *http.Request) {
	apiKey := r.URL.Query().Get("api_key")
	if apiKey == "" {
		auth := r.Header.Get("Authorization")
		apiKey = strings.TrimPrefix(auth, "Bearer ")
	}
	info, err := h.svc.ValidateAPIKey(r.Context(), apiKey)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (h *Handler) createAPIKey(w http.ResponseWriter, r *http.Request) {
	userID, _ := r.Context().Value(ctxUserID).(string)
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	key, err := h.svc.CreateAPIKey(r.Context(), userID, req.Name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"api_key": key})
}

func (h *Handler) revokeAPIKey(w http.ResponseWriter, r *http.Request) {
	keyID := chi.URLParam(r, "key_id")
	if err := h.svc.RevokeAPIKey(r.Context(), keyID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) getMe(w http.ResponseWriter, r *http.Request) {
	userID, _ := r.Context().Value(ctxUserID).(string)
	user, err := h.svc.GetMe(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, user)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
