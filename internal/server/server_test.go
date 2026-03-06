package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func startTestServer(t *testing.T) *Server {
	t.Helper()
	srv, err := New(Options{
		Port:         0,
		InMemoryDB:   true,
		PingInterval: 5 * time.Second,
		PongTimeout:  2 * time.Second,
		ReclaimDays:  90,
		LogLevel:     "warn",
	})
	if err != nil {
		t.Fatal(err)
	}

	go func() { _ = srv.Start() }()
	t.Cleanup(func() {
		srv.Close()
	})

	return srv
}

func connectWS(t *testing.T, srv *Server) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	url := fmt.Sprintf("ws://localhost:%d/ws", srv.Port())
	ws, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.CloseNow() })
	return ws
}

func sendMsg(t *testing.T, ws *websocket.Conn, v any) {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := ws.Write(ctx, websocket.MessageText, data); err != nil {
		t.Fatal(err)
	}
}

func readMsg(t *testing.T, ws *websocket.Conn) map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, data, err := ws.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var msg map[string]any
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatal(err)
	}
	return msg
}

func readBytes(t *testing.T, ws *websocket.Conn) (websocket.MessageType, []byte) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	msgType, data, err := ws.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return msgType, data
}

func registerServer(t *testing.T, ws *websocket.Conn, username, installID string) {
	t.Helper()
	sendMsg(t, ws, map[string]any{
		"type":      "server_register",
		"username":  username,
		"installId": installID,
	})
	msg := readMsg(t, ws)
	if msg["type"] != "server_registered" {
		t.Fatalf("expected server_registered, got %v", msg)
	}
}

func connectClient(t *testing.T, ws *websocket.Conn, username string) {
	t.Helper()
	sendMsg(t, ws, map[string]any{
		"type":     "client_connect",
		"username": username,
	})
	msg := readMsg(t, ws)
	if msg["type"] != "client_connected" {
		t.Fatalf("expected client_connected, got %v", msg)
	}
}

func getJSON(t *testing.T, url string) map[string]any {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

// --- Server Registration Flow ---

func TestServerRegistration(t *testing.T) {
	srv := startTestServer(t)

	t.Run("successfully registers server with valid username", func(t *testing.T) {
		ws := connectWS(t, srv)
		registerServer(t, ws, "test-server", "install-1")

		names := srv.Manager.GetWaitingUsernames()
		found := false
		for _, n := range names {
			if n == "test-server" {
				found = true
			}
		}
		if !found {
			t.Error("expected test-server in waiting")
		}
	})

	t.Run("receives server_registered response", func(t *testing.T) {
		ws := connectWS(t, srv)
		sendMsg(t, ws, map[string]any{
			"type":      "server_register",
			"username":  "reg-test",
			"installId": "install-2",
		})
		msg := readMsg(t, ws)
		if msg["type"] != "server_registered" {
			t.Errorf("expected server_registered, got %v", msg["type"])
		}
	})
}

// --- Client Connection Flow ---

func TestClientConnection(t *testing.T) {
	srv := startTestServer(t)

	t.Run("connects client to waiting server", func(t *testing.T) {
		serverWS := connectWS(t, srv)
		clientWS := connectWS(t, srv)

		registerServer(t, serverWS, "pair-test", "install-1")
		connectClient(t, clientWS, "pair-test")
	})

	t.Run("removes server from waiting after pairing", func(t *testing.T) {
		serverWS := connectWS(t, srv)
		clientWS := connectWS(t, srv)

		registerServer(t, serverWS, "remove-test", "install-1")
		if !srv.Manager.IsWaiting("remove-test") {
			t.Error("expected server to be waiting")
		}

		connectClient(t, clientWS, "remove-test")
		if srv.Manager.IsWaiting("remove-test") {
			t.Error("expected server to be removed from waiting")
		}
	})
}

// --- Message Forwarding ---

func TestMessageForwarding(t *testing.T) {
	srv := startTestServer(t)

	t.Run("forwards text from client to server", func(t *testing.T) {
		serverWS := connectWS(t, srv)
		clientWS := connectWS(t, srv)

		registerServer(t, serverWS, "fwd-c2s", "install-1")
		connectClient(t, clientWS, "fwd-c2s")

		testMsg := `{"test":"hello from client"}`
		ctx := context.Background()
		if err := clientWS.Write(ctx, websocket.MessageText, []byte(testMsg)); err != nil {
			t.Fatal(err)
		}

		_, data := readBytes(t, serverWS)
		if string(data) != testMsg {
			t.Errorf("expected %s, got %s", testMsg, string(data))
		}
	})

	t.Run("forwards text from server to client", func(t *testing.T) {
		serverWS := connectWS(t, srv)
		clientWS := connectWS(t, srv)

		registerServer(t, serverWS, "fwd-s2c", "install-1")
		connectClient(t, clientWS, "fwd-s2c")

		testMsg := `{"test":"hello from server"}`
		ctx := context.Background()
		if err := serverWS.Write(ctx, websocket.MessageText, []byte(testMsg)); err != nil {
			t.Fatal(err)
		}

		_, data := readBytes(t, clientWS)
		if string(data) != testMsg {
			t.Errorf("expected %s, got %s", testMsg, string(data))
		}
	})

	t.Run("forwards binary data correctly", func(t *testing.T) {
		serverWS := connectWS(t, srv)
		clientWS := connectWS(t, srv)

		registerServer(t, serverWS, "fwd-bin", "install-1")
		connectClient(t, clientWS, "fwd-bin")

		binaryData := []byte{0x00, 0x01, 0x02, 0xff, 0xfe, 0xfd}
		ctx := context.Background()
		if err := clientWS.Write(ctx, websocket.MessageBinary, binaryData); err != nil {
			t.Fatal(err)
		}

		msgType, data := readBytes(t, serverWS)
		if msgType != websocket.MessageBinary {
			t.Errorf("expected binary message, got %v", msgType)
		}
		if len(data) != len(binaryData) {
			t.Errorf("expected %d bytes, got %d", len(binaryData), len(data))
		}
		for i, b := range binaryData {
			if data[i] != b {
				t.Errorf("byte %d: expected %x, got %x", i, b, data[i])
			}
		}
	})

	t.Run("handles multiple messages in sequence", func(t *testing.T) {
		serverWS := connectWS(t, srv)
		clientWS := connectWS(t, srv)

		registerServer(t, serverWS, "fwd-seq", "install-1")
		connectClient(t, clientWS, "fwd-seq")

		messages := []string{"message1", "message2", "message3"}
		ctx := context.Background()
		for _, m := range messages {
			if err := clientWS.Write(ctx, websocket.MessageText, []byte(m)); err != nil {
				t.Fatal(err)
			}
		}

		for _, expected := range messages {
			_, data := readBytes(t, serverWS)
			if string(data) != expected {
				t.Errorf("expected %s, got %s", expected, string(data))
			}
		}
	})
}

// --- Server Offline ---

func TestServerOffline(t *testing.T) {
	srv := startTestServer(t)

	t.Run("returns unknown_username for unregistered username", func(t *testing.T) {
		clientWS := connectWS(t, srv)
		sendMsg(t, clientWS, map[string]any{
			"type":     "client_connect",
			"username": "nonexistent-username",
		})
		msg := readMsg(t, clientWS)
		if msg["type"] != "client_error" {
			t.Errorf("expected client_error, got %v", msg["type"])
		}
		if msg["reason"] != "unknown_username" {
			t.Errorf("expected unknown_username, got %v", msg["reason"])
		}
	})

	t.Run("returns server_offline when server registered but not connected", func(t *testing.T) {
		serverWS := connectWS(t, srv)
		registerServer(t, serverWS, "offline-test", "install-1")
		serverWS.Close(websocket.StatusNormalClosure, "bye")
		time.Sleep(100 * time.Millisecond)

		clientWS := connectWS(t, srv)
		sendMsg(t, clientWS, map[string]any{
			"type":     "client_connect",
			"username": "offline-test",
		})
		msg := readMsg(t, clientWS)
		if msg["type"] != "client_error" {
			t.Errorf("expected client_error, got %v", msg["type"])
		}
		if msg["reason"] != "server_offline" {
			t.Errorf("expected server_offline, got %v", msg["reason"])
		}
	})
}

// --- Username Taken ---

func TestUsernameTaken(t *testing.T) {
	srv := startTestServer(t)

	t.Run("rejects registration with different installId", func(t *testing.T) {
		ws1 := connectWS(t, srv)
		registerServer(t, ws1, "taken-test", "install-1")

		ws2 := connectWS(t, srv)
		sendMsg(t, ws2, map[string]any{
			"type":      "server_register",
			"username":  "taken-test",
			"installId": "install-2",
		})
		msg := readMsg(t, ws2)
		if msg["type"] != "server_rejected" {
			t.Errorf("expected server_rejected, got %v", msg["type"])
		}
		if msg["reason"] != "username_taken" {
			t.Errorf("expected username_taken, got %v", msg["reason"])
		}
	})
}

// --- Same installId Replacement ---

func TestSameInstallIdReplacement(t *testing.T) {
	srv := startTestServer(t)

	t.Run("replaces waiting connection with same installId", func(t *testing.T) {
		ws1 := connectWS(t, srv)
		registerServer(t, ws1, "replace-test", "install-1")
		if !srv.Manager.IsWaiting("replace-test") {
			t.Error("expected replace-test to be waiting")
		}

		ws2 := connectWS(t, srv)
		registerServer(t, ws2, "replace-test", "install-1")

		// Old connection should be closed
		time.Sleep(100 * time.Millisecond)
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		_, _, err := ws1.Read(ctx)
		if err == nil {
			t.Error("expected old connection to be closed")
		}
	})

	t.Run("allows new client to connect after replacement", func(t *testing.T) {
		ws1 := connectWS(t, srv)
		registerServer(t, ws1, "replace-cli", "install-1")

		ws2 := connectWS(t, srv)
		registerServer(t, ws2, "replace-cli", "install-1")
		time.Sleep(100 * time.Millisecond)

		clientWS := connectWS(t, srv)
		connectClient(t, clientWS, "replace-cli")

		// Verify forwarding works to new server
		ctx := context.Background()
		clientWS.Write(ctx, websocket.MessageText, []byte("test message"))
		_, data := readBytes(t, ws2)
		if string(data) != "test message" {
			t.Errorf("expected 'test message', got %s", string(data))
		}
	})
}

// --- Reconnection ---

func TestReconnection(t *testing.T) {
	srv := startTestServer(t)

	t.Run("allows server to reconnect after disconnect", func(t *testing.T) {
		ws := connectWS(t, srv)
		registerServer(t, ws, "recon-test", "install-1")
		ws.Close(websocket.StatusNormalClosure, "bye")
		time.Sleep(100 * time.Millisecond)

		ws2 := connectWS(t, srv)
		registerServer(t, ws2, "recon-test", "install-1")
		if !srv.Manager.IsWaiting("recon-test") {
			t.Error("expected recon-test to be waiting after reconnect")
		}
	})

	t.Run("allows client to connect after server reconnects", func(t *testing.T) {
		ws := connectWS(t, srv)
		registerServer(t, ws, "recon-cli", "install-1")
		ws.Close(websocket.StatusNormalClosure, "bye")
		time.Sleep(100 * time.Millisecond)

		ws2 := connectWS(t, srv)
		registerServer(t, ws2, "recon-cli", "install-1")

		clientWS := connectWS(t, srv)
		connectClient(t, clientWS, "recon-cli")

		ctx := context.Background()
		clientWS.Write(ctx, websocket.MessageText, []byte("reconnect test"))
		_, data := readBytes(t, ws2)
		if string(data) != "reconnect test" {
			t.Errorf("expected 'reconnect test', got %s", string(data))
		}
	})
}

// --- Full Relay Flow ---

func TestFullRelayFlow(t *testing.T) {
	srv := startTestServer(t)

	t.Run("bidirectional communication", func(t *testing.T) {
		serverWS := connectWS(t, srv)
		clientWS := connectWS(t, srv)

		registerServer(t, serverWS, "full-test", "install-1")
		connectClient(t, clientWS, "full-test")

		ctx := context.Background()

		// Client to server
		clientWS.Write(ctx, websocket.MessageText, []byte(`{"type":"client_message"}`))
		_, data := readBytes(t, serverWS)
		var msg map[string]any
		json.Unmarshal(data, &msg)
		if msg["type"] != "client_message" {
			t.Errorf("expected client_message, got %v", msg["type"])
		}

		// Server to client
		serverWS.Write(ctx, websocket.MessageText, []byte(`{"type":"server_response"}`))
		_, data = readBytes(t, clientWS)
		json.Unmarshal(data, &msg)
		if msg["type"] != "server_response" {
			t.Errorf("expected server_response, got %v", msg["type"])
		}
	})

	t.Run("client disconnection closes server", func(t *testing.T) {
		serverWS := connectWS(t, srv)
		clientWS := connectWS(t, srv)

		registerServer(t, serverWS, "disc-cli", "install-1")
		connectClient(t, clientWS, "disc-cli")

		clientWS.Close(websocket.StatusNormalClosure, "bye")
		time.Sleep(100 * time.Millisecond)

		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		_, _, err := serverWS.Read(ctx)
		if err == nil {
			t.Error("expected server connection to be closed")
		}
	})

	t.Run("server disconnection closes client", func(t *testing.T) {
		serverWS := connectWS(t, srv)
		clientWS := connectWS(t, srv)

		registerServer(t, serverWS, "disc-srv", "install-1")
		connectClient(t, clientWS, "disc-srv")

		serverWS.Close(websocket.StatusNormalClosure, "bye")
		time.Sleep(100 * time.Millisecond)

		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		_, _, err := clientWS.Read(ctx)
		if err == nil {
			t.Error("expected client connection to be closed")
		}
	})
}

// --- Invalid Username ---

func TestInvalidUsername(t *testing.T) {
	srv := startTestServer(t)

	tests := []struct {
		name     string
		username string
	}{
		{"too short", "ab"},
		{"underscore", "user_name"},
		{"starts with hyphen", "-invalid-name"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ws := connectWS(t, srv)
			sendMsg(t, ws, map[string]any{
				"type":      "server_register",
				"username":  tt.username,
				"installId": "install-1",
			})
			msg := readMsg(t, ws)
			if msg["type"] != "server_rejected" {
				t.Errorf("expected server_rejected, got %v", msg["type"])
			}
			if msg["reason"] != "invalid_username" {
				t.Errorf("expected invalid_username, got %v", msg["reason"])
			}
		})
	}
}

// --- Health Endpoint ---

func TestHealthEndpoint(t *testing.T) {
	srv := startTestServer(t)

	t.Run("reports correct waiting count", func(t *testing.T) {
		url := fmt.Sprintf("http://localhost:%d/health", srv.Port())
		initial := getJSON(t, url)
		initialWaiting := int(initial["waiting"].(float64))

		ws := connectWS(t, srv)
		registerServer(t, ws, "health-wait", "install-1")

		health := getJSON(t, url)
		if int(health["waiting"].(float64)) != initialWaiting+1 {
			t.Errorf("expected waiting to increase by 1")
		}
	})

	t.Run("reports correct pair count", func(t *testing.T) {
		url := fmt.Sprintf("http://localhost:%d/health", srv.Port())
		initial := getJSON(t, url)
		initialPairs := int(initial["pairs"].(float64))

		serverWS := connectWS(t, srv)
		clientWS := connectWS(t, srv)
		registerServer(t, serverWS, "health-pair", "install-1")
		connectClient(t, clientWS, "health-pair")

		health := getJSON(t, url)
		if int(health["pairs"].(float64)) != initialPairs+1 {
			t.Errorf("expected pairs to increase by 1")
		}
	})
}

// --- Online Endpoint ---

func TestOnlineEndpoint(t *testing.T) {
	srv := startTestServer(t)

	t.Run("returns false for offline username", func(t *testing.T) {
		url := fmt.Sprintf("http://localhost:%d/online/nobody", srv.Port())
		result := getJSON(t, url)
		if result["online"] != false {
			t.Error("expected online=false")
		}
	})

	t.Run("returns true for waiting username", func(t *testing.T) {
		ws := connectWS(t, srv)
		registerServer(t, ws, "online-test", "install-1")

		url := fmt.Sprintf("http://localhost:%d/online/online-test", srv.Port())
		result := getJSON(t, url)
		if result["online"] != true {
			t.Error("expected online=true")
		}
	})
}

// --- Concurrent Forwarding ---

func TestConcurrentForwarding(t *testing.T) {
	srv := startTestServer(t)
	serverWS := connectWS(t, srv)
	clientWS := connectWS(t, srv)

	registerServer(t, serverWS, "concurrent", "install-1")
	connectClient(t, clientWS, "concurrent")

	const msgCount = 50
	ctx := context.Background()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < msgCount; i++ {
			msg := fmt.Sprintf("msg-%d", i)
			clientWS.Write(ctx, websocket.MessageText, []byte(msg))
		}
	}()

	received := 0
	for received < msgCount {
		readCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		_, _, err := serverWS.Read(readCtx)
		cancel()
		if err != nil {
			t.Fatalf("read error after %d messages: %v", received, err)
		}
		received++
	}

	wg.Wait()
	if received != msgCount {
		t.Errorf("expected %d messages, got %d", msgCount, received)
	}
}
