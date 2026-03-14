package connections

import (
	"context"
	"testing"
)

func TestMemStore(t *testing.T) {
	runStoreTests(t, func() Store { return NewMemStore() })
}

func runStoreTests(t *testing.T, newStore func() Store) {
	t.Helper()
	ctx := context.Background()

	t.Run("SetWaiting and GetWaiting", func(t *testing.T) {
		s := newStore()
		evicted, err := s.SetWaiting(ctx, "alice", "conn-1")
		if err != nil {
			t.Fatal(err)
		}
		if evicted != "" {
			t.Errorf("expected no eviction, got %s", evicted)
		}

		id, ok := s.GetWaiting(ctx, "alice")
		if !ok {
			t.Fatal("expected alice to be waiting")
		}
		if id != "conn-1" {
			t.Errorf("expected conn-1, got %s", id)
		}
	})

	t.Run("SetWaiting evicts previous", func(t *testing.T) {
		s := newStore()
		s.SetWaiting(ctx, "alice", "conn-1")

		evicted, err := s.SetWaiting(ctx, "alice", "conn-2")
		if err != nil {
			t.Fatal(err)
		}
		if evicted != "conn-1" {
			t.Errorf("expected conn-1 evicted, got %q", evicted)
		}

		id, ok := s.GetWaiting(ctx, "alice")
		if !ok || id != "conn-2" {
			t.Errorf("expected conn-2 waiting, got %s", id)
		}
	})

	t.Run("SetWaiting same connID no eviction", func(t *testing.T) {
		s := newStore()
		s.SetWaiting(ctx, "alice", "conn-1")

		evicted, _ := s.SetWaiting(ctx, "alice", "conn-1")
		if evicted != "" {
			t.Errorf("expected no eviction for same connID, got %s", evicted)
		}
	})

	t.Run("RemoveWaiting matching connID", func(t *testing.T) {
		s := newStore()
		s.SetWaiting(ctx, "alice", "conn-1")

		if !s.RemoveWaiting(ctx, "alice", "conn-1") {
			t.Error("expected RemoveWaiting to succeed")
		}
		if s.IsWaiting(ctx, "alice") {
			t.Error("expected alice to not be waiting")
		}
	})

	t.Run("RemoveWaiting mismatched connID", func(t *testing.T) {
		s := newStore()
		s.SetWaiting(ctx, "alice", "conn-1")

		if s.RemoveWaiting(ctx, "alice", "conn-2") {
			t.Error("expected RemoveWaiting to fail for wrong connID")
		}
		if !s.IsWaiting(ctx, "alice") {
			t.Error("expected alice to still be waiting")
		}
	})

	t.Run("CreatePair and GetPeer", func(t *testing.T) {
		s := newStore()
		s.SetWaiting(ctx, "alice", "server-1")

		pairID, err := s.CreatePair(ctx, "server-1", "client-1", "alice")
		if err != nil {
			t.Fatal(err)
		}
		if pairID == "" {
			t.Error("expected non-empty pairID")
		}

		// Server should no longer be waiting
		if s.IsWaiting(ctx, "alice") {
			t.Error("expected alice to not be waiting after pairing")
		}

		// Both sides should see peer
		peer, ok := s.GetPeer(ctx, "server-1")
		if !ok || peer != "client-1" {
			t.Errorf("expected client-1 as peer, got %s", peer)
		}

		peer, ok = s.GetPeer(ctx, "client-1")
		if !ok || peer != "server-1" {
			t.Errorf("expected server-1 as peer, got %s", peer)
		}
	})

	t.Run("IsPaired", func(t *testing.T) {
		s := newStore()
		s.SetWaiting(ctx, "alice", "server-1")

		if s.IsPaired(ctx, "server-1") {
			t.Error("expected not paired before CreatePair")
		}

		s.CreatePair(ctx, "server-1", "client-1", "alice")

		if !s.IsPaired(ctx, "server-1") {
			t.Error("expected server-1 to be paired")
		}
		if !s.IsPaired(ctx, "client-1") {
			t.Error("expected client-1 to be paired")
		}
	})

	t.Run("RemovePair", func(t *testing.T) {
		s := newStore()
		s.SetWaiting(ctx, "alice", "server-1")
		s.CreatePair(ctx, "server-1", "client-1", "alice")

		peer, ok := s.RemovePair(ctx, "server-1")
		if !ok {
			t.Fatal("expected RemovePair to succeed")
		}
		if peer != "client-1" {
			t.Errorf("expected client-1, got %s", peer)
		}

		if s.IsPaired(ctx, "server-1") || s.IsPaired(ctx, "client-1") {
			t.Error("expected neither side to be paired after RemovePair")
		}
	})

	t.Run("RemovePair nonexistent", func(t *testing.T) {
		s := newStore()
		_, ok := s.RemovePair(ctx, "nonexistent")
		if ok {
			t.Error("expected RemovePair to fail for nonexistent")
		}
	})

	t.Run("Stats", func(t *testing.T) {
		s := newStore()
		s.SetWaiting(ctx, "alice", "server-1")
		s.SetWaiting(ctx, "bob", "server-2")

		w, p := s.Stats(ctx)
		if w != 2 {
			t.Errorf("expected 2 waiting, got %d", w)
		}
		if p != 0 {
			t.Errorf("expected 0 pairs, got %d", p)
		}

		s.CreatePair(ctx, "server-1", "client-1", "alice")

		w, p = s.Stats(ctx)
		if w != 1 {
			t.Errorf("expected 1 waiting, got %d", w)
		}
		if p != 1 {
			t.Errorf("expected 1 pair, got %d", p)
		}
	})

	t.Run("GetWaitingUsernames", func(t *testing.T) {
		s := newStore()
		s.SetWaiting(ctx, "alice", "server-1")
		s.SetWaiting(ctx, "bob", "server-2")

		names := s.GetWaitingUsernames(ctx)
		if len(names) != 2 {
			t.Errorf("expected 2 usernames, got %d", len(names))
		}

		nameMap := make(map[string]bool)
		for _, n := range names {
			nameMap[n] = true
		}
		if !nameMap["alice"] || !nameMap["bob"] {
			t.Error("expected alice and bob")
		}
	})
}
