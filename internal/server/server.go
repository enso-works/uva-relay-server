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
	Port         int
	DataDir      string
	InMemoryDB   bool
	PingInterval time.Duration
	PongTimeout  time.Duration
	ReclaimDays  int
	LogLevel     string
}

func New(opts Options) (*Server, error) {
	level := parseLogLevel(opts.LogLevel)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

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
		PingInterval: opts.PingInterval,
		PongTimeout:  opts.PongTimeout,
	}
	if cfg.PingInterval == 0 {
		cfg.PingInterval = 60 * time.Second
	}
	if cfg.PongTimeout == 0 {
		cfg.PongTimeout = 30 * time.Second
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
			w.Header().Set("Access-Control-Allow-Origin", "*")
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
	mux.HandleFunc("GET /status", s.handleStatus)
	mux.HandleFunc("GET /online/{username}", s.handleOnline)
	mux.HandleFunc("/ws", s.handleWS)

	port := opts.Port
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return nil, fmt.Errorf("listen: %w", err)
	}

	s.listener = ln
	s.httpServer = &http.Server{
		Handler: corsHandler(mux),
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
	return s.httpServer.Shutdown(ctx)
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
		OriginPatterns: []string{"*"},
	})
	if err != nil {
		s.Logger.Debug("websocket accept error", "err", err)
		return
	}

	handlerCfg := handler.Config{
		PingInterval: s.cfg.PingInterval,
		PongTimeout:  s.cfg.PongTimeout,
	}

	handler.ServeWS(r.Context(), ws, s.Manager, handlerCfg, s.Logger)
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
