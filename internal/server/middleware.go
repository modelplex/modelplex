package server

import (
	"log/slog"
	"net/http"
)

// RequestLoggingMiddleware logs incoming HTTP request details if debug logging is enabled.
func RequestLoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if slog.Default().Enabled(r.Context(), slog.LevelDebug) {
			method := r.Method
			uri := r.RequestURI
			remoteAddr := r.RemoteAddr
			userAgent := r.UserAgent()

			slog.DebugContext(r.Context(), "Incoming HTTP request",
				"method", method,
				"uri", uri,
				"remote_addr", remoteAddr,
				"user_agent", userAgent,
			)
		}
		next.ServeHTTP(w, r)
	})
}
