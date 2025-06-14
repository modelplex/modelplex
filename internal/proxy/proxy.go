// Package proxy provides OpenAI-compatible API proxy functionality.
package proxy

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
)

const (
	// Default model creation timestamp for OpenAI compatibility
	defaultModelCreated = 1677610602
)

// OpenAIProxy provides OpenAI-compatible HTTP endpoints.
type OpenAIProxy struct {
	mux Multiplexer
}

// New creates a new OpenAI proxy with the given multiplexer.
func New(mux Multiplexer) *OpenAIProxy {
	return &OpenAIProxy{mux: mux}
}

// ChatCompletionRequest represents an OpenAI chat completion request.
type ChatCompletionRequest struct {
	Model    string                   `json:"model"`
	Messages []map[string]interface{} `json:"messages"`
}

// CompletionRequest represents an OpenAI completion request.
type CompletionRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

// ModelsResponse represents an OpenAI models list response.
type ModelsResponse struct {
	Object string      `json:"object"`
	Data   []ModelInfo `json:"data"`
}

// ModelInfo represents information about a single model.
type ModelInfo struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

// HandleChatCompletions handles chat completion requests.
func (p *OpenAIProxy) HandleChatCompletions(w http.ResponseWriter, r *http.Request) {
	var req ChatCompletionRequest
	if err := p.decodeJSONRequest(r, &req, w); err != nil {
		return
	}

	model := p.normalizeModel(req.Model)
	result, err := p.mux.ChatCompletion(r.Context(), model, req.Messages)
	p.handleResponse(w, result, err, "chat completion")
}

// HandleCompletions handles completion requests.
func (p *OpenAIProxy) HandleCompletions(w http.ResponseWriter, r *http.Request) {
	var req CompletionRequest
	if err := p.decodeJSONRequest(r, &req, w); err != nil {
		return
	}

	model := p.normalizeModel(req.Model)
	result, err := p.mux.Completion(r.Context(), model, req.Prompt)
	p.handleResponse(w, result, err, "completion")
}

// HandleModels handles model listing requests.
func (p *OpenAIProxy) HandleModels(w http.ResponseWriter, _ *http.Request) {
	models := p.mux.ListModels()

	data := make([]ModelInfo, len(models))
	for i, model := range models {
		data[i] = ModelInfo{
			ID:      model,
			Object:  "model",
			Created: defaultModelCreated,
			OwnedBy: "modelplex",
		}
	}

	response := ModelsResponse{
		Object: "list",
		Data:   data,
	}

	p.writeJSONResponse(w, response, "models")
}

func (p *OpenAIProxy) decodeJSONRequest(r *http.Request, req interface{}, w http.ResponseWriter) error {
	if err := json.NewDecoder(r.Body).Decode(req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid JSON: %v", err), http.StatusBadRequest)
		return err
	}
	return nil
}

func (p *OpenAIProxy) handleResponse(w http.ResponseWriter, result interface{}, err error, operation string) {
	if err != nil {
		slog.Error("Operation failed", "operation", operation, "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	p.writeJSONResponse(w, result, operation)
}

func (p *OpenAIProxy) writeJSONResponse(w http.ResponseWriter, data interface{}, responseType string) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(data); err != nil {
		slog.Error("Failed to encode response", "type", responseType, "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}

func (p *OpenAIProxy) normalizeModel(model string) string {
	if strings.HasPrefix(model, "modelplex-") {
		return strings.TrimPrefix(model, "modelplex-")
	}
	return model
}

func writeError(w http.ResponseWriter, statusCode int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	errorResp := map[string]interface{}{
		"error": map[string]interface{}{
			"message": message,
			"type":    "invalid_request_error",
		},
	}

	if err := json.NewEncoder(w).Encode(errorResp); err != nil {
		slog.Error("Failed to encode error response", "error", err)
	}
}
