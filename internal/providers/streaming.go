// Package providers implements AI provider abstractions.
// This file contains common streaming functionality shared across all providers.
package providers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// StreamingRequestConfig holds configuration for making streaming requests
type StreamingRequestConfig struct {
	BaseURL     string
	Endpoint    string
	Payload     interface{}
	Headers     map[string]string
	UseSSE      bool // true for SSE format (OpenAI/Anthropic), false for line-by-line JSON (Ollama)
	Transformer func(interface{}) interface{} // optional response transformer
}

// makeStreamingRequest is a generic function for making streaming HTTP requests
// It handles both SSE format (OpenAI/Anthropic) and line-by-line JSON (Ollama)
func makeStreamingRequest(ctx context.Context, client *http.Client, config StreamingRequestConfig) (<-chan interface{}, error) {
	jsonData, err := json.Marshal(config.Payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", config.BaseURL+config.Endpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	for key, value := range config.Headers {
		req.Header.Set(key, value)
	}

	resp, err := client.Do(req)
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

	// Start goroutine to read streaming response
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
			
			var chunk interface{}
			var err error

			if config.UseSSE {
				// Handle SSE format (OpenAI/Anthropic)
				if strings.HasPrefix(line, "data: ") {
					data := strings.TrimPrefix(line, "data: ")
					
					// Check for end marker
					if data == "[DONE]" {
						return
					}
					
					// Parse JSON chunk
					err = json.Unmarshal([]byte(data), &chunk)
				} else {
					continue // Skip non-data lines in SSE
				}
			} else {
				// Handle line-by-line JSON format (Ollama)
				err = json.Unmarshal([]byte(line), &chunk)
			}

			if err != nil {
				continue // Skip malformed chunks
			}
			
			// Apply transformer if provided
			if config.Transformer != nil {
				chunk = config.Transformer(chunk)
				if chunk == nil {
					continue // Skip if transformer returns nil
				}
			}
			
			// Send chunk to channel
			select {
			case streamChan <- chunk:
			case <-ctx.Done():
				return
			}
		}
	}()

	return streamChan, nil
}