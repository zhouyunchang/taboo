package sync

import (
	"database/sql"
	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
	"time"
)

// Targets have one ordered stream. This is deliberately stricter than per-key
// ordering: targets can flatten several projects/environments into the same key.
func (s *Service) claimOrdered(id string) (string, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var target, org string
	var sequence int64
	err = tx.QueryRow(`SELECT target_id,org_id,event_sequence FROM sync_runs WHERE id=? AND status='pending'`, id).Scan(&target, &org, &sequence)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	result, err := tx.Exec(`UPDATE sync_targets SET name=name WHERE id=? AND org_id=? AND enabled=1`, target, org)
	if err != nil {
		return "", err
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return "", nil
	}
	var first string
	err = tx.QueryRow(`SELECT id FROM sync_runs WHERE target_id=? AND status='pending' ORDER BY event_sequence,id LIMIT 1`, target).Scan(&first)
	if err != nil {
		return "", err
	}
	if first != id {
		return "", nil
	}
	var pending int
	err = tx.QueryRow(`SELECT COUNT(*) FROM outbox_events WHERE org_id=? AND status='pending' AND event_sequence<?`, org, sequence).Scan(&pending)
	if err != nil {
		return "", err
	}
	if pending > 0 {
		return "", nil
	}
	token := tc.NewID()
	now := time.Now().Unix()
	result, err = tx.Exec(`UPDATE sync_runs SET lease_token=?,lease_until=? WHERE id=? AND status='pending' AND lease_until<=? AND next_attempt_at<=?`, token, now+60, id, now, now)
	if err != nil {
		return "", err
	}
	n, _ = result.RowsAffected()
	if n != 1 {
		return "", nil
	}
	if err = tx.Commit(); err != nil {
		return "", err
	}
	return token, nil
}
