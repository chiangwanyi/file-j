// Package protocol defines the wire format spoken between the Vue front-end and
// the Go backend over a local WebSocket connection.
//
// The format is a deliberately small JSON-RPC dialect:
//
//	client -> server   {"id":"7","type":"request","method":"fs.list","params":{...}}
//	server -> client   {"id":"7","type":"response","ok":true,"result":{...}}
//	server -> client   {"type":"event","event":"transfer.progress","payload":{...}}
//
// Keeping the envelope this small means the front-end can implement it in ~100
// lines with no RPC framework, and every message stays human readable in a log.
package protocol

import "encoding/json"

// Request is a client initiated call.
type Request struct {
	ID     string          `json:"id"`
	Type   string          `json:"type"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// Response is the reply to exactly one Request, correlated by ID.
type Response struct {
	ID     string `json:"id"`
	Type   string `json:"type"`
	OK     bool   `json:"ok"`
	Result any    `json:"result,omitempty"`
	Error  *Error `json:"error,omitempty"`
}

// Event is an unsolicited server push. Events never carry an ID.
type Event struct {
	Type    string `json:"type"`
	Event   string `json:"event"`
	Payload any    `json:"payload"`
}

// Error is the machine readable failure representation. Code is a stable
// identifier the UI can branch on (see the Err* constants), Message is meant to
// be shown to the user verbatim.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Detail  string `json:"detail,omitempty"`
}

func (e *Error) Error() string {
	if e.Detail != "" {
		return e.Message + ": " + e.Detail
	}
	return e.Message
}

// Stable error codes shared with the front-end.
const (
	CodeBadRequest   = "BAD_REQUEST"
	CodeNotFound     = "NOT_FOUND"
	CodePermission   = "PERMISSION_DENIED"
	CodeConflict     = "ALREADY_EXISTS"
	CodeUnsupported  = "UNSUPPORTED"
	CodeAuthFailed   = "AUTH_FAILED"
	CodeHostKey      = "HOST_KEY_MISMATCH"
	CodeConnect      = "CONNECT_FAILED"
	CodeCanceled     = "CANCELED"
	CodeInternal     = "INTERNAL"
	CodeNotConnected = "NOT_CONNECTED"
)

// Errorf builds a protocol level error.
func Errorf(code, message string) *Error {
	return &Error{Code: code, Message: message}
}

// Wrapf builds a protocol level error with a technical detail string.
func Wrapf(code, message, detail string) *Error {
	return &Error{Code: code, Message: message, Detail: detail}
}
