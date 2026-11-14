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

func mustSearch(t *testing.T, s *Service, req SearchRequest) *SearchResponse {
	t.Helper()
	if req.Limit == 0 {
		req.Limit = DefaultLimit
	}
	return mustResult(t, "Search", func() (*SearchResponse, *Error) { return s.Search(t.Context(), req) })
}
func searchBody(text string) string {
	return fmt.Sprintf(`{"type":"message","id":"m","message":{"role":"user","content":%q}}`, text)
}
func TestSearchValidationBeforeHistoryAccess(t *testing.T) {
	s := svc(cfgSource("broken", "pi", filepath.Join(t.TempDir(), "missing")))
	for _, req := range []SearchRequest{
		{Query: "", Limit: 1}, {Query: "  \n ", Limit: 1}, {Query: "\xff", Limit: 1}, {Query: strings.Repeat("x", 257), Limit: 1},
		{Query: "x", Harness: "future", Limit: 1}, {Query: "x", Limit: -1}, {Query: "x", Limit: 501}, {Query: "x", Limit: 0},
	} {
		if _, err := s.Search(t.Context(), req); err == nil || err.Code != CodeInvalidRequest {
			t.Errorf("%+v returned %v", req, err)
		}
	}
}
func TestSearchFixtureSourcesAndRead(t *testing.T) {
	root := copyFixtures(t)
	// Distinct source files and nested/archived fixture paths exercise the same service.
	additions := map[string]string{
		"pi/--home-example-project--/2026-01-02T03-04-05-000Z_example-custom-id.jsonl":            searchBody("searchbridge"),
		"claude/-home-example-project/c1a2d3e4-0000-4000-8000-000000000001.jsonl":                 `{"type":"user","sessionId":"c1a2d3e4-0000-4000-8000-000000000001","message":{"content":"searchbridge"}}`,
		"codex/2026/01/02/rollout-2026-01-02T08-00-00-0e5a0000-0000-4000-8000-000000000001.jsonl": `{"type":"compacted","payload":{"message":"searchbridge"}}`,
		"codex-archive/rollout-2025-12-30T09-00-00-0e5a0000-0000-4000-8000-000000000002.jsonl":    `{"type":"compacted","payload":{"message":"searchbridge"}}`,
	}
	for rel, addition := range additions {
		path := filepath.Join(root, rel)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, path, string(data)+addition+"\n")
	}
	s := svc(fixtureSources(root)...)
	resp := mustSearch(t, s, SearchRequest{Query: "searchbridge"})
	if resp.Partial || !resp.EOF || len(resp.Sources) != 4 || len(resp.Hits) < 3 {
		t.Fatalf("fixtures %+v", resp)
	}
	seen := map[string]bool{}
	for _, hit := range resp.Hits {
		seen[hit.Session.SourceID] = true
		read := mustRead(t, s, hit.Session.Ref, hit.Line-1, 1)
		if len(read.Records) != 1 || read.Records[0].Line != hit.Line {
			t.Fatalf("cannot recover hit %+v", hit)
		}
		if hit.Snippet[hit.MatchStart:hit.MatchEnd] == "" {
			t.Fatalf("missing match %+v", hit)
		}
	}
	for _, name := range []string{"pi", "claude", "codex", "codex-archive"} {
		if !seen[name] {
			t.Errorf("missing source %s", name)
		}
	}
	if got := mustSearch(t, s, SearchRequest{Query: "Search for widget usages.", Harness: "claude"}); len(got.Hits) != 1 || len(got.Sources) != 1 || got.Hits[0].Session.Kind != "subagent" {
		t.Fatalf("filtered subagent %+v", got)
	}
}
func TestSearchSourceStatesAndIdentity(t *testing.T) {
	base := t.TempDir()
	good := filepath.Join(base, "good")
	writeFile(t, filepath.Join(good, "a.jsonl"), pi3("same", searchBody("needle")))
	writeFile(t, filepath.Join(good, "b.jsonl"), pi3("same", searchBody("needle")))
	missing := filepath.Join(base, "missing")
	partial := mustSearch(t, svc(cfgSource("a", "pi", missing), cfgSource("b", "pi", good)), SearchRequest{Query: "needle"})
	if !partial.Partial || len(partial.Hits) != 2 || partial.Hits[0].Session.Ref == partial.Hits[1].Session.Ref || partial.Sources[0].State != SourceError || partial.Sources[1].State != SourceReady {
		t.Fatalf("partial %+v", partial)
	}
	absent := mustSearch(t, svc(config.Source{ID: "absent", Harness: "pi", Root: missing, Enabled: true, Default: true}), SearchRequest{Query: "x"})
	if absent.Partial || !absent.EOF || absent.Sources[0].State != SourceAbsent {
		t.Fatalf("absent %+v", absent)
	}
	if empty := mustSearch(t, svc(), SearchRequest{Query: "x"}); len(empty.Sources) != 0 || !empty.EOF {
		t.Fatalf("empty %+v", empty)
	}
	_, err := svc(cfgSource("bad", "pi", missing)).Search(t.Context(), SearchRequest{Query: "x", Limit: 1})
	if err == nil || err.Code != CodeSourcesUnavailable {
		t.Fatalf("all failed %v", err)
	}
}
func TestSearchPagesAndCursors(t *testing.T) {
	dir := t.TempDir()
	want := []string{}
	for i := range 5 {
		var body []string
		for n := range 4 {
			body = append(body, searchBody(fmt.Sprintf("needle-%d-%d", i, n)))
			want = append(want, fmt.Sprintf("needle-%d-%d", i, n))
		}
		writeFile(t, filepath.Join(dir, fmt.Sprintf("%d.jsonl", i)), pi3(fmt.Sprint(i), body...))
	}
	s := svc(cfgSource("pi", "pi", dir))
	var got []string
	cursor := ""
	for page := 0; page < 30; page++ {
		r := mustSearch(t, s, SearchRequest{Query: "needle", Limit: 2, Cursor: cursor})
		for _, hit := range r.Hits {
			got = append(got, hit.Snippet)
		}
		if r.EOF {
			break
		}
		if r.NextCursor == nil || *r.NextCursor == cursor {
			t.Fatalf("nonadvancing cursor %+v", r)
		}
		cursor = *r.NextCursor
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q want %q", got, want)
	}
	if r := mustSearch(t, s, SearchRequest{Query: "needle", Limit: 4, Cursor: cursor}); r.EOF && len(r.Hits) == 0 {
		t.Fatal("limit change lost cursor position")
	}
	for _, bad := range []string{"q1.!!!", "q2.abc", "q1.YWJj", "q1." + strings.Repeat("a", MaxSearchCursorBytes)} {
		if _, err := s.Search(t.Context(), SearchRequest{Query: "needle", Limit: 1, Cursor: bad}); err == nil || err.Code != CodeInvalidRequest {
			t.Errorf("bad %q: %v", bad, err)
		}
	}
	if _, err := s.Search(t.Context(), SearchRequest{Query: "other", Limit: 1, Cursor: cursor}); err == nil || err.Code != CodeInvalidRequest {
		t.Fatalf("mismatched cursor %v", err)
	}
}
func TestSearchLateFailedCandidatePreservesEarlierPages(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "pi/a.jsonl"), pi3("a", searchBody("needle a")))
	writeFile(t, filepath.Join(dir, "claude/b.jsonl"), `{"type":"user","sessionId":"b","message":{"content":"needle bad"}}`+"\n"+`{"type":"fork-context-ref"}`+"\n")
	writeFile(t, filepath.Join(dir, "claude/c.jsonl"), `{"type":"user","sessionId":"c","message":{"content":"needle c"}}`+"\n")
	// The first source has independent valid hits; the later Claude failure
	// must not retroactively invalidate earlier files or the later candidate.
	s := svc(cfgSource("a", "pi", filepath.Join(dir, "pi")), cfgSource("b", "claude", filepath.Join(dir, "claude")))
	first := mustSearch(t, s, SearchRequest{Query: "needle", Limit: 1})
	if len(first.Hits) != 1 || first.Hits[0].Session.Path != "a.jsonl" || first.NextCursor == nil || !first.Partial || !reflect.DeepEqual(diagCodes(first.Diagnostics), []string{DiagUnsupportedFormat}) {
		t.Fatalf("first %+v", first)
	}
	second := mustSearch(t, s, SearchRequest{Query: "needle", Limit: 1, Cursor: *first.NextCursor})
	if len(second.Hits) != 1 || second.Hits[0].Session.Path != "c.jsonl" || second.Partial || len(second.Diagnostics) != 0 {
		t.Fatalf("second %+v", second)
	}
}

func TestSearchFileValidation(t *testing.T) {
	for _, tt := range []struct{ name, kind, rel, body, code string }{
		{"claude late fork", "claude", "bad.jsonl", `{"type":"user","sessionId":"c","message":{"content":"needle"}}` + "\n" + `{"type":"fork-context-ref"}` + "\n", DiagUnsupportedFormat},
		{"codex late mode", "codex", "rollout-bad.jsonl", `{"type":"session_meta","payload":{"id":"c"}}` + "\n" + `{"type":"response_item","payload":{"type":"message","content":[{"type":"text","text":"needle"}]}}` + "\n" + `{"type":"session_meta","payload":{"history_mode":"paginated"}}` + "\n", DiagUnsupportedFormat},
		{"pi invalid header", "pi", "bad.jsonl", `{"type":"session","version":3}` + "\n" + searchBody("needle") + "\n", DiagInvalidHistory},
		{"claude conflicting identity", "claude", "bad.jsonl", `{"type":"user","sessionId":"a","message":{"content":"needle"}}` + "\n" + `{"type":"user","sessionId":"b","message":{"content":"needle"}}` + "\n", DiagInvalidHistory},
		{"claude missing identity", "claude", "bad.jsonl", `{"type":"summary","summary":"needle"}` + "\n", DiagIdentityUnavailable},
		{"oversized line", "pi", "bad.jsonl", pi3("x", searchBody("needle")) + strings.Repeat("x", MaxLineBytes+1) + "\n", DiagOversizedLine},
		{"oversized unterminated", "pi", "bad.jsonl", pi3("x", searchBody("needle")) + strings.Repeat("x", MaxLineBytes+1), DiagOversizedLine},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, filepath.Join(dir, tt.rel), tt.body)
			if tt.kind == "pi" {
				writeFile(t, filepath.Join(dir, "z.jsonl"), pi3("z", searchBody("needle")))
			}
			r := mustSearch(t, svc(cfgSource(tt.kind, tt.kind, dir)), SearchRequest{Query: "needle", Limit: 1})
			if len(r.Diagnostics) == 0 || r.Diagnostics[0].Code != tt.code || !r.Partial {
				t.Fatalf("validation %+v", r)
			}
			for _, hit := range r.Hits {
				if hit.Session.Path == tt.rel {
					t.Fatalf("invalid file published a hit %+v", hit)
				}
			}
		})
	}
}
func TestSearchDamagedLinesAndTail(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.jsonl"), pi3("x", searchBody("needle one"), "{bad", searchBody("needle two"))+`{"type":"message","message":{"content":"needle final"}`)
	s := svc(cfgSource("pi", "pi", dir))
	r := mustSearch(t, s, SearchRequest{Query: "needle", Limit: 1})
	if len(r.Hits) != 1 || r.Hits[0].Line != 2 || r.NextCursor == nil || !r.Partial || !reflect.DeepEqual(diagCodes(r.Diagnostics), []string{DiagMalformedLine}) {
		t.Fatalf("first %+v", r)
	}
	r = mustSearch(t, s, SearchRequest{Query: "needle", Limit: 1, Cursor: *r.NextCursor})
	if len(r.Hits) != 1 || r.Hits[0].Line != 4 || !r.EOF || !r.Partial || !reflect.DeepEqual(diagCodes(r.Diagnostics), []string{DiagIncompleteTail}) {
		t.Fatalf("second %+v", r)
	}
}
func TestSearchCancellation(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.jsonl"), pi3("x")+strings.Repeat("z", MaxLineBytes+1))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := svc(cfgSource("pi", "pi", dir)).Search(ctx, SearchRequest{Query: "z", Limit: 1})
	if err == nil || err.Code != CodeCanceled {
		t.Fatalf("cancellation: %v", err)
	}
}
func TestSearchNoNativeWrites(t *testing.T) {
	root := copyFixtures(t)
	before := map[string]string{}
	_ = filepath.WalkDir(root, func(p string, _ os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		data, err := os.ReadFile(p)
		if err == nil {
			before[p] = string(data)
		}
		return nil
	})
	_ = mustSearch(t, svc(fixtureSources(root)...), SearchRequest{Query: "widget"})
	after := map[string]string{}
	_ = filepath.WalkDir(root, func(p string, _ os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		data, err := os.ReadFile(p)
		if err == nil {
			after[p] = string(data)
		}
		return nil
	})
	if !reflect.DeepEqual(before, after) {
		t.Fatal("native files or directory entries changed")
	}
}
