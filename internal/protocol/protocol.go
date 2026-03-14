package protocol

import (
	"encoding/json"
	"regexp"
)

var usernameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,30}[a-z0-9]$`)

func IsValidUsername(username string) bool {
	return usernameRe.MatchString(username)
}

// Message types

type ServerRegister struct {
	Type        string `json:"type"`
	Username    string `json:"username"`
	InstallID   string `json:"installId"`
	AuthToken   string `json:"authToken,omitempty"`
	JWT         string `json:"jwt,omitempty"`
	MultiClient bool   `json:"multiClient,omitempty"`
}

type ServerRegistered struct {
	Type        string `json:"type"`
	JWT         string `json:"jwt,omitempty"`
	MultiClient bool   `json:"multiClient,omitempty"`
}

type ServerRejected struct {
	Type   string `json:"type"`
	Reason string `json:"reason"`
}

type ClientConnect struct {
	Type     string `json:"type"`
	Username string `json:"username"`
	ClientID string `json:"clientId,omitempty"`
}

type ClientConnected struct {
	Type     string `json:"type"`
	ClientID string `json:"clientId,omitempty"`
}

type ClientError struct {
	Type   string `json:"type"`
	Reason string `json:"reason"`
}

// Multi-client control messages (relay -> server)

type RelayClientJoined struct {
	Type     string `json:"type"`
	ClientID string `json:"clientId"`
}

type RelayClientLeft struct {
	Type     string `json:"type"`
	ClientID string `json:"clientId"`
}

func NewRelayClientJoined(clientID string) RelayClientJoined {
	return RelayClientJoined{Type: "relay_client_joined", ClientID: clientID}
}

func NewRelayClientLeft(clientID string) RelayClientLeft {
	return RelayClientLeft{Type: "relay_client_left", ClientID: clientID}
}

// Backpressure notification (relay -> sender)

type RelayBackpressure struct {
	Type   string `json:"type"`
	Action string `json:"action"`
}

func NewRelayBackpressure(action string) RelayBackpressure {
	return RelayBackpressure{Type: "relay_backpressure", Action: action}
}

// BufferedFrame represents a single buffered WebSocket frame.
type BufferedFrame struct {
	Timestamp int64  `json:"ts"`
	Direction string `json:"dir"`     // "s2c" or "c2s"
	MsgType   int    `json:"msgType"` // websocket.MessageText=1, Binary=2
	Data      string `json:"data"`    // base64 encoded
}

// BufferedMessages is sent to a reconnecting client with missed messages.
type BufferedMessages struct {
	Type     string          `json:"type"`
	Messages []BufferedFrame `json:"messages"`
}

func NewBufferedMessages(messages []BufferedFrame) BufferedMessages {
	return BufferedMessages{Type: "buffered_messages", Messages: messages}
}

// Constructors

func NewServerRegistered(jwt string, multiClient bool) ServerRegistered {
	return ServerRegistered{Type: "server_registered", JWT: jwt, MultiClient: multiClient}
}

func NewServerRejected(reason string) ServerRejected {
	return ServerRejected{Type: "server_rejected", Reason: reason}
}

func NewClientConnected(clientID string) ClientConnected {
	return ClientConnected{Type: "client_connected", ClientID: clientID}
}

func NewClientError(reason string) ClientError {
	return ClientError{Type: "client_error", Reason: reason}
}

// ParseMessage attempts to parse a JSON message and returns the typed struct.
// Returns nil if the message type is unknown.
func ParseMessage(data []byte) any {
	var raw struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil
	}

	switch raw.Type {
	case "server_register":
		var msg ServerRegister
		if err := json.Unmarshal(data, &msg); err != nil {
			return nil
		}
		return &msg
	case "client_connect":
		var msg ClientConnect
		if err := json.Unmarshal(data, &msg); err != nil {
			return nil
		}
		return &msg
	default:
		return nil
	}
}
