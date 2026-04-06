package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/mshve/llm-proxy-system/internal/llmproxy/provider"
	"github.com/mshve/llm-proxy-system/pkg/stats"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Service interface {
	Complete(ctx context.Context, apiKey string, req *provider.CompletionRequest) (*provider.CompletionResponse, error)
	ListModels(ctx context.Context) ([]provider.Model, error)
	ClearCache(ctx context.Context) error
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

	r.Post("/completions", h.completions)
	r.Get("/models", h.models)
	r.Delete("/cache", h.clearCache)

	r.Get("/stats", stats.Handler(h.stats))
	r.Get("/metrics", promhttp.Handler().ServeHTTP)
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	})

	return r
}

func (h *Handler) completions(w http.ResponseWriter, r *http.Request) {
	auth := r.Header.Get("Authorization")
	apiKey := strings.TrimPrefix(auth, "Bearer ")
	if apiKey == "" {
		writeError(w, http.StatusUnauthorized, "missing api key")
		return
	}

	var req struct {
		Model    string              `json:"model"`
		Messages []provider.Message `json:"messages"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}

	provReq := &provider.CompletionRequest{Model: req.Model, Messages: req.Messages}
	resp, err := h.svc.Complete(r.Context(), apiKey, provReq)
	if err != nil {
		if strings.Contains(err.Error(), "auth") {
			writeError(w, http.StatusUnauthorized, err.Error())
			return
		}
		if strings.Contains(err.Error(), "insufficient balance") {
			writeError(w, http.StatusPaymentRequired, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"content": resp.Content,
		"model":   resp.Model,
		"usage": map[string]int{
			"prompt_tokens":     resp.PromptTokens,
			"completion_tokens": resp.CompletionTokens,
			"total_tokens":      resp.TotalTokens,
		},
		"latency_ms": resp.LatencyMs,
	})
}

func (h *Handler) models(w http.ResponseWriter, r *http.Request) {
	models, err := h.svc.ListModels(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, models)
}

func (h *Handler) clearCache(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.ClearCache(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
