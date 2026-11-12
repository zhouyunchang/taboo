package secret

import (
	"context"
	"database/sql"
	"errors"

	"github.com/zhouyunchang/taboo/apps/server-go/internal/auth"
	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/events"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/folder"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/outbox"
)

var (
	ErrRotationVersionConflict  = errors.New("rotation: secret version conflict")
	ErrRotationApprovalRequired = errors.New("rotation: project approval required")
	ErrRotationSecretNotFound   = errors.New("rotation: secret not found")
)

// WriteGeneratedVersion writes a rotation-generated value as a new secret
// version. It is intentionally an internal path: the caller has already
// checked the rotation policy permission, and this method must not go through
// the HTTP approval queue. Projects that require approval are rejected so the
// caller can ask the operator to use the normal approval flow.
func (s *Service) WriteGeneratedVersion(ctx context.Context, orgID, projectID, folderPath, key, value, reason, actorID, actorName, actorKind string, expected int) (int, error) {
	if s == nil || s.DB == nil {
		return 0, errors.New("rotation: no database")
	}
	norm, err := folder.NormalizePath(folderPath)
	if err != nil {
		return 0, err
	}
	if norm == "" {
		norm = "/"
	}
	var secretID, projectSlug string
	var latest, approvalRequired int
	if err := s.DB.QueryRowContext(ctx, `SELECT s.id, s.latest_version, pr.slug, pr.approval_required
		FROM secrets s
		JOIN folders f ON f.id = s.folder_id
		JOIN projects pr ON pr.id = s.project_id
		WHERE s.project_id = ? AND s.key = ? AND f.path = ?`,
		projectID, key, norm).Scan(&secretID, &latest, &projectSlug, &approvalRequired); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrRotationSecretNotFound
		}
		return 0, err
	}
	if approvalRequired == 1 {
		return 0, ErrRotationApprovalRequired
	}
	if expected > 0 && latest != expected {
		return 0, ErrRotationVersionConflict
	}
	next := latest + 1
	dek, err := s.orgDEK(orgID)
	if err != nil {
		return 0, err
	}
	ct, err := tc.Seal(dek, value, tc.SecretContext(orgID, projectID, secretID, next))
	if err != nil {
		return 0, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	// Re-check inside the transaction so a concurrent write cannot be
	// overwritten between the first read and the update.
	if err := tx.QueryRowContext(ctx, `SELECT latest_version FROM secrets WHERE id = ?`, secretID).Scan(&latest); err != nil {
		return 0, err
	}
	if latest != next-1 {
		return 0, ErrRotationVersionConflict
	}
	res, err := tx.ExecContext(ctx, `UPDATE secrets SET latest_version = ?, updated_at = datetime('now') WHERE id = ? AND latest_version = ?`, next, secretID, latest)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return 0, ErrRotationVersionConflict
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO secret_versions (id, secret_id, version, ciphertext, created_by) VALUES (?, ?, ?, ?, ?)`,
		tc.NewID(), secretID, next, ct, actorID); err != nil {
		return 0, err
	}
	kind := actorKind
	if kind == "" {
		kind = auth.KindUser
	}
	actor := &auth.Actor{ID: actorID, Name: actorName, Kind: kind}
	if err := auth.Audit(tx, orgID, actor, "secrets.rotation.generate", "project/"+projectSlug+"/secret/"+key,
		map[string]any{"version": next, "reason": reason}, ""); err != nil {
		return 0, err
	}
	e := events.SecretEvent{
		EventID: tc.NewID(), OrgID: orgID, ProjectID: projectID, ProjectSlug: projectSlug,
		Folder: norm, Key: key, Version: next, Action: "updated",
		ActorID: actorID, ActorName: actorName, ActorKind: kind, SecretID: secretID,
		Ciphertext: ct,
	}
	if err := outbox.Insert(tx, e); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return next, nil
}
