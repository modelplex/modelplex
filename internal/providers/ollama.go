// Package providers implements AI provider abstractions.
// OllamaProvider provides local Ollama API integration with key differences from OpenAI:
// - No authentication required (local server)
// - Uses "/api/chat" and "/api/generate" endpoints instead of "/chat/completions" and "/completions"
// - Requires explicit "stream": false parameter to disable streaming
// - Typically runs on localhost:11434 by default
// - Supports local LLM models without external API dependencies
package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"log/slog"

	"github.com/modelplex/modelplex/internal/config"
)

// OllamaModelDetails provides nested information about an Ollama model.
type OllamaModelDetails struct {
	ParentModel       string   `json:"parent_model"`
	Format            string   `json:"format"`
	Family            string   `json:"family"`
	Families          []string `json:"families"`
	ParameterSize     string   `json:"parameter_size"`
	QuantizationLevel string   `json:"quantization_level"`
}

// OllamaModelInfo defines the structure for a single model in Ollama's API response.
type OllamaModelInfo struct {
	Name       string             `json:"name"`
	Model      string             `json:"model"`
	ModifiedAt string             `json:"modified_at"`
	Size       int64              `json:"size"`
	Digest     string             `json:"digest"`
	Details    OllamaModelDetails `json:"details"`
}

// OllamaModelsListResponse defines the structure for the Ollama API's model list response.
type OllamaModelsListResponse struct {
	Models []OllamaModelInfo `json:"models"`
}

// OllamaProvider implements the Provider interface for Ollama local API.
type OllamaProvider struct {
	name     string
	baseURL  string
	models   []string
	priority int
	client   *http.Client
}

// NewOllamaProvider creates a new Ollama provider instance.
func NewOllamaProvider(cfg *config.Provider) *OllamaProvider {
	return &OllamaProvider{
		name:     cfg.Name,
		baseURL:  cfg.BaseURL,
		models:   cfg.Models,
		priority: cfg.Priority,
		client:   &http.Client{},
	}
}

// Name returns the provider name.
func (p *OllamaProvider) Name() string {
	return p.name
}

// Priority returns the provider priority for model routing.
func (p *OllamaProvider) Priority() int {
	return p.priority
}

// ListModels returns the list of available models for this provider.
func (p *OllamaProvider) ListModels() []string {
	response, err := p.makeGetRequest(context.Background(), "/api/tags")
	if err != nil {
		slog.Error("Failed to list models from Ollama", "error", err, "provider", p.name)
		return []string{} // Return empty list on error
	}

	var models []string
	for _, modelInfo := range response.Models {
		models = append(models, modelInfo.Name) // 'Name' field contains the model ID like "llama2:latest"
	}
	return models
}

func (p *OllamaProvider) makeGetRequest(ctx context.Context, endpoint string) (*OllamaModelsListResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Ollama typically does not require auth headers.
	// req.Header.Set("Content-Type", "application/json") // Not strictly needed for GET with no body but good practice

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var ollamaModelsListResponse OllamaModelsListResponse
	if err := json.Unmarshal(body, &ollamaModelsListResponse); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response body: %w", err)
	}

	return &ollamaModelsListResponse, nil
}

// ChatCompletion performs a chat completion request with Ollama-specific parameters.
func (p *OllamaProvider) ChatCompletion(
	ctx context.Context, model string, messages []map[string]interface{},
) (interface{}, error) {
	payload := map[string]interface{}{
		"model":    model,
		"messages": messages,
		"stream":   false,
	}

	return p.makeRequest(ctx, "/api/chat", payload)
}

// Completion performs a completion request using Ollama's generate endpoint.
func (p *OllamaProvider) Completion(ctx context.Context, model, prompt string) (interface{}, error) {
	payload := map[string]interface{}{
		"model":  model,
		"prompt": prompt,
		"stream": false,
	}

	return p.makeRequest(ctx, "/api/generate", payload)
}

func (p *OllamaProvider) makeRequest(ctx context.Context, endpoint string, payload interface{}) (interface{}, error) {
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+endpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var result interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	return result, nil
}

// ChatCompletionStream performs a streaming chat completion request.
func (p *OllamaProvider) ChatCompletionStream(
	ctx context.Context, model string, messages []map[string]interface{},
) (<-chan interface{}, error) {
	payload := map[string]interface{}{
		"model":    model,
		"messages": messages,
		"stream":   true, // Enable streaming for Ollama
	}

	return p.makeStreamingRequest(ctx, "/api/chat", payload)
}

// CompletionStream performs a streaming completion request.
func (p *OllamaProvider) CompletionStream(ctx context.Context, model, prompt string) (<-chan interface{}, error) {
	payload := map[string]interface{}{
		"model":  model,
		"prompt": prompt,
		"stream": true, // Enable streaming for Ollama
	}

	return p.makeStreamingRequest(ctx, "/api/generate", payload)
}

func (p *OllamaProvider) makeStreamingRequest(ctx context.Context, endpoint string,
	payload interface{}) (<-chan interface{}, error) {
	reqConfig := StreamingRequestConfig{
		BaseURL:     p.baseURL,
		Endpoint:    endpoint,
		Payload:     payload,
		Headers:     map[string]string{}, // Ollama doesn't require authentication
		UseSSE:      false,               // Ollama uses line-by-line JSON, not SSE
		Transformer: p.transformStreamingResponse,
	}

	return makeStreamingRequest(ctx, p.client, reqConfig)
}

// transformStreamingResponse transforms Ollama streaming response to OpenAI format
func (p *OllamaProvider) transformStreamingResponse(chunk interface{}) interface{} {
	// For now, pass through as-is. In a full implementation, we would
	// transform Ollama's streaming format to match OpenAI's format
	// This would involve converting Ollama's response format to OpenAI's delta format
	return chunk
}
