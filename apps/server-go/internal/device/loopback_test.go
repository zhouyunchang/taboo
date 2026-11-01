package device

import "testing"

func TestLoopback(t *testing.T) {
	ok := []string{"http://127.0.0.1:43123/callback", "http://localhost:9/callback", "http://[::1]:44000/callback"}
	bad := []string{
		"https://127.0.0.1:43123/callback",
		"http://127.0.0.1/callback",
		"http://127.0.0.1:43123/callback?x=1",
		"http://127.0.0.1:43123/callback#x",
		"http://evil.com/callback",
		"http://127.0.0.1.evil.com:43123/callback",
		"http://user:pass@127.0.0.1:43123/callback",
		"http://127.0.0.1:43123/other",
	}
	for _, u := range ok {
		if !Loopback(u) {
			t.Fatalf("want allow %s", u)
		}
	}
	for _, u := range bad {
		if Loopback(u) {
			t.Fatalf("want reject %s", u)
		}
	}
}
