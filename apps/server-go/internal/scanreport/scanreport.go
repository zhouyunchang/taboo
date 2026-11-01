// 扫描报告只保存规则、路径、行号和指纹。命中原文不得进入请求或数据库。
package scanreport

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/apperr"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/auth"
	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
)

var (
	ruleIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
	fingerprint   = regexp.MustCompile(`^[0-9a-f]{64}$`)
	sourcePattern = regexp.MustCompile(`^[A-Za-z0-9_.:/@+-]{1,128}$`)
)

type Service struct {
	DB *sql.DB
}

type Finding struct {
	RuleID      string `json:"rule_id"`
	Level       string `json:"level"`
	File        string `json:"file"`
	Line        int    `json:"line"`
	Fingerprint string `json:"fingerprint"`
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
}

func (s *Service) authorize(w http.ResponseWriter, r *http.Request) (orgID, slug string, ok bool) {
	slug = chi.URLParam(r, "slug")
	if err := s.DB.QueryRow(`SELECT id FROM orgs WHERE slug = ?`, slug).Scan(&orgID); err != nil {
		writeErr(w, apperr.NotFound)
		return "", slug, false
	}
	u := auth.From(r)
	if u == nil {
		writeErr(w, apperr.Forbidden)
		return "", slug, false
	}
	if u.Kind == auth.KindIdentity {
		var got string
		err := s.DB.QueryRow(`SELECT org_id FROM machine_identities WHERE id = ? AND status = 'active'`, u.ID).Scan(&got)
		if err == nil && got == orgID {
			return orgID, slug, true
		}
		writeErr(w, apperr.Forbidden)
		return "", slug, false
	}
	if _, member := auth.Membership(s.DB, orgID, u.ID); !member {
		writeErr(w, apperr.Forbidden)
		return "", slug, false
	}
	return orgID, slug, true
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

func (s *Service) Create(w http.ResponseWriter, r *http.Request) {
	orgID, slug, ok := s.authorize(w, r)
	if !ok {
		return
	}
	var b struct {
		Source   string    `json:"source"`
		Findings []Finding `json:"findings"`
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
	critical, warning := 0, 0
	for _, f := range b.Findings {
		if !ruleIDPattern.MatchString(f.RuleID) || (f.Level != "critical" && f.Level != "warning") || !validFile(f.File) || f.Line < 1 || f.Line > 10_000_000 || !fingerprint.MatchString(f.Fingerprint) {
			writeErr(w, apperr.New(400, "INVALID", "finding must be rule id, level, relative file, line and sha256 fingerprint"))
			return
		}
		if f.Level == "critical" {
			critical++
		} else {
			warning++
		}
	}
	raw, err := json.Marshal(b.Findings)
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", "encode failed"))
		return
	}
	// 二次确认：落库 JSON 只有允许的字段。
	var round []map[string]any
	if err = json.Unmarshal(raw, &round); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", "encode failed"))
		return
	}
	for _, item := range round {
		if len(item) != 5 {
			writeErr(w, apperr.New(400, "INVALID", "scan report rejected"))
			return
		}
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
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, id, orgID, u.ID, u.Name, b.Source, len(b.Findings), critical, warning, string(raw)); err != nil {
		internalErr(w, err)
		return
	}
	if err = auth.AuditReq(tx, r, orgID, u, "scan_report.create", "org/"+slug+"/scan-report/"+id,
		map[string]any{"findings": len(b.Findings), "critical": critical, "warning": warning, "source": b.Source}); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", "audit failed"))
		return
	}
	if err = tx.Commit(); err != nil {
		internalErr(w, err)
		return
	}
	writeJSON(w, 201, Report{ID: id, Source: b.Source, ActorName: u.Name, FindingCount: len(b.Findings), CriticalCount: critical, WarningCount: warning})
}

func (s *Service) List(w http.ResponseWriter, r *http.Request) {
	orgID, _, ok := s.authorize(w, r)
	if !ok {
		return
	}
	rows, err := s.DB.Query(`SELECT id, source, actor_name, finding_count, critical_count, warning_count, created_at
		FROM scan_reports WHERE org_id = ? ORDER BY created_at DESC LIMIT 100`, orgID)
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
	writeJSON(w, 200, map[string]any{"reports": out})
}

func (s *Service) Get(w http.ResponseWriter, r *http.Request) {
	orgID, _, ok := s.authorize(w, r)
	if !ok {
		return
	}
	var item Report
	var raw string
	err := s.DB.QueryRow(`SELECT id, source, actor_name, finding_count, critical_count, warning_count, created_at, findings_json
		FROM scan_reports WHERE org_id = ? AND id = ?`, orgID, chi.URLParam(r, "id")).
		Scan(&item.ID, &item.Source, &item.ActorName, &item.FindingCount, &item.CriticalCount, &item.WarningCount, &item.CreatedAt, &raw)
	if err == sql.ErrNoRows {
		writeErr(w, apperr.NotFound)
		return
	}
	if err != nil {
		internalErr(w, err)
		return
	}
	item.Findings = []Finding{}
	if err = json.Unmarshal([]byte(raw), &item.Findings); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", "stored report is unreadable"))
		return
	}
	writeJSON(w, 200, item)
}

// internalErr logs the cause server-side and returns a generic 500 (no DB details to clients).
func internalErr(w http.ResponseWriter, err error) {
	slog.Error("scan report internal error", "err", err)
	writeErr(w, apperr.New(500, "INTERNAL", "internal error"))
}
