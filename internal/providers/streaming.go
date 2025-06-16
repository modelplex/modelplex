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
	BaseURL  string
	Endpoint string
	Payload  interface{}
	Headers  map[string]string
	// UseSSE true for SSE format (OpenAI/Anthropic), false for line-by-line JSON (Ollama)
	UseSSE      bool
	Transformer func(interface{}) interface{} // optional response transformer
}

// makeStreamingRequest is a generic function for making streaming HTTP requests
// It handles both SSE format (OpenAI/Anthropic) and line-by-line JSON (Ollama)
func makeStreamingRequest(ctx context.Context, client *http.Client,
	reqConfig StreamingRequestConfig) (<-chan interface{}, error) {
	jsonData, err := json.Marshal(reqConfig.Payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", reqConfig.BaseURL+reqConfig.Endpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	for key, value := range reqConfig.Headers {
		req.Header.Set(key, value)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	// Ensure response body is always closed
	defer func() {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close() // Explicitly ignore error in defer
		}
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Create channel for streaming chunks
	streamChan := make(chan interface{})

	// Start goroutine to read streaming response
	go func() {
		defer close(streamChan)
		processStreamingResponse(ctx, resp.Body, streamChan, reqConfig)
	}()

	return streamChan, nil
}

// processStreamingResponse handles the streaming response parsing
func processStreamingResponse(ctx context.Context, body io.ReadCloser,
	streamChan chan interface{}, reqConfig StreamingRequestConfig) {
	scanner := bufio.NewScanner(body)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines
		if line == "" {
			continue
		}

		chunk, shouldContinue := parseStreamingLine(line, reqConfig)
		if !shouldContinue {
			continue
		}

		// Send chunk to channel
		select {
		case streamChan <- chunk:
		case <-ctx.Done():
			return
		}
	}
}

// parseStreamingLine parses a single line from the streaming response
func parseStreamingLine(line string, reqConfig StreamingRequestConfig) (interface{}, bool) {
	var chunk interface{}
	var err error

	if reqConfig.UseSSE {
		chunk, err = parseSSELine(line)
		if err != nil {
			if err.Error() == "done" {
				return nil, false // End of stream
			}
			if err.Error() == "skip" {
				return nil, false // Skip this line
			}
			return nil, false // Parse error, skip
		}
	} else {
		// Handle line-by-line JSON format (Ollama)
		err = json.Unmarshal([]byte(line), &chunk)
		if err != nil {
			return nil, false // Skip malformed chunks
		}
	}

	// Apply transformer if provided
	if reqConfig.Transformer != nil {
		chunk = reqConfig.Transformer(chunk)
		if chunk == nil {
			return nil, false // Skip if transformer returns nil
		}
	}

	return chunk, true
}

// parseSSELine parses a Server-Sent Events line
func parseSSELine(line string) (interface{}, error) {
	if !strings.HasPrefix(line, "data: ") {
		return nil, fmt.Errorf("skip") // Skip non-data lines in SSE
	}

	data := strings.TrimPrefix(line, "data: ")

	// Check for end marker
	if data == "[DONE]" {
		return nil, fmt.Errorf("done")
	}

	// Parse JSON chunk
	var chunk interface{}
	err := json.Unmarshal([]byte(data), &chunk)
	return chunk, err
}
