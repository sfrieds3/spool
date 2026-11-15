package continuation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sfrieds3/spool/internal/config"
	"github.com/sfrieds3/spool/internal/history"
)

type scriptedHistory struct {
	History
	pages     map[int64]*history.ListResponse
	failure   int64
	listCalls []int64
	read      func(history.ReadRequest) (*history.ReadResponse, *history.Error)
}

func (h *scriptedHistory) List(ctx context.Context, r history.ListRequest) (*history.ListResponse, *history.Error) {
	h.listCalls = append(h.listCalls, r.Offset)
	if r.Offset == h.failure {
		return nil, &history.Error{Code: history.CodeSourcesUnavailable, Message: "failed later"}
	}
	return h.pages[r.Offset], nil
}
func (h *scriptedHistory) Read(ctx context.Context, r history.ReadRequest) (*history.ReadResponse, *history.Error) {
	if h.read != nil {
		return h.read(r)
	}
	return h.History.Read(ctx, r)
}
func ptr[T any](v T) *T { return &v }
func TestCatalogSparsePagesAndFailures(t *testing.T) {
	f := setup(t)
	sess, _ := f.s.History.Describe(context.Background(), f.ref)
	d := history.Diagnostic{Code: "fixture", Message: "same"}
	h := &scriptedHistory{History: f.s.History, failure: -1, pages: map[int64]*history.ListResponse{
		0:   {Sessions: []history.Session{*sess}, Page: history.Page{NextOffset: ptr(int64(500)), Diagnostics: []history.Diagnostic{d}}},
		500: {Page: history.Page{NextOffset: ptr(int64(501)), Diagnostics: []history.Diagnostic{d}}},
		501: {Sessions: []history.Session{*sess}},
	}}
	diagnostics := 0
	rows, e := Catalog(context.Background(), h, "", func(history.Diagnostic) { diagnostics++ })
	if e != nil || len(rows) != 1 || diagnostics != 1 || fmt.Sprint(h.listCalls) != "[0 500 501]" {
		t.Fatalf("%v %v %v %v", rows, e, diagnostics, h.listCalls)
	}
	h.failure = 501
	if rows, e = Catalog(context.Background(), h, "", nil); e == nil || rows != nil {
		t.Fatal("later failure offered partial catalog")
	}
	h.failure = 0
	if _, e := Catalog(context.Background(), h, "", nil); e == nil {
		t.Fatal("all-source failure hidden")
	}
	h.failure = -1
	h.pages[500].NextOffset = ptr(int64(500))
	if _, e := Catalog(context.Background(), h, "", nil); e == nil {
		t.Fatal("nonadvancing offset accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := Catalog(ctx, h, "", nil); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
}
func TestCatalogMoreThan500GenericSessions(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 507; i++ {
		data := fmt.Sprintf("{\"type\":\"session\",\"version\":3,\"id\":\"generic-%03d\"}\n", i)
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("%03d.jsonl", i)), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	h := history.NewService(&config.Config{Sources: []config.Source{{ID: "many", Harness: "pi", Root: dir, Enabled: true}}})
	rows, e := Catalog(context.Background(), h, "pi", nil)
	if e != nil || len(rows) != 507 {
		t.Fatalf("%d %v", len(rows), e)
	}
	for i, s := range rows {
		if s.NativeID != fmt.Sprintf("generic-%03d", i) {
			t.Fatal("order changed")
		}
	}
}
func TestScopeExactCoverageAndBudget(t *testing.T) {
	f := setup(t)
	file, _ := os.OpenFile(f.path, os.O_APPEND|os.O_WRONLY, 0)
	file.WriteString("not JSON\n")
	file.WriteString(`{"type":"message","message":{"content":"` + strings.Repeat("x", 6000) + `"}}` + "\n")
	file.WriteString(`{"type":"message","message":{"content":"last complete"}}` + "\n")
	file.WriteString(`{"type":"message"`)
	file.Close()
	p := f.prepare(t)
	if p.data.EOF || !strings.Contains(p.coverage(), "[1-2,4-5]") || len(p.data.Diagnostics) != 2 {
		t.Fatalf("%s %#v", p.Summary(), p.data.Diagnostics)
	}
	if !json.Valid([]byte(strings.TrimPrefix(string(p.Input()), inputFraming))) {
		t.Fatal("split native JSON")
	}
	if len(p.Input()) > p.settings.MaxInputBytes {
		t.Fatal("over budget")
	}
	bounded := *f.cfg.Drafting
	bounded.MaxInputBytes = 2500
	p, e := f.s.Prepare(context.Background(), f.ref, "pi", "goal", &bounded)
	if e != nil {
		t.Fatal(e)
	}
	if len(p.Input()) > 2500 || len(p.data.Records) != 2 || p.data.EOF {
		t.Fatalf("budget/split: %s", p.Summary())
	}
	bounded.MaxInputBytes = 1
	if _, e := f.s.Prepare(context.Background(), f.ref, "pi", "goal", &bounded); e == nil {
		t.Fatal("impossible framing budget accepted")
	}
}
func TestScopeNeverRetriesNonadvancingIncompleteTail(t *testing.T) {
	f := setup(t)
	calls := 0
	h := &scriptedHistory{History: f.s.History, read: func(r history.ReadRequest) (*history.ReadResponse, *history.Error) {
		calls++
		return &history.ReadResponse{Page: history.Page{NextOffset: ptr(r.Offset), Diagnostics: []history.Diagnostic{{Code: history.DiagIncompleteTail}}}}, nil
	}}
	f.s.History = h
	p := f.prepare(t)
	if calls != 1 || p.data.EOF {
		t.Fatalf("calls=%d %s", calls, p.Summary())
	}
}
func TestScopeOversizedRecordAndDiagnostics(t *testing.T) {
	f := setup(t)
	file, _ := os.OpenFile(f.path, os.O_APPEND|os.O_WRONLY, 0)
	file.WriteString(strings.Repeat("x", history.MaxLineBytes+1) + "\n")
	file.WriteString("{}\n")
	file.Close()
	p := f.prepare(t)
	if !p.data.EOF || len(p.data.Diagnostics) != 1 || p.data.Diagnostics[0].Code != history.DiagOversizedLine || !strings.Contains(p.coverage(), "1-2,4") {
		t.Fatal(p.Summary())
	}
}
func TestHostileLabelsAreInertSingleRows(t *testing.T) {
	s := "a\t\n\r\x1b[31mb\u202ec\u2069d\u2028e\u0085f"
	got := CleanLabel(s)
	if got != "a[31mbcdef" {
		t.Fatalf("%q", got)
	}
}

func TestDenseDiagnosticsFitPinnedVersionOneProvenance(t *testing.T) {
	f := setup(t)
	file, e := os.OpenFile(f.path, os.O_APPEND|os.O_WRONLY, 0)
	if e != nil {
		t.Fatal(e)
	}
	file.WriteString(strings.Repeat("not JSON\n{}\n", 600))
	file.Close()
	p := f.prepare(t)
	if len(p.coverage()) <= 8192 {
		t.Fatal("fixture did not exercise multi-item scope provenance")
	}
	d, e := f.s.Generate(context.Background(), p, p.ConfirmDisclosure(), f.cwd)
	if e != nil {
		t.Fatal("paid drafting produced infeasible provenance", e)
	}
	if len(d.trustedNotes) < 3 {
		t.Fatal("scope provenance was not split")
	}
	for _, note := range d.trustedNotes {
		if len(note) > 8192 {
			t.Fatal("oversized trusted note")
		}
	}
	if e := d.Replace(d.Bytes()); e != nil {
		t.Fatal(e)
	}
	if _, e := f.s.Publish(context.Background(), d, d.Approve()); e != nil {
		t.Fatal(e)
	}
}
