package strictjson

import (
	"strings"
	"testing"
)

func TestCheckDuplicateKeys(t *testing.T) {
	for _, tc := range []struct {
		doc string
		bad bool
	}{
		{`{"a":1,"b":{"a":2}}`, false},
		{`{"a":1,"a":2}`, true},
		{`{"a":{"b":[{"c":1,"c":2}]}}`, true},
		{`[{"x":1},{"x":2}]`, false},
		{`{"a":`, true},
	} {
		if err := CheckDuplicateKeys([]byte(tc.doc)); (err != nil) != tc.bad {
			t.Errorf("%s: err = %v", tc.doc, err)
		}
	}
}

func TestDecode(t *testing.T) {
	type doc struct {
		A *string `json:"a"`
	}
	var d doc
	if err := Decode([]byte(`{"a":"x"}`), &d); err != nil || *d.A != "x" {
		t.Fatalf("decode = %v", err)
	}
	if err := Decode([]byte(`{"a":"x","b":1}`), &d); err == nil {
		t.Error("unknown field accepted")
	}
	if err := Decode([]byte(`{"a":"x"} {}`), &d); err == nil || !strings.Contains(err.Error(), "trailing") {
		t.Errorf("trailing data: %v", err)
	}
}

func TestRequireKeysNullVersusOmitted(t *testing.T) {
	req := map[string][]string{"": {"title"}, "sessions[]": {"note", "ref"}, "workspace": {"path"}}
	ok := `{"title":null,"workspace":{"path":null},"sessions":[{"ref":"r","note":null},{"ref":"s","note":"n"}]}`
	keys, err := Keys([]byte(ok))
	if err != nil {
		t.Fatal(err)
	}
	if err := RequireKeys(keys, req); err != nil {
		t.Fatalf("null values rejected: %v", err)
	}
	for _, bad := range []string{
		`{"workspace":{"path":null},"sessions":[]}`,
		`{"title":null,"workspace":{},"sessions":[]}`,
		`{"title":null,"workspace":{"path":"/"},"sessions":[{"ref":"r","note":null},{"ref":"s"}]}`,
	} {
		keys, err := Keys([]byte(bad))
		if err != nil {
			t.Fatal(err)
		}
		if err := RequireKeys(keys, req); err == nil {
			t.Errorf("%s: missing key accepted", bad)
		}
	}
	if _, err := Keys([]byte(`{"a":{"b":1,"b":2}}`)); err == nil {
		t.Error("Keys accepted duplicate key")
	}
}
