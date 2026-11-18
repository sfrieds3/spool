package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sfrieds3/spool/internal/config"
	"github.com/sfrieds3/spool/internal/continuation"
	"github.com/sfrieds3/spool/internal/handoff"
	"github.com/sfrieds3/spool/internal/history"
	"github.com/sfrieds3/spool/internal/testutil"
)

func fakeCommand(t *testing.T, name, script string) string {
	t.Helper()
	p := testutil.Executable(t, name, script)
	t.Setenv("PATH", filepath.Dir(p)+string(os.PathListSeparator)+os.Getenv("PATH"))
	return p
}

const firstFzf = `IFS= read -r first
printf '%s\n' "$first"
`

func pickRun(t *testing.T, w workspace, input string, args ...string) result {
	t.Helper()
	var out, err bytes.Buffer
	a := app{stdin: strings.NewReader(input), stdout: &out, stderr: &err, env: w.env(), ctx: context.Background(), terminal: func(io.Reader, io.Writer) bool { return true }}
	code := a.run(append([]string{"pick", "--config", w.config}, args...))
	return result{code: code, stdout: out.String(), stderr: err.String()}
}
func TestPickHelpAndInvalidWithoutDependencies(t *testing.T) {
	w := newWorkspace(t)
	t.Setenv("PATH", t.TempDir())
	for _, test := range []struct {
		args []string
		code int
	}{{[]string{"--help"}, 0}, {[]string{"--harness", "unknown"}, 2}, {[]string{"extra"}, 2}, {[]string{"--no-such-option"}, 2}, {[]string{"--config", "/nonexistent/config"}, 2}} {
		r := pickRun(t, w, "", test.args...)
		if r.code != test.code || r.stdout != "" {
			t.Fatalf("%v: %d %q %q", test.args, r.code, r.stdout, r.stderr)
		}
	}
	if r := pickRun(t, w, "", "--ref"); r.code != 1 || !strings.Contains(r.stderr, "fzf") {
		t.Fatalf("missing fzf: %#v", r)
	}
}
func TestPickRequiresBothActualTerminalDescriptors(t *testing.T) {
	w := newWorkspace(t)
	null, e := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if e != nil {
		t.Fatal(e)
	}
	defer null.Close()
	if terminalStreams(null, null) {
		t.Fatal("/dev/null is not a tty")
	}
	var diagnostics bytes.Buffer
	for _, pair := range []struct {
		in  io.Reader
		out io.Writer
	}{{null, &bytes.Buffer{}}, {strings.NewReader(""), null}, {null, null}} {
		a := app{stdin: pair.in, stdout: pair.out, stderr: &diagnostics, env: w.env(), ctx: context.Background()}
		if code := a.run([]string{"pick", "--config", w.config}); code != 2 {
			t.Fatal(code)
		}
	}
}
func TestPickRefInspectAndCancelStayInert(t *testing.T) {
	w := newWorkspace(t)
	fakeCommand(t, "fzf", firstFzf)
	before := tree(t, w.dir)
	r := pickRun(t, w, "", "--ref", "--harness", "pi")
	if r.code != 0 || !strings.HasSuffix(r.stdout, "\n") || strings.Count(r.stdout, "\n") != 1 {
		t.Fatalf("%#v", r)
	}
	if _, e := history.DecodeRef(strings.TrimSpace(r.stdout)); e != nil {
		t.Fatal(e)
	}
	r = pickRun(t, w, "i\nc\n", "--harness", "pi")
	if r.code != 130 || r.stdout != "" || !strings.Contains(r.stderr, `"content":"untrusted_history"`) || strings.Count(r.stderr, "Actions:") != 2 {
		t.Fatalf("%#v", r)
	}
	if !equalMaps(before, tree(t, w.dir)) {
		t.Fatal("browsing mutated history")
	}
	if _, e := os.Stat(filepath.Join(w.state(), "spool", "handoffs")); !os.IsNotExist(e) {
		t.Fatal("browsing published")
	}
	r = pickRun(t, w, "r\nc\n", "--harness", "claude")
	if r.code != 130 || !strings.Contains(r.stderr, "handoff instead") {
		t.Fatalf("unsupported resume not recoverable: %#v", r)
	}
}
func TestPickFzfFramingAndEnvironment(t *testing.T) {
	w := newWorkspace(t)
	capture := filepath.Join(t.TempDir(), "capture")
	t.Setenv("CAPTURE", capture)
	t.Setenv("FZF_DEFAULT_OPTS", "--bind=start:execute(evil)")
	t.Setenv("FZF_DEFAULT_OPTS_FILE", "/evil")
	t.Setenv("FZF_DEFAULT_COMMAND", "evil")
	t.Setenv("SHELL", "/evil")
	fakeCommand(t, "fzf", `printf '%s\n' "$@" > "$CAPTURE.args"
printf '%s|%s|%s|%s' "$FZF_DEFAULT_OPTS" "$FZF_DEFAULT_OPTS_FILE" "$FZF_DEFAULT_COMMAND" "$SHELL" > "$CAPTURE.env"
`+firstFzf)
	r := pickRun(t, w, "", "--ref")
	if r.code != 0 {
		t.Fatalf("%#v", r)
	}
	args, _ := os.ReadFile(capture + ".args")
	for _, arg := range []string{"--no-multi", "--no-ansi", "--with-nth=2..", "--nth=2..", "--preview"} {
		if !strings.Contains(string(args), arg) {
			t.Error(arg)
		}
	}
	env, _ := os.ReadFile(capture + ".env")
	if string(env) != "|||/bin/sh" {
		t.Fatalf("untrusted fzf defaults: %q", env)
	}
}
func TestPickRejectsUntrustedSelectionsAndFzfFailure(t *testing.T) {
	for name, script := range map[string]string{
		"altered":   `IFS= read -r first; printf '%s changed\n' "$first"`,
		"multiple":  `IFS= read -r first; printf '%s\n%s\n' "$first" "$first"`,
		"forged":    `printf 's1.forged\tpi\t\t\t\n'`,
		"oversized": `i=0; while [ "$i" -lt 800 ]; do printf '%0100d' 0; i=$((i+1)); done; exec /bin/sleep 10`,
		"no match":  `exit 1`, "cancel": `exit 130`,
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorkspace(t)
			fakeCommand(t, "fzf", script)
			start := time.Now()
			r := pickRun(t, w, "", "--ref")
			want := 1
			if name == "cancel" {
				want = 130
			}
			if r.code != want || r.stdout != "" {
				t.Fatalf("%#v", r)
			}
			if time.Since(start) > 4*time.Second {
				t.Fatal("fzf overflow was not canceled")
			}
		})
	}
}
func TestFiveFieldRowsSanitizeWithoutChangingRefs(t *testing.T) {
	hostile := "same\tlabel\n\r\x1b[31m\u202e\u2069\u2028"
	ref := history.Ref{SourceID: "fixture", Harness: "pi", Path: "generic.jsonl", NativeID: "generic"}.Encode()
	row := sessionRow(history.Session{Ref: ref, Harness: "pi", Cwd: &hostile, Title: &hostile})
	parts := strings.Split(row, "\t")
	if len(parts) != 5 || parts[0] != ref || strings.ContainsAny(row, "\n\r\x1b\u202e\u2069\u2028") {
		t.Fatalf("%q", row)
	}
}
func TestActualPreviewExecutionWithHostileStaticPaths(t *testing.T) {
	base := t.TempDir()
	hostile := filepath.Join(base, "space ' quote ; $(not-a-command) `none`")
	if e := os.Mkdir(hostile, 0700); e != nil {
		t.Fatal(e)
	}
	executable := filepath.Join(hostile, "spool ' binary")
	configPath := filepath.Join(hostile, "config ; ' $(none).json")
	capture := filepath.Join(t.TempDir(), "preview")
	t.Setenv("CAPTURE", capture)
	if e := os.WriteFile(executable, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$CAPTURE\"\nprintf '{\"content\":\"untrusted_history\"}\\n'\n"), 0700); e != nil {
		t.Fatal(e)
	}
	ref := history.Ref{SourceID: "generic", Harness: "pi", Path: "a ' ; $(data).jsonl", NativeID: "generic"}.Encode()
	// Fake fzf executes its actual preview string under /bin/sh, emulating
	// fzf's quoted {1} substitution while ignoring every visible label.
	fzf := fakeCommand(t, "fzf", `while [ "$#" -gt 0 ]; do if [ "$1" = --preview ]; then preview=$2; shift; fi; shift; done
IFS= read -r first
ref=${first%%	*}
/bin/sh -c "${preview%???} '$ref'" > "$CAPTURE.out"
printf '%s\n' "$first"
`)
	cmd := exec.Command(fzf, "--preview", previewCommand(executable, configPath))
	cmd.Env = fzfEnvironment()
	cmd.Stdin = strings.NewReader(ref + "\tpi\t\t\t$(label)\n")
	if out, e := cmd.Output(); e != nil || !strings.HasPrefix(string(out), ref) {
		t.Fatalf("%s %v", out, e)
	}
	args, _ := os.ReadFile(capture)
	want := "__pick-preview\n--config\n" + configPath + "\n--\n" + ref + "\n"
	if string(args) != want {
		t.Fatalf("preview argv %q want %q", args, want)
	}
	preview, _ := os.ReadFile(capture + ".out")
	if !strings.Contains(string(preview), "untrusted_history") {
		t.Fatal("preview not executed")
	}
}
func continuationFixture(t *testing.T) (workspace, *config.Config, *continuation.Service, string, string) {
	t.Helper()
	w := newWorkspace(t)
	cwd := t.TempDir()
	path := filepath.Join(w.dir, "pi", "example-custom-id.jsonl")
	if e := os.RemoveAll(filepath.Dir(path)); e != nil {
		t.Fatal(e)
	}
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		t.Fatal(e)
	}
	// Dedicated generic Pi file; eliminate unrelated fixture cwd metadata.
	data := fmt.Sprintf("{\"type\":\"session\",\"version\":3,\"id\":\"generic\",\"cwd\":%q}\n{\"type\":\"message\",\"message\":{\"role\":\"user\",\"content\":\"generic data\"}}\n", cwd)
	if e := os.WriteFile(path, []byte(data), 0600); e != nil {
		t.Fatal(e)
	}
	cfg := &config.Config{Sources: []config.Source{{ID: "pi", Harness: "pi", Root: filepath.Dir(path), Enabled: true}}, Drafting: &config.Drafting{Provider: "example", Model: "exact-model", TimeoutSeconds: 30, MaxInputBytes: 131072}}
	v := map[string]any{"version": 1, "sources": []map[string]any{{"id": "pi", "harness": "pi", "root": filepath.Dir(path)}}, "drafting": cfg.Drafting}
	b, _ := json.Marshal(v)
	os.WriteFile(w.config, b, 0600)
	hist := history.NewService(cfg)
	page, e := hist.List(context.Background(), history.ListRequest{Limit: 500})
	if e != nil {
		t.Fatal(e)
	}
	svc := &continuation.Service{History: hist, Sources: cfg.Sources}
	return w, cfg, svc, page.Sessions[0].Ref, cwd
}

const fakeModel = `for a in "$@"; do if [ "$a" = --list-models ]; then printf 'example exact-model 1000\n'; exit 0; fi; done
/bin/cat > "$MODEL_INPUT"
printf '%s' '{"title":"Generic draft","goal":"Finish generic parser","constraints":[],"decisions":[],"progress":[],"outstanding":[],"notes":[]}'
`

func TestPickDisclosureReviewSaveAndCancellation(t *testing.T) {
	for _, phase := range []string{"disclosure", "review", "save"} {
		t.Run(phase, func(t *testing.T) {
			w, _, _, _, cwd := continuationFixture(t)
			fakeCommand(t, "fzf", firstFzf)
			capture := filepath.Join(t.TempDir(), "model-input")
			t.Setenv("MODEL_INPUT", capture)
			fakeCommand(t, "pi", fakeModel)
			input := "h\nclaude\nContinue generic work\n" + cwd + "\n"
			switch phase {
			case "disclosure":
				input += "no\n"
			case "review":
				input += "yes\nc\n"
			case "save":
				input += "yes\na\ns\n"
			}
			before := tree(t, w.dir)
			r := pickRun(t, w, input)
			want := 130
			if phase == "save" {
				want = 0
			}
			if r.code != want || r.stdout != "" {
				t.Fatalf("%#v", r)
			}
			if !strings.Contains(r.stderr, "secrets") || !strings.Contains(r.stderr, "example/exact-model") || !strings.Contains(r.stderr, "included physical lines") && !strings.Contains(r.stderr, "Included physical lines") {
				t.Fatal("disclosure incomplete", r.stderr)
			}
			_, e := os.Stat(capture)
			if phase == "disclosure" && !os.IsNotExist(e) {
				t.Fatal("declined disclosure spawned model")
			}
			if phase != "disclosure" && e != nil {
				t.Fatal("model missing", e)
			}
			entries, _ := os.ReadDir(filepath.Join(w.state(), "spool", "handoffs"))
			if (phase == "save") != (len(entries) == 1) {
				t.Fatal("unapproved publication or save failed", entries)
			}
			if !equalMaps(before, tree(t, w.dir)) {
				t.Fatal("history mutated")
			}
		})
	}
}
func TestPromptCancellationWithoutCompetingReaders(t *testing.T) {
	for _, phase := range []string{"action", "model disclosure", "review"} {
		t.Run(phase, func(t *testing.T) {
			in, out, e := os.Pipe()
			if e != nil {
				t.Fatal(e)
			}
			defer in.Close()
			defer out.Close()
			ctx, cancel := context.WithCancel(context.Background())
			var diagnostics bytes.Buffer
			u := pickUI{app: app{stderr: &diagnostics}, input: bufio.NewReader(contextInput{ctx, in})}
			done := make(chan error, 1)
			go func() { _, err := u.line(phase + ": "); done <- err }()
			cancel()
			select {
			case err := <-done:
				if err != context.Canceled {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("prompt ignored cancellation")
			}
			// No abandoned reader may steal bytes after cancellation.
			out.Write([]byte("next\n"))
			line, e := bufio.NewReader(in).ReadString('\n')
			if e != nil || line != "next\n" {
				t.Fatal("stdin reader leaked")
			}
		})
	}
}
func TestEditorArgvHasNoShellExpansion(t *testing.T) {
	args, e := editorArgv(`editor --arg 'a b' "c d" '$HOME;$(no)'`)
	if e != nil || fmt.Sprint(args) != "[editor --arg a b c d $HOME;$(no)]" {
		t.Fatalf("%v %v", args, e)
	}
	for _, s := range []string{"", `editor 'unterminated`, `editor \`} {
		if _, e := editorArgv(s); e == nil {
			t.Fatal("accepted", s)
		}
	}
}
func TestFakeEditorEditsAndRejectsUnsafeFiles(t *testing.T) {
	for _, mode := range []string{"valid", "atomic valid", "source injection", "symlink", "directory", "too big", "insecure", "failed", "canceled"} {
		t.Run(mode, func(t *testing.T) {
			w, cfg, svc, ref, cwd := continuationFixture(t)
			capture := filepath.Join(t.TempDir(), "input")
			t.Setenv("MODEL_INPUT", capture)
			fakeCommand(t, "pi", fakeModel)
			svc.Drafter = continuation.PiDrafter{Sources: cfg.Sources}
			prepared, e := svc.Prepare(context.Background(), ref, "claude", "Continue generic work", cfg.Drafting)
			if e != nil {
				t.Fatal(e)
			}
			draft, e := svc.Generate(context.Background(), prepared, prepared.ConfirmDisclosure(), cwd)
			if e != nil {
				t.Fatal(e)
			}
			switch mode {
			case "valid", "atomic valid":
				captureBytes := bytes.Replace(draft.Bytes(), []byte("Finish generic parser"), []byte("User reviewed goal"), 1)
				os.WriteFile(capture, captureBytes, 0600)
			case "source injection":
				os.WriteFile(capture, []byte(`{"version":1,"goal":"Injected","sessions":[]}`), 0600)
			}
			script := `last=''; for a in "$@"; do last=$a; done
printf '%s' "$last" > "$EDITOR_PATH_CAPTURE"
`
			switch mode {
			case "atomic valid":
				script += `/bin/cat "$MODEL_INPUT" > "$last.new"; /bin/chmod 600 "$last.new"; /bin/mv "$last.new" "$last"`
			case "valid", "source injection":
				script += `/bin/cat "$MODEL_INPUT" > "$last"`
			case "symlink":
				script += `/bin/rm "$last"; /bin/ln -s "$MODEL_INPUT" "$last"`
			case "directory":
				script += `/bin/rm "$last"; /bin/mkdir "$last"`
			case "too big":
				script += `i=0; while [ "$i" -lt 3000 ]; do printf '%0100d' 0; i=$((i+1)); done > "$last"`
			case "insecure":
				script += `/bin/chmod 644 "$last"`
			case "failed":
				script += "exit 1"
			case "canceled":
				script += "exec /bin/sleep 30"
			}
			editor := fakeCommand(t, "editor", script)
			editorCapture := filepath.Join(t.TempDir(), "path")
			t.Setenv("EDITOR_PATH_CAPTURE", editorCapture)
			env := w.env()
			get := env.Getenv
			env.Getenv = func(k string) string {
				if k == "VISUAL" {
					return shellQuote(editor) + " --literal '$HOME;$(none)'"
				}
				return get(k)
			}
			u := pickUI{app: app{env: env, stdin: strings.NewReader(""), stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}}}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "canceled" {
				timer := time.AfterFunc(time.Second, cancel)
				defer timer.Stop()
			}
			e = u.edit(ctx, cfg, draft)
			if mode == "canceled" && (!errors.Is(e, context.Canceled) || u.app.pickFail(e, exitOperational) != exitCanceled) {
				t.Fatalf("editor cancellation lost: %v", e)
			}
			if mode == "valid" || mode == "atomic valid" {
				if e != nil || !strings.Contains(draft.Render(), "User reviewed goal") {
					t.Fatalf("valid edit %v", e)
				}
			} else if e == nil {
				t.Fatal("unsafe edit accepted")
			}
			private, _ := os.ReadFile(editorCapture)
			if _, e := os.Stat(filepath.Dir(string(private))); !os.IsNotExist(e) {
				t.Fatal("editor temp not cleaned")
			}
			if _, e := os.Stat(filepath.Join(w.state(), "spool", "handoffs")); !os.IsNotExist(e) {
				t.Fatal("editor published")
			}
		})
	}
}
func TestContinuationDocumentationContract(t *testing.T) {
	b, e := os.ReadFile("../../docs/continuation.md")
	if e != nil {
		t.Fatal(e)
	}
	for _, claim := range []string{"spool pick --ref", "spool pick --harness pi", "secrets", "save-only", "residual pathname race", "not a snapshot", "--no-context-files", "--append-system-prompt", "256 KiB", "32 KiB", "130", "native Pi", "Claude", "Codex"} {
		if !strings.Contains(string(b), claim) {
			t.Error("missing documentation", claim)
		}
	}
	if !strings.Contains(handoff.Preamble, "does not execute its contents") {
		t.Fatal("brief trust warning weakened")
	}
}

func TestNativeResumeUnavailableForClaudeSubagentsAndCodexArchives(t *testing.T) {
	w := newWorkspace(t)
	cfg, e := config.Load(w.config, w.env())
	if e != nil {
		t.Fatal(e)
	}
	hist := history.NewService(cfg)
	svc := &continuation.Service{History: hist, Sources: cfg.Sources}
	page, herr := hist.List(context.Background(), history.ListRequest{Limit: 500})
	if herr != nil {
		t.Fatal(herr)
	}
	seenSubagent, seenArchive := false, false
	for _, s := range page.Sessions {
		if s.Kind == "subagent" {
			seenSubagent = true
		}
		if s.SourceID == "codex-archive" {
			seenArchive = true
		}
		if s.Harness != "pi" {
			if _, e := svc.ResolveResume(context.Background(), s.Ref, t.TempDir()); e == nil {
				t.Fatal("unproven resume accepted")
			}
		}
	}
	if !seenSubagent || !seenArchive {
		t.Fatal("required generic mappings not exercised")
	}
}
func TestInspectJSONEscapesBidiAndInvisibleControls(t *testing.T) {
	encoded := `{"content":"untrusted_history","text":"` + "\u202e\u0085\u2028\x1b" + `"}`
	safe := safeInspectJSON(encoded)
	if strings.ContainsAny(safe, "\u202e\u0085\u2028\x1b") {
		t.Fatalf("invisible control retained: %q", safe)
	}
	if !json.Valid([]byte(safe)) {
		t.Fatalf("invalid JSON: %q", safe)
	}
}
func TestPreviewEscapesRealControlsWithoutChangingRead(t *testing.T) {
	w, cfg, _, ref, _ := continuationFixture(t)
	path := filepath.Join(cfg.Sources[0].Root, "example-custom-id.jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	record, err := json.Marshal(map[string]any{"type": "message", "message": map[string]string{"role": "user", "content": "untrusted\u202e\u0085\u2028\x1bdata"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(append(data, record...), '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	ordinary := w.run(t, "read", "--config", w.config, "--", ref)
	if ordinary.code != 0 || !strings.Contains(ordinary.stdout, "\u202e") {
		t.Fatalf("ordinary read no longer preserves native text: %#v", ordinary)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := strings.Replace(previewCommand(executable, w.config), "{1}", shellQuote(ref), 1)
	cmd := exec.Command("/bin/sh", "-c", command)
	cmd.Env = append(os.Environ(), "SPOOL_TEST_AS_MAIN=1", "HOME="+w.home)
	preview, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("real preview command failed: %v: %s", err, preview)
	}
	if strings.ContainsAny(string(preview), "\u202e\u0085\u2028\x1b") || !strings.Contains(string(preview), `\u202e`) || !json.Valid(preview) {
		t.Fatalf("unsafe preview JSON: %q", preview)
	}
	var decoded any
	decoder := json.NewDecoder(bytes.NewReader(preview))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		t.Fatal(err)
	}
	canonical, err := json.Marshal(decoded)
	if err != nil {
		t.Fatal(err)
	}
	original, err := json.Marshal(ordinary.json)
	if err != nil || !bytes.Equal(canonical, original) {
		t.Fatalf("preview changed native JSON semantics: %v", err)
	}
}

func TestRealPartialSourceDiagnosticsRemainSelectable(t *testing.T) {
	w := newWorkspace(t)
	var v map[string]any
	b, _ := os.ReadFile(w.config)
	json.Unmarshal(b, &v)
	sources := v["sources"].([]any)
	sources = append(sources, map[string]any{"id": "unavailable", "harness": "pi", "root": filepath.Join(t.TempDir(), "missing")})
	v["sources"] = sources
	b, _ = json.Marshal(v)
	os.WriteFile(w.config, b, 0600)
	fakeCommand(t, "fzf", firstFzf)
	r := pickRun(t, w, "", "--ref")
	if r.code != 0 || !strings.Contains(r.stderr, "source_unavailable") || r.stdout == "" {
		t.Fatalf("%#v", r)
	}
}
func TestPickRevalidatesSelectedRefAfterFzf(t *testing.T) {
	w, _, _, _, _ := continuationFixture(t)
	path := filepath.Join(w.dir, "pi", "example-custom-id.jsonl")
	t.Setenv("SOURCE_TO_DELETE", path)
	fakeCommand(t, "fzf", `IFS= read -r first; /bin/rm "$SOURCE_TO_DELETE"; printf '%s\n' "$first"`)
	r := pickRun(t, w, "", "--ref")
	if r.code != 3 || r.stdout != "" {
		t.Fatalf("%#v", r)
	}
}

func TestDuplicateVisibleLabelsRetainExactRefIdentity(t *testing.T) {
	first := history.Ref{SourceID: "generic", Harness: "pi", Path: "first.jsonl", NativeID: "duplicate"}.Encode()
	second := history.Ref{SourceID: "generic", Harness: "pi", Path: "second.jsonl", NativeID: "duplicate"}.Encode()
	title := "identical"
	rows := []history.Session{{Ref: first, Harness: "pi", Title: &title}, {Ref: second, Harness: "pi", Title: &title}}
	fzf := fakeCommand(t, "fzf", `IFS= read -r first; IFS= read -r second; printf '%s\n' "$second"`)
	var diagnostics bytes.Buffer
	a := app{stderr: &diagnostics}
	chosen, e := a.chooseSession(context.Background(), fzf, rows, "")
	if e != nil || chosen != second {
		t.Fatalf("%s %v", chosen, e)
	}
}
func TestNativeResumeAndReceiverMissingDependenciesStartNothing(t *testing.T) {
	_, _, svc, ref, cwd := continuationFixture(t)
	t.Setenv("PATH", t.TempDir())
	if _, e := svc.ResolveResume(context.Background(), ref, cwd); e == nil || !strings.Contains(e.Error(), "PATH") {
		t.Fatal(e)
	}
}
