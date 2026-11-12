package rotation

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"time"

	"github.com/zhouyunchang/taboo/apps/server-go/internal/auth"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/buildinfo"
	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/events"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/outbox"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/project"
)

// StartWorker starts the polling loop in the current process. The worker
// heartbeat is what the API health endpoint reads; leases keep two workers
// from claiming the same policy.
func (s *Service) StartWorker(ctx context.Context, interval time.Duration) {
	if s == nil || s.DB == nil || ctx.Err() != nil {
		return
	}
	if s.WorkerID == "" {
		host, _ := os.Hostname()
		if host == "" {
			host = "worker"
		}
		s.WorkerID = host + "-" + tc.NewID()[:8]
	}
	if s.Role == "" {
		s.Role = "rotation"
	}
	if s.Version == "" {
		s.Version = buildinfo.Get().Version
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		s.writeHeartbeat(ctx, 0, 0)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				claimed, errs := s.Sweep(ctx)
				s.Watchdog(ctx)
				s.writeHeartbeat(ctx, claimed, errs)
			}
		}
	}()
}

func (s *Service) writeHeartbeat(ctx context.Context, claimed, errors int) {
	now := time.Now().Unix()
	_, err := s.DB.ExecContext(ctx, `INSERT INTO worker_heartbeats
		(worker_id, role, hostname, version, started_at, last_seen_at, last_sweep_at, claimed, errors)
		VALUES (?,?,?,?,?,?,?,?,?)
		ON CONFLICT(worker_id) DO UPDATE SET
			role=excluded.role, hostname=excluded.hostname, version=excluded.version,
			last_seen_at=excluded.last_seen_at, last_sweep_at=excluded.last_sweep_at,
			claimed=excluded.claimed, errors=excluded.errors`,
		s.WorkerID, s.Role, hostname(), s.Version, now-1, now, now, claimed, errors)
	if err != nil {
		slog.Error("rotation heartbeat failed", "err", err)
	}
}

func hostname() string {
	if h, err := os.Hostname(); err == nil {
		return h
	}
	return "worker"
}

// Sweep claims due active policies and creates one run each. It returns the
// number claimed and the number of errors for the heartbeat.
func (s *Service) Sweep(ctx context.Context) (int, int) {
	if s == nil || s.DB == nil || ctx.Err() != nil {
		return 0, 0
	}
	now := time.Now().Unix()
	rows, err := s.DB.QueryContext(ctx, `SELECT id FROM rotation_policies
		WHERE status='active' AND next_run_at > 0 AND next_run_at <= ? AND lease_until <= ?
		ORDER BY next_run_at, id LIMIT 64`, now, now)
	if err != nil {
		return 0, 1
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	claimed, errs := 0, 0
	for _, id := range ids {
		if ctx.Err() != nil {
			break
		}
		ok, err := s.firePolicy(ctx, id, now)
		if err != nil {
			errs++
			slog.Error("rotation fire failed", "id", id, "err", err)
			continue
		}
		if ok {
			claimed++
		}
	}
	return claimed, errs
}

func (s *Service) firePolicy(ctx context.Context, policyID string, now int64) (bool, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	claim := tc.NewID()
	res, err := tx.ExecContext(ctx, `UPDATE rotation_policies SET lease_token=?, lease_until=?
		WHERE id=? AND status='active' AND next_run_at > 0 AND next_run_at <= ? AND lease_until <= ?`,
		claim, now+60, policyID, now, now)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return false, nil
	}
	var interval, grace int
	var cron, mode, ownerID, orgID, projectID, slug, folderPath, key, status string
	var secretID string
	var dueAt int64
	var oldVersion, autoGenerate int
	err = tx.QueryRowContext(ctx, `SELECT p.interval_seconds, p.cron, p.mode, p.owner_user_id, p.grace_seconds,
		p.status, p.next_run_at, p.org_id, p.project_id, pr.slug, f.path, s.key, s.id, s.latest_version, p.auto_generate
		FROM rotation_policies p
		JOIN secrets s ON s.id=p.secret_id
		JOIN projects pr ON pr.id=p.project_id
		JOIN folders f ON f.id=s.folder_id
		WHERE p.id=? AND p.lease_token=?`, policyID, claim).
		Scan(&interval, &cron, &mode, &ownerID, &grace, &status, &dueAt, &orgID, &projectID, &slug, &folderPath, &key, &secretID, &oldVersion, &autoGenerate)
	if err != nil {
		return false, err
	}
	if status != statusActive {
		return false, nil
	}
	policy := Policy{ID: policyID, Mode: mode, Interval: interval, Cron: cron, Grace: grace, Owner: &Owner{ID: ownerID}, AutoGenerate: autoGenerate == 1}
	p := project.Ctx{ID: projectID, OrgID: orgID, Slug: slug}
	var open int
	_ = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM rotation_runs WHERE policy_id=? AND status IN (?,?,?,?,?,?)`,
		policyID, openRunStatuses[0], openRunStatuses[1], openRunStatuses[2], openRunStatuses[3], openRunStatuses[4], openRunStatuses[5]).Scan(&open)
	next, nerr := nextAfter(time.Unix(dueAt, 0), interval, cron, time.Now())
	if nerr != nil {
		next = time.Now().Add(24 * time.Hour)
	}
	if open > 0 {
		if _, err := tx.ExecContext(ctx, `UPDATE rotation_policies SET next_run_at=?, lease_token='', lease_until=0,
			updated_at=datetime('now') WHERE id=? AND lease_token=?`, next.Unix(), policyID, claim); err != nil {
			return false, err
		}
		if err := auth.Audit(tx, orgID, &auth.Actor{ID: "system", Name: "rotation", Kind: "system"},
			"rotation.run.coalesced", "project/"+slug+"/secret/"+key,
			map[string]any{"policy_id": policyID, "due_at": dueAt, "next_run_at": next.Unix()}, ""); err != nil {
			return false, err
		}
		if err := tx.Commit(); err != nil {
			return false, err
		}
		return false, nil
	}
	runID, _, _, err := s.insertRun(tx, p, secretID, key, folderPath, policy, "schedule", dueAt, int64(oldVersion),
		&auth.Actor{ID: "system", Name: "rotation", Kind: "system"})
	if err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE rotation_policies SET next_run_at=?, lease_token='', lease_until=0,
		updated_at=datetime('now') WHERE id=? AND lease_token=?`, next.Unix(), policyID, claim); err != nil {
		return false, err
	}
	if err := auth.Audit(tx, orgID, &auth.Actor{ID: "system", Name: "rotation", Kind: "system"},
		"rotation.run.created", "project/"+slug+"/secret/"+key,
		map[string]any{"run_id": runID, "policy_id": policyID, "due_at": dueAt, "old_version": oldVersion}, ""); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	if mode == modeGenerate && policy.AutoGenerate {
		s.autoGenerate(ctx, runID)
	}
	return true, nil
}

// autoGenerate is the worker path for auto_generate=true. It is best-effort:
// if generation fails, the run remains awaiting_generation for a human.
func (s *Service) autoGenerate(ctx context.Context, runID string) {
	run, err := s.runRow(runID)
	if err != nil || run.Status != "awaiting_generation" || run.SecretID == "" || s.Secrets == nil {
		return
	}
	var p project.Ctx
	var secretID, key, folderPath string
	if err := s.DB.QueryRow(`SELECT r.org_id, r.project_id, pr.slug, r.secret_id, r.secret_key, r.folder_path
		FROM rotation_runs r JOIN projects pr ON pr.id=r.project_id WHERE r.id=?`, runID).
		Scan(&p.OrgID, &p.ID, &p.Slug, &secretID, &key, &folderPath); err != nil {
		return
	}
	policy, err := s.loadPolicy(secretID, key)
	if err != nil || policy.Generation == nil {
		return
	}
	value, err := GenerateValue(*policy.Generation)
	if err != nil {
		return
	}
	actor := &auth.Actor{ID: "system", Name: "rotation", Kind: "system"}
	next, err := s.Secrets.WriteGeneratedVersion(ctx, p.OrgID, p.ID, folderPath, key, value,
		"auto-generated rotation", actor.ID, actor.Name, actor.Kind, run.OldVersion)
	if err != nil {
		slog.Error("rotation auto-generate failed", "err", err, "run", runID)
		return
	}
	evidence, _ := json.Marshal(map[string]any{
		"generation_profile": policy.Generation.Profile,
		"generation_length":  policy.Generation.Length,
		"generation_entropy": policy.Generation.MinEntropyBits,
	})
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE rotation_runs SET new_version=?, status='awaiting_distribution',
		evidence_json=?, started_at=datetime('now'), updated_at=datetime('now')
		WHERE id=? AND status='awaiting_generation'`, next, string(evidence), runID); err != nil {
		return
	}
	if err := auth.Audit(tx, p.OrgID, actor, "rotation.run.generated", "project/"+p.Slug+"/secret/"+key,
		map[string]any{"run_id": runID, "old_version": run.OldVersion, "new_version": next,
			"profile": policy.Generation.Profile, "length": policy.Generation.Length,
			"entropy": policy.Generation.MinEntropyBits}, ""); err != nil {
		return
	}
	_ = tx.Commit()
}

// Watchdog marks open runs past their grace window as overdue. It does not
// revoke anything and does not disable the policy.
func (s *Service) Watchdog(ctx context.Context) {
	if s == nil || s.DB == nil || ctx.Err() != nil {
		return
	}
	now := time.Now().Unix()
	rows, err := s.DB.QueryContext(ctx, `SELECT r.id, r.org_id, r.project_id, pr.slug, r.secret_key, r.folder_path, r.secret_id,
		r.policy_id, r.due_at, r.grace_until, r.old_version, COALESCE(p.mode,'')
		FROM rotation_runs r JOIN projects pr ON pr.id=r.project_id
		LEFT JOIN rotation_policies p ON p.id=r.policy_id
		WHERE r.status IN (?,?,?,?,?) AND r.grace_until < ? LIMIT 64`,
		openRunStatuses[0], openRunStatuses[1], openRunStatuses[2], openRunStatuses[3], openRunStatuses[4], now)
	if err != nil {
		return
	}
	type item struct {
		id, org, project, slug, key, folder, secretID, policy, mode string
		due, grace                                                  int64
		oldVersion                                                  int
	}
	items := []item{}
	for rows.Next() {
		var x item
		if rows.Scan(&x.id, &x.org, &x.project, &x.slug, &x.key, &x.folder, &x.secretID, &x.policy, &x.due, &x.grace, &x.oldVersion, &x.mode) == nil {
			items = append(items, x)
		}
	}
	rows.Close()
	for _, x := range items {
		tx, err := s.DB.BeginTx(ctx, nil)
		if err != nil {
			continue
		}
		res, err := tx.ExecContext(ctx, `UPDATE rotation_runs SET status='overdue', updated_at=datetime('now')
			WHERE id=? AND status IN (?,?,?,?,?) AND grace_until < ?`,
			x.id, openRunStatuses[0], openRunStatuses[1], openRunStatuses[2], openRunStatuses[3], openRunStatuses[4], now)
		if err != nil {
			tx.Rollback()
			continue
		}
		n, _ := res.RowsAffected()
		if n != 1 {
			tx.Rollback()
			continue
		}
		system := &auth.Actor{ID: "system", Name: "rotation", Kind: "system"}
		_ = auth.Audit(tx, x.org, system, "rotation.run.overdue", "project/"+x.slug+"/secret/"+x.key,
			map[string]any{"run_id": x.id, "policy_id": x.policy}, "")
		e := events.SecretEvent{
			EventID: tc.NewID(), OrgID: x.org, ProjectID: x.project, ProjectSlug: x.slug,
			Folder: x.folder, Key: x.key, Version: x.oldVersion, Action: "rotation_overdue",
			ActorID: "system", ActorName: "rotation", ActorKind: "system", SecretID: x.secretID,
			RunID: x.id, PolicyID: x.policy, Mode: x.mode, OldVersion: x.oldVersion, DueAt: x.due, GraceUntil: x.grace,
		}
		_ = outbox.Insert(tx, e)
		_ = tx.Commit()
	}
}
