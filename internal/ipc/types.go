// Package ipc implements length-prefixed JSON framing over Unix domain sockets.
package ipc

// Request is the envelope for all IPC calls between Strata services.
type Request struct {
	V      int            `json:"v"`
	ReqID  string         `json:"req_id"`
	Method string         `json:"method"`
	Auth   *Auth          `json:"auth,omitempty"`
	Params map[string]any `json:"params,omitempty"`
}

type Auth struct {
	Token string `json:"token"`
}

// Response is the envelope for all IPC replies.
type Response struct {
	V      int    `json:"v"`
	ReqID  string `json:"req_id"`
	OK     bool   `json:"ok"`
	Result any    `json:"result,omitempty"`
	Error  *Error `json:"error,omitempty"`
}

type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Error codes used across all Strata services.
const (
	ErrInvalidRequest = 1
	ErrAuthRequired   = 2
	ErrPermDenied     = 3
	ErrNotFound       = 4
	ErrInternal       = 5
)

func SuccessResponse(reqID string, result any) Response {
	return Response{V: 1, ReqID: reqID, OK: true, Result: result}
}

func ErrorResponse(reqID string, code int, msg string) Response {
	return Response{V: 1, ReqID: reqID, OK: false, Error: &Error{Code: code, Message: msg}}
}
