package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"time"
)

type CompletionRequest struct {
	Model    string
	Messages []Message
}

type Message struct {
	Role    string
	Content string
}

type CompletionResponse struct {
	Content          string
	Model            string
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	LatencyMs        int64
}

type Model struct {
	ID       string `json:"id"`
	Provider string `json:"provider"`
}

type LLMProvider interface {
	Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error)
	ListModels(ctx context.Context) ([]Model, error)
}

// MockProvider

type MockProvider struct{}

func (m *MockProvider) Complete(_ context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	time.Sleep(time.Duration(50+rand.Intn(200)) * time.Millisecond)
	prompt := rand.Intn(150) + 50
	completion := rand.Intn(100) + 30
	return &CompletionResponse{
		Content:          "Mock response for: " + req.Messages[len(req.Messages)-1].Content,
		Model:            req.Model,
		PromptTokens:     prompt,
		CompletionTokens: completion,
		TotalTokens:      prompt + completion,
		LatencyMs:        int64(50 + rand.Intn(200)),
	}, nil
}

func (m *MockProvider) ListModels(_ context.Context) ([]Model, error) {
	return []Model{
		{ID: "mock-gpt-4", Provider: "mock"},
		{ID: "mock-claude-3", Provider: "mock"},
	}, nil
}

// OpenAIProvider

type OpenAIProvider struct {
	apiKey string
	client *http.Client
}

func NewOpenAIProvider(apiKey string) *OpenAIProvider {
	return &OpenAIProvider{apiKey: apiKey, client: &http.Client{Timeout: 60 * time.Second}}
}

type openAIRequest struct {
	Model    string          `json:"model"`
	Messages []openAIMessage `json:"messages"`
}

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
	Model string `json:"model"`
}

func (p *OpenAIProvider) Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	msgs := make([]openAIMessage, len(req.Messages))
	for i, m := range req.Messages {
		msgs[i] = openAIMessage{Role: m.Role, Content: m.Content}
	}
	body, _ := json.Marshal(openAIRequest{Model: req.Model, Messages: msgs})

	start := time.Now()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.openai.com/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	latency := time.Since(start).Milliseconds()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openai: status %d", resp.StatusCode)
	}

	var oaiResp openAIResponse
	if err := json.NewDecoder(resp.Body).Decode(&oaiResp); err != nil {
		return nil, err
	}
	content := ""
	if len(oaiResp.Choices) > 0 {
		content = oaiResp.Choices[0].Message.Content
	}
	return &CompletionResponse{
		Content:          content,
		Model:            oaiResp.Model,
		PromptTokens:     oaiResp.Usage.PromptTokens,
		CompletionTokens: oaiResp.Usage.CompletionTokens,
		TotalTokens:      oaiResp.Usage.TotalTokens,
		LatencyMs:        latency,
	}, nil
}

func (p *OpenAIProvider) ListModels(_ context.Context) ([]Model, error) {
	return []Model{
		{ID: "gpt-4", Provider: "openai"},
		{ID: "gpt-4-turbo", Provider: "openai"},
		{ID: "gpt-3.5-turbo", Provider: "openai"},
	}, nil
}

func NewProvider(providerType string) (LLMProvider, error) {
	switch providerType {
	case "openai":
		key := os.Getenv("OPENAI_API_KEY")
		if key == "" {
			return nil, fmt.Errorf("OPENAI_API_KEY not set")
		}
		return NewOpenAIProvider(key), nil
	case "mock", "":
		return &MockProvider{}, nil
	default:
		return nil, fmt.Errorf("unknown provider: %s", providerType)
	}
}
