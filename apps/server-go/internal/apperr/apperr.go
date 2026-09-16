// 统一错误模型：{code, message, details} —— 对齐设计文档 §5 统一约定
package apperr

import "encoding/json"

type Error struct {
	Status  int             `json:"-"`
	Code    string          `json:"code"`
	Message string          `json:"message"`
	Details json.RawMessage `json:"details,omitempty"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

func New(status int, code, message string) *Error {
	return &Error{Status: status, Code: code, Message: message}
}

var (
	Unauthorized      = New(401, "UNAUTHORIZED", "missing or invalid token")
	BadCredentials    = New(401, "BAD_CREDENTIALS", "invalid email or password")
	InvalidRefresh    = New(401, "INVALID_REFRESH", "invalid refresh token")
	TooManyRequests   = New(429, "RATE_LIMITED", "too many attempts, try again later")
	Forbidden         = New(403, "FORBIDDEN", "no access")
	NotFound          = New(404, "NOT_FOUND", "not found")
	InvalidEmail      = New(400, "INVALID_EMAIL", "invalid email")
	WeakPassword      = New(400, "WEAK_PASSWORD", "password must be at least 8 chars")
	EmailTaken        = New(409, "EMAIL_TAKEN", "email already registered")
	InvalidInput      = New(400, "INVALID", "invalid input")
	EnvNotFound       = New(404, "NOT_FOUND", "env not found")
	SecretNotFound    = New(404, "NOT_FOUND", "secret not found")
	VersionNotFound   = New(404, "NOT_FOUND", "version not found")
)
