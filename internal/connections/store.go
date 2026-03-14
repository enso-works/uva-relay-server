package connections

import "context"

// Store manages the routing state for relay connections.
// It tracks which connections are waiting and which are paired,
// but does not hold actual WebSocket connections.
type Store interface {
	// SetWaiting registers a connID as waiting for the given username.
	// If a previous connID was waiting, it is returned as evicted.
	SetWaiting(ctx context.Context, username, connID string) (evictedConnID string, err error)

	// GetWaiting returns the connID waiting for the given username.
	GetWaiting(ctx context.Context, username string) (connID string, ok bool)

	// RemoveWaiting removes the waiting entry for the username,
	// but only if the current waiting connID matches.
	RemoveWaiting(ctx context.Context, username, connID string) bool

	// CreatePair pairs server and client connections and removes the server from waiting.
	CreatePair(ctx context.Context, serverConnID, clientConnID, username string) (pairID string, err error)

	// GetPeer returns the paired peer's connID.
	GetPeer(ctx context.Context, connID string) (peerConnID string, ok bool)

	// RemovePair removes a pair and returns the peer's connID.
	RemovePair(ctx context.Context, connID string) (peerConnID string, ok bool)

	// IsPaired checks if a connID is currently paired.
	IsPaired(ctx context.Context, connID string) bool

	// IsWaiting checks if a username has a waiting connection.
	IsWaiting(ctx context.Context, username string) bool

	// Stats returns counts of waiting connections and pairs.
	Stats(ctx context.Context) (waiting, pairs int)

	// GetWaitingUsernames returns all usernames with waiting connections.
	GetWaitingUsernames(ctx context.Context) []string
}
