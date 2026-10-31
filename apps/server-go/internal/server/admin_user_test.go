package server

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	orgsvc "github.com/zhouyunchang/taboo/apps/server-go/internal/org"
)

func (f *federationFixture) plain(method, path, body, bearer string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		r.Header.Set("Authorization", "Bearer "+bearer)
	}
	w := httptest.NewRecorder()
	f.h.ServeHTTP(w, r)
	return w
}

func TestHealthzReportsBuildVersion(t *testing.T) {
	f := fixture(t, "")
	w := f.plain("GET", "/api/v1/healthz", "", "")
	checkStatus(t, w, 200)
	var b struct {
		OK      bool   `json:"ok"`
		Version string `json:"version"`
		Commit  string `json:"commit"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &b); err != nil || !b.OK || b.Version == "" || b.Commit == "" {
		t.Fatalf("healthz body %q (%v)", w.Body.String(), err)
	}
}

// 运维停用本地账号后：密码登录、旧 refresh、旧 access 全部失效；重新启用后恢复。
func TestAdminDisabledLocalUserCannotAuthenticate(t *testing.T) {
	f := fixture(t, "")
	const email, pw = "ops-smoke@example.test", "smoke-pass-123"
	if _, err := orgsvc.AdminCreateUser(f.db, f.master, email, "Ops", pw, "active"); err != nil {
		t.Fatal(err)
	}
	login := `{"email":"` + email + `","password":"` + pw + `"}`
	w := f.plain("POST", "/api/v1/auth/login", login, "")
	checkStatus(t, w, 200)
	var tk struct {
		Tokens struct{ Access, Refresh string } `json:"tokens"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &tk); err != nil || tk.Tokens.Access == "" {
		t.Fatalf("login body %q", w.Body.String())
	}
	checkStatus(t, f.plain("GET", "/api/v1/me", "", tk.Tokens.Access), 200)

	if changed, err := orgsvc.AdminSetUserStatus(f.db, email, "disabled"); err != nil || !changed {
		t.Fatalf("disable: %v %v", changed, err)
	}
	if changed, _ := orgsvc.AdminSetUserStatus(f.db, email, "disabled"); changed {
		t.Fatal("disable is not idempotent")
	}
	w = f.plain("POST", "/api/v1/auth/login", login, "")
	checkStatus(t, w, 401)
	if !strings.Contains(w.Body.String(), "BAD_CREDENTIALS") {
		t.Fatalf("disabled login must look like bad credentials: %s", w.Body.String())
	}
	checkStatus(t, f.plain("GET", "/api/v1/me", "", tk.Tokens.Access), 401)
	checkStatus(t, f.plain("POST", "/api/v1/auth/refresh", `{"refresh":"`+tk.Tokens.Refresh+`"}`, ""), 401)
	var n int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE action='admin.user.disable'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("disable audit rows=%d err=%v", n, err)
	}
	if st, _ := orgsvc.AdminUserStatus(f.db, email); st != "disabled" {
		t.Fatalf("status %q", st)
	}

	if _, err := orgsvc.AdminSetUserStatus(f.db, email, "active"); err != nil {
		t.Fatal(err)
	}
	checkStatus(t, f.plain("POST", "/api/v1/auth/login", login, ""), 200)
	if _, err := orgsvc.AdminSetUserStatus(f.db, "nobody@example.test", "disabled"); err != orgsvc.ErrUserNotFound {
		t.Fatalf("missing user: %v", err)
	}
}
