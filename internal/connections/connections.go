package connections

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/anthropics/uvame-relay/internal/protocol"
	"github.com/anthropics/uvame-relay/internal/ratelimit"
	"github.com/anthropics/uvame-relay/internal/registry"
	"github.com/coder/websocket"
	"github.com/google/uuid"
)

// Multi-client binary header markers (also encode original message type).
const (
	MCHeaderText   byte = 0x01 // wrapped text frame
	MCHeaderBinary byte = 0x02 // wrapped binary frame
	MCHeaderSize        = 1 + 16 // marker + 16-byte UUID
)

const maxPendingWrites int64 = 100

// Conn represents a WebSocket connection with metadata.
type Conn struct {
	ID            string
	WS            *websocket.Conn
	Username      string
	IsServer      bool
	MultiClient   bool   // server supports multiple clients
	ClientID      string // client's unique ID in multi-client mode
	PendingWrites atomic.Int64
}

// NewConn creates a Conn with a generated unique ID.
func NewConn(ws *websocket.Conn) *Conn {
	return &Conn{
		ID: uuid.NewString(),
		WS: ws,
	}
}

type RegistrationResult string

const (
	Registered     RegistrationResult = "registered"
	UsernameTaken  RegistrationResult = "username_taken"
	InvalidUsername RegistrationResult = "invalid_username"
)

type ConnectionResult struct {
	Status   string
	ServerWS *Conn
	ClientID string // populated for multi-client connections
}

type multiClientGroup struct {
	serverConn *Conn
	clients    map[string]*Conn // clientID -> *Conn
}

// Manager coordinates WebSocket connections using a Store for routing state
// and a local map for WebSocket references.
type Manager struct {
	mu          sync.Mutex
	store       Store
	conns       map[string]*Conn                // connID -> *Conn (local WebSocket connections)
	multiGroups map[string]*multiClientGroup    // username -> group
	registry    *registry.Registry
	limiter     *ratelimit.Limiter
	buffer      *Buffer
}

// ManagerOption configures optional Manager behavior.
type ManagerOption func(*Manager)

// WithLimiter attaches a rate limiter for message forwarding.
func WithLimiter(l *ratelimit.Limiter) ManagerOption {
	return func(m *Manager) {
		m.limiter = l
	}
}

// WithBuffer attaches a message ring buffer for replay on reconnect.
func WithBuffer(b *Buffer) ManagerOption {
	return func(m *Manager) {
		m.buffer = b
	}
}

const relayWriteTimeout = 10 * time.Second

func NewManager(reg *registry.Registry, store Store, opts ...ManagerOption) *Manager {
	m := &Manager{
		store:       store,
		conns:       make(map[string]*Conn),
		multiGroups: make(map[string]*multiClientGroup),
		registry:    reg,
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
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

	ctx := context.Background()

	m.mu.Lock()
	m.conns[conn.ID] = conn
	evictedID, _ := m.store.SetWaiting(ctx, username, conn.ID)
	var evictedConn *Conn
	if evictedID != "" {
		evictedConn = m.conns[evictedID]
		delete(m.conns, evictedID)
	}
	m.mu.Unlock()

	if evictedConn != nil {
		go func() { _ = evictedConn.WS.Close(websocket.StatusNormalClosure, "Replaced by new connection") }()
	}

	return Registered
}

func (m *Manager) ConnectClient(conn *Conn, username, clientID string) ConnectionResult {
	if !m.registry.IsRegistered(username) {
		return ConnectionResult{Status: "unknown_username"}
	}

	ctx := context.Background()

	m.mu.Lock()
	serverConnID, ok := m.store.GetWaiting(ctx, username)
	if !ok {
		m.mu.Unlock()
		return ConnectionResult{Status: "server_offline"}
	}

	serverConn := m.conns[serverConnID]
	if serverConn == nil {
		m.store.RemoveWaiting(ctx, username, serverConnID)
		m.mu.Unlock()
		return ConnectionResult{Status: "server_offline"}
	}

	if serverConn.MultiClient {
		// Multi-client mode: server stays in waiting
		if clientID == "" {
			clientID = uuid.NewString()
		}
		conn.ClientID = clientID
		conn.Username = username

		group, exists := m.multiGroups[username]
		if !exists {
			group = &multiClientGroup{
				serverConn: serverConn,
				clients:    make(map[string]*Conn),
			}
			m.multiGroups[username] = group
		}
		group.clients[clientID] = conn
		m.conns[conn.ID] = conn
		m.mu.Unlock()

		// Notify server of new client
		m.sendControl(serverConn, protocol.NewRelayClientJoined(clientID))
		m.registry.UpdateLastSeen(username)

		return ConnectionResult{Status: "connected", ServerWS: serverConn, ClientID: clientID}
	}

	// Legacy single-client mode
	m.conns[conn.ID] = conn
	m.store.CreatePair(ctx, serverConnID, conn.ID, username)
	m.mu.Unlock()

	m.registry.UpdateLastSeen(username)

	return ConnectionResult{Status: "connected", ServerWS: serverConn}
}

func (m *Manager) Forward(sender *Conn, msgType websocket.MessageType, data []byte) {
	ctx := context.Background()

	if m.limiter != nil && !m.limiter.AllowMessage(ctx, sender.ID) {
		slog.Warn("message rate limit exceeded", "username", sender.Username, "connID", sender.ID)
		return
	}

	// Multi-client client -> server
	if sender.ClientID != "" {
		m.forwardFromClient(sender, msgType, data)
		return
	}

	// Multi-client server -> client(s)
	if sender.MultiClient && sender.Username != "" {
		m.forwardFromServer(sender, msgType, data)
		return
	}

	// Legacy single-pair forwarding
	m.mu.Lock()
	peerID, ok := m.store.GetPeer(ctx, sender.ID)
	if !ok {
		m.mu.Unlock()
		return
	}
	target := m.conns[peerID]
	m.mu.Unlock()

	if target == nil {
		return
	}

	m.writeToConn(target, sender, msgType, data)

	// Buffer the message for potential replay on reconnect
	direction := "c2s"
	if sender.IsServer {
		direction = "s2c"
	}
	m.buffer.Push(ctx, sender.Username, direction, int(msgType), data)
}

// forwardFromClient wraps a client message with a clientID header and sends to server.
func (m *Manager) forwardFromClient(client *Conn, msgType websocket.MessageType, data []byte) {
	m.mu.Lock()
	group := m.multiGroups[client.Username]
	if group == nil {
		m.mu.Unlock()
		return
	}
	server := group.serverConn
	m.mu.Unlock()

	wrapped := WrapMultiClientFrame(client.ClientID, msgType, data)
	m.writeToConn(server, client, websocket.MessageBinary, wrapped)
}

// forwardFromServer parses multi-client header for targeted sends, or broadcasts.
func (m *Manager) forwardFromServer(server *Conn, msgType websocket.MessageType, data []byte) {
	// Check for multi-client header (targeted send)
	if msgType == websocket.MessageBinary {
		if clientID, origType, payload, ok := ParseMultiClientHeader(data); ok {
			m.mu.Lock()
			group := m.multiGroups[server.Username]
			if group == nil {
				m.mu.Unlock()
				return
			}
			client := group.clients[clientID]
			m.mu.Unlock()

			if client != nil {
				m.writeToConn(client, server, origType, payload)
			}
			return
		}
	}

	// No header - broadcast to all clients
	m.mu.Lock()
	group := m.multiGroups[server.Username]
	if group == nil {
		m.mu.Unlock()
		return
	}
	clients := make([]*Conn, 0, len(group.clients))
	for _, c := range group.clients {
		clients = append(clients, c)
	}
	m.mu.Unlock()

	for _, client := range clients {
		m.writeToConn(client, server, msgType, data)
	}
}

// writeToConn writes a message to a target with backpressure checking.
func (m *Manager) writeToConn(target, sender *Conn, msgType websocket.MessageType, data []byte) {
	pending := target.PendingWrites.Add(1)
	defer target.PendingWrites.Add(-1)

	if pending > maxPendingWrites {
		go m.sendControl(sender, protocol.NewRelayBackpressure("slow_peer"))
		slog.Warn("backpressure: dropping message", "target", target.ID, "pending", pending)
		return
	}

	writeCtx, cancel := context.WithTimeout(context.Background(), relayWriteTimeout)
	defer cancel()
	if err := target.WS.Write(writeCtx, msgType, data); err != nil {
		direction := "client->server"
		if sender.IsServer {
			direction = "server->client"
		}
		slog.Warn("relay forward write failed", "direction", direction, "username", sender.Username, "msgType", msgType, "len", len(data), "err", err)
	}
}

func (m *Manager) HandleClose(conn *Conn) bool {
	ctx := context.Background()

	m.mu.Lock()

	// Multi-client client disconnect
	if conn.ClientID != "" {
		group := m.multiGroups[conn.Username]
		if group != nil {
			delete(group.clients, conn.ClientID)
			delete(m.conns, conn.ID)
			serverConn := group.serverConn

			if len(group.clients) == 0 {
				delete(m.multiGroups, conn.Username)
			}
			m.mu.Unlock()

			m.sendControl(serverConn, protocol.NewRelayClientLeft(conn.ClientID))
			slog.Info("multi-client: client left", "clientID", conn.ClientID, "username", conn.Username)
			return false
		}
	}

	// Multi-client server disconnect
	if conn.MultiClient && conn.Username != "" {
		group := m.multiGroups[conn.Username]
		if group != nil {
			clients := make([]*Conn, 0, len(group.clients))
			for _, c := range group.clients {
				clients = append(clients, c)
				delete(m.conns, c.ID)
			}
			delete(m.multiGroups, conn.Username)
			m.store.RemoveWaiting(ctx, conn.Username, conn.ID)
			delete(m.conns, conn.ID)
			m.mu.Unlock()

			for _, c := range clients {
				go func(cl *Conn) { _ = cl.WS.Close(websocket.StatusNormalClosure, "Server disconnected") }(c)
			}
			slog.Info("multi-client: server disconnected", "username", conn.Username, "clientCount", len(clients))
			return true
		}
	}

	// Check if this was a waiting connection
	if conn.Username != "" {
		if m.store.RemoveWaiting(ctx, conn.Username, conn.ID) {
			delete(m.conns, conn.ID)
			m.mu.Unlock()
			return false
		}
	}

	// Legacy single-pair close
	peerID, wasPaired := m.store.RemovePair(ctx, conn.ID)
	delete(m.conns, conn.ID)

	var peerConn *Conn
	if wasPaired {
		peerConn = m.conns[peerID]
		delete(m.conns, peerID)
	}
	m.mu.Unlock()

	if !wasPaired {
		return false
	}

	closingSide := "client"
	if conn.IsServer {
		closingSide = "server"
	}
	slog.Info("pair broken", "closingSide", closingSide, "username", conn.Username)

	if peerConn != nil {
		go func() { _ = peerConn.WS.Close(websocket.StatusNormalClosure, "Peer disconnected") }()
	}
	return true
}

func (m *Manager) IsPaired(conn *Conn) bool {
	ctx := context.Background()
	m.mu.Lock()
	defer m.mu.Unlock()

	// Multi-client server: paired if it has any clients
	if conn.MultiClient && conn.Username != "" {
		if group := m.multiGroups[conn.Username]; group != nil && len(group.clients) > 0 {
			return true
		}
	}

	// Multi-client client: check if in a group
	if conn.ClientID != "" {
		if group := m.multiGroups[conn.Username]; group != nil {
			if _, ok := group.clients[conn.ClientID]; ok {
				return true
			}
		}
	}

	return m.store.IsPaired(ctx, conn.ID)
}

func (m *Manager) IsWaiting(username string) bool {
	return m.store.IsWaiting(context.Background(), username)
}

func (m *Manager) GetWaitingCount() int {
	w, _ := m.store.Stats(context.Background())
	return w
}

func (m *Manager) GetPairCount() int {
	m.mu.Lock()
	multiPairs := 0
	for _, g := range m.multiGroups {
		multiPairs += len(g.clients)
	}
	m.mu.Unlock()

	_, storePairs := m.store.Stats(context.Background())
	return storePairs + multiPairs
}

func (m *Manager) GetWaitingUsernames() []string {
	return m.store.GetWaitingUsernames(context.Background())
}

// GetBufferedMessages returns buffered frames for a username, if any.
func (m *Manager) GetBufferedMessages(username string) []protocol.BufferedFrame {
	if m.buffer == nil {
		return nil
	}
	frames, err := m.buffer.GetFrames(context.Background(), username)
	if err != nil {
		slog.Warn("failed to get buffered messages", "username", username, "err", err)
		return nil
	}
	return frames
}

// ClearBuffer removes all buffered messages for a username.
func (m *Manager) ClearBuffer(username string) {
	if m.buffer == nil {
		return
	}
	m.buffer.Clear(context.Background(), username)
}

func (m *Manager) CloseAll(code websocket.StatusCode, reason string) {
	m.mu.Lock()
	conns := make([]*Conn, 0, len(m.conns))
	for _, c := range m.conns {
		conns = append(conns, c)
	}
	m.conns = make(map[string]*Conn)
	m.multiGroups = make(map[string]*multiClientGroup)
	m.mu.Unlock()

	for _, c := range conns {
		_ = c.WS.Close(code, reason)
	}
}

// sendControl sends a JSON control message to a connection.
func (m *Manager) sendControl(conn *Conn, msg any) {
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	writeCtx, cancel := context.WithTimeout(context.Background(), relayWriteTimeout)
	defer cancel()
	_ = conn.WS.Write(writeCtx, websocket.MessageText, data)
}

// WrapMultiClientFrame wraps data with a clientID header for multi-client forwarding.
func WrapMultiClientFrame(clientID string, msgType websocket.MessageType, data []byte) []byte {
	clientUUID, _ := uuid.Parse(clientID)
	wrapped := make([]byte, MCHeaderSize+len(data))
	if msgType == websocket.MessageText {
		wrapped[0] = MCHeaderText
	} else {
		wrapped[0] = MCHeaderBinary
	}
	copy(wrapped[1:17], clientUUID[:])
	copy(wrapped[17:], data)
	return wrapped
}

// ParseMultiClientHeader parses a multi-client header from a binary frame.
// Returns the clientID, original message type, payload, and whether parsing succeeded.
func ParseMultiClientHeader(data []byte) (clientID string, origMsgType websocket.MessageType, payload []byte, ok bool) {
	if len(data) < MCHeaderSize {
		return "", 0, nil, false
	}
	marker := data[0]
	if marker != MCHeaderText && marker != MCHeaderBinary {
		return "", 0, nil, false
	}
	var uid uuid.UUID
	copy(uid[:], data[1:17])
	clientID = uid.String()
	if marker == MCHeaderText {
		origMsgType = websocket.MessageText
	} else {
		origMsgType = websocket.MessageBinary
	}
	payload = data[17:]
	return clientID, origMsgType, payload, true
}
