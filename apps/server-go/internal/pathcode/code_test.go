package pathcode

import "testing"

func TestValidCodes(t *testing.T) {
	for _, code := range []string{"a", "lin", "bei-qiao", "u1"} {
		if !Valid(code) {
			t.Fatalf("%s should be valid", code)
		}
	}
	for _, code := range []string{"", "-", "-a", "a-", "API", "api", "settings", "login", "new", "invite", "has space", "中文"} {
		if Valid(code) {
			t.Fatalf("%s should be rejected", code)
		}
	}
}

func TestSlugFallsBack(t *testing.T) {
	if got := Slug("Lin.Zhang"); got != "lin-zhang" {
		t.Fatal(got)
	}
	if got := Slug("API"); got != "user" {
		t.Fatal(got)
	}
	if got := Slug("***"); got != "user" {
		t.Fatal(got)
	}
}
