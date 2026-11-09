package oidc

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/apperr"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/auth"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/rbac"
)

type SecurityPolicy struct {
	Profile        string   `json:"profile"`
	SessionTTL     int      `json:"session_ttl"`
	MaxAuthAge     int      `json:"max_auth_age"`
	RequiredACR    []string `json:"required_acr"`
	RequiredAMR    []string `json:"required_amr"`
	OnlineAudience string   `json:"online_audience"`
	RPLogout       bool     `json:"rp_logout"`
}

func (s *Service) policy(id string) (SecurityPolicy, error) {
	p := SecurityPolicy{Profile: s.SessionProfile, SessionTTL: 900, MaxAuthAge: 900, RequiredACR: []string{}, RequiredAMR: []string{}}
	var raw string
	err := s.DB.QueryRow(`SELECT configuration FROM provider_security WHERE provider_id=?`, id).Scan(&raw)
	if err == sql.ErrNoRows {
		return p, nil
	}
	if err != nil {
		return p, err
	}
	err = json.Unmarshal([]byte(raw), &p)
	return p, err
}

func (s *Service) Security(w http.ResponseWriter, r *http.Request) {
	org, slug, ok := s.orgOf(w, r)
	if !ok || !auth.Require(s.DB, w, r, org, "", rbac.Members, "") {
		return
	}
	p, err := s.providerByOrgSlug(slug, chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, apperr.NotFound)
		return
	}
	policy, err := s.policy(p.ID)
	if err != nil {
		writeErr(w, apperr.New(503, "STORAGE_UNAVAILABLE", "policy unavailable"))
		return
	}
	writeJSON(w, 200, policy)
}

func (s *Service) PutSecurity(w http.ResponseWriter, r *http.Request) {
	org, slug, ok := s.orgOf(w, r)
	if !ok || s.personalBlocked(w, org) || !auth.Require(s.DB, w, r, org, "", rbac.Members, "") {
		return
	}
	p, err := s.providerByOrgSlug(slug, chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, apperr.NotFound)
		return
	}
	var policy SecurityPolicy
	if json.NewDecoder(r.Body).Decode(&policy) != nil || (policy.Profile != "BOUNDED" && policy.Profile != "ONLINE_REQUIRED") || policy.SessionTTL < 60 || policy.SessionTTL > 900 || policy.MaxAuthAge < 60 || policy.MaxAuthAge > 900 || len(policy.RequiredACR) > 16 || len(policy.RequiredAMR) > 16 || len(policy.OnlineAudience) > 1024 {
		writeErr(w, apperr.InvalidInput)
		return
	}
	for _, values := range [][]string{policy.RequiredACR, policy.RequiredAMR} {
		for _, v := range values {
			if v == "" || len(v) > 200 || strings.ContainsAny(v, " \t\r\n") {
				writeErr(w, apperr.InvalidInput)
				return
			}
		}
	}
	doc, _, err := discoCache.getContext(r.Context(), p.ID, p.Issuer)
	incompatible := err != nil
	if !incompatible {
		if policy.Profile == "ONLINE_REQUIRED" && (!doc.SupportsOnline() || policy.OnlineAudience == "") {
			incompatible = true
		}
		if policy.RPLogout && doc.EndSessionEndpoint == "" {
			incompatible = true
		}
		for _, claim := range []struct {
			required bool
			name     string
		}{{len(policy.RequiredACR) > 0, "acr"}, {len(policy.RequiredAMR) > 0, "amr"}} {
			if claim.required && !includes(doc.ClaimsSupported, claim.name) {
				incompatible = true
			}
		}
		if len(doc.ACRValues) > 0 {
			for _, v := range policy.RequiredACR {
				if !includes(doc.ACRValues, v) {
					incompatible = true
				}
			}
		}
	}
	if incompatible {
		writeErr(w, apperr.New(400, "INCOMPATIBLE_SECURITY_PROFILE", "provider does not declare the required public capabilities"))
		return
	}
	raw, _ := json.Marshal(policy)
	tx, err := s.DB.BeginTx(r.Context(), nil)
	if err != nil {
		writeErr(w, apperr.New(503, "STORAGE_UNAVAILABLE", "try again"))
		return
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`INSERT INTO provider_security(provider_id,org_id,configuration) VALUES(?,?,?) ON CONFLICT(provider_id) DO UPDATE SET configuration=excluded.configuration`, p.ID, org, string(raw)); err != nil {
		writeErr(w, apperr.New(503, "STORAGE_UNAVAILABLE", "try again"))
		return
	}
	if err = auth.AuditReq(tx, r, org, auth.From(r), "oidc.security", "provider/"+p.ID, map[string]any{"policy": policy}); err != nil {
		writeErr(w, apperr.New(503, "AUDIT_UNAVAILABLE", "policy unchanged"))
		return
	}
	if tx.Commit() != nil {
		writeErr(w, apperr.New(503, "STORAGE_UNAVAILABLE", "try again"))
		return
	}
	writeJSON(w, 200, policy)
}

func includes(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}
