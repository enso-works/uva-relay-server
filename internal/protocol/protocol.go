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
	Type      string `json:"type"`
	Username  string `json:"username"`
	InstallID string `json:"installId"`
}

type ServerRegistered struct {
	Type string `json:"type"`
}

type ServerRejected struct {
	Type   string `json:"type"`
	Reason string `json:"reason"`
}

type ClientConnect struct {
	Type     string `json:"type"`
	Username string `json:"username"`
}

type ClientConnected struct {
	Type string `json:"type"`
}

type ClientError struct {
	Type   string `json:"type"`
	Reason string `json:"reason"`
}

// Constructors

func NewServerRegistered() ServerRegistered {
	return ServerRegistered{Type: "server_registered"}
}

func NewServerRejected(reason string) ServerRejected {
	return ServerRejected{Type: "server_rejected", Reason: reason}
}

func NewClientConnected() ClientConnected {
	return ClientConnected{Type: "client_connected"}
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
