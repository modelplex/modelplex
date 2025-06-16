## Contributing

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Run tests (`go test -v ./...`)
4. Commit your changes (`git commit -am 'Add amazing feature'`)
5. Push to the branch (`git push origin feature/amazing-feature`)
6. Create a Pull Request

### Development Guidelines

- **Go 1.24+** required
- **Comprehensive tests** for new features
- **Structured logging** with slog
- **Security-first** approach
- **OpenAI API compatibility** maintained

#### PR Guidelines

- **Docker Testing**: Include `[test-docker]` in your PR title to trigger Docker build and test workflows for non-main branch PRs. This is useful when testing Docker-related changes before merging to main.

### Testing

```bash
# Run all tests
go test -v ./...

# Run tests with race detection
go test -v -race ./...

# Run integration tests
go test -v -run Integration ./test/integration/...

# Generate coverage report
go test -v -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

