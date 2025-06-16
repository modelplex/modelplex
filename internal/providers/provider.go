package providers

import (
	"context"

	"github.com/modelplex/modelplex/internal/config"
)

// Provider defines the interface that all AI providers must implement.
type Provider interface {
	Name() string
	Priority() int
	ChatCompletion(ctx context.Context, model string, messages []map[string]interface{}) (interface{}, error)
	Completion(ctx context.Context, model, prompt string) (interface{}, error)
	ListModels() []string

	// Streaming methods
	ChatCompletionStream(ctx context.Context, model string, messages []map[string]interface{}) (<-chan interface{}, error)
	CompletionStream(ctx context.Context, model, prompt string) (<-chan interface{}, error)
}

// NewProvider creates a new provider instance based on the configuration type.
func NewProvider(cfg *config.Provider) Provider {
	switch cfg.Type {
	case "openai":
		return NewOpenAIProvider(cfg)
	case "anthropic":
		return NewAnthropicProvider(cfg)
	case "ollama":
		return NewOllamaProvider(cfg)
	default:
		return nil
	}
}
