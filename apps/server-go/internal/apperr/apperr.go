// 统一错误模型：{code, message, details} —— 对齐设计文档 §5 统一约定
package apperr

import (
	"encoding/json"
	"log/slog"
)

type Error struct {
	Status  int             `json:"-"`
	Code    string          `json:"code"`
	Message string          `json:"message"`
	Details json.RawMessage `json:"details,omitempty"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

// genericInternal 是 500 INTERNAL 对外唯一的文案。
const genericInternal = "internal error"

// MarshalJSON 保证 500 INTERNAL 不把内部错误原文（SQL、驱动、路径等）返回给客户端；
// 原文只写服务端日志。
func (e Error) MarshalJSON() ([]byte, error) {
	type wire Error
	out := wire(e)
	if e.Status >= 500 && e.Code == "INTERNAL" && e.Message != genericInternal {
		slog.Error("internal error", "message", e.Message)
		out.Message = genericInternal
		out.Details = nil
	}
	return json.Marshal(out)
}

func New(status int, code, message string) *Error {
	return &Error{Status: status, Code: code, Message: message}
}

// WithDetails returns a copy that carries structured context. Shared sentinel
// errors are never mutated.
func (e *Error) WithDetails(v any) *Error {
	if e == nil {
		return nil
	}
	cp := *e
	if b, err := json.Marshal(v); err == nil {
		cp.Details = b
	}
	return &cp
}

var (
	Unauthorized    = New(401, "UNAUTHORIZED", "missing or invalid token")
	BadCredentials  = New(401, "BAD_CREDENTIALS", "invalid email or password")
	InvalidRefresh  = New(401, "INVALID_REFRESH", "invalid refresh token")
	TooManyRequests = New(429, "RATE_LIMITED", "too many attempts, try again later")
	Forbidden       = New(403, "FORBIDDEN", "no access")
	NotFound        = New(404, "NOT_FOUND", "not found")
	InvalidEmail    = New(400, "INVALID_EMAIL", "invalid email")
	WeakPassword    = New(400, "WEAK_PASSWORD", "password must be at least 8 chars")
	EmailTaken      = New(409, "EMAIL_TAKEN", "email already registered")
	InvalidInput    = New(400, "INVALID", "invalid input")
	EnvNotFound     = New(404, "NOT_FOUND", "env not found")
	SecretNotFound  = New(404, "NOT_FOUND", "secret not found")
	VersionNotFound = New(404, "NOT_FOUND", "version not found")
)
