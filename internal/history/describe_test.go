package history

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/sfrieds3/spool/internal/config"
)

func describeErr(t *testing.T, s *Service, ref string) string {
	t.Helper()
	_, err := s.Describe(context.Background(), ref)
	if err == nil {
		t.Fatal("Describe succeeded, want error")
	}
	return err.Code
}

func TestDescribeMatchesReadSession(t *testing.T) {
	s := svc(fixtureSources(copyFixtures(t))...)
	list := mustList(t, s, ListRequest{})
	if len(list.Sessions) == 0 {
		t.Fatal("no fixture sessions")
	}
	for _, sess := range list.Sessions {
		got, err := s.Describe(context.Background(), sess.Ref)
		if err != nil {
			t.Fatalf("%s: %v", sess.Path, err)
		}
		read := mustRead(t, s, sess.Ref, 0, 1)
		if !reflect.DeepEqual(*got, read.Session) {
			t.Fatalf("%s: describe = %+v, read = %+v", sess.Path, *got, read.Session)
		}
		if !got.MetadataComplete {
			t.Errorf("%s: metadata_complete false", sess.Path)
		}
	}
}

func TestDescribeErrorsMatchRead(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.jsonl")
	writeFile(t, good, pi3("p", `{"type":"custom","id":"1"}`))
	late := filepath.Join(dir, "late.jsonl")
	// An unsupported composite marker after the first 1 MiB is still found.
	writeFile(t, late, `{"type":"user","sessionId":"cs","uuid":"u1","parentUuid":null,"cwd":"/home/example"}`+"\n"+
		`{"type":"user","sessionId":"cs","uuid":"u2","parentUuid":"u1","message":"`+strings.Repeat("x", MetadataScanBytes+10)+`"}`+"\n"+
		`{"type":"fork-context-ref","agentId":"a","parentSessionId":"other","parentLastUuid":"u0","contextLength":3}`+"\n")
	s := svc(cfgSource("pi", "pi", dir), cfgSource("claude", "claude", dir), config.Source{ID: "off", Harness: "pi", Root: dir})
	enc := func(raw string) string { return "s1." + base64.RawURLEncoding.EncodeToString([]byte(raw)) }
	cases := map[string]string{
		"malformed":        "s1.!!",
		"traversal":        enc(`["pi","pi","../x.jsonl","p"]`),
		"disabled source":  Ref{SourceID: "off", Harness: "pi", Path: "good.jsonl", NativeID: "p"}.Encode(),
		"missing":          Ref{SourceID: "pi", Harness: "pi", Path: "gone.jsonl", NativeID: "p"}.Encode(),
		"replaced":         Ref{SourceID: "pi", Harness: "pi", Path: "good.jsonl", NativeID: "other"}.Encode(),
		"late unsupported": Ref{SourceID: "claude", Harness: "claude", Path: "late.jsonl", NativeID: "cs"}.Encode(),
	}
	want := map[string]string{
		"malformed": CodeInvalidRequest, "traversal": CodeInvalidRequest, "disabled source": CodeInvalidRequest,
		"missing": CodeSessionNotFound, "replaced": CodeSessionNotFound, "late unsupported": CodeUnsupportedFormat,
	}
	for name, ref := range cases {
		r, d := readErr(t, s, ref), describeErr(t, s, ref)
		if r != d || d != want[name] {
			t.Errorf("%s: read = %s, describe = %s, want %s", name, r, d, want[name])
		}
	}
	// Describe returns no records and does not modify the file.
	before, _ := os.ReadFile(good)
	sess, err := s.Describe(context.Background(), Ref{SourceID: "pi", Harness: "pi", Path: "good.jsonl", NativeID: "p"}.Encode())
	if err != nil || sess.NativeID != "p" || sess.Cwd == nil || *sess.Cwd != "/home/example/p" {
		t.Fatalf("describe = %+v %v", sess, err)
	}
	after, _ := os.ReadFile(good)
	if string(before) != string(after) {
		t.Fatal("source changed")
	}
}
