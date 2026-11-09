// Package server is the panel's HTTP side: the security headers every answer carries, request logging,
// and putting the API and the web app on one address.
package server

import (
	"bufio"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// contentSecurityPolicy only lets the panel load things from itself. Inline styles are allowed because
// Svelte sets a few (a meter's width); scripts never are.
const contentSecurityPolicy = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'; object-src 'none'"

// New puts the API under /api, the installer under /install.sh and /downloads, and the web app everywhere else.
func New(api, installer, app http.Handler, secure bool, log *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/api/", api)
	mux.Handle("/install.sh", installer)
	mux.Handle("/downloads/", installer)
	mux.Handle("/", app)
	return logRequests(log, securityHeaders(secure, mux))
}

func securityHeaders(secure bool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		if secure {
			h.Set("Strict-Transport-Security", "max-age=31536000")
		}
		next.ServeHTTP(w, r)
	})
}

// statusRecorder remembers the status written, for the log line.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// Unwrap lets http.ResponseController reach the real writer (for flushing).
func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// Hijack hands the connection over for a websocket (the agents' and the browser's live updates).
func (s *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	s.status = http.StatusSwitchingProtocols
	return http.NewResponseController(s.ResponseWriter).Hijack()
}

func logRequests(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		// Only the API is logged: the web app's files would drown it.
		if len(r.URL.Path) >= 5 && r.URL.Path[:5] == "/api/" {
			log.Info("request", "method", r.Method, "path", r.URL.Path, "status", rec.status, "took", time.Since(start).Round(time.Millisecond))
		}
	})
}
