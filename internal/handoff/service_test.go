package handoff

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sfrieds3/spool/internal/history"
)

func TestCreateShowRoundTrip(t *testing.T) {
	e := newEnv(t)
	refs := e.refs(t)
	brief := fmt.Sprintf(`{"version":1,"title":"Port","goal":"Finish the port","constraints":["no new deps"],"decisions":["use stdlib"],
		"progress":["parser done"],"outstanding":["tests","docs"],"notes":["n"],
		"workspace":{"repository":{"branch":"main"}},
		"sessions":[{"ref":%q,"note":"original plan"},{"ref":%q}]}`, refs["pi"], refs["codex"])
	h := e.create(t, brief)
	if !strings.HasPrefix(h.ID, "20261112t153000z-") {
		t.Fatalf("id = %s", h.ID)
	}
	if h.CreatedAt != "2026-11-12T15:30:00Z" || *h.Origin.Host != "example-host" || h.Origin.SpoolVersion != "v0.0.0-test" {
		t.Fatalf("origin = %+v %s", h.Origin, h.CreatedAt)
	}
	if h.Workspace.Path != "/home/example/project" || h.Workspace.PathProvenance != ProvenanceProcessCwd || *h.Workspace.Repository.Branch != "main" || h.Workspace.Repository.Provenance != ProvenanceAuthorReported {
		t.Fatalf("workspace = %+v", h.Workspace)
	}
	if len(h.Sessions) != 2 || h.Sessions[0].Harness != "pi" || h.Sessions[1].Harness != "codex" || *h.Sessions[0].Note != "original plan" || h.Sessions[1].Note != nil {
		t.Fatalf("sessions = %+v", h.Sessions)
	}
	described, _ := e.hist.Describe(context.Background(), refs["pi"])
	if h.Sessions[0].OriginalPath != described.OriginalPath || !reflect.DeepEqual(h.Sessions[0].Cwd, described.Cwd) || h.Sessions[0].NativeID != described.NativeID {
		t.Fatalf("snapshot = %+v, described = %+v", h.Sessions[0], described)
	}
	raw, _ := json.Marshal(h)
	if strings.Contains(string(raw), `"records"`) || strings.Contains(string(raw), `"native"`) {
		t.Fatal("handoff contains transcript records")
	}

	// Stored file and permissions.
	ents := e.entries(t)
	if len(ents) != 1 || ents[0] != h.ID+".json" {
		t.Fatalf("entries = %v", ents)
	}
	for p, want := range map[string]os.FileMode{e.dir(): 0o700, filepath.Join(e.dir(), ents[0]): 0o600} {
		info, err := os.Stat(p)
		if err != nil || info.Mode().Perm() != want {
			t.Fatalf("%s mode = %v, want %v", p, info.Mode().Perm(), want)
		}
	}

	show, err := e.svc.Show(context.Background(), h.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(show.Handoff, h) || show.Content != ContentHandoffBrief {
		t.Fatalf("show = %+v\nwant %+v", show.Handoff, h)
	}
}

func TestCreateMinimalAndAuthorPath(t *testing.T) {
	e := newEnv(t)
	h := e.create(t, `{"version":1,"goal":"g","workspace":{"path":"/srv/other"}}`)
	if h.Title != nil || h.Workspace.Path != "/srv/other" || h.Workspace.PathProvenance != ProvenanceAuthorReported || h.Workspace.Repository != nil {
		t.Fatalf("handoff = %+v", h)
	}
	for _, l := range [][]string{h.Constraints, h.Decisions, h.Progress, h.Outstanding, h.Notes} {
		if l == nil || len(l) != 0 {
			t.Fatal("lists must be empty")
		}
	}
	raw, _ := history.Marshal(h)
	if !strings.Contains(string(raw), `"decisions":[]`) || !strings.Contains(string(raw), `"title":null`) || !strings.Contains(string(raw), `"repository":null`) {
		t.Fatalf("encoding = %s", raw)
	}
}

func TestCreateHostAndCwdFallbacks(t *testing.T) {
	e := newEnv(t)
	e.svc.opts.Hostname = func() (string, error) { return "", errors.New("no host") }
	if h := e.create(t, `{"version":1,"goal":"g"}`); h.Origin.Host != nil {
		t.Fatal("failed host lookup should be null")
	}
	e.clock = e.clock.Add(time.Second)
	e.svc.opts.Hostname = func() (string, error) { return strings.Repeat("h", MaxHostBytes+1), nil }
	if h := e.create(t, `{"version":1,"goal":"g"}`); h.Origin.Host != nil {
		t.Fatal("oversized host should be null")
	}
	e.svc.opts.Getwd = func() (string, error) { return "", errors.New("gone") }
	if _, err := e.svc.Create(context.Background(), []byte(`{"version":1,"goal":"g"}`)); err == nil || err.Code != history.CodeInternal {
		t.Fatalf("getwd failure = %v", err)
	}
	e.svc.opts.Getwd = func() (string, error) { return "/" + strings.Repeat("d", MaxPathBytes), nil }
	if _, err := e.svc.Create(context.Background(), []byte(`{"version":1,"goal":"g"}`)); err == nil || err.Code != history.CodeInvalidRequest {
		t.Fatalf("long cwd = %v", err)
	}
	// Author-supplied path avoids the working directory entirely.
	if _, err := e.svc.Create(context.Background(), []byte(`{"version":1,"goal":"g","workspace":{"path":"/x"}}`)); err != nil {
		t.Fatal(err)
	}
}

func TestCreateFailuresWriteNothing(t *testing.T) {
	e := newEnv(t)
	refs := e.refs(t)
	missing := history.Ref{SourceID: "pi", Harness: "pi", Path: "gone.jsonl", NativeID: "x"}.Encode()
	disabled := history.Ref{SourceID: "codex-archive", Harness: "codex", Path: "a.jsonl", NativeID: "x"}.Encode()
	cases := map[string]struct{ brief, code string }{
		"invalid brief":     {`{"version":1}`, history.CodeInvalidRequest},
		"malformed ref":     {`{"version":1,"goal":"g","sessions":[{"ref":"s1.!!"}]}`, history.CodeInvalidRequest},
		"missing session":   {fmt.Sprintf(`{"version":1,"goal":"g","sessions":[{"ref":%q},{"ref":%q}]}`, refs["pi"], missing), history.CodeSessionNotFound},
		"disabled source":   {fmt.Sprintf(`{"version":1,"goal":"g","sessions":[{"ref":%q}]}`, disabled), history.CodeInvalidRequest},
		"duplicate session": {fmt.Sprintf(`{"version":1,"goal":"g","sessions":[{"ref":%q},{"ref":%q}]}`, refs["pi"], refs["pi"]), history.CodeInvalidRequest},
	}
	for name, tc := range cases {
		_, err := e.svc.Create(context.Background(), []byte(tc.brief))
		if err == nil || err.Code != tc.code {
			t.Errorf("%s: err = %v, want %s", name, err, tc.code)
		}
	}
	mustNotExist(t, e.state)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := e.svc.Create(ctx, []byte(fmt.Sprintf(`{"version":1,"goal":"g","sessions":[{"ref":%q}]}`, refs["pi"]))); err == nil || err.Code != history.CodeCanceled {
		t.Fatalf("canceled = %v", err)
	}
	mustNotExist(t, e.state)
}

func TestCreateCanceledWithoutSessions(t *testing.T) {
	for _, stage := range []string{"before create", "during metadata", "before publication"} {
		t.Run(stage, func(t *testing.T) {
			e := newEnv(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch stage {
			case "before create":
				cancel()
			case "during metadata":
				e.svc.opts.Getwd = func() (string, error) { cancel(); return "/workspace", nil }
			case "before publication":
				create := e.svc.ops.create
				e.svc.ops.create = func(root *os.Root, name string) (tempFile, error) {
					cancel()
					return create(root, name)
				}
			}
			if _, err := e.svc.Create(ctx, []byte(`{"version":1,"goal":"g"}`)); err == nil || err.Code != history.CodeCanceled {
				t.Fatalf("canceled create = %v", err)
			}
			if stage == "before publication" {
				if entries := e.entries(t); len(entries) != 0 {
					t.Fatalf("canceled publication left files: %v", entries)
				}
			} else {
				mustNotExist(t, e.state)
			}
		})
	}
}

// Authored input alone cannot exceed MaxHandoffBytes (escapes do not expand
// beyond their input spelling), but large session snapshots can, so the
// encoded-size check is exercised directly.
func TestCreateRejectsOversizedHandoff(t *testing.T) {
	e := newEnv(t)
	h := &Handoff{Version: Version, Goal: "g", Notes: make([]string, MaxItems), Constraints: []string{}, Decisions: []string{}, Progress: []string{}, Outstanding: []string{}, Sessions: []SessionSnapshot{},
		Origin: Origin{SpoolVersion: "v"}, CreatedAt: "2026-11-12T15:30:00Z", Workspace: Workspace{Path: "/x", PathProvenance: ProvenanceProcessCwd}}
	for i := range h.Notes {
		h.Notes[i] = strings.Repeat("\x01", MaxItemBytes)
	}
	var data []byte
	err := e.service([]byte{1, 2, 3, 4}).assignID(h, e.clock, &data)
	if err == nil || err.Code != history.CodeInvalidRequest || !strings.Contains(err.Message, "exceeds") {
		t.Fatalf("oversized = %v", err)
	}
	if data != nil {
		t.Fatal("oversized handoff was encoded for writing")
	}
}

func TestMaximalHandoffFitsAndIsReadable(t *testing.T) {
	e := newEnv(t)
	item := strings.Repeat("a", MaxItemBytes)
	list := func() []string {
		out := make([]string, MaxItems)
		for i := range out {
			out[i] = item
		}
		return out
	}
	h := &Handoff{Version: Version, Goal: item, Title: ptr(strings.Repeat("t", MaxTitleBytes)),
		Constraints: list(), Decisions: []string{}, Progress: []string{}, Outstanding: []string{}, Notes: []string{}, Sessions: []SessionSnapshot{},
		Origin: Origin{SpoolVersion: "v", Host: ptr(strings.Repeat("h", MaxHostBytes))}, CreatedAt: "2026-11-12T15:30:00Z",
		Workspace: Workspace{Path: "/" + strings.Repeat("p", MaxPathBytes-1), PathProvenance: ProvenanceAuthorReported}}
	var data []byte
	if err := e.svc.assignID(h, e.clock, &data); err != nil {
		t.Fatalf("assignID: %v", err)
	}
	if err := os.MkdirAll(e.dir(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.dir(), fileName(h.ID)), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Show(context.Background(), h.ID); err != nil {
		t.Fatalf("show maximal: %v", err)
	}
}

func TestCreateCollisionRetriesWithSameTimestamp(t *testing.T) {
	e := newEnv(t)
	// The first ID's random bytes repeat, so the second create collides once.
	first := e.service([]byte{1, 1, 1, 1, 0, 0, 0, 0, 0, 0, 0, 1})
	h1, err := first.Create(context.Background(), []byte(`{"version":1,"goal":"one"}`))
	if err != nil {
		t.Fatal(err)
	}
	second := e.service([]byte{1, 1, 1, 1, 0, 0, 0, 0, 0, 0, 0, 2, 2, 2, 2, 2, 0, 0, 0, 0, 0, 0, 0, 3})
	h2, err := second.Create(context.Background(), []byte(`{"version":1,"goal":"two"}`))
	if err != nil {
		t.Fatal(err)
	}
	if h2.Handoff.ID != "20261112t153000z-02020202" || h1.Handoff.ID != "20261112t153000z-01010101" || h2.Handoff.CreatedAt != h1.Handoff.CreatedAt {
		t.Fatalf("ids = %s %s", h1.Handoff.ID, h2.Handoff.ID)
	}
	show, serr := e.svc.Show(context.Background(), h1.Handoff.ID)
	if serr != nil || show.Handoff.Goal != "one" {
		t.Fatalf("original overwritten: %+v %v", show, serr)
	}
	if ents := e.entries(t); len(ents) != 2 || contains(ents, ".tmp-") {
		t.Fatalf("entries = %v", ents)
	}
	// Exhausting attempts reports state_unavailable without overwriting.
	stuck := make([]byte, 0, 12*maxLinkAttempts)
	for i := 0; i < maxLinkAttempts; i++ {
		stuck = append(stuck, 1, 1, 1, 1, 0, 0, 0, 0, 0, 0, 0, byte(i))
	}
	if _, err := e.service(stuck).Create(context.Background(), []byte(`{"version":1,"goal":"three"}`)); err == nil || err.Code != CodeStateUnavailable {
		t.Fatalf("exhausted = %v", err)
	}
	if ents := e.entries(t); len(ents) != 2 {
		t.Fatalf("entries = %v", ents)
	}
}

func TestConcurrentCreators(t *testing.T) {
	e := newEnv(t)
	var wg sync.WaitGroup
	ids := make(chan string, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s := NewService(e.cfg, e.hist, e.state, Options{Now: func() time.Time { return e.clock }, Getwd: func() (string, error) { return "/w", nil }})
			r, err := s.Create(context.Background(), []byte(fmt.Sprintf(`{"version":1,"goal":"g%d"}`, i)))
			if err != nil {
				t.Error(err)
				return
			}
			ids <- r.Handoff.ID
		}(i)
	}
	wg.Wait()
	close(ids)
	seen := map[string]bool{}
	for id := range ids {
		seen[id] = true
	}
	if len(seen) != 16 || len(e.entries(t)) != 16 {
		t.Fatalf("published %d distinct, %d entries", len(seen), len(e.entries(t)))
	}
}

func TestPublishFailureInjection(t *testing.T) {
	boom := errors.New("injected")
	type hook func(*fileOps)
	failing := map[string]hook{
		"create": func(o *fileOps) {
			o.create = func(*os.Root, string) (tempFile, error) { return nil, boom }
		},
		"short write": func(o *fileOps) {
			inner := o.create
			o.create = func(r *os.Root, n string) (tempFile, error) {
				f, err := inner(r, n)
				return shortFile{f}, err
			}
		},
		"sync": func(o *fileOps) {
			inner := o.create
			o.create = func(r *os.Root, n string) (tempFile, error) {
				f, err := inner(r, n)
				return syncFail{f}, err
			}
		},
		"link": func(o *fileOps) {
			o.link = func(*os.Root, string, string) error { return boom }
		},
	}
	for name, h := range failing {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			h(&e.svc.ops)
			_, err := e.svc.Create(context.Background(), []byte(`{"version":1,"goal":"g"}`))
			if err == nil || err.Code != CodeStateUnavailable {
				t.Fatalf("err = %v", err)
			}
			if ents := e.entries(t); len(ents) != 0 {
				t.Fatalf("left entries %v", ents)
			}
		})
	}
	warning := map[string]struct {
		hook hook
		code string
	}{
		"cleanup":  {func(o *fileOps) { o.remove = func(*os.Root, string) error { return boom } }, DiagTempCleanupFailed},
		"dir sync": {func(o *fileOps) { o.syncDir = func(*os.Root) error { return boom } }, DiagDirectorySyncFailed},
	}
	for name, w := range warning {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			w.hook(&e.svc.ops)
			resp, err := e.svc.Create(context.Background(), []byte(`{"version":1,"goal":"g"}`))
			if err != nil || !resp.OK || len(resp.Warnings) != 1 || resp.Warnings[0].Code != w.code {
				t.Fatalf("resp = %+v err = %v", resp, err)
			}
			if _, err := e.svc.Show(context.Background(), resp.Handoff.ID); err != nil {
				t.Fatalf("published handoff unreadable: %v", err)
			}
			list, _ := e.svc.List(context.Background(), ListRequest{Limit: 10})
			if len(list.Handoffs) != 1 || list.Partial {
				t.Fatalf("list = %+v", list)
			}
		})
	}
}

type shortFile struct{ tempFile }

func (f shortFile) Write(p []byte) (int, error) { return f.tempFile.Write(p[:len(p)/2]) }

type syncFail struct{ tempFile }

func (syncFail) Sync() error { return errors.New("injected sync") }

func TestCreateInventsNothingAndRejectsUnsupportedSessions(t *testing.T) {
	e := newEnv(t)
	refs := e.refs(t)
	h := e.create(t, fmt.Sprintf(`{"version":1,"goal":"g","sessions":[{"ref":%q},{"ref":%q}]}`, refs["pi"], refs["claude"]))
	for _, l := range [][]string{h.Constraints, h.Decisions, h.Progress, h.Outstanding, h.Notes} {
		if len(l) != 0 {
			t.Fatalf("content derived from history: %+v", h)
		}
	}
	if h.Title != nil || h.Goal != "g" {
		t.Fatalf("handoff = %+v", h)
	}

	// A session whose external-history marker appears late in the file.
	path := filepath.Join(e.sessions, "claude", "p", "late.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	content := `{"type":"user","sessionId":"late","uuid":"u1","parentUuid":null,"cwd":"/home/example"}` + "\n" +
		`{"type":"user","sessionId":"late","uuid":"u2","parentUuid":"u1","message":"` + strings.Repeat("x", history.MetadataScanBytes) + `"}` + "\n" +
		`{"type":"fork-context-ref","agentId":"a","parentSessionId":"other","parentLastUuid":"u0","contextLength":3}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	ref := history.Ref{SourceID: "claude", Harness: "claude", Path: "p/late.jsonl", NativeID: "late"}.Encode()
	before := e.entries(t)
	e.clock = e.clock.Add(time.Second)
	if _, err := e.svc.Create(context.Background(), []byte(fmt.Sprintf(`{"version":1,"goal":"g","sessions":[{"ref":%q}]}`, ref))); err == nil || err.Code != history.CodeUnsupportedFormat {
		t.Fatalf("unsupported = %v", err)
	}
	if after := e.entries(t); len(after) != len(before) {
		t.Fatalf("entries changed: %v -> %v", before, after)
	}
}
