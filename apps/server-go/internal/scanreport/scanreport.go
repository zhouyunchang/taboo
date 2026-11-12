// 扫描报告只保存规则、路径、行号和指纹。命中原文只在上传时用来计算组织 HMAC，不落库、不回显。
package scanreport

import (
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/apperr"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/auth"
	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/rbac"
)

var (
	ruleIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
	fingerprintRe = regexp.MustCompile(`^[0-9a-f]{64}$`)
	sourcePattern = regexp.MustCompile(`^[A-Za-z0-9_.:/@+-]{1,128}$`)
)

type Service struct {
	DB        *sql.DB
	MasterKey []byte
}

type Finding struct {
	RuleID      string `json:"rule_id"`
	Level       string `json:"level"`
	File        string `json:"file"`
	Line        int    `json:"line"`
	Fingerprint string `json:"fingerprint"`
	Alg         string `json:"alg"`
}

type findingIn struct {
	RuleID string `json:"rule_id"`
	Level  string `json:"level"`
	File   string `json:"file"`
	Line   int    `json:"line"`
	Match  string `json:"match"`
}

type Report struct {
	ID            string    `json:"id"`
	Source        string    `json:"source"`
	ActorName     string    `json:"actor_name"`
	FindingCount  int       `json:"finding_count"`
	CriticalCount int       `json:"critical_count"`
	WarningCount  int       `json:"warning_count"`
	CreatedAt     string    `json:"created_at"`
	Findings      []Finding `json:"findings,omitempty"`
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, e *apperr.Error) { writeJSON(w, e.Status, e) }

func (s *Service) Routes(r chi.Router) {
	r.Get("/", s.List)
	r.Post("/", s.Create)
	r.Get("/{id}", s.Get)
	r.Delete("/{id}", s.Delete)
}

// Fingerprint 是 HMAC-SHA256(组织密钥, rule|file|line|match)。密钥不离开服务端。
func Fingerprint(master []byte, orgID, rule, file string, line int, match string) string {
	mac := hmac.New(sha256.New, scanKey(master, orgID))
	mac.Write([]byte(rule))
	mac.Write([]byte{0})
	mac.Write([]byte(file))
	mac.Write([]byte{0})
	mac.Write([]byte(strconv.Itoa(line)))
	mac.Write([]byte{0})
	mac.Write([]byte(match))
	return hex.EncodeToString(mac.Sum(nil))
}

func scanKey(master []byte, orgID string) []byte {
	mac := hmac.New(sha256.New, master)
	mac.Write([]byte("taboo/scan-fingerprint/v1"))
	mac.Write([]byte{0})
	mac.Write([]byte(orgID))
	return mac.Sum(nil)
}

func (s *Service) authorize(w http.ResponseWriter, r *http.Request) (orgID, slug string, orgWide bool, ok bool) {
	slug = chi.URLParam(r, "slug")
	if err := s.DB.QueryRow(`SELECT id FROM orgs WHERE slug = ?`, slug).Scan(&orgID); err != nil {
		writeErr(w, apperr.NotFound)
		return "", slug, false, false
	}
	u := auth.From(r)
	if u == nil {
		writeErr(w, apperr.Forbidden)
		return "", slug, false, false
	}
	if u.Kind == auth.KindIdentity {
		var got string
		var listAll int
		err := s.DB.QueryRow(`SELECT org_id, list_scan_reports FROM machine_identities WHERE id = ? AND status = 'active'`, u.ID).Scan(&got, &listAll)
		if err != nil || got != orgID {
			writeErr(w, apperr.Forbidden)
			return "", slug, false, false
		}
		return orgID, slug, listAll == 1, true
	}
	if _, member := auth.Membership(s.DB, orgID, u.ID); !member {
		writeErr(w, apperr.Forbidden)
		return "", slug, false, false
	}
	return orgID, slug, true, true
}

func canDelete(db *sql.DB, r *http.Request, orgID string) bool {
	u := auth.From(r)
	if u == nil || u.Kind != auth.KindUser {
		return false
	}
	role, ok := auth.Membership(db, orgID, u.ID)
	return ok && rbac.Privileged(role)
}

func validFile(path string) bool {
	if path == "" || len(path) > 512 || strings.Contains(path, "\\") || strings.Contains(path, "\x00") {
		return false
	}
	if strings.HasPrefix(path, "/") || strings.Contains(path, "://") {
		return false
	}
	for _, part := range strings.Split(path, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return utf8.ValidString(path) && !strings.ContainsAny(path, "\r\n\t")
}

func validMatch(match string) bool {
	return match != "" && len(match) <= 2048 && utf8.ValidString(match) && !strings.Contains(match, "\x00")
}

func (s *Service) Create(w http.ResponseWriter, r *http.Request) {
	orgID, slug, _, ok := s.authorize(w, r)
	if !ok {
		return
	}
	if len(s.MasterKey) == 0 {
		writeErr(w, apperr.New(500, "INTERNAL", "internal error"))
		return
	}
	var b struct {
		Source   string      `json:"source"`
		Findings []findingIn `json:"findings"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&b); err != nil {
		writeErr(w, apperr.New(400, "INVALID", "scan report rejected"))
		return
	}
	if b.Source == "" {
		b.Source = "cli"
	}
	if !sourcePattern.MatchString(b.Source) {
		writeErr(w, apperr.New(400, "INVALID", "invalid scan source"))
		return
	}
	if len(b.Findings) > 2000 {
		writeErr(w, apperr.New(400, "INVALID", "too many findings"))
		return
	}
	stored := make([]Finding, len(b.Findings))
	critical, warning := 0, 0
	for i, f := range b.Findings {
		if !ruleIDPattern.MatchString(f.RuleID) || (f.Level != "critical" && f.Level != "warning") || !validFile(f.File) || f.Line < 1 || f.Line > 10_000_000 || !validMatch(f.Match) {
			writeErr(w, apperr.New(400, "INVALID", "finding must be rule id, level, relative file, line and match"))
			return
		}
		stored[i] = Finding{
			RuleID: f.RuleID, Level: f.Level, File: f.File, Line: f.Line, Alg: "hmac-sha256",
			Fingerprint: Fingerprint(s.MasterKey, orgID, f.RuleID, f.File, f.Line, f.Match),
		}
		if f.Level == "critical" {
			critical++
		} else {
			warning++
		}
	}
	raw, err := json.Marshal(stored)
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", "encode failed"))
		return
	}
	var round []map[string]any
	if err = json.Unmarshal(raw, &round); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", "encode failed"))
		return
	}
	for _, item := range round {
		if len(item) != 6 || item["alg"] != "hmac-sha256" {
			writeErr(w, apperr.New(400, "INVALID", "scan report rejected"))
			return
		}
	}
	if strings.Contains(string(raw), `"match"`) {
		writeErr(w, apperr.New(500, "INTERNAL", "encode failed"))
		return
	}
	u := auth.From(r)
	id := tc.NewID()
	tx, err := s.DB.Begin()
	if err != nil {
		internalErr(w, err)
		return
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`INSERT INTO scan_reports (id, org_id, created_by, actor_name, source, finding_count, critical_count, warning_count, findings_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, id, orgID, u.ID, u.Name, b.Source, len(stored), critical, warning, string(raw)); err != nil {
		internalErr(w, err)
		return
	}
	if err = auth.AuditReq(tx, r, orgID, u, "scan_report.create", "org/"+slug+"/scan-report/"+id,
		map[string]any{"findings": len(stored), "critical": critical, "warning": warning, "source": b.Source, "alg": "hmac-sha256"}); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", "audit failed"))
		return
	}
	if err = tx.Commit(); err != nil {
		internalErr(w, err)
		return
	}
	writeJSON(w, 201, Report{ID: id, Source: b.Source, ActorName: u.Name, FindingCount: len(stored), CriticalCount: critical, WarningCount: warning})
}

func (s *Service) List(w http.ResponseWriter, r *http.Request) {
	orgID, _, orgWide, ok := s.authorize(w, r)
	if !ok {
		return
	}
	query := `SELECT id, source, actor_name, finding_count, critical_count, warning_count, created_at
		FROM scan_reports WHERE org_id = ?`
	args := []any{orgID}
	if !orgWide {
		query += ` AND created_by = ?`
		args = append(args, auth.From(r).ID)
	}
	query += ` ORDER BY created_at DESC LIMIT 100`
	rows, err := s.DB.Query(query, args...)
	if err != nil {
		internalErr(w, err)
		return
	}
	defer rows.Close()
	out := []Report{}
	for rows.Next() {
		var item Report
		if err = rows.Scan(&item.ID, &item.Source, &item.ActorName, &item.FindingCount, &item.CriticalCount, &item.WarningCount, &item.CreatedAt); err != nil {
			internalErr(w, err)
			return
		}
		out = append(out, item)
	}
	writeJSON(w, 200, map[string]any{"reports": out, "can_delete": canDelete(s.DB, r, orgID)})
}

func (s *Service) Get(w http.ResponseWriter, r *http.Request) {
	orgID, _, orgWide, ok := s.authorize(w, r)
	if !ok {
		return
	}
	query := `SELECT id, source, actor_name, finding_count, critical_count, warning_count, created_at, findings_json, created_by
		FROM scan_reports WHERE org_id = ? AND id = ?`
	args := []any{orgID, chi.URLParam(r, "id")}
	var item Report
	var raw, createdBy string
	err := s.DB.QueryRow(query, args...).Scan(&item.ID, &item.Source, &item.ActorName, &item.FindingCount, &item.CriticalCount, &item.WarningCount, &item.CreatedAt, &raw, &createdBy)
	if err == sql.ErrNoRows {
		writeErr(w, apperr.NotFound)
		return
	}
	if err != nil {
		internalErr(w, err)
		return
	}
	if !orgWide && createdBy != auth.From(r).ID {
		writeErr(w, apperr.NotFound)
		return
	}
	item.Findings = []Finding{}
	if err = json.Unmarshal([]byte(raw), &item.Findings); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", "stored report is unreadable"))
		return
	}
	for i := range item.Findings {
		if item.Findings[i].Alg == "" {
			item.Findings[i].Alg = "sha256"
		}
		if !fingerprintRe.MatchString(item.Findings[i].Fingerprint) {
			writeErr(w, apperr.New(500, "INTERNAL", "stored report is unreadable"))
			return
		}
	}
	writeJSON(w, 200, item)
}

func (s *Service) Delete(w http.ResponseWriter, r *http.Request) {
	orgID, slug, _, ok := s.authorize(w, r)
	if !ok {
		return
	}
	if !canDelete(s.DB, r, orgID) {
		writeErr(w, apperr.Forbidden)
		return
	}
	id := chi.URLParam(r, "id")
	u := auth.From(r)
	tx, err := s.DB.Begin()
	if err != nil {
		internalErr(w, err)
		return
	}
	defer tx.Rollback()
	res, err := tx.Exec(`DELETE FROM scan_reports WHERE org_id = ? AND id = ?`, orgID, id)
	if err != nil {
		internalErr(w, err)
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		writeErr(w, apperr.NotFound)
		return
	}
	if err = auth.AuditReq(tx, r, orgID, u, "scan_report.delete", "org/"+slug+"/scan-report/"+id, nil); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", "audit failed"))
		return
	}
	if err = tx.Commit(); err != nil {
		internalErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"id": id, "deleted": true})
}

func internalErr(w http.ResponseWriter, err error) {
	slog.Error("scan report internal error", "err", err)
	writeErr(w, apperr.New(500, "INTERNAL", "internal error"))
}
