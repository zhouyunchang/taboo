package secret

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/apperr"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/auth"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/locator"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/project"
)

// Access 用定位符访问目录或其中一把密钥。密钥名来自查询参数或正文，不在路径里。
func (s *Service) Access(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.Query().Get("locator")
	if r.Method == http.MethodPut {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeErr(w, apperr.InvalidInput)
			return
		}
		var probe struct {
			Locator string `json:"locator"`
		}
		_ = json.Unmarshal(body, &probe)
		if raw == "" {
			raw = probe.Locator
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
	}
	loc, err := locator.Parse(raw)
	if err != nil {
		writeErr(w, apperr.New(400, "INVALID_LOCATOR", "locator must be owner/project or owner/project/folder"))
		return
	}
	target, aerr := s.locate(r, loc)
	if aerr != nil {
		writeErr(w, aerr)
		return
	}
	r = r.WithContext(project.With(r.Context(), target.project))
	query := r.URL.Query()
	query.Set("env", "dev")
	query.Set("path", target.folder)
	r.URL.RawQuery = query.Encode()
	switch r.Method {
	case http.MethodPut:
		s.Upsert(w, r)
	case http.MethodDelete:
		key := query.Get("key")
		if key == "" {
			writeErr(w, apperr.New(400, "INVALID", "key is required"))
			return
		}
		s.Delete(w, withKeyParam(r, key))
	default:
		if key := query.Get("key"); key != "" {
			s.Reveal(w, withKeyParam(r, key))
			return
		}
		s.accessList(w, r, target)
	}
}

type located struct {
	project project.Ctx
	folder  string
}

func (s *Service) locate(r *http.Request, loc locator.Location) (located, *apperr.Error) {
	orgID, err := orgByCode(s.DB, loc.Owner)
	if err != nil {
		return located{}, apperr.New(404, "LOCATOR_NOT_FOUND", "locator was not found")
	}
	actor := auth.From(r)
	if actor == nil {
		return located{}, apperr.Unauthorized
	}
	if actor.Kind == auth.KindUser {
		if _, ok := auth.Membership(s.DB, orgID, actor.ID); !ok {
			return located{}, apperr.Forbidden
		}
	} else if actor.OrganizationID != "" && actor.OrganizationID != orgID {
		return located{}, apperr.Forbidden
	}
	var projectID, slug string
	err = s.DB.QueryRow(`SELECT id, slug FROM projects WHERE org_id=? AND slug=?`, orgID, loc.Project).Scan(&projectID, &slug)
	if err != nil {
		return located{}, apperr.New(404, "LOCATOR_NOT_FOUND", "locator was not found")
	}
	if !auth.ProjectVisible(s.DB, actor, orgID, projectID) {
		return located{}, apperr.Forbidden
	}
	return located{project: project.Ctx{ID: projectID, OrgID: orgID, Slug: slug}, folder: loc.Folder}, nil
}

func orgByCode(db *sql.DB, code string) (string, error) {
	var kind, ref string
	err := db.QueryRow(`SELECT kind, ref_id FROM path_codes WHERE code=?`, code).Scan(&kind, &ref)
	if err == nil {
		if kind == "organization" {
			return ref, nil
		}
		var id string
		err = db.QueryRow(`SELECT id FROM orgs WHERE kind='personal' AND owner_user_id=?`, ref).Scan(&id)
		return id, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	var id string
	err = db.QueryRow(`SELECT id FROM orgs WHERE slug=?`, code).Scan(&id)
	return id, err
}

func (s *Service) accessList(w http.ResponseWriter, r *http.Request, target located) {
	envSlug := "dev"
	if !auth.Require(s.DB, w, r, target.project.OrgID, target.project.ID, "read", envSlug) {
		return
	}
	envID, err := s.envOf(target.project.ID, envSlug)
	if err != nil {
		writeErr(w, apperr.EnvNotFound)
		return
	}
	folderID, norm, ferr := s.folderRef(envID, target.folder, false)
	if ferr != nil {
		writeErr(w, apperr.New(404, "LOCATOR_NOT_FOUND", "locator was not found"))
		return
	}
	folders := []map[string]string{}
	rows, err := s.DB.Query(`SELECT name, path FROM folders WHERE env_id=? AND parent_id=? ORDER BY name`, envID, folderID)
	if err != nil {
		internalErr(w, err)
		return
	}
	for rows.Next() {
		var name, path string
		if err = rows.Scan(&name, &path); err != nil {
			rows.Close()
			internalErr(w, err)
			return
		}
		folders = append(folders, map[string]string{"name": name, "path": path})
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		internalErr(w, err)
		return
	}
	rows.Close()
	secrets := []Meta{}
	rows, err = s.DB.Query(`SELECT id, folder, key, comment, tags, latest_version, updated_at FROM secrets WHERE env_id=? AND folder_id=? ORDER BY key`, envID, folderID)
	if err != nil {
		internalErr(w, err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var item Meta
		var tags string
		if err = rows.Scan(&item.ID, &item.Folder, &item.Key, &item.Comment, &tags, &item.Version, &item.Updated); err != nil {
			internalErr(w, err)
			return
		}
		_ = json.Unmarshal([]byte(tags), &item.Tags)
		secrets = append(secrets, item)
	}
	writeJSON(w, 200, map[string]any{"folder": norm, "folders": folders, "secrets": secrets})
}

func withKeyParam(r *http.Request, key string) *http.Request {
	ctx := r.Context()
	rc := chi.RouteContext(ctx)
	if rc == nil {
		rc = chi.NewRouteContext()
		ctx = context.WithValue(ctx, chi.RouteCtxKey, rc)
	}
	rc.URLParams.Add("key", key)
	return r.WithContext(ctx)
}
