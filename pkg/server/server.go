package server

import (
	"embed"
	"html/template"
	"net/http"

	"github.com/maks112v/minicast/pkg/audio"
	ws "github.com/maks112v/minicast/pkg/websocket"
	"go.uber.org/zap"
)

//go:embed templates/*
var templates embed.FS

// Server represents the HTTP server
type Server struct {
	wsManager *ws.Manager
	logger    *zap.SugaredLogger
	audio     *audio.Processor
}

// New creates a new server instance
func New(logger *zap.SugaredLogger) *Server {
	return &Server{
		wsManager: ws.NewManager(logger),
		logger:    logger,
		audio:     audio.NewProcessor(44100, 2, 16), // CD quality audio
	}
}

// Start starts the HTTP server
func (s *Server) Start(addr string) error {
	// Serve static files from the current directory
	fs := http.FileServer(http.Dir("."))
	http.Handle("/static/", http.StripPrefix("/static/", fs))

	// Root endpoint serves index.html
	http.HandleFunc("/", s.corsMiddleware(s.serveIndexPage))

	// WebSocket endpoint
	http.HandleFunc("/ws", s.corsMiddleware(s.handleWebSocket))

	// Serve the stream player page
	http.HandleFunc("/listen", s.corsMiddleware(s.serveStreamPage))

	s.logger.Info("Starting streaming server on http://localhost" + addr + "/")
	s.logger.Info("Stream player available at http://localhost" + addr + "/listen")
	return http.ListenAndServe(addr, nil)
}

// corsMiddleware handles CORS headers
func (s *Server) corsMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, PUT, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers",
			"Content-Type, Authorization, Accept, Origin, X-Requested-With")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	}
}

// handleWebSocket handles WebSocket connections
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Upgrade HTTP connection to WebSocket
	conn, err := s.wsManager.GetUpgrader().Upgrade(w, r, nil)
	if err != nil {
		s.logger.Errorf("Failed to upgrade connection: %v", err)
		return
	}

	// Check if this is a source connection
	isSource := r.URL.Query().Get("source") == "true"

	if isSource {
		s.wsManager.HandleSource(conn)
	} else {
		s.wsManager.HandleListener(conn)
	}
}

// serveIndexPage serves the index page
func (s *Server) serveIndexPage(w http.ResponseWriter, r *http.Request) {
	tmpl, err := template.ParseFS(templates, "templates/index.html")
	if err != nil {
		s.logger.Errorf("Failed to parse template: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	if err := tmpl.Execute(w, nil); err != nil {
		s.logger.Errorf("Failed to execute template: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
}

// serveStreamPage serves the stream player page
func (s *Server) serveStreamPage(w http.ResponseWriter, r *http.Request) {
	tmpl, err := template.ParseFS(templates, "templates/player.html")
	if err != nil {
		s.logger.Errorf("Failed to parse template: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	if err := tmpl.Execute(w, nil); err != nil {
		s.logger.Errorf("Failed to execute template: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
}
