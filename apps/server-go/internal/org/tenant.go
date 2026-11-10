package org

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/zhouyunchang/taboo/apps/server-go/internal/apperr"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/auth"
	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/pathcode"
)

const (
	KindPersonal     = "personal"
	KindOrganization = "organization"
)

// ControlDB is the unscoped control connection. Creating a tenant inserts rows
// the caller's current organization scope cannot see yet.
func (s *Service) controlDB() *sql.DB {
	if s.ControlDB != nil {
		return s.ControlDB
	}
	return s.DB
}

func IsPersonal(q interface {
	QueryRow(string, ...any) *sql.Row
}, orgID string) (bool, error) {
	var kind string
	if err := q.QueryRow(`SELECT kind FROM orgs WHERE id=?`, orgID).Scan(&kind); err != nil {
		return false, err
	}
	return kind == KindPersonal, nil
}

func (s *Service) sharedOrg(w http.ResponseWriter, orgID string) bool {
	personal, err := IsPersonal(s.DB, orgID)
	if err != nil {
		writeErr(w, apperr.New(503, "STORAGE_UNAVAILABLE", "try again"))
		return false
	}
	if personal {
		writeErr(w, apperr.New(409, "PERSONAL_NAMESPACE", "personal spaces cannot be shared or federated"))
		return false
	}
	return true
}

func personalLabel(name, email string) string {
	name = strings.TrimSpace(name)
	if name != "" {
		return name
	}
	local, _, _ := strings.Cut(email, "@")
	if local == "" {
		return "个人"
	}
	return local
}

// provisionTenant creates a key tenant, its owner membership, and the default
// project environments. Personal spaces set owner_user_id; organizations do not.
func provisionTenant(tx *sql.Tx, masterKey []byte, name, slug, kind, ownerUserID string) (string, string, *apperr.Error) {
	if kind != KindPersonal && kind != KindOrganization {
		return "", "", apperr.InvalidInput
	}
	if strings.TrimSpace(name) == "" || ownerUserID == "" {
		return "", "", apperr.InvalidInput
	}
	if !pathcode.Valid(slug) {
		return "", "", apperr.New(400, "INVALID_CODE", "code must be 1-39 lowercase letters, digits, or inner hyphens")
	}
	var existing string
	err := tx.QueryRow(`SELECT id FROM orgs WHERE slug=?`, slug).Scan(&existing)
	if err == nil {
		return "", "", apperr.New(409, "CODE_TAKEN", "this code is already used")
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", "", apperr.New(500, "INTERNAL", err.Error())
	}
	orgID := tc.NewID()
	sealed, err := tc.Seal(masterKey, hexEncode(randomBytes(tc.KeyLen)), tc.DEKContext(orgID))
	if err != nil {
		return "", "", apperr.New(500, "INTERNAL", "DEK encryption failed")
	}
	var owner any
	if kind == KindPersonal {
		owner = ownerUserID
	}
	if kind == KindOrganization {
		if err = pathcode.Claim(tx, slug, pathcode.KindOrganization, orgID); err != nil {
			if errors.Is(err, pathcode.ErrTaken) || errors.Is(err, pathcode.ErrInvalid) {
				return "", "", apperr.New(409, "CODE_TAKEN", "this code is already used")
			}
			return "", "", apperr.New(500, "INTERNAL", err.Error())
		}
	}
	if _, err = tx.Exec(`INSERT INTO orgs (id, name, slug, dek_encrypted, kind, owner_user_id) VALUES (?, ?, ?, ?, ?, ?)`,
		orgID, name, slug, sealed, kind, owner); err != nil {
		if isUnique(err) {
			return "", "", apperr.New(409, "CODE_TAKEN", "this code is already used")
		}
		return "", "", apperr.New(500, "INTERNAL", err.Error())
	}
	if _, err = tx.Exec(`INSERT INTO org_members (org_id, user_id, role) VALUES (?, ?, 'owner')`, orgID, ownerUserID); err != nil {
		return "", "", apperr.New(500, "INTERNAL", err.Error())
	}
	projectID := tc.NewID()
	if _, err = tx.Exec(`INSERT INTO projects (id, org_id, name, slug) VALUES (?, ?, 'default', 'default')`, projectID, orgID); err != nil {
		return "", "", apperr.New(500, "INTERNAL", err.Error())
	}
	envID := tc.NewID()
	if _, err = tx.Exec(`INSERT INTO environments (id, project_id, name, slug, sort_order, protected, approval_required) VALUES (?, ?, 'dev', 'dev', 0, 0, 0)`,
		envID, projectID); err != nil {
		return "", "", apperr.New(500, "INTERNAL", err.Error())
	}
	if err = ensureRootFolder(tx, envID); err != nil {
		return "", "", apperr.New(503, "STORAGE_UNAVAILABLE", "change was not committed")
	}
	return orgID, slug, nil
}

// EnsurePersonalSpace gives a user the one personal tenant they are allowed to
// own. It does nothing when that tenant already exists.
func EnsurePersonalSpace(tx *sql.Tx, masterKey []byte, userID, email, name string) error {
	var id string
	err := tx.QueryRow(`SELECT id FROM orgs WHERE kind='personal' AND owner_user_id=?`, userID).Scan(&id)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	code, err := userHandle(tx, userID, email)
	if err != nil {
		return err
	}
	if id != "" {
		return nil
	}
	_, _, aerr := provisionTenant(tx, masterKey, personalLabel(name, email), code, KindPersonal, userID)
	if aerr != nil {
		return aerr
	}
	return nil
}

func userHandle(tx *sql.Tx, userID, email string) (string, error) {
	var handle string
	if err := tx.QueryRow(`SELECT COALESCE(handle,'') FROM users WHERE id=?`, userID).Scan(&handle); err != nil {
		return "", err
	}
	if handle != "" {
		if err := pathcode.Claim(tx, handle, pathcode.KindUser, userID); err != nil {
			return "", err
		}
		return handle, nil
	}
	seed := email
	if at := strings.IndexByte(email, '@'); at > 0 {
		seed = email[:at]
	}
	code, err := pathcode.ClaimNew(tx, seed, pathcode.KindUser, userID)
	if err != nil {
		return "", err
	}
	if _, err = tx.Exec(`UPDATE users SET handle=? WHERE id=?`, code, userID); err != nil {
		return "", err
	}
	return code, nil
}

// BackfillPersonalSpaces creates an empty personal space for every user who
// does not have one. Existing organization keys stay where they are.
func BackfillPersonalSpaces(db *sql.DB, masterKey []byte) error {
	rows, err := db.Query(`SELECT u.id, u.email, u.name FROM users u
		WHERE NOT EXISTS (SELECT 1 FROM orgs o WHERE o.kind='personal' AND o.owner_user_id=u.id)`)
	if err != nil {
		return err
	}
	type user struct{ id, email, name string }
	pending := []user{}
	for rows.Next() {
		var u user
		if err = rows.Scan(&u.id, &u.email, &u.name); err != nil {
			rows.Close()
			return err
		}
		pending = append(pending, u)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, u := range pending {
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		err = EnsurePersonalSpace(tx, masterKey, u.id, u.email, u.name)
		if err == nil {
			err = tx.Commit()
		}
		tx.Rollback()
		if err != nil && !isUnique(err) {
			return err
		}
	}
	return nil
}

func isUnique(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") || strings.Contains(msg, "duplicate key")
}

func (s *Service) CreateOrganization(w http.ResponseWriter, r *http.Request) {
	u := auth.From(r)
	if u == nil || u.Kind != auth.KindUser {
		writeErr(w, apperr.Forbidden)
		return
	}
	if u.OrganizationID != "" {
		writeErr(w, apperr.New(403, "FEDERATED_SESSION", "organization SSO sessions cannot create an organization"))
		return
	}
	var b struct {
		Name string `json:"name"`
		Code string `json:"code"`
	}
	if jsonDecode(r, &b) != nil {
		writeErr(w, apperr.InvalidInput)
		return
	}
	name := strings.TrimSpace(b.Name)
	if name == "" || len(name) > 80 {
		writeErr(w, apperr.InvalidInput)
		return
	}
	code := strings.ToLower(strings.TrimSpace(b.Code))
	if code == "" {
		code = slugify(name)
	}
	if !pathcode.Valid(code) {
		writeErr(w, apperr.New(400, "INVALID_CODE", "code must be 1-39 lowercase letters, digits, or inner hyphens"))
		return
	}
	tx, started := auth.BeginMutation(s.controlDB(), w, r)
	if !started {
		return
	}
	defer tx.Rollback()
	orgID, slug, aerr := provisionTenant(tx, s.MasterKey, name, code, KindOrganization, u.ID)
	if aerr != nil {
		writeErr(w, aerr)
		return
	}
	if !auth.CommitMutation(tx, w, r, orgID, u, "org.create", "org/"+slug, map[string]any{"name": name}) {
		return
	}
	writeJSON(w, 201, map[string]any{"id": orgID, "name": name, "slug": slug, "kind": KindOrganization, "role": "owner"})
}
