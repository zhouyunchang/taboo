package httpx

import (
	"net"
	"net/http"

	"github.com/go-chi/chi/v5/middleware"
)

func ClientIP(r *http.Request) string {
	if r == nil {
		return ""
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && host != "" {
		return host
	}
	return r.RemoteAddr
}

func UserAgent(r *http.Request) string {
	if r == nil {
		return ""
	}
	return r.UserAgent()
}

func RequestID(r *http.Request) string {
	if r == nil {
		return ""
	}
	return middleware.GetReqID(r.Context())
}
