package locator

import "testing"

func TestParseLocator(t *testing.T) {
	loc, err := Parse("beiqiao/api/payments")
	if err != nil || loc.Owner != "beiqiao" || loc.Project != "api" || loc.Folder != "/payments/" {
		t.Fatalf("%+v %v", loc, err)
	}
	loc, err = Parse("/lin/notes/")
	if err != nil || loc.Folder != "/" {
		t.Fatalf("%+v %v", loc, err)
	}
	if _, err = Parse("lin"); !IsInvalid(err) {
		t.Fatal("owner alone should be invalid")
	}
	if _, err = Parse("api/notes"); !IsInvalid(err) {
		t.Fatal("reserved owner should be invalid")
	}
}
