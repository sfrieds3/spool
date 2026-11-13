package handoff

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sfrieds3/spool/internal/config"
	"github.com/sfrieds3/spool/internal/history"
)

func TestExclusionFromHistoryRoots(t *testing.T) {
	base := t.TempDir()
	hist := filepath.Join(base, "hist")
	if err := os.MkdirAll(hist, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(hist, link); err != nil {
		t.Fatal(err)
	}
	missingUnderLink := filepath.Join(link, "missing-root")
	dangling := filepath.Join(base, "dangling")
	danglingState := filepath.Join(base, "future-state")
	if err := os.Symlink(filepath.Join(danglingState, "handoffs"), dangling); err != nil {
		t.Fatal(err)
	}
	cases := map[string]struct {
		root, state string
	}{
		"state inside root":            {hist, filepath.Join(hist, "state")},
		"state equals root":            {hist, hist},
		"state contains root":          {filepath.Join(base, "s", "handoffs", "deep"), filepath.Join(base, "s")},
		"state via symlinked ancestor": {hist, filepath.Join(link, "state")},
		"missing root under symlink":   {missingUnderLink, filepath.Join(hist, "missing-root", "x")},
		"disabled source still counts": {hist, filepath.Join(hist, "s")},
		"dangling root targets state":  {dangling, danglingState},
		"root under dangling link":     {filepath.Join(dangling, "nested"), danglingState},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			src := config.Source{ID: "pi", Harness: "pi", Root: tc.root, Enabled: name != "disabled source still counts"}
			cfg := &config.Config{Sources: []config.Source{src}}
			s := NewService(cfg, history.NewService(cfg), tc.state, Options{Getwd: func() (string, error) { return "/w", nil }})
			before := snapshotTree(t, base)
			_, err := s.Create(context.Background(), []byte(`{"version":1,"goal":"g"}`))
			if err == nil || err.Code != history.CodeInvalidConfig {
				t.Fatalf("err = %v", err)
			}
			if after := snapshotTree(t, base); after != before {
				t.Fatalf("filesystem changed:\n%s\n%s", before, after)
			}
		})
	}
	// A sibling directory sharing a name prefix is not an overlap.
	cfg := &config.Config{Sources: []config.Source{{ID: "pi", Harness: "pi", Root: hist, Enabled: true}}}
	s := NewService(cfg, history.NewService(cfg), hist+"-state", Options{Getwd: func() (string, error) { return "/w", nil }})
	if _, err := s.Create(context.Background(), []byte(`{"version":1,"goal":"g"}`)); err != nil {
		t.Fatalf("sibling prefix: %v", err)
	}
}

func TestExclusionCaseInsensitiveAlias(t *testing.T) {
	base := t.TempDir()
	hist := filepath.Join(base, "Hist")
	if err := os.MkdirAll(hist, 0o755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(base, "hist")
	if _, err := os.Stat(alias); err != nil {
		t.Skip("filesystem is case-sensitive")
	}
	cfg := &config.Config{Sources: []config.Source{{ID: "pi", Harness: "pi", Root: hist, Enabled: true}}}
	s := NewService(cfg, history.NewService(cfg), filepath.Join(alias, "state"), Options{Getwd: func() (string, error) { return "/w", nil }})
	if _, err := s.Create(context.Background(), []byte(`{"version":1,"goal":"g"}`)); err == nil || err.Code != history.CodeInvalidConfig {
		t.Fatalf("case alias = %v", err)
	}
}

func snapshotTree(t *testing.T, root string) string {
	t.Helper()
	var b strings.Builder
	filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err == nil {
			fmt.Fprintf(&b, "%s %v\n", p, info.Mode())
		}
		return nil
	})
	return b.String()
}

func TestInsecureOrInvalidHandoffDirectory(t *testing.T) {
	for name, setup := range map[string]func(t *testing.T, e *env){
		"group readable": func(t *testing.T, e *env) {
			os.MkdirAll(e.dir(), 0o700)
			os.Chmod(e.dir(), 0o750)
		},
		"world writable": func(t *testing.T, e *env) {
			os.MkdirAll(e.dir(), 0o700)
			os.Chmod(e.dir(), 0o707)
		},
		"symlink": func(t *testing.T, e *env) {
			real := filepath.Join(filepath.Dir(e.state), "real")
			os.MkdirAll(real, 0o700)
			os.MkdirAll(e.state, 0o700)
			if err := os.Symlink(real, e.dir()); err != nil {
				t.Fatal(err)
			}
		},
		"file": func(t *testing.T, e *env) {
			os.MkdirAll(e.state, 0o700)
			os.WriteFile(e.dir(), []byte("x"), 0o600)
		},
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			setup(t, e)
			before := snapshotTree(t, e.state)
			if _, err := e.svc.Create(context.Background(), []byte(`{"version":1,"goal":"g"}`)); err == nil || err.Code != CodeStateUnavailable {
				t.Fatalf("create = %v", err)
			}
			if _, err := e.svc.List(context.Background(), ListRequest{Limit: 10}); err == nil || err.Code != CodeStateUnavailable {
				t.Fatalf("list = %v", err)
			}
			if _, err := e.svc.Show(context.Background(), "20261112t153000z-00000000"); err == nil || err.Code != CodeStateUnavailable {
				t.Fatalf("show = %v", err)
			}
			if after := snapshotTree(t, e.state); after != before {
				t.Fatalf("permissions or entries changed:\n%s\n%s", before, after)
			}
		})
	}
}

func TestUnwritableStateDirectory(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root ignores permissions")
	}
	e := newEnv(t)
	parent := filepath.Dir(e.state)
	if err := os.MkdirAll(parent, 0o500); err != nil {
		t.Fatal(err)
	}
	os.Chmod(parent, 0o500)
	t.Cleanup(func() { os.Chmod(parent, 0o700) })
	if _, err := e.svc.Create(context.Background(), []byte(`{"version":1,"goal":"g"}`)); err == nil || err.Code != CodeStateUnavailable {
		t.Fatalf("err = %v", err)
	}
}

func TestShow(t *testing.T) {
	e := newEnv(t)
	if _, err := e.svc.Show(context.Background(), "20261112t153000z-00000000"); err == nil || err.Code != CodeHandoffNotFound {
		t.Fatalf("missing dir = %v", err)
	}
	mustNotExist(t, e.state)
	for _, bad := range []string{"../x", "20261112t153000z-0000000/", "..", "a/b", "20261112T153000Z-00000000"} {
		if _, err := e.svc.Show(context.Background(), bad); err == nil || err.Code != history.CodeInvalidRequest {
			t.Errorf("%q = %v", bad, err)
		}
	}
	h := e.create(t, `{"version":1,"goal":"g"}`)
	if _, err := e.svc.Show(context.Background(), "20261112t153000z-ffffffff"); err == nil || err.Code != CodeHandoffNotFound {
		t.Fatalf("unknown = %v", err)
	}
	good, _ := os.ReadFile(filepath.Join(e.dir(), fileName(h.ID)))
	invalid := map[string]func(id string){
		"unknown field": func(id string) {
			writeStored(t, e, id, strings.Replace(string(good), `"goal":`, `"extra":1,"goal":`, 1))
		},
		"future version": func(id string) { writeStored(t, e, id, strings.Replace(string(good), `"version":1`, `"version":2`, 1)) },
		"id mismatch":    func(id string) { writeStored(t, e, id, string(good)) },
		"missing key": func(id string) {
			writeStored(t, e, id, strings.Replace(withID(good, h.ID, id), `"title":null,`, ``, 1))
		},
		"null array": func(id string) {
			writeStored(t, e, id, strings.Replace(withID(good, h.ID, id), `"notes":[]`, `"notes":null`, 1))
		},
		"blank goal": func(id string) {
			writeStored(t, e, id, strings.Replace(withID(good, h.ID, id), `"goal":"g"`, `"goal":" "`, 1))
		},
		"bad provenance": func(id string) {
			writeStored(t, e, id, strings.Replace(withID(good, h.ID, id), ProvenanceProcessCwd, "guessed", 1))
		},
		"time mismatch": func(id string) {
			writeStored(t, e, id, strings.Replace(withID(good, h.ID, id), "15:30:00Z", "15:30:01Z", 1))
		},
		"duplicate key": func(id string) {
			writeStored(t, e, id, strings.Replace(withID(good, h.ID, id), `"goal":"g"`, `"goal":"g","goal":"h"`, 1))
		},
		"trailing": func(id string) { writeStored(t, e, id, withID(good, h.ID, id)+"{}") },
		"control in host": func(id string) {
			writeStored(t, e, id, strings.Replace(withID(good, h.ID, id), `"example-host"`, `"ex\u001b[31m"`, 1))
		},
		"oversized": func(id string) { writeStored(t, e, id, withID(good, h.ID, id)+strings.Repeat(" ", MaxHandoffBytes)) },
		"symlink": func(id string) {
			if err := os.Symlink(filepath.Join(e.dir(), fileName(h.ID)), filepath.Join(e.dir(), fileName(id))); err != nil {
				t.Fatal(err)
			}
		},
		"fifo": func(id string) { makeFifo(t, filepath.Join(e.dir(), fileName(id))) },
	}
	i := 0
	for name, setup := range invalid {
		i++
		id := fmt.Sprintf("20261112t153000z-%08x", i)
		setup(id)
		done := make(chan struct{})
		go func() {
			defer close(done)
			_, err := e.svc.Show(context.Background(), id)
			if err == nil || err.Code != CodeHandoffInvalid {
				t.Errorf("%s: err = %v", name, err)
			}
		}()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatalf("%s: show blocked", name)
		}
	}
}

func withID(data []byte, from, to string) string {
	return strings.Replace(string(data), `"id":"`+from+`"`, `"id":"`+to+`"`, 1)
}

func writeStored(t *testing.T, e *env, id, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(e.dir(), fileName(id)), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestList(t *testing.T) {
	e := newEnv(t)
	resp, err := e.svc.List(context.Background(), ListRequest{Limit: 50})
	if err != nil || len(resp.Handoffs) != 0 || !resp.EOF || resp.NextOffset != nil {
		t.Fatalf("empty = %+v %v", resp, err)
	}
	mustNotExist(t, e.state)
	for _, bad := range []ListRequest{{Offset: -1, Limit: 1}, {Limit: 0}, {Limit: history.MaxLimit + 1}} {
		if _, err := e.svc.List(context.Background(), bad); err == nil || err.Code != history.CodeInvalidRequest {
			t.Errorf("%+v = %v", bad, err)
		}
	}
	var ids []string
	for i := 0; i < 7; i++ {
		e.clock = e.clock.Add(time.Second)
		goal := fmt.Sprintf("goal %d", i)
		if i == 3 {
			goal = strings.Repeat("é", 400)
		}
		h := e.create(t, fmt.Sprintf(`{"version":1,"goal":%q,"outstanding":["a","b"]}`, goal))
		ids = append(ids, h.ID)
	}
	// Damaged row in the middle and ignored non-handoff entries.
	damaged := "20261112t153004z-00000000"
	writeStored(t, e, damaged, "{")
	os.WriteFile(filepath.Join(e.dir(), ".tmp-"+damaged+"-ab"), []byte("x"), 0o600)
	os.WriteFile(filepath.Join(e.dir(), "README"), []byte("x"), 0o600)

	var seen []string
	var diags []string
	offset := int64(0)
	for pages := 0; ; pages++ {
		if pages > 10 {
			t.Fatal("pagination did not terminate")
		}
		resp, err := e.svc.List(context.Background(), ListRequest{Offset: offset, Limit: 3})
		if err != nil {
			t.Fatal(err)
		}
		for _, h := range resp.Handoffs {
			seen = append(seen, h.ID)
			if h.OutstandingCount != 2 || h.SessionCount != 0 || h.WorkspacePath != "/home/example/project" {
				t.Fatalf("summary = %+v", h)
			}
		}
		for _, d := range resp.Diagnostics {
			diags = append(diags, *d.Path)
			if d.Code != DiagHandoffInvalid || !resp.Partial {
				t.Fatalf("diagnostic = %+v", d)
			}
		}
		if resp.EOF {
			break
		}
		offset = *resp.NextOffset
	}
	want := make([]string, 0, len(ids))
	for i := len(ids) - 1; i >= 0; i-- {
		want = append(want, ids[i])
	}
	if strings.Join(seen, ",") != strings.Join(want, ",") || len(diags) != 1 || diags[0] != fileName(damaged) {
		t.Fatalf("seen = %v\nwant = %v\ndiags = %v", seen, want, diags)
	}

	// Goal excerpt is cut at a UTF-8 boundary.
	full, _ := e.svc.List(context.Background(), ListRequest{Limit: 50})
	for _, h := range full.Handoffs {
		if h.ID == ids[3] {
			if !h.GoalTruncated || len(h.Goal) > MaxGoalExcerptBytes || !strings.HasPrefix(strings.Repeat("é", 400), h.Goal) {
				t.Fatalf("excerpt = %d bytes truncated=%v", len(h.Goal), h.GoalTruncated)
			}
		} else if h.GoalTruncated {
			t.Fatal("short goal marked truncated")
		}
	}
}

func TestListDiagnosticBudgetEndsPage(t *testing.T) {
	e := newEnv(t)
	if err := os.MkdirAll(e.dir(), 0o700); err != nil {
		t.Fatal(err)
	}
	n := history.MaxDiagnostics + 6
	for i := 0; i < n; i++ {
		writeStored(t, e, fmt.Sprintf("20261112t153000z-%08x", i), "{")
	}
	resp, err := e.svc.List(context.Background(), ListRequest{Limit: history.MaxLimit})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Diagnostics) != history.MaxDiagnostics || resp.OmittedDiagnostics != 0 || resp.EOF || *resp.NextOffset != int64(history.MaxDiagnostics) {
		t.Fatalf("first page: %d diags, next %v", len(resp.Diagnostics), resp.NextOffset)
	}
	resp, _ = e.svc.List(context.Background(), ListRequest{Offset: *resp.NextOffset, Limit: history.MaxLimit})
	if len(resp.Diagnostics) != 6 || !resp.EOF {
		t.Fatalf("second page: %d diags eof %v", len(resp.Diagnostics), resp.EOF)
	}
}

func TestListResponseByteBudget(t *testing.T) {
	e := newEnv(t)
	if err := os.MkdirAll(e.dir(), 0o700); err != nil {
		t.Fatal(err)
	}
	// Large titles and paths make each summary about 25 KiB escaped, so 500
	// rows exceed the 8 MiB response limit.
	title := strings.Repeat("\x01", MaxTitleBytes)
	path := "/" + strings.Repeat("\x01", MaxPathBytes-1)
	for i := 0; i < 400; i++ {
		h := &Handoff{Version: Version, ID: fmt.Sprintf("20261112t153000z-%08x", i), CreatedAt: "2026-11-12T15:30:00Z", Title: &title,
			Goal: strings.Repeat("\x01", MaxItemBytes), Constraints: []string{}, Decisions: []string{}, Progress: []string{}, Outstanding: []string{}, Notes: []string{},
			Origin: Origin{SpoolVersion: "v"}, Sessions: []SessionSnapshot{}, Workspace: Workspace{Path: path, PathProvenance: ProvenanceAuthorReported}}
		data, err := history.Marshal(h)
		if err != nil {
			t.Fatal(err)
		}
		writeStored(t, e, h.ID, string(data))
	}
	resp, err := e.svc.List(context.Background(), ListRequest{Limit: history.MaxLimit})
	if err != nil {
		t.Fatal(err)
	}
	out, _ := history.Marshal(resp)
	if resp.EOF || resp.NextOffset == nil || len(out) > history.MaxResponseBytes || len(resp.Handoffs) == 0 || resp.Partial {
		t.Fatalf("page: %d rows, %d bytes, eof %v", len(resp.Handoffs), len(out), resp.EOF)
	}
}
