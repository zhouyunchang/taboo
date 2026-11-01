// 密钥引用：值里保存字面量，只在 reveal / export 且 resolve=true 时展开。
// 同项目 ${env:KEY}，跨项目 ${project:env:KEY}。目标一律是该环境根目录下的 key。
package secret

import (
	"database/sql"
	"net/http"
	"strings"

	"github.com/zhouyunchang/taboo/apps/server-go/internal/apperr"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/auth"
	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
)

const (
	maxRefDepth = 8
	maxRefLoads = 64
	maxRefBytes = 1 << 20
)

type reference struct {
	raw     string
	project string // 空表示“当前这条值所属的项目”
	env     string
	key     string
	start   int
	end     int
}

type refFrame struct {
	identity string
	raw      string
}

// scanReferences 找出值中的引用。没有冒号的 ${...} 视为普通文本。
// 带冒号但段数或字符不合法时返回 REFERENCE_INVALID，避免静默留下未展开的片段。
func scanReferences(value string) ([]reference, *apperr.Error) {
	var out []reference
	for i := 0; i < len(value); {
		start := strings.Index(value[i:], "${")
		if start < 0 {
			break
		}
		start += i
		end := strings.IndexByte(value[start:], '}')
		if end < 0 {
			break
		}
		end += start
		body := value[start+2 : end]
		i = end + 1
		if !strings.Contains(body, ":") {
			continue
		}
		parts := strings.Split(body, ":")
		if len(parts) != 2 && len(parts) != 3 {
			return nil, apperr.New(400, "REFERENCE_INVALID", "secret reference must be ${env:KEY} or ${project:env:KEY}: "+value[start:end+1])
		}
		for _, p := range parts {
			if !validKey(p) {
				return nil, apperr.New(400, "REFERENCE_INVALID", "secret reference has an invalid segment: "+value[start:end+1])
			}
		}
		ref := reference{raw: value[start : end+1], env: parts[0], key: parts[1], start: start, end: end + 1}
		if len(parts) == 3 {
			ref.project, ref.env, ref.key = parts[0], parts[1], parts[2]
		}
		out = append(out, ref)
	}
	return out, nil
}

func referenceLiterals(value string) []string {
	refs, err := scanReferences(value)
	if err != nil || len(refs) == 0 {
		return nil
	}
	out := make([]string, len(refs))
	for i, ref := range refs {
		out[i] = ref.raw
	}
	return out
}

type refLoader func(projectID, projectSlug string, ref reference, stack []refFrame) (ownerID, ownerSlug, identity, plaintext string, err *apperr.Error)

func expandRefs(value, projectID, projectSlug string, stack []refFrame, loads *int, load refLoader) (string, *apperr.Error) {
	refs, err := scanReferences(value)
	if err != nil {
		return "", err
	}
	if len(refs) == 0 {
		return value, nil
	}
	var b strings.Builder
	last := 0
	for _, ref := range refs {
		if len(stack) >= maxRefDepth {
			return "", apperr.New(400, "REFERENCE_DEPTH", "secret reference depth exceeded (max 8)").WithDetails(map[string]any{"reference": ref.raw, "max": maxRefDepth})
		}
		*loads++
		if *loads > maxRefLoads {
			return "", apperr.New(400, "REFERENCE_LIMIT", "too many secret references in one value")
		}
		b.WriteString(value[last:ref.start])
		ownerID, ownerSlug, identity, plain, err := load(projectID, projectSlug, ref, stack)
		if err != nil {
			return "", err
		}
		inner, err := expandRefs(plain, ownerID, ownerSlug, append(stack, refFrame{identity: identity, raw: ref.raw}), loads, load)
		if err != nil {
			return "", err
		}
		if b.Len()+len(inner) > maxRefBytes {
			return "", apperr.New(400, "REFERENCE_LIMIT", "resolved secret is too large")
		}
		b.WriteString(inner)
		last = ref.end
	}
	b.WriteString(value[last:])
	if b.Len() > maxRefBytes {
		return "", apperr.New(400, "REFERENCE_LIMIT", "resolved secret is too large")
	}
	return b.String(), nil
}

func queryBool(r *http.Request, name string) bool {
	switch strings.ToLower(strings.TrimSpace(r.URL.Query().Get(name))) {
	case "1", "true", "yes":
		return true
	default:
		return false
	}
}

func (s *Service) resolveStored(r *http.Request, orgID string, project ProjectCtx, raw string) (string, *apperr.Error) {
	loads := 0
	return expandRefs(raw, project.ID, project.Slug, nil, &loads, func(projectID, projectSlug string, ref reference, stack []refFrame) (string, string, string, string, *apperr.Error) {
		return s.dereference(r, orgID, projectID, projectSlug, ref, stack)
	})
}

func (s *Service) dereference(r *http.Request, orgID, projectID, projectSlug string, ref reference, stack []refFrame) (ownerID, ownerSlug, identity, plaintext string, err *apperr.Error) {
	ownerID, ownerSlug = projectID, projectSlug
	if ref.project != "" {
		var id, slug string
		qerr := s.DB.QueryRow(`SELECT id, slug FROM projects WHERE org_id = ? AND slug = ?`, orgID, ref.project).Scan(&id, &slug)
		if qerr == sql.ErrNoRows {
			return "", "", "", "", missingRef(ref.raw)
		}
		if qerr != nil {
			return "", "", "", "", apperr.New(500, "INTERNAL", "reference lookup failed")
		}
		ownerID, ownerSlug = id, slug
	}
	actor := auth.From(r)
	if !auth.ProjectVisible(s.DB, actor, orgID, ownerID) || !auth.Can(s.DB, actor, orgID, ownerID, "reveal", ref.env) {
		_ = auth.AuditReq(s.DB, r, orgID, actor, "secrets.reference_denied",
			"project/"+ownerSlug+"/env/"+ref.env+"/secret/"+ref.key,
			map[string]any{"reference": ref.raw, "reason": "forbidden"})
		return "", "", "", "", apperr.New(403, "REFERENCE_FORBIDDEN", "no reveal access for "+ref.raw).WithDetails(map[string]any{"reference": ref.raw})
	}
	identity = ownerID + "\x00" + ref.env + "\x00" + ref.key
	for _, frame := range stack {
		if frame.identity == identity {
			chain := make([]string, 0, len(stack)+1)
			for _, f := range stack {
				chain = append(chain, f.raw)
			}
			chain = append(chain, ref.raw)
			return "", "", "", "", apperr.New(400, "REFERENCE_CYCLE", "secret reference cycle: "+strings.Join(chain, " -> ")).WithDetails(map[string]any{"chain": chain})
		}
	}
	envID, qerr := s.envOf(ownerID, ref.env)
	if qerr != nil {
		return "", "", "", "", missingRef(ref.raw)
	}
	var folderID string
	qerr = s.DB.QueryRow(`SELECT id FROM folders WHERE env_id = ? AND path = '/'`, envID).Scan(&folderID)
	if qerr != nil {
		return "", "", "", "", missingRef(ref.raw)
	}
	var secretID string
	var version int
	qerr = s.DB.QueryRow(`SELECT id, latest_version FROM secrets WHERE env_id = ? AND folder_id = ? AND key = ?`, envID, folderID, ref.key).Scan(&secretID, &version)
	if qerr == sql.ErrNoRows {
		return "", "", "", "", missingRef(ref.raw)
	}
	if qerr != nil {
		return "", "", "", "", apperr.New(500, "INTERNAL", "reference lookup failed")
	}
	var ct string
	if qerr = s.DB.QueryRow(`SELECT ciphertext FROM secret_versions WHERE secret_id = ? AND version = ?`, secretID, version).Scan(&ct); qerr != nil {
		return "", "", "", "", missingRef(ref.raw)
	}
	dek, qerr := s.orgDEK(orgID)
	if qerr != nil {
		return "", "", "", "", apperr.New(500, "INTERNAL", "dek unwrap failed")
	}
	plaintext, qerr = tc.Open(dek, ct, tc.SecretContext(orgID, ownerID, envID, secretID, version))
	if qerr != nil {
		return "", "", "", "", apperr.New(500, "INTERNAL", "decrypt failed")
	}
	if qerr = auth.AuditReq(s.DB, r, orgID, actor, "secrets.reference",
		"project/"+ownerSlug+"/env/"+ref.env+"/secret/"+ref.key,
		map[string]any{"reference": ref.raw, "version": version}); qerr != nil {
		return "", "", "", "", apperr.New(500, "INTERNAL", "audit failed")
	}
	return ownerID, ownerSlug, identity, plaintext, nil
}

func missingRef(raw string) *apperr.Error {
	return apperr.New(404, "REFERENCE_NOT_FOUND", "secret reference not found: "+raw).WithDetails(map[string]any{"reference": raw})
}
