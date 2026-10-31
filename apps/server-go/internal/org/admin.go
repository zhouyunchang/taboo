package org

import (
	"database/sql"
	"errors"
	"strings"

	"github.com/zhouyunchang/taboo/apps/server-go/internal/auth"
	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
)

// 运维 CLI（taboo-server user ...）使用的本地账号管理。只作用于 auth_source='local'
// 的账号，走控制连接（与公开认证接口同一信任级别），每次变更与审计同事务提交。

var ErrUserNotFound = errors.New("local user not found")

// UserStatuses 是 CLI 允许设置的账号状态。
var UserStatuses = map[string]bool{"active": true, "disabled": true}

// AdminUserStatus 返回本地账号状态。
func AdminUserStatus(db *sql.DB, email string) (string, error) {
	var st string
	err := db.QueryRow(`SELECT status FROM users WHERE email = ? AND auth_source='local'`, normEmail(email)).Scan(&st)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrUserNotFound
	}
	return st, err
}

// AdminSetUserStatus 启用/停用本地账号；停用时同时吊销其 refresh token 与浏览器会话。
// 返回状态是否实际变化（幂等）。
func AdminSetUserStatus(db *sql.DB, email, status string) (bool, error) {
	if !UserStatuses[status] {
		return false, errors.New("status must be active or disabled")
	}
	email = normEmail(email)
	tx, err := db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var id, cur string
	if err := tx.QueryRow(`SELECT id, status FROM users WHERE email = ? AND auth_source='local'`, email).Scan(&id, &cur); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, ErrUserNotFound
		}
		return false, err
	}
	if cur == status {
		return false, nil
	}
	if _, err := tx.Exec(`UPDATE users SET status = ? WHERE id = ?`, status, id); err != nil {
		return false, err
	}
	if status != "active" {
		if _, err := tx.Exec(`DELETE FROM refresh_tokens WHERE user_id = ?`, id); err != nil {
			return false, err
		}
		if _, err := tx.Exec(`DELETE FROM browser_sessions WHERE user_id = ?`, id); err != nil {
			return false, err
		}
	}
	action := "admin.user.enable"
	if status != "active" {
		action = "admin.user.disable"
	}
	if err := auditUserOrgs(tx, id, action, email, map[string]any{"via": "cli", "from": cur, "to": status}); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

// AdminCreateUser 创建本地账号（含个人组织/默认项目/环境），供注册关闭时的运维建号。
func AdminCreateUser(db *sql.DB, masterKey []byte, email, name, password string, status string) (NewLocalUser, error) {
	email = normEmail(email)
	if !emailRe.MatchString(email) {
		return NewLocalUser{}, errors.New("invalid email")
	}
	if len(password) < 8 {
		return NewLocalUser{}, errors.New("password must be at least 8 chars")
	}
	if !UserStatuses[status] {
		return NewLocalUser{}, errors.New("status must be active or disabled")
	}
	var exists string
	if err := db.QueryRow(`SELECT id FROM users WHERE email = ?`, email).Scan(&exists); err == nil {
		return NewLocalUser{}, errors.New("email already registered")
	}
	hash, err := tc.HashPassword(password)
	if err != nil {
		return NewLocalUser{}, err
	}
	tx, err := db.Begin()
	if err != nil {
		return NewLocalUser{}, err
	}
	defer tx.Rollback()
	u, aerr := CreateLocalUser(tx, masterKey, email, name, hash)
	if aerr != nil {
		return NewLocalUser{}, aerr
	}
	if status != "active" {
		if _, err := tx.Exec(`UPDATE users SET status = ? WHERE id = ?`, status, u.UserID); err != nil {
			return NewLocalUser{}, err
		}
	}
	if err := auditUserOrgs(tx, u.UserID, "admin.user.create", email, map[string]any{"via": "cli", "status": status}); err != nil {
		return NewLocalUser{}, err
	}
	return u, tx.Commit()
}

func auditUserOrgs(tx *sql.Tx, userID, action, email string, meta map[string]any) error {
	rows, err := tx.Query(`SELECT org_id FROM org_members WHERE user_id = ?`, userID)
	if err != nil {
		return err
	}
	var orgs []string
	for rows.Next() {
		var o string
		if err := rows.Scan(&o); err != nil {
			rows.Close()
			return err
		}
		orgs = append(orgs, o)
	}
	rows.Close()
	if len(orgs) == 0 {
		orgs = []string{"-"}
	}
	for _, o := range orgs {
		if err := auth.Audit(tx, o, nil, action, "user/"+email, meta, "cli"); err != nil {
			return err
		}
	}
	return nil
}

func normEmail(e string) string { return strings.ToLower(strings.TrimSpace(e)) }
