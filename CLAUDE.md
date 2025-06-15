# Modelplex Project Memory

## Project Overview
**Modelplex** is a production-ready system for running AI agents in complete network isolation through Unix socket communication. It acts as a proxy/multiplexer between isolated environments (VMs, containers) and AI providers (OpenAI, Anthropic, Ollama).

**Data Flows:**
- Guest VM LLM Agent → Unix Socket → Modelplex Proxy → Model Multiplexer → AI Providers (OpenAI/Anthropic/Ollama)
- Guest VM LLM Agent ← Unix Socket ← Modelplex Proxy ← Model Multiplexer ← AI Providers (responses)
- Modelplex MCP Client → MCP Servers (filesystem, database, etc.)

**Communication:**
- **Transport**: Unix domain socket (modelplex.socket)
- **Protocol**: HTTP with OpenAI-compatible API
- **Authentication**: API keys managed by Modelplex host
- **Isolation**: Complete network isolation for guest environments

## Development Workflow

### 1. Setup & Environment
- **Language**: Go 1.24.4+ for security patches
- **Config**: TOML with `${ENV_VAR_NAME}` substitution
- **CLI**: jessevdk/go-flags
- **Testing**: testify framework
- **Logging**: slog structured logging (never standard log package)

### 2. Branch & Development Process
1. **Create branch**: `{gh username}/{feature-name}` format
2. **Code following standards**: See Code Quality section below
3. **Run tests**: `go test -v ./...` and `go test -v -race ./...`
4. **Run linting**: `golangci-lint run`, `gofmt -s -w .`, `goimports -w .`
5. **Test Docker builds** if applicable
6. **Create PR** with detailed technical context

### 3. Code Quality Standards
- **Comments focus on "why"** - explain reasoning, not what code does
- **Documentation comments** for all exported functions/types (Go convention)
- **Structured logging** with slog key-value pairs
- **No sensitive data in logs**
- **OpenAI API compatibility** must be maintained
- **Synchronization patterns**:
  - `errgroup.Group` with `.Go()` and `.Wait()` for goroutines
  - Channels for coordination
  - `context.WithTimeout()` for timeouts instead of sleeps

### 4. Testing Requirements
- **No sleeps in tests** - use synchronization primitives instead
- **Use `t.Context()`** when functions need context
- **Require** 100% pass rate
- **Ensure** security/vulnerability checks pass

### 5. PR Review & Merge
- All PRs require review and approval
- CI/CD must pass: tests, linting, security scans, builds
- No merge until all conversations resolved

### 6. Docker Strategy
- **Base**: golang:1.24.4-alpine (matches go.mod)
- **Multi-stage builds** for minimal production images and security-hardened runner
- **Non-root user**: modelplex:1001
- **Static compilation**: CGO_ENABLED=0

### 7. CI/CD Pipeline
- **Security scanning**: gosec, govulncheck
- **Multi-platform builds**: Linux, macOS, Windows, ARM64
- **Docker integration** with GitHub Container Registry
- **Registry**: ghcr.io/modelplex/modelplex

## Technical Architecture

### Directory Structure
```
cmd/modelplex/           # CLI entry point
internal/
├── config/              # TOML configuration
├── multiplexer/         # Model routing and provider selection
├── providers/           # AI provider implementations
├── proxy/               # OpenAI-compatible API proxy
├── server/              # Unix socket HTTP server
├── mcp/                 # Model Context Protocol integration
└── monitoring/          # Structured logging utilities
test/
├── integration/         # Full system tests
└── testutil/           # Test helpers
```

### Key Dependencies
```go
require (
    github.com/gorilla/mux v1.8.1           // HTTP routing
    github.com/jessevdk/go-flags v1.5.0     // CLI parsing
    github.com/pelletier/go-toml/v2 v2.1.1  // TOML config
    github.com/stretchr/testify v1.8.4      // Testing
)
```

### Provider Implementations

**OpenAI Provider**
- Full API compatibility, Bearer token auth
- Direct passthrough for requests/responses

**Anthropic Provider**
- x-api-key header, anthropic-version: "2023-06-01"
- System message transformation, response normalization

**Ollama Provider**
- No authentication, local endpoints (/api/chat, /api/generate)
- stream: false parameter, response normalization

### Configuration Format
```toml
[[providers]]
name = "openai"
type = "openai"
base_url = "https://api.openai.com/v1"
api_key = "${OPENAI_API_KEY}"
models = ["gpt-4", "gpt-3.5-turbo"]
priority = 1

[mcp]
enabled = true
servers = [
    { name = "filesystem", command = "mcp-server-filesystem", args = ["/workspace"] }
]
```

## API & Configuration

### Supported Endpoints
- `POST /v1/chat/completions` - Chat completions (all providers)
- `POST /v1/completions` - Text completions (OpenAI, Ollama)
- `GET /v1/models` - List available models
- `GET /health` - Health check

### Environment Variables
- `OPENAI_API_KEY` - OpenAI authentication
- `ANTHROPIC_API_KEY` - Anthropic authentication

### File Requirements
- **config.toml** - default configuration
- **modelplex.socket** - default Unix socket path
- **.dockerignore** - exclude development files

## Security & Performance

## Future Development

### Adding New Providers
1. Implement Provider interface in internal/providers/
2. Add configuration parsing in internal/config/
3. Add comprehensive tests with mocks
4. Update multiplexer registration
5. Document API differences
6. Add integration tests

### MCP Pass-through Proxy (Future)
Currently only implements MCP client. Future: MCP server implementation to accept external clients.

**Target Flow:**
```
External MCP Client -> Modelplex MCP Server -> Backend MCP Server 1
                                           -> Backend MCP Server 2
                                           -> Backend MCP Server 3
```

## Troubleshooting

### Common Issues
- **Docker builds**: Ensure Go version matches Dockerfile/go.mod
- **Tests**: Run `go mod tidy`, check race conditions
- **CI/CD**: Use govulncheck (not Nancy), upload-artifact v4+

### Success Metrics
- All tests pass with race detection
- golangci-lint, gofmt, goimports clean
- govulncheck passes
- All CI/CD workflows pass
- Documentation up to date

This knowledge base enables efficient development while maintaining high quality and security standards.
