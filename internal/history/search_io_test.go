package history

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sfrieds3/spool/internal/jsonl"
)

// writeExactSession builds a full-size file from valid, individually bounded
// physical records so the test exercises the actual request byte accounting.
func writeExactSession(t *testing.T, path string, size int64) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	header := pi3("large")
	if _, err = f.WriteString(header); err != nil {
		t.Fatal(err)
	}
	remaining := size - int64(len(header))
	const overhead = int64(len(`{"type":"custom","data":""}`) + 1)
	chunk := `{"type":"custom","data":"` + strings.Repeat("a", 2<<20) + `"}` + "\n"
	for remaining >= int64(len(chunk))+overhead {
		if _, err = f.WriteString(chunk); err != nil {
			t.Fatal(err)
		}
		remaining -= int64(len(chunk))
	}
	if remaining < overhead {
		t.Fatalf("bad remainder %d", remaining)
	}
	last := `{"type":"custom","data":"` + strings.Repeat("b", int(remaining-overhead)) + `"}` + "\n"
	if int64(len(last)) != remaining {
		t.Fatalf("last size %d != %d", len(last), remaining)
	}
	if _, err = f.WriteString(last); err != nil {
		t.Fatal(err)
	}
}

func TestSearchNativeRequestByteBoundary(t *testing.T) {
	if testing.Short() {
		t.Skip("128 MiB native-read integration fixture")
	}
	dir := t.TempDir()
	writeExactSession(t, filepath.Join(dir, "a.jsonl"), MaxSearchFileBytes)
	writeExactSession(t, filepath.Join(dir, "b.jsonl"), MaxSearchFileBytes)
	writeFile(t, filepath.Join(dir, "c.jsonl"), pi3("c", searchBody("needle")))
	s := svc(cfgSource("pi", "pi", dir))
	first := mustSearch(t, s, SearchRequest{Query: "needle", Limit: 1})
	if first.EOF || first.NextCursor == nil || len(first.Hits) != 0 || len(first.Diagnostics) != 1 || first.Diagnostics[0].Code != DiagScanLimit || first.Diagnostics[0].Path == nil || *first.Diagnostics[0].Path != "c.jsonl" {
		t.Fatalf("request boundary %+v", first)
	}
	second := mustSearch(t, s, SearchRequest{Query: "needle", Limit: 1, Cursor: *first.NextCursor})
	if !second.EOF || len(second.Hits) != 1 || second.Hits[0].Session.Path != "c.jsonl" {
		t.Fatalf("after boundary %+v", second)
	}
}

func TestSearchCountsBufferedReadAhead(t *testing.T) {
	content := []byte("a\n" + strings.Repeat("x", 100000) + "\n")
	r := &countedExtent{ctx: t.Context(), source: bytes.NewReader(content), remaining: int64(len(content))}
	lines := jsonl.NewReader(r, 2)
	line, err := lines.Next()
	if err != nil || line.Number != 1 || lines.Consumed() != 2 || r.read <= lines.Consumed() {
		t.Fatalf("read ahead line=%+v read=%d consumed=%d error=%v", line, r.read, lines.Consumed(), err)
	}
	line, err = lines.Next()
	if err != nil || !line.Oversized || r.read != int64(len(content)) || lines.Consumed() != int64(len(content)) {
		t.Fatalf("drained line=%+v read=%d error=%v", line, r.read, err)
	}
}

func TestSearchCapturedExtentAppendAndShrink(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.jsonl")
	writeFile(t, path, pi3("a", searchBody("needle")))
	root, err := openSourceRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	file, info, err := openRegular(root, "a.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	appendFile, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = appendFile.WriteString(searchBody("later needle") + "\n"); err != nil {
		t.Fatal(err)
	}
	_ = appendFile.Close()
	out := scanSearchFile(searchFileInput{t.Context(), cfgSource("pi", "pi", dir), root, "a.jsonl", file, info, 0, newLiteralMatcher("needle", false), 10, 1 << 20, 1 << 20})
	_ = file.Close()
	if out.fatal != nil || len(out.hits) != 1 || out.hits[0].Line != 2 || out.bytesRead != info.Size() {
		t.Fatalf("captured append %+v", out)
	}
	file, info, err = openRegular(root, "a.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Truncate(path, info.Size()-8); err != nil {
		t.Fatal(err)
	}
	out = scanSearchFile(searchFileInput{t.Context(), cfgSource("pi", "pi", dir), root, "a.jsonl", file, info, 0, newLiteralMatcher("needle", false), 10, 1 << 20, 1 << 20})
	_ = file.Close()
	if out.fatal == nil || out.fatal.Code != DiagFileUnreadable || len(out.hits) != 0 {
		t.Fatalf("unexpected EOF %+v", out)
	}
}

type cancelOnRead struct {
	data  *bytes.Reader
	reads int
	stop  context.CancelFunc
}

func (r *cancelOnRead) Read(p []byte) (int, error) {
	r.reads++
	if r.reads == 3 {
		r.stop()
	}
	return r.data.Read(p)
}
func TestSearchCancellationDuringOversizedLineDrain(t *testing.T) {
	ctx, stop := context.WithCancel(t.Context())
	defer stop()
	src := &cancelOnRead{data: bytes.NewReader(bytes.Repeat([]byte("x"), MaxLineBytes+100000)), stop: stop}
	extent := &countedExtent{ctx: ctx, source: src, remaining: int64(src.data.Len())}
	_, err := jsonl.NewReader(extent, MaxLineBytes).Next()
	if !errors.Is(err, context.Canceled) || src.reads >= 100 || extent.read >= int64(MaxLineBytes) {
		t.Fatalf("drain cancellation reads=%d bytes=%d error=%v", src.reads, extent.read, err)
	}
	if n, e := io.Copy(io.Discard, extent); n != 0 || !errors.Is(e, context.Canceled) {
		t.Fatalf("context leaked: %d %v", n, e)
	}
}
