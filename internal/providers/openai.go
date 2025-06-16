// Package providers implements AI provider abstractions.
// OpenAIProvider implements the standard OpenAI API format.
// This serves as the reference implementation that other providers adapt to.
// Uses standard OpenAI endpoints, headers, and request/response formats.
package providers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/modelplex/modelplex/internal/config"
)

// OpenAIProvider implements the Provider interface for OpenAI API.
type OpenAIProvider struct {
	name     string
	baseURL  string
	apiKey   string
	models   []string
	priority int
	client   *http.Client
}

// NewOpenAIProvider creates a new OpenAI provider instance.
func NewOpenAIProvider(cfg *config.Provider) *OpenAIProvider {
	apiKey := cfg.APIKey
	if strings.HasPrefix(apiKey, "${") && strings.HasSuffix(apiKey, "}") {
		envVar := strings.TrimSuffix(strings.TrimPrefix(apiKey, "${"), "}")
		apiKey = os.Getenv(envVar)
	}

	return &OpenAIProvider{
		name:     cfg.Name,
		baseURL:  cfg.BaseURL,
		apiKey:   apiKey,
		models:   cfg.Models,
		priority: cfg.Priority,
		client:   &http.Client{},
	}
}

// Name returns the provider name.
func (p *OpenAIProvider) Name() string {
	return p.name
}

// Priority returns the provider priority for model routing.
func (p *OpenAIProvider) Priority() int {
	return p.priority
}

// ListModels returns the list of available models for this provider.
func (p *OpenAIProvider) ListModels() []string {
	return p.models
}

// ChatCompletion performs a chat completion request.
func (p *OpenAIProvider) ChatCompletion(
	ctx context.Context, model string, messages []map[string]interface{},
) (interface{}, error) {
	payload := map[string]interface{}{
		"model":    model,
		"messages": messages,
	}

	return p.makeRequest(ctx, "/chat/completions", payload)
}

// Completion performs a completion request.
func (p *OpenAIProvider) Completion(ctx context.Context, model, prompt string) (interface{}, error) {
	payload := map[string]interface{}{
		"model":  model,
		"prompt": prompt,
	}

	return p.makeRequest(ctx, "/completions", payload)
}

func (p *OpenAIProvider) makeRequest(ctx context.Context, endpoint string, payload interface{}) (interface{}, error) {
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+endpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

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
func (p *OpenAIProvider) ChatCompletionStream(
	ctx context.Context, model string, messages []map[string]interface{},
) (<-chan interface{}, error) {
	payload := map[string]interface{}{
		"model":    model,
		"messages": messages,
		"stream":   true,
	}

	return p.makeStreamingRequest(ctx, "/chat/completions", payload)
}

// CompletionStream performs a streaming completion request.
func (p *OpenAIProvider) CompletionStream(ctx context.Context, model, prompt string) (<-chan interface{}, error) {
	payload := map[string]interface{}{
		"model":  model,
		"prompt": prompt,
		"stream": true,
	}

	return p.makeStreamingRequest(ctx, "/completions", payload)
}

func (p *OpenAIProvider) makeStreamingRequest(ctx context.Context, endpoint string, payload interface{}) (<-chan interface{}, error) {
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+endpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Create channel for streaming chunks
	streamChan := make(chan interface{})

	// Start goroutine to read SSE stream
	go func() {
		defer close(streamChan)
		defer resp.Body.Close()

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			
			// Skip empty lines
			if line == "" {
				continue
			}
			
			// Handle SSE data lines
			if strings.HasPrefix(line, "data: ") {
				data := strings.TrimPrefix(line, "data: ")
				
				// Check for end marker
				if data == "[DONE]" {
					return
				}
				
				// Parse JSON chunk
				var chunk interface{}
				if err := json.Unmarshal([]byte(data), &chunk); err != nil {
					continue // Skip malformed chunks
				}
				
				// Send chunk to channel
				select {
				case streamChan <- chunk:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return streamChan, nil
}
