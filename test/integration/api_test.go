package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/modelplex/modelplex/internal/config"
	"github.com/modelplex/modelplex/internal/server"
)

func TestIntegration_FullAPIFlow(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create temporary socket
	tmpDir := t.TempDir()
	socketPath := filepath.Join(tmpDir, "test.socket")

	// Create test config
	cfg := &config.Config{
		Server: config.Server{
			LogLevel:       "info",
			MaxRequestSize: 1024 * 1024,
		},
		Providers: []config.Provider{
			{
				Name:     "test-provider",
				Type:     "openai",
				BaseURL:  "http://localhost:8999",
				APIKey:   "test-key",
				Models:   []string{"test-model"},
				Priority: 1,
			},
		},
		MCP: config.MCPConfig{
			Servers: []config.MCPServer{},
		},
	}

	// Start test server
	srv := server.New(cfg, socketPath)
	done := srv.Start()
	select {
	case err := <-done:
		if err != nil && err != http.ErrServerClosed {
			t.Fatalf("Failed to start server: %v", err)
		}
	default:
	}

	// Verify socket exists
	_, err := os.Stat(socketPath)
	require.NoError(t, err)

	defer srv.Stop(t.Context())

	// Test health endpoint
	t.Run("Health Check", func(t *testing.T) {
		response := makeUnixRequest(t, socketPath, "GET", "/health", nil)
		defer response.Body.Close()
		assert.Equal(t, 200, response.StatusCode)

		var healthResponse map[string]interface{}
		err := json.NewDecoder(response.Body).Decode(&healthResponse)
		require.NoError(t, err)
		assert.Equal(t, "ok", healthResponse["status"])
		assert.Equal(t, "modelplex", healthResponse["service"])
	})

	// Test models endpoint
	t.Run("List Models", func(t *testing.T) {
		response := makeUnixRequest(t, socketPath, "GET", "/v1/models", nil)
		defer response.Body.Close()
		assert.Equal(t, 200, response.StatusCode)

		var modelsResponse map[string]interface{}
		err := json.NewDecoder(response.Body).Decode(&modelsResponse)
		require.NoError(t, err)
		assert.Equal(t, "list", modelsResponse["object"])

		data := modelsResponse["data"].([]interface{})
		assert.NotEmpty(t, data)
	})

	// Test chat completions with error (since we don't have a real provider)
	t.Run("Chat Completions Error", func(t *testing.T) {
		requestBody := map[string]interface{}{
			"model": "test-model",
			"messages": []map[string]interface{}{
				{"role": "user", "content": "Hello"},
			},
		}

		body, err := json.Marshal(requestBody)
		require.NoError(t, err)

		response := makeUnixRequest(t, socketPath, "POST", "/v1/chat/completions", bytes.NewReader(body))
		defer response.Body.Close()
		// Expect error since we don't have a real provider
		assert.Equal(t, 500, response.StatusCode)
	})

	// Test invalid endpoints
	t.Run("Invalid Endpoint", func(t *testing.T) {
		response := makeUnixRequest(t, socketPath, "GET", "/invalid", nil)
		defer response.Body.Close()
		assert.Equal(t, 404, response.StatusCode)
	})
}

func TestIntegration_ConfigValidation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tmpDir := t.TempDir()

	tests := []struct {
		name        string
		config      *config.Config
		expectError bool
	}{
		{
			name: "valid minimal config",
			config: &config.Config{
				Server: config.Server{
					LogLevel: "info",
				},
				Providers: []config.Provider{},
			},
			expectError: false,
		},
		{
			name: "config with providers",
			config: &config.Config{
				Server: config.Server{
					LogLevel:       "debug",
					MaxRequestSize: 2048,
				},
				Providers: []config.Provider{
					{
						Name:     "provider1",
						Type:     "openai",
						BaseURL:  "https://api.example.com",
						APIKey:   "key1",
						Models:   []string{"model1"},
						Priority: 1,
					},
				},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			socketPath := filepath.Join(tmpDir, tt.name+".socket")
			srv := server.New(tt.config, socketPath)

			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()

			done := srv.Start()

			select {
			case err := <-done:
				if tt.expectError {
					assert.Error(t, err)
				} else if err != nil && err != http.ErrServerClosed {
					// Server start might return listener closed error when we stop it quickly
					t.Logf("Unexpected error: %v", err)
				}
			case <-ctx.Done():
				// Test timeout - this is expected for valid configs
				if !tt.expectError {
					// Server should be running, verify socket exists
					_, err := os.Stat(socketPath)
					assert.NoError(t, err)
				}
			}

			srv.Stop(t.Context())
		})
	}
}

// makeUnixRequest makes an HTTP request over a Unix socket
func makeUnixRequest(t *testing.T, socketPath, method, path string, body *bytes.Reader) *http.Response {
	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", socketPath)
			},
		},
		Timeout: 5 * time.Second,
	}

	var req *http.Request
	var err error

	if body != nil {
		req, err = http.NewRequest(method, "http://unix"+path, body)
	} else {
		req, err = http.NewRequest(method, "http://unix"+path, http.NoBody)
	}
	require.NoError(t, err)

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := client.Do(req)
	require.NoError(t, err)

	return resp
}
