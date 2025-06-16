package proxy

import "context"

// Multiplexer defines the interface for model multiplexing
type Multiplexer interface {
	ChatCompletion(ctx context.Context, model string, messages []map[string]interface{}) (interface{}, error)
	Completion(ctx context.Context, model, prompt string) (interface{}, error)
	ListModels() []string
	
	// Streaming methods
	ChatCompletionStream(ctx context.Context, model string, messages []map[string]interface{}) (<-chan interface{}, error)
	CompletionStream(ctx context.Context, model, prompt string) (<-chan interface{}, error)
}
