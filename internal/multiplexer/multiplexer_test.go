package multiplexer

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/modelplex/modelplex/internal/config"
	"github.com/modelplex/modelplex/internal/providers"
)

// MockProvider implements the Provider interface for testing
type MockProvider struct {
	mock.Mock
}

func (m *MockProvider) Name() string {
	args := m.Called()
	return args.String(0)
}

func (m *MockProvider) Priority() int {
	args := m.Called()
	return args.Int(0)
}

func (m *MockProvider) ChatCompletion(ctx context.Context, model string, messages []map[string]interface{}) (interface{}, error) {
	args := m.Called(ctx, model, messages)
	return args.Get(0), args.Error(1)
}

func (m *MockProvider) Completion(ctx context.Context, model, prompt string) (interface{}, error) {
	args := m.Called(ctx, model, prompt)
	return args.Get(0), args.Error(1)
}

func (m *MockProvider) ListModels() []string {
	args := m.Called()
	return args.Get(0).([]string)
}

func (m *MockProvider) ChatCompletionStream(ctx context.Context, model string, messages []map[string]interface{}) (<-chan interface{}, error) {
	args := m.Called(ctx, model, messages)
	return args.Get(0).(<-chan interface{}), args.Error(1)
}

func (m *MockProvider) CompletionStream(ctx context.Context, model, prompt string) (<-chan interface{}, error) {
	args := m.Called(ctx, model, prompt)
	return args.Get(0).(<-chan interface{}), args.Error(1)
}

func TestNew(t *testing.T) {
	configs := []config.Provider{
		{
			Name:     "openai",
			Type:     "openai",
			Models:   []string{"gpt-4", "gpt-3.5-turbo"},
			Priority: 1,
		},
		{
			Name:     "anthropic",
			Type:     "anthropic",
			Models:   []string{"claude-3-sonnet"},
			Priority: 2,
		},
	}

	mux := New(configs)
	require.NotNil(t, mux)

	// Note: This test is limited since we can't easily mock the provider creation
	// In a real scenario, we'd inject a provider factory
	models := mux.ListModels()
	assert.NotEmpty(t, models)
}

func TestModelMultiplexer_GetProvider(t *testing.T) {
	// Create mock providers
	provider1 := &MockProvider{}
	provider1.On("Name").Return("provider1")
	provider1.On("Priority").Return(1)
	provider1.On("ListModels").Return([]string{"model1", "model2"})

	provider2 := &MockProvider{}
	provider2.On("Name").Return("provider2")
	provider2.On("Priority").Return(2)
	provider2.On("ListModels").Return([]string{"model3"})

	// Create multiplexer with manual setup (since we can't easily mock provider creation)
	mux := &ModelMultiplexer{
		providers: []providers.Provider{provider1, provider2},
		modelMap: map[string]providers.Provider{
			"model1": provider1,
			"model2": provider1,
			"model3": provider2,
		},
	}

	tests := []struct {
		name          string
		model         string
		expectedName  string
		expectedError bool
	}{
		{
			name:         "existing model1",
			model:        "model1",
			expectedName: "provider1",
		},
		{
			name:         "existing model3",
			model:        "model3",
			expectedName: "provider2",
		},
		{
			name:         "non-existing model falls back to first provider",
			model:        "unknown-model",
			expectedName: "provider1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider, err := mux.GetProvider(tt.model)

			if tt.expectedError {
				assert.Error(t, err)
				assert.Nil(t, provider)
			} else {
				require.NoError(t, err)
				require.NotNil(t, provider)
				assert.Equal(t, tt.expectedName, provider.Name())
			}
		})
	}
}

func TestModelMultiplexer_GetProvider_NoProviders(t *testing.T) {
	mux := &ModelMultiplexer{
		providers: []providers.Provider{},
		modelMap:  map[string]providers.Provider{},
	}

	provider, err := mux.GetProvider("any-model")
	assert.Error(t, err)
	assert.Nil(t, provider)
	assert.Contains(t, err.Error(), "no provider available")
}

func TestModelMultiplexer_ListModels(t *testing.T) {
	mux := &ModelMultiplexer{
		modelMap: map[string]providers.Provider{
			"gpt-4":           nil,
			"gpt-3.5-turbo":   nil,
			"claude-3-sonnet": nil,
		},
	}

	models := mux.ListModels()
	assert.Len(t, models, 3)
	assert.Contains(t, models, "gpt-4")
	assert.Contains(t, models, "gpt-3.5-turbo")
	assert.Contains(t, models, "claude-3-sonnet")
}

func TestModelMultiplexer_ChatCompletion(t *testing.T) {
	provider := &MockProvider{}

	messages := []map[string]interface{}{
		{"role": "user", "content": "Hello"},
	}

	expectedResponse := map[string]interface{}{
		"id": "test-response",
		"choices": []map[string]interface{}{
			{"message": map[string]interface{}{"content": "Hello back!"}},
		},
	}

	provider.On("ChatCompletion", mock.Anything, "gpt-4", messages).Return(expectedResponse, nil)

	mux := &ModelMultiplexer{
		providers: []providers.Provider{provider},
		modelMap: map[string]providers.Provider{
			"gpt-4": provider,
		},
	}

	result, err := mux.ChatCompletion(context.Background(), "gpt-4", messages)
	require.NoError(t, err)
	assert.Equal(t, expectedResponse, result)

	provider.AssertExpectations(t)
}

func TestModelMultiplexer_ChatCompletion_Error(t *testing.T) {
	provider := &MockProvider{}

	messages := []map[string]interface{}{
		{"role": "user", "content": "Hello"},
	}

	expectedError := errors.New("provider error")
	provider.On("ChatCompletion", mock.Anything, "gpt-4", messages).Return(nil, expectedError)

	mux := &ModelMultiplexer{
		providers: []providers.Provider{provider},
		modelMap: map[string]providers.Provider{
			"gpt-4": provider,
		},
	}

	result, err := mux.ChatCompletion(context.Background(), "gpt-4", messages)
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Equal(t, expectedError, err)

	provider.AssertExpectations(t)
}

func TestModelMultiplexer_Completion(t *testing.T) {
	provider := &MockProvider{}

	prompt := "Complete this sentence"
	expectedResponse := map[string]interface{}{
		"id": "test-completion",
		"choices": []map[string]interface{}{
			{"text": " with something interesting."},
		},
	}

	provider.On("Completion", mock.Anything, "gpt-3.5-turbo-instruct", prompt).Return(expectedResponse, nil)

	mux := &ModelMultiplexer{
		providers: []providers.Provider{provider},
		modelMap: map[string]providers.Provider{
			"gpt-3.5-turbo-instruct": provider,
		},
	}

	result, err := mux.Completion(context.Background(), "gpt-3.5-turbo-instruct", prompt)
	require.NoError(t, err)
	assert.Equal(t, expectedResponse, result)

	provider.AssertExpectations(t)
}

func TestModelMultiplexer_ModelNotFound(t *testing.T) {
	mux := &ModelMultiplexer{
		providers: []providers.Provider{},
		modelMap:  map[string]providers.Provider{},
	}

	result, err := mux.ChatCompletion(context.Background(), "nonexistent-model", nil)
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "no provider available")
}
