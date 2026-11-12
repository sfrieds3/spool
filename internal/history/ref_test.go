package history

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sfrieds3/spool/internal/config"
)

func rawRef(content string) string {
	return refPrefix + base64.RawURLEncoding.EncodeToString([]byte(content))
}

func TestRefRoundTrip(t *testing.T) {
	r := Ref{SourceID: "codex-archive", Harness: "codex", Path: "2026/01/rollout-<x>&é.jsonl", NativeID: "id/with\"quote"}
	got, err := DecodeRef(r.Encode())
	if err != nil || got != r {
		t.Fatalf("round trip = %+v, %v", got, err)
	}
	if r.Encode() != r.Encode() {
		t.Fatal("encoding is not deterministic")
	}
}

func TestDecodeRefRejects(t *testing.T) {
	valid := Ref{SourceID: "pi", Harness: "pi", Path: "a/b.jsonl", NativeID: "x"}.Encode()
	tests := map[string]string{
		"empty":            "",
		"no prefix":        strings.TrimPrefix(valid, refPrefix),
		"other version":    "s2." + strings.TrimPrefix(valid, refPrefix),
		"bad base64":       refPrefix + "!!!",
		"padded base64":    valid + "=",
		"not json":         rawRef(`nope`),
		"object":           rawRef(`{"a":1}`),
		"three fields":     rawRef(`["pi","pi","a.jsonl"]`),
		"trailing":         rawRef(`["pi","pi","a.jsonl","x"] []`),
		"bad source":       rawRef(`["../pi","pi","a.jsonl","x"]`),
		"bad harness":      rawRef(`["pi","cursor","a.jsonl","x"]`),
		"absolute path":    rawRef(`["pi","pi","/etc/passwd","x"]`),
		"traversal":        rawRef(`["pi","pi","../outside.jsonl","x"]`),
		"inner traversal":  rawRef(`["pi","pi","a/../../outside.jsonl","x"]`),
		"dot segment":      rawRef(`["pi","pi","./a.jsonl","x"]`),
		"backslash":        rawRef(`["pi","pi","a\\..\\b.jsonl","x"]`),
		"nul":              rawRef(`["pi","pi","a\u0000.jsonl","x"]`),
		"empty native id":  rawRef(`["pi","pi","a.jsonl",""]`),
		"non-canonical":    rawRef(`["pi", "pi","a.jsonl","x"]`),
		"oversized":        refPrefix + strings.Repeat("A", MaxRefLength),
		"long native id":   Ref{SourceID: "pi", Harness: "pi", Path: "a.jsonl", NativeID: strings.Repeat("x", maxNativeIDBytes+1)}.Encode(),
		"trailing slash":   rawRef(`["pi","pi","a/","x"]`),
		"double separator": rawRef(`["pi","pi","a//b.jsonl","x"]`),
	}
	for name, ref := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := DecodeRef(ref)
			if e, ok := err.(*Error); !ok || e.Code != CodeInvalidRequest {
				t.Fatalf("err = %v, want invalid_request", err)
			}
		})
	}
}

func TestReadResolvesOnlyThroughEnabledSources(t *testing.T) {
	root := copyFixtures(t)
	sources := fixtureSources(root)
	s := svc(sources...)
	ref := refFor(t, mustList(t, s, ListRequest{}), "example-custom-id.jsonl")

	disabled := append([]config.Source(nil), sources...)
	disabled[0].Enabled = false
	if code := readErr(t, svc(disabled...), ref); code != CodeInvalidRequest {
		t.Fatalf("disabled source: %s", code)
	}
	unconfigured := Ref{SourceID: "other", Harness: "pi", Path: "a.jsonl", NativeID: "x"}.Encode()
	if code := readErr(t, s, unconfigured); code != CodeInvalidRequest {
		t.Fatalf("unconfigured source: %s", code)
	}
	// A traversal reference never reaches the filesystem.
	writeFile(t, filepath.Join(root, "outside.jsonl"), pi3("outside"))
	if code := readErr(t, s, rawRef(`["pi","pi","../outside.jsonl","outside"]`)); code != CodeInvalidRequest {
		t.Fatalf("traversal: %s", code)
	}
	// A path that is not a session candidate is rejected.
	if code := readErr(t, s, Ref{SourceID: "codex", Harness: "codex", Path: "notes.jsonl", NativeID: "x"}.Encode()); code != CodeInvalidRequest {
		t.Fatalf("non-candidate: %s", code)
	}
}

func TestDuplicateNativeIDsAreIsolated(t *testing.T) {
	dir := t.TempDir()
	other := t.TempDir()
	writeFile(t, filepath.Join(dir, "a/one.jsonl"), pi3("same", `{"type":"custom","id":"1","parentId":null,"customType":"first"}`))
	writeFile(t, filepath.Join(dir, "b/two.jsonl"), pi3("same", `{"type":"custom","id":"2","parentId":null,"customType":"second"}`))
	writeFile(t, filepath.Join(other, "a/one.jsonl"), pi3("same", `{"type":"custom","id":"3","parentId":null,"customType":"third"}`))
	s := svc(cfgSource("p1", "pi", dir), cfgSource("p2", "pi", other))
	list := mustList(t, s, ListRequest{})
	if len(list.Sessions) != 3 {
		t.Fatalf("sessions = %d", len(list.Sessions))
	}
	seen := map[string]string{}
	for _, sess := range list.Sessions {
		if prev, dup := seen[sess.Ref]; dup {
			t.Fatalf("duplicate ref for %s and %s", prev, sess.Path)
		}
		seen[sess.Ref] = sess.SourceID + ":" + sess.Path
		got := mustRead(t, s, sess.Ref, 0, 10)
		want := map[string]string{"p1:a/one.jsonl": "first", "p1:b/two.jsonl": "second", "p2:a/one.jsonl": "third"}[sess.SourceID+":"+sess.Path]
		if *got.Records[1].Subtype != want {
			t.Fatalf("%s read %s", sess.Path, *got.Records[1].Subtype)
		}
	}
	// Stable across repeated discovery.
	again := mustList(t, s, ListRequest{})
	for i := range again.Sessions {
		if again.Sessions[i].Ref != list.Sessions[i].Ref {
			t.Fatal("references changed across unchanged discovery")
		}
	}
}

func TestRenamedMissingAndReplacedFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	writeFile(t, path, pi3("original"))
	s := svc(cfgSource("pi", "pi", dir))
	ref := mustList(t, s, ListRequest{}).Sessions[0].Ref

	// Replaced with a different conversation: not silently confused.
	writeFile(t, path, pi3("replacement"))
	if code := readErr(t, s, ref); code != CodeSessionNotFound {
		t.Fatalf("replaced: %s", code)
	}
	// Renamed: old reference is gone.
	writeFile(t, path, pi3("original"))
	if err := os.Rename(path, filepath.Join(dir, "renamed.jsonl")); err != nil {
		t.Fatal(err)
	}
	if code := readErr(t, s, ref); code != CodeSessionNotFound {
		t.Fatalf("renamed: %s", code)
	}
	// Directory at the referenced path.
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if code := readErr(t, s, ref); code != CodeSessionNotFound {
		t.Fatalf("directory: %s", code)
	}
}
