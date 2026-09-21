package rbac

import "testing"

func TestUserMatrix(t *testing.T) {
	cases := []struct {
		role, action string
		prot         bool
		want         bool
	}{
		{"viewer", Read, false, true},
		{"viewer", Reveal, false, false},
		{"viewer", Write, false, false},
		{"viewer", Admin, false, false},
		{"viewer", Members, false, false},
		{"viewer", AuditRead, false, false},
		{"developer", Read, true, true},
		{"developer", Reveal, false, true},
		{"developer", Write, false, true},
		{"developer", Reveal, true, false},
		{"developer", Write, true, false},
		{"developer", Admin, false, false},
		{"admin", Reveal, true, true},
		{"admin", Write, true, true},
		{"admin", Admin, false, true},
		{"admin", Members, false, false},
		{"admin", AuditExport, false, true},
		{"owner", Members, false, true},
		{"owner", Admin, false, true},
		{"owner", Manage, true, true},
	}
	for _, c := range cases {
		if got := UserAllows(c.role, c.action, c.prot); got != c.want {
			t.Fatalf("%s %s prot=%v: got %v want %v", c.role, c.action, c.prot, got, c.want)
		}
	}
}

func TestEffectiveRole(t *testing.T) {
	if r, ok := EffectiveRole("developer", true, "", false); ok || r != "" {
		t.Fatal("restricted without grant must deny")
	}
	if r, ok := EffectiveRole("developer", true, "viewer", true); !ok || r != "viewer" {
		t.Fatalf("grant override: %s %v", r, ok)
	}
	if r, ok := EffectiveRole("owner", true, "viewer", true); !ok || r != "owner" {
		t.Fatalf("owner ignores restrict: %s %v", r, ok)
	}
}

func TestIdentityAllows(t *testing.T) {
	if IdentityAllows(Read, []string{"read"}) != true {
		t.Fatal("read scope can read")
	}
	if IdentityAllows(Reveal, []string{"read"}) != false {
		t.Fatal("read scope cannot reveal")
	}
	if IdentityAllows(Reveal, []string{"reveal"}) != true {
		t.Fatal("reveal scope can reveal")
	}
	if IdentityAllows(Write, []string{"reveal"}) != false {
		t.Fatal("reveal cannot write")
	}
	if IdentityAllows(Write, []string{"write"}) != true || IdentityAllows(Reveal, []string{"write"}) != true {
		t.Fatal("write ⊃ reveal")
	}
	if IdentityAllows(Admin, []string{"write"}) {
		t.Fatal("identity has no admin")
	}
}
