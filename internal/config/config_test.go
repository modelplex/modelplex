package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad(t *testing.T) {
	tests := []struct {
		name       string
		configData string
		wantErr    bool
		validate   func(*testing.T, *Config)
	}{
		{
			name: "valid config",
			configData: `
[server]
log_level = "info"
max_request_size = 10485760

[[providers]]
name = "openai"
type = "openai"
base_url = "https://api.openai.com/v1"
api_key = "test-key"
models = ["gpt-4", "gpt-3.5-turbo"]
priority = 1

[mcp]
[[mcp.servers]]
name = "filesystem"
command = "npx"
args = ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"]
`,
			wantErr: false,
			validate: func(t *testing.T, cfg *Config) {
				assert.Equal(t, "info", cfg.Server.LogLevel)
				assert.Equal(t, int64(10485760), cfg.Server.MaxRequestSize)
				require.Len(t, cfg.Providers, 1)
				assert.Equal(t, "openai", cfg.Providers[0].Name)
				assert.Equal(t, "openai", cfg.Providers[0].Type)
				assert.Equal(t, "https://api.openai.com/v1", cfg.Providers[0].BaseURL)
				assert.Equal(t, "test-key", cfg.Providers[0].APIKey)
				assert.Equal(t, []string{"gpt-4", "gpt-3.5-turbo"}, cfg.Providers[0].Models)
				assert.Equal(t, 1, cfg.Providers[0].Priority)
				require.Len(t, cfg.MCP.Servers, 1)
				assert.Equal(t, "filesystem", cfg.MCP.Servers[0].Name)
				assert.Equal(t, "npx", cfg.MCP.Servers[0].Command)
				assert.Equal(t, []string{"-y", "@modelcontextprotocol/server-filesystem", "/tmp"}, cfg.MCP.Servers[0].Args)
			},
		},
		{
			name: "minimal config",
			configData: `
[server]
log_level = "debug"
`,
			wantErr: false,
			validate: func(t *testing.T, cfg *Config) {
				assert.Equal(t, "debug", cfg.Server.LogLevel)
				assert.Equal(t, int64(0), cfg.Server.MaxRequestSize)
				assert.Empty(t, cfg.Providers)
				assert.Empty(t, cfg.MCP.Servers)
			},
		},
		{
			name:       "invalid toml",
			configData: `invalid toml content [[[`,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary config file
			tmpFile, err := os.CreateTemp("", "config-*.toml")
			require.NoError(t, err)
			defer os.Remove(tmpFile.Name())

			_, err = tmpFile.WriteString(tt.configData)
			require.NoError(t, err)
			if closeErr := tmpFile.Close(); closeErr != nil {
				t.Errorf("Failed to close temp file: %v", closeErr)
			}

			// Test loading
			cfg, err := Load(tmpFile.Name())
			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, cfg)
			if tt.validate != nil {
				tt.validate(t, cfg)
			}
		})
	}
}

func TestLoadNonExistentFile(t *testing.T) {
	_, err := Load("non-existent-file.toml")
	assert.Error(t, err)
}
