package harness

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const fixtures = "../../testdata/sessions"

func parseFile(t *testing.T, a Adapter, path string) []Entry {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []Entry
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 1<<20)
	for n := int64(1); sc.Scan(); n++ {
		e, err := a.Parse(n, sc.Bytes())
		if err != nil {
			t.Fatalf("%s line %d: %v", path, n, err)
		}
		out = append(out, e)
	}
	return out
}

func s(p *string) string {
	if p == nil {
		return "<nil>"
	}
	return *p
}

func formatCode(err error) string {
	var fe *FormatError
	if errors.As(err, &fe) {
		return fe.Code
	}
	return ""
}

func TestPiFixture(t *testing.T) {
	path := filepath.Join(fixtures, "pi/--home-example-project--/2026-01-02T03-04-05-000Z_example-custom-id.jsonl")
	entries := parseFile(t, Pi{}, path)
	h := entries[0]
	if h.Identity == nil || h.Identity.NativeID != "example-custom-id" || s(h.Identity.Cwd) != "/home/example/project" || s(h.Identity.Started) != "2026-01-02T03:04:05.000Z" {
		t.Fatalf("header identity = %+v", h.Identity)
	}
	if h.ID != nil {
		t.Fatal("header is not a tree entry")
	}
	if !entries[1].SetsTitle || s(entries[1].Title) != "Example widget refactor" {
		t.Fatalf("session_info title = %+v", entries[1])
	}
	want := []struct{ typ, sub, id, parent string }{
		{"message", "user", "e0000002", "e0000001"},
		{"message", "assistant", "e0000003", "e0000002"},
		{"message", "toolResult", "e0000004", "e0000003"},
		{"message", "user", "e0000005", "e0000002"}, // second branch from e0000002
		{"branch_summary", "<nil>", "e0000006", "e0000002"},
		{"compaction", "<nil>", "e0000007", "e0000006"},
		{"custom", "example-extension", "e0000008", "e0000007"},
		{"future_entry_kind", "<nil>", "e0000009", "e0000008"},
	}
	for i, w := range want {
		e := entries[i+2]
		if s(e.Type) != w.typ || s(e.Subtype) != w.sub || s(e.ID) != w.id || s(e.ParentID) != w.parent {
			t.Errorf("entry %d = %s/%s %s<-%s, want %+v", i+2, s(e.Type), s(e.Subtype), s(e.ID), s(e.ParentID), w)
		}
		if e.Unsupported != "" || e.Identity != nil {
			t.Errorf("entry %d unexpected identity/unsupported", i+2)
		}
	}
}

func TestPiRejectsUnsupportedHeaders(t *testing.T) {
	tests := map[string]struct{ line, code string }{
		"v1 without version": {`{"type":"session","id":"x","timestamp":"t","cwd":"/a"}`, CodeUnsupportedFormat},
		"v2":                 {`{"type":"session","version":2,"id":"x"}`, CodeUnsupportedFormat},
		"v4":                 {`{"type":"session","version":4,"id":"x"}`, CodeUnsupportedFormat},
		"string version":     {`{"type":"session","version":"3","id":"x"}`, CodeUnsupportedFormat},
		"not a header":       {`{"type":"message","id":"x"}`, CodeUnsupportedFormat},
		"missing id":         {`{"type":"session","version":3}`, CodeInvalidHistory},
		"empty id":           {`{"type":"session","version":3,"id":""}`, CodeInvalidHistory},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := Pi{}.Parse(1, []byte(tt.line))
			if got := formatCode(err); got != tt.code {
				t.Fatalf("code = %q (%v), want %q", got, err, tt.code)
			}
		})
	}
}

func TestClaudeFixtures(t *testing.T) {
	const sid = "c1a2d3e4-0000-4000-8000-000000000001"
	dir := filepath.Join(fixtures, "claude/-home-example-project")
	entries := parseFile(t, Claude{}, filepath.Join(dir, sid+".jsonl"))
	if entries[0].Identity != nil || s(entries[0].Type) != "summary" {
		t.Fatalf("summary record = %+v", entries[0])
	}
	first := entries[1]
	if first.Identity == nil || first.Identity.NativeID != sid || s(first.Identity.Cwd) != "/home/example/project" {
		t.Fatalf("identity = %+v", first.Identity)
	}
	if s(first.ID) != "a0000000-0000-4000-8000-000000000001" || first.ParentID != nil {
		t.Fatalf("root record relationships = %s <- %s", s(first.ID), s(first.ParentID))
	}
	if s(entries[2].ParentID) != s(first.ID) {
		t.Fatal("parentUuid not preserved")
	}
	if s(entries[4].Type) != "system" || s(entries[4].Subtype) != "compact_boundary" {
		t.Fatalf("compaction record = %s/%s", s(entries[4].Type), s(entries[4].Subtype))
	}
	if !entries[5].SetsTitle || entries[5].TitlePriority >= entries[6].TitlePriority || s(entries[6].Title) != "Widget nil bug" {
		t.Fatal("custom titles must outrank AI titles")
	}

	subRel := sid + "/subagents/agent-example1.jsonl"
	if !(Claude{}).Candidate(subRel) || (Claude{}).Kind(subRel) != "subagent" || (Claude{}).Kind(sid+".jsonl") != "session" {
		t.Fatal("nested subagent transcripts must be discovered as subagents")
	}
	sub := parseFile(t, Claude{}, filepath.Join(dir, filepath.FromSlash(subRel)))
	if sub[0].Identity == nil || sub[0].Identity.NativeID != sid {
		t.Fatal("subagent records its parent session id")
	}
}

func TestClaudeForkContextIsUnsupported(t *testing.T) {
	e, err := Claude{}.Parse(1, []byte(`{"type":"fork-context-ref","agentId":"a1","parentSessionId":"p","parentLastUuid":"u","contextLength":12}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(e.Unsupported, "fork-context-ref") {
		t.Fatalf("fork-context-ref must be diagnosed, got %+v", e)
	}
}

func TestCodexFixtures(t *testing.T) {
	const id = "0e5a0000-0000-4000-8000-000000000001"
	path := filepath.Join(fixtures, "codex/2026/01/02/rollout-2026-01-02T08-00-00-"+id+".jsonl")
	if !(Codex{}).Candidate("2026/01/02/rollout-x.jsonl") || (Codex{}).Candidate("2026/notes.jsonl") {
		t.Fatal("candidate selection")
	}
	entries := parseFile(t, Codex{}, path)
	if entries[0].Identity == nil || entries[0].Identity.NativeID != id || s(entries[0].Identity.Cwd) != "/home/example/project" {
		t.Fatalf("identity = %+v", entries[0].Identity)
	}
	want := []struct{ typ, sub string }{
		{"session_meta", "<nil>"},
		{"turn_context", "<nil>"},
		{"response_item", "message"},
		{"response_item", "function_call"},
		{"response_item", "function_call_output"},
		{"event_msg", "token_count"},
		{"event_msg", "thread_name_updated"},
		{"compacted", "<nil>"},
		{"future_rollout_item", "<nil>"},
	}
	for i, w := range want {
		if s(entries[i].Type) != w.typ || s(entries[i].Subtype) != w.sub || entries[i].Unsupported != "" {
			t.Errorf("entry %d = %s/%s %q", i, s(entries[i].Type), s(entries[i].Subtype), entries[i].Unsupported)
		}
	}
	if !entries[6].SetsTitle || s(entries[6].Title) != "Run example tests" {
		t.Fatal("thread name")
	}
	// Archived legacy fork: forked_from_id alone is provenance, not composite.
	arch := filepath.Join(fixtures, "codex-archive/rollout-2025-12-30T09-00-00-0e5a0000-0000-4000-8000-000000000002.jsonl")
	if e := parseFile(t, Codex{}, arch); e[0].Unsupported != "" {
		t.Fatalf("legacy fork rejected: %s", e[0].Unsupported)
	}
}

func TestCodexRejects(t *testing.T) {
	meta := func(extra string) string {
		return `{"timestamp":"t","type":"session_meta","payload":{"id":"x","cwd":"/a"` + extra + `}}`
	}
	for name, extra := range map[string]string{
		"paginated history":       `,"history_mode":"paginated"`,
		"history base":            `,"history_base":{"thread_id":"p"}`,
		"inherited fork ordinal":  `,"forked_from_id":"p","forked_from_ordinal_exclusive":10`,
		"subagent history prefix": `,"subagent_history_start_ordinal":4`,
	} {
		t.Run(name, func(t *testing.T) {
			e, err := Codex{}.Parse(1, []byte(meta(extra)))
			if err != nil || e.Unsupported == "" {
				t.Fatalf("want unsupported marker, got %+v %v", e, err)
			}
		})
	}
	if e, _ := (Codex{}).Parse(1, []byte(meta(`,"history_mode":"legacy","history_base":null`))); e.Unsupported != "" {
		t.Fatal("explicit legacy mode is self-contained")
	}
	if _, err := (Codex{}).Parse(1, []byte(`{"type":"response_item","payload":{}}`)); formatCode(err) != CodeUnsupportedFormat {
		t.Fatalf("non-meta first line: %v", err)
	}
	if _, err := (Codex{}).Parse(1, []byte(`{"type":"session_meta","payload":{"cwd":"/a"}}`)); formatCode(err) != CodeInvalidHistory {
		t.Fatalf("missing id: %v", err)
	}
	// A later composite session_meta also makes the file unsupported.
	if e, _ := (Codex{}).Parse(5, []byte(meta(`,"history_mode":"paginated"`))); e.Unsupported == "" {
		t.Fatal("later marker ignored")
	}
}
