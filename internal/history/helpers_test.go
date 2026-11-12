package history

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sfrieds3/spool/internal/config"
)

const fixtureRoot = "../../testdata/sessions"

// copyFixtures copies the synthetic fixtures into a temporary directory and
// returns its path.
func copyFixtures(t *testing.T) string {
	t.Helper()
	dst := t.TempDir()
	err := filepath.WalkDir(fixtureRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(fixtureRoot, p)
		target := filepath.Join(dst, rel)
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
	return dst
}

func fixtureSources(root string) []config.Source {
	return []config.Source{
		{ID: "pi", Harness: "pi", Root: filepath.Join(root, "pi"), Enabled: true},
		{ID: "claude", Harness: "claude", Root: filepath.Join(root, "claude"), Enabled: true},
		{ID: "codex", Harness: "codex", Root: filepath.Join(root, "codex"), Enabled: true},
		{ID: "codex-archive", Harness: "codex", Root: filepath.Join(root, "codex-archive"), Enabled: true},
	}
}

func svc(sources ...config.Source) *Service {
	return NewService(&config.Config{Sources: sources})
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustList(t *testing.T, s *Service, req ListRequest) *ListResponse {
	t.Helper()
	if req.Limit == 0 {
		req.Limit = DefaultLimit
	}
	resp, err := s.List(context.Background(), req)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	return resp
}

func mustRead(t *testing.T, s *Service, ref string, offset int64, limit int) *ReadResponse {
	t.Helper()
	resp, err := s.Read(context.Background(), ReadRequest{Ref: ref, Offset: offset, Limit: limit})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	return resp
}

func readErr(t *testing.T, s *Service, ref string) string {
	t.Helper()
	_, err := s.Read(context.Background(), ReadRequest{Ref: ref, Limit: DefaultLimit})
	if err == nil {
		t.Fatal("Read succeeded, want error")
	}
	return err.Code
}

func refFor(t *testing.T, resp *ListResponse, pathSuffix string) string {
	t.Helper()
	for _, s := range resp.Sessions {
		if strings.HasSuffix(s.Path, pathSuffix) {
			return s.Ref
		}
	}
	t.Fatalf("no session with path suffix %q in %+v", pathSuffix, resp.Sessions)
	return ""
}

func diagCodes(d []Diagnostic) []string {
	var out []string
	for _, x := range d {
		out = append(out, x.Code)
	}
	return out
}

// pi3 builds a Pi v3 session with n body records.
func pi3(id string, body ...string) string {
	var b strings.Builder
	b.WriteString(`{"type":"session","version":3,"id":"` + id + `","timestamp":"2026-01-01T00:00:00Z","cwd":"/home/example/p"}` + "\n")
	for _, l := range body {
		b.WriteString(l + "\n")
	}
	return b.String()
}

func cfgSource(id, harness, root string) config.Source {
	return config.Source{ID: id, Harness: harness, Root: root, Enabled: true}
}
