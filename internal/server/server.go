package server

import (
	"context"
	"crypto/subtle"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/matijazezelj/aib/internal/certs"
	"github.com/matijazezelj/aib/internal/graph"
	"github.com/matijazezelj/aib/internal/ui"
)

// Server is the AIB HTTP server providing the REST API and web UI.
type Server struct {
	store    *graph.SQLiteStore
	engine   graph.GraphEngine
	tracker  *certs.Tracker
	logger   *slog.Logger
	listen   string
	readOnly bool
	apiToken string
	srv      *http.Server
}

// New creates a new Server.
func New(store *graph.SQLiteStore, engine graph.GraphEngine, tracker *certs.Tracker, logger *slog.Logger, listen string, readOnly bool, apiToken string) *Server {
	return &Server{
		store:    store,
		engine:   engine,
		tracker:  tracker,
		logger:   logger,
		listen:   listen,
		readOnly: readOnly,
		apiToken: apiToken,
	}
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
	mux := http.NewServeMux()
	RegisterRoutes(mux, s)

	// Serve embedded static UI files
	mux.Handle("/", http.FileServer(http.FS(ui.StaticFiles())))

	var handler http.Handler = mux
	handler = s.authMiddleware(handler)

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
	fmt.Printf("AIB server running at http://localhost%s\n", s.listen)

	return s.srv.ListenAndServe()
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.srv.Shutdown(ctx)
}
