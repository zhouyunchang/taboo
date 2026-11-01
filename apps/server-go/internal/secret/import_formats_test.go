package secret

import "testing"

func TestDopplerAndOnePasswordImport(t *testing.T) {
	doppler := `{
	  "DOPPLER_PROJECT": "demo",
	  "API_KEY": "doppler-secret",
	  "secrets": {"NESTED": {"computed": "from-api", "raw": "raw"}}
	}`
	entries, err := parseImport("doppler", doppler)
	if err != nil || len(entries) != 1 || entries[0].Key != "NESTED" || entries[0].Value != "from-api" {
		t.Fatalf("%+v %v", entries, err)
	}
	flat, err := parseImport("doppler", `{"API_KEY":"flat","DOPPLER_CONFIG":"dev"}`)
	if err != nil || len(flat) != 1 || flat[0].Key != "API_KEY" || flat[0].Value != "flat" {
		t.Fatalf("%+v %v", flat, err)
	}
	op := `{"title":"API","fields":[
	  {"label":"credential","type":"CONCEALED","value":"op-secret"},
	  {"label":"notes","purpose":"NOTES","value":"ignore"},
	  {"label":"empty","type":"STRING","value":""}
	]}`
	got, err := parseImport("1password", op)
	if err != nil || len(got) != 1 || got[0].Key != "CREDENTIAL" || got[0].Value != "op-secret" {
		t.Fatalf("%+v %v", got, err)
	}
	many := `[{"title":"A","fields":[{"label":"password","value":"one"}]},{"title":"B","fields":[{"label":"password","value":"two"}]}]`
	got, err = parseImport("op", many)
	if err != nil || len(got) != 2 || got[0].Key == got[1].Key {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err = parseImport("nope", "A=B"); err == nil {
		t.Fatal("unknown format accepted")
	}
}
