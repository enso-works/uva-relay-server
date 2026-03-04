package connections

import (
	"testing"

	"github.com/anthropics/uvame-relay/internal/registry"
	"github.com/anthropics/uvame-relay/internal/testutil"
)

func newTestManager() (*Manager, *registry.Registry) {
	reg := registry.NewFromDB(testutil.NewTestDB())
	return NewManager(reg), reg
}

// mockConn creates a Conn with a nil WS (safe for map operations in tests,
// but cannot actually send/receive messages).
func mockConn(username string, isServer bool) *Conn {
	return &Conn{
		WS:       nil,
		Username: username,
		IsServer: isServer,
	}
}

func TestRegisterServer(t *testing.T) {
	t.Run("registers server successfully", func(t *testing.T) {
		m, _ := newTestManager()
		conn := mockConn("alice", true)
		result := m.RegisterServer(conn, "alice", "install-1")
		if result != Registered {
			t.Errorf("expected registered, got %s", result)
		}
		if !m.IsWaiting("alice") {
			t.Error("expected alice to be waiting")
		}
	})

	t.Run("rejects invalid username", func(t *testing.T) {
		m, _ := newTestManager()
		conn := mockConn("ab", true)
		result := m.RegisterServer(conn, "ab", "install-1")
		if result != InvalidUsername {
			t.Errorf("expected invalid_username, got %s", result)
		}
	})

	t.Run("rejects username taken by different installId", func(t *testing.T) {
		m, _ := newTestManager()
		conn1 := mockConn("alice", true)
		m.RegisterServer(conn1, "alice", "install-1")

		conn2 := mockConn("alice", true)
		result := m.RegisterServer(conn2, "alice", "install-2")
		if result != UsernameTaken {
			t.Errorf("expected username_taken, got %s", result)
		}
	})

	t.Run("registers multiple different usernames", func(t *testing.T) {
		m, _ := newTestManager()
		conn1 := mockConn("alice", true)
		conn2 := mockConn("bob", true)
		m.RegisterServer(conn1, "alice", "install-1")
		m.RegisterServer(conn2, "bob", "install-2")

		if m.GetWaitingCount() != 2 {
			t.Errorf("expected 2 waiting, got %d", m.GetWaitingCount())
		}
	})
}

func TestConnectClient(t *testing.T) {
	t.Run("connects to waiting server", func(t *testing.T) {
		m, _ := newTestManager()
		server := mockConn("alice", true)
		m.RegisterServer(server, "alice", "install-1")

		client := mockConn("", false)
		result := m.ConnectClient(client, "alice")
		if result.Status != "connected" {
			t.Errorf("expected connected, got %s", result.Status)
		}
		if result.ServerWS != server {
			t.Error("expected server conn in result")
		}
	})

	t.Run("returns server_offline when no waiting connection", func(t *testing.T) {
		m, reg := newTestManager()
		// Register in DB but don't have waiting conn
		reg.Register("alice", "install-1")

		client := mockConn("", false)
		result := m.ConnectClient(client, "alice")
		if result.Status != "server_offline" {
			t.Errorf("expected server_offline, got %s", result.Status)
		}
	})

	t.Run("returns unknown_username for unregistered username", func(t *testing.T) {
		m, _ := newTestManager()
		client := mockConn("", false)
		result := m.ConnectClient(client, "nonexistent")
		if result.Status != "unknown_username" {
			t.Errorf("expected unknown_username, got %s", result.Status)
		}
	})
}

func TestHandleClose(t *testing.T) {
	t.Run("removes waiting connection", func(t *testing.T) {
		m, _ := newTestManager()
		conn := mockConn("alice", true)
		m.RegisterServer(conn, "alice", "install-1")

		result := m.HandleClose(conn)
		if result {
			t.Error("expected false (not a pair)")
		}
		if m.IsWaiting("alice") {
			t.Error("expected alice to be removed from waiting")
		}
	})
}

func TestIsPaired(t *testing.T) {
	t.Run("returns false for waiting connection", func(t *testing.T) {
		m, _ := newTestManager()
		conn := mockConn("alice", true)
		m.RegisterServer(conn, "alice", "install-1")

		if m.IsPaired(conn) {
			t.Error("expected not paired")
		}
	})
}

func TestGetWaitingUsernames(t *testing.T) {
	m, _ := newTestManager()
	conn1 := mockConn("alice", true)
	conn2 := mockConn("bob", true)
	m.RegisterServer(conn1, "alice", "install-1")
	m.RegisterServer(conn2, "bob", "install-2")

	names := m.GetWaitingUsernames()
	if len(names) != 2 {
		t.Errorf("expected 2 names, got %d", len(names))
	}

	nameMap := make(map[string]bool)
	for _, n := range names {
		nameMap[n] = true
	}
	if !nameMap["alice"] || !nameMap["bob"] {
		t.Error("expected alice and bob in waiting")
	}
}
