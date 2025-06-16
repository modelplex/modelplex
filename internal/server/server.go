// Package server provides HTTP server functionality over Unix domain sockets.
package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"sync"
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
	startMtx   sync.Mutex
	started    chan struct{}
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
		started:    make(chan struct{}),
	}
}

// Start starts the HTTP server listening on the Unix socket.
func (s *Server) Start() <-chan error {
	done := make(chan error, 1)
	err := func() (err error) {
		s.startMtx.Lock()
		defer s.startMtx.Unlock()

		if s.listener != nil {
			return errors.New("server is already running")
		}

		// Check if socket already exists
		_, err = os.Stat(s.socketPath)
		if err == nil {
			return fmt.Errorf("socket file already exists: %s", s.socketPath)
		}

		// Create listener
		s.listener, err = net.Listen("unix", s.socketPath)
		if err != nil {
			return fmt.Errorf("failed to listen on socket: %w", err)
		}
		close(s.started)
		return nil
	}()
	if err != nil {
		done <- err
		return done
	}

	// Mutex no longer necessary after s.started is closed

	// Set up server
	router := mux.NewRouter()
	s.setupRoutes(router)

	s.server = &http.Server{
		Handler:      router,
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
	}

	slog.Info("Modelplex server listening", "socket", s.socketPath)

	go func() {
		done <- s.server.Serve(s.listener)
	}()
	return done
}

// Stop gracefully shuts down the server and cleans up the Unix socket.
// It doesn't return an error because it operates idempotently.
func (s *Server) Stop(ctx context.Context) {
	select {
	case <-s.started:
	default:
		slog.Warn("Server not started, nothing to stop")
		return
	}

	if s.listener == nil {
		return // Nothing to stop
	}

	// Shutdown server with timeout
	if s.server != nil {
		if err := s.server.Shutdown(ctx); err != nil {
			slog.Error("Error shutting down server", "error", err)
		}
	}

	// Close listener
	if err := s.listener.Close(); err != nil {
		slog.Error("Error closing listener", "error", err)
	}

	// Clean up socket file
	if err := os.Remove(s.socketPath); err != nil && !os.IsNotExist(err) {
		slog.Error("Error removing socket file", "path", s.socketPath, "error", err)
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
