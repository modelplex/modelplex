package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/modelplex/modelplex/internal/config"
	// No direct dependency on proxy.ModelInfo for Anthropic model structs
)

// captureSlogOutput captures slog output for the duration of the provided function.
// Re-defined here for simplicity; in a real project, this would be a shared test utility.
func captureSlogOutput(fn func()) string {
	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, nil) // Simplified handler
	originalLogger := slog.Default()
	slog.SetDefault(slog.New(handler))
	defer slog.SetDefault(originalLogger)

	fn()
	return buf.String()
}

func TestAnthropicProvider_ListModels_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/v1/models", r.URL.Path) // Anthropic endpoint
		assert.Equal(t, "test-anthropic-api-key", r.Header.Get("x-api-key"))
		assert.Equal(t, "2023-06-01", r.Header.Get("anthropic-version"))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		response := AnthropicModelsListResponse{
			Data: []AnthropicModelInfo{
				{ID: "claude-2", DisplayName: "Claude 2", CreatedAt: "2023-01-01T00:00:00Z", Type: "model"},
				{ID: "claude-instant-1", DisplayName: "Claude Instant 1", CreatedAt: "2023-01-01T00:00:00Z", Type: "model"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		err := json.NewEncoder(w).Encode(response)
		require.NoError(t, err)
	}))
	defer server.Close()

	providerCfg := &config.Provider{
		Name:    "anthropic-test-success",
		Type:    "anthropic",
		BaseURL: server.URL,
		APIKey:  "test-anthropic-api-key",
	}
	provider := NewAnthropicProvider(providerCfg)
	require.NotNil(t, provider)

	var models []string
	logOutput := captureSlogOutput(func() {
		models = provider.ListModels()
	})

	assert.ElementsMatch(t, []string{"claude-2", "claude-instant-1"}, models)
	assert.NotContains(t, strings.ToLower(logOutput), "level=error")
	assert.NotContains(t, strings.ToLower(logOutput), "failed")
}

func TestAnthropicProvider_ListModels_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/v1/models", r.URL.Path)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal server error"))
	}))
	defer server.Close()

	providerCfg := &config.Provider{
		Name:    "anthropic-test-server-error",
		Type:    "anthropic",
		BaseURL: server.URL,
		APIKey:  "test-anthropic-api-key",
	}
	provider := NewAnthropicProvider(providerCfg)
	require.NotNil(t, provider)

	var models []string
	logOutput := captureSlogOutput(func() {
		models = provider.ListModels()
	})

	assert.Empty(t, models)
	assert.Contains(t, logOutput, "Failed to list models from Anthropic")
	assert.Contains(t, logOutput, "provider=anthropic-test-server-error")
	assert.Contains(t, logOutput, "API request failed with status 500")
	assert.Contains(t, logOutput, "internal server error")
}

func TestAnthropicProvider_ListModels_MalformedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/v1/models", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data": [{"id": "claude-2"}, malformed_json`)) // Invalid JSON
	}))
	defer server.Close()

	providerCfg := &config.Provider{
		Name:    "anthropic-test-malformed",
		Type:    "anthropic",
		BaseURL: server.URL,
		APIKey:  "test-anthropic-api-key",
	}
	provider := NewAnthropicProvider(providerCfg)
	require.NotNil(t, provider)

	var models []string
	logOutput := captureSlogOutput(func() {
		models = provider.ListModels()
	})

	assert.Empty(t, models)
	assert.Contains(t, logOutput, "Failed to list models from Anthropic")
	assert.Contains(t, logOutput, "provider=anthropic-test-malformed")
	assert.Contains(t, logOutput, "failed to unmarshal response body")
}

func TestNewAnthropicProvider_APIKeyFromEnv(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "env-anthropic-api-key", r.Header.Get("x-api-key"))
		response := AnthropicModelsListResponse{Data: []AnthropicModelInfo{}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	envVarName := "TEST_ANTHROPIC_API_KEY"
	originalEnvValue, isSet := os.LookupEnv(envVarName)
	err := os.Setenv(envVarName, "env-anthropic-api-key")
	require.NoError(t, err)
	defer func() {
		if isSet {
			_ = os.Setenv(envVarName, originalEnvValue)
		} else {
			_ = os.Unsetenv(envVarName)
		}
	}()

	providerCfg := &config.Provider{
		Name:    "anthropic-env-key-test",
		Type:    "anthropic",
		BaseURL: server.URL,
		APIKey:  "${" + envVarName + "}",
	}
	provider := NewAnthropicProvider(providerCfg)
	require.NotNil(t, provider)
	_ = provider.ListModels() // Trigger request
}

func TestAnthropicProvider_makeGetRequest_ContextCancelled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond) // Make handler slow
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data": []}`))
	}))
	defer server.Close()

	providerCfg := &config.Provider{
		Name:    "anthropic-context-cancel",
		Type:    "anthropic",
		BaseURL: server.URL,
		APIKey:  "test-key",
	}
	p, ok := NewAnthropicProvider(providerCfg).(*AnthropicProvider)
	require.True(t, ok)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := p.makeGetRequest(ctx, "/v1/models")
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "context canceled")
}

func TestAnthropicProvider_ListModels_EmptyResponseData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := AnthropicModelsListResponse{Data: []AnthropicModelInfo{}} // Empty data
		w.Header().Set("Content-Type", "application/json")
		err := json.NewEncoder(w).Encode(response)
		require.NoError(t, err)
	}))
	defer server.Close()

	providerCfg := &config.Provider{Name: "anthropic-empty-data", BaseURL: server.URL, APIKey: "test"}
	provider := NewAnthropicProvider(providerCfg)

	var models []string
	logOutput := captureSlogOutput(func() {
		models = provider.ListModels()
	})

	assert.Empty(t, models)
	assert.NotContains(t, strings.ToLower(logOutput), "level=error")
}

func TestAnthropicProvider_ListModels_NilResponseData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rawResponse := `{"data": null}` // Data is null
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(rawResponse))
		require.NoError(t, err)
	}))
	defer server.Close()

	providerCfg := &config.Provider{Name: "anthropic-nil-data", BaseURL: server.URL, APIKey: "test"}
	provider := NewAnthropicProvider(providerCfg)

	var models []string
	logOutput := captureSlogOutput(func() {
		models = provider.ListModels()
	})

	assert.Empty(t, models)
	assert.NotContains(t, strings.ToLower(logOutput), "level=error")
}

var anthropicTestSetupOnce sync.Once

func setupAnthropicTestLogging() {
	anthropicTestSetupOnce.Do(func() {
		// Global setup for Anthropic tests, if any.
	})
}

func TestMain(m *testing.M) {
	// This TestMain will be shadowed by the one in openai_test.go if they are in the same package.
	// However, if `go test ./...` is run or they are part of the same test binary,
	// only one TestMain (per package) is executed.
	// For provider tests, each `*_test.go` file is in the `providers` package.
	// So, this TestMain will conflict with others.
	// It's better to have one TestMain for the package, e.g., in a `main_test.go` or one of the existing `*_test.go` files.
	// For now, commenting out the os.Exit to avoid premature exit if this TestMain runs.
	// The slog capture is per-test, so global logger state isn't strictly an issue here.
	// setupAnthropicTestLogging()
	// code := m.Run()
	// os.Exit(code)
}
