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

func TestOllamaProvider_ListModels_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/tags", r.URL.Path) // Ollama endpoint for listing models
		// Ollama doesn't use auth headers, so no need to check for them.
		// Content-Type for GET is not standard but makeGetRequest might set it.
		// assert.Equal(t, "application/json", r.Header.Get("Content-Type"))


		response := OllamaModelsListResponse{
			Models: []OllamaModelInfo{
				{Name: "llama2:latest", Model: "llama2:latest", ModifiedAt: "2023-01-01T00:00:00Z", Size: 12345},
				{Name: "mistral:7b", Model: "mistral:7b", ModifiedAt: "2023-01-01T00:00:00Z", Size: 67890},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		err := json.NewEncoder(w).Encode(response)
		require.NoError(t, err)
	}))
	defer server.Close()

	providerCfg := &config.Provider{
		Name:    "ollama-test-success",
		Type:    "ollama",
		BaseURL: server.URL,
		// No APIKey needed for Ollama
	}
	provider := NewOllamaProvider(providerCfg)
	require.NotNil(t, provider)

	var models []string
	logOutput := captureSlogOutput(func() {
		models = provider.ListModels()
	})

	assert.ElementsMatch(t, []string{"llama2:latest", "mistral:7b"}, models)
	assert.NotContains(t, strings.ToLower(logOutput), "level=error")
	assert.NotContains(t, strings.ToLower(logOutput), "failed")
}

func TestOllamaProvider_ListModels_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/tags", r.URL.Path)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("ollama server error"))
	}))
	defer server.Close()

	providerCfg := &config.Provider{
		Name:    "ollama-test-server-error",
		Type:    "ollama",
		BaseURL: server.URL,
	}
	provider := NewOllamaProvider(providerCfg)
	require.NotNil(t, provider)

	var models []string
	logOutput := captureSlogOutput(func() {
		models = provider.ListModels()
	})

	assert.Empty(t, models)
	assert.Contains(t, logOutput, "Failed to list models from Ollama")
	assert.Contains(t, logOutput, "provider=ollama-test-server-error")
	assert.Contains(t, logOutput, "API request failed with status 500")
	assert.Contains(t, logOutput, "ollama server error")
}

func TestOllamaProvider_ListModels_MalformedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/tags", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"models": [{"name": "llama2"}, malformed_json`)) // Invalid JSON
	}))
	defer server.Close()

	providerCfg := &config.Provider{
		Name:    "ollama-test-malformed",
		Type:    "ollama",
		BaseURL: server.URL,
	}
	provider := NewOllamaProvider(providerCfg)
	require.NotNil(t, provider)

	var models []string
	logOutput := captureSlogOutput(func() {
		models = provider.ListModels()
	})

	assert.Empty(t, models)
	assert.Contains(t, logOutput, "Failed to list models from Ollama")
	assert.Contains(t, logOutput, "provider=ollama-test-malformed")
	assert.Contains(t, logOutput, "failed to unmarshal response body")
}

func TestOllamaProvider_makeGetRequest_ContextCancelled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond) // Make handler slow
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"models": []}`))
	}))
	defer server.Close()

	providerCfg := &config.Provider{
		Name:    "ollama-context-cancel",
		Type:    "ollama",
		BaseURL: server.URL,
	}
	// Cast to access unexported makeGetRequest for this white-box test
	p, ok := NewOllamaProvider(providerCfg).(*OllamaProvider)
	require.True(t, ok)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := p.makeGetRequest(ctx, "/api/tags")
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "context canceled")
}

func TestOllamaProvider_ListModels_EmptyResponseData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := OllamaModelsListResponse{Models: []OllamaModelInfo{}} // Empty models array
		w.Header().Set("Content-Type", "application/json")
		err := json.NewEncoder(w).Encode(response)
		require.NoError(t, err)
	}))
	defer server.Close()

	providerCfg := &config.Provider{Name: "ollama-empty-data", BaseURL: server.URL}
	provider := NewOllamaProvider(providerCfg)

	var models []string
	logOutput := captureSlogOutput(func() {
		models = provider.ListModels()
	})

	assert.Empty(t, models)
	assert.NotContains(t, strings.ToLower(logOutput), "level=error")
}

func TestOllamaProvider_ListModels_NilResponseData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rawResponse := `{"models": null}` // Models field is null
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(rawResponse))
		require.NoError(t, err)
	}))
	defer server.Close()

	providerCfg := &config.Provider{Name: "ollama-nil-data", BaseURL: server.URL}
	provider := NewOllamaProvider(providerCfg)

	var models []string
	logOutput := captureSlogOutput(func() {
		models = provider.ListModels()
	})

	assert.Empty(t, models)
	assert.NotContains(t, strings.ToLower(logOutput), "level=error")
}

var ollamaTestSetupOnce sync.Once

func setupOllamaTestLogging() {
	ollamaTestSetupOnce.Do(func() {
		// Global setup for Ollama tests, if any.
	})
}

// TestMain needs to be defined only once per package.
// If openai_test.go or anthropic_test.go already defines it, this one will be ignored or cause a conflict.
// It's best to have a single main_test.go or ensure only one _test.go file defines TestMain.
// For now, commenting out os.Exit to avoid issues if run in conjunction with other tests in the same package.
/*
func TestMain(m *testing.M) {
	setupOllamaTestLogging()
	// To prevent verbose output from tests unless explicitly captured and asserted:
	// originalLogger := slog.Default()
	// quietLogger := slog.New(slog.NewTextHandler(io.Discard, nil))
	// slog.SetDefault(quietLogger)

	code := m.Run()

	// slog.SetDefault(originalLogger) // Restore if changed globally
	os.Exit(code)
}
*/
