package history

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func readAll(t *testing.T, s *Service, ref string, limit int) (lines []int64, pages []*ReadResponse) {
	t.Helper()
	offset := int64(0)
	for i := 0; ; i++ {
		if i > 1000 {
			t.Fatal("pagination did not terminate")
		}
		p := mustRead(t, s, ref, offset, limit)
		pages = append(pages, p)
		for _, r := range p.Records {
			lines = append(lines, r.Line)
		}
		if p.EOF || p.NextOffset == nil {
			return lines, pages
		}
		if *p.NextOffset <= offset && len(p.Records) == 0 && len(p.Diagnostics) == 0 {
			t.Fatalf("no progress at offset %d", offset)
		}
		if *p.NextOffset == offset {
			return lines, pages // incomplete tail: caller must not loop
		}
		offset = *p.NextOffset
	}
}

func singleSession(t *testing.T, harness, rel, content string) (*Service, string, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, filepath.FromSlash(rel))
	writeFile(t, path, content)
	s := svc(cfgSource(harness, harness, dir))
	list := mustList(t, s, ListRequest{})
	if len(list.Sessions) != 1 {
		t.Fatalf("list = %+v", list)
	}
	return s, list.Sessions[0].Ref, path
}

func TestReadFixturesPreserveRecords(t *testing.T) {
	root := copyFixtures(t)
	s := svc(fixtureSources(root)...)
	list := mustList(t, s, ListRequest{})
	for _, sess := range list.Sessions {
		resp := mustRead(t, s, sess.Ref, 0, MaxLimit)
		if resp.Partial || !resp.EOF || resp.NextOffset != nil {
			t.Errorf("%s page = %+v", sess.Path, resp.Page)
		}
		raw, err := os.ReadFile(filepath.Join(root, sess.SourceID, filepath.FromSlash(sess.Path)))
		if err != nil {
			t.Fatal(err)
		}
		lines := bytes.Split(bytes.TrimSuffix(raw, []byte("\n")), []byte("\n"))
		if len(resp.Records) != len(lines) {
			t.Fatalf("%s: %d records for %d lines", sess.Path, len(resp.Records), len(lines))
		}
		for i, r := range resp.Records {
			if r.Line != int64(i+1) || !bytes.Equal(r.Native, lines[i]) {
				t.Errorf("%s line %d not preserved exactly", sess.Path, i+1)
			}
		}
	}
	// Relationship fields for a Claude record.
	ref := refFor(t, list, "c1a2d3e4-0000-4000-8000-000000000001.jsonl")
	resp := mustRead(t, s, ref, 0, MaxLimit)
	r := resp.Records[2]
	if *r.ID != "a0000000-0000-4000-8000-000000000002" || *r.ParentID != "a0000000-0000-4000-8000-000000000001" || *r.Type != "assistant" {
		t.Fatalf("claude record = %+v", r)
	}
	if !bytes.Contains(resp.Records[3].Native, []byte(`"extraResultField":true`)) {
		t.Fatal("unknown tool-result field dropped")
	}
	// Original cwd is preserved although it does not exist here.
	if *resp.Session.Cwd != "/home/example/project" {
		t.Fatal("cwd")
	}
}

func TestReadPagination(t *testing.T) {
	var body []string
	for i := range 23 {
		body = append(body, fmt.Sprintf(`{"type":"custom","id":"%d","parentId":null,"n":%d}`, i, i))
	}
	s, ref, _ := singleSession(t, "pi", "a.jsonl", pi3("p", body...))
	want := make([]int64, 24)
	for i := range want {
		want[i] = int64(i + 1)
	}
	for _, limit := range []int{1, 5, 24, MaxLimit} {
		lines, pages := readAll(t, s, ref, limit)
		if !reflect.DeepEqual(lines, want) {
			t.Fatalf("limit %d lines = %v", limit, lines)
		}
		lastPage := pages[len(pages)-1]
		if !lastPage.EOF || lastPage.NextOffset != nil || lastPage.Partial {
			t.Fatalf("limit %d last page = %+v", limit, lastPage.Page)
		}
		for _, p := range pages[:len(pages)-1] {
			if p.EOF || p.NextOffset == nil || len(p.Records) != limit {
				t.Fatalf("limit %d intermediate page = %+v", limit, p.Page)
			}
		}
	}
	// Exact-boundary: limit equal to remaining records ends with eof.
	p := mustRead(t, s, ref, 19, 5)
	if len(p.Records) != 5 || !p.EOF {
		t.Fatalf("boundary page = %+v", p.Page)
	}
	p = mustRead(t, s, ref, 24, 5)
	if len(p.Records) != 0 || !p.EOF || p.NextOffset != nil {
		t.Fatalf("offset at eof = %+v", p.Page)
	}
	p = mustRead(t, s, ref, 1000, 5)
	if len(p.Records) != 0 || !p.EOF {
		t.Fatalf("offset past eof = %+v", p.Page)
	}
	for _, bad := range []ReadRequest{{Ref: ref, Limit: 0}, {Ref: ref, Limit: MaxLimit + 1}, {Ref: ref, Offset: -1, Limit: 1}} {
		if _, err := s.Read(t.Context(), bad); err == nil || err.Code != CodeInvalidRequest {
			t.Errorf("%+v: %v", bad, err)
		}
	}
}

func TestReadResponseByteLimits(t *testing.T) {
	// Each record ~3 MiB: two fit in a response, the third is deferred.
	big := func(i int) string {
		return fmt.Sprintf(`{"type":"custom","id":"%d","parentId":null,"data":"%s"}`, i, strings.Repeat("x", 3<<20))
	}
	// A line over the 4 MiB line limit is omitted.
	huge := `{"type":"custom","id":"huge","data":"` + strings.Repeat("y", MaxLineBytes) + `"}`
	// A line under the line limit whose record cannot fit in a response by
	// itself: its native id is repeated in the normalized id field.
	escaped := `{"type":"custom","id":"` + strings.Repeat("i", MaxLineBytes-64) + `"}`
	s, ref, _ := singleSession(t, "pi", "a.jsonl", pi3("p", big(1), big(2), huge, big(3), escaped, big(4)))

	lines, pages := readAll(t, s, ref, MaxLimit)
	if !reflect.DeepEqual(lines, []int64{1, 2, 3, 5, 7}) {
		t.Fatalf("lines = %v", lines)
	}
	var omitted []string
	for _, p := range pages {
		out, _ := Marshal(p)
		if len(out) > MaxResponseBytes {
			t.Fatalf("response %d bytes", len(out))
		}
		for _, d := range p.Diagnostics {
			omitted = append(omitted, fmt.Sprintf("%s@%d", d.Code, *d.Line))
		}
	}
	if !reflect.DeepEqual(omitted, []string{DiagOversizedLine + "@4", DiagRecordTooLarge + "@6"}) {
		t.Fatalf("omissions = %v", omitted)
	}
	// Page 1: header and two 3 MiB records; line 4 is omitted; line 5 is
	// deferred to the next page because it would exceed the response limit.
	if len(pages) != 2 || len(pages[0].Records) != 3 || *pages[0].NextOffset != 4 {
		t.Fatalf("first page = %d records next %d", len(pages[0].Records), *pages[0].NextOffset)
	}
}

func TestReadDamagedFiles(t *testing.T) {
	ok := func(i int) string { return fmt.Sprintf(`{"type":"custom","id":"%d","parentId":null}`, i) }
	content := pi3("p", ok(1), `{"type":`, `[1,2]`, ``, "\xff\xfe", ok(2), `{"type":"custom","id":"huge","data":"`+strings.Repeat("z", MaxLineBytes+10)+`"}`, ok(3))
	s, ref, _ := singleSession(t, "pi", "a.jsonl", content)
	p := mustRead(t, s, ref, 0, MaxLimit)
	var lines []int64
	for _, r := range p.Records {
		lines = append(lines, r.Line)
	}
	if !reflect.DeepEqual(lines, []int64{1, 2, 7, 9}) {
		t.Fatalf("lines = %v", lines)
	}
	got := map[int64]string{}
	for _, d := range p.Diagnostics {
		got[*d.Line] = d.Code
		if *d.Path != "a.jsonl" {
			t.Errorf("diagnostic path %v", d.Path)
		}
	}
	want := map[int64]string{3: DiagMalformedLine, 4: DiagMalformedLine, 5: DiagMalformedLine, 6: DiagMalformedLine, 8: DiagOversizedLine}
	if !reflect.DeepEqual(got, want) || !p.Partial || !p.EOF {
		t.Fatalf("diagnostics = %v page = %+v", got, p.Page)
	}
	// Diagnostics for skipped lines are reported once even across pages.
	_, pages := readAll(t, s, ref, 1)
	count := 0
	for _, pg := range pages {
		count += len(pg.Diagnostics)
	}
	if count != 5 {
		t.Fatalf("diagnostics across pages = %d", count)
	}
}

func TestReadFatalIdentityErrors(t *testing.T) {
	tests := []struct {
		name, harness, rel, content, code string
	}{
		{"malformed pi header", "pi", "a.jsonl", "{\"type\":\"session\"\n" + `{"type":"custom"}` + "\n", CodeInvalidHistory},
		{"legacy pi", "pi", "a.jsonl", `{"type":"session","version":2,"id":"x"}` + "\n", CodeUnsupportedFormat},
		{"codex composite", "codex", "rollout-a.jsonl", `{"type":"session_meta","payload":{"id":"x","history_mode":"paginated"}}` + "\n", CodeUnsupportedFormat},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, filepath.Join(dir, tt.rel), tt.content)
			s := svc(cfgSource(tt.harness, tt.harness, dir))
			// Listing reports it as a candidate diagnostic, not a session.
			list := mustList(t, s, ListRequest{})
			if len(list.Sessions) != 0 || len(list.Diagnostics) != 1 || !list.Partial {
				t.Fatalf("list = %+v", list)
			}
			ref := Ref{SourceID: tt.harness, Harness: tt.harness, Path: tt.rel, NativeID: "x"}.Encode()
			if code := readErr(t, s, ref); code != tt.code {
				t.Fatalf("read code = %s, want %s", code, tt.code)
			}
		})
	}
	// Claude fork-context file read after discovery: unsupported, not empty.
	dir := t.TempDir()
	path := filepath.Join(dir, "p/s.jsonl")
	writeFile(t, path, `{"type":"user","sessionId":"cs","uuid":"u1","parentUuid":null,"cwd":"/home/example"}`+"\n")
	s := svc(cfgSource("claude", "claude", dir))
	ref := mustList(t, s, ListRequest{}).Sessions[0].Ref
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString(`{"type":"fork-context-ref","agentId":"a","parentSessionId":"other","parentLastUuid":"u0","contextLength":3}` + "\n")
	f.Close()
	if code := readErr(t, s, ref); code != CodeUnsupportedFormat {
		t.Fatalf("fork-context read = %s", code)
	}
}

func TestReadIncompleteTailAndAppend(t *testing.T) {
	ok := func(i int) string { return fmt.Sprintf(`{"type":"custom","id":"%d","parentId":null}`, i) }
	s, ref, path := singleSession(t, "pi", "a.jsonl", pi3("p", ok(1))+`{"type":"custom","id":"2"`)
	p := mustRead(t, s, ref, 0, MaxLimit)
	if len(p.Records) != 2 || p.EOF || p.NextOffset == nil || *p.NextOffset != 2 || !p.Partial {
		t.Fatalf("tail page = %+v", p.Page)
	}
	if p.Diagnostics[0].Code != DiagIncompleteTail || *p.Diagnostics[0].Line != 3 {
		t.Fatalf("diag = %+v", p.Diagnostics)
	}
	// Retrying at the offset without changes yields the same retry offset and
	// no records; callers must not loop on it.
	again := mustRead(t, s, ref, 2, MaxLimit)
	if len(again.Records) != 0 || again.EOF || *again.NextOffset != 2 {
		t.Fatalf("retry = %+v", again.Page)
	}
	// An offset beyond the incomplete line cannot skip it.
	beyond := mustRead(t, s, ref, 10, MaxLimit)
	if beyond.EOF || *beyond.NextOffset != 2 {
		t.Fatalf("beyond = %+v", beyond.Page)
	}
	// A huge unterminated tail is withheld without being buffered.
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString(`,"pad":"` + strings.Repeat("q", MaxLineBytes+1))
	f.Close()
	huge := mustRead(t, s, ref, 2, MaxLimit)
	if huge.EOF || *huge.NextOffset != 2 || huge.Diagnostics[0].Code != DiagIncompleteTail {
		t.Fatalf("huge tail = %+v", huge.Page)
	}
	// The writer finishes the record (now oversized), then appends another.
	writeFile(t, path, pi3("p", ok(1))+`{"type":"custom","id":"2","parentId":"1"}`+"\n"+ok(3)+"\n")
	done := mustRead(t, s, ref, 2, MaxLimit)
	if len(done.Records) != 2 || done.Records[0].Line != 3 || !done.EOF || done.Partial {
		t.Fatalf("completed = %+v", done.Page)
	}
}

func TestReadDiagnosticOverflow(t *testing.T) {
	var body []string
	for range MaxDiagnostics*2 + 5 {
		body = append(body, "garbage")
	}
	body = append(body, `{"type":"custom","id":"last","parentId":null}`)
	s, ref, _ := singleSession(t, "pi", "a.jsonl", pi3("p", body...))
	total := 0
	var records []int64
	offset := int64(0)
	for i := 0; ; i++ {
		if i > 10 {
			t.Fatal("no progress")
		}
		p := mustRead(t, s, ref, offset, MaxLimit)
		if len(p.Diagnostics) > MaxDiagnostics {
			t.Fatalf("%d diagnostics", len(p.Diagnostics))
		}
		total += len(p.Diagnostics) + p.OmittedDiagnostics
		for _, r := range p.Records {
			records = append(records, r.Line)
		}
		if p.EOF {
			break
		}
		offset = *p.NextOffset
	}
	if total != MaxDiagnostics*2+5 || !reflect.DeepEqual(records, []int64{1, int64(MaxDiagnostics*2 + 7)}) {
		t.Fatalf("total diags %d records %v", total, records)
	}
}

func TestReadDoesNotModifySources(t *testing.T) {
	root := copyFixtures(t)
	before := snapshot(t, root)
	s := svc(fixtureSources(root)...)
	for _, sess := range mustList(t, s, ListRequest{}).Sessions {
		readAll(t, s, sess.Ref, 2)
	}
	if after := snapshot(t, root); !reflect.DeepEqual(before, after) {
		t.Fatal("sources changed")
	}
}

// snapshot records every entry's path, mode, size, mtime, and content.
func snapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		v := fmt.Sprintf("%v %d %d", info.Mode(), info.Size(), info.ModTime().UnixNano())
		if info.Mode().IsRegular() {
			data, _ := os.ReadFile(p)
			v += " " + string(data)
		}
		out[p] = v
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestReadOversizedMetadataIsNullNotFatal(t *testing.T) {
	dir := t.TempDir()
	cwd := "/" + strings.Repeat("c", maxMetadataField)
	writeFile(t, filepath.Join(dir, "a.jsonl"), `{"type":"session","version":3,"id":"big-cwd","cwd":"`+cwd+`"}`+"\n")
	s := svc(cfgSource("pi", "pi", dir))
	ref := Ref{SourceID: "pi", Harness: "pi", Path: "a.jsonl", NativeID: "big-cwd"}.Encode()
	p := mustRead(t, s, ref, 0, 10)
	if p.Session.Cwd != nil || !p.Partial || p.Diagnostics[0].Code != DiagMetadataOversized || len(p.Records) != 1 {
		t.Fatalf("read = %+v", p)
	}
	if !strings.Contains(string(p.Records[0].Native), cwd) {
		t.Fatal("native header lost the cwd")
	}
}
