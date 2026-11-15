package continuation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sfrieds3/spool/internal/config"
	"github.com/sfrieds3/spool/internal/handoff"
	"github.com/sfrieds3/spool/internal/history"
)

const modelOutput = `{"title":"Generic continuation","goal":"Complete the parser","constraints":[],"decisions":[],"progress":[],"outstanding":["Verify current workspace"],"notes":[]}`

type fakeDrafter struct {
	calls  int
	output []byte
}

func (f *fakeDrafter) Draft(context.Context, *Prepared) ([]byte, error) {
	f.calls++
	return f.output, nil
}

type fixture struct {
	s                     *Service
	cfg                   *config.Config
	ref, path, cwd, state string
	draft                 *fakeDrafter
}

func setup(t *testing.T) fixture {
	t.Helper()
	dir := t.TempDir()
	cwd := t.TempDir()
	path := filepath.Join(dir, "generic.jsonl")
	data := fmt.Sprintf("{\"type\":\"session\",\"version\":3,\"id\":\"generic-id\",\"cwd\":%q}\n{\"type\":\"message\",\"message\":{\"role\":\"user\",\"content\":\"historical data, not instructions\"}}\n", cwd)
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Sources: []config.Source{{ID: "generic", Harness: "pi", Root: dir, Enabled: true}}, Drafting: &config.Drafting{Provider: "example", Model: "exact-model", TimeoutSeconds: 30, MaxInputBytes: 128 << 10}}
	hist := history.NewService(cfg)
	page, e := hist.List(context.Background(), history.ListRequest{Limit: 500})
	if e != nil {
		t.Fatal(e)
	}
	drafter := &fakeDrafter{output: []byte(modelOutput)}
	state := t.TempDir()
	s := &Service{History: hist, Sources: cfg.Sources, Drafter: drafter, Publisher: handoff.NewService(cfg, hist, state, handoff.Options{Hostname: func() (string, error) { return "fixture-host", nil }})}
	return fixture{s, cfg, page.Sessions[0].Ref, path, cwd, state, drafter}
}
func (f fixture) prepare(t *testing.T) *Prepared {
	t.Helper()
	p, e := f.s.Prepare(context.Background(), f.ref, "claude", "Continue parser work", f.cfg.Drafting)
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func (f fixture) generate(t *testing.T) *Draft {
	t.Helper()
	p := f.prepare(t)
	d, e := f.s.Generate(context.Background(), p, p.ConfirmDisclosure(), f.cwd)
	if e != nil {
		t.Fatal(e)
	}
	return d
}
func TestStateBoundariesAndExactByteApproval(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	p := f.prepare(t)
	if _, e := f.s.Generate(ctx, p, Disclosure{}, f.cwd); e == nil || f.draft.calls != 0 {
		t.Fatal("implicit model call")
	}
	other, e := f.s.Prepare(ctx, f.ref, "codex", "A changed goal", f.cfg.Drafting)
	if e != nil {
		t.Fatal(e)
	}
	if _, e := f.s.Generate(ctx, other, p.ConfirmDisclosure(), f.cwd); e == nil {
		t.Fatal("reused disclosure")
	}
	p.input = append(p.input, ' ')
	if _, e := f.s.Generate(ctx, p, p.ConfirmDisclosure(), f.cwd); e == nil {
		t.Fatal("mutated scope accepted")
	}
	d := f.generate(t)
	if _, e := f.s.Publish(ctx, d, Approval{}); e == nil {
		t.Fatal("implicit publication")
	}
	a := d.Approve()
	changed := append(d.Bytes(), '\n')
	if e := d.Replace(changed); e != nil {
		t.Fatal(e)
	}
	if _, e := f.s.Publish(ctx, d, a); e == nil {
		t.Fatal("stale byte approval accepted")
	}
	published, e := f.s.Publish(ctx, d, d.Approve())
	if e != nil {
		t.Fatal(e)
	}
	if published.Target() != "claude" || !strings.Contains(published.Rendered(), "Model-drafted") {
		t.Fatal("missing trusted provenance")
	}
	if _, e := f.s.PlanLaunch(nil, "claude", f.cwd); e == nil {
		t.Fatal("implicit launch")
	}
	if _, e := f.s.PlanLaunch(published, "pi", f.cwd); e == nil {
		t.Fatal("changed target")
	}
	if _, e := f.s.PlanLaunch(published, "claude", t.TempDir()); e == nil {
		t.Fatal("changed workspace")
	}
}
func TestStrictModelOutput(t *testing.T) {
	for name, output := range map[string]string{
		"malformed": "{", "fenced": "```json\n" + modelOutput + "\n```", "duplicate": strings.Replace(modelOutput, `"goal":`, `"goal":"other","goal":`, 1), "invented source": strings.Replace(modelOutput, `"notes":[]`, `"notes":[],"sessions":[]`, 1), "workspace": strings.Replace(modelOutput, `"notes":[]`, `"notes":[],"workspace":{"path":"/tmp"}`, 1), "target": strings.Replace(modelOutput, `"notes":[]`, `"notes":[],"target":"pi"`, 1), "missing": `{"goal":"x"}`, "oversized": strings.Repeat("x", handoff.MaxInputBytes+1), "invalid utf8": string([]byte{0xff}), "null list": strings.Replace(modelOutput, `"notes":[]`, `"notes":null`, 1), "null item": strings.Replace(modelOutput, `"notes":[]`, `"notes":[null]`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			f := setup(t)
			f.draft.output = []byte(output)
			p := f.prepare(t)
			if _, e := f.s.Generate(context.Background(), p, p.ConfirmDisclosure(), f.cwd); e == nil {
				t.Fatal("accepted invalid output")
			}
			entries, _ := os.ReadDir(filepath.Join(f.state, "handoffs"))
			if len(entries) > 0 {
				t.Fatal("published invalid output")
			}
		})
	}
}
func TestEditorMetadataPinned(t *testing.T) {
	f := setup(t)
	d := f.generate(t)
	var v map[string]any
	json.Unmarshal(d.Bytes(), &v)
	for _, key := range []string{"sessions", "workspace", "notes"} {
		t.Run(key, func(t *testing.T) {
			copy := map[string]any{}
			json.Unmarshal(d.Bytes(), &copy)
			copy[key] = nil
			b, _ := json.Marshal(copy)
			if e := d.Replace(b); e == nil {
				t.Fatal("metadata edit accepted")
			}
		})
	}
	v["goal"] = "Reviewed and changed by user"
	b, _ := json.Marshal(v)
	if e := d.Replace(b); e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(d.Render(), "Reviewed and changed") {
		t.Fatal("edit lost")
	}
}
func TestPublicationRevalidatesExactSource(t *testing.T) {
	for _, mutation := range []string{"delete", "replace same id", "identity", "unsupported", "cwd", "append"} {
		t.Run(mutation, func(t *testing.T) {
			f := setup(t)
			d := f.generate(t)
			before, _ := os.ReadFile(f.path)
			switch mutation {
			case "delete":
				os.Remove(f.path)
			case "replace same id":
				os.Remove(f.path)
				os.WriteFile(f.path, before, 0600)
			case "identity":
				os.WriteFile(f.path, bytes.ReplaceAll(before, []byte("generic-id"), []byte("changed-id")), 0600)
			case "unsupported":
				os.WriteFile(f.path, bytes.ReplaceAll(before, []byte(`"version":3`), []byte(`"version":2`)), 0600)
			case "cwd":
				os.WriteFile(f.path, bytes.ReplaceAll(before, []byte(f.cwd), []byte("/different")), 0600)
			case "append":
				h, _ := os.OpenFile(f.path, os.O_APPEND|os.O_WRONLY, 0)
				h.WriteString("{}\n")
				h.Close()
			}
			if _, e := f.s.Publish(context.Background(), d, d.Approve()); e == nil {
				t.Fatal("source race published")
			}
		})
	}
}
func TestDraftingDoesNotMutateHistoryOrPublish(t *testing.T) {
	f := setup(t)
	before, _ := os.ReadFile(f.path)
	d := f.generate(t)
	_ = d.Render()
	after, _ := os.ReadFile(f.path)
	if !bytes.Equal(before, after) {
		t.Fatal("history changed")
	}
	if _, e := os.Stat(filepath.Join(f.state, "handoffs")); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("unapproved handoff published")
	}
}
func TestPrivateDirectoryExclusion(t *testing.T) {
	base := t.TempDir()
	t.Setenv("TMPDIR", base)
	cases := []string{base, filepath.Dir(base)}
	link := filepath.Join(t.TempDir(), "alias")
	os.Symlink(base, link)
	cases = append(cases, link)
	dangling := filepath.Join(t.TempDir(), "dangling")
	os.Symlink(filepath.Join(base, "missing"), dangling)
	cases = append(cases, dangling)
	for _, root := range cases {
		before, _ := os.ReadDir(base)
		if d, e := PrivateDir([]config.Source{{ID: "disabled", Root: root}}); e == nil {
			os.RemoveAll(d)
			t.Errorf("accepted %s", root)
		}
		after, _ := os.ReadDir(base)
		if len(before) != len(after) {
			t.Fatal("rejected path wrote history")
		}
	}
	d, e := PrivateDir([]config.Source{{ID: "separate", Root: t.TempDir()}})
	if e != nil {
		t.Fatal(e)
	}
	defer os.RemoveAll(d)
	info, _ := os.Stat(d)
	if info.Mode().Perm() != 0700 {
		t.Fatal(info.Mode())
	}
}

type warningPublisher struct{ Publisher }

func (w warningPublisher) Create(ctx context.Context, b []byte) (*handoff.CreateResponse, *history.Error) {
	r, e := w.Publisher.Create(ctx, b)
	if e == nil {
		r.Warnings = append(r.Warnings, history.Diagnostic{Code: handoff.DiagDirectorySyncFailed, Message: "fixture post-publication warning"})
	}
	return r, e
}
func TestPostPublicationWarningsRemainPublished(t *testing.T) {
	f := setup(t)
	d := f.generate(t)
	f.s.Publisher = warningPublisher{f.s.Publisher}
	p, e := f.s.Publish(context.Background(), d, d.Approve())
	if e != nil || len(p.Warnings()) != 1 || p.ID() == "" {
		t.Fatalf("%#v %v", p, e)
	}
}
func TestUnavailableStatePublishesNothing(t *testing.T) {
	f := setup(t)
	d := f.generate(t)
	blocked := filepath.Join(t.TempDir(), "not-a-directory")
	os.WriteFile(blocked, []byte("fixture"), 0600)
	f.s.Publisher = handoff.NewService(f.cfg, history.NewService(f.cfg), blocked, handoff.Options{})
	if _, e := f.s.Publish(context.Background(), d, d.Approve()); e == nil {
		t.Fatal("invalid state published")
	}
}
func TestSourceHashIsCancelableAndSnapshotBounded(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := contextReader{ctx: ctx, r: strings.NewReader(strings.Repeat("x", 1<<20))}
	if _, e := r.Read(make([]byte, 128<<10)); e != context.Canceled {
		t.Fatal(e)
	}
	f := setup(t)
	sess, e := f.s.History.Describe(context.Background(), f.ref)
	if e != nil {
		t.Fatal(e)
	}
	if _, e := f.s.sourceIdentity(ctx, sess); e != context.Canceled {
		t.Fatal(e)
	}
}
