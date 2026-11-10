package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
)

// CollapseEnvironments 把一个项目的多套环境收成 dev 这一套。
// 同目录同名密钥保留优先级 prod、staging、dev。项目审批开关取原来的 prod。
func CollapseEnvironments(db migrationDB) error {
	if err := addColumn(db, `ALTER TABLE projects ADD COLUMN approval_required INTEGER NOT NULL DEFAULT 0`); err != nil {
		return err
	}
	rows, err := db.Query(`SELECT id FROM projects`)
	if err != nil {
		return err
	}
	var projects []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		projects = append(projects, id)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, projectID := range projects {
		if err = collapseProject(db, projectID); err != nil {
			return err
		}
	}
	return nil
}

type envRow struct {
	id, slug string
	approval int
}

func collapseProject(db migrationDB, projectID string) error {
	rows, err := db.Query(`SELECT id, slug, COALESCE(approval_required,0) FROM environments WHERE project_id=?`, projectID)
	if err != nil {
		return err
	}
	var envs []envRow
	for rows.Next() {
		var item envRow
		if err = rows.Scan(&item.id, &item.slug, &item.approval); err != nil {
			rows.Close()
			return err
		}
		envs = append(envs, item)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	if len(envs) == 0 {
		return nil
	}
	winner := envs[0]
	approval := 0
	for _, item := range envs {
		if item.slug == "prod" {
			approval = item.approval
		}
		if envRank(item.slug) > envRank(winner.slug) {
			winner = item
		}
	}
	if _, err = db.Exec(`UPDATE projects SET approval_required=? WHERE id=?`, approval, projectID); err != nil {
		return err
	}
	for _, item := range envs {
		if item.id == winner.id {
			continue
		}
		if err = copyMissingSecrets(db, item.id, winner.id); err != nil {
			return err
		}
	}
	for _, item := range envs {
		if item.id == winner.id {
			continue
		}
		if err = retargetEnv(db, projectID, item.id, winner.id); err != nil {
			return err
		}
		if _, err = db.Exec(`DELETE FROM environments WHERE id=?`, item.id); err != nil {
			return err
		}
	}
	if winner.slug != "dev" {
		if _, err = db.Exec(`UPDATE environments SET slug='dev', name='dev', protected=0, approval_required=0 WHERE id=?`, winner.id); err != nil {
			return err
		}
	} else if _, err = db.Exec(`UPDATE environments SET protected=0, approval_required=0 WHERE id=?`, winner.id); err != nil {
		return err
	}
	return nil
}

func envRank(slug string) int {
	switch slug {
	case "prod":
		return 3
	case "staging":
		return 2
	case "dev":
		return 1
	default:
		return 0
	}
}

func copyMissingSecrets(db migrationDB, fromEnv, toEnv string) error {
	rows, err := db.Query(`SELECT id, folder, key, comment, tags, latest_version, created_at, updated_at FROM secrets WHERE env_id=?`, fromEnv)
	if err != nil {
		return err
	}
	type secretRow struct {
		id, folder, key, comment, tags, created, updated string
		version                                          int
	}
	var secrets []secretRow
	for rows.Next() {
		var item secretRow
		if err = rows.Scan(&item.id, &item.folder, &item.key, &item.comment, &item.tags, &item.version, &item.created, &item.updated); err != nil {
			rows.Close()
			return err
		}
		secrets = append(secrets, item)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, item := range secrets {
		var existing string
		err = db.QueryRow(`SELECT id FROM secrets WHERE env_id=? AND folder=? AND key=?`, toEnv, item.folder, item.key).Scan(&existing)
		if err == nil {
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		folderID, err := ensureFolderPath(db, toEnv, item.folder)
		if err != nil {
			return err
		}
		versions, err := db.Query(`SELECT version, ciphertext, created_by, created_at FROM secret_versions WHERE secret_id=?`, item.id)
		if err != nil {
			return err
		}
		type versionRow struct {
			version            int
			ciphertext, by, at string
		}
		var copied []versionRow
		for versions.Next() {
			var row versionRow
			if err = versions.Scan(&row.version, &row.ciphertext, &row.by, &row.at); err != nil {
				versions.Close()
				return err
			}
			copied = append(copied, row)
		}
		if err = versions.Err(); err != nil {
			versions.Close()
			return err
		}
		versions.Close()
		secretID := newStoreID()
		if _, err = db.Exec(`INSERT INTO secrets(id, env_id, folder, folder_id, key, comment, tags, latest_version, created_at, updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)`,
			secretID, toEnv, item.folder, folderID, item.key, item.comment, item.tags, item.version, item.created, item.updated); err != nil {
			return err
		}
		for _, row := range copied {
			if _, err = db.Exec(`INSERT INTO secret_versions(id, secret_id, version, ciphertext, created_by, created_at) VALUES(?,?,?,?,?,?)`,
				newStoreID(), secretID, row.version, row.ciphertext, row.by, row.at); err != nil {
				return err
			}
		}
	}
	return nil
}

func ensureFolderPath(db migrationDB, envID, path string) (string, error) {
	if path == "" {
		path = "/"
	}
	var id string
	err := db.QueryRow(`SELECT id FROM folders WHERE env_id=? AND path=?`, envID, path).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	var parent any
	name := "/"
	if path != "/" {
		parentID, err := ensureFolderPath(db, envID, parentFolder(path))
		if err != nil {
			return "", err
		}
		parent = parentID
		trimmed := strings.Trim(path, "/")
		if i := strings.LastIndex(trimmed, "/"); i >= 0 {
			name = trimmed[i+1:]
		} else {
			name = trimmed
		}
	}
	id = newStoreID()
	_, err = db.Exec(`INSERT INTO folders(id, env_id, parent_id, name, path) VALUES(?,?,?,?,?)`, id, envID, parent, name, path)
	return id, err
}

func parentFolder(path string) string {
	trimmed := strings.Trim(path, "/")
	i := strings.LastIndex(trimmed, "/")
	if i < 0 {
		return "/"
	}
	return "/" + trimmed[:i] + "/"
}

func retargetEnv(db migrationDB, projectID, fromID, toID string) error {
	if err := mergeScopes(db, projectID, fromID, toID); err != nil {
		return err
	}
	if err := retargetMemberGrants(db, projectID, fromID, toID); err != nil && !missingTable(err) {
		return err
	}
	if err := retargetWorkloadGrants(db, projectID, fromID, toID); err != nil && !missingTable(err) {
		return err
	}
	for _, table := range []string{"approval_requests", "access_requests"} {
		if _, err := db.Exec(`UPDATE `+table+` SET env_id=? WHERE project_id=? AND env_id=?`, toID, projectID, fromID); err != nil && !missingTable(err) {
			return err
		}
	}
	return nil
}

func mergeScopes(db migrationDB, projectID, fromID, toID string) error {
	rows, err := db.Query(`SELECT identity_id, permission FROM identity_scopes WHERE project_id=? AND env_id=?`, projectID, fromID)
	if err != nil {
		if missingTable(err) {
			return nil
		}
		return err
	}
	type scope struct{ id, permission string }
	var items []scope
	for rows.Next() {
		var item scope
		if err = rows.Scan(&item.id, &item.permission); err != nil {
			rows.Close()
			return err
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, item := range items {
		var current string
		err = db.QueryRow(`SELECT permission FROM identity_scopes WHERE identity_id=? AND project_id=? AND env_id=?`, item.id, projectID, toID).Scan(&current)
		if errors.Is(err, sql.ErrNoRows) {
			if _, err = db.Exec(`UPDATE identity_scopes SET env_id=? WHERE identity_id=? AND project_id=? AND env_id=?`, toID, item.id, projectID, fromID); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		if permRank(item.permission) > permRank(current) {
			if _, err = db.Exec(`UPDATE identity_scopes SET permission=? WHERE identity_id=? AND project_id=? AND env_id=?`, item.permission, item.id, projectID, toID); err != nil {
				return err
			}
		}
		if _, err = db.Exec(`DELETE FROM identity_scopes WHERE identity_id=? AND project_id=? AND env_id=?`, item.id, projectID, fromID); err != nil {
			return err
		}
	}
	return nil
}

func retargetMemberGrants(db migrationDB, projectID, fromID, toID string) error {
	rows, err := db.Query(`SELECT org_id, user_id, action FROM member_action_grants WHERE project_id=? AND env_id=?`, projectID, fromID)
	if err != nil {
		return err
	}
	type grant struct{ org, user, action string }
	var items []grant
	for rows.Next() {
		var item grant
		if err = rows.Scan(&item.org, &item.user, &item.action); err != nil {
			rows.Close()
			return err
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, item := range items {
		var n int
		if err = db.QueryRow(`SELECT COUNT(*) FROM member_action_grants WHERE org_id=? AND user_id=? AND project_id=? AND env_id=? AND action=?`, item.org, item.user, projectID, toID, item.action).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			_, err = db.Exec(`DELETE FROM member_action_grants WHERE org_id=? AND user_id=? AND project_id=? AND env_id=? AND action=?`, item.org, item.user, projectID, fromID, item.action)
		} else {
			_, err = db.Exec(`UPDATE member_action_grants SET env_id=? WHERE org_id=? AND user_id=? AND project_id=? AND env_id=? AND action=?`, toID, item.org, item.user, projectID, fromID, item.action)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func retargetWorkloadGrants(db migrationDB, projectID, fromID, toID string) error {
	rows, err := db.Query(`SELECT binding_id, action FROM workload_grants WHERE project_id=? AND env_id=?`, projectID, fromID)
	if err != nil {
		return err
	}
	type grant struct{ binding, action string }
	var items []grant
	for rows.Next() {
		var item grant
		if err = rows.Scan(&item.binding, &item.action); err != nil {
			rows.Close()
			return err
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, item := range items {
		var n int
		if err = db.QueryRow(`SELECT COUNT(*) FROM workload_grants WHERE binding_id=? AND project_id=? AND env_id=? AND action=?`, item.binding, projectID, toID, item.action).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			_, err = db.Exec(`DELETE FROM workload_grants WHERE binding_id=? AND project_id=? AND env_id=? AND action=?`, item.binding, projectID, fromID, item.action)
		} else {
			_, err = db.Exec(`UPDATE workload_grants SET env_id=? WHERE binding_id=? AND project_id=? AND env_id=? AND action=?`, toID, item.binding, projectID, fromID, item.action)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func permRank(permission string) int {
	switch permission {
	case "write":
		return 3
	case "reveal":
		return 2
	default:
		return 1
	}
}

func missingTable(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "no such table") || strings.Contains(msg, "does not exist")
}

func isDuplicate(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE") || strings.Contains(msg, "duplicate key")
}

func newStoreID() string {
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}
