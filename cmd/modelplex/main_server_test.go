package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"testing"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/modelplex/modelplex/internal/config"
	"github.com/modelplex/modelplex/internal/server"
)

func TestHTTPServerByDefault(t *testing.T) {
	// Create a test config
	cfg := &config.Config{
		Providers: []config.Provider{
			{
				Name:     "test",
				Type:     "openai",
				BaseURL:  "http://localhost:8080",
				APIKey:   "test-key",
				Models:   []string{"test-model"},
				Priority: 1,
			},
		},
		Server: config.Server{
			LogLevel:       "info",
			MaxRequestSize: 1024,
		},
	}

	// Test HTTP server creation
	srv := server.NewWithHTTPAddress(cfg, "127.0.0.1:0") // Use port 0 to get a random available port
	
	// Start server using errgroup
	eg, _ := errgroup.WithContext(context.Background())
	eg.Go(func() error {
		if err := srv.Start(); err != nil && err != http.ErrServerClosed {
			return err
		}
		return nil
	})
	
	// Give server time to start
	time.Sleep(100 * time.Millisecond)
	
	// Stop server
	srv.Stop()
	
	// Wait for server to finish
	_ = eg.Wait() // Ignore error as we expect server to be stopped
}

func TestSocketServerWhenSpecified(t *testing.T) {
	// Create a test config
	cfg := &config.Config{
		Providers: []config.Provider{
			{
				Name:     "test",
				Type:     "openai",
				BaseURL:  "http://localhost:8080",
				APIKey:   "test-key",
				Models:   []string{"test-model"},
				Priority: 1,
			},
		},
		Server: config.Server{
			LogLevel:       "info",
			MaxRequestSize: 1024,
		},
	}

	// Test socket server creation
	socketPath := "/tmp/test-modelplex.socket"
	srv := server.NewWithSocket(cfg, socketPath)
	
	// Start server using errgroup
	eg, _ := errgroup.WithContext(context.Background())
	eg.Go(func() error {
		if err := srv.Start(); err != nil && err != http.ErrServerClosed {
			return err
		}
		return nil
	})
	
	// Give server time to start
	time.Sleep(100 * time.Millisecond)
	
	// Check if socket file exists
	if _, err := os.Stat(socketPath); os.IsNotExist(err) {
		t.Errorf("Socket file was not created: %s", socketPath)
	}
	
	// Stop server
	srv.Stop()
	
	// Wait for server to finish
	_ = eg.Wait() // Ignore error as we expect server to be stopped
	
	// Check if socket file was cleaned up
	if _, err := os.Stat(socketPath); !os.IsNotExist(err) {
		t.Errorf("Socket file was not cleaned up: %s", socketPath)
	}
}

func TestInternalStatusEndpoint(t *testing.T) {
	// Create a test config
	cfg := &config.Config{
		Providers: []config.Provider{
			{Name: "test1", Type: "openai"},
			{Name: "test2", Type: "anthropic"},
		},
		MCP: config.MCPConfig{
			Servers: []config.MCPServer{
				{Name: "server1", Command: "test"},
			},
		},
		Server: config.Server{
			LogLevel:       "info",
			MaxRequestSize: 1024,
		},
	}

	// Find an available port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to get available port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()

	// Start HTTP server
	srv := server.NewWithHTTPAddress(cfg, fmt.Sprintf("127.0.0.1:%d", port))
	
	// Start server using errgroup
	eg, _ := errgroup.WithContext(context.Background())
	eg.Go(func() error {
		if err := srv.Start(); err != nil && err != http.ErrServerClosed {
			return err
		}
		return nil
	})
	
	// Give server time to start
	time.Sleep(200 * time.Millisecond)
	
	// Test internal status endpoint
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/_internal/status", port))
	if err != nil {
		t.Fatalf("Failed to get status: %v", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", resp.StatusCode)
	}
	
	var status map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		t.Fatalf("Failed to decode status response: %v", err)
	}
	
	// Verify status content
	expectedFields := []string{"service", "status", "mode", "address", "providers", "mcp_servers"}
	for _, field := range expectedFields {
		if _, exists := status[field]; !exists {
			t.Errorf("Expected field %s in status response", field)
		}
	}
	
	if status["service"] != "modelplex" {
		t.Errorf("Expected service=modelplex, got %v", status["service"])
	}
	
	if status["providers"] != float64(2) { // JSON numbers are float64
		t.Errorf("Expected 2 providers, got %v", status["providers"])
	}
	
	if status["mcp_servers"] != float64(1) {
		t.Errorf("Expected 1 MCP server, got %v", status["mcp_servers"])
	}
	
	// Stop server
	srv.Stop()
	
	// Wait for server to finish
	_ = eg.Wait() // Ignore error as we expect server to be stopped
}
