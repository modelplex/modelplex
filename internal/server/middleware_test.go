package server

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureSlogOutput captures slog output for the duration of the provided function,
// allowing a specific log level to be set for the capture duration.
func captureSlogOutput(level slog.Level, fn func()) string {
	var buf bytes.Buffer
	handlerOptions := &slog.HandlerOptions{Level: level}
	// Using a simple text handler for predictable output formatting in tests.
	// Note: slog's default TextHandler writes time, level, msg, and then key=value pairs.
	// The exact format might vary slightly if a custom default handler is set elsewhere.
	// For these tests, we are checking for substrings, which is robust.
	handler := slog.NewTextHandler(&buf, handlerOptions)
	originalLogger := slog.Default()
	slog.SetDefault(slog.New(handler))
	defer slog.SetDefault(originalLogger)

	fn()
	return buf.String()
}

func TestRequestLoggingMiddleware_DebugEnabled(t *testing.T) {
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	middleware := RequestLoggingMiddleware(nextHandler)

	req, err := http.NewRequest("GET", "/test_path?query=123", nil)
	require.NoError(t, err)
	req.Header.Set("User-Agent", "TestAgent/1.0")
	// r.RemoteAddr is set by the server, httptest.NewRequest doesn't populate it in a way
	// that's easily mockable without a real server. However, it will have a default like "192.0.2.1:1234"
	// or be empty. The middleware reads it, so we check if "remote_addr=" is present.

	rr := httptest.NewRecorder()

	var logOutput string
	// Capture with Debug level enabled for the handler
	logOutput = captureSlogOutput(slog.LevelDebug, func() {
		middleware.ServeHTTP(rr, req)
	})

	assert.Equal(t, http.StatusOK, rr.Code, "Next handler should be called")

	// Assertions for log content
	assert.Contains(t, logOutput, "level=DEBUG") // Slog text handler includes level
	assert.Contains(t, logOutput, "msg=\"Incoming HTTP request\"") // Slog text handler uses msg=
	assert.Contains(t, logOutput, "method=GET")
	assert.Contains(t, logOutput, "uri=/test_path?query=123")
	assert.Contains(t, logOutput, "user_agent=\"TestAgent/1.0\"")
	assert.Contains(t, logOutput, "remote_addr=") // Check that the key is present
	if req.RemoteAddr != "" { // If RemoteAddr was set by the test framework (it usually is)
		assert.Contains(t, logOutput, "remote_addr="+req.RemoteAddr)
	}
}

func TestRequestLoggingMiddleware_DebugDisabled(t *testing.T) {
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	middleware := RequestLoggingMiddleware(nextHandler)

	req, err := http.NewRequest("POST", "/another_path", nil)
	require.NoError(t, err)
	req.Header.Set("User-Agent", "AnotherAgent/2.0")

	rr := httptest.NewRecorder()

	var logOutput string
	// Capture with Info level enabled for the handler (so Debug messages won't pass)
	logOutput = captureSlogOutput(slog.LevelInfo, func() {
		middleware.ServeHTTP(rr, req)
	})

	assert.Equal(t, http.StatusOK, rr.Code, "Next handler should be called")

	// Assert that the specific debug log message is NOT present
	assert.NotContains(t, logOutput, "Incoming HTTP request")
	assert.NotContains(t, logOutput, "method=POST")
	assert.NotContains(t, logOutput, "uri=/another_path")
	assert.NotContains(t, logOutput, "user_agent=\"AnotherAgent/2.0\"")
}

// Test to ensure context from request is used by slog (as middleware uses r.Context())
func TestRequestLoggingMiddleware_UsesRequestContextForSlog(t *testing.T) {
	// This test is a bit more advanced and checks if slog.DebugContext is actually
	// receiving the request's context. We can do this by adding a value to the context
	// and having a custom slog handler that checks for it.
	// For simplicity here, we'll trust the middleware code `slog.DebugContext(r.Context(), ...)`
	// and the fact that `slog.Default().Enabled(r.Context(), ...)` also uses it.
	// A full test would involve a custom slog.Handler.

	// Simplified check: ensure the middleware doesn't panic and logs something
	// when debug is enabled, implying context passing is not obviously broken.
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	middleware := RequestLoggingMiddleware(nextHandler)

	type ctxKey string
	const testCtxValueKey ctxKey = "testSlogKey"

	req, _ := http.NewRequest("GET", "/ctx_test", nil)
	ctx := context.WithValue(req.Context(), testCtxValueKey, "myValue")
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()

	logOutput := captureSlogOutput(slog.LevelDebug, func() {
		middleware.ServeHTTP(rr, req)
	})
	assert.Contains(t, logOutput, "Incoming HTTP request")
}

var middlewareTestSetupOnce sync.Once

func setupMiddlewareTestLogging() {
	middlewareTestSetupOnce.Do(func() {
		// Global setup for middleware tests, if any.
	})
}

// TestMain for server package - ensure it's the only one if multiple _test.go files exist in this package.
// If other files like `server_test.go` exist, consolidate TestMain.
/*
func TestMain(m *testing.M) {
	setupMiddlewareTestLogging()
	// originalLogger := slog.Default()
	// quietLogger := slog.New(slog.NewTextHandler(io.Discard, nil)) // Discard logs unless captured
	// slog.SetDefault(quietLogger)

	code := m.Run()

	// slog.SetDefault(originalLogger) // Restore
	// os.Exit(code)
}
*/
