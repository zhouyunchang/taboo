package secret

import "testing"

func TestParseDotenv(t *testing.T) {
	entries, err := parseDotenv("# comment\n\nexport FOO=bar\nBAR=\"a\\nb\"\nBAZ='q # not comment'\nFOO=second\nQUOTED=\"tail\" # note\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 4 {
		t.Fatalf("entries: %+v", entries)
	}
	if entries[0].Key != "FOO" || entries[0].Value != "second" {
		t.Fatalf("last duplicate wins: %+v", entries[0])
	}
	if entries[1].Value != "a\nb" || entries[2].Value != "q # not comment" || entries[3].Key != "QUOTED" || entries[3].Value != "tail" {
		t.Fatalf("quotes: %+v", entries)
	}
	if _, err = parseDotenv("=bad\n"); err == nil || err.Code != "INVALID_ENV" {
		t.Fatalf("invalid: %v", err)
	}
	if _, err = parseDotenv("GOOD=1\nBAD='unterminated\n"); err == nil {
		t.Fatal("unclosed quote accepted")
	}
	if _, err = parseDotenv("\n# only\n"); err == nil || err.Code != "INVALID_ENV" {
		t.Fatalf("empty: %v", err)
	}
}
