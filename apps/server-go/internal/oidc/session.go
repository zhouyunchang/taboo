package oidc

import (
	"encoding/json"
	"net/http"
	"net/url"
	"time"

	"github.com/zhouyunchang/taboo/apps/server-go/internal/apperr"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/auth"
	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
	federation "github.com/zhouyunchang/taboo/apps/server-go/internal/federation/oidc"
)

type sessionCredentials struct {
	Access, Refresh, IDToken, Nonce, Subject string
	AccessExpires, IdentityExpires, AuthTime int64
	ACR                                      string
	AMR                                      []string
	LogoutEndpoint                           string
}

func claimTime(claims map[string]any, key string) int64 {
	v, ok := claims[key].(float64)
	if !ok || v <= 0 || v != float64(int64(v)) {
		return 0
	}
	return int64(v)
}
func claimAMR(claims map[string]any) []string {
	list, ok := claims["amr"].([]any)
	if !ok {
		return nil
	}
	out := []string{}
	for _, x := range list {
		v, ok := x.(string)
		if !ok {
			return nil
		}
		out = append(out, v)
	}
	return out
}
func sessionContext(org, hash string, revision int) tc.Context {
	return tc.Context{Purpose: "oidc-session", Org: org, Object: hash, Version: revision}
}
func (s *Service) sealSession(org, hash string, revision int, b sessionCredentials) (string, error) {
	raw, err := json.Marshal(b)
	if err != nil {
		return "", err
	}
	return tc.Seal(s.MasterKey, string(raw), sessionContext(org, hash, revision))
}
func (s *Service) openSession(org, hash string, revision int, enc string) (sessionCredentials, error) {
	var b sessionCredentials
	raw, err := tc.Open(s.MasterKey, enc, sessionContext(org, hash, revision))
	if err != nil {
		return b, err
	}
	err = json.Unmarshal([]byte(raw), &b)
	return b, err
}

func (p SecurityPolicy) sensitive(b sessionCredentials) bool {
	now := time.Now().Unix()
	if b.AuthTime <= 0 || b.AuthTime > now+30 || now-b.AuthTime > int64(p.MaxAuthAge) {
		return false
	}
	if len(p.RequiredACR) > 0 && !includes(p.RequiredACR, b.ACR) {
		return false
	}
	for _, amr := range p.RequiredAMR {
		if !includes(b.AMR, amr) {
			return false
		}
	}
	return true
}

func (s *Service) CheckSession(r *http.Request, a *auth.Actor) error {
	origin, err := s.origin()
	if err != nil || a.BrowserOrigin != origin {
		return apperr.Unauthorized
	}
	p, err := s.scanProvider(s.DB.QueryRow(providerSelect+` WHERE o.id=? AND o.org_id=? AND o.enabled=1`, a.AuthenticationSource, a.OrganizationID))
	if err != nil || !s.allowedIssuer(p.Issuer) {
		return apperr.Unauthorized
	}
	policy, err := s.policy(p.ID)
	if err != nil || (policy.Profile != "BOUNDED" && policy.Profile != "ONLINE_REQUIRED") {
		return apperr.Unauthorized
	}
	var enc, state string
	var revision int
	var expires int64
	err = s.DB.QueryRow(`SELECT c.ciphertext,c.revision,c.refresh_state,s.expires_at FROM browser_credentials c JOIN browser_sessions s ON s.token_hash=c.token_hash AND s.org_id=c.org_id WHERE c.token_hash=? AND c.org_id=?`, a.SessionHash, a.OrganizationID).Scan(&enc, &revision, &state, &expires)
	if err != nil {
		return apperr.Unauthorized
	}
	if state != "" {
		return apperr.New(503, "SESSION_REFRESH_PENDING", "sign in again if session refresh does not complete")
	}
	b, err := s.openSession(a.OrganizationID, a.SessionHash, revision, enc)
	if err != nil {
		return apperr.Unauthorized
	}
	now := time.Now().Unix()
	needsRefresh := b.IdentityExpires <= now || (policy.Profile == "ONLINE_REQUIRED" && b.AccessExpires <= now+30)
	if needsRefresh {
		if b.Refresh == "" {
			return apperr.Unauthorized
		}
		// Persist before the outbound exchange. A crash or uncertain response cannot
		// cause another instance to reuse a possibly rotated refresh token.
		result, err := s.DB.Exec(`UPDATE browser_credentials SET refresh_state='inflight' WHERE token_hash=? AND org_id=? AND revision=? AND refresh_state=''`, a.SessionHash, a.OrganizationID, revision)
		if err != nil {
			return apperr.New(503, "SESSION_UNAVAILABLE", "try again")
		}
		n, _ := result.RowsAffected()
		if n != 1 {
			return apperr.New(503, "SESSION_REFRESH_PENDING", "reload after the current refresh completes")
		}
		doc, _, err := discoCache.getContext(r.Context(), p.ID, p.Issuer)
		var refreshed federation.Identity
		if err == nil {
			refreshed, err = doc.Refresh(r.Context(), p.ClientID, p.ClientSecret, b.Refresh, b.Nonce, b.Subject)
		}
		if err == nil && refreshed.Claims != nil {
			if _, present := refreshed.Claims["auth_time"]; present && claimTime(refreshed.Claims, "auth_time") != b.AuthTime {
				err = federation.ErrToken
			}
			if acr, ok := refreshed.Claims["acr"].(string); ok {
				b.ACR = acr
			}
			if _, ok := refreshed.Claims["amr"]; ok {
				b.AMR = claimAMR(refreshed.Claims)
			}
		}
		if err != nil {
			// Any unknown exchange outcome ends this local session. If storage is down,
			// the persisted inflight marker still prevents refresh-token reuse.
			_, _ = s.DB.Exec(`DELETE FROM browser_sessions WHERE token_hash=?`, a.SessionHash)
			return apperr.New(401, "SESSION_REAUTHENTICATION_REQUIRED", "sign in again to continue")
		}
		b.Access, b.AccessExpires = refreshed.AccessToken, refreshed.AccessExpiresAt.Unix()
		if refreshed.RefreshToken != "" {
			b.Refresh = refreshed.RefreshToken
		}
		if refreshed.RawIDToken != "" {
			b.IDToken = refreshed.RawIDToken
			b.IdentityExpires = refreshed.ExpiresAt.Unix()
		} else {
			b.IdentityExpires = b.AccessExpires
		}
		ciphertext, err := s.sealSession(a.OrganizationID, a.SessionHash, revision+1, b)
		if err != nil {
			return apperr.New(503, "SESSION_UNAVAILABLE", "sign in again")
		}
		tx, err := s.DB.BeginTx(r.Context(), nil)
		if err != nil {
			return apperr.New(503, "SESSION_UNAVAILABLE", "sign in again")
		}
		defer tx.Rollback()
		result, err = tx.Exec(`UPDATE browser_credentials SET ciphertext=?,revision=revision+1,refresh_state='' WHERE token_hash=? AND org_id=? AND revision=? AND refresh_state='inflight'`, ciphertext, a.SessionHash, a.OrganizationID, revision)
		if err != nil {
			return apperr.New(503, "SESSION_UNAVAILABLE", "sign in again")
		}
		n, _ = result.RowsAffected()
		if n != 1 {
			return apperr.Unauthorized
		}
		if err = auth.AuditReq(tx, r, a.OrganizationID, a, "auth.session.refresh", "session", nil); err != nil {
			return apperr.New(503, "AUDIT_UNAVAILABLE", "sign in again")
		}
		if tx.Commit() != nil {
			return apperr.New(503, "SESSION_UNAVAILABLE", "sign in again")
		}
	}
	if policy.Profile == "ONLINE_REQUIRED" {
		doc, _, err := discoCache.getContext(r.Context(), p.ID, p.Issuer)
		if err == nil {
			err = doc.CheckAccess(r.Context(), b.Access, policy.OnlineAudience, p.ClientID, p.ClientSecret, b.Subject)
		}
		if err == federation.ErrToken {
			return apperr.Unauthorized
		}
		if err != nil {
			return apperr.New(503, "IDENTITY_PROVIDER_UNAVAILABLE", "online session verification unavailable")
		}
	}
	a.SessionProfile, a.SessionExpires, a.SensitiveAllowed = policy.Profile, expires, policy.sensitive(b)
	a.ReauthenticationURL = "/api/v1/auth/oidc/" + url.PathEscape(p.OrgSlug) + "/login?pid=" + url.QueryEscape(p.ID) + "&reauth=1"
	a.AuthValidUntil = b.AuthTime + int64(policy.MaxAuthAge)
	a.RPLogoutEnabled = b.LogoutEndpoint != ""

	return nil
}
