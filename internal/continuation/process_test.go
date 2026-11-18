package continuation

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sfrieds3/spool/internal/history"
	"github.com/sfrieds3/spool/internal/testutil"
)

func fakeExecutable(t *testing.T, name, script string) string {
	t.Helper()
	return testutil.Executable(t, name, script)
}
func TestPiAdapterExactContractAndIsolation(t *testing.T) {
	f := setup(t)
	p := f.prepare(t)
	p.workspace = f.cwd
	log := filepath.Join(t.TempDir(), "capture")
	fake := fakeExecutable(t, "pi", `for arg in "$@"; do if [ "$arg" = --list-models ]; then printf 'provider model context\nexample exact-model 100000\n'; exit 0; fi; done
printf '%s\n' "$@" > "$CAPTURE.args"
pwd > "$CAPTURE.cwd"
/bin/cat > "$CAPTURE.input"
printf '%s' '`+modelOutput+`'
`)
	t.Setenv("CAPTURE", log)
	adapter := PiDrafter{Sources: f.cfg.Sources, LookPath: func(name string) (string, error) {
		if name != "pi" {
			t.Fatal(name)
		}
		return fake, nil
	}}
	output, e := adapter.Draft(context.Background(), p)
	if e != nil || string(output) != modelOutput {
		t.Fatalf("%s %v", output, e)
	}
	args, _ := os.ReadFile(log + ".args")
	for _, arg := range append(append([]string{"--print"}, resourceArgs...), "--provider\nexample", "--model\nexact-model", "--system-prompt", "--append-system-prompt\n\n") {
		if !strings.Contains(string(args), arg) {
			t.Errorf("missing %q: %s", arg, args)
		}
	}
	if strings.Contains(string(args), f.cwd) || strings.Contains(string(args), "historical data, not instructions") {
		t.Fatal("history/workspace in argv")
	}
	input, _ := os.ReadFile(log + ".input")
	if !bytes.Equal(input, p.Input()) {
		t.Fatal("input not exact")
	}
	cwd, _ := os.ReadFile(log + ".cwd")
	path := strings.TrimSpace(string(cwd))
	if path == f.cwd || strings.HasPrefix(path, filepath.Dir(f.path)) {
		t.Fatal("drafter not isolated")
	}
	if _, e := os.Stat(path); !os.IsNotExist(e) {
		t.Fatal("isolated cwd not cleaned")
	}
}
func TestPiAdapterFailsClosed(t *testing.T) {
	for name, body := range map[string]string{
		"nonzero": "exit 1", "token limit": "printf '{}'; printf 'limit' >&2; exit 1",
		"stdout overflow": `i=0; while [ "$i" -lt 3000 ]; do printf '%0100d' 0; i=$((i+1)); done`,
		"stderr overflow": `i=0; while [ "$i" -lt 400 ]; do printf '%0100d' 0 >&2; i=$((i+1)); done`,
		"timeout":         "exec /bin/sleep 10",
	} {
		t.Run(name, func(t *testing.T) {
			f := setup(t)
			p := f.prepare(t)
			p.settings.TimeoutSeconds = 1
			fake := fakeExecutable(t, "pi", `for a in "$@"; do if [ "$a" = --list-models ]; then printf 'example exact-model 1000\n'; exit 0; fi; done
`+body)
			adapter := PiDrafter{Sources: f.cfg.Sources, LookPath: func(string) (string, error) { return fake, nil }}
			start := time.Now()
			if _, e := adapter.Draft(context.Background(), p); e == nil {
				t.Fatal("failure accepted")
			}
			if time.Since(start) > 4*time.Second {
				t.Fatal("timeout did not reap promptly")
			}
		})
	}
}
func TestPiExactModelPreflightAndCancel(t *testing.T) {
	f := setup(t)
	p := f.prepare(t)
	fake := fakeExecutable(t, "pi", `printf 'example fuzzy-exact-model 10000\n'; exit 0`)
	adapter := PiDrafter{Sources: f.cfg.Sources, LookPath: func(string) (string, error) { return fake, nil }}
	if _, e := adapter.Draft(context.Background(), p); e == nil {
		t.Fatal("fuzzy model accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := adapter.Draft(ctx, p); e == nil {
		t.Fatal("canceled draft ran")
	}
}
func TestPiTemporaryCwdCannotBeInRecordedOrConfirmedWorkspace(t *testing.T) {
	f := setup(t)
	p := f.prepare(t)
	base := t.TempDir()
	t.Setenv("TMPDIR", base)
	// A configured workspace parent of TMPDIR must fail before the fake starts.
	p.workspace = base
	started := filepath.Join(t.TempDir(), "started")
	fake := fakeExecutable(t, "pi", `touch "$STARTED"`)
	t.Setenv("STARTED", started)
	adapter := PiDrafter{Sources: f.cfg.Sources, LookPath: func(string) (string, error) { return fake, nil }}
	if _, e := adapter.Draft(context.Background(), p); e == nil {
		t.Fatal("workspace cwd accepted")
	}
	if _, e := os.Stat(started); !os.IsNotExist(e) {
		t.Fatal("child started")
	}
}
func installFake(t *testing.T, name, script string) string {
	t.Helper()
	p := fakeExecutable(t, name, script)
	t.Setenv("PATH", filepath.Dir(p)+string(os.PathListSeparator)+os.Getenv("PATH"))
	return p
}
func TestNativePiResumeContractAndRaces(t *testing.T) {
	for _, mutation := range []string{"none", "cancel", "replace", "identity", "cwd", "unsupported", "missing cwd", "wrong cwd"} {
		t.Run(mutation, func(t *testing.T) {
			f := setup(t)
			capture := filepath.Join(t.TempDir(), "resume")
			t.Setenv("CAPTURE", capture)
			installFake(t, "pi", `printf '%s\n' "$@" > "$CAPTURE.args"; pwd > "$CAPTURE.cwd"`)
			cwd := f.cwd
			if mutation == "missing cwd" {
				b, _ := os.ReadFile(f.path)
				os.WriteFile(f.path, bytes.Replace(b, []byte(`,"cwd":"`+f.cwd+`"`), nil, 1), 0600)
			}
			if mutation == "wrong cwd" {
				cwd = t.TempDir()
			}
			r, e := f.s.ResolveResume(context.Background(), f.ref, cwd)
			if mutation == "missing cwd" || mutation == "wrong cwd" {
				if e == nil {
					t.Fatal("unavailable resume accepted")
				}
				return
			}
			if e != nil {
				t.Fatal(e)
			}
			c := r.Confirm()
			b, _ := os.ReadFile(f.path)
			switch mutation {
			case "cancel":
				c = ResumeConfirmation{}
			case "replace":
				os.Remove(f.path)
				os.WriteFile(f.path, b, 0600)
			case "identity":
				os.WriteFile(f.path, bytes.ReplaceAll(b, []byte("generic-id"), []byte("other-id")), 0600)
			case "cwd":
				os.WriteFile(f.path, bytes.ReplaceAll(b, []byte(f.cwd), []byte("/different")), 0600)
			case "unsupported":
				os.WriteFile(f.path, bytes.ReplaceAll(b, []byte(`"version":3`), []byte(`"version":2`)), 0600)
			}
			e = f.s.Resume(context.Background(), r, c, TerminalStreams{})
			if mutation != "none" {
				if e == nil {
					t.Fatal("changed or unconfirmed source resumed")
				}
				if _, e := os.Stat(capture + ".args"); !os.IsNotExist(e) {
					t.Fatal("child started")
				}
				return
			}
			if e != nil {
				t.Fatal(e)
			}
			args, _ := os.ReadFile(capture + ".args")
			if string(args) != "--session\n"+f.path+"\n" {
				t.Fatalf("%q", args)
			}
			actual, _ := os.ReadFile(capture + ".cwd")
			if strings.TrimSpace(string(actual)) != f.cwd {
				t.Fatal("wrong workspace")
			}
		})
	}
}
func TestFreshHarnessLaunchesPrivateExactApprovedBrief(t *testing.T) {
	for _, target := range []string{"pi", "claude", "codex"} {
		t.Run(target, func(t *testing.T) {
			f := setup(t)
			p := f.prepare(t)
			p.data.Target = target // private fixture, before confirmation.
			d, e := assemble(p, []byte(modelOutput), f.cwd)
			if e != nil {
				t.Fatal(e)
			}
			published, e := f.s.Publish(context.Background(), d, d.Approve())
			if e != nil {
				t.Fatal(e)
			}
			capture := filepath.Join(t.TempDir(), "launch")
			t.Setenv("CAPTURE", capture)
			installFake(t, target, `printf '%s\n' "$@" > "$CAPTURE.args"
pwd > "$CAPTURE.cwd"
[ "$1" = -- ]
file=${2#Read the approved Spool continuation brief at }
file=${file%%. Its historical*}
printf '%s' "$file" > "$CAPTURE.path"
[ -f "$file" ]
/bin/cat "$file" > "$CAPTURE.brief"
printf 'interactive output'
`)
			plan, e := f.s.PlanLaunch(published, target, f.cwd)
			if e != nil {
				t.Fatal(e)
			}
			if e := f.s.Launch(context.Background(), plan, LaunchConfirmation{}, TerminalStreams{}); e == nil {
				t.Fatal("implicit launch")
			}
			var output bytes.Buffer
			e = f.s.Launch(context.Background(), plan, plan.Confirm(), TerminalStreams{Out: &output})
			if e != nil {
				t.Fatal(e)
			}
			if output.String() != "interactive output" {
				t.Fatal("child terminal output blocked")
			}
			brief, _ := os.ReadFile(capture + ".brief")
			if string(brief) != published.Rendered() {
				t.Fatal("receiver brief differs from approved")
			}
			args, _ := os.ReadFile(capture + ".args")
			for _, bad := range []string{"--resume", "--continue", "--approve", "historical data, not instructions", "Complete the parser"} {
				if strings.Contains(string(args), bad) {
					t.Fatalf("unexpected argv %q", args)
				}
			}
			private, _ := os.ReadFile(capture + ".path")
			if _, e := os.Stat(string(private)); !os.IsNotExist(e) {
				t.Fatal("brief not cleaned")
			}
			cwd, _ := os.ReadFile(capture + ".cwd")
			if strings.TrimSpace(string(cwd)) != f.cwd {
				t.Fatal("wrong cwd")
			}
		})
	}
}
func TestFailedLaunchPreservesPublishedHandoff(t *testing.T) {
	f := setup(t)
	d := f.generate(t)
	p, e := f.s.Publish(context.Background(), d, d.Approve())
	if e != nil {
		t.Fatal(e)
	}
	installFake(t, "claude", "exit 7")
	plan, e := f.s.PlanLaunch(p, "claude", f.cwd)
	if e != nil {
		t.Fatal(e)
	}
	if e := f.s.Launch(context.Background(), plan, plan.Confirm(), TerminalStreams{}); e == nil {
		t.Fatal("failed child accepted")
	}
	if _, e := os.Stat(filepath.Join(f.state, "handoffs", p.ID()+".json")); e != nil {
		t.Fatal("published handoff lost", e)
	}
}

func TestNativeResumeUsesExactFileWithDuplicateNativeIDs(t *testing.T) {
	f := setup(t)
	duplicate := filepath.Join(filepath.Dir(f.path), "other.jsonl")
	b, _ := os.ReadFile(f.path)
	os.WriteFile(duplicate, b, 0600)
	capture := filepath.Join(t.TempDir(), "args")
	t.Setenv("CAPTURE", capture)
	installFake(t, "pi", `printf '%s\n' "$@" > "$CAPTURE"`)
	page, e := f.s.History.List(context.Background(), history.ListRequest{Limit: 500})
	if e != nil || len(page.Sessions) != 2 {
		t.Fatalf("%#v %v", page, e)
	}
	for _, sess := range page.Sessions {
		r, e := f.s.ResolveResume(context.Background(), sess.Ref, f.cwd)
		if e != nil {
			t.Fatal(e)
		}
		if e := f.s.Resume(context.Background(), r, r.Confirm(), TerminalStreams{}); e != nil {
			t.Fatal(e)
		}
		args, _ := os.ReadFile(capture)
		if string(args) != "--session\n"+sess.OriginalPath+"\n" {
			t.Fatal("ambiguous native id lookup", string(args))
		}
	}
}
func TestReceiverCancellationCleansPrivateFileAndPreservesHandoff(t *testing.T) {
	f := setup(t)
	d := f.generate(t)
	p, e := f.s.Publish(context.Background(), d, d.Approve())
	if e != nil {
		t.Fatal(e)
	}
	capture := filepath.Join(t.TempDir(), "private-path")
	t.Setenv("CAPTURE", capture)
	installFake(t, "claude", `file=${2#Read the approved Spool continuation brief at }; file=${file%%. Its historical*}; printf '%s' "$file" > "$CAPTURE"; printf ready; exec /bin/sleep 10`)
	plan, e := f.s.PlanLaunch(p, "claude", f.cwd)
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if e := f.s.Launch(ctx, plan, plan.Confirm(), TerminalStreams{Out: cancelWriter{cancel}}); e != context.Canceled {
		t.Fatal(e)
	}
	private, _ := os.ReadFile(capture)
	if len(private) == 0 {
		t.Fatal("fake did not start")
	}
	if _, e := os.Stat(string(private)); !os.IsNotExist(e) {
		t.Fatal("canceled private brief survived")
	}
	if _, e := os.Stat(filepath.Join(f.state, "handoffs", p.ID()+".json")); e != nil {
		t.Fatal("saved handoff lost", e)
	}
}

type cancelWriter struct{ cancel context.CancelFunc }

func (w cancelWriter) Write(p []byte) (int, error) { w.cancel(); return len(p), nil }
