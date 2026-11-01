package auth

import "testing"

func TestParseAndMatchCIDRs(t *testing.T) {
	list, err := ParseCIDRs([]string{"10.1.2.3/8", "192.0.2.10", "2001:db8::1"})
	if err != nil {
		t.Fatal(err)
	}
	if list[0] != "10.0.0.0/8" || list[1] != "192.0.2.10/32" || list[2] != "2001:db8::1/128" {
		t.Fatalf("normalized: %#v", list)
	}
	stored := EncodeCIDRs(list)
	if !IPAllowed(stored, "10.9.9.9") || !IPAllowed(stored, "192.0.2.10") || IPAllowed(stored, "192.0.2.11") {
		t.Fatalf("match failed for %s", stored)
	}
	if IPAllowed(stored, "") || IPAllowed(stored, "not-an-ip") {
		t.Fatal("bad client address was allowed")
	}
	if !IPAllowed("", "203.0.113.5") || !IPAllowed("[]", "203.0.113.5") {
		t.Fatal("empty allowlist must permit")
	}
	if _, err = ParseCIDRs([]string{"nope"}); err != ErrCIDR {
		t.Fatalf("want ErrCIDR, got %v", err)
	}
}
