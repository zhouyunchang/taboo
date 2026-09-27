package workload

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/apperr"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/auth"
	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/federation/oauth"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/rbac"
)

type Trust struct {
	ID                string `json:"id"`
	OrgID             string `json:"-"`
	Name              string `json:"name"`
	Issuer            string `json:"issuer"`
	Audience          string `json:"audience"`
	MetadataMode      string `json:"metadata_mode"`
	TokenProfile      string `json:"token_profile"`
	ClientID          string `json:"client_id"`
	SecretEnc         string `json:"-"`
	MaxLifetime       int    `json:"max_lifetime"`
	Enabled           bool   `json:"enabled"`
	RevocationProfile string `json:"revocation_profile"`
}
type cached struct {
	p     *oauth.Provider
	until time.Time
}
type cache struct {
	mu      sync.Mutex
	entries map[string]cached
}
type Service struct {
	DB        *sql.DB
	MasterKey []byte
	Allowlist []string
	cache     *cache
}

func New(db *sql.DB, key []byte, allow []string) *Service {
	return &Service{DB: db, MasterKey: key, Allowlist: allow, cache: &cache{entries: map[string]cached{}}}
}
func (s *Service) WithDB(db *sql.DB) *Service {
	return &Service{DB: db, MasterKey: s.MasterKey, Allowlist: s.Allowlist, cache: s.cache}
}
func (s *Service) allowed(issuer string) bool {
	for _, x := range s.Allowlist {
		if x == issuer {
			return true
		}
	}
	return false
}

const trustColumns = `id,org_id,name,issuer,audience,metadata_mode,token_profile,client_id,client_secret_enc,max_lifetime,enabled`

func scanTrust(row interface{ Scan(...any) error }) (Trust, error) {
	var t Trust
	var enabled int
	err := row.Scan(&t.ID, &t.OrgID, &t.Name, &t.Issuer, &t.Audience, &t.MetadataMode, &t.TokenProfile, &t.ClientID, &t.SecretEnc, &t.MaxLifetime, &enabled)
	t.Enabled = enabled == 1
	t.RevocationProfile = "BOUNDED"
	if t.TokenProfile == oauth.Introspection {
		t.RevocationProfile = "ONLINE_REQUIRED"
	}
	return t, err
}
func (s *Service) discover(r *http.Request, t Trust) (*oauth.Provider, error) {
	key := tc.SHA256(t.Issuer + "\x00" + t.MetadataMode)
	s.cache.mu.Lock()
	entry, ok := s.cache.entries[key]
	s.cache.mu.Unlock()
	if ok && time.Now().Before(entry.until) {
		return entry.p, nil
	}
	p, err := oauth.Discover(r.Context(), t.Issuer, t.MetadataMode)
	if err != nil {
		return nil, err
	}
	s.cache.mu.Lock()
	if len(s.cache.entries) >= 128 {
		clear(s.cache.entries)
	}
	s.cache.entries[key] = cached{p, time.Now().Add(5 * time.Minute)}
	s.cache.mu.Unlock()
	return p, nil
}

// Authentication is selected explicitly by trust and organization headers. A
// failed external token is never retried as a local token or browser session.
func (s *Service) Authenticate(r *http.Request) (*auth.Actor, error) {
	org := r.Header.Get("X-Taboo-Organization")
	trustID := r.Header.Get("X-Taboo-Trust")
	h := r.Header.Get("Authorization")
	if org == "" || trustID == "" || !strings.HasPrefix(h, "Bearer ") {
		return nil, apperr.Unauthorized
	}
	raw := strings.TrimPrefix(h, "Bearer ")
	if raw == "" || len(raw) > 16384 {
		return nil, apperr.Unauthorized
	}
	var orgID string
	if err := s.DB.QueryRow(`SELECT id FROM orgs WHERE slug=?`, org).Scan(&orgID); err != nil {
		return nil, apperr.Unauthorized
	}
	t, err := scanTrust(s.DB.QueryRow(`SELECT `+trustColumns+` FROM oauth_trusts WHERE id=? AND org_id=? AND enabled=1`, trustID, orgID))
	if err != nil || !s.allowed(t.Issuer) {
		return nil, apperr.Unauthorized
	}
	p, err := s.discover(r, t)
	if err != nil {
		return nil, apperr.New(503, "AUTHORIZATION_SERVER_UNAVAILABLE", "resource token validation unavailable")
	}
	var token oauth.ResourceToken
	switch t.TokenProfile {
	case oauth.JWT:
		token, err = p.VerifyJWT(r.Context(), raw, t.Audience, time.Duration(t.MaxLifetime)*time.Second)
	case oauth.Introspection:
		var secret string
		secret, err = tc.Open(s.MasterKey, t.SecretEnc, tc.Context{Purpose: "oauth-introspection", Org: t.OrgID, Object: t.ID, Version: 1})
		if err == nil {
			token, err = p.Introspect(r.Context(), raw, t.Audience, t.ClientID, secret)
		}
	default:
		return nil, apperr.Unauthorized
	}
	if err == oauth.ErrUnavailable {
		return nil, apperr.New(503, "AUTHORIZATION_SERVER_UNAVAILABLE", "resource token validation unavailable")
	}
	if err != nil {
		return nil, apperr.Unauthorized
	}
	var a auth.Actor
	if err = s.DB.QueryRow(`SELECT b.id,m.name FROM workload_bindings b JOIN machine_identities m ON m.id=b.id AND m.org_id=b.org_id WHERE b.trust_id=? AND b.org_id=? AND b.subject=? AND b.client_id=? AND b.enabled=1 AND m.status='active' AND m.auth_type='external_oauth'`, t.ID, t.OrgID, token.Subject, token.ClientID).Scan(&a.ID, &a.Name); err != nil {
		return nil, apperr.Unauthorized
	}
	a.Kind = auth.KindIdentity
	a.WorkloadBinding = a.ID
	a.OrganizationID = t.OrgID
	a.AuthenticationSource = "oauth:" + t.ID
	a.OAuthScopes = strings.Split(token.Scope, " ")
	return &a, nil
}

func reply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, status int, code, message string) {
	reply(w, status, apperr.New(status, code, message))
}
func (s *Service) owner(w http.ResponseWriter, r *http.Request) (string, bool) {
	var org string
	if err := s.DB.QueryRow(`SELECT id FROM orgs WHERE slug=?`, chi.URLParam(r, "slug")).Scan(&org); err != nil {
		fail(w, 404, "NOT_FOUND", "organization not found")
		return "", false
	}
	a := auth.From(r)
	if a == nil || a.Kind != auth.KindUser {
		fail(w, 403, "FORBIDDEN", "organization owner required")
		return "", false
	}
	if !auth.Require(s.DB, w, r, org, "", rbac.Members, "") {
		return "", false
	}
	return org, true
}
func (s *Service) Routes(r chi.Router) {
	r.Route("/orgs/{slug}/oauth-trusts", func(r chi.Router) {
		r.Get("/", s.ListTrusts)
		r.Post("/", s.CreateTrust)
		r.Patch("/{id}", s.UpdateTrust)
		r.Delete("/{id}", s.DeleteTrust)
	})
	r.Route("/orgs/{slug}/workloads", func(r chi.Router) {
		r.Get("/", s.ListBindings)
		r.Post("/", s.CreateBinding)
		r.Put("/{id}/grants", s.PutGrants)
		r.Post("/{id}/revoke", s.RevokeBinding)
	})
}
func (s *Service) ListTrusts(w http.ResponseWriter, r *http.Request) {
	org, ok := s.owner(w, r)
	if !ok {
		return
	}
	rows, err := s.DB.Query(`SELECT `+trustColumns+` FROM oauth_trusts WHERE org_id=? ORDER BY name,id`, org)
	if err != nil {
		fail(w, 500, "STORAGE_ERROR", "trust list unavailable")
		return
	}
	defer rows.Close()
	result := []Trust{}
	for rows.Next() {
		t, err := scanTrust(rows)
		if err != nil {
			fail(w, 500, "STORAGE_ERROR", "trust list unavailable")
			return
		}
		result = append(result, t)
	}
	if rows.Err() != nil {
		fail(w, 500, "STORAGE_ERROR", "trust list unavailable")
		return
	}
	reply(w, 200, map[string]any{"trusts": result})
}

type trustInput struct {
	Name, Issuer, Audience string
	MetadataMode           string `json:"metadata_mode"`
	TokenProfile           string `json:"token_profile"`
	ClientID               string `json:"client_id"`
	ClientSecret           string `json:"client_secret"`
	MaxLifetime            int    `json:"max_lifetime"`
	Enabled                *bool  `json:"enabled"`
}

func (s *Service) validateTrust(w http.ResponseWriter, r *http.Request, t *Trust, b trustInput) bool {
	if b.Name != "" {
		t.Name = b.Name
	}
	if b.Issuer != "" {
		t.Issuer = b.Issuer
	}
	if b.Audience != "" {
		t.Audience = b.Audience
	}
	if b.MetadataMode != "" {
		t.MetadataMode = b.MetadataMode
	}
	if b.TokenProfile != "" {
		t.TokenProfile = b.TokenProfile
	}
	if b.ClientID != "" {
		t.ClientID = b.ClientID
	}
	if b.MaxLifetime != 0 {
		t.MaxLifetime = b.MaxLifetime
	}
	if b.Enabled != nil {
		t.Enabled = *b.Enabled
	}
	if t.Name == "" || len(t.Name) > 200 || t.Audience == "" || len(t.Audience) > 1024 || t.MaxLifetime < 60 || t.MaxLifetime > 900 || !s.allowed(t.Issuer) {
		fail(w, 400, "INVALID_TRUST", "name, allowed issuer, resource audience and lifetime between 60 and 900 seconds are required")
		return false
	}
	p, err := s.discover(r, *t)
	if err != nil || !p.Supports(t.TokenProfile) {
		fail(w, 400, "INCOMPATIBLE_PROVIDER", "the selected public protocol profile is unavailable")
		return false
	}
	if b.ClientSecret != "" {
		t.SecretEnc, err = tc.Seal(s.MasterKey, b.ClientSecret, tc.Context{Purpose: "oauth-introspection", Org: t.OrgID, Object: t.ID, Version: 1})
		if err != nil {
			fail(w, 500, "ENCRYPTION_FAILED", "credential encryption failed")
			return false
		}
	}
	if t.TokenProfile == oauth.Introspection && (t.ClientID == "" || t.SecretEnc == "") {
		fail(w, 400, "INTROSPECTION_CREDENTIAL_REQUIRED", "introspection requires a registered resource server credential")
		return false
	}
	return true
}
func (s *Service) CreateTrust(w http.ResponseWriter, r *http.Request) {
	org, ok := s.owner(w, r)
	if !ok {
		return
	}
	var b trustInput
	if json.NewDecoder(r.Body).Decode(&b) != nil {
		fail(w, 400, "INVALID_INPUT", "invalid trust")
		return
	}
	t := Trust{ID: tc.NewID(), OrgID: org, MetadataMode: "oauth", TokenProfile: oauth.JWT, MaxLifetime: 900, Enabled: true}
	if !s.validateTrust(w, r, &t, b) {
		return
	}
	tx, err := s.DB.Begin()
	if err != nil {
		fail(w, 500, "STORAGE_ERROR", "trust unavailable")
		return
	}
	defer tx.Rollback()
	enabled := 0
	if t.Enabled {
		enabled = 1
	}
	if _, err = tx.Exec(`INSERT INTO oauth_trusts(`+trustColumns+`) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, t.ID, org, t.Name, t.Issuer, t.Audience, t.MetadataMode, t.TokenProfile, t.ClientID, t.SecretEnc, t.MaxLifetime, enabled); err != nil {
		fail(w, 500, "STORAGE_ERROR", "trust could not be saved")
		return
	}
	if err = auth.AuditReq(tx, r, org, auth.From(r), "workload.trust.create", "oauth-trust/"+t.ID, map[string]any{"issuer": t.Issuer, "profile": t.TokenProfile}); err != nil {
		fail(w, 503, "AUDIT_UNAVAILABLE", "trust change could not be recorded")
		return
	}
	if tx.Commit() != nil {
		fail(w, 503, "STORAGE_ERROR", "trust could not be saved")
		return
	}
	reply(w, 201, map[string]any{"id": t.ID})
}
func (s *Service) UpdateTrust(w http.ResponseWriter, r *http.Request) {
	org, ok := s.owner(w, r)
	if !ok {
		return
	}
	t, err := scanTrust(s.DB.QueryRow(`SELECT `+trustColumns+` FROM oauth_trusts WHERE id=? AND org_id=?`, chi.URLParam(r, "id"), org))
	if err != nil {
		fail(w, 404, "NOT_FOUND", "trust not found")
		return
	}
	var b trustInput
	if json.NewDecoder(r.Body).Decode(&b) != nil {
		fail(w, 400, "INVALID_INPUT", "invalid trust")
		return
	}
	if b.Issuer != "" && b.Issuer != t.Issuer {
		fail(w, 409, "NEW_TRUST_REQUIRED", "register a new issuer and explicitly bind its workload identities")
		return
	}
	// A disabled trust can be revoked during an upstream outage. Other edits need
	// fresh capability validation; retained discovery cache is bounded to 5 minutes.
	onlyDisable := b.Enabled != nil && !*b.Enabled && b.Name == "" && b.Issuer == "" && b.Audience == "" && b.MetadataMode == "" && b.TokenProfile == "" && b.ClientID == "" && b.ClientSecret == "" && b.MaxLifetime == 0
	if onlyDisable {
		t.Enabled = false
	} else if !s.validateTrust(w, r, &t, b) {
		return
	}
	tx, err := s.DB.Begin()
	if err != nil {
		fail(w, 500, "STORAGE_ERROR", "trust unavailable")
		return
	}
	defer tx.Rollback()
	enabled := 0
	if t.Enabled {
		enabled = 1
	}
	if _, err = tx.Exec(`UPDATE oauth_trusts SET name=?,issuer=?,audience=?,metadata_mode=?,token_profile=?,client_id=?,client_secret_enc=?,max_lifetime=?,enabled=? WHERE id=? AND org_id=?`, t.Name, t.Issuer, t.Audience, t.MetadataMode, t.TokenProfile, t.ClientID, t.SecretEnc, t.MaxLifetime, enabled, t.ID, org); err != nil {
		fail(w, 500, "STORAGE_ERROR", "trust could not be saved")
		return
	}
	if err = auth.AuditReq(tx, r, org, auth.From(r), "workload.trust.update", "oauth-trust/"+t.ID, map[string]any{"enabled": t.Enabled, "profile": t.TokenProfile}); err != nil {
		fail(w, 503, "AUDIT_UNAVAILABLE", "trust change could not be recorded")
		return
	}
	if tx.Commit() != nil {
		fail(w, 503, "STORAGE_ERROR", "trust could not be saved")
		return
	}
	reply(w, 200, map[string]any{"id": t.ID})
}
func (s *Service) DeleteTrust(w http.ResponseWriter, r *http.Request) {
	org, ok := s.owner(w, r)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	tx, err := s.DB.Begin()
	if err != nil {
		fail(w, 500, "STORAGE_ERROR", "trust unavailable")
		return
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`UPDATE machine_identities SET status='revoked' WHERE org_id=? AND id IN (SELECT id FROM workload_bindings WHERE trust_id=? AND org_id=?)`, org, id, org); err != nil {
		fail(w, 500, "STORAGE_ERROR", "trust could not be revoked")
		return
	}
	res, err := tx.Exec(`DELETE FROM oauth_trusts WHERE id=? AND org_id=?`, id, org)
	if err != nil {
		fail(w, 500, "STORAGE_ERROR", "trust could not be removed")
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		fail(w, 404, "NOT_FOUND", "trust not found")
		return
	}
	if err = auth.AuditReq(tx, r, org, auth.From(r), "workload.trust.delete", "oauth-trust/"+id, nil); err != nil {
		fail(w, 503, "AUDIT_UNAVAILABLE", "trust change could not be recorded")
		return
	}
	if tx.Commit() != nil {
		fail(w, 503, "STORAGE_ERROR", "trust could not be removed")
		return
	}
	reply(w, 200, map[string]any{"deleted": true})
}
