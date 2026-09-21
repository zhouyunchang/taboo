package oidc

import "testing"

func TestGroupAliases(t *testing.T) {
	got := groupAliases("/taboo/admins")
	want := map[string]bool{"/taboo/admins": true, "taboo/admins": true, "admins": true}
	if len(got) != 3 {
		t.Fatalf("aliases=%v", got)
	}
	for _, g := range got {
		if !want[g] {
			t.Fatalf("unexpected alias %q in %v", g, got)
		}
	}
}

func TestMapRoleKeycloakGroups(t *testing.T) {
	claims := map[string]any{
		"groups": []any{"/taboo/admins", "/taboo/devs"},
		"realm_access": map[string]any{
			"roles": []any{"offline_access", "uma_authorization", "taboo-admin"},
		},
	}
	roleMap := map[string]string{"admins": "admin", "devs": "developer", "taboo-admin": "admin"}
	if g := MapRole(claims, "groups", roleMap, "viewer"); g != "admin" {
		t.Fatalf("groups → %s", g)
	}
	if g := MapRole(claims, "realm_access.roles", roleMap, "viewer"); g != "admin" {
		t.Fatalf("realm roles → %s", g)
	}
	if g := MapRole(claims, "groups", nil, "developer"); g != "developer" {
		t.Fatalf("default → %s", g)
	}
}

func TestCapJITRole(t *testing.T) {
	if capJITRole("owner") != "admin" {
		t.Fatal("owner must cap at admin")
	}
	if capJITRole("developer") != "developer" {
		t.Fatal("developer unchanged")
	}
}

func TestOAuthScopes(t *testing.T) {
	if g := oauthScopes("openid,email,profile"); g != "openid email profile" {
		t.Fatalf("got %q", g)
	}
	if g := oauthScopes("openid email profile"); g != "openid email profile" {
		t.Fatalf("got %q", g)
	}
}
