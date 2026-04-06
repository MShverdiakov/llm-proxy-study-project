package handler

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/mshve/llm-proxy-system/internal/billing/store"
	"github.com/mshve/llm-proxy-system/pkg/stats"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Service interface {
	GetBalance(ctx context.Context, userID string) (int64, error)
	Deposit(ctx context.Context, userID string, amount int64) error
	RecordUsage(ctx context.Context, userID, model string, tokens int64) error
	GetTransactions(ctx context.Context, userID string) ([]*store.Transaction, error)
	GetUsage(ctx context.Context, userID, period string) (*store.UsageSummary, error)
}

type Handler struct {
	svc   Service
	stats *stats.Stats
}

func New(svc Service, s *stats.Stats) *Handler {
	return &Handler{svc: svc, stats: s}
}

func (h *Handler) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return stats.Middleware(h.stats, next)
	})

	r.Get("/billing/balance/{user_id}", h.getBalance)
	r.Post("/billing/deposit", h.deposit)
	r.Get("/billing/usage", h.getUsage)
	r.Get("/billing/transactions/{user_id}", h.getTransactions)

	r.Get("/stats", stats.Handler(h.stats))
	r.Get("/metrics", promhttp.Handler().ServeHTTP)
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	})

	return r
}

func (h *Handler) getBalance(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "user_id")
	balance, err := h.svc.GetBalance(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user_id": userID, "balance": balance})
}

func (h *Handler) deposit(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID string `json:"user_id"`
		Amount int64  `json:"amount"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := h.svc.Deposit(r.Context(), req.UserID, req.Amount); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	balance, _ := h.svc.GetBalance(r.Context(), req.UserID)
	writeJSON(w, http.StatusOK, map[string]any{"user_id": req.UserID, "balance": balance})
}

func (h *Handler) getUsage(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user_id")
	period := r.URL.Query().Get("period")
	if period == "" {
		period = "day"
	}
	summary, err := h.svc.GetUsage(r.Context(), userID, period)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

func (h *Handler) getTransactions(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "user_id")
	txs, err := h.svc.GetTransactions(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, txs)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
