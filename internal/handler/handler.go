package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/anthropics/uvame-relay/internal/connections"
	"github.com/anthropics/uvame-relay/internal/protocol"
	"github.com/coder/websocket"
)

type Config struct {
	PingInterval    time.Duration
	PongTimeout     time.Duration
	ServerAuthToken string
}

const writeTimeout = 10 * time.Second

// ServeWS handles a single WebSocket connection's lifecycle.
// Each connection gets its own goroutine running this function.
func ServeWS(ctx context.Context, ws *websocket.Conn, mgr *connections.Manager, cfg Config, logger *slog.Logger, bearerToken string) {
	conn := &connections.Conn{WS: ws}
	var pingStop context.CancelFunc

	defer func() {
		if pingStop != nil {
			pingStop()
		}
		mgr.HandleClose(conn)
		_ = ws.CloseNow()
	}()

	for {
		msgType, data, err := ws.Read(ctx)
		if err != nil {
			logger.Info("connection read error", "username", conn.Username, "isServer", conn.IsServer, "err", err)
			return
		}

		// Check pairing dynamically -- the server side gets paired by the
		// client's goroutine calling ConnectClient, so we check the manager
		// rather than a local boolean.
		if mgr.IsPaired(conn) {
			mgr.Forward(conn, msgType, data)
			continue
		}

		// Pre-pairing: only accept text frames with JSON
		if msgType != websocket.MessageText {
			logger.Debug("binary message before pairing, ignoring")
			continue
		}

		msg := protocol.ParseMessage(data)
		if msg == nil {
			logger.Debug("unknown message type before pairing")
			continue
		}

		switch m := msg.(type) {
		case *protocol.ServerRegister:
			if cfg.ServerAuthToken != "" && m.AuthToken != cfg.ServerAuthToken && bearerToken != cfg.ServerAuthToken {
				sendJSON(ctx, ws, protocol.NewServerRejected("unauthorized"))
				logger.Info("server rejected", "username", m.Username, "reason", "unauthorized")
				_ = ws.Close(websocket.StatusPolicyViolation, "Registration rejected: unauthorized")
				return
			}

			result := mgr.RegisterServer(conn, m.Username, m.InstallID)

			if result == connections.Registered {
				conn.Username = m.Username
				conn.IsServer = true
				sendJSON(ctx, ws, protocol.NewServerRegistered())
				logger.Info("server registered", "username", m.Username)

				var pingCtx context.Context
				pingCtx, pingStop = context.WithCancel(ctx)
				go pingLoop(pingCtx, ws, mgr, conn, cfg, logger)
			} else {
				sendJSON(ctx, ws, protocol.NewServerRejected(string(result)))
				logger.Info("server rejected", "username", m.Username, "reason", result)
				_ = ws.Close(websocket.StatusNormalClosure, "Registration rejected: "+string(result))
				return
			}

		case *protocol.ClientConnect:
			connResult := mgr.ConnectClient(conn, m.Username)

			if connResult.Status == "connected" {
				conn.Username = m.Username
				conn.IsServer = false
				sendJSON(ctx, ws, protocol.NewClientConnected())
				logger.Info("pair connected", "username", m.Username)
				// Stop ping on server side (paired connections don't need keepalive from relay)
				// The server side's ping goroutine will detect pairing and exit.
			} else {
				sendJSON(ctx, ws, protocol.NewClientError(connResult.Status))
				logger.Info("client connection failed", "username", m.Username, "reason", connResult.Status)
				_ = ws.Close(websocket.StatusNormalClosure, "Connection failed: "+connResult.Status)
				return
			}
		}
	}
}

func sendJSON(ctx context.Context, ws *websocket.Conn, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	writeCtx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	_ = ws.Write(writeCtx, websocket.MessageText, data)
}

func pingLoop(ctx context.Context, ws *websocket.Conn, mgr *connections.Manager, conn *connections.Conn, cfg Config, logger *slog.Logger) {
	ticker := time.NewTicker(cfg.PingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Stop pinging once paired
			if mgr.IsPaired(conn) {
				return
			}

			pingCtx, cancel := context.WithTimeout(ctx, cfg.PongTimeout)
			err := ws.Ping(pingCtx)
			cancel()
			if err != nil {
				logger.Debug("ping failed, closing connection", "username", conn.Username, "err", err)
				_ = ws.Close(websocket.StatusNormalClosure, "Pong timeout")
				return
			}
		}
	}
}
