// Package server provides HTTP server functionality over Unix domain sockets.
package server

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"os"
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

// Server provides HTTP server functionality over Unix domain sockets.
type Server struct {
	config     *config.Config
	socketPath string
	listener   net.Listener
	server     *http.Server
	mux        *multiplexer.ModelMultiplexer
	proxy      *proxy.OpenAIProxy
}

// New creates a new server instance with the given configuration and socket path.
func New(cfg *config.Config, socketPath string) *Server {
	muxer := multiplexer.New(cfg.Providers)
	pr := proxy.New(muxer)

	return &Server{
		config:     cfg,
		socketPath: socketPath,
		mux:        muxer,
		proxy:      pr,
	}
}

// Start starts the HTTP server listening on the Unix socket.
func (s *Server) Start() error {
	if err := os.RemoveAll(s.socketPath); err != nil {
		return err
	}

	listener, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return err
	}
	s.listener = listener

	router := mux.NewRouter()
	s.setupRoutes(router)

	s.server = &http.Server{
		Handler:      router,
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
	}

	slog.Info("Modelplex server listening", "socket", s.socketPath)
	return s.server.Serve(listener)
}

// Stop gracefully shuts down the server and cleans up the Unix socket.
func (s *Server) Stop() {
	if s.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := s.server.Shutdown(ctx); err != nil {
			slog.Error("Error shutting down server", "error", err)
		}
	}
	if s.listener != nil {
		if err := s.listener.Close(); err != nil {
			slog.Error("Error closing listener", "error", err)
		}
	}
	if err := os.RemoveAll(s.socketPath); err != nil {
		slog.Error("Error removing socket path", "path", s.socketPath, "error", err)
	}
}

func (s *Server) setupRoutes(router *mux.Router) {
	v1 := router.PathPrefix("/v1").Subrouter()

	// OpenAI-compatible endpoints
	v1.HandleFunc("/chat/completions", s.proxy.HandleChatCompletions).Methods("POST")
	v1.HandleFunc("/completions", s.proxy.HandleCompletions).Methods("POST")
	v1.HandleFunc("/models", s.proxy.HandleModels).Methods("GET")

	// Health check
	router.HandleFunc("/health", s.handleHealth).Methods("GET")
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte(`{"status":"ok","service":"modelplex"}`)); err != nil {
		slog.Error("Error writing health response", "error", err)
	}
}
