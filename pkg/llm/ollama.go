package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

// OllamaClient calls a local Ollama instance via its HTTP API.
// Configure with OLLAMA_BASE_URL (default http://localhost:11434) and
// OLLAMA_MODEL (default llama3).
type OllamaClient struct {
	baseURL string
	model   string
	http    *http.Client
}

// NewOllama creates an OllamaClient with explicit base URL and model.
func NewOllama(baseURL, model string) *OllamaClient {
	return &OllamaClient{baseURL: baseURL, model: model, http: &http.Client{}}
}

// NewOllamaFromEnv creates an OllamaClient from environment variables.
func NewOllamaFromEnv() *OllamaClient {
	baseURL := os.Getenv("OLLAMA_BASE_URL")
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	model := os.Getenv("OLLAMA_MODEL")
	if model == "" {
		model = "llama3"
	}
	return NewOllama(baseURL, model)
}

// generateRequest is the body sent to POST /api/generate.
type generateRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream"`
}

// generateResponse is the relevant subset of Ollama's response.
type generateResponse struct {
	Response string `json:"response"`
}

// chatMessage is a single turn for /api/chat.
type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// chatRequest is the body sent to POST /api/chat.
type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
}

// chatResponse is the relevant subset of Ollama's chat response.
type chatResponse struct {
	Message chatMessage `json:"message"`
}

func (c *OllamaClient) Summarize(ctx context.Context, prompt string) (string, error) {
	body, _ := json.Marshal(generateRequest{Model: c.model, Prompt: prompt, Stream: false})
	resp, err := c.post(ctx, "/api/generate", body)
	if err != nil {
		return "", err
	}
	var gr generateResponse
	if err := json.Unmarshal(resp, &gr); err != nil {
		return "", fmt.Errorf("ollama generate decode: %w", err)
	}
	return gr.Response, nil
}

func (c *OllamaClient) Chat(ctx context.Context, system, user string) (string, error) {
	msgs := []chatMessage{
		{Role: "system", Content: system},
		{Role: "user", Content: user},
	}
	body, _ := json.Marshal(chatRequest{Model: c.model, Messages: msgs, Stream: false})
	resp, err := c.post(ctx, "/api/chat", body)
	if err != nil {
		return "", err
	}
	var cr chatResponse
	if err := json.Unmarshal(resp, &cr); err != nil {
		return "", fmt.Errorf("ollama chat decode: %w", err)
	}
	return cr.Message.Content, nil
}

func (c *OllamaClient) post(ctx context.Context, path string, body []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama %s: %w", path, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("ollama %s read: %w", path, err)
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("ollama %s status %d: %s", path, resp.StatusCode, data)
	}
	return data, nil
}
