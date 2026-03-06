package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/anthropics/uvame-relay/internal/config"
	"github.com/anthropics/uvame-relay/internal/connections"
	"github.com/anthropics/uvame-relay/internal/handler"
	"github.com/anthropics/uvame-relay/internal/registry"
	"github.com/anthropics/uvame-relay/internal/testutil"
	"github.com/coder/websocket"
)

type Server struct {
	httpServer *http.Server
	listener   net.Listener
	Manager    *connections.Manager
	Registry   *registry.Registry
	Logger     *slog.Logger
	startTime  time.Time
	cfg        *config.Config
}

type Options struct {
	Port             int
	DataDir          string
	InMemoryDB       bool
	PingInterval     time.Duration
	PongTimeout      time.Duration
	ReclaimDays      int
	LogLevel         string
	LogJSON          bool
	AdminToken       string
	ServerAuthToken  string
	AllowedOrigins   []string
	WSOriginPatterns []string
}

func New(opts Options) (*Server, error) {
	level := parseLogLevel(opts.LogLevel)
	handlerOpts := &slog.HandlerOptions{Level: level}
	var logHandler slog.Handler
	if opts.LogJSON {
		logHandler = slog.NewJSONHandler(os.Stderr, handlerOpts)
	} else {
		logHandler = slog.NewTextHandler(os.Stderr, handlerOpts)
	}
	logger := slog.New(logHandler)
	if opts.AdminToken == "" {
		logger.Warn("admin token is not configured; operational endpoints remain public")
	}
	if opts.ServerAuthToken == "" {
		logger.Warn("server auth token is not configured; websocket server registration is open")
	}
	if len(opts.AllowedOrigins) == 0 || (len(opts.AllowedOrigins) == 1 && opts.AllowedOrigins[0] == "*") {
		logger.Warn("cors allowed origins are fully open")
	}
	if len(opts.WSOriginPatterns) == 0 || (len(opts.WSOriginPatterns) == 1 && opts.WSOriginPatterns[0] == "*") {
		logger.Warn("websocket origin patterns are fully open")
	}

	var reg *registry.Registry
	var err error
	if opts.InMemoryDB {
		reg = registry.NewFromDB(testutil.NewTestDB())
	} else {
		dataDir := opts.DataDir
		if dataDir == "" {
			home, _ := os.UserHomeDir()
			dataDir = home + "/.uvame-relay"
		}
		reg, err = registry.New(dataDir)
		if err != nil {
			return nil, fmt.Errorf("create registry: %w", err)
		}
	}

	reclaimDays := opts.ReclaimDays
	if reclaimDays == 0 {
		reclaimDays = 90
	}
	reclaimed := reg.ReclaimInactive(reclaimDays)
	if reclaimed > 0 {
		logger.Info("reclaimed inactive usernames", "count", reclaimed)
	}

	mgr := connections.NewManager(reg)

	cfg := &config.Config{
		PingInterval:     opts.PingInterval,
		PongTimeout:      opts.PongTimeout,
		AdminToken:       opts.AdminToken,
		ServerAuthToken:  opts.ServerAuthToken,
		WSOriginPatterns: append([]string(nil), opts.WSOriginPatterns...),
	}
	if cfg.PingInterval == 0 {
		cfg.PingInterval = 60 * time.Second
	}
	if cfg.PongTimeout == 0 {
		cfg.PongTimeout = 30 * time.Second
	}
	if len(cfg.WSOriginPatterns) == 0 {
		cfg.WSOriginPatterns = []string{"*"}
	}

	mux := http.NewServeMux()

	s := &Server{
		Manager:   mgr,
		Registry:  reg,
		Logger:    logger,
		startTime: time.Now(),
		cfg:       cfg,
	}

	corsHandler := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", allowedOrigin(r, opts.AllowedOrigins))
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusOK)
				return
			}
			next.ServeHTTP(w, r)
		})
	}

	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("GET /ready", s.handleReady)
	mux.HandleFunc("GET /status", s.withAdminAuth(s.handleStatus))
	mux.HandleFunc("GET /online/{username}", s.withAdminAuth(s.handleOnline))
	mux.HandleFunc("/ws", s.handleWS)

	port := opts.Port
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return nil, fmt.Errorf("listen: %w", err)
	}

	s.listener = ln
	s.httpServer = &http.Server{
		Handler:           corsHandler(mux),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	return s, nil
}

func (s *Server) Port() int {
	return s.listener.Addr().(*net.TCPAddr).Port
}

func (s *Server) Start() error {
	s.Logger.Info("relay server listening", "port", s.Port())
	return s.httpServer.Serve(s.listener)
}

func (s *Server) Shutdown(ctx context.Context) error {
	s.Manager.CloseAll(websocket.StatusGoingAway, "Server shutting down")
	if err := s.httpServer.Shutdown(ctx); err != nil {
		return err
	}
	return s.Registry.Close()
}

func (s *Server) Close() error {
	s.Manager.CloseAll(websocket.StatusGoingAway, "Server shutting down")
	_ = s.Registry.Close()
	return s.httpServer.Close()
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{
		"status":  "ok",
		"uptime":  time.Since(s.startTime).Seconds(),
		"waiting": s.Manager.GetWaitingCount(),
		"pairs":   s.Manager.GetPairCount(),
	})
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	if err := s.Registry.Ping(); err != nil {
		http.Error(w, "registry unavailable", http.StatusServiceUnavailable)
		return
	}

	writeJSON(w, map[string]any{
		"status": "ready",
	})
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	writeJSON(w, map[string]any{
		"status":     "ok",
		"uptime":     time.Since(s.startTime).Seconds(),
		"waiting":    s.Manager.GetWaitingCount(),
		"pairs":      s.Manager.GetPairCount(),
		"registered": s.Registry.Count(),
		"memory": map[string]any{
			"alloc":      mem.Alloc,
			"totalAlloc": mem.TotalAlloc,
			"sys":        mem.Sys,
			"numGC":      mem.NumGC,
		},
	})
}

func (s *Server) handleOnline(w http.ResponseWriter, r *http.Request) {
	username := r.PathValue("username")
	online := s.Manager.IsWaiting(username)
	writeJSON(w, map[string]any{"online": online})
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: s.cfg.WSOriginPatterns,
	})
	if err != nil {
		s.Logger.Debug("websocket accept error", "err", err)
		return
	}

	handlerCfg := handler.Config{
		PingInterval:    s.cfg.PingInterval,
		PongTimeout:     s.cfg.PongTimeout,
		ServerAuthToken: s.cfg.ServerAuthToken,
	}

	handler.ServeWS(r.Context(), ws, s.Manager, handlerCfg, s.Logger, bearerToken(r.Header.Get("Authorization")))
}

func (s *Server) withAdminAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.AdminToken == "" {
			next(w, r)
			return
		}

		token := r.Header.Get("X-Relay-Admin-Token")
		if token == "" {
			token = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		}
		if token != s.cfg.AdminToken {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		next(w, r)
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func parseLogLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug", "trace":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error", "fatal":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func allowedOrigin(r *http.Request, configured []string) string {
	if len(configured) == 0 {
		return "*"
	}
	if len(configured) == 1 && configured[0] == "*" {
		return "*"
	}

	requestOrigin := r.Header.Get("Origin")
	for _, origin := range configured {
		if origin == requestOrigin {
			return origin
		}
	}

	return configured[0]
}

func bearerToken(header string) string {
	if header == "" {
		return ""
	}
	return strings.TrimPrefix(header, "Bearer ")
}
