package secret

import (
	"strings"
	"testing"

	"github.com/zhouyunchang/taboo/apps/server-go/internal/apperr"
)

func TestScanReferences(t *testing.T) {
	refs, err := scanReferences("plain ${HOME} ${dev:DB} and ${other:staging:TOKEN}")
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 2 || refs[0].env != "dev" || refs[0].key != "DB" || refs[0].project != "" {
		t.Fatalf("same-project ref: %+v", refs)
	}
	if refs[1].project != "other" || refs[1].env != "staging" || refs[1].key != "TOKEN" {
		t.Fatalf("cross-project ref: %+v", refs[1])
	}
	if _, err = scanReferences("${dev:}"); err == nil || err.Code != "REFERENCE_INVALID" {
		t.Fatalf("invalid segment: %v", err)
	}
	if _, err = scanReferences("${a:b:c:d}"); err == nil || err.Code != "REFERENCE_INVALID" {
		t.Fatalf("too many segments: %v", err)
	}
}

func TestExpandReferencesCycleDepthAndLiteral(t *testing.T) {
	values := map[string]string{
		"a/dev/A": "prefix ${dev:B}",
		"a/dev/B": "ok",
		"a/dev/C": "${dev:D}",
		"a/dev/D": "${dev:C}",
		"a/dev/E": "${other:dev:F}",
		"b/dev/F": "from-b",
	}
	load := func(projectID, projectSlug string, ref reference, stack []refFrame) (string, string, string, string, *apperr.Error) {
		ownerID := projectID
		if ref.project == "other" {
			ownerID = "b"
		} else if ref.project != "" {
			return "", "", "", "", missingRef(ref.raw)
		}
		identity := ownerID + "\x00" + ref.env + "\x00" + ref.key
		for _, frame := range stack {
			if frame.identity == identity {
				return "", "", "", "", apperr.New(400, "REFERENCE_CYCLE", "cycle")
			}
		}
		value, ok := values[ownerID+"/"+ref.env+"/"+ref.key]
		if !ok {
			return "", "", "", "", missingRef(ref.raw)
		}
		slug := projectSlug
		if ownerID == "b" {
			slug = "other"
		}
		return ownerID, slug, identity, value, nil
	}
	loads := 0
	got, err := expandRefs(values["a/dev/A"], "a", "default", nil, &loads, load)
	if err != nil || got != "prefix ok" {
		t.Fatalf("expand: %q %v", got, err)
	}
	loads = 0
	_, err = expandRefs(values["a/dev/C"], "a", "default", nil, &loads, load)
	if err == nil || err.Code != "REFERENCE_CYCLE" {
		t.Fatalf("cycle: %v", err)
	}
	loads = 0
	_, err = expandRefs("${dev:MISSING}", "a", "default", nil, &loads, load)
	if err == nil || err.Code != "REFERENCE_NOT_FOUND" || !strings.Contains(err.Message, "${dev:MISSING}") {
		t.Fatalf("missing: %v", err)
	}
	loads = 0
	got, err = expandRefs(values["a/dev/E"], "a", "default", nil, &loads, load)
	if err != nil || got != "from-b" {
		t.Fatalf("cross project: %q %v", got, err)
	}
	deep := "${dev:A}"
	for i := 0; i < maxRefDepth+1; i++ {
		values["a/dev/"+string(rune('A'+i))] = "${dev:" + string(rune('A'+i+1)) + "}"
	}
	values["a/dev/"+string(rune('A'+maxRefDepth+1))] = "end"
	loads = 0
	_, err = expandRefs("${dev:A}", "a", "default", nil, &loads, func(projectID, projectSlug string, ref reference, stack []refFrame) (string, string, string, string, *apperr.Error) {
		identity := "a\x00" + ref.env + "\x00" + ref.key
		value := values["a/"+ref.env+"/"+ref.key]
		if value == "" {
			return "", "", "", "", missingRef(ref.raw)
		}
		return "a", "default", identity, value, nil
	})
	if err == nil || err.Code != "REFERENCE_DEPTH" {
		t.Fatalf("depth: %v", err)
	}
	_ = deep
}
