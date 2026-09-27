// 事务 outbox：密钥变更与业务同一事务落库，worker 再投递到事件总线。
package outbox

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/events"
)

type DBTX interface {
	Exec(query string, args ...any) (sql.Result, error)
	QueryRow(query string, args ...any) *sql.Row
}

type Service struct {
	DB  *sql.DB
	Bus *events.Bus
}

// NextSequence uses the existing organization audit lock. Gaps in this counter
// are harmless; the audit hash chain is based on entries, not counter adjacency.
func NextSequence(tx DBTX, org string) (int64, error) {
	var seq int64
	err := tx.QueryRow(`INSERT INTO audit_heads(org_id,revision) VALUES(?,1) ON CONFLICT(org_id) DO UPDATE SET revision=audit_heads.revision+1 RETURNING revision`, org).Scan(&seq)
	return seq, err
}
func Insert(tx DBTX, e events.SecretEvent) error {
	if db, ok := tx.(*sql.DB); ok {
		transaction, err := db.Begin()
		if err != nil {
			return err
		}
		defer transaction.Rollback()
		if err = Insert(transaction, e); err != nil {
			return err
		}
		return transaction.Commit()
	}
	var err error
	e.Sequence, err = NextSequence(tx, e.OrgID)
	if err != nil {
		return err
	}

	if e.EventID == "" {
		e.EventID = tc.NewID()
	}
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO outbox_events (id, org_id, event_id, kind, payload, status, created_at,event_sequence)
		VALUES (?, ?, ?, 'secret', ?, 'pending', datetime('now'),?)`,
		tc.NewID(), e.OrgID, e.EventID, string(b), e.Sequence)
	return err
}

func (s *Service) StartWorker(ctx context.Context, interval time.Duration) {
	if s == nil || s.DB == nil {
		return
	}
	go func() {
		tick := time.NewTicker(interval)
		defer tick.Stop()
		for {
			s.Drain()
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
			}
		}
	}()
}

func (s *Service) Drain() {
	if s.DB == nil {
		return
	}
	rows, err := s.DB.Query(`SELECT id, payload FROM outbox_events WHERE status = 'pending' ORDER BY event_sequence,id LIMIT 20`)
	if err != nil {
		return
	}
	type row struct{ id, payload string }
	var list []row
	for rows.Next() {
		var x row
		if err := rows.Scan(&x.id, &x.payload); err == nil {
			list = append(list, x)
		}
	}
	rows.Close()
	for _, x := range list {
		if err := s.materialize(x.id); err != nil {
			slog.Error("outbox materialization failed", "id", x.id, "err", err)
		}
	}
}

func (s *Service) materialize(id string) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Atomic claim: competitors block, then observe no pending row. An interrupted
	// transaction leaves the event pending and does not strand partial tasks.
	var payload string
	err = tx.QueryRow(`UPDATE outbox_events SET status='done' WHERE id=? AND status='pending' RETURNING payload`, id).Scan(&payload)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	var event events.SecretEvent
	if err = json.Unmarshal([]byte(payload), &event); err != nil {
		if _, err = tx.Exec(`UPDATE outbox_events SET status='dead',error='invalid event payload' WHERE id=?`, id); err != nil {
			return err
		}
		return tx.Commit()
	}
	if event.Sequence <= 0 {
		if _, err = tx.Exec(`UPDATE outbox_events SET status='dead',error='legacy event requires explicit current-state requeue' WHERE id=?`, id); err != nil {
			return err
		}
		return tx.Commit()
	}
	if s.Bus == nil {
		return fmt.Errorf("durable event consumers unavailable")
	}
	if err = s.Bus.Dispatch(tx, event); err != nil {
		return err
	}
	return tx.Commit()
}
