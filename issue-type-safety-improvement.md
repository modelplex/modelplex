# Improve type safety by reducing interface{} usage in streaming implementation

## Summary

The current streaming implementation makes extensive use of `interface{}` types which reduces type safety and makes the code harder to maintain and debug. We should introduce concrete types for better type safety and developer experience.

## Current State

### Main Areas Using interface{}:
- **Provider Interface** (`internal/proxy/interfaces.go:7,8,12,13`): Methods return `interface{}` and `<-chan interface{}`
- **Provider Implementation** (`internal/providers/provider.go:13,14,18,19`): Core provider interface with generic returns
- **Streaming Implementation** (`internal/providers/streaming.go`): Generic `interface{}` throughout streaming pipeline
- **Message Format**: `[]map[string]interface{}` instead of structured message types
- **HTTP Payloads**: Generic `interface{}` for request/response handling

### Specific Locations:
```go
// Current - internal/proxy/interfaces.go
ChatCompletion(ctx context.Context, model string, messages []map[string]interface{}) (interface{}, error)
ChatCompletionStream(ctx context.Context, model string, messages []map[string]interface{}) (<-chan interface{}, error)

// Current - internal/providers/streaming.go
type StreamingRequestConfig struct {
    Payload interface{}
    Transformer func(interface{}) interface{}
}
```

## Proposed Improvements

### 1. Define Concrete Types
```go
// Message types
type Message struct {
    Role    string `json:"role"`
    Content string `json:"content"`
    Name    string `json:"name,omitempty"`
}

// Response types
type ChatCompletionResponse struct {
    ID      string    `json:"id"`
    Object  string    `json:"object"`
    Created int64     `json:"created"`
    Model   string    `json:"model"`
    Choices []Choice  `json:"choices"`
    Usage   Usage     `json:"usage,omitempty"`
}

// Streaming chunk types
type StreamChunk struct {
    ID      string       `json:"id"`
    Object  string       `json:"object"`
    Created int64        `json:"created"`
    Model   string       `json:"model"`
    Choices []StreamChoice `json:"choices"`
}
```

### 2. Update Provider Interface
```go
type Provider interface {
    ChatCompletion(ctx context.Context, model string, messages []Message) (*ChatCompletionResponse, error)
    ChatCompletionStream(ctx context.Context, model string, messages []Message) (<-chan *StreamChunk, error)
    // ... other methods
}
```

### 3. Improve Streaming Configuration
```go
type StreamingRequestConfig struct {
    BaseURL  string
    Endpoint string
    Payload  interface{} // This could be more specific too
    Headers  map[string]string
    UseSSE   bool
    Transformer func(*StreamChunk) *StreamChunk // More specific transformer
}
```

## Benefits

1. **Type Safety**: Compile-time type checking instead of runtime errors
2. **IDE Support**: Better autocomplete, navigation, and refactoring
3. **Documentation**: Self-documenting code with clear data structures
4. **Debugging**: Easier to debug with concrete types instead of generic interfaces
5. **Maintenance**: Clearer contracts between components
6. **Performance**: Potential performance improvements by avoiding type assertions

## Implementation Strategy

### Phase 1: Core Types (Low Risk)
- Define concrete types for messages, responses, and stream chunks
- Keep existing interface{} methods as deprecated wrappers
- Add new typed methods alongside existing ones

### Phase 2: Provider Updates (Medium Risk)  
- Update provider implementations to use concrete types internally
- Maintain backwards compatibility during transition
- Update tests to use new types

### Phase 3: Interface Migration (Higher Risk)
- Update core interfaces to use concrete types
- Remove deprecated interface{} methods
- Update all callers to use new typed interfaces

## Testing Requirements

- Comprehensive unit tests for all new types
- Integration tests to ensure compatibility across providers
- Backwards compatibility tests during transition period
- Performance benchmarks to measure impact

## Priority

**Medium Priority** - This is a code quality improvement that will make the codebase more maintainable and safer, but doesn't affect functionality. Should be tackled when we have capacity for refactoring work.

---

*Originally identified in PR #35 code review by @shazow*