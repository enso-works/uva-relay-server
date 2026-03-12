package connections

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/anthropics/uvame-relay/internal/protocol"
	"github.com/anthropics/uvame-relay/internal/registry"
	"github.com/coder/websocket"
)

type Conn struct {
	WS       *websocket.Conn
	Username string
	IsServer bool
}

type pair struct {
	server *Conn
	client *Conn
}

type RegistrationResult string

const (
	Registered      RegistrationResult = "registered"
	UsernameTaken   RegistrationResult = "username_taken"
	InvalidUsername RegistrationResult = "invalid_username"
)

type ConnectionResult struct {
	Status   string
	ServerWS *Conn
}

type Manager struct {
	mu       sync.Mutex
	waiting  map[string]*Conn // username -> waiting server conn
	pairs    map[*Conn]*pair  // conn -> pair (both server and client entries)
	registry *registry.Registry
}

const relayWriteTimeout = 10 * time.Second

func NewManager(reg *registry.Registry) *Manager {
	return &Manager{
		waiting:  make(map[string]*Conn),
		pairs:    make(map[*Conn]*pair),
		registry: reg,
	}
}

func (m *Manager) RegisterServer(conn *Conn, username, installID string) RegistrationResult {
	if !protocol.IsValidUsername(username) {
		return InvalidUsername
	}

	if !m.registry.CanRegister(username, installID) {
		return UsernameTaken
	}

	if !m.registry.Register(username, installID) {
		return UsernameTaken
	}

	m.mu.Lock()
	existing := m.waiting[username]
	m.waiting[username] = conn
	m.mu.Unlock()

	if existing != nil {
		// Use Close in a goroutine to avoid blocking the registering connection.
		// Close sends a close frame and waits for response; we fire and forget.
		go func() { _ = existing.WS.Close(websocket.StatusNormalClosure, "Replaced by new connection") }()
	}

	return Registered
}

func (m *Manager) ConnectClient(conn *Conn, username string) ConnectionResult {
	if !m.registry.IsRegistered(username) {
		return ConnectionResult{Status: "unknown_username"}
	}

	m.mu.Lock()
	serverConn, ok := m.waiting[username]
	if !ok {
		m.mu.Unlock()
		return ConnectionResult{Status: "server_offline"}
	}

	delete(m.waiting, username)

	p := &pair{server: serverConn, client: conn}
	m.pairs[serverConn] = p
	m.pairs[conn] = p
	m.mu.Unlock()

	m.registry.UpdateLastSeen(username)

	return ConnectionResult{Status: "connected", ServerWS: serverConn}
}

func (m *Manager) Forward(sender *Conn, msgType websocket.MessageType, data []byte) {
	m.mu.Lock()
	p := m.pairs[sender]
	if p == nil {
		m.mu.Unlock()
		return
	}
	var target *Conn
	if p.server == sender {
		target = p.client
	} else {
		target = p.server
	}
	m.mu.Unlock()

	// Bound relay writes so a slow peer cannot stall a goroutine indefinitely.
	writeCtx, cancel := context.WithTimeout(context.Background(), relayWriteTimeout)
	defer cancel()
	if err := target.WS.Write(writeCtx, msgType, data); err != nil {
		direction := "server→client"
		if p.server == sender {
			direction = "server→client"
		} else {
			direction = "client→server"
		}
		slog.Warn("relay forward write failed", "direction", direction, "username", sender.Username, "msgType", msgType, "len", len(data), "err", err)
	}
}

func (m *Manager) HandleClose(conn *Conn) bool {
	m.mu.Lock()

	// Check if this was a waiting connection
	if conn.Username != "" {
		if w := m.waiting[conn.Username]; w == conn {
			delete(m.waiting, conn.Username)
			m.mu.Unlock()
			return false
		}
	}

	// Check if this was part of a pair
	p := m.pairs[conn]
	if p == nil {
		m.mu.Unlock()
		return false
	}

	delete(m.pairs, p.server)
	delete(m.pairs, p.client)

	var other *Conn
	if p.server == conn {
		other = p.client
	} else {
		other = p.server
	}
	m.mu.Unlock()

	closingSide := "client"
	if p.server == conn {
		closingSide = "server"
	}
	slog.Info("pair broken", "closingSide", closingSide, "username", conn.Username)
	go func() { _ = other.WS.Close(websocket.StatusNormalClosure, "Peer disconnected") }()
	return true
}

func (m *Manager) IsPaired(conn *Conn) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.pairs[conn] != nil
}

func (m *Manager) IsWaiting(username string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.waiting[username]
	return ok
}

func (m *Manager) GetWaitingCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.waiting)
}

func (m *Manager) GetPairCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Each pair has 2 entries in the map
	return len(m.pairs) / 2
}

func (m *Manager) GetWaitingUsernames() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	names := make([]string, 0, len(m.waiting))
	for name := range m.waiting {
		names = append(names, name)
	}
	return names
}

func (m *Manager) CloseAll(code websocket.StatusCode, reason string) {
	m.mu.Lock()
	conns := make([]*Conn, 0)
	for _, c := range m.waiting {
		conns = append(conns, c)
	}
	for c := range m.pairs {
		conns = append(conns, c)
	}
	m.waiting = make(map[string]*Conn)
	m.pairs = make(map[*Conn]*pair)
	m.mu.Unlock()

	for _, c := range conns {
		_ = c.WS.Close(code, reason)
	}
}
