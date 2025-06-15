# Modelplex Project Memory

## Project Overview
**Modelplex** is a production-ready system for running AI agents in complete network isolation through Unix socket communication. It acts as a proxy/multiplexer between isolated environments (VMs, containers) and AI providers (OpenAI, Anthropic, Ollama).

### Core Architecture
```
+-----------------+    +------------------+    +-----------------+
|   Guest VM      |    |   Modelplex      |    |   Providers     |
|                 |    |   (Host)         |    |                 |
|  +-----------+  |    |  +-------------+ |    | +-------------+ |
|  |    LLM    |  |    |  |   Model     | |    | |   OpenAI    | |
|  |   Agent   |<-+----+->| Multiplexer |<+----+>|  Anthropic  | |
|  +-----------+  |    |  +-------------+ |    | |   Ollama    | |
|                 |    |  +-------------+ |    | +-------------+ |
|                 |    |  |    MCP      | |    |                 |
|                 |    |  | Integration |<+----+-> MCP Servers   |
|                 |    |  +-------------+ |    |                 |
+-----------------+    +------------------+    +-----------------+
        |                        |
        +----modelplex.socket----+
```

## Technology Stack & Requirements

### Core Technologies
- **Language**: Go 1.24+ (currently 1.24.4 for security patches)
- **Config**: TOML (not YAML) with environment variable substitution
- **CLI**: jessevdk/go-flags for professional CLI interface
- **HTTP Router**: gorilla/mux for OpenAI-compatible API
- **Testing**: testify framework with comprehensive test coverage
- **Logging**: structured logging with slog (not standard log package)

### Key Dependencies
```go
require (
    github.com/gorilla/mux v1.8.1
    github.com/jessevdk/go-flags v1.5.0
    github.com/pelletier/go-toml/v2 v2.1.1
    github.com/stretchr/testify v1.8.4
)
```

## Code Architecture & Components

### Directory Structure
```
cmd/modelplex/           # CLI application entry point
internal/
├── config/              # TOML configuration management
├── multiplexer/         # Model routing and provider selection
├── providers/           # AI provider implementations (OpenAI, Anthropic, Ollama)
├── proxy/               # OpenAI-compatible API proxy
├── server/              # Unix socket HTTP server
├── mcp/                 # Model Context Protocol integration
└── monitoring/          # Structured logging utilities
test/
├── integration/         # Full system integration tests
└── testutil/           # Test helper utilities
```

### Provider Implementations

#### OpenAI Provider
- Full OpenAI API compatibility
- Bearer token authentication
- Direct passthrough for requests/responses
- Supports chat completions and text completions

#### Anthropic Provider
- x-api-key header authentication
- anthropic-version: "2023-06-01" header required
- System message transformation (moved to separate field)
- Response format normalization to OpenAI-compatible

#### Ollama Provider
- No authentication required
- Local inference endpoints: /api/chat, /api/generate
- stream: false parameter required
- Response format normalization

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

## Development Guidelines & Standards

### Code Quality Standards
- **Code comments focused on "why"** - explain reasoning, not what the code does
- **Required documentation comments** for all exported functions, types, and identifiers (Go convention)
- **Auto-documentation support** - comments should be suitable for godoc generation
- **Structured logging** with slog using key-value pairs
- **Comprehensive testing** - unit tests for all components
- **Security-first approach** - no sensitive data in logs
- **OpenAI API compatibility** must be maintained
- **Go idioms** - follow standard Go conventions

### Comment Guidelines
- **Exported identifiers**: Must have documentation comments starting with the identifier name
- **Package documentation**: Package-level comments explaining purpose and usage
- **Complex logic**: Comments explaining "why" decisions were made, not "what" is happening
- **API differences**: Document how provider implementations differ from OpenAI standard
- **Security considerations**: Comment on security-related code decisions
- **Performance notes**: Explain performance trade-offs when relevant

### Testing Requirements
- **55+ tests** across all components with 100% pass rate
- **Unit tests** for providers, multiplexer, proxy, server
- **Integration tests** for full system validation
- **Mock-based testing** with testify framework
- **Race detection** for concurrent safety testing
- **Test commands**:
  - `go test -v ./...` - all tests
  - `go test -v -race ./...` - with race detection
  - `go test -v -run Integration ./test/integration/...` - integration only

### Security & Vulnerability Management
- **Go 1.24.4** required for latest security patches
- **govulncheck** for vulnerability scanning (replaced Nancy)
- **gosec** for security analysis
- **Structured logging** prevents injection attacks
- **Minimal dependencies** - only essential libraries
- **Regular dependency updates** via CI/CD

## CI/CD Pipeline & Infrastructure

### GitHub Actions Workflows
- **Multi-Go version testing** (1.23, 1.24.4)
- **Comprehensive linting** with golangci-lint
- **Security scanning** with gosec and govulncheck
- **Multi-platform builds** (Linux, macOS, Windows, ARM64)
- **Docker integration** with GitHub Container Registry
- **Codecov integration** for coverage reporting

### Docker & Container Strategy
- **Base image**: golang:1.24.4-alpine (matches go.mod)
- **Multi-stage builds** for minimal production images
- **Non-root user**: modelplex:1001 for security
- **Static binary compilation** with CGO_ENABLED=0
- **Health checks** for container monitoring
- **Registry**: ghcr.io/shazow/modelplex with multiple tags

### Container Tags Strategy
- `ghcr.io/shazow/modelplex:latest` - latest main branch
- `ghcr.io/shazow/modelplex:main-{10-char-sha}` - specific commits
- `ghcr.io/shazow/modelplex:main` - main branch tracking

## Development Workflow & Practices

### Branch Naming Convention
- Use `jzila/{feature-name}` format for all branches
- Examples: `jzila/update-readme`, `jzila/fix-docker-build`

### Commit Message Standards
- Use conventional commits format
- Types: feat, fix, docs, refactor, test, ci, security
- Include comprehensive descriptions with technical details
- Reference specific files and line numbers when relevant

### PR Creation Process
1. Create feature branch with `jzila/` prefix
2. Implement changes following code standards
3. Run full test suite locally
4. Run linting: `golangci-lint run`
5. Test Docker builds if applicable
6. Create PR with detailed description and technical context
7. Include breaking changes, testing info, and migration notes

### Code Review & Quality Checks
- All PRs require review and approval
- CI/CD must pass: tests, linting, security scans, builds
- No merge until all conversations resolved
- Maintain backward compatibility for OpenAI API

## Configuration & Environment

### Environment Variables
- `OPENAI_API_KEY` - OpenAI authentication
- `ANTHROPIC_API_KEY` - Anthropic authentication
- Config supports `${VAR_NAME}` substitution

### File Structure Requirements
- **config.toml** - default configuration file
- **modelplex.socket** - default Unix socket path
- **.dockerignore** - exclude development files from builds
- **justfile** - task automation (if present)

## API Compatibility & Endpoints

### Supported Endpoints
- `POST /v1/chat/completions` - Chat completions (all providers)
- `POST /v1/completions` - Text completions (OpenAI, Ollama)
- `GET /v1/models` - List available models
- `GET /health` - Health check endpoint

### Request/Response Handling
- All requests/responses follow OpenAI specification
- Provider-specific differences handled internally
- Model routing based on availability and priority
- Automatic failover to lower priority providers

## Performance & Optimization

### Build Optimizations
- Static binary compilation for portability
- Multi-stage Docker builds for minimal image size
- GitHub Actions caching for faster CI/CD
- Layer optimization in Dockerfiles

### Runtime Performance
- Priority-based provider routing
- Connection pooling and reuse
- Structured logging for minimal overhead
- Unix socket communication for maximum performance

## Security Considerations

### Network Isolation
- Unix domain sockets only - no network dependencies
- Complete isolation of guest environments
- Host-based provider communication only

### Container Security
- Non-root user execution (UID 1001)
- Minimal attack surface with Alpine base
- Static binaries with no external dependencies
- Regular security updates via automated CI/CD

### Data Protection
- No sensitive data in logs (structured logging prevents leaks)
- API keys managed via environment variables
- No persistent storage of credentials
- Audit trail through structured logging

## Troubleshooting & Common Issues

### Docker Build Failures
- Ensure Go version matches between Dockerfile and go.mod
- Check .dockerignore excludes development files
- Verify GHCR permissions for registry pushes

### Test Failures
- Run `go mod tidy` if dependency issues
- Check race conditions with `go test -race`
- Verify mock expectations in testify tests

### CI/CD Issues
- Nancy vulnerability scanner replaced with govulncheck
- Upload-artifact updated to v4 from deprecated v3
- Ensure all GitHub Actions use latest versions

## Future Development Guidelines

### Adding New Providers
1. Implement Provider interface in internal/providers/
2. Add configuration parsing in internal/config/
3. Add comprehensive tests with mocks
4. Update multiplexer registration
5. Document API differences and authentication
6. Add integration tests

### Extending MCP Integration
1. Follow Model Context Protocol specification
2. Add server configuration in TOML
3. Implement tool calling interfaces
4. Add proper error handling and timeouts
5. Test with real MCP servers

### MCP Pass-through Proxy (Future Feature)
Currently, Modelplex only implements an MCP client for internal tool execution. Future development should include:

1. **MCP Server Implementation** - Accept connections from external MCP clients
2. **Tool Aggregation** - Merge tools from multiple backend MCP servers into unified interface
3. **Request Routing** - Route tool calls from external clients to appropriate backend servers
4. **Protocol Support** - Handle MCP JSON-RPC over stdin/stdout and other transports
5. **Connection Management** - Manage multiple concurrent MCP client connections
6. **Namespace Management** - Handle tool name conflicts across multiple backend servers

**Target Architecture:**
```
External MCP Client -> Modelplex MCP Server -> Backend MCP Server 1
                                           -> Backend MCP Server 2
                                           -> Backend MCP Server 3
```

This would enable Modelplex to act as a centralized MCP proxy, allowing isolated environments to access multiple MCP servers through a single connection point while maintaining the same network isolation benefits.

### Performance Enhancements
- Profile with Go pprof before optimizing
- Maintain OpenAI API compatibility
- Test under load with realistic scenarios
- Monitor resource usage in containers

## Key Success Metrics

### Code Quality
- **55+ tests** with 100% pass rate maintained
- **Zero security vulnerabilities** in dependencies
- **Clean linting** with golangci-lint
- **Complete OpenAI API compatibility**

### Production Readiness
- **Multi-platform builds** working correctly
- **Docker containers** running in production
- **GitHub Container Registry** integration functional
- **Comprehensive documentation** and examples

This knowledge base should enable efficient development and maintenance of the Modelplex project while maintaining high quality and security standards.