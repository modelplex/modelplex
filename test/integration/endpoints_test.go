package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/modelplex/modelplex/internal/config"
	"github.com/modelplex/modelplex/internal/server"
)

// getAvailablePort returns an available TCP port
func getAvailablePort(t *testing.T) int {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close() // Ignore close error for port allocation helper
	return port
}

// startServerWithErrgroup starts a server using errgroup and waits for it to be ready
func startServerWithErrgroup(t *testing.T, srv *server.Server) (cleanup func()) {
	eg, _ := errgroup.WithContext(context.Background())
	eg.Go(srv.Start)

	// Wait for server to be ready
	waitForServerReady(t, srv)

	return func() {
		srv.Stop()
		_ = eg.Wait() // Ignore expected shutdown errors
	}
}

// waitForServerReady waits for the server to be ready by checking its health endpoint or socket
func waitForServerReady(t *testing.T, srv *server.Server) {
	timeout := time.After(5 * time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			t.Fatal("Timeout waiting for server to be ready")
		case <-ticker.C:
			if srv.SocketPath() != "" {
				// Unix socket server - check if socket file exists
				if _, err := os.Stat(srv.SocketPath()); err == nil {
					return
				}
			} else if addr := srv.Addr(); addr != nil {
				// HTTP server - check health endpoint
				resp, err := http.Get(fmt.Sprintf("http://%s/health", addr))
				if err == nil {
					_ = resp.Body.Close() // Ignore close error in readiness check
					if resp.StatusCode == http.StatusOK {
						return
					}
				}
			}
		}
	}
}

// TestIntegration_HTTPEndpoints tests the full HTTP endpoint functionality
func TestIntegration_HTTPEndpoints(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create test config
	cfg := &config.Config{
		Providers: []config.Provider{
			{
				Name:     "test-openai",
				Type:     "openai",
				BaseURL:  "http://localhost:8080",
				APIKey:   "test-key",
				Models:   []string{"gpt-4", "gpt-3.5-turbo"},
				Priority: 1,
			},
			{
				Name:     "test-anthropic",
				Type:     "anthropic",
				BaseURL:  "http://localhost:8081",
				APIKey:   "test-key-2",
				Models:   []string{"claude-3-sonnet"},
				Priority: 2,
			},
		},
		MCP: config.MCPConfig{
			Servers: []config.MCPServer{
				{Name: "filesystem", Command: "test-fs", Args: []string{"/tmp"}},
				{Name: "brave-search", Command: "test-search"},
			},
		},
		Server: config.Server{
			LogLevel:       "info",
			MaxRequestSize: 1024 * 1024,
		},
	}

	// Find an available port and start server
	port := getAvailablePort(t)
	srv := server.NewWithHTTPAddress(cfg, fmt.Sprintf("127.0.0.1:%d", port))

	cleanup := startServerWithErrgroup(t, srv)
	defer cleanup()

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	client := &http.Client{Timeout: 5 * time.Second}

	t.Run("Health Check", func(t *testing.T) {
		resp, err := client.Get(baseURL + "/health")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var health map[string]interface{}
		err = json.NewDecoder(resp.Body).Decode(&health)
		require.NoError(t, err)

		assert.Equal(t, "ok", health["status"])
		assert.Equal(t, "modelplex", health["service"])
	})

	t.Run("OpenAI Models Endpoint (New Structure)", func(t *testing.T) {
		resp, err := client.Get(baseURL + "/models/v1/models")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var models map[string]interface{}
		err = json.NewDecoder(resp.Body).Decode(&models)
		require.NoError(t, err)

		assert.Equal(t, "list", models["object"])
		data := models["data"].([]interface{})
		assert.Len(t, data, 3) // gpt-4, gpt-3.5-turbo, claude-3-sonnet

		// Check model names
		modelNames := make([]string, len(data))
		for i, model := range data {
			modelMap := model.(map[string]interface{})
			modelNames[i] = modelMap["id"].(string)
		}
		assert.Contains(t, modelNames, "gpt-4")
		assert.Contains(t, modelNames, "gpt-3.5-turbo")
		assert.Contains(t, modelNames, "claude-3-sonnet")
	})

	t.Run("MCP Tools Endpoint", func(t *testing.T) {
		resp, err := client.Get(baseURL + "/mcp/v1/tools")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var tools map[string]interface{}
		err = json.NewDecoder(resp.Body).Decode(&tools)
		require.NoError(t, err)

		assert.Contains(t, tools, "tools")
		assert.Contains(t, tools, "message")
	})

	t.Run("Internal Status Endpoint", func(t *testing.T) {
		resp, err := client.Get(baseURL + "/_internal/status")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var status map[string]interface{}
		err = json.NewDecoder(resp.Body).Decode(&status)
		require.NoError(t, err)

		assert.Equal(t, "modelplex", status["service"])
		assert.Equal(t, "running", status["status"])
		assert.Equal(t, "http", status["mode"])
		assert.Equal(t, float64(2), status["providers"])   // 2 providers
		assert.Equal(t, float64(2), status["mcp_servers"]) // 2 MCP servers
		assert.Contains(t, status, "address")
	})

	t.Run("Internal Config Endpoint", func(t *testing.T) {
		resp, err := client.Get(baseURL + "/_internal/config")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var config map[string]interface{}
		err = json.NewDecoder(resp.Body).Decode(&config)
		require.NoError(t, err)

		assert.Contains(t, config, "server")
		assert.Contains(t, config, "providers")
		assert.Contains(t, config, "mcp")

		// Verify API keys are sanitized (not included)
		providers := config["providers"].([]interface{})
		for _, provider := range providers {
			providerMap := provider.(map[string]interface{})
			assert.NotContains(t, providerMap, "api_key")
		}
	})

	t.Run("Internal Metrics Endpoint", func(t *testing.T) {
		resp, err := client.Get(baseURL + "/_internal/metrics")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var metrics map[string]interface{}
		err = json.NewDecoder(resp.Body).Decode(&metrics)
		require.NoError(t, err)

		assert.Contains(t, metrics, "requests_total")
		assert.Contains(t, metrics, "message")
	})

	t.Run("Backward Compatibility - Old Models Endpoint", func(t *testing.T) {
		resp, err := client.Get(baseURL + "/v1/models")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var models map[string]interface{}
		err = json.NewDecoder(resp.Body).Decode(&models)
		require.NoError(t, err)

		assert.Equal(t, "list", models["object"])
		data := models["data"].([]interface{})
		assert.Len(t, data, 3) // Same models as new endpoint
	})

	t.Run("Invalid Endpoint Returns 404", func(t *testing.T) {
		resp, err := client.Get(baseURL + "/invalid/endpoint")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	})
}

// TestIntegration_SocketSecurity tests socket mode security restrictions
func TestIntegration_SocketSecurity(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create test config
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

	// Create temporary socket
	tmpDir := t.TempDir()
	socketPath := tmpDir + "/security-test.socket"

	// Start socket server
	srv := server.NewWithSocket(cfg, socketPath)

	cleanup := startServerWithErrgroup(t, srv)
	defer cleanup()

	// Create HTTP client for Unix socket
	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", socketPath)
			},
		},
		Timeout: 5 * time.Second,
	}

	t.Run("Regular Endpoints Available on Socket", func(t *testing.T) {
		endpoints := []string{
			"/health",
			"/models/v1/models",
			"/mcp/v1/tools",
			"/v1/models", // Backward compatibility
		}

		for _, endpoint := range endpoints {
			t.Run(endpoint, func(t *testing.T) {
				resp, err := client.Get("http://unix" + endpoint)
				require.NoError(t, err)
				defer resp.Body.Close()

				assert.Equal(t, http.StatusOK, resp.StatusCode)
			})
		}
	})

	t.Run("Internal Endpoints Blocked on Socket", func(t *testing.T) {
		internalEndpoints := []string{
			"/_internal/status",
			"/_internal/config",
			"/_internal/metrics",
		}

		for _, endpoint := range internalEndpoints {
			t.Run(endpoint, func(t *testing.T) {
				resp, err := client.Get("http://unix" + endpoint)
				require.NoError(t, err)
				defer resp.Body.Close()

				assert.Equal(t, http.StatusNotFound, resp.StatusCode,
					"Internal endpoint %s should not be available via socket", endpoint)
			})
		}
	})

	t.Run("Health Check Response Format", func(t *testing.T) {
		resp, err := client.Get("http://unix/health")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var health map[string]interface{}
		err = json.NewDecoder(resp.Body).Decode(&health)
		require.NoError(t, err)

		assert.Equal(t, "ok", health["status"])
		assert.Equal(t, "modelplex", health["service"])
	})
}

// TestIntegration_HTTPvsSocket compares HTTP and socket mode behavior
func TestIntegration_HTTPvsSocket(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

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

	t.Run("HTTP Mode", func(t *testing.T) {
		// Find available port
		port := getAvailablePort(t)

		// Create server and client
		srv := server.NewWithHTTPAddress(cfg, fmt.Sprintf("127.0.0.1:%d", port))
		client := &http.Client{Timeout: 5 * time.Second}
		baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)

		cleanup := startServerWithErrgroup(t, srv)
		defer cleanup()

		// Test common endpoints
		commonEndpoints := []string{"/health", "/models/v1/models", "/v1/models"}

		for _, endpoint := range commonEndpoints {
			t.Run("Common Endpoint: "+endpoint, func(t *testing.T) {
				resp, err := client.Get(baseURL + endpoint)
				require.NoError(t, err)
				defer resp.Body.Close()

				assert.Equal(t, http.StatusOK, resp.StatusCode)

				// Verify response has content
				body, err := io.ReadAll(resp.Body)
				require.NoError(t, err)
				assert.NotEmpty(t, body)
			})
		}

		// Test internal endpoints (should work in HTTP mode)
		internalEndpoint := "/_internal/status"
		resp, err := client.Get(baseURL + internalEndpoint)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("Socket Mode", func(t *testing.T) {
		// Create socket path
		socketPath := t.TempDir() + "/compare-test.socket"

		// Create server and client
		srv := server.NewWithSocket(cfg, socketPath)
		client := &http.Client{
			Transport: &http.Transport{
				DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
					return net.Dial("unix", socketPath)
				},
			},
			Timeout: 5 * time.Second,
		}
		baseURL := "http://unix"

		cleanup := startServerWithErrgroup(t, srv)
		defer cleanup()

		// Test common endpoints
		commonEndpoints := []string{"/health", "/models/v1/models", "/v1/models"}

		for _, endpoint := range commonEndpoints {
			t.Run("Common Endpoint: "+endpoint, func(t *testing.T) {
				resp, err := client.Get(baseURL + endpoint)
				require.NoError(t, err)
				defer resp.Body.Close()

				assert.Equal(t, http.StatusOK, resp.StatusCode)

				// Verify response has content
				body, err := io.ReadAll(resp.Body)
				require.NoError(t, err)
				assert.NotEmpty(t, body)
			})
		}

		// Test internal endpoints (should NOT work in socket mode)
		internalEndpoint := "/_internal/status"
		resp, err := client.Get(baseURL + internalEndpoint)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	})
}
