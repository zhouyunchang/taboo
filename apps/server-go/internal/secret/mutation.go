package secret

import (
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/http"

	"github.com/zhouyunchang/taboo/apps/server-go/internal/apperr"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/auth"
)

// Receipts contain only a keyed request digest and the public response. They
// commit atomically with the mutation, audit and outbox. Authorization is always
// rechecked before reaching this helper, including on a replay.
func (s *Service) replayMutation(tx *sql.Tx, w http.ResponseWriter, r *http.Request, org string, body any) bool {
	key := r.Header.Get("Idempotency-Key")
	if key == "" {
		return false
	}
	if len(key) > 128 {
		writeErr(w, apperr.InvalidInput)
		return true
	}
	encoded, _ := json.Marshal(body)
	mac := hmac.New(sha256.New, s.MasterKey)
	mac.Write([]byte("taboo/mutation/v1\x00" + r.Method + "\x00" + r.URL.RequestURI() + "\x00"))
	mac.Write(encoded)
	hash := hex.EncodeToString(mac.Sum(nil))
	actor := auth.From(r).ID
	res, err := tx.Exec(`INSERT INTO mutation_receipts(org_id,actor_id,request_key,request_hash,response) VALUES(?,?,?,?,'') ON CONFLICT(org_id,actor_id,request_key) DO NOTHING`, org, actor, key, hash)
	if err != nil {
		writeErr(w, apperr.New(503, "MUTATION_UNAVAILABLE", "retry with the same idempotency key"))
		return true
	}
	n, err := res.RowsAffected()
	if err != nil {
		writeErr(w, apperr.New(503, "MUTATION_UNAVAILABLE", "retry with the same idempotency key"))
		return true
	}
	if n == 1 {
		return false
	}
	var previous, response string
	if err = tx.QueryRow(`SELECT request_hash,response FROM mutation_receipts WHERE org_id=? AND actor_id=? AND request_key=?`, org, actor, key).Scan(&previous, &response); err != nil || response == "" {
		writeErr(w, apperr.New(503, "MUTATION_UNAVAILABLE", "retry with the same idempotency key"))
		return true
	}
	if !hmac.Equal([]byte(hash), []byte(previous)) {
		writeErr(w, apperr.New(409, "IDEMPOTENCY_CONFLICT", "this idempotency key was used for a different request"))
		return true
	}
	w.Header().Set("Idempotency-Replayed", "true")
	writeJSON(w, 200, json.RawMessage(response))
	return true
}

func saveMutation(tx *sql.Tx, r *http.Request, org string, response any) error {
	if r.Header.Get("Idempotency-Key") == "" {
		return nil
	}
	b, err := json.Marshal(response)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`UPDATE mutation_receipts SET response=? WHERE org_id=? AND actor_id=? AND request_key=?`, string(b), org, auth.From(r).ID, r.Header.Get("Idempotency-Key"))
	return err
}

// Check completed receipts before resolving an object that may have been deleted
// by the original request. The main transaction repeats this check to close races.
func (s *Service) checkReplay(w http.ResponseWriter, r *http.Request, org string, body any) bool {
	if r.Header.Get("Idempotency-Key") == "" {
		return false
	}
	tx, err := s.DB.Begin()
	if err != nil {
		writeErr(w, apperr.New(503, "MUTATION_UNAVAILABLE", "retry with the same idempotency key"))
		return true
	}
	defer tx.Rollback()
	return s.replayMutation(tx, w, r, org, body)
}
