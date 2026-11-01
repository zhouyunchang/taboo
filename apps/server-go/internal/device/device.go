// Browser login for the CLI: device-code polling and loopback PKCE.
// Codes are stored as hashes. Redirects are limited to a loopback /callback.
package device

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/zhouyunchang/taboo/apps/server-go/internal/apperr"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/auth"
	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
)

const (
	clientID    = "taboo-cli"
	userCodeLen = 8
	deviceTTL   = 10 * time.Minute
	codeTTL     = 2 * time.Minute
	pollEvery   = 5
)

var userAlphabet = []byte("ABCDEFGHJKLMNPQRSTUVWXYZ23456789")

type Service struct {
	DB        *sql.DB
	JWTSecret string
	Origin    string
}

func (s *Service) Start(w http.ResponseWriter, r *http.Request) {
	var b struct {
		ClientID string `json:"client_id"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&b) != nil || b.ClientID != clientID {
		writeErr(w, apperr.InvalidInput)
		return
	}
	deviceRaw, err := randomToken()
	if err != nil {
		writeErr(w, apperr.New(503, "LOGIN_UNAVAILABLE", "try again"))
		return
	}
	userCode, err := newUserCode()
	if err != nil {
		writeErr(w, apperr.New(503, "LOGIN_UNAVAILABLE", "try again"))
		return
	}
	exp := time.Now().Add(deviceTTL).Unix()
	_, err = s.DB.Exec(`INSERT INTO device_authorizations(id,device_hash,user_code_hash,client_id,status,interval_seconds,expires_at,created_at) VALUES(?,?,?,?, 'pending', ?, ?, ?)`,
		tc.NewID(), tc.SHA256(deviceRaw), tc.SHA256(normalizeCode(userCode)), clientID, pollEvery, exp, time.Now().Unix())
	if err != nil {
		writeErr(w, apperr.New(503, "LOGIN_UNAVAILABLE", "try again"))
		return
	}
	origin := strings.TrimRight(s.Origin, "/")
	if origin == "" {
		origin = "http://127.0.0.1"
	}
	verification := origin + "/device"
	writeJSON(w, 200, map[string]any{
		"device_code":               deviceRaw,
		"user_code":                 formatCode(userCode),
		"verification_uri":          verification,
		"verification_uri_complete": verification + "?user_code=" + url.QueryEscape(formatCode(userCode)),
		"expires_in":                int(deviceTTL.Seconds()),
		"interval":                  pollEvery,
	})
}

func (s *Service) Token(w http.ResponseWriter, r *http.Request) {
	var b struct {
		DeviceCode string `json:"device_code"`
		GrantType  string `json:"grant_type"`
		ClientID   string `json:"client_id"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&b) != nil || b.ClientID != clientID || b.GrantType != "urn:ietf:params:oauth:grant-type:device_code" || len(b.DeviceCode) < 20 || len(b.DeviceCode) > 200 {
		oauthErr(w, 400, "invalid_request")
		return
	}
	now := time.Now().Unix()
	var id, status, userID, orgID string
	var interval int
	var exp, last int64
	err := s.DB.QueryRow(`SELECT id, status, user_id, org_id, interval_seconds, expires_at, last_polled_at FROM device_authorizations WHERE device_hash=?`, tc.SHA256(b.DeviceCode)).Scan(&id, &status, &userID, &orgID, &interval, &exp, &last)
	if err != nil {
		oauthErr(w, 400, "invalid_grant")
		return
	}
	if exp <= now || status == "expired" {
		_, _ = s.DB.Exec(`UPDATE device_authorizations SET status='expired' WHERE id=? AND status='pending'`, id)
		oauthErr(w, 400, "expired_token")
		return
	}
	if last > 0 && now-last < int64(interval) {
		oauthErr(w, 400, "slow_down")
		return
	}
	if _, err = s.DB.Exec(`UPDATE device_authorizations SET last_polled_at=? WHERE id=?`, now, id); err != nil {
		oauthErr(w, 503, "temporarily_unavailable")
		return
	}
	switch status {
	case "pending":
		oauthErr(w, 400, "authorization_pending")
	case "denied":
		oauthErr(w, 400, "access_denied")
	case "consumed":
		oauthErr(w, 400, "invalid_grant")
	case "approved":
		s.issueDevice(w, r, id, userID, orgID, exp)
	default:
		oauthErr(w, 400, "invalid_grant")
	}
}

func (s *Service) issueDevice(w http.ResponseWriter, r *http.Request, id, userID, orgID string, exp int64) {
	tx, err := s.DB.Begin()
	if err != nil {
		oauthErr(w, 503, "temporarily_unavailable")
		return
	}
	defer tx.Rollback()
	res, err := tx.Exec(`UPDATE device_authorizations SET status='consumed' WHERE id=? AND status='approved' AND expires_at>?`, id, time.Now().Unix())
	if err != nil {
		oauthErr(w, 503, "temporarily_unavailable")
		return
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		oauthErr(w, 400, "invalid_grant")
		return
	}
	pair, err := s.tokens(tx, userID)
	if err != nil || auth.AuditReq(tx, r, orgID, &auth.Actor{ID: userID, Kind: auth.KindUser}, "auth.device.consume", "device/"+id, nil) != nil || tx.Commit() != nil {
		oauthErr(w, 503, "temporarily_unavailable")
		return
	}
	writeJSON(w, 200, map[string]any{"tokens": pair, "user": map[string]any{"id": userID}})
}

func (s *Service) Decision(w http.ResponseWriter, r *http.Request) {
	actor := auth.From(r)
	if actor == nil || actor.Kind != auth.KindUser {
		writeErr(w, apperr.Forbidden)
		return
	}
	var b struct {
		UserCode string `json:"user_code"`
		OrgSlug  string `json:"org_slug"`
		Approve  bool   `json:"approve"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&b) != nil || b.OrgSlug == "" {
		writeErr(w, apperr.InvalidInput)
		return
	}
	var org string
	if s.DB.QueryRow(`SELECT id FROM orgs WHERE slug=?`, b.OrgSlug).Scan(&org) != nil {
		writeErr(w, apperr.NotFound)
		return
	}
	if _, ok := auth.Membership(s.DB, org, actor.ID); !ok {
		writeErr(w, apperr.Forbidden)
		return
	}
	status := "denied"
	action := "auth.device.deny"
	if b.Approve {
		status = "approved"
		action = "auth.device.approve"
	}
	tx, err := s.DB.Begin()
	if err != nil {
		writeErr(w, apperr.New(503, "LOGIN_UNAVAILABLE", "try again"))
		return
	}
	defer tx.Rollback()
	res, err := tx.Exec(`UPDATE device_authorizations SET status=?, user_id=?, org_id=? WHERE user_code_hash=? AND status='pending' AND expires_at>?`,
		status, actor.ID, org, tc.SHA256(normalizeCode(b.UserCode)), time.Now().Unix())
	if err != nil {
		writeErr(w, apperr.New(503, "LOGIN_UNAVAILABLE", "try again"))
		return
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		writeErr(w, apperr.New(404, "NOT_FOUND", "code is unknown or no longer pending"))
		return
	}
	if auth.AuditReq(tx, r, org, actor, action, "org/"+b.OrgSlug, nil) != nil || tx.Commit() != nil {
		writeErr(w, apperr.New(503, "AUDIT_UNAVAILABLE", "try again"))
		return
	}
	writeJSON(w, 200, map[string]any{"status": status})
}

func (s *Service) Authorize(w http.ResponseWriter, r *http.Request) {
	actor := auth.From(r)
	if actor == nil || actor.Kind != auth.KindUser {
		writeErr(w, apperr.Forbidden)
		return
	}
	var b struct {
		CodeChallenge string `json:"code_challenge"`
		RedirectURI   string `json:"redirect_uri"`
		OrgSlug       string `json:"org_slug"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&b) != nil || !validChallenge(b.CodeChallenge) || !Loopback(b.RedirectURI) {
		writeErr(w, apperr.InvalidInput)
		return
	}
	var org string
	if s.DB.QueryRow(`SELECT id FROM orgs WHERE slug=?`, b.OrgSlug).Scan(&org) != nil {
		writeErr(w, apperr.NotFound)
		return
	}
	if _, ok := auth.Membership(s.DB, org, actor.ID); !ok {
		writeErr(w, apperr.Forbidden)
		return
	}
	raw, err := randomToken()
	if err != nil {
		writeErr(w, apperr.New(503, "LOGIN_UNAVAILABLE", "try again"))
		return
	}
	tx, err := s.DB.Begin()
	if err != nil {
		writeErr(w, apperr.New(503, "LOGIN_UNAVAILABLE", "try again"))
		return
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`INSERT INTO cli_auth_codes(code_hash,user_id,org_id,challenge,redirect_uri,expires_at) VALUES(?,?,?,?,?,?)`,
		tc.SHA256(raw), actor.ID, org, b.CodeChallenge, b.RedirectURI, time.Now().Add(codeTTL).Unix()); err != nil {
		writeErr(w, apperr.New(503, "LOGIN_UNAVAILABLE", "try again"))
		return
	}
	if auth.AuditReq(tx, r, org, actor, "auth.cli.authorize", "org/"+b.OrgSlug, nil) != nil || tx.Commit() != nil {
		writeErr(w, apperr.New(503, "AUDIT_UNAVAILABLE", "try again"))
		return
	}
	target, _ := url.Parse(b.RedirectURI)
	q := target.Query()
	q.Set("code", raw)
	target.RawQuery = q.Encode()
	writeJSON(w, 200, map[string]any{"redirect": target.String()})
}

func (s *Service) Exchange(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Code         string `json:"code"`
		Verifier     string `json:"code_verifier"`
		RedirectURI  string `json:"redirect_uri"`
		ClientID     string `json:"client_id"`
		CodeVerifier string `json:"verifier"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&b) != nil || b.ClientID != clientID {
		writeErr(w, apperr.InvalidInput)
		return
	}
	if b.Verifier == "" {
		b.Verifier = b.CodeVerifier
	}
	if !validVerifier(b.Verifier) || !Loopback(b.RedirectURI) || len(b.Code) < 20 || len(b.Code) > 200 {
		writeErr(w, apperr.InvalidInput)
		return
	}
	tx, err := s.DB.Begin()
	if err != nil {
		writeErr(w, apperr.New(503, "LOGIN_UNAVAILABLE", "try again"))
		return
	}
	defer tx.Rollback()
	var userID, orgID, challenge, redirect string
	err = tx.QueryRow(`UPDATE cli_auth_codes SET consumed=1 WHERE code_hash=? AND consumed=0 AND expires_at>? RETURNING user_id, org_id, challenge, redirect_uri`,
		tc.SHA256(b.Code), time.Now().Unix()).Scan(&userID, &orgID, &challenge, &redirect)
	if err != nil {
		writeErr(w, apperr.New(400, "INVALID_GRANT", "code is unknown or already used"))
		return
	}
	sum := sha256.Sum256([]byte(b.Verifier))
	got := base64.RawURLEncoding.EncodeToString(sum[:])
	if subtle.ConstantTimeCompare([]byte(got), []byte(challenge)) != 1 || redirect != b.RedirectURI {
		writeErr(w, apperr.New(400, "INVALID_GRANT", "verifier or redirect does not match"))
		return
	}
	pair, err := s.tokens(tx, userID)
	if err != nil || auth.AuditReq(tx, r, orgID, &auth.Actor{ID: userID, Kind: auth.KindUser}, "auth.cli.exchange", "cli", nil) != nil || tx.Commit() != nil {
		writeErr(w, apperr.New(503, "LOGIN_UNAVAILABLE", "try again"))
		return
	}
	writeJSON(w, 200, map[string]any{"tokens": pair, "user": map[string]any{"id": userID}})
}

func (s *Service) tokens(tx *sql.Tx, userID string) (*auth.TokenPair, error) {
	var email, name, status string
	if err := tx.QueryRow(`SELECT email, name, status FROM users WHERE id=?`, userID).Scan(&email, &name, &status); err != nil || status != "active" {
		if err == nil {
			err = sql.ErrNoRows
		}
		return nil, err
	}
	return auth.IssueTokens(tx, s.JWTSecret, &auth.Actor{ID: userID, Email: email, Name: name, Kind: auth.KindUser})
}

// Loopback accepts only http://127.0.0.1, localhost or [::1] with an explicit
// port and the path /callback. There is no query, fragment or userinfo.
func Loopback(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Scheme != "http" || u.Path != "/callback" {
		return false
	}
	host := u.Hostname()
	if host != "127.0.0.1" && host != "localhost" && host != "::1" {
		return false
	}
	port := u.Port()
	if port == "" {
		return false
	}
	n, err := net.LookupPort("tcp", port)
	return err == nil && n > 0 && n < 65536
}

func validChallenge(v string) bool {
	if len(v) < 43 || len(v) > 128 {
		return false
	}
	for _, c := range v {
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '-' && c != '_' {
			return false
		}
	}
	return true
}

func validVerifier(v string) bool {
	if len(v) < 43 || len(v) > 128 {
		return false
	}
	for _, c := range v {
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && !strings.ContainsRune("-._~", c) {
			return false
		}
	}
	return true
}

func normalizeCode(v string) string {
	return strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(v), "-", ""))
}

func formatCode(v string) string {
	if len(v) != userCodeLen {
		return v
	}
	return v[:4] + "-" + v[4:]
}

func newUserCode() (string, error) {
	buf := make([]byte, userCodeLen)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	for i := range buf {
		buf[i] = userAlphabet[int(buf[i])%len(userAlphabet)]
	}
	return string(buf), nil
}

func randomToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func oauthErr(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]string{"error": code})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, e *apperr.Error) { writeJSON(w, e.Status, e) }
