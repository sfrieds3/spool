package history

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/sfrieds3/spool/internal/config"
)

func TestListFixturesAllHarnesses(t *testing.T) {
	root := copyFixtures(t)
	s := svc(fixtureSources(root)...)
	resp := mustList(t, s, ListRequest{})
	if resp.Partial || !resp.EOF || resp.NextOffset != nil || len(resp.Diagnostics) != 0 {
		t.Fatalf("page = %+v", resp.Page)
	}
	type row struct{ src, kind, id, cwd, title string }
	var got []row
	for _, x := range resp.Sessions {
		r := row{x.SourceID, x.Kind, x.NativeID, "<nil>", "<nil>"}
		if x.Cwd != nil {
			r.cwd = *x.Cwd
		}
		if x.Title != nil {
			r.title = *x.Title
		}
		got = append(got, r)
		if x.StartedAt == nil || x.StartedAt.Provenance != ProvenanceNativeRecord {
			t.Errorf("%s started_at = %+v", x.Path, x.StartedAt)
		}
		if x.FileModifiedAt == nil || x.FileModifiedAt.Provenance != ProvenanceFilesystemMtime {
			t.Errorf("%s file_modified_at = %+v", x.Path, x.FileModifiedAt)
		}
		if !x.MetadataComplete {
			t.Errorf("%s metadata incomplete", x.Path)
		}
	}
	const sid = "c1a2d3e4-0000-4000-8000-000000000001"
	want := []row{
		{"claude", "session", sid, "/home/example/project", "Widget nil bug"},
		{"claude", "subagent", sid, "/home/example/project", "<nil>"},
		{"codex", "session", "0e5a0000-0000-4000-8000-000000000001", "/home/example/project", "Run example tests"},
		{"codex-archive", "session", "0e5a0000-0000-4000-8000-000000000002", "/home/example/old-checkout", "<nil>"},
		{"pi", "session", "example-custom-id", "/home/example/project", "Example widget refactor"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sessions:\n got %v\nwant %v", got, want)
	}
	// The summary-only first line is not used as a title and prompts are
	// never rendered into titles.
	for _, x := range resp.Sessions {
		if x.Title != nil && strings.Contains(*x.Title, "Find the widget") {
			t.Fatal("prompt-derived title")
		}
	}
	// Harness filter.
	codex := mustList(t, s, ListRequest{Harness: "codex"})
	if len(codex.Sessions) != 2 || len(codex.Sources) != 2 {
		t.Fatalf("codex filter = %+v", codex)
	}
}

func TestListPagination(t *testing.T) {
	dir := t.TempDir()
	for i := range 7 {
		writeFile(t, filepath.Join(dir, fmt.Sprintf("s%02d.jsonl", i)), pi3(fmt.Sprintf("id-%d", i)))
	}
	writeFile(t, filepath.Join(dir, "s03b.jsonl"), "not json\n")
	s := svc(cfgSource("pi", "pi", dir))
	full := mustList(t, s, ListRequest{Limit: MaxLimit})
	if len(full.Sessions) != 7 || len(full.Diagnostics) != 1 {
		t.Fatalf("full = %d sessions %v", len(full.Sessions), diagCodes(full.Diagnostics))
	}
	for _, limit := range []int{1, 2, 3, 8} {
		var ids []string
		var diags int
		offset := int64(0)
		for pages := 0; ; pages++ {
			if pages > 20 {
				t.Fatal("pagination did not terminate")
			}
			p := mustList(t, s, ListRequest{Offset: offset, Limit: limit})
			for _, x := range p.Sessions {
				ids = append(ids, x.NativeID)
			}
			diags += len(p.Diagnostics)
			if p.EOF {
				if p.NextOffset != nil {
					t.Fatal("eof page has continuation")
				}
				break
			}
			if *p.NextOffset != offset+int64(limit) {
				t.Fatalf("next = %d", *p.NextOffset)
			}
			offset = *p.NextOffset
		}
		if !reflect.DeepEqual(ids, []string{"id-0", "id-1", "id-2", "id-3", "id-4", "id-5", "id-6"}) || diags != 1 {
			t.Fatalf("limit %d: ids %v diags %d", limit, ids, diags)
		}
	}
	// Offset boundaries.
	end := mustList(t, s, ListRequest{Offset: 8, Limit: 5})
	if !end.EOF || len(end.Sessions) != 0 || end.NextOffset != nil {
		t.Fatalf("offset at end = %+v", end.Page)
	}
	past := mustList(t, s, ListRequest{Offset: 100, Limit: 5})
	if !past.EOF || len(past.Sessions) != 0 {
		t.Fatalf("offset past end = %+v", past.Page)
	}
	last := mustList(t, s, ListRequest{Offset: 7, Limit: 1})
	if !last.EOF || len(last.Sessions) != 1 {
		t.Fatalf("last row = %+v", last.Page)
	}
	for _, bad := range []ListRequest{{Limit: 0}, {Limit: -1}, {Limit: MaxLimit + 1}, {Offset: -1, Limit: 1}, {Harness: "cursor", Limit: 1}} {
		if _, err := s.List(context.Background(), bad); err == nil || err.Code != CodeInvalidRequest {
			t.Errorf("%+v: err = %v", bad, err)
		}
	}
}

func TestListMetadata(t *testing.T) {
	dir := t.TempDir()
	// Title appears beyond the metadata budget: metadata_complete is false.
	big := `{"type":"custom","id":"1","parentId":null,"customType":"x","data":"` + strings.Repeat("a", MetadataScanBytes) + `"}`
	writeFile(t, filepath.Join(dir, "big.jsonl"), pi3("big", big, `{"type":"session_info","id":"2","parentId":"1","name":"late"}`))
	// Header larger than the budget.
	writeFile(t, filepath.Join(dir, "hugeheader.jsonl"), `{"type":"session","version":3,"id":"h","cwd":"`+strings.Repeat("x", MetadataScanBytes)+`"}`+"\n")
	// Oversized cwd metadata within budget.
	writeFile(t, filepath.Join(dir, "longcwd.jsonl"), `{"type":"session","version":3,"id":"c","cwd":"/`+strings.Repeat("x", maxMetadataField)+`"}`+"\n")
	// Cleared title.
	writeFile(t, filepath.Join(dir, "cleared.jsonl"), pi3("cleared", `{"type":"session_info","id":"1","parentId":null,"name":"x"}`, `{"type":"session_info","id":"2","parentId":"1"}`))
	// Legacy and empty files.
	writeFile(t, filepath.Join(dir, "legacy.jsonl"), `{"type":"session","id":"old"}`+"\n")
	writeFile(t, filepath.Join(dir, "empty.jsonl"), "")
	writeFile(t, filepath.Join(dir, "partial.jsonl"), `{"type":"session","version":3,"id":"p"`)

	s := svc(cfgSource("pi", "pi", dir))
	resp := mustList(t, s, ListRequest{})
	sessions := map[string]Session{}
	for _, x := range resp.Sessions {
		sessions[x.NativeID] = x
	}
	if b := sessions["big"]; b.MetadataComplete || b.Title != nil {
		t.Errorf("big = complete %v title %v", b.MetadataComplete, b.Title)
	}
	if c := sessions["cleared"]; c.Title != nil || !c.MetadataComplete {
		t.Errorf("cleared title = %v", c.Title)
	}
	diags := map[string]string{}
	for _, d := range resp.Diagnostics {
		diags[*d.Path] = d.Code
		if *d.SourceID != "pi" {
			t.Errorf("diagnostic source = %s", *d.SourceID)
		}
	}
	want := map[string]string{
		"hugeheader.jsonl": DiagMetadataOversized,
		"longcwd.jsonl":    DiagMetadataOversized,
		"legacy.jsonl":     DiagUnsupportedFormat,
		"empty.jsonl":      DiagIdentityUnavailable,
		"partial.jsonl":    DiagIdentityUnavailable,
	}
	if !reflect.DeepEqual(diags, want) {
		t.Fatalf("diagnostics = %v", diags)
	}
	if !resp.Partial {
		t.Fatal("candidate failures must be partial")
	}
}

func TestListSourceStates(t *testing.T) {
	base := t.TempDir()
	good := filepath.Join(base, "good")
	writeFile(t, filepath.Join(good, "a.jsonl"), pi3("a"))
	missing := filepath.Join(base, "missing")
	notDir := filepath.Join(base, "file")
	writeFile(t, notDir, "x")

	// Absent built-in default: successful empty catalog.
	resp := mustList(t, svc(config.Source{ID: "pi", Harness: "pi", Root: missing, Enabled: true, Default: true}), ListRequest{})
	if resp.Partial || len(resp.Sessions) != 0 || resp.Sources[0].State != SourceAbsent || !resp.EOF {
		t.Fatalf("absent default = %+v", resp)
	}
	// No enabled sources: successful empty catalog.
	resp = mustList(t, svc(config.Source{ID: "pi", Harness: "pi", Root: good, Enabled: false}), ListRequest{})
	if len(resp.Sources) != 0 || len(resp.Sessions) != 0 || resp.Partial {
		t.Fatalf("no enabled = %+v", resp)
	}
	resp = mustList(t, svc(), ListRequest{})
	if len(resp.Sources) != 0 || !resp.EOF {
		t.Fatalf("no sources = %+v", resp)
	}
	// Partial failure: explicit missing root plus a usable source.
	resp = mustList(t, svc(cfgSource("bad", "pi", missing), cfgSource("good", "pi", good)), ListRequest{})
	if !resp.Partial || len(resp.Sessions) != 1 || resp.Sources[0].State != SourceError || resp.Sources[1].State != SourceReady {
		t.Fatalf("partial = %+v", resp)
	}
	if resp.Diagnostics[0].Code != DiagSourceUnavailable || *resp.Diagnostics[0].SourceID != "bad" {
		t.Fatalf("diagnostic = %+v", resp.Diagnostics[0])
	}
	// All requested sources fail.
	_, err := svc(cfgSource("bad", "pi", missing), cfgSource("file", "codex", notDir)).List(context.Background(), ListRequest{Limit: 10})
	if err == nil || err.Code != CodeSourcesUnavailable || len(err.Sources) != 2 {
		t.Fatalf("all failed = %v", err)
	}
	// Filtering to the failed harness fails; filtering to the good one does not.
	mixed := svc(cfgSource("bad", "codex", missing), cfgSource("good", "pi", good))
	if _, err := mixed.List(context.Background(), ListRequest{Harness: "codex", Limit: 10}); err == nil || err.Code != CodeSourcesUnavailable {
		t.Fatalf("filtered failure = %v", err)
	}
	if r := mustList(t, mixed, ListRequest{Harness: "pi"}); r.Partial {
		t.Fatal("unrequested failed source affected the result")
	}
	// Unreadable root (skipped as root, where permissions are not enforced).
	if os.Geteuid() != 0 {
		locked := filepath.Join(base, "locked")
		writeFile(t, filepath.Join(locked, "a.jsonl"), pi3("x"))
		os.Chmod(locked, 0)
		defer os.Chmod(locked, 0o755)
		_, err := svc(cfgSource("locked", "pi", locked)).List(context.Background(), ListRequest{Limit: 10})
		if err == nil || err.Code != CodeSourcesUnavailable {
			t.Fatalf("unreadable root = %v", err)
		}
	}
}

func TestListResponseBudget(t *testing.T) {
	dir := t.TempDir()
	// Many sessions with large titles exceed the response budget at the max limit.
	title := strings.Repeat("t", maxMetadataField-10)
	n := MaxLimit
	for i := range n {
		cwd := "/" + strings.Repeat("c", maxMetadataField-10)
		header := `{"type":"session","version":3,"id":"` + fmt.Sprintf("id%04d", i) + `","cwd":"` + cwd + `"}`
		writeFile(t, filepath.Join(dir, fmt.Sprintf("s%04d.jsonl", i)), header+"\n"+`{"type":"session_info","id":"1","parentId":null,"name":"`+title+`"}`+"\n")
	}
	s := svc(cfgSource("pi", "pi", dir))
	total := 0
	offset := int64(0)
	pages := 0
	for {
		p := mustList(t, s, ListRequest{Offset: offset, Limit: MaxLimit})
		out, _ := Marshal(p)
		if len(out) > MaxResponseBytes {
			t.Fatalf("response %d bytes exceeds limit", len(out))
		}
		total += len(p.Sessions)
		pages++
		if p.EOF {
			break
		}
		offset = *p.NextOffset
	}
	if total != n || pages < 2 {
		t.Fatalf("total %d over %d pages", total, pages)
	}
}
