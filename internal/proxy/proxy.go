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
	Stream   bool                     `json:"stream,omitempty"`
}

// CompletionRequest represents an OpenAI completion request.
type CompletionRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream,omitempty"`
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

	if req.Stream {
		p.handleChatCompletionStream(w, r, model, req.Messages)
	} else {
		p.handleChatCompletion(w, r, model, req.Messages)
	}
}

// HandleCompletions handles completion requests.
func (p *OpenAIProxy) HandleCompletions(w http.ResponseWriter, r *http.Request) {
	var req CompletionRequest
	if err := p.decodeJSONRequest(r, &req, w); err != nil {
		return
	}

	model := p.normalizeModel(req.Model)

	if req.Stream {
		p.handleCompletionStream(w, r, model, req.Prompt)
	} else {
		p.handleCompletion(w, r, model, req.Prompt)
	}
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

func (p *OpenAIProxy) handleChatCompletionStream(w http.ResponseWriter, r *http.Request,
	model string, messages []map[string]interface{}) {
	streamChan, err := p.mux.ChatCompletionStream(r.Context(), model, messages)
	if err != nil {
		slog.Error("Chat completion stream failed", "error", err)
		writeError(w, http.StatusInternalServerError, "Internal server error")
		return
	}
	p.writeSSEResponse(w, streamChan, "chat completion stream")
}

func (p *OpenAIProxy) handleChatCompletion(w http.ResponseWriter, r *http.Request,
	model string, messages []map[string]interface{}) {
	result, err := p.mux.ChatCompletion(r.Context(), model, messages)
	p.handleResponse(w, result, err, "chat completion")
}

func (p *OpenAIProxy) handleCompletionStream(w http.ResponseWriter, r *http.Request, model, prompt string) {
	streamChan, err := p.mux.CompletionStream(r.Context(), model, prompt)
	if err != nil {
		slog.Error("Completion stream failed", "error", err)
		writeError(w, http.StatusInternalServerError, "Internal server error")
		return
	}
	p.writeSSEResponse(w, streamChan, "completion stream")
}

func (p *OpenAIProxy) handleCompletion(w http.ResponseWriter, r *http.Request, model, prompt string) {
	result, err := p.mux.Completion(r.Context(), model, prompt)
	p.handleResponse(w, result, err, "completion")
}

func (p *OpenAIProxy) decodeJSONRequest(r *http.Request, req interface{}, w http.ResponseWriter) error {
	if err := json.NewDecoder(r.Body).Decode(req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Invalid JSON: %v", err))
		return err
	}
	return nil
}

func (p *OpenAIProxy) handleResponse(w http.ResponseWriter, result interface{}, err error, operation string) {
	if err != nil {
		slog.Error("Operation failed", "operation", operation, "error", err)
		writeError(w, http.StatusInternalServerError, "Internal server error")
		return
	}
	p.writeJSONResponse(w, result, operation)
}

func (p *OpenAIProxy) writeJSONResponse(w http.ResponseWriter, data interface{}, responseType string) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(data); err != nil {
		slog.Error("Failed to encode response", "type", responseType, "error", err)
		// Can't write error response here as headers are already sent
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

func (p *OpenAIProxy) writeSSEResponse(w http.ResponseWriter, streamChan <-chan interface{}, operation string) {
	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	flusher, ok := w.(http.Flusher)
	if !ok {
		slog.Error("Response writer does not support flushing", "operation", operation)
		return
	}

	// Write streaming chunks
	for chunk := range streamChan {
		// Marshal the chunk to JSON
		jsonData, err := json.Marshal(chunk)
		if err != nil {
			slog.Error("Failed to marshal streaming chunk", "operation", operation, "error", err)
			continue
		}

		// Write in SSE format: "data: <json>\n\n"
		if _, err := fmt.Fprintf(w, "data: %s\n\n", jsonData); err != nil {
			slog.Error("Failed to write streaming chunk", "operation", operation, "error", err)
			return
		}

		flusher.Flush()
	}

	// Write the [DONE] marker
	if _, err := fmt.Fprint(w, "data: [DONE]\n\n"); err != nil {
		slog.Error("Failed to write DONE marker", "operation", operation, "error", err)
	}
	flusher.Flush()
}
