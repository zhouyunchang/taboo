// 定位符第一段：用户名和组织 code 共用的全局代号。
package pathcode

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"regexp"
	"strconv"
	"strings"
)

const (
	KindUser         = "user"
	KindOrganization = "organization"
)

var (
	ErrInvalid = errors.New("invalid code")
	ErrTaken   = errors.New("code taken")
)

var reserved = map[string]struct{}{
	"api": {}, "invite": {}, "login": {}, "settings": {}, "new": {},
}

var codePattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,37}[a-z0-9])?$`)

type execer interface {
	Exec(string, ...any) (sql.Result, error)
	QueryRow(string, ...any) *sql.Row
}

// ValidToken 报告一段路径代号的字符是否合法。项目代号用这一层。
func ValidToken(code string) bool {
	return codePattern.MatchString(code)
}

// Valid 报告 code 能否作为定位符第一段。用户名和组织 code 不能使用保留词。
func Valid(code string) bool {
	if _, blocked := reserved[code]; blocked {
		return false
	}
	return ValidToken(code)
}

// Slug 把任意输入收成可用的候选。收不成时返回 user。
func Slug(seed string) string {
	seed = strings.ToLower(strings.TrimSpace(seed))
	var b strings.Builder
	lastDash := false
	for _, r := range seed {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if ok {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash && b.Len() > 0 {
			b.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 39 {
		out = strings.Trim(out[:39], "-")
	}
	if !Valid(out) {
		return "user"
	}
	return out
}

// Claim 登记一个调用方指定的 code。同一归属重复登记视为成功。
func Claim(q execer, code, kind, ref string) error {
	if !Valid(code) || (kind != KindUser && kind != KindOrganization) || ref == "" {
		return ErrInvalid
	}
	var gotKind, gotRef string
	err := q.QueryRow(`SELECT kind, ref_id FROM path_codes WHERE code=?`, code).Scan(&gotKind, &gotRef)
	if err == nil {
		if gotKind == kind && gotRef == ref {
			return nil
		}
		return ErrTaken
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if _, err = q.Exec(`INSERT INTO path_codes(code, kind, ref_id) VALUES(?,?,?)`, code, kind, ref); err != nil {
		if isUnique(err) {
			return ErrTaken
		}
		return err
	}
	return nil
}

// ClaimNew 按种子分配一个尚未占用的 code 并登记。
func ClaimNew(q execer, seed, kind, ref string) (string, error) {
	base := Slug(seed)
	for i := 0; i < 20; i++ {
		code := base
		if i > 0 {
			suffix := "-" + strconv.Itoa(i+1)
			head := base
			if len(head)+len(suffix) > 39 {
				head = strings.Trim(head[:39-len(suffix)], "-")
			}
			code = head + suffix
		}
		err := Claim(q, code, kind, ref)
		if err == nil {
			return code, nil
		}
		if !errors.Is(err, ErrTaken) {
			return "", err
		}
	}
	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	code := "u" + hex.EncodeToString(buf)
	if err := Claim(q, code, kind, ref); err != nil {
		return "", err
	}
	return code, nil
}

// Release 归还确认前放弃的用户名。
func Release(q execer, code string) error {
	_, err := q.Exec(`DELETE FROM path_codes WHERE code=?`, code)
	return err
}

func isUnique(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE") || strings.Contains(msg, "duplicate key")
}
