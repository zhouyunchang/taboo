package secret

import (
	"strings"
	"unicode/utf8"

	"github.com/zhouyunchang/taboo/apps/server-go/internal/apperr"
)

const maxImportEntries = 500

type envEntry struct {
	Key   string
	Value string
	Line  int
}

type envLineError struct {
	Line    int    `json:"line"`
	Message string `json:"message"`
}

// parseDotenv 解析单行 KEY=VALUE。.env 注释、export 前缀、单双引号。
// 同一文件里重复的 key 以后出现的为准。非法行整批拒绝，避免部分写入。
func parseDotenv(content string) ([]envEntry, *apperr.Error) {
	if !utf8.ValidString(content) {
		return nil, apperr.New(400, "INVALID_ENV", ".env must be UTF-8")
	}
	content = strings.TrimPrefix(content, "\uFEFF")
	if len(content) > 1<<20 {
		return nil, apperr.New(400, "INVALID_ENV", ".env is larger than 1MB")
	}
	var problems []envLineError
	order := []string{}
	byKey := map[string]envEntry{}
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	for i, line := range lines {
		n := i + 1
		raw := strings.TrimRight(line, "\r")
		trim := strings.TrimSpace(raw)
		if trim == "" || strings.HasPrefix(trim, "#") {
			continue
		}
		body := trim
		if strings.HasPrefix(body, "export ") {
			body = strings.TrimSpace(strings.TrimPrefix(body, "export "))
		}
		eq := strings.IndexByte(body, '=')
		if eq <= 0 {
			problems = append(problems, envLineError{Line: n, Message: "expected KEY=VALUE"})
			continue
		}
		key := body[:eq]
		val := body[eq+1:]
		if !validKey(key) {
			problems = append(problems, envLineError{Line: n, Message: "invalid key"})
			continue
		}
		parsed, ok := unquoteEnv(val)
		if !ok {
			problems = append(problems, envLineError{Line: n, Message: "unclosed quote"})
			continue
		}
		if _, seen := byKey[key]; !seen {
			order = append(order, key)
		}
		byKey[key] = envEntry{Key: key, Value: parsed, Line: n}
	}
	if len(problems) > 0 {
		return nil, apperr.New(400, "INVALID_ENV", "invalid .env").WithDetails(map[string]any{"lines": problems})
	}
	if len(order) == 0 {
		return nil, apperr.New(400, "INVALID_ENV", ".env has no keys")
	}
	if len(order) > maxImportEntries {
		return nil, apperr.New(400, "INVALID_ENV", "too many keys")
	}
	out := make([]envEntry, len(order))
	for i, key := range order {
		out[i] = byKey[key]
	}
	return out, nil
}

func unquoteEnv(val string) (string, bool) {
	if val == "" {
		return "", true
	}
	if val[0] != '"' && val[0] != '\'' {
		if hash := strings.Index(val, " #"); hash >= 0 {
			val = strings.TrimSpace(val[:hash])
		}
		return val, true
	}
	quote := val[0]
	var b strings.Builder
	closed := false
	for i := 1; i < len(val); i++ {
		if val[i] == quote && (quote == '\'' || val[i-1] != '\\') {
			// 结束引号之后只允许空白或行内注释。
			rest := strings.TrimSpace(val[i+1:])
			if rest != "" && !strings.HasPrefix(rest, "#") {
				return "", false
			}
			closed = true
			break
		}
		if quote == '\'' || val[i] != '\\' {
			b.WriteByte(val[i])
			continue
		}
		if i+1 >= len(val) {
			return "", false
		}
		switch val[i+1] {
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 't':
			b.WriteByte('\t')
		default:
			b.WriteByte(val[i+1])
		}
		i++
	}
	if !closed {
		return "", false
	}
	return b.String(), true
}
