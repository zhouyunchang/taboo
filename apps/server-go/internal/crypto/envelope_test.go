package crypto

import (
	"encoding/hex"
	"strings"
	"testing"
)

func TestEnvelopeContextAndLegacy(t *testing.T) {
	key := make([]byte, 32)
	c := SecretContext("o", "p", "e", "s", 1)
	ct, err := Seal(key, "value", c)
	if err != nil {
		t.Fatal(err)
	}
	if value, err := Open(key, ct, c); err != nil || value != "value" {
		t.Fatalf("roundtrip: %s %v", value, err)
	}
	for _, other := range []Context{SecretContext("other", "p", "e", "s", 1), SecretContext("o", "other", "e", "s", 1), SecretContext("o", "p", "other", "s", 1), SecretContext("o", "p", "e", "other", 1), SecretContext("o", "p", "e", "s", 2), {Purpose: "credential", Org: "o", Project: "p", Environment: "e", Object: "s", Version: 1}} {
		if _, err := Open(key, ct, other); err == nil {
			t.Fatalf("accepted swapped context: %+v", other)
		}
	}
	legacy, _ := Encrypt(key, "old")
	if value, err := Open(key, legacy, c); err != nil || value != "old" {
		t.Fatal("legacy read failed")
	}
	if _, err := Open(key, strings.Replace(ct, "v1:", "v2:", 1), c); err == nil {
		t.Fatal("unknown envelope downgraded")
	}
}

func TestDEKCacheTracksWrappedKeyAndReturnsCopies(t *testing.T) {
	master := make([]byte, 32)
	cache := NewDEKCache()
	key := make([]byte, 32)
	wrapped, _ := Seal(master, hex.EncodeToString(key), DEKContext("a"))
	got, err := cache.Get(master, "a", wrapped)
	if err != nil {
		t.Fatal(err)
	}
	got[0] = 42
	again, _ := cache.Get(master, "a", wrapped)
	if again[0] != 0 {
		t.Fatal("caller mutated cache")
	}
	key[0] = 12
	replacement, _ := Seal(master, hex.EncodeToString(key), DEKContext("a"))
	got, err = cache.Get(master, "a", replacement)
	if err != nil || got[0] != 12 {
		t.Fatal("stale key after rotation")
	}
	if _, err = cache.Get(master, "b", wrapped); err == nil {
		t.Fatal("wrapped DEK moved across tenants")
	}
	master[0] = 1
	if _, err = cache.Get(master, "a", replacement); err == nil {
		t.Fatal("cache ignored master key change")
	}
}
