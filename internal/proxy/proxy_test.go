package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/modelplex/modelplex/internal/providers"
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

// --- Mock Provider ---
type mockProvider struct {
	modelsToReturn []string
	nameToReturn   string
	// errToReturn    error // ListModels in providers currently logs and returns empty list on error
}

func (mp *mockProvider) Name() string {
	return mp.nameToReturn
}

func (mp *mockProvider) ListModels() []string {
	// if mp.errToReturn != nil {
	// 	// Simulate providers logging their own errors and returning empty
	// 	slog.Error("mockProvider ListModels error", "error", mp.errToReturn, "provider_name", mp.nameToReturn)
	// 	return []string{}
	// }
	return mp.modelsToReturn
}

func (mp *mockProvider) Priority() int                                     { return 0 }
func (mp *mockProvider) ChatCompletion(context.Context, string, []map[string]interface{}) (interface{}, error) { return nil, nil }
func (mp *mockProvider) Completion(context.Context, string, string) (interface{}, error) { return nil, nil }
func (mp *mockProvider) ChatCompletionStream(context.Context, string, []map[string]interface{}) (<-chan interface{}, error) { return nil, nil }
func (mp *mockProvider) CompletionStream(context.Context, string, string) (<-chan interface{}, error) { return nil, nil }

// --- Mock Multiplexer ---
type mockMultiplexer struct {
	providersToReturn []providers.Provider
	// Implement other Multiplexer methods if used by other proxy functions being tested
}

func (mm *mockMultiplexer) GetAllProviders() []providers.Provider {
	return mm.providersToReturn
}

// Dummy implementations for other Multiplexer methods if they were part of an interface used by OpenAIProxy
func (mm *mockMultiplexer) GetProvider(model string) (providers.Provider, error) {
	if len(mm.providersToReturn) > 0 {
		return mm.providersToReturn[0], nil
	}
	return nil, nil
}
func (mm *mockMultiplexer) ListModels() []string { return []string{} } // Not used by HandleModels directly
func (mm *mockMultiplexer) ChatCompletion(ctx context.Context, model string, messages []map[string]interface{}) (interface{}, error) {
	return nil, nil
}
func (mm *mockMultiplexer) Completion(ctx context.Context, model, prompt string) (interface{}, error) {
	return nil, nil
}
func (mm *mockMultiplexer) ChatCompletionStream(ctx context.Context, model string, messages []map[string]interface{}) (<-chan interface{}, error) {
	return nil, nil
}
func (mm *mockMultiplexer) CompletionStream(ctx context.Context, model, prompt string) (<-chan interface{}, error) {
	return nil, nil
}

func TestHandleModels_Success(t *testing.T) {
	provider1 := &mockProvider{
		nameToReturn:   "p1",
		modelsToReturn: []string{"modelA", "modelB"},
	}
	provider2 := &mockProvider{
		nameToReturn:   "p2",
		modelsToReturn: []string{"modelC", "modelA"}, // modelA is duplicate
	}
	provider3 := &mockProvider{ // Provider with no models
		nameToReturn:   "p3",
		modelsToReturn: []string{},
	}

	muxer := &mockMultiplexer{
		providersToReturn: []providers.Provider{provider1, provider2, provider3},
	}

	proxy := New(muxer) // New is defined in proxy.go

	req, err := http.NewRequest("GET", "/v1/models", nil)
	require.NoError(t, err)

	rr := httptest.NewRecorder()

	var logOutput string
	captureSlogOutput(func() {
		proxy.HandleModels(rr, req)
	})

	assert.Equal(t, http.StatusOK, rr.Code)
	// Check if "Provider returned no models" for p3 was logged at Debug level
	// This requires the default slog level to be Debug or lower for the log to be captured.
	// If default is Info, Debug logs from HandleModels won't appear.
	// For this test, we'll assume it might be logged and not fail if it's not present,
	// as it's a Debug log. If it were an Error/Warn log, assertion would be stricter.
	assert.Contains(t, logOutput, "provider_name=p3") // This checks our slog.Debug in HandleModels

	var response ModelsResponse
	err = json.Unmarshal(rr.Body.Bytes(), &response)
	require.NoError(t, err)

	assert.Equal(t, "list", response.Object)
	require.Len(t, response.Data, 3, "Expected 3 unique models")

	// Sort for consistent assertion
	sort.Slice(response.Data, func(i, j int) bool {
		return response.Data[i].ID < response.Data[j].ID
	})

	expectedModels := []ModelInfo{
		{ID: "modelA", Object: "model", Created: defaultModelCreated, OwnedBy: "p1"}, // p1 lists modelA first
		{ID: "modelB", Object: "model", Created: defaultModelCreated, OwnedBy: "p1"},
		{ID: "modelC", Object: "model", Created: defaultModelCreated, OwnedBy: "p2"},
	}

	// Adjust expectation for modelA's ownership based on typical map iteration behavior (last one wins if not careful)
	// However, the code is `if _, exists := allModelsMap[modelID]; !exists`, so first encountered wins.
	// Provider1 (p1) lists modelA first.

	assert.Equal(t, expectedModels[0].ID, response.Data[0].ID)
	assert.Equal(t, expectedModels[0].Object, response.Data[0].Object)
	assert.Equal(t, expectedModels[0].Created, response.Data[0].Created)
	assert.Equal(t, "p1", response.Data[0].OwnedBy) // modelA should be owned by p1

	assert.Equal(t, expectedModels[1].ID, response.Data[1].ID)
	assert.Equal(t, "p1", response.Data[1].OwnedBy) // modelB by p1

	assert.Equal(t, expectedModels[2].ID, response.Data[2].ID)
	assert.Equal(t, "p2", response.Data[2].OwnedBy) // modelC by p2
}

func TestHandleModels_NoProviders(t *testing.T) {
	muxer := &mockMultiplexer{
		providersToReturn: []providers.Provider{}, // No providers
	}
	proxy := New(muxer)

	req, err := http.NewRequest("GET", "/v1/models", nil)
	require.NoError(t, err)
	rr := httptest.NewRecorder()

	proxy.HandleModels(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	var response ModelsResponse
	err = json.Unmarshal(rr.Body.Bytes(), &response)
	require.NoError(t, err)

	assert.Equal(t, "list", response.Object)
	assert.Empty(t, response.Data, "Expected no models when there are no providers")
}

func TestHandleModels_ProviderReturnsEmpty(t *testing.T) {
	provider1 := &mockProvider{
		nameToReturn:   "p1",
		modelsToReturn: []string{"modelX", "modelY"},
	}
	provider2 := &mockProvider{
		nameToReturn:   "p2",
		modelsToReturn: []string{}, // This provider returns no models
	}
	muxer := &mockMultiplexer{
		providersToReturn: []providers.Provider{provider1, provider2},
	}
	proxy := New(muxer)

	req, err := http.NewRequest("GET", "/v1/models", nil)
	require.NoError(t, err)
	rr := httptest.NewRecorder()

	proxy.HandleModels(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	var response ModelsResponse
	err = json.Unmarshal(rr.Body.Bytes(), &response)
	require.NoError(t, err)

	assert.Equal(t, "list", response.Object)
	require.Len(t, response.Data, 2, "Expected 2 models from provider1")

	// Sort for consistent assertion
	sort.Slice(response.Data, func(i, j int) bool {
		return response.Data[i].ID < response.Data[j].ID
	})

	assert.Equal(t, "modelX", response.Data[0].ID)
	assert.Equal(t, "p1", response.Data[0].OwnedBy)
	assert.Equal(t, "modelY", response.Data[1].ID)
	assert.Equal(t, "p1", response.Data[1].OwnedBy)
}

// TestMain for proxy package - ensure it's the only one if multiple _test.go files exist in this package.
// If other files like `proxy_openai_test.go` exist, consolidate TestMain.
// For now, assuming this is the main test file for the proxy package.
var proxyTestSetupOnce sync.Once

func setupProxyTestLogging() {
	proxyTestSetupOnce.Do(func() {
		// Global setup for proxy tests, if any.
	})
}

/*
// Only one TestMain per package. If other _test.go files in 'proxy' package have TestMain, this will conflict.
func TestMain(m *testing.M) {
	setupProxyTestLogging()
	// originalLogger := slog.Default()
	// quietLogger := slog.New(slog.NewTextHandler(io.Discard, nil)) // Discard logs unless captured
	// slog.SetDefault(quietLogger)

	code := m.Run()

	// slog.SetDefault(originalLogger) // Restore
	// os.Exit(code)
}
*/
