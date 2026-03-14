package connections

import (
	"context"
	"sync"

	"github.com/google/uuid"
)

type memStore struct {
	mu      sync.Mutex
	waiting map[string]string // username -> connID
	peers   map[string]string // connID -> peerConnID (bidirectional)
}

// NewMemStore creates an in-memory Store implementation.
func NewMemStore() Store {
	return &memStore{
		waiting: make(map[string]string),
		peers:   make(map[string]string),
	}
}

func (s *memStore) SetWaiting(_ context.Context, username, connID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	evicted := s.waiting[username]
	s.waiting[username] = connID
	if evicted == connID {
		return "", nil
	}
	return evicted, nil
}

func (s *memStore) GetWaiting(_ context.Context, username string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.waiting[username]
	return id, ok
}

func (s *memStore) RemoveWaiting(_ context.Context, username, connID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.waiting[username] == connID {
		delete(s.waiting, username)
		return true
	}
	return false
}

func (s *memStore) CreatePair(_ context.Context, serverConnID, clientConnID, username string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.waiting, username)
	pairID := uuid.NewString()
	s.peers[serverConnID] = clientConnID
	s.peers[clientConnID] = serverConnID
	return pairID, nil
}

func (s *memStore) GetPeer(_ context.Context, connID string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	peer, ok := s.peers[connID]
	return peer, ok
}

func (s *memStore) RemovePair(_ context.Context, connID string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	peer, ok := s.peers[connID]
	if !ok {
		return "", false
	}
	delete(s.peers, connID)
	delete(s.peers, peer)
	return peer, true
}

func (s *memStore) IsPaired(_ context.Context, connID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.peers[connID]
	return ok
}

func (s *memStore) IsWaiting(_ context.Context, username string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.waiting[username]
	return ok
}

func (s *memStore) Stats(_ context.Context) (int, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.waiting), len(s.peers) / 2
}

func (s *memStore) GetWaitingUsernames(_ context.Context) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	names := make([]string, 0, len(s.waiting))
	for name := range s.waiting {
		names = append(names, name)
	}
	return names
}
