package oidc

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/apperr"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/auth"
	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/totp"
)

// Sealed with the PKCE verifier and consumed by the same one-use browser-bound
// authorization request. Neither email nor an administrator-supplied sub proves
// ownership. Replacement is explicit and preserves the local user and resource IDs.
type replacementProof struct {
	Verifier, User, Organization, BrowserHash, LocalRevision string
	AuthUntil                                                int64
}

func localRevision(db interface{ QueryRow(string, ...any) *sql.Row }, user string) (string, error) {
	var hash, source, status, secret string
	var enabled int
	err := db.QueryRow(`SELECT password_hash,auth_source,status,COALESCE(totp_secret_enc,''),totp_enabled FROM users WHERE id=?`, user).Scan(&hash, &source, &status, &secret, &enabled)
	if err != nil || source != "local" || status != "active" {
		return "", errors.New("local account changed")
	}
	encoded, _ := json.Marshal([]any{hash, secret, enabled})
	return tc.SHA256(string(encoded)), nil
}

func (s *Service) Replace(w http.ResponseWriter, r *http.Request) {
	origin, err := s.origin()
	a := auth.From(r)
	if err != nil || r.Header.Get("Origin") != origin || a == nil || a.Kind != auth.KindUser {
		writeErr(w, apperr.Forbidden)
		return
	}
	if r.URL.Query().Get("pid") == "" {
		writeErr(w, apperr.InvalidInput)
		return
	}
	p, err := s.providerByOrgSlug(chi.URLParam(r, "slug"), r.URL.Query().Get("pid"))
	if err != nil || !s.allowedIssuer(p.Issuer) {
		writeErr(w, apperr.NotFound)
		return
	}
	if _, ok := auth.Membership(s.DB, p.OrgID, a.ID); !ok || !auth.OrganizationLoginAllowed(s.DB, a, p.OrgID) || a.OrganizationID != "" && a.OrganizationID != p.OrgID {
		writeErr(w, apperr.Forbidden)
		return
	}
	proof := &replacementProof{User: a.ID, Organization: p.OrgID}
	if a.BrowserOrigin != "" {
		if !a.SensitiveAllowed || a.AuthenticationSource == p.ID {
			writeErr(w, apperr.Forbidden)
			return
		}
		proof.BrowserHash = a.SessionHash
		proof.AuthUntil = a.AuthValidUntil
	} else {
		// A bearer session alone is not sufficient to attach another sign-in method.
		if s.DisablePassword {
			writeErr(w, apperr.Forbidden)
			return
		}
		var b struct {
			Password string `json:"password"`
			Code     string `json:"code"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&b) != nil {
			writeErr(w, apperr.InvalidInput)
			return
		}
		var hash, secret, source, status string
		var enabled int
		var last int64
		err = s.DB.QueryRow(`SELECT password_hash,auth_source,status,COALESCE(totp_secret_enc,''),totp_enabled,totp_last_step FROM users WHERE id=?`, a.ID).Scan(&hash, &source, &status, &secret, &enabled, &last)
		valid, _, _ := tc.CheckPassword(b.Password, hash)
		if err != nil || !valid || source != "local" || status != "active" {
			writeErr(w, apperr.Unauthorized)
			return
		}
		if enabled == 1 {
			plain, e := tc.Open(s.MasterKey, secret, tc.Context{Purpose: "user-totp", Object: a.ID, Version: 1})
			if e != nil {
				writeErr(w, apperr.Unauthorized)
				return
			}
			step, ok := totp.Verify(plain, b.Code, last)
			if !ok {
				writeErr(w, apperr.Unauthorized)
				return
			}
			result, e := s.DB.Exec(`UPDATE users SET totp_last_step=? WHERE id=? AND totp_last_step=? AND totp_secret_enc=? AND totp_enabled=1`, step, a.ID, last, secret)
			if e != nil {
				writeErr(w, apperr.Unauthorized)
				return
			}
			count, _ := result.RowsAffected()
			if count != 1 {
				writeErr(w, apperr.Unauthorized)
				return
			}
		}
		encoded, _ := json.Marshal([]any{hash, secret, enabled})
		proof.LocalRevision = tc.SHA256(string(encoded))
		current, e := localRevision(s.DB, a.ID)
		if e != nil || current != proof.LocalRevision {
			writeErr(w, apperr.Unauthorized)
			return
		}
	}
	s.startLogin(w, r, proof)
}

func (s *Service) validateReplacement(r *http.Request, p *provider, proof *replacementProof) error {
	if proof.Organization != p.OrgID || (proof.BrowserHash != "" && time.Now().Unix() > proof.AuthUntil) {
		return apperr.Forbidden
	}
	if proof.BrowserHash != "" {
		a, err := auth.BrowserActor(s.DB, r)
		if err != nil || a.ID != proof.User || a.SessionHash != proof.BrowserHash || a.OrganizationID != p.OrgID {
			return apperr.Unauthorized
		}
		if err = s.CheckSession(r, a); err != nil {
			return err
		}
		if !a.SensitiveAllowed {
			return apperr.Forbidden
		}
	} else {
		current, err := localRevision(s.DB, proof.User)
		if err != nil || current != proof.LocalRevision {
			return apperr.Unauthorized
		}
	}
	return nil
}

func (s *Service) replaceIdentity(tx *sql.Tx, r *http.Request, p *provider, proof *replacementProof, subject string) (string, error) {
	// Serialize concurrent account replacement. Recheck membership and both local
	// proofs inside the mutation transaction, not only before network exchange.
	if proof.BrowserHash != "" && time.Now().Unix() > proof.AuthUntil {
		return "", apperr.Unauthorized
	}
	rows, e := tx.Query(`SELECT org_id FROM org_members WHERE user_id=? AND role='owner' ORDER BY org_id`, proof.User)
	if e != nil {
		return "", e
	}
	organizations := []string{}
	for rows.Next() {
		var org string
		if e = rows.Scan(&org); e != nil {
			rows.Close()
			return "", e
		}
		organizations = append(organizations, org)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return "", e
	}
	for _, org := range organizations {
		if _, e = tx.Exec(`UPDATE orgs SET name=name WHERE id=?`, org); e != nil {
			return "", e
		}
	}
	result, err := tx.Exec(`UPDATE users SET name=name WHERE id=? AND status='active'`, proof.User)
	if err != nil {
		return "", err
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return "", apperr.Unauthorized
	}
	var role string
	if err = tx.QueryRow(`SELECT role FROM org_members WHERE org_id=? AND user_id=?`, p.OrgID, proof.User).Scan(&role); err != nil {
		return "", err
	}
	if proof.BrowserHash != "" {
		var revision, pid string
		err = tx.QueryRow(`SELECT config_hash,provider_id FROM browser_sessions WHERE token_hash=? AND user_id=? AND org_id=? AND expires_at>?`, proof.BrowserHash, proof.User, p.OrgID, time.Now().Unix()).Scan(&revision, &pid)
		if err != nil {
			return "", err
		}
		current, e := auth.ProviderRevision(tx, pid)
		if e != nil || current != revision {
			return "", apperr.Unauthorized
		}
	} else {
		var loginPolicy string
		e := tx.QueryRow(`SELECT login_policy FROM org_security WHERE org_id=?`, p.OrgID).Scan(&loginPolicy)
		if e != nil && e != sql.ErrNoRows || loginPolicy == "EXTERNAL_REQUIRED" {
			return "", apperr.Forbidden
		}
		current, e := localRevision(tx, proof.User)
		if e != nil || current != proof.LocalRevision {
			return "", apperr.Unauthorized
		}
	}
	var existing string
	err = tx.QueryRow(`SELECT user_id FROM external_identities WHERE issuer=? AND subject=?`, p.Issuer, subject).Scan(&existing)
	if err == nil && existing != proof.User {
		return "", apperr.New(409, "IDENTITY_ALREADY_BOUND", "new identity already belongs to another account")
	}
	if err != nil && err != sql.ErrNoRows {
		return "", err
	}
	if _, err = tx.Exec(`DELETE FROM external_identities WHERE user_id=?`, proof.User); err != nil {
		return "", err
	}
	if _, err = tx.Exec(`INSERT INTO external_identities(issuer,subject,user_id) VALUES(?,?,?)`, p.Issuer, subject, proof.User); err != nil {
		return "", err
	}
	if _, err = tx.Exec(`UPDATE users SET auth_source=?,external_sub=?,password_hash='!',totp_enabled=0,totp_secret_enc=NULL,totp_pending_enc=NULL WHERE id=?`, p.ID, subject, proof.User); err != nil {
		return "", err
	}
	if _, err = tx.Exec(`DELETE FROM recovery_codes WHERE user_id=?`, proof.User); err != nil {
		return "", err
	}
	if _, err = tx.Exec(`DELETE FROM refresh_tokens WHERE user_id=?`, proof.User); err != nil {
		return "", err
	}
	if _, err = tx.Exec(`DELETE FROM browser_sessions WHERE user_id=?`, proof.User); err != nil {
		return "", err
	}
	if _, err = tx.Exec(`INSERT INTO federation_admissions(org_id,provider_id,user_id,last_role) VALUES(?,?,?,'') ON CONFLICT(org_id,user_id) DO UPDATE SET provider_id=excluded.provider_id,last_role=''`, p.OrgID, p.ID, proof.User); err != nil {
		return "", err
	}
	for _, org := range organizations {
		if !auth.ExternalOwnerAvailable(tx, org) {
			return "", apperr.New(409, "EXTERNAL_OWNER_REQUIRED", "verify another external owner in each affected organization before replacing this identity")
		}
	}
	actor := &auth.Actor{ID: proof.User, Kind: auth.KindUser, AuthenticationSource: p.ID}
	err = auth.AuditReq(tx, r, p.OrgID, actor, "oidc.identity.replace", "user/"+proof.User, map[string]any{"provider_id": p.ID})
	return proof.User, err
}

// Connections exposes only the sign-in choices registered for a current member.
// It does not expose provider credentials or grant membership in another org.
func (s *Service) Connections(w http.ResponseWriter, r *http.Request) {
	a := auth.From(r)
	if a == nil || a.Kind != auth.KindUser {
		writeErr(w, apperr.Forbidden)
		return
	}
	var org string
	if err := s.DB.QueryRow(`SELECT id FROM orgs WHERE slug=?`, chi.URLParam(r, "slug")).Scan(&org); err != nil {
		writeErr(w, apperr.NotFound)
		return
	}
	if _, ok := auth.Membership(s.DB, org, a.ID); !ok || a.OrganizationID != "" && a.OrganizationID != org || !auth.OrganizationLoginAllowed(s.DB, a, org) {
		writeErr(w, apperr.Forbidden)
		return
	}
	rows, err := s.DB.Query(`SELECT id,name,issuer FROM oidc_providers WHERE org_id=? AND enabled=1 ORDER BY name,id`, org)
	if err != nil {
		writeErr(w, apperr.New(503, "STORAGE_UNAVAILABLE", "try again"))
		return
	}
	defer rows.Close()
	type choice struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Issuer  string `json:"issuer"`
		Current bool   `json:"current"`
	}
	choices := []choice{}
	for rows.Next() {
		var c choice
		if rows.Scan(&c.ID, &c.Name, &c.Issuer) != nil {
			writeErr(w, apperr.New(503, "STORAGE_UNAVAILABLE", "try again"))
			return
		}
		if !s.allowedIssuer(c.Issuer) {
			continue
		}
		c.Current = c.ID == a.AuthenticationSource
		choices = append(choices, c)
	}
	if rows.Err() != nil {
		writeErr(w, apperr.New(503, "STORAGE_UNAVAILABLE", "try again"))
		return
	}
	writeJSON(w, 200, map[string]any{"connections": choices, "authentication": map[bool]string{true: "oidc", false: "local"}[a.BrowserOrigin != ""]})
}
