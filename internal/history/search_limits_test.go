package history

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCountedExtentUnderlyingBytesAndCancellation(t *testing.T) {
	data := bytes.Repeat([]byte("x"), 200000)
	ctx, cancel := context.WithCancel(t.Context())
	r := &countedExtent{ctx: ctx, source: bytes.NewReader(data), remaining: 70000}
	buf := make([]byte, 200000)
	if n, err := r.Read(buf); err != nil || n != 64<<10 || r.read != 64<<10 {
		t.Fatalf("bounded read %d %v %d", n, err, r.read)
	}
	cancel()
	if n, err := r.Read(buf); n != 0 || err == nil || r.read != 64<<10 {
		t.Fatalf("canceled read %d %v %d", n, err, r.read)
	}
	r = &countedExtent{ctx: t.Context(), source: bytes.NewReader(data), remaining: 70000}
	if n, err := io.Copy(io.Discard, r); err != nil || n != 70000 || r.read != 70000 || r.remaining != 0 {
		t.Fatalf("extent copy %d %v %+v", n, err, r)
	}
}

func TestSearchBoundariesAndRows(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "000.jsonl"), pi3("zero", searchBody("needle")))
	for i := 1; i <= MaxSearchRows; i++ {
		writeFile(t, filepath.Join(dir, fmt.Sprintf("%03d.jsonl", i)), pi3(fmt.Sprint(i)))
	}
	s := svc(cfgSource("pi", "pi", dir))
	first := mustSearch(t, s, SearchRequest{Query: "nomatch"})
	if first.EOF || first.NextCursor == nil || !reflect.DeepEqual(diagCodes(first.Diagnostics), []string{DiagScanLimit}) || len(first.Hits) != 0 {
		t.Fatalf("row stop %+v", first)
	}
	second := mustSearch(t, s, SearchRequest{Query: "nomatch", Cursor: *first.NextCursor})
	if !second.EOF || second.Partial {
		t.Fatalf("row resume %+v", second)
	}
	// Exact 64 MiB is attempted; one byte over is rejected before read.
	boundary := filepath.Join(dir, "zz-boundary.jsonl")
	big := filepath.Join(dir, "zz-oversized.jsonl")
	f, err := os.Create(boundary)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Truncate(MaxSearchFileBytes); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	f, err = os.Create(big)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Truncate(MaxSearchFileBytes + 1); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	resp := mustSearch(t, svc(cfgSource("pi", "pi", dir)), SearchRequest{Query: "missing", Cursor: ""})
	if resp.NextCursor == nil {
		t.Fatal("expected row continuation")
	}
	for !resp.EOF {
		resp = mustSearch(t, svc(cfgSource("pi", "pi", dir)), SearchRequest{Query: "missing", Cursor: *resp.NextCursor})
	}
	// Both entries are reached across scan pages; the exact-size file is
	// diagnosed by its oversized line rather than the per-file size cap.
	if !reflect.DeepEqual(diagCodes(resp.Diagnostics), []string{DiagOversizedLine, DiagSessionTooLarge}) {
		t.Fatalf("size boundary %v", diagCodes(resp.Diagnostics))
	}
}

func TestSearchDiagnosticRetention(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.jsonl"), pi3("x")+strings.Repeat("[invalid]\n", MaxDiagnostics+31)+searchBody("needle")+"\n")
	resp := mustSearch(t, svc(cfgSource("pi", "pi", dir)), SearchRequest{Query: "needle"})
	if len(resp.Hits) != 1 || len(resp.Diagnostics) != MaxDiagnostics || resp.OmittedDiagnostics != 31 || !resp.Partial {
		t.Fatalf("diagnostic count hits=%d kept=%d omitted=%d", len(resp.Hits), len(resp.Diagnostics), resp.OmittedDiagnostics)
	}
	for _, d := range resp.Diagnostics {
		if strings.Contains(d.Message, "needle") || strings.Contains(d.Message, "[invalid]") {
			t.Fatal("diagnostic leaked native input")
		}
	}
}

func TestSearchHugeNativeMetadataSkipsAndAdvances(t *testing.T) {
	// The 4 MiB header timestamp is repeated in Session and in the hit's
	// native timestamp; together they exceed one response's allowance.
	dir := t.TempDir()
	huge := strings.Repeat("t", MaxLineBytes-150)
	content := `{"type":"session","version":3,"id":"x","timestamp":"` + huge + `"}` + "\n" +
		`{"type":"message","timestamp":"` + huge + `","message":{"role":"user","content":"needle"}}` + "\n" + searchBody("needle small") + "\n"
	writeFile(t, filepath.Join(dir, "a.jsonl"), content)
	s := svc(cfgSource("pi", "pi", dir))
	r := mustSearch(t, s, SearchRequest{Query: "needle", Limit: 1})
	if len(r.Hits) != 0 || len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != DiagRecordTooLarge || r.NextCursor == nil {
		t.Fatalf("huge first hits=%d diags=%v cursor=%v", len(r.Hits), diagCodes(r.Diagnostics), r.NextCursor)
	}
	if r.Diagnostics[0].Line == nil || *r.Diagnostics[0].Line != 2 {
		t.Fatalf("huge line %+v", r.Diagnostics[0])
	}
	next := mustSearch(t, s, SearchRequest{Query: "needle", Limit: 1, Cursor: *r.NextCursor})
	if len(next.Hits) != 1 || next.Hits[0].Line != 3 || !next.EOF || len(next.Diagnostics) != 0 {
		t.Fatalf("huge resume hits=%v diags=%v eof=%v", next.Hits, diagCodes(next.Diagnostics), next.EOF)
	}
}

func TestSearchSerializedEscapingAndSourceMetadata(t *testing.T) {
	dir := t.TempDir()
	body := searchBody(strings.Repeat("\\\"\n", 1000) + "needle")
	for i := range 12 {
		writeFile(t, filepath.Join(dir, fmt.Sprintf("%02d.jsonl", i)), pi3(fmt.Sprint(i), body))
	}
	s := svc(cfgSource("pi", "pi", dir))
	cursor := ""
	for pages := 0; pages < 20; pages++ {
		r := mustSearch(t, s, SearchRequest{Query: "needle", Limit: 2, Cursor: cursor})
		b, err := Marshal(r)
		if err != nil || len(b) > MaxResponseBytes {
			t.Fatalf("serialized bytes=%d error=%v", len(b), err)
		}
		if len(r.Hits) != 2 || r.Hits[0].MatchEnd-r.Hits[0].MatchStart != 6 {
			t.Fatalf("escaped hit %+v", r)
		}
		if r.EOF {
			break
		}
		cursor = *r.NextCursor
	}
}

func TestSearchBytePageDefersMatchingHit(t *testing.T) {
	dir := t.TempDir()
	stamp := strings.Repeat("t", 3<<20)
	large := `{"type":"message","timestamp":"` + stamp + `","message":{"role":"user","content":"needle"}}`
	writeFile(t, filepath.Join(dir, "a.jsonl"), pi3("x", large, large, large))
	s := svc(cfgSource("pi", "pi", dir))
	cursor := ""
	var lines []int64
	for pages := 0; pages < 5; pages++ {
		r := mustSearch(t, s, SearchRequest{Query: "needle", Limit: 10, Cursor: cursor})
		b, err := Marshal(r)
		if err != nil || len(b) > MaxResponseBytes {
			t.Fatalf("oversized response %d %v", len(b), err)
		}
		for _, h := range r.Hits {
			lines = append(lines, h.Line)
		}
		if r.EOF {
			break
		}
		if r.NextCursor == nil {
			t.Fatal("missing byte continuation")
		}
		cursor = *r.NextCursor
	}
	if !reflect.DeepEqual(lines, []int64{2, 3, 4}) {
		t.Fatalf("byte pages lines %v", lines)
	}
}

func TestSearchEscapedFilesystemDiagnosticsStayWithinResponseLimit(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "root")
	// Distinct symlink names carry hundreds of escaped control bytes each;
	// a raw-path diagnostic reserve would undercount their serialized JSON.
	prefix := filepath.Join(root, strings.Repeat("\x01", 160), strings.Repeat("\x02", 160), strings.Repeat("\x03", 160))
	if err := os.MkdirAll(prefix, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := range MaxDiagnostics {
		name := fmt.Sprintf("%03d", i) + strings.Repeat("\x05", 160)
		if err := os.Symlink(filepath.Join(base, "outside"), filepath.Join(prefix, name)); err != nil {
			t.Fatal(err)
		}
	}
	stamp := strings.Repeat("t", MaxLineBytes-180)
	large := `{"type":"message","timestamp":"` + stamp + `","message":{"role":"user","content":"needle"}}`
	writeFile(t, filepath.Join(root, "z.jsonl"), `{"type":"session","version":3,"id":"x","timestamp":"`+stamp+`"}`+"\n"+large+"\n"+searchBody("needle small")+"\n")
	s := svc(cfgSource("pi", "pi", root))
	first := mustSearch(t, s, SearchRequest{Query: "needle", Limit: 1})
	body, err := Marshal(first)
	if err != nil || len(body) > MaxResponseBytes || len(first.Diagnostics) != MaxDiagnostics || first.OmittedDiagnostics < 1 || first.NextCursor == nil {
		t.Fatalf("first bytes=%d err=%v diags=%d omitted=%d cursor=%v", len(body), err, len(first.Diagnostics), first.OmittedDiagnostics, first.NextCursor)
	}
	second := mustSearch(t, s, SearchRequest{Query: "needle", Limit: 1, Cursor: *first.NextCursor})
	body, err = Marshal(second)
	if err != nil || len(body) > MaxResponseBytes || len(second.Hits) != 1 || second.Hits[0].Line != 3 || !second.EOF {
		t.Fatalf("second bytes=%d err=%v hits=%d eof=%v", len(body), err, len(second.Hits), second.EOF)
	}
}

func TestSearchFinalMetadataDeferralDoesNotRepeatDiagnostics(t *testing.T) {
	dir := t.TempDir()
	timestamp := strings.Repeat("t", 2<<20)
	body := func() string {
		return `{"type":"message","timestamp":"` + timestamp + `","message":{"role":"user","content":"needle"}}`
	}
	writeFile(t, filepath.Join(dir, "a.jsonl"), `{"type":"session","version":3,"id":"x","timestamp":"`+timestamp+`"}`+"\n"+body()+"\n"+body()+"\n"+"not-json\n"+body()+"\n")
	s := svc(cfgSource("pi", "pi", dir))
	cursor := ""
	for page := 0; page < 3; page++ {
		r := mustSearch(t, s, SearchRequest{Query: "needle", Limit: 3, Cursor: cursor})
		if len(r.Hits) != 1 || r.Hits[0].Line != int64(page+2)+boolInt(page == 2) {
			t.Fatalf("page %d hits=%v", page, r.Hits)
		}
		if page == 0 && (r.Partial || len(r.Diagnostics) != 0) {
			t.Fatalf("first prematurely delivered diagnostics %+v", r)
		}
		if page == 1 && (!r.Partial || !reflect.DeepEqual(diagCodes(r.Diagnostics), []string{DiagMalformedLine})) {
			t.Fatalf("second missing line diagnostic %+v", r)
		}
		if page == 2 && (r.Partial || !r.EOF || len(r.Diagnostics) != 0) {
			t.Fatalf("third repeated diagnostic %+v", r)
		}
		if r.NextCursor != nil {
			cursor = *r.NextCursor
		}
	}
}

func boolInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

func TestSearchCanonicalCursorAndStaleLine(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.jsonl"), pi3("a", searchBody("needle"), searchBody("needle")))
	s := svc(cfgSource("pi", "pi", dir))
	first := mustSearch(t, s, SearchRequest{Query: "needle", Limit: 1})
	if first.NextCursor == nil {
		t.Fatal("missing continuation")
	}
	cursor := *first.NextCursor
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(cursor, "q1."))
	if err != nil {
		t.Fatal(err)
	}
	var parts []any
	if err := json.Unmarshal(payload, &parts); err != nil {
		t.Fatal(err)
	}
	bad := []string{
		"q1." + base64.RawURLEncoding.EncodeToString(append(payload, ' ')),
		"q1." + base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`["%s",0,9999]`, parts[0]))),
		"q1." + base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`["%s",9999,0]`, parts[0]))),
		"q1." + base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`["%s",-1,0]`, parts[0]))),
	}
	for _, c := range bad {
		if _, e := s.Search(t.Context(), SearchRequest{Query: "needle", Limit: 1, Cursor: c}); e == nil || e.Code != CodeInvalidRequest {
			t.Errorf("cursor %q err %v", c, e)
		}
	}
	for _, req := range []SearchRequest{{Query: "needle", Limit: 1, CaseSensitive: true, Cursor: cursor}, {Query: "needle", Harness: "pi", Limit: 1, Cursor: cursor}} {
		if _, e := s.Search(t.Context(), req); e == nil || e.Code != CodeInvalidRequest {
			t.Fatalf("request mismatch %+v: %v", req, e)
		}
	}
	other := svc(cfgSource("other", "pi", dir))
	if _, e := other.Search(t.Context(), SearchRequest{Query: "needle", Limit: 1, Cursor: cursor}); e == nil || e.Code != CodeInvalidRequest {
		t.Fatalf("source mismatch %v", e)
	}
	// Changing the candidate catalog makes a row outside it stale.
	if err := os.Remove(filepath.Join(dir, "a.jsonl")); err != nil {
		t.Fatal(err)
	}
	if _, e := s.Search(t.Context(), SearchRequest{Query: "needle", Limit: 1, Cursor: cursor}); e == nil || e.Code != CodeInvalidRequest {
		t.Fatalf("stale catalog %v", e)
	}
}
