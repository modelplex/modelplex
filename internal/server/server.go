// Package server provides HTTP server functionality over Unix domain sockets.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/mux"

	"github.com/modelplex/modelplex/internal/config"
	"github.com/modelplex/modelplex/internal/multiplexer"
	"github.com/modelplex/modelplex/internal/proxy"
)

const (
	// Server timeout constants
	shutdownTimeout = 5 * time.Second
	readTimeout     = 30 * time.Second
	writeTimeout    = 30 * time.Second
)

// Server provides HTTP server functionality over Unix domain sockets or HTTP.
type Server struct {
	config     *config.Config
	socketPath string
	host       string
	port       int
	httpAddr   string
	useSocket  bool
	// Channel-based coordination instead of mutex
	ready    chan struct{}                // Signals when server is ready
	done     chan struct{}                // Signals when server should stop
	listener atomic.Pointer[net.Listener] // Atomic access to listener
	server   atomic.Pointer[http.Server]  // Atomic access to server
	mux      *multiplexer.ModelMultiplexer
	proxy    *proxy.OpenAIProxy
	startMtx sync.Mutex
	started  chan struct{}
}

// NewWithSocket creates a new server instance with Unix socket.
func NewWithSocket(cfg *config.Config, socketPath string) *Server {
	mux := multiplexer.New(cfg.Providers)
	proxy := proxy.New(mux)

	return &Server{
		config:     cfg,
		socketPath: socketPath,
		useSocket:  true,
		ready:      make(chan struct{}),
		done:       make(chan struct{}),
		mux:        mux,
		proxy:      proxy,
		started:    make(chan struct{}),
	}
}

// NewWithHTTPAddress creates a new server instance with HTTP using address string.
func NewWithHTTPAddress(cfg *config.Config, addr string) *Server {
	mux := multiplexer.New(cfg.Providers)
	proxy := proxy.New(mux)

	return &Server{
		config:    cfg,
		httpAddr:  addr,
		useSocket: false,
		ready:     make(chan struct{}),
		done:      make(chan struct{}),
		mux:       mux,
		proxy:     proxy,
		started:   make(chan struct{}),
	}
}

// NewWithHTTP creates a new server instance with HTTP.
func NewWithHTTP(cfg *config.Config, host string, port int) *Server {
	mux := multiplexer.New(cfg.Providers)
	proxy := proxy.New(mux)

	return &Server{
		config:    cfg,
		host:      host,
		port:      port,
		useSocket: false,
		ready:     make(chan struct{}),
		done:      make(chan struct{}),
		mux:       mux,
		proxy:     proxy,
		started:   make(chan struct{}),
	}
}

// New creates a new server instance with the given configuration and socket path.
// Deprecated: Use NewWithSocket or NewWithHTTP instead.
func New(cfg *config.Config, socketPath string) *Server {
	return NewWithSocket(cfg, socketPath)
}

// Start starts the HTTP server listening on either Unix socket or HTTP port.
func (s *Server) Start() <-chan error {
	done := make(chan error, 1)
	err := func() (err error) {
		s.startMtx.Lock()
		defer s.startMtx.Unlock()

		if s.listener.Load() != nil {
			return errors.New("server is already running")
		}

		var listener net.Listener

		if s.useSocket {
			// Check if socket already exists and error if it does
			if _, err := os.Stat(s.socketPath); err == nil {
				return fmt.Errorf("socket file already exists: %s", s.socketPath)
			}
			listener, err = net.Listen("unix", s.socketPath)
			if err != nil {
				return fmt.Errorf("failed to listen on socket: %w", err)
			}
			slog.Info("Modelplex server listening", "socket", s.socketPath)
		} else {
			var addr string
			if s.httpAddr != "" {
				addr = s.httpAddr
			} else {
				addr = fmt.Sprintf("%s:%d", s.host, s.port)
			}
			listener, err = net.Listen("tcp", addr)
			if err != nil {
				return fmt.Errorf("failed to listen on address: %w", err)
			}
			slog.Info("Modelplex server listening", "address", addr)
		}

		// Store listener atomically
		s.listener.Store(&listener)
		close(s.started)
		close(s.ready)
		return nil
	}()
	if err != nil {
		done <- err
		return done
	}

	// Set up server
	router := mux.NewRouter()
	s.setupRoutes(router)

	server := &http.Server{
		Handler:      router,
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
	}

	// Store server atomically
	s.server.Store(server)

	go func() {
		listenerPtr := s.listener.Load()
		if listenerPtr != nil {
			done <- server.Serve(*listenerPtr)
		}
	}()
	return done
}

// Stop gracefully shuts down the server and cleans up resources.
// It doesn't return an error because it operates idempotently.
func (s *Server) Stop(ctx context.Context) {
	select {
	case <-s.started:
	default:
		slog.Warn("Server not started, nothing to stop")
		return
	}

	// Shutdown server with timeout
	serverPtr := s.server.Load()
	if serverPtr != nil {
		if err := serverPtr.Shutdown(ctx); err != nil {
			slog.Error("Error shutting down server", "error", err)
		}
	}

	// Close listener
	listenerPtr := s.listener.Load()
	if listenerPtr != nil {
		if err := (*listenerPtr).Close(); err != nil {
			slog.Error("Error closing listener", "error", err)
		}
	}

	// Clean up socket file if using socket
	if s.useSocket {
		if err := os.Remove(s.socketPath); err != nil && !os.IsNotExist(err) {
			slog.Error("Error removing socket file", "path", s.socketPath, "error", err)
		}
	}
}

// Ready returns a channel that will be closed when the server is ready to accept connections.
func (s *Server) Ready() <-chan struct{} {
	return s.ready
}

// WaitReady waits for the server to be ready with a timeout.
func (s *Server) WaitReady(timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	select {
	case <-s.ready:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("timeout waiting for server to be ready")
	}
}

// Addr returns the actual network address the server is listening on.
// Returns nil if the server is not started or is using a Unix socket.
func (s *Server) Addr() net.Addr {
	if s.useSocket {
		return nil
	}

	listenerPtr := s.listener.Load()
	if listenerPtr == nil {
		return nil
	}

	return (*listenerPtr).Addr()
}

// SocketPath returns the Unix socket path if the server is using a socket.
// Returns empty string if the server is using HTTP.
func (s *Server) SocketPath() string {
	if s.useSocket {
		return s.socketPath
	}
	return ""
}

func (s *Server) setupRoutes(router *mux.Router) {
	// OpenAI-compatible endpoints under /models/v1
	modelsV1 := router.PathPrefix("/models/v1").Subrouter()
	modelsV1.HandleFunc("/chat/completions", s.proxy.HandleChatCompletions).Methods("POST")
	modelsV1.HandleFunc("/completions", s.proxy.HandleCompletions).Methods("POST")
	modelsV1.HandleFunc("/models", s.proxy.HandleModels).Methods("GET")

	// MCP-style RPC under /mcp/v1
	mcpV1 := router.PathPrefix("/mcp/v1").Subrouter()
	mcpV1.HandleFunc("/tools", s.handleMCPTools).Methods("GET")
	mcpV1.HandleFunc("/tools/{tool}/call", s.handleMCPToolCall).Methods("POST")

	// Internal host-only RPC under /_internal (only available on HTTP, not socket)
	if !s.useSocket {
		internal := router.PathPrefix("/_internal").Subrouter()
		internal.HandleFunc("/status", s.handleInternalStatus).Methods("GET")
		internal.HandleFunc("/config", s.handleInternalConfig).Methods("GET")
		internal.HandleFunc("/metrics", s.handleInternalMetrics).Methods("GET")
	}

	// Health check at root level
	router.HandleFunc("/health", s.handleHealth).Methods("GET")

	// Backward compatibility: Keep old /v1 endpoints for now
	v1 := router.PathPrefix("/v1").Subrouter()
	v1.HandleFunc("/chat/completions", s.proxy.HandleChatCompletions).Methods("POST")
	v1.HandleFunc("/completions", s.proxy.HandleCompletions).Methods("POST")
	v1.HandleFunc("/models", s.proxy.HandleModels).Methods("GET")
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte(`{"status":"ok","service":"modelplex"}`)); err != nil {
		slog.Error("Error writing health response", "error", err)
	}
}

// MCP endpoint handlers
func (s *Server) handleMCPTools(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	// TODO: Implement MCP tools listing
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte(`{"tools":[],"message":"MCP tools endpoint - implementation pending"}`)); err != nil {
		slog.Error("Error writing MCP tools response", "error", err)
	}
}

func (s *Server) handleMCPToolCall(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	// TODO: Implement MCP tool calling
	w.WriteHeader(http.StatusOK)
	message := `{"result":null,"message":"MCP tool call endpoint - implementation pending"}`
	if _, err := w.Write([]byte(message)); err != nil {
		slog.Error("Error writing MCP tool call response", "error", err)
	}
}

// Internal endpoint handlers (only available on HTTP, not socket)
func (s *Server) handleInternalStatus(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	status := map[string]interface{}{
		"service":     "modelplex",
		"status":      "running",
		"mode":        "http",
		"providers":   len(s.config.Providers),
		"mcp_servers": len(s.config.MCP.Servers),
	}

	// Add address information
	if s.httpAddr != "" {
		status["address"] = s.httpAddr
	} else if s.host != "" && s.port != 0 {
		status["host"] = s.host
		status["port"] = s.port
	}

	if err := json.NewEncoder(w).Encode(status); err != nil {
		slog.Error("Error writing internal status response", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

func (s *Server) handleInternalConfig(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	// Return sanitized config (without API keys)
	sanitizedConfig := map[string]interface{}{
		"server": s.config.Server,
		"providers": func() []map[string]interface{} {
			var providers []map[string]interface{}
			for _, p := range s.config.Providers {
				providers = append(providers, map[string]interface{}{
					"name":     p.Name,
					"type":     p.Type,
					"base_url": p.BaseURL,
					"models":   p.Models,
					"priority": p.Priority,
					// Exclude API key for security
				})
			}
			return providers
		}(),
		"mcp": s.config.MCP,
	}
	if err := json.NewEncoder(w).Encode(sanitizedConfig); err != nil {
		slog.Error("Error writing internal config response", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

func (s *Server) handleInternalMetrics(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	// TODO: Implement metrics collection
	metrics := map[string]interface{}{
		"requests_total":   0,
		"requests_success": 0,
		"requests_error":   0,
		"uptime_seconds":   0,
		"message":          "Metrics collection - implementation pending",
	}
	if err := json.NewEncoder(w).Encode(metrics); err != nil {
		slog.Error("Error writing internal metrics response", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}
