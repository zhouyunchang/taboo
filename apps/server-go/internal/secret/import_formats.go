package secret

import (
	"encoding/json"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/zhouyunchang/taboo/apps/server-go/internal/apperr"
)

func parseImport(format, content string) ([]envEntry, *apperr.Error) {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "dotenv", "env":
		return parseDotenv(content)
	case "doppler":
		return parseDoppler(content)
	case "1password", "op":
		return parseOnePassword(content)
	default:
		return nil, apperr.New(400, "INVALID", "format must be dotenv, doppler, or 1password")
	}
}

func finishImport(order []string, byKey map[string]envEntry) ([]envEntry, *apperr.Error) {
	if len(order) == 0 {
		return nil, apperr.New(400, "INVALID_ENV", "import has no keys")
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

func remember(order *[]string, byKey map[string]envEntry, key, value string) {
	if _, seen := byKey[key]; !seen {
		*order = append(*order, key)
	}
	byKey[key] = envEntry{Key: key, Value: value}
}

func parseDoppler(content string) ([]envEntry, *apperr.Error) {
	if !utf8.ValidString(content) || len(content) > 1<<20 {
		return nil, apperr.New(400, "INVALID_ENV", "doppler export must be UTF-8 JSON under 1MB")
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(content), &raw); err != nil || raw == nil {
		return nil, apperr.New(400, "INVALID_ENV", "doppler export must be a JSON object")
	}
	if secrets, ok := raw["secrets"]; ok {
		var nested map[string]json.RawMessage
		if json.Unmarshal(secrets, &nested) != nil {
			return nil, apperr.New(400, "INVALID_ENV", "doppler secrets must be an object")
		}
		raw = nested
	}
	order := []string{}
	byKey := map[string]envEntry{}
	for key, msg := range raw {
		if strings.HasPrefix(key, "DOPPLER_") || !validKey(key) {
			continue
		}
		value, ok := dopplerValue(msg)
		if !ok {
			continue
		}
		remember(&order, byKey, key, value)
	}
	return finishImport(order, byKey)
}

func dopplerValue(msg json.RawMessage) (string, bool) {
	var text string
	if json.Unmarshal(msg, &text) == nil {
		return text, true
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(msg, &obj) != nil {
		return "", false
	}
	for _, name := range []string{"computed", "raw", "value"} {
		if part, ok := obj[name]; ok && json.Unmarshal(part, &text) == nil {
			return text, true
		}
	}
	return "", false
}

type opField struct {
	Label   string          `json:"label"`
	ID      string          `json:"id"`
	Value   json.RawMessage `json:"value"`
	Type    string          `json:"type"`
	Purpose string          `json:"purpose"`
}

type opItem struct {
	Title  string    `json:"title"`
	Fields []opField `json:"fields"`
}

func parseOnePassword(content string) ([]envEntry, *apperr.Error) {
	if !utf8.ValidString(content) || len(content) > 1<<20 {
		return nil, apperr.New(400, "INVALID_ENV", "1password export must be UTF-8 JSON under 1MB")
	}
	items, err := opItems([]byte(content))
	if err != nil {
		return nil, apperr.New(400, "INVALID_ENV", "1password export must be op JSON")
	}
	prefix := len(items) > 1
	order := []string{}
	byKey := map[string]envEntry{}
	for _, item := range items {
		for _, field := range item.Fields {
			if strings.EqualFold(field.Type, "SECTION") || strings.EqualFold(field.Purpose, "NOTES") {
				continue
			}
			var value string
			if json.Unmarshal(field.Value, &value) != nil || value == "" {
				continue
			}
			label := field.Label
			if label == "" {
				label = field.ID
			}
			key, ok := envKey(label)
			if !ok {
				continue
			}
			if prefix {
				if title, ok := envKey(item.Title); ok {
					key = title + "__" + key
				}
			}
			if !validKey(key) {
				continue
			}
			remember(&order, byKey, key, value)
		}
	}
	return finishImport(order, byKey)
}

func opItems(body []byte) ([]opItem, error) {
	var list []opItem
	if json.Unmarshal(body, &list) == nil && len(list) > 0 {
		return list, nil
	}
	var wrapped struct {
		Items []opItem `json:"items"`
	}
	if json.Unmarshal(body, &wrapped) == nil && len(wrapped.Items) > 0 {
		return wrapped.Items, nil
	}
	var one opItem
	if err := json.Unmarshal(body, &one); err != nil || len(one.Fields) == 0 {
		return nil, err
	}
	return []opItem{one}, nil
}

func envKey(label string) (string, bool) {
	var b strings.Builder
	prevUnderscore := false
	for _, r := range label {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(unicode.ToUpper(r))
			prevUnderscore = false
		case r >= 'A' && r <= 'Z' || r >= '0' && r <= '9':
			b.WriteRune(r)
			prevUnderscore = false
		default:
			if b.Len() > 0 && !prevUnderscore {
				b.WriteByte('_')
				prevUnderscore = true
			}
		}
	}
	key := strings.Trim(b.String(), "_")
	if key == "" || !validKey(key) {
		return "", false
	}
	return key, true
}
