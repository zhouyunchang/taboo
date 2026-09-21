// 事务 outbox：密钥变更与业务同一事务落库，worker 再投递到事件总线。
package outbox

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"time"

	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/events"
)

type DBTX interface {
	Exec(query string, args ...any) (sql.Result, error)
}

type Service struct {
	DB  *sql.DB
	Bus *events.Bus
}

func Insert(tx DBTX, e events.SecretEvent) error {
	if e.EventID == "" {
		e.EventID = tc.NewID()
	}
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO outbox_events (id, org_id, event_id, kind, payload, status, created_at)
		VALUES (?, ?, ?, 'secret', ?, 'pending', datetime('now'))`,
		tc.NewID(), e.OrgID, e.EventID, string(b))
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
	rows, err := s.DB.Query(`SELECT id, payload FROM outbox_events WHERE status = 'pending' ORDER BY created_at LIMIT 20`)
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
		var e events.SecretEvent
		if err := json.Unmarshal([]byte(x.payload), &e); err != nil {
			_, _ = s.DB.Exec(`UPDATE outbox_events SET status = 'dead', error = ? WHERE id = ?`, err.Error(), x.id)
			continue
		}
		if s.Bus != nil {
			s.Bus.Publish(e)
		}
		if _, err := s.DB.Exec(`UPDATE outbox_events SET status = 'done' WHERE id = ?`, x.id); err != nil {
			slog.Error("outbox mark done", "id", x.id, "err", err)
		}
	}
}
