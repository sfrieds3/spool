//go:build unix

package history

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestConfinementAndSymlinks(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "root")
	outside := filepath.Join(base, "outside")
	writeFile(t, filepath.Join(root, "ok.jsonl"), pi3("ok"))
	writeFile(t, filepath.Join(outside, "secret.jsonl"), pi3("secret"))
	writeFile(t, filepath.Join(outside, "dir/inner.jsonl"), pi3("inner"))
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.Symlink(filepath.Join(outside, "secret.jsonl"), filepath.Join(root, "link.jsonl")))
	must(os.Symlink(filepath.Join(outside, "dir"), filepath.Join(root, "linkdir")))
	must(os.Symlink("ok.jsonl", filepath.Join(root, "inside-link.jsonl")))
	must(syscall.Mkfifo(filepath.Join(root, "fifo.jsonl"), 0o644))

	s := svc(cfgSource("pi", "pi", root))
	list := mustList(t, s, ListRequest{})
	if len(list.Sessions) != 1 || list.Sessions[0].NativeID != "ok" {
		t.Fatalf("sessions = %+v", list.Sessions)
	}
	codes := map[string]string{}
	for _, d := range list.Diagnostics {
		codes[*d.Path] = d.Code
	}
	want := map[string]string{
		"link.jsonl":        DiagSymlinkSkipped,
		"linkdir":           DiagSymlinkSkipped,
		"inside-link.jsonl": DiagSymlinkSkipped,
		"fifo.jsonl":        DiagNotRegularFile,
	}
	for p, c := range want {
		if codes[p] != c {
			t.Errorf("diagnostic for %s = %q, want %q (all: %v)", p, codes[p], c, codes)
		}
	}
	if !list.Partial {
		t.Error("skipped candidates must mark the listing partial")
	}

	// Forged references to symlinked or special files are not read.
	for _, p := range []string{"link.jsonl", "linkdir/inner.jsonl", "fifo.jsonl"} {
		ref := Ref{SourceID: "pi", Harness: "pi", Path: p, NativeID: "secret"}.Encode()
		if code := readErr(t, s, ref); code != CodeSessionNotFound {
			t.Errorf("read %s = %s", p, code)
		}
	}
}

func TestSelectedRootMayBeSymlink(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "real")
	writeFile(t, filepath.Join(real, "a.jsonl"), pi3("via-link"))
	link := filepath.Join(base, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	s := svc(cfgSource("pi", "pi", link))
	list := mustList(t, s, ListRequest{})
	if len(list.Sessions) != 1 {
		t.Fatalf("sessions = %+v diags = %+v", list.Sessions, list.Diagnostics)
	}
	if list.Sessions[0].OriginalPath != filepath.Join(link, "a.jsonl") {
		t.Fatalf("original path = %s", list.Sessions[0].OriginalPath)
	}
	mustRead(t, s, list.Sessions[0].Ref, 0, 10)
}

func TestDiscoveryCancellation(t *testing.T) {
	root := copyFixtures(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := svc(fixtureSources(root)...)
	if _, err := s.List(ctx, ListRequest{Limit: 10}); err == nil || err.Code != CodeCanceled {
		t.Fatalf("list err = %v", err)
	}
	ref := Ref{SourceID: "pi", Harness: "pi", Path: "--home-example-project--/2026-01-02T03-04-05-000Z_example-custom-id.jsonl", NativeID: "example-custom-id"}.Encode()
	if _, err := s.Read(ctx, ReadRequest{Ref: ref, Limit: 10}); err == nil || err.Code != CodeCanceled {
		t.Fatalf("read err = %v", err)
	}
}
