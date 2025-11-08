package apiserver

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/go-logr/logr"
	httpSwagger "github.com/swaggo/http-swagger"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kagent-dev/khook/internal/interfaces"
)

// Server represents the HTTP API server
type Server struct {
	port           string
	dedupManager   interfaces.DeduplicationManager
	k8sClient      client.Client
	statusManager  interfaces.StatusManager
	logger         logr.Logger
	httpServer     *http.Server
	eventHandlers  *EventHandlers
	hookHandlers   *HookHandlers
	healthHandlers *HealthHandlers
	statsHandlers  *StatsHandlers
}

// Config holds configuration for the API server
type Config struct {
	Port          string
	DedupManager  interfaces.DeduplicationManager
	K8sClient     client.Client
	StatusManager interfaces.StatusManager
}

// NewServer creates a new API server instance
func NewServer(config Config) *Server {
	logger := log.Log.WithName("apiserver")

	// Set default port if not specified
	port := config.Port
	if port == "" {
		port = "8082"
	}

	server := &Server{
		port:          port,
		dedupManager:  config.DedupManager,
		k8sClient:     config.K8sClient,
		statusManager: config.StatusManager,
		logger:        logger,
	}

	// Initialize handlers
	server.eventHandlers = NewEventHandlers(server)
	server.hookHandlers = NewHookHandlers(server)
	server.healthHandlers = NewHealthHandlers(server)
	server.statsHandlers = NewStatsHandlers(server)

	return server
}

// Start starts the HTTP API server
func (s *Server) Start(ctx context.Context) error {
	mux := http.NewServeMux()

	// Register routes
	s.registerRoutes(mux)

	// Create HTTP server with timeouts
	s.httpServer = &http.Server{
		Addr:         ":" + s.port,
		Handler:      s.middleware(mux),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	s.logger.Info("Starting API server", "port", s.port)

	// Start server in a goroutine
	errCh := make(chan error, 1)
	go func() {
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- fmt.Errorf("failed to start API server: %w", err)
		}
	}()

	// Wait for context cancellation or server error
	select {
	case <-ctx.Done():
		s.logger.Info("Shutting down API server")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := s.httpServer.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("error during server shutdown: %w", err)
		}
		return ctx.Err()
	case err := <-errCh:
		return err
	}
}

// GetMux returns a new ServeMux with all routes registered
func (s *Server) GetMux() *http.ServeMux {
	mux := http.NewServeMux()
	s.registerRoutes(mux)
	return mux
}

// registerRoutes registers all API routes
func (s *Server) registerRoutes(mux *http.ServeMux) {
	// API v1 routes
	apiV1 := "/api/v1"

	// Event endpoints
	mux.HandleFunc(apiV1+"/events", s.eventHandlers.ListEvents)
	mux.HandleFunc(apiV1+"/events/stream", s.eventHandlers.StreamEvents)

	// Hook endpoints
	mux.HandleFunc(apiV1+"/hooks", s.hookHandlers.ListHooks)

	// Statistics endpoints
	mux.HandleFunc(apiV1+"/stats/events/summary", s.statsHandlers.EventSummary)
	mux.HandleFunc(apiV1+"/stats/events/by-type", s.statsHandlers.EventsByType)

	// Health and diagnostics endpoints
	mux.HandleFunc(apiV1+"/health", s.healthHandlers.Health)
	mux.HandleFunc(apiV1+"/diagnostics", s.healthHandlers.Diagnostics)
	mux.HandleFunc(apiV1+"/metrics", s.healthHandlers.Metrics)

	// Swagger documentation endpoints
	s.registerSwaggerRoutes(mux)
}

// Middleware applies common middleware to all requests
func (s *Server) Middleware(next http.Handler) http.Handler {
	return s.middleware(next)
}

// middleware applies common middleware to all requests
func (s *Server) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// CORS headers
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		// Handle preflight requests
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		// Request logging
		s.logger.V(1).Info("API request",
			"method", r.Method,
			"path", r.URL.Path,
			"remoteAddr", r.RemoteAddr)

		// Call next handler
		next.ServeHTTP(w, r)

		// Log response
		s.logger.V(2).Info("API response",
			"method", r.Method,
			"path", r.URL.Path,
			"duration", time.Since(start))
	})
}

// writeJSON writes a JSON response
func (s *Server) writeJSON(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if err := jsonEncode(w, data); err != nil {
		s.logger.Error(err, "Failed to encode JSON response")
	}
}

// writeError writes an error response
func (s *Server) writeError(w http.ResponseWriter, statusCode int, message string) {
	s.writeJSON(w, statusCode, map[string]string{
		"error": message,
	})
}

// registerSwaggerRoutes registers Swagger UI routes
func (s *Server) registerSwaggerRoutes(mux *http.ServeMux) {
	swaggerJSONPath := "./docs/swagger/swagger.json"
	if _, err := os.Stat(swaggerJSONPath); err == nil {
		// Serve swagger.json directly
		mux.HandleFunc("/swagger/doc.json", func(w http.ResponseWriter, r *http.Request) {
			http.ServeFile(w, r, swaggerJSONPath)
		})

		// Use http-swagger to serve Swagger UI
		mux.HandleFunc("/swagger/", func(w http.ResponseWriter, r *http.Request) {
			// Build URL dynamically based on request
			scheme := "http"
			if r.TLS != nil {
				scheme = "https"
			}
			host := r.Host
			if host == "" {
				host = "localhost:" + s.port
			}
			docURL := scheme + "://" + host + "/swagger/doc.json"

			handler := httpSwagger.Handler(
				httpSwagger.URL(docURL),
				httpSwagger.DeepLinking(true),
				httpSwagger.DocExpansion("none"),
				httpSwagger.DomID("swagger-ui"),
			)
			handler.ServeHTTP(w, r)
		})
	} else {
		s.logger.Info("Swagger JSON not found, skipping Swagger UI", "path", swaggerJSONPath)
	}
}
