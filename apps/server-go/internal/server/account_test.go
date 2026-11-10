package server

import (
	"encoding/json"
	"testing"

	orgsvc "github.com/zhouyunchang/taboo/apps/server-go/internal/org"
)

func TestPersonalSpaceAndCreatedOrganization(t *testing.T) {
	f := fixture(t, "")
	const email, password = "person@example.test", "person-pass-123"
	created, err := orgsvc.AdminCreateUser(f.db, f.master, email, "Person", password, "active")
	if err != nil {
		t.Fatal(err)
	}
	var kind, owner, name string
	if err = f.db.QueryRow(`SELECT kind, owner_user_id, name FROM orgs WHERE id=?`, created.OrgID).Scan(&kind, &owner, &name); err != nil {
		t.Fatal(err)
	}
	if kind != "personal" || owner != created.UserID || name != "Person" {
		t.Fatalf("personal space %s %s %s", kind, owner, name)
	}
	login := f.plain("POST", "/api/v1/auth/login", `{"email":"`+email+`","password":"`+password+`"}`, "")
	checkStatus(t, login, 200)
	var session struct {
		Tokens struct{ Access string } `json:"tokens"`
	}
	if json.Unmarshal(login.Body.Bytes(), &session) != nil || session.Tokens.Access == "" {
		t.Fatal(login.Body.String())
	}
	token := session.Tokens.Access
	me := f.plain("GET", "/api/v1/me", "", token)
	checkStatus(t, me, 200)
	var body struct {
		Orgs []struct {
			ID, Name, Slug, Kind string
		} `json:"orgs"`
	}
	if json.Unmarshal(me.Body.Bytes(), &body) != nil || len(body.Orgs) != 1 || body.Orgs[0].Kind != "personal" || body.Orgs[0].Slug != created.Slug {
		t.Fatalf("me %s", me.Body.String())
	}
	checkStatus(t, f.plain("POST", "/api/v1/orgs/"+created.Slug+"/members", `{"email":"peer@example.test","role":"developer"}`, token), 409)
	checkStatus(t, f.plain("POST", "/api/v1/orgs/"+created.Slug+"/invites", `{"email":"peer@example.test","role":"developer"}`, token), 409)
	checkStatus(t, f.plain("PUT", "/api/v1/orgs/"+created.Slug+"/security", `{"login_policy":"EXTERNAL_REQUIRED"}`, token), 409)

	var profile struct {
		User struct {
			Handle          string `json:"handle"`
			HandleConfirmed bool   `json:"handle_confirmed"`
		} `json:"user"`
	}
	if json.Unmarshal(me.Body.Bytes(), &profile) != nil || profile.User.Handle != "person" || profile.User.HandleConfirmed {
		t.Fatalf("generated handle: %s", me.Body.String())
	}
	checkStatus(t, f.plain("POST", "/api/v1/orgs", `{"name":"Taken","code":"person"}`, token), 409)
	renamed := f.plain("PATCH", "/api/v1/me/handle", `{"handle":"person-renamed"}`, token)
	checkStatus(t, renamed, 200)
	confirmed := f.plain("POST", "/api/v1/me/handle/confirm", ``, token)
	checkStatus(t, confirmed, 200)
	checkStatus(t, f.plain("PATCH", "/api/v1/me/handle", `{"handle":"person-again"}`, token), 409)

	made := f.plain("POST", "/api/v1/orgs", `{"name":"Acme","code":"acme"}`, token)
	checkStatus(t, made, 201)
	var org struct {
		ID, Slug, Kind, Role string
	}
	if json.Unmarshal(made.Body.Bytes(), &org) != nil || org.Kind != "organization" || org.Role != "owner" || org.Slug == "" {
		t.Fatalf("create org %s", made.Body.String())
	}
	if _, err = orgsvc.AdminCreateUser(f.db, f.master, "peer@example.test", "Peer", "peer-pass-123", "active"); err != nil {
		t.Fatal(err)
	}
	added := f.plain("POST", "/api/v1/orgs/"+org.Slug+"/members", `{"email":"peer@example.test","role":"developer"}`, token)
	checkStatus(t, added, 201)

	me = f.plain("GET", "/api/v1/me", "", token)
	checkStatus(t, me, 200)
	body.Orgs = nil
	if json.Unmarshal(me.Body.Bytes(), &body) != nil || len(body.Orgs) != 2 || body.Orgs[0].Kind != "organization" || body.Orgs[1].Kind != "personal" {
		t.Fatalf("default space should be the organization: %s", me.Body.String())
	}
}
