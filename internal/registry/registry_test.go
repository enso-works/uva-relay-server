package registry

import (
	"testing"
	"time"

	"github.com/anthropics/uvame-relay/internal/testutil"
)

func newTestRegistry() *Registry {
	return NewFromDB(testutil.NewTestDB())
}

func TestCanRegister(t *testing.T) {
	r := newTestRegistry()

	t.Run("returns true for unregistered username", func(t *testing.T) {
		if !r.CanRegister("alice", "install-1") {
			t.Error("expected true for unregistered username")
		}
	})

	t.Run("returns true for same installId", func(t *testing.T) {
		r.Register("bob", "install-1")
		if !r.CanRegister("bob", "install-1") {
			t.Error("expected true for same installId")
		}
	})

	t.Run("returns false for different installId", func(t *testing.T) {
		r.Register("charlie", "install-1")
		if r.CanRegister("charlie", "install-2") {
			t.Error("expected false for different installId")
		}
	})

	t.Run("returns false for invalid username", func(t *testing.T) {
		if r.CanRegister("ab", "install-1") {
			t.Error("expected false for invalid username")
		}
	})
}

func TestRegister(t *testing.T) {
	t.Run("registers new username", func(t *testing.T) {
		r := newTestRegistry()
		if !r.Register("alice", "install-1") {
			t.Error("expected registration to succeed")
		}
		if !r.IsRegistered("alice") {
			t.Error("expected alice to be registered")
		}
	})

	t.Run("allows same installId to re-register", func(t *testing.T) {
		r := newTestRegistry()
		r.Register("alice", "install-1")
		if !r.Register("alice", "install-1") {
			t.Error("expected re-registration to succeed")
		}
	})

	t.Run("rejects different installId", func(t *testing.T) {
		r := newTestRegistry()
		r.Register("alice", "install-1")
		if r.Register("alice", "install-2") {
			t.Error("expected registration to fail for different installId")
		}
	})

	t.Run("rejects invalid username", func(t *testing.T) {
		r := newTestRegistry()
		if r.Register("ab", "install-1") {
			t.Error("expected registration to fail for invalid username")
		}
	})

	t.Run("updates last_seen_at on re-registration", func(t *testing.T) {
		r := newTestRegistry()
		r.Register("alice", "install-1")
		rec1 := r.Get("alice")

		time.Sleep(10 * time.Millisecond)
		r.Register("alice", "install-1")
		rec2 := r.Get("alice")

		if rec2.LastSeenAt == rec1.LastSeenAt {
			t.Error("expected last_seen_at to be updated")
		}
	})

	t.Run("stores install_id correctly", func(t *testing.T) {
		r := newTestRegistry()
		r.Register("alice", "my-install-id")
		rec := r.Get("alice")
		if rec.InstallID != "my-install-id" {
			t.Errorf("expected install_id my-install-id, got %s", rec.InstallID)
		}
	})
}

func TestIsRegistered(t *testing.T) {
	r := newTestRegistry()

	if r.IsRegistered("alice") {
		t.Error("expected false for unregistered")
	}

	r.Register("alice", "install-1")
	if !r.IsRegistered("alice") {
		t.Error("expected true for registered")
	}
}

func TestUpdateLastSeen(t *testing.T) {
	r := newTestRegistry()
	r.Register("alice", "install-1")
	rec1 := r.Get("alice")

	time.Sleep(10 * time.Millisecond)
	r.UpdateLastSeen("alice")
	rec2 := r.Get("alice")

	if rec2.LastSeenAt == rec1.LastSeenAt {
		t.Error("expected last_seen_at to be updated")
	}
}

func TestReclaimInactive(t *testing.T) {
	r := newTestRegistry()

	// Insert with old timestamp directly
	r.Register("old-user", "install-1")
	oldTime := time.Now().UTC().AddDate(0, 0, -100).Format(time.RFC3339Nano)
	_, _ = r.db.Exec("UPDATE usernames SET last_seen_at = ? WHERE username = ?", oldTime, "old-user")

	r.Register("new-user", "install-2")

	count := r.ReclaimInactive(90)
	if count != 1 {
		t.Errorf("expected 1 reclaimed, got %d", count)
	}

	if r.IsRegistered("old-user") {
		t.Error("expected old-user to be reclaimed")
	}
	if !r.IsRegistered("new-user") {
		t.Error("expected new-user to still be registered")
	}
}

func TestDelete(t *testing.T) {
	r := newTestRegistry()

	if r.Delete("nonexistent") {
		t.Error("expected false for non-existent")
	}

	r.Register("alice", "install-1")
	if !r.Delete("alice") {
		t.Error("expected true for deleting existing username")
	}
	if r.IsRegistered("alice") {
		t.Error("expected alice to be unregistered after delete")
	}
}

func TestList(t *testing.T) {
	r := newTestRegistry()

	list := r.List()
	if len(list) != 0 {
		t.Errorf("expected empty list, got %d", len(list))
	}

	r.Register("charlie", "install-3")
	r.Register("alice", "install-1")
	r.Register("bob", "install-2")

	list = r.List()
	if len(list) != 3 {
		t.Fatalf("expected 3 records, got %d", len(list))
	}
	if list[0].Username != "alice" || list[1].Username != "bob" || list[2].Username != "charlie" {
		t.Error("expected records sorted by username")
	}
}

func TestGet(t *testing.T) {
	r := newTestRegistry()

	if rec := r.Get("nonexistent"); rec != nil {
		t.Error("expected nil for non-existent")
	}

	r.Register("alice", "install-1")
	rec := r.Get("alice")
	if rec == nil {
		t.Fatal("expected non-nil record")
	}
	if rec.Username != "alice" {
		t.Errorf("expected username alice, got %s", rec.Username)
	}
	if rec.InstallID != "install-1" {
		t.Errorf("expected install_id install-1, got %s", rec.InstallID)
	}
	if rec.RegisteredAt == "" {
		t.Error("expected non-empty registered_at")
	}
	if rec.LastSeenAt == "" {
		t.Error("expected non-empty last_seen_at")
	}
}

func TestCount(t *testing.T) {
	r := newTestRegistry()

	if c := r.Count(); c != 0 {
		t.Errorf("expected 0, got %d", c)
	}

	r.Register("alice", "install-1")
	r.Register("bob", "install-2")

	if c := r.Count(); c != 2 {
		t.Errorf("expected 2, got %d", c)
	}
}
