package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

type UserInfo struct {
	UserID string `json:"user_id"`
	Role   string `json:"role"`
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type CompletionRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type CompletionResponse struct {
	Content   string `json:"content"`
	Model     string `json:"model"`
	Usage     Usage  `json:"usage"`
	LatencyMs int64  `json:"latency_ms"`
}

type Model struct {
	ID       string `json:"id"`
	Provider string `json:"provider"`
}

type AuthClient interface {
	Validate(ctx context.Context, apiKey string) (*UserInfo, error)
}

type BillingClient interface {
	Balance(ctx context.Context, userID string) (int64, error)
	Deposit(ctx context.Context, userID string, amount int64) error
}

type LLMProxyClient interface {
	Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error)
	Models(ctx context.Context) ([]Model, error)
}

func newHTTPClient() *http.Client {
	return &http.Client{Transport: otelhttp.NewTransport(http.DefaultTransport)}
}

// HTTPAuthClient

type HTTPAuthClient struct {
	baseURL string
	http    *http.Client
}

func NewHTTPAuthClient(baseURL string) *HTTPAuthClient {
	return &HTTPAuthClient{baseURL: baseURL, http: newHTTPClient()}
}

func (c *HTTPAuthClient) Validate(ctx context.Context, apiKey string) (*UserInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/auth/validate?api_key="+apiKey, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("auth validate: status %d", resp.StatusCode)
	}
	var info UserInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, err
	}
	return &info, nil
}

// HTTPBillingClient

type HTTPBillingClient struct {
	baseURL string
	http    *http.Client
}

func NewHTTPBillingClient(baseURL string) *HTTPBillingClient {
	return &HTTPBillingClient{baseURL: baseURL, http: newHTTPClient()}
}

func (c *HTTPBillingClient) Balance(ctx context.Context, userID string) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/billing/balance/"+userID, nil)
	if err != nil {
		return 0, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("billing balance: status %d", resp.StatusCode)
	}
	var result struct {
		Balance int64 `json:"balance"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, err
	}
	return result.Balance, nil
}

func (c *HTTPBillingClient) Deposit(ctx context.Context, userID string, amount int64) error {
	body, _ := json.Marshal(map[string]any{"user_id": userID, "amount": amount})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/billing/deposit", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("billing deposit: status %d", resp.StatusCode)
	}
	return nil
}

// HTTPLLMProxyClient

type HTTPLLMProxyClient struct {
	baseURL string
	http    *http.Client
}

func NewHTTPLLMProxyClient(baseURL string) *HTTPLLMProxyClient {
	return &HTTPLLMProxyClient{baseURL: baseURL, http: newHTTPClient()}
}

func (c *HTTPLLMProxyClient) Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	body, _ := json.Marshal(req)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("llm complete: status %d", resp.StatusCode)
	}
	var result CompletionResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *HTTPLLMProxyClient) Models(ctx context.Context) ([]Model, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/models", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("llm models: status %d", resp.StatusCode)
	}
	var models []Model
	if err := json.NewDecoder(resp.Body).Decode(&models); err != nil {
		return nil, err
	}
	return models, nil
}
