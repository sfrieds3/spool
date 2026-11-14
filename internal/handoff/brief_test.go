package handoff

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/sfrieds3/spool/internal/history"
)

func TestParseBriefMinimal(t *testing.T) {
	b, err := ParseBrief([]byte(`{"version":1,"goal":"Ship it"}`))
	if err != nil {
		t.Fatal(err)
	}
	if b.Goal != "Ship it" || b.Title != nil || b.WorkspacePath != nil || b.Repository != nil {
		t.Fatalf("brief = %+v", b)
	}
	for _, l := range [][]string{b.Constraints, b.Decisions, b.Progress, b.Outstanding, b.Notes} {
		if l == nil || len(l) != 0 {
			t.Fatalf("lists must be empty, not nil: %+v", b)
		}
	}
}

func TestParseBriefPreservesItems(t *testing.T) {
	items := []string{"  leading space kept", "# heading-like", "multi\nline\r\n", "unicode ✓ \u00e9", "\\u escape \u2028"}
	doc := map[string]any{
		"version": 1, "title": "T", "goal": "G\n\nmore",
		"constraints": items, "decisions": items, "progress": items, "outstanding": items, "notes": items,
		"workspace": map[string]any{"path": "/home/example/p", "repository": map[string]any{"branch": "main", "commit": nil}},
		"sessions":  []any{map[string]any{"ref": "s1.a", "note": "why"}, map[string]any{"ref": "s1.b", "note": nil}},
	}
	data, _ := json.Marshal(doc)
	b, err := ParseBrief(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range [][]string{b.Constraints, b.Decisions, b.Progress, b.Outstanding, b.Notes} {
		if !reflect.DeepEqual(l, items) {
			t.Fatalf("items = %q", l)
		}
	}
	if *b.WorkspacePath != "/home/example/p" || *b.Repository.Branch != "main" || b.Repository.Commit != nil || b.Repository.Provenance != ProvenanceAuthorReported {
		t.Fatalf("workspace = %+v %+v", b.WorkspacePath, b.Repository)
	}
	if len(b.Sessions) != 2 || *b.Sessions[0].Note != "why" || b.Sessions[1].Note != nil {
		t.Fatalf("sessions = %+v", b.Sessions)
	}
}

func TestParseBriefNullMeansAbsent(t *testing.T) {
	b, err := ParseBrief([]byte(`{"version":1,"goal":"g","title":null,"constraints":null,"workspace":null,"sessions":null}`))
	if err != nil {
		t.Fatal(err)
	}
	if b.Title != nil || len(b.Constraints) != 0 || b.WorkspacePath != nil || len(b.Sessions) != 0 {
		t.Fatalf("brief = %+v", b)
	}
	if b, err := ParseBrief([]byte(`{"version":1,"goal":"g","workspace":{}}`)); err != nil || b.WorkspacePath != nil {
		t.Fatalf("empty workspace = %+v %v", b, err)
	}
}

func TestParseBriefRejects(t *testing.T) {
	many := func(n int) string {
		parts := make([]string, n)
		for i := range parts {
			parts[i] = `"x"`
		}
		return "[" + strings.Join(parts, ",") + "]"
	}
	sessions := func(n int) string {
		parts := make([]string, n)
		for i := range parts {
			parts[i] = fmt.Sprintf(`{"ref":"s1.%d"}`, i)
		}
		return "[" + strings.Join(parts, ",") + "]"
	}
	g := `"version":1,"goal":"g"`
	cases := map[string]string{
		"empty":                "",
		"array":                `[]`,
		"not json":             `{`,
		"trailing":             `{` + g + `} {}`,
		"trailing garbage":     `{` + g + `}x`,
		"duplicate key":        `{` + g + `,"goal":"h"}`,
		"nested duplicate":     `{` + g + `,"workspace":{"path":"/a","path":"/b"}}`,
		"unknown field":        `{` + g + `,"extra":1}`,
		"unknown nested":       `{` + g + `,"workspace":{"cwd":"/a"}}`,
		"missing version":      `{"goal":"g"}`,
		"version 2":            `{"version":2,"goal":"g"}`,
		"version 1.0":          `{"version":1.0,"goal":"g"}`,
		"version string":       `{"version":"1","goal":"g"}`,
		"missing goal":         `{"version":1}`,
		"null goal":            `{"version":1,"goal":null}`,
		"blank goal":           `{"version":1,"goal":" \t\n\u00a0"}`,
		"goal too long":        `{"version":1,"goal":"` + strings.Repeat("a", MaxItemBytes+1) + `"}`,
		"goal number":          `{"version":1,"goal":1}`,
		"nul":                  `{"version":1,"goal":"a\u0000b"}`,
		"blank title":          `{` + g + `,"title":""}`,
		"title too long":       `{` + g + `,"title":"` + strings.Repeat("a", MaxTitleBytes+1) + `"}`,
		"blank item":           `{` + g + `,"notes":["ok"," "]}`,
		"null item":            `{` + g + `,"notes":[null]}`,
		"number item":          `{` + g + `,"notes":[1]}`,
		"too many items":       `{` + g + `,"progress":` + many(MaxItems+1) + `}`,
		"relative path":        `{` + g + `,"workspace":{"path":"rel/dir"}}`,
		"unclean path":         `{` + g + `,"workspace":{"path":"/a/../b"}}`,
		"blank path":           `{` + g + `,"workspace":{"path":""}}`,
		"empty repository":     `{` + g + `,"workspace":{"repository":{}}}`,
		"null-only repository": `{` + g + `,"workspace":{"repository":{"branch":null}}}`,
		"blank branch":         `{` + g + `,"workspace":{"repository":{"branch":" "}}}`,
		"repo too long":        `{` + g + `,"workspace":{"repository":{"remote":"` + strings.Repeat("a", MaxRepoFieldBytes+1) + `"}}}`,
		"session no ref":       `{` + g + `,"sessions":[{"note":"n"}]}`,
		"session null":         `{` + g + `,"sessions":[null]}`,
		"session empty ref":    `{` + g + `,"sessions":[{"ref":""}]}`,
		"duplicate ref":        `{` + g + `,"sessions":[{"ref":"s1.a"},{"ref":"s1.a"}]}`,
		"blank note":           `{` + g + `,"sessions":[{"ref":"s1.a","note":""}]}`,
		"too many sessions":    `{` + g + `,"sessions":` + sessions(MaxSessions+1) + `}`,
		"oversized input":      `{` + g + `,"notes":["` + strings.Repeat("a", MaxInputBytes) + `"]}`,
	}
	for name, doc := range cases {
		if _, err := ParseBrief([]byte(doc)); err == nil || err.Code != history.CodeInvalidRequest {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if _, err := ParseBrief([]byte("{\"version\":1,\"goal\":\"\xff\"}")); err == nil {
		t.Error("invalid UTF-8 accepted")
	}
}

func TestIDs(t *testing.T) {
	at := time.Date(2026, 11, 12, 15, 30, 0, 999, time.UTC)
	id, err := newID(at, bytes.NewReader([]byte{0x1a, 0x2b, 0x3c, 0x4d}))
	if err != nil || id != "20261112t153000z-1a2b3c4d" || !ValidID(id) {
		t.Fatalf("id = %q %v", id, err)
	}
	later, _ := newID(at.Add(time.Second), bytes.NewReader([]byte{0, 0, 0, 0}))
	if later <= id {
		t.Fatalf("ids do not sort by second: %s <= %s", later, id)
	}
	for _, bad := range []string{"", "../20261112t153000z-1a2b3c4d", "20261112t153000z-1a2b3c4d/x", "20261112T153000Z-1a2b3c4d", "20261112t153000z-1A2B3C4D", "20261112t153000z-1a2b3c4d0", "20261112t153000z-1a2b3c4d.json", ".."} {
		if ValidID(bad) {
			t.Errorf("%q accepted", bad)
		}
	}
	if idFromFileName("20261112t153000z-1a2b3c4d.json") != "20261112t153000z-1a2b3c4d" || idFromFileName(".tmp-20261112t153000z-1a2b3c4d-00") != "" {
		t.Fatal("idFromFileName")
	}
}
