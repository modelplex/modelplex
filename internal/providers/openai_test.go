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
	"github.com/modelplex/modelplex/internal/proxy"
)

// captureSlogOutput captures slog output for the duration of the provided function.
func captureSlogOutput(fn func()) string {
	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, nil) // Simplified handler without time for easier assertion
	originalLogger := slog.Default()
	slog.SetDefault(slog.New(handler))
	defer slog.SetDefault(originalLogger)

	fn()
	return buf.String()
}

func TestOpenAIProvider_ListModels_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/models", r.URL.Path)
		authHeader := r.Header.Get("Authorization")
		assert.Equal(t, "Bearer test-api-key", authHeader)
		// Content-Type for GET is not standard but was in previous makeGetRequest, so testing its presence.
		// assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		response := OpenAIModelsListResponse{
			Object: "list",
			Data: []proxy.ModelInfo{
				{ID: "gpt-4", Object: "model", Created: 123, OwnedBy: "openai"},
				{ID: "gpt-3.5-turbo", Object: "model", Created: 123, OwnedBy: "openai"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		err := json.NewEncoder(w).Encode(response)
		require.NoError(t, err)
	}))
	defer server.Close()

	providerCfg := &config.Provider{
		Name:    "openai-test-success",
		Type:    "openai",
		BaseURL: server.URL,
		APIKey:  "test-api-key",
	}
	provider := NewOpenAIProvider(providerCfg)
	require.NotNil(t, provider)

	var models []string
	logOutput := captureSlogOutput(func() {
		models = provider.ListModels()
	})

	assert.ElementsMatch(t, []string{"gpt-4", "gpt-3.5-turbo"}, models)
	assert.NotContains(t, strings.ToLower(logOutput), "level=error") // Check for "level=error" which slog text handler produces
	assert.NotContains(t, strings.ToLower(logOutput), "failed")
}

func TestOpenAIProvider_ListModels_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/models", r.URL.Path)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal server error"))
	}))
	defer server.Close()

	providerCfg := &config.Provider{
		Name:    "openai-test-server-error",
		Type:    "openai",
		BaseURL: server.URL,
		APIKey:  "test-api-key",
	}
	provider := NewOpenAIProvider(providerCfg)
	require.NotNil(t, provider)

	var models []string
	logOutput := captureSlogOutput(func() {
		models = provider.ListModels()
	})

	assert.Empty(t, models)
	assert.Contains(t, logOutput, "Failed to list models from OpenAI")
	assert.Contains(t, logOutput, "provider=openai-test-server-error")
	assert.Contains(t, logOutput, "API request failed with status 500")
	assert.Contains(t, logOutput, "internal server error")
}

func TestOpenAIProvider_ListModels_MalformedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/models", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"object": "list", "data": [{"id": "gpt-4"}, malformed_json`)) // Invalid JSON
	}))
	defer server.Close()

	providerCfg := &config.Provider{
		Name:    "openai-test-malformed",
		Type:    "openai",
		BaseURL: server.URL,
		APIKey:  "test-api-key",
	}
	provider := NewOpenAIProvider(providerCfg)
	require.NotNil(t, provider)

	var models []string
	logOutput := captureSlogOutput(func() {
		models = provider.ListModels()
	})

	assert.Empty(t, models)
	assert.Contains(t, logOutput, "Failed to list models from OpenAI")
	assert.Contains(t, logOutput, "provider=openai-test-malformed")
	assert.Contains(t, logOutput, "failed to unmarshal response body")
}

func TestNewOpenAIProvider_APIKeyFromEnv(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		assert.Equal(t, "Bearer env-api-key-value", authHeader)
		response := OpenAIModelsListResponse{Object: "list", Data: []proxy.ModelInfo{}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	envVarName := "TEST_OPENAI_API_KEY_FOR_PROVIDER"
	originalEnvValue, isSet := os.LookupEnv(envVarName)
	err := os.Setenv(envVarName, "env-api-key-value")
	require.NoError(t, err)
	defer func() {
		if isSet {
			_ = os.Setenv(envVarName, originalEnvValue)
		} else {
			_ = os.Unsetenv(envVarName)
		}
	}()

	providerCfg := &config.Provider{
		Name:    "openai-env-key-test",
		Type:    "openai",
		BaseURL: server.URL,
		APIKey:  "${" + envVarName + "}",
	}
	provider := NewOpenAIProvider(providerCfg)
	require.NotNil(t, provider)
	_ = provider.ListModels() // Trigger request
}

func TestOpenAIProvider_makeGetRequest_ContextCancelled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond) // Make handler slow
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"object": "list", "data": []}`))
	}))
	defer server.Close()

	providerCfg := &config.Provider{
		Name:    "openai-context-cancel",
		Type:    "openai",
		BaseURL: server.URL,
		APIKey:  "test-key",
	}
	// Need to cast to access the unexported makeGetRequest method.
	// This specific test is white-box testing the request cancellation.
	p, ok := NewOpenAIProvider(providerCfg).(*OpenAIProvider)
	require.True(t, ok)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := p.makeGetRequest(ctx, "/models") // Pass the cancelled context
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "context canceled")
}

func TestOpenAIProvider_ListModels_EmptyResponseData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := OpenAIModelsListResponse{
			Object: "list",
			Data:   []proxy.ModelInfo{}, // Empty data
		}
		w.Header().Set("Content-Type", "application/json")
		err := json.NewEncoder(w).Encode(response)
		require.NoError(t, err)
	}))
	defer server.Close()

	providerCfg := &config.Provider{Name: "openai-empty-data", BaseURL: server.URL, APIKey: "test"}
	provider := NewOpenAIProvider(providerCfg)

	var models []string
	logOutput := captureSlogOutput(func() {
		models = provider.ListModels()
	})

	assert.Empty(t, models)
	assert.NotContains(t, strings.ToLower(logOutput), "level=error")
}

func TestOpenAIProvider_ListModels_NilResponseData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rawResponse := `{"object": "list", "data": null}` // Data is null
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(rawResponse))
		require.NoError(t, err)
	}))
	defer server.Close()

	providerCfg := &config.Provider{Name: "openai-nil-data", BaseURL: server.URL, APIKey: "test"}
	provider := NewOpenAIProvider(providerCfg)

	var models []string
	logOutput := captureSlogOutput(func() {
		models = provider.ListModels()
	})

	assert.Empty(t, models)
	assert.NotContains(t, strings.ToLower(logOutput), "level=error")
}

var testSetupOnce sync.Once

func setupTestLogging() {
	testSetupOnce.Do(func() {
		// No global logging setup needed as captureSlogOutput handles it per test.
	})
}

func TestMain(m *testing.M) {
	setupTestLogging()
	// To prevent verbose output from tests unless explicitly captured and asserted:
	// originalLogger := slog.Default()
	// quietLogger := slog.New(slog.NewTextHandler(io.Discard, nil))
	// slog.SetDefault(quietLogger)

	code := m.Run()

	// slog.SetDefault(originalLogger) // Restore if changed globally
	os.Exit(code)
}
