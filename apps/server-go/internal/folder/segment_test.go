package folder

import "testing"

func TestNormalizePathRejectsSpaces(t *testing.T) {
	for _, bad := range []string{" /a b/", "/a b/", "/te st/", "/a/\tb/", "/a\u00a0b/"} {
		if got, err := NormalizePath(bad); err == nil {
			t.Fatalf("accepted %q as %q", bad, got)
		}
	}
	for _, ok := range []string{"/db/", "/db/mysql/", "/数据库/表/", "/a-b_c.d/", "/v1.2/"} {
		if _, err := NormalizePath(ok); err != nil {
			t.Fatalf("rejected valid %q: %v", ok, err)
		}
	}
}
