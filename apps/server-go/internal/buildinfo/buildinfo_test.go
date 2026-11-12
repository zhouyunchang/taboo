package buildinfo

import "testing"

func TestGetDefaults(t *testing.T) {
	i := Get()
	if i.Version == "" || i.Commit == "" {
		t.Fatalf("empty build info: %+v", i)
	}
	if i.String() == "" {
		t.Fatal("empty string form")
	}
}
