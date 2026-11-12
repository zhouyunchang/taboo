package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRealIPTrust(t *testing.T) {
	var got string
	h := RealIP(ParseTrusted(""))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { got = ClientIP(r) }))
	cases := []struct {
		peer    string
		headers map[string]string
		want    string
	}{
		{"172.18.0.4:5000", map[string]string{"X-Real-IP": "203.0.113.9"}, "203.0.113.9"},
		{"172.18.0.4:5000", map[string]string{"True-Client-IP": "10.9.9.9", "X-Real-IP": "203.0.113.9"}, "203.0.113.9"},
		{"172.18.0.4:5000", map[string]string{"True-Client-IP": "10.9.9.9"}, "172.18.0.4"},
		{"172.18.0.4:5000", map[string]string{"X-Forwarded-For": "1.1.1.1, 203.0.113.7"}, "203.0.113.7"},
		{"198.51.100.2:5000", map[string]string{"X-Real-IP": "10.0.0.1"}, "198.51.100.2"},
		{"172.18.0.4:5000", map[string]string{"X-Real-IP": "not-an-ip"}, "172.18.0.4"},
	}
	for _, c := range cases {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = c.peer
		for k, v := range c.headers {
			req.Header.Set(k, v)
		}
		h.ServeHTTP(httptest.NewRecorder(), req)
		if got != c.want {
			t.Errorf("peer %s headers %v: got %s want %s", c.peer, c.headers, got, c.want)
		}
	}
	if ParseTrusted("none") != nil {
		t.Fatal("none should trust nothing")
	}
}
