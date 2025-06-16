package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/modelplex/modelplex/internal/config"
)

func TestNewOpenAIProvider(t *testing.T) {
	tests := []struct {
		name     string
		config   config.Provider
		envVars  map[string]string
		expected *OpenAIProvider
	}{
		{
			name: "direct api key",
			config: config.Provider{
				Name:     "openai",
				BaseURL:  "https://api.openai.com/v1",
				APIKey:   "sk-test123",
				Models:   []string{"gpt-4"},
				Priority: 1,
			},
			expected: &OpenAIProvider{
				name:     "openai",
				baseURL:  "https://api.openai.com/v1",
				apiKey:   "sk-test123",
				models:   []string{"gpt-4"},
				priority: 1,
			},
		},
		{
			name: "env var api key",
			config: config.Provider{
				Name:     "openai",
				BaseURL:  "https://api.openai.com/v1",
				APIKey:   "${OPENAI_API_KEY}",
				Models:   []string{"gpt-4", "gpt-3.5-turbo"},
				Priority: 2,
			},
			envVars: map[string]string{
				"OPENAI_API_KEY": "sk-env-test456",
			},
			expected: &OpenAIProvider{
				name:     "openai",
				baseURL:  "https://api.openai.com/v1",
				apiKey:   "sk-env-test456",
				models:   []string{"gpt-4", "gpt-3.5-turbo"},
				priority: 2,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variables
			for key, value := range tt.envVars {
				if err := os.Setenv(key, value); err != nil {
					t.Errorf("Failed to set env var: %v", err)
				}
			}
			defer func() {
				for key := range tt.envVars {
					_ = os.Unsetenv(key)
				}
			}()

			cfg := tt.config // Create copy to avoid memory aliasing
			provider := NewOpenAIProvider(&cfg)

			assert.Equal(t, tt.expected.name, provider.Name())
			assert.Equal(t, tt.expected.baseURL, provider.baseURL)
			assert.Equal(t, tt.expected.apiKey, provider.apiKey)
			assert.Equal(t, tt.expected.models, provider.ListModels())
			assert.Equal(t, tt.expected.priority, provider.Priority())
		})
	}
}

func TestOpenAIProvider_ChatCompletion(t *testing.T) {
	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/chat/completions", r.URL.Path)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))

		// Verify request body
		var req map[string]interface{}
		err := json.NewDecoder(r.Body).Decode(&req)
		require.NoError(t, err)
		assert.Equal(t, "gpt-4", req["model"])
		assert.NotEmpty(t, req["messages"])

		// Send response
		response := map[string]interface{}{
			"id":      "chatcmpl-123",
			"object":  "chat.completion",
			"created": 1677652288,
			"model":   "gpt-4",
			"choices": []map[string]interface{}{
				{
					"index": 0,
					"message": map[string]interface{}{
						"role":    "assistant",
						"content": "Hello! How can I help you today?",
					},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]interface{}{
				"prompt_tokens":     9,
				"completion_tokens": 12,
				"total_tokens":      21,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Errorf("Failed to encode response: %v", err)
		}
	}))
	defer server.Close()

	provider := NewOpenAIProvider(&config.Provider{
		Name:    "test",
		BaseURL: server.URL,
		APIKey:  "test-key",
		Models:  []string{"gpt-4"},
	})

	messages := []map[string]interface{}{
		{"role": "user", "content": "Hello"},
	}

	result, err := provider.ChatCompletion(context.Background(), "gpt-4", messages)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify response structure
	response, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "chatcmpl-123", response["id"])
	assert.Equal(t, "chat.completion", response["object"])
}

func TestOpenAIProvider_ChatCompletion_Error(t *testing.T) {
	// Create test server that returns error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		if _, err := w.Write([]byte(`{"error": {"message": "Invalid API key", "type": "invalid_request_error"}}`)); err != nil {
			t.Errorf("Failed to write error response: %v", err)
		}
	}))
	defer server.Close()

	provider := NewOpenAIProvider(&config.Provider{
		Name:    "test",
		BaseURL: server.URL,
		APIKey:  "invalid-key",
		Models:  []string{"gpt-4"},
	})

	messages := []map[string]interface{}{
		{"role": "user", "content": "Hello"},
	}

	result, err := provider.ChatCompletion(context.Background(), "gpt-4", messages)
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "401")
}

func TestOpenAIProvider_Completion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/completions", r.URL.Path)

		var req map[string]interface{}
		err := json.NewDecoder(r.Body).Decode(&req)
		require.NoError(t, err)
		assert.Equal(t, "gpt-3.5-turbo-instruct", req["model"])
		assert.Equal(t, "Complete this: Hello", req["prompt"])

		response := map[string]interface{}{
			"id":      "cmpl-123",
			"object":  "text_completion",
			"created": 1677652288,
			"model":   "gpt-3.5-turbo-instruct",
			"choices": []map[string]interface{}{
				{
					"text":          " world!",
					"index":         0,
					"logprobs":      nil,
					"finish_reason": "stop",
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Errorf("Failed to encode response: %v", err)
		}
	}))
	defer server.Close()

	provider := NewOpenAIProvider(&config.Provider{
		Name:    "test",
		BaseURL: server.URL,
		APIKey:  "test-key",
		Models:  []string{"gpt-3.5-turbo-instruct"},
	})

	result, err := provider.Completion(context.Background(), "gpt-3.5-turbo-instruct", "Complete this: Hello")
	require.NoError(t, err)
	require.NotNil(t, result)

	response, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "cmpl-123", response["id"])
	assert.Equal(t, "text_completion", response["object"])
}
