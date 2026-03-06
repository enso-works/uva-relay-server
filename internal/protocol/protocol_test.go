package protocol

import (
	"encoding/json"
	"testing"
)

func TestIsValidUsername(t *testing.T) {
	tests := []struct {
		name     string
		username string
		want     bool
	}{
		{"valid simple", "alice", true},
		{"valid with hyphen", "dev-server", true},
		{"valid with numbers", "server123", true},
		{"valid mixed", "my-home-pc", true},
		{"valid min length", "abc", true},
		{"valid max length", "abcdefghijklmnopqrstuvwxyz012345", true},

		{"too short 1 char", "a", false},
		{"too short 2 chars", "ab", false},
		{"too long", "abcdefghijklmnopqrstuvwxyz0123456", false},
		{"starts with hyphen", "-invalid", false},
		{"ends with hyphen", "invalid-", false},
		{"uppercase", "Alice", false},
		{"underscore", "user_name", false},
		{"space", "user name", false},
		{"special chars", "user@name", false},
		{"empty", "", false},
		{"only hyphens", "---", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsValidUsername(tt.username)
			if got != tt.want {
				t.Errorf("IsValidUsername(%q) = %v, want %v", tt.username, got, tt.want)
			}
		})
	}
}

func TestParseServerRegister(t *testing.T) {
	data := []byte(`{"type":"server_register","username":"alice","installId":"abc-123","authToken":"server-secret"}`)
	msg := ParseMessage(data)
	sr, ok := msg.(*ServerRegister)
	if !ok {
		t.Fatalf("expected *ServerRegister, got %T", msg)
	}
	if sr.Username != "alice" {
		t.Errorf("expected username alice, got %s", sr.Username)
	}
	if sr.InstallID != "abc-123" {
		t.Errorf("expected installId abc-123, got %s", sr.InstallID)
	}
	if sr.AuthToken != "server-secret" {
		t.Errorf("expected authToken server-secret, got %s", sr.AuthToken)
	}
}

func TestParseClientConnect(t *testing.T) {
	data := []byte(`{"type":"client_connect","username":"bob"}`)
	msg := ParseMessage(data)
	cc, ok := msg.(*ClientConnect)
	if !ok {
		t.Fatalf("expected *ClientConnect, got %T", msg)
	}
	if cc.Username != "bob" {
		t.Errorf("expected username bob, got %s", cc.Username)
	}
}

func TestParseUnknownType(t *testing.T) {
	data := []byte(`{"type":"unknown"}`)
	msg := ParseMessage(data)
	if msg != nil {
		t.Errorf("expected nil for unknown type, got %T", msg)
	}
}

func TestParseInvalidJSON(t *testing.T) {
	data := []byte(`not json`)
	msg := ParseMessage(data)
	if msg != nil {
		t.Errorf("expected nil for invalid JSON, got %T", msg)
	}
}

func TestNewServerRegisteredJSON(t *testing.T) {
	msg := NewServerRegistered()
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	expected := `{"type":"server_registered"}`
	if string(data) != expected {
		t.Errorf("expected %s, got %s", expected, string(data))
	}
}

func TestNewServerRejectedJSON(t *testing.T) {
	msg := NewServerRejected("username_taken")
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	expected := `{"type":"server_rejected","reason":"username_taken"}`
	if string(data) != expected {
		t.Errorf("expected %s, got %s", expected, string(data))
	}
}

func TestNewClientConnectedJSON(t *testing.T) {
	msg := NewClientConnected()
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	expected := `{"type":"client_connected"}`
	if string(data) != expected {
		t.Errorf("expected %s, got %s", expected, string(data))
	}
}

func TestNewClientErrorJSON(t *testing.T) {
	msg := NewClientError("server_offline")
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	expected := `{"type":"client_error","reason":"server_offline"}`
	if string(data) != expected {
		t.Errorf("expected %s, got %s", expected, string(data))
	}
}
