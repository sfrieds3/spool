package handoff

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sfrieds3/spool/internal/config"
	"github.com/sfrieds3/spool/internal/history"
)

const fixtureRoot = "../../testdata/sessions"

// env is an isolated fixture copy, history service, and state directory.
type env struct {
	sessions string
	state    string
	cfg      *config.Config
	hist     *history.Service
	clock    time.Time
	svc      *Service
}

func newEnv(t *testing.T) *env {
	t.Helper()
	base := t.TempDir()
	e := &env{sessions: filepath.Join(base, "sessions"), state: filepath.Join(base, "state", "spool")}
	err := filepath.WalkDir(fixtureRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(fixtureRoot, p)
		target := filepath.Join(e.sessions, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	e.cfg = &config.Config{Sources: []config.Source{
		{ID: "pi", Harness: "pi", Root: filepath.Join(e.sessions, "pi"), Enabled: true},
		{ID: "claude", Harness: "claude", Root: filepath.Join(e.sessions, "claude"), Enabled: true},
		{ID: "codex", Harness: "codex", Root: filepath.Join(e.sessions, "codex"), Enabled: true},
		{ID: "codex-archive", Harness: "codex", Root: filepath.Join(e.sessions, "codex-archive"), Enabled: false},
	}}
	e.hist = history.NewService(e.cfg)
	e.clock = time.Date(2026, 11, 12, 15, 30, 0, 0, time.UTC)
	e.svc = e.service(nil)
	return e
}

// service returns a Service with deterministic dependencies. random, when
// non-nil, supplies the random bytes.
func (e *env) service(random []byte) *Service {
	opts := Options{
		SpoolVersion: "v0.0.0-test",
		Now:          func() time.Time { return e.clock },
		Hostname:     func() (string, error) { return "example-host", nil },
		Getwd:        func() (string, error) { return "/home/example/project", nil },
	}
	if random != nil {
		opts.Random = bytes.NewReader(random)
	}
	return NewService(e.cfg, e.hist, e.state, opts)
}

func (e *env) refs(t *testing.T) map[string]string {
	t.Helper()
	list, err := e.hist.List(context.Background(), history.ListRequest{Limit: history.MaxLimit})
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, s := range list.Sessions {
		if s.Kind == "session" {
			out[s.Harness] = s.Ref
		}
	}
	return out
}

func (e *env) create(t *testing.T, brief string) *Handoff {
	t.Helper()
	resp, err := e.svc.Create(context.Background(), []byte(brief))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return resp.Handoff
}

func (e *env) dir() string { return filepath.Join(e.state, "handoffs") }

// entries lists names in the handoff directory, or nil when absent.
func (e *env) entries(t *testing.T) []string {
	t.Helper()
	ents, err := os.ReadDir(e.dir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatal(err)
	}
	var out []string
	for _, d := range ents {
		out = append(out, d.Name())
	}
	return out
}

func mustNotExist(t *testing.T, p string) {
	t.Helper()
	if _, err := os.Lstat(p); !os.IsNotExist(err) {
		t.Fatalf("%s exists (err %v)", p, err)
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if strings.Contains(v, s) {
			return true
		}
	}
	return false
}
