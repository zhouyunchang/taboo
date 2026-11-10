package secret

import (
	"database/sql"
	"encoding/json"
	"net/http"

	"github.com/zhouyunchang/taboo/apps/server-go/internal/apperr"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/auth"
	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/folder"
)

type importEntry struct {
	Key     string `json:"key"`
	Action  string `json:"action"`
	Version int    `json:"version"`
}

type importResult struct {
	Preview   bool          `json:"preview"`
	Strategy  string        `json:"strategy,omitempty"`
	Created   int           `json:"created"`
	Updated   int           `json:"updated"`
	Skipped   int           `json:"skipped"`
	Conflicts []string      `json:"conflicts"`
	Entries   []importEntry `json:"entries"`
}

type queryRower interface {
	QueryRow(string, ...any) *sql.Row
}

func existingSecret(q queryRower, envID, folderID, key string) (id string, version int, ok bool, err error) {
	err = q.QueryRow(`SELECT id, latest_version FROM secrets WHERE env_id = ? AND folder_id = ? AND key = ?`, envID, folderID, key).Scan(&id, &version)
	if err == sql.ErrNoRows {
		return "", 0, false, nil
	}
	if err != nil {
		return "", 0, false, err
	}
	return id, version, true, nil
}

func planEntry(strategy string, key string, version int, exists bool) importEntry {
	if !exists {
		return importEntry{Key: key, Action: "create", Version: 1}
	}
	switch strategy {
	case "skip":
		return importEntry{Key: key, Action: "skip", Version: version}
	case "overwrite":
		return importEntry{Key: key, Action: "overwrite", Version: version + 1}
	default:
		return importEntry{Key: key, Action: "conflict", Version: version}
	}
}

func summarize(preview bool, strategy string, entries []importEntry) importResult {
	out := importResult{Preview: preview, Strategy: strategy, Conflicts: []string{}, Entries: entries}
	for _, e := range entries {
		switch e.Action {
		case "create":
			out.Created++
		case "overwrite":
			out.Updated++
		case "skip":
			out.Skipped++
		case "conflict":
			out.Conflicts = append(out.Conflicts, e.Key)
		}
	}
	return out
}

// POST /import?env=&path=  导入 .env。preview 只返回计划；冲突必须显式 skip 或 overwrite。
func (s *Service) Import(w http.ResponseWriter, r *http.Request) {
	u := auth.From(r)
	p := projectOf(r)
	envSlug := q(r, "env", "dev")
	if !auth.Require(s.DB, w, r, p.OrgID, p.ID, "write", envSlug) {
		return
	}
	envID, err := s.envOf(p.ID, envSlug)
	if err != nil {
		writeErr(w, apperr.EnvNotFound)
		return
	}
	var b struct {
		Content  string `json:"content"`
		Strategy string `json:"strategy"`
		Preview  bool   `json:"preview"`
		Format   string `json:"format"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20+4096)).Decode(&b); err != nil {
		writeErr(w, apperr.InvalidInput)
		return
	}
	if b.Strategy != "" && b.Strategy != "skip" && b.Strategy != "overwrite" {
		writeErr(w, apperr.New(400, "INVALID", "strategy must be skip or overwrite"))
		return
	}
	entries, aerr := parseImport(b.Format, b.Content)
	if aerr != nil {
		writeErr(w, aerr)
		return
	}
	normPath, err := folder.NormalizePath(q(r, "path", "/"))
	if err != nil {
		writeErr(w, apperr.New(400, "INVALID", "invalid folder path"))
		return
	}
	var folderID string
	if b.Preview {
		folderID, err = folder.ResolveID(s.DB, envID, normPath, false)
		if err != nil {
			if isNotFound(err) {
				planned := make([]importEntry, len(entries))
				for i, e := range entries {
					planned[i] = importEntry{Key: e.Key, Action: "create", Version: 1}
				}
				writeJSON(w, 200, summarize(true, b.Strategy, planned))
				return
			}
			internalErr(w, err)
			return
		}
		planned := make([]importEntry, len(entries))
		for i, e := range entries {
			_, version, exists, err := existingSecret(s.DB, envID, folderID, e.Key)
			if err != nil {
				internalErr(w, err)
				return
			}
			planned[i] = planEntry(b.Strategy, e.Key, version, exists)
		}
		writeJSON(w, 200, summarize(true, b.Strategy, planned))
		return
	}
	required, err := s.needsApproval(envID)
	if err != nil {
		writeErr(w, apperr.New(503, "STORAGE_UNAVAILABLE", "try again"))
		return
	}
	if required {
		writeErr(w, apperr.New(409, "APPROVAL_REQUIRED", "this environment requires approval; import individual changes or turn approval off"))
		return
	}
	if s.checkReplay(w, r, p.OrgID, b) {
		return
	}
	dek, err := s.orgDEK(p.OrgID)
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", "dek unwrap failed"))
		return
	}
	tx, err := s.DB.Begin()
	if err != nil {
		internalErr(w, err)
		return
	}
	defer tx.Rollback()
	if s.replayMutation(tx, w, r, p.OrgID, b) {
		return
	}
	folderID, err = folder.ResolveID(tx, envID, normPath, true)
	if err != nil {
		internalErr(w, err)
		return
	}
	type current struct {
		id      string
		version int
		exists  bool
		item    importEntry
		value   string
	}
	state := make([]current, len(entries))
	var conflicts []string
	for i, e := range entries {
		secretID, version, exists, err := existingSecret(tx, envID, folderID, e.Key)
		if err != nil {
			internalErr(w, err)
			return
		}
		item := planEntry(b.Strategy, e.Key, version, exists)
		state[i] = current{id: secretID, version: version, exists: exists, item: item, value: e.Value}
		if item.Action == "conflict" {
			conflicts = append(conflicts, e.Key)
		}
	}
	if len(conflicts) > 0 {
		writeErr(w, apperr.New(409, "IMPORT_CONFLICT", "existing keys need strategy skip or overwrite").WithDetails(map[string]any{"conflicts": conflicts}))
		return
	}
	planned := make([]importEntry, len(state))
	for i, cur := range state {
		planned[i] = cur.item
		if cur.item.Action == "skip" {
			continue
		}
		secretID := cur.id
		version := cur.version
		next := 1
		eventAction := "created"
		if !cur.exists {
			secretID = tc.NewID()
			if _, err = tx.Exec(`INSERT INTO secrets (id, env_id, project_id, folder, folder_id, key, comment, tags) VALUES (?, ?, (SELECT project_id FROM environments WHERE id=?), ?, ?, ?, '', '[]')`,
				secretID, envID, envID, normPath, folderID, cur.item.Key); err != nil {
				internalErr(w, err)
				return
			}
		} else {
			next = version + 1
			eventAction = "updated"
		}
		ct, err := tc.Seal(dek, cur.value, tc.SecretContext(p.OrgID, p.ID, envID, secretID, next))
		if err != nil {
			writeErr(w, apperr.New(500, "INTERNAL", "encrypt failed"))
			return
		}
		if res, err := tx.Exec(`UPDATE secrets SET latest_version = ?, updated_at = datetime('now') WHERE id = ? AND latest_version = ?`, next, secretID, version); !updatedOne(res, err) {
			writeErr(w, apperr.New(409, "VERSION_CONFLICT", "secret changed; reload before retrying"))
			return
		}
		if _, err = tx.Exec(`INSERT INTO secret_versions (id, secret_id, version, ciphertext, created_by) VALUES (?, ?, ?, ?, ?)`,
			tc.NewID(), secretID, next, ct, u.ID); err != nil {
			internalErr(w, err)
			return
		}
		if _, err = s.enqueue(tx, r, p, envSlug, normPath, cur.item.Key, next, eventAction, secretID, envID); err != nil {
			internalErr(w, err)
			return
		}
	}
	result := summarize(false, b.Strategy, planned)
	keys := make([]string, len(planned))
	for i, e := range planned {
		keys[i] = e.Key
	}
	if err := auth.AuditReq(tx, r, p.OrgID, u, "secrets.import", "project/"+p.Slug+"/env/"+envSlug,
		map[string]any{"strategy": b.Strategy, "created": result.Created, "updated": result.Updated, "skipped": result.Skipped, "keys": keys}); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", "audit failed"))
		return
	}
	if err := saveMutation(tx, r, p.OrgID, result); err != nil {
		writeErr(w, apperr.New(503, "MUTATION_UNAVAILABLE", "retry with the same idempotency key"))
		return
	}
	if err := tx.Commit(); err != nil {
		internalErr(w, err)
		return
	}
	writeJSON(w, 200, result)
}

func isNotFound(err error) bool {
	ae, ok := err.(*apperr.Error)
	return ok && ae != nil && ae.Status == 404
}
