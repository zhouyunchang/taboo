package oidc

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/apperr"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/auth"
	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/store"
)

// BackChannel is the OIDC back-channel logout endpoint. It does not use the
// browser session or CSRF: the logout token is the credential.
func (s *Service) BackChannel(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil || r.PostForm.Get("logout_token") == "" {
		writeErr(w, apperr.New(400, "INVALID_LOGOUT_TOKEN", "logout token was rejected"))
		return
	}
	raw := r.PostForm.Get("logout_token")
	p, err := s.providerByOrgSlug(chi.URLParam(r, "slug"), r.URL.Query().Get("pid"))
	if err != nil || !p.Enabled || !s.allowedIssuer(p.Issuer) {
		writeErr(w, apperr.New(400, "INVALID_LOGOUT_TOKEN", "logout token was rejected"))
		return
	}
	doc, _, err := discoCache.getContext(r.Context(), p.ID, p.Issuer)
	if err != nil {
		slog.Info("back-channel discovery failed", "provider", p.ID)
		writeErr(w, apperr.New(503, "IDP_UNREACHABLE", "identity provider unavailable"))
		return
	}
	sub, sid, replay, exp, err := doc.VerifyLogoutToken(r.Context(), raw, p.ClientID)
	if err != nil {
		slog.Info("back-channel token rejected", "provider", p.ID)
		writeErr(w, apperr.New(400, "INVALID_LOGOUT_TOKEN", "logout token was rejected"))
		return
	}
	tx, err := s.DB.Begin()
	if err != nil {
		writeErr(w, apperr.New(503, "SESSION_UNAVAILABLE", "try again"))
		return
	}
	defer tx.Rollback()
	_, err = tx.Exec(`INSERT INTO logout_replays(token_hash, provider_id, expires_at) VALUES(?,?,?)`, tc.SHA256(p.ID+"\x00"+replay), p.ID, exp.Unix())
	if store.IsUniqueViolation(err) {
		writeErr(w, apperr.New(400, "INVALID_LOGOUT_TOKEN", "logout token was rejected"))
		return
	}
	if err != nil {
		writeErr(w, apperr.New(503, "SESSION_UNAVAILABLE", "try again"))
		return
	}
	query := `SELECT token_hash FROM browser_sessions WHERE provider_id=? AND subject=?`
	arg := sub
	if sid != "" {
		query = `SELECT token_hash FROM browser_sessions WHERE provider_id=? AND sid=?`
		arg = sid
	}
	rows, err := tx.Query(query, p.ID, arg)
	if err != nil {
		writeErr(w, apperr.New(503, "SESSION_UNAVAILABLE", "try again"))
		return
	}
	var hashes []string
	for rows.Next() {
		var hash string
		if rows.Scan(&hash) == nil {
			hashes = append(hashes, hash)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		writeErr(w, apperr.New(503, "SESSION_UNAVAILABLE", "try again"))
		return
	}
	for _, hash := range hashes {
		if _, err = tx.Exec(`DELETE FROM browser_credentials WHERE token_hash=?`, hash); err != nil {
			writeErr(w, apperr.New(503, "SESSION_UNAVAILABLE", "try again"))
			return
		}
		if _, err = tx.Exec(`DELETE FROM browser_sessions WHERE token_hash=?`, hash); err != nil {
			writeErr(w, apperr.New(503, "SESSION_UNAVAILABLE", "try again"))
			return
		}
	}
	if err = auth.AuditReq(tx, r, p.OrgID, &auth.Actor{ID: "system", Kind: "system", Name: "backchannel"}, "auth.backchannel_logout", "provider/"+p.ID, map[string]any{"sessions": len(hashes)}); err != nil || tx.Commit() != nil {
		writeErr(w, apperr.New(503, "AUDIT_UNAVAILABLE", "logout could not be recorded"))
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
}
