# Claude Development Corrections

Specific corrections made during GitHub issue #17 implementation session.

## Code Review Feedback Addressed

### Test Synchronization
**Issue**: Prefer `errgroup.Go` for automatic synchronization
**Fix**: Replace plain goroutines with errgroup in tests, then simplify further
```go
// Before
go func() {
    if err := srv.Start(); err != nil && err != http.ErrServerClosed {
        t.Errorf("Failed to start server: %v", err)
    }
}()

// After (simplified)
eg, _ := errgroup.WithContext(context.Background())
eg.Go(srv.Start)
// ... test logic ...
_ = eg.Wait()
```

### Socket File Handling
**Issue**: Error if socket already exists instead of removing
**Fix**: Check for existence and error rather than silent removal
```go
// Before
if err := os.RemoveAll(s.socketPath); err != nil {
    return err
}

// After
if _, err := os.Stat(s.socketPath); err == nil {
    return fmt.Errorf("socket file already exists: %s", s.socketPath)
}
```

### Direct Assignment
**Issue**: Assign directly to `s.listener`
**Fix**: Remove intermediate variable
```go
// Before
listener, err = net.Listen("unix", s.socketPath)
s.listener = listener

// After
s.listener, err = net.Listen("unix", s.socketPath)
```

### Socket Cleanup
**Issue**: Prefer explicit error handling over silent removal
**Fix**: Use `os.Remove()` instead of `os.RemoveAll()` for single files
```go
// Before
if err := os.RemoveAll(s.socketPath); err != nil {
    slog.Error("Error removing socket path", ...)
}

// After
if err := os.Remove(s.socketPath); err != nil {
    slog.Error("Failed to remove socket file", ...)
}
```

## Git Practices
Always include the AI agent as the commit author, for example:
```bash
git commit --author="GitHub Copilot <noreply@github.com>" -m "commit message"
```

## Dependencies Added
- `golang.org/x/sync/errgroup` for better test synchronization

## Test Updates
- Converted `test_endpoints.sh` to Go integration tests in `test/integration/endpoints_test.go`
- Removed redundant `TestInternalEndpointsNotAvailableOnSocket` (covered by integration tests)
- Updated test expectations for `localhost:11435` default
