package server

import (
	"context"
	"crypto/subtle"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/matijazezelj/aib/internal/certs"
	"github.com/matijazezelj/aib/internal/graph"
	"github.com/matijazezelj/aib/internal/scanner"
	"github.com/matijazezelj/aib/internal/ui"
)

// Server is the AIB HTTP server providing the REST API and web UI.
type Server struct {
	store      *graph.SQLiteStore
	engine     graph.GraphEngine
	tracker    *certs.Tracker
	scanner    *scanner.Scanner
	logger     *slog.Logger
	listen     string
	readOnly   bool
	apiToken   string
	corsOrigin string
	version    string
	srv        *http.Server

	allowedPaths []string

	// rate limiter state
	limiters sync.Map // map[string]*ipLimiter
	done     chan struct{}

	shutdownOnce sync.Once
}

type ipLimiter struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// New creates a new Server.
func New(store *graph.SQLiteStore, engine graph.GraphEngine, tracker *certs.Tracker, sc *scanner.Scanner, logger *slog.Logger, listen string, readOnly bool, apiToken string, corsOrigin string, allowedPaths []string, version string) *Server {
	return &Server{
		store:        store,
		engine:       engine,
		tracker:      tracker,
		scanner:      sc,
		logger:       logger,
		listen:       listen,
		readOnly:     readOnly,
		apiToken:     apiToken,
		corsOrigin:   corsOrigin,
		allowedPaths: allowedPaths,
		version:      version,
	}
}

// securityHeaders adds standard security headers to all responses.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; script-src 'self' https://unpkg.com; style-src 'self' 'unsafe-inline' https://unpkg.com; connect-src 'self' https://cdn.simpleicons.org; img-src 'self' data:")
		next.ServeHTTP(w, r)
	})
}

// limitBody caps request body size to 1 MB on mutating methods.
func limitBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch {
			r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MB
		}
		next.ServeHTTP(w, r)
	})
}

// startLimiterCleanup runs background cleanup of stale rate-limiter entries.
// It stops when the done channel is closed.
func (s *Server) startLimiterCleanup() {
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-s.done:
				return
			case <-ticker.C:
				s.limiters.Range(func(key, value any) bool {
					il := value.(*ipLimiter)
					if time.Since(il.lastSeen) > 10*time.Minute {
						s.limiters.Delete(key)
					}
					return true
				})
			}
		}
	}()
}

// rateLimiter limits API requests to 10/sec burst 20 per client IP.
func (s *Server) rateLimiter(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}

		ip, _, _ := net.SplitHostPort(r.RemoteAddr)
		if ip == "" {
			ip = r.RemoteAddr
		}

		val, _ := s.limiters.LoadOrStore(ip, &ipLimiter{
			limiter:  rate.NewLimiter(10, 20),
			lastSeen: time.Now(),
		})
		il := val.(*ipLimiter)
		il.lastSeen = time.Now()

		if !il.limiter.Allow() {
			writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
			return
		}

		next.ServeHTTP(w, r)
	})
}

// corsMiddleware adds CORS headers when a cors_origin is configured.
func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.corsOrigin != "" && strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Access-Control-Allow-Origin", s.corsOrigin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			w.Header().Set("Access-Control-Max-Age", "86400")

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// authMiddleware returns a handler that checks for a valid bearer token
// on /api/ routes when an API token is configured.
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only protect API routes (not static UI or healthz)
		if s.apiToken != "" && strings.HasPrefix(r.URL.Path, "/api/") {
			auth := r.Header.Get("Authorization")
			token := strings.TrimPrefix(auth, "Bearer ")
			if token == auth || subtle.ConstantTimeCompare([]byte(token), []byte(s.apiToken)) != 1 {
				writeError(w, http.StatusUnauthorized, "unauthorized")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// Start starts the HTTP server.
func (s *Server) Start() error {
	s.done = make(chan struct{})

	mux := http.NewServeMux()
	RegisterRoutes(mux, s)

	// Serve embedded static UI files
	mux.Handle("/", http.FileServer(http.FS(ui.StaticFiles())))

	// Middleware chain: security headers → body limit → CORS → rate limit → auth → mux
	var handler http.Handler = mux
	handler = s.authMiddleware(handler)
	handler = s.rateLimiter(handler)
	handler = s.corsMiddleware(handler)
	handler = limitBody(handler)
	handler = securityHeaders(handler)

	s.startLimiterCleanup()

	s.srv = &http.Server{
		Addr:         s.listen,
		Handler:      handler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	s.logger.Info("starting server", "listen", s.listen)
	if s.apiToken != "" {
		s.logger.Info("API authentication enabled")
	} else {
		s.logger.Warn("API authentication disabled (set server.api_token to enable)")
	}
	if s.readOnly {
		s.logger.Info("server running in read-only mode (scan triggers disabled)")
	}
	s.logger.Info("AIB server running", "url", "http://localhost"+s.listen)

	return s.srv.ListenAndServe()
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown(ctx context.Context) error {
	if s.srv == nil {
		return nil
	}

	var err error
	s.shutdownOnce.Do(func() {
		if s.done != nil {
			close(s.done)
		}
		err = s.srv.Shutdown(ctx)
	})
	return err
}
