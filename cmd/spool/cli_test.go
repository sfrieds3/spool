package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sfrieds3/spool/internal/config"
)

const fixtureRoot = "../../testdata/sessions"

// TestMain lets subprocess tests re-execute this binary as spool itself.
func TestMain(m *testing.M) {
	if os.Getenv("SPOOL_TEST_AS_MAIN") == "1" {
		main()
		return
	}
	os.Exit(m.Run())
}

// workspace is an isolated fixture copy and configuration.
type workspace struct {
	dir    string // root of copied sessions
	home   string
	config string // explicit config including all fixture sources
}

func newWorkspace(t *testing.T) workspace {
	t.Helper()
	base := t.TempDir()
	w := workspace{dir: filepath.Join(base, "sessions"), home: filepath.Join(base, "home")}
	if err := os.MkdirAll(w.home, 0o755); err != nil {
		t.Fatal(err)
	}
	err := filepath.WalkDir(fixtureRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(fixtureRoot, p)
		target := filepath.Join(w.dir, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	tmpl, err := os.ReadFile("../../testdata/fixtures-config.json.in")
	if err != nil {
		t.Fatal(err)
	}
	w.config = filepath.Join(base, "config.json")
	w.write(t, w.config, strings.ReplaceAll(string(tmpl), "@ROOT@", w.dir))
	return w
}

// state returns the isolated XDG_STATE_HOME.
func (w workspace) state() string { return filepath.Join(w.home, ".local-state") }

func (w workspace) write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (w workspace) env() config.Env {
	vars := map[string]string{"XDG_CONFIG_HOME": filepath.Join(w.home, ".config"), "XDG_STATE_HOME": w.state()}
	return config.Env{
		Getenv: func(k string) string { return vars[k] },
		Home:   func() (string, error) { return w.home, nil },
	}
}

type result struct {
	code   int
	stdout string
	stderr string
	json   map[string]any
}

func (w workspace) run(t *testing.T, args ...string) result {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := app{stdout: &stdout, stderr: &stderr, env: w.env(), ctx: context.Background()}.run(args)
	return decode(t, code, stdout.String(), stderr.String())
}

func decode(t *testing.T, code int, stdout, stderr string) result {
	t.Helper()
	r := result{code: code, stdout: stdout, stderr: stderr}
	if !strings.HasSuffix(stdout, "\n") || strings.Count(stdout, "\n") != 1 {
		t.Fatalf("stdout is not exactly one JSON line: %q", truncate(stdout))
	}
	dec := json.NewDecoder(strings.NewReader(stdout))
	dec.UseNumber()
	if err := dec.Decode(&r.json); err != nil {
		t.Fatalf("stdout is not JSON: %v", err)
	}
	if dec.More() {
		t.Fatal("stdout has more than one JSON value")
	}
	if r.json["version"] != json.Number("1") {
		t.Fatalf("version = %v", r.json["version"])
	}
	return r
}

func (r result) errorCode() string {
	e, _ := r.json["error"].(map[string]any)
	if e == nil {
		return ""
	}
	c, _ := e["code"].(string)
	return c
}

func (r result) sessions() []map[string]any {
	var out []map[string]any
	list, _ := r.json["sessions"].([]any)
	for _, s := range list {
		out = append(out, s.(map[string]any))
	}
	return out
}

func refWithPath(t *testing.T, r result, suffix string) string {
	t.Helper()
	for _, s := range r.sessions() {
		if strings.HasSuffix(s["path"].(string), suffix) {
			return s["ref"].(string)
		}
	}
	t.Fatalf("no session %s", suffix)
	return ""
}

func TestListAndReadSuccess(t *testing.T) {
	w := newWorkspace(t)
	r := w.run(t, "list", "--config", w.config)
	if r.code != 0 || r.json["ok"] != true || r.json["partial"] != false || r.json["eof"] != true || len(r.sessions()) != 5 || r.stderr != "" {
		t.Fatalf("list = %d %s", r.code, r.stdout)
	}
	// Flags after positional arguments, and the harness filter.
	r = w.run(t, "list", "--harness", "claude", "--limit", "1", "--config="+w.config)
	if r.code != 0 || len(r.sessions()) != 1 || r.json["next_offset"] != json.Number("1") {
		t.Fatalf("filtered = %s", r.stdout)
	}
	r = w.run(t, "list", "--harness=claude", "--offset", "1", "--config", w.config)
	if len(r.sessions()) != 1 || r.sessions()[0]["kind"] != "subagent" || r.json["eof"] != true {
		t.Fatalf("second page = %s", r.stdout)
	}
	ref := refWithPath(t, w.run(t, "list", "--config", w.config), "example-custom-id.jsonl")
	r = w.run(t, "read", ref, "--config", w.config, "--limit", "3")
	if r.code != 0 || r.json["content"] != "untrusted_history" || r.json["next_offset"] != json.Number("3") {
		t.Fatalf("read = %s", r.stdout)
	}
	r = w.run(t, "read", "--config", w.config, "--offset", "3", "--", ref)
	if r.code != 0 || r.json["eof"] != true || len(r.json["records"].([]any)) != 7 {
		t.Fatalf("read rest = %s", r.stdout)
	}
}

func TestEmptyCatalogs(t *testing.T) {
	w := newWorkspace(t)
	// Implicit defaults under an empty home: all roots absent.
	r := w.run(t, "list")
	if r.code != 0 || len(r.sessions()) != 0 || r.json["partial"] != false {
		t.Fatalf("absent defaults = %s", r.stdout)
	}
	for _, s := range r.json["sources"].([]any) {
		if s.(map[string]any)["state"] != "absent" {
			t.Fatalf("source = %v", s)
		}
	}
	empty := filepath.Join(w.home, "empty.json")
	w.write(t, empty, `{"version":1,"sources":[]}`)
	r = w.run(t, "list", "--config", empty)
	if r.code != 0 || len(r.sessions()) != 0 {
		t.Fatalf("no sources = %s", r.stdout)
	}
}

func TestCLIErrors(t *testing.T) {
	w := newWorkspace(t)
	missingRoot := filepath.Join(w.home, "bad.json")
	w.write(t, missingRoot, `{"version":1,"sources":[{"id":"pi","harness":"pi","root":"`+filepath.Join(w.home, "nope")+`"}]}`)
	ref := refWithPath(t, w.run(t, "list", "--config", w.config), "example-custom-id.jsonl")
	tests := []struct {
		name string
		args []string
		code int
		err  string
	}{
		{"unknown list flag", []string{"list", "--bogus"}, 2, "invalid_request"},
		{"list extra argument", []string{"list", "extra"}, 2, "invalid_request"},
		{"unknown harness", []string{"list", "--harness", "cursor", "--config", w.config}, 2, "invalid_request"},
		{"zero limit", []string{"list", "--limit", "0", "--config", w.config}, 2, "invalid_request"},
		{"limit too large", []string{"list", "--limit", "501", "--config", w.config}, 2, "invalid_request"},
		{"negative offset", []string{"list", "--offset", "-1", "--config", w.config}, 2, "invalid_request"},
		{"non-numeric limit", []string{"list", "--limit", "ten"}, 2, "invalid_request"},
		{"missing flag value", []string{"list", "--config"}, 2, "invalid_request"},
		{"read without ref", []string{"read", "--config", w.config}, 2, "invalid_request"},
		{"read two refs", []string{"read", ref, ref, "--config", w.config}, 2, "invalid_request"},
		{"read malformed ref", []string{"read", "not-a-ref", "--config", w.config}, 2, "invalid_request"},
		{"read bad limit", []string{"read", ref, "--limit", "0", "--config", w.config}, 2, "invalid_request"},
		{"missing config", []string{"list", "--config", filepath.Join(w.home, "missing.json")}, 2, "invalid_config"},
		{"all sources fail", []string{"list", "--config", missingRoot}, 1, "sources_unavailable"},
		{"read source unavailable", []string{"read", ref, "--config", missingRoot}, 1, "source_unavailable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := w.run(t, tt.args...)
			if r.code != tt.code || r.json["ok"] != false || r.errorCode() != tt.err || r.stderr != "" {
				t.Fatalf("got %d %s stderr %q", r.code, r.stdout, r.stderr)
			}
		})
	}
	// Missing session.
	os.Remove(filepath.Join(w.dir, "pi/--home-example-project--/2026-01-02T03-04-05-000Z_example-custom-id.jsonl"))
	if r := w.run(t, "read", ref, "--config", w.config); r.code != 3 || r.errorCode() != "session_not_found" {
		t.Fatalf("missing = %d %s", r.code, r.stdout)
	}
}

// spool runs the real executable entry point in a subprocess with an
// isolated environment and a PATH without any harness executables.
func spool(t *testing.T, w workspace, args ...string) result {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, args...)
	cmd.Env = []string{
		"SPOOL_TEST_AS_MAIN=1",
		"HOME=" + w.home,
		"XDG_CONFIG_HOME=" + filepath.Join(w.home, ".config"),
		"XDG_STATE_HOME=" + w.state(),
		"PATH=" + filepath.Join(w.home, "empty-bin"),
	}
	cmd.Dir = w.home
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err = cmd.Run()
	code := 0
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return decode(t, code, stdout.String(), stderr.String())
}

func TestSubprocessContract(t *testing.T) {
	w := newWorkspace(t)
	// Success.
	list := spool(t, w, "list", "--config", w.config)
	if list.code != 0 || len(list.sessions()) != 5 || list.stderr != "" {
		t.Fatalf("list = %d %s %q", list.code, list.stdout, list.stderr)
	}
	ref := refWithPath(t, list, "example-custom-id.jsonl")
	read := spool(t, w, "read", "--config", w.config, ref)
	if read.code != 0 || read.json["view"] != "native_file_order" || read.stderr != "" {
		t.Fatalf("read = %d %s", read.code, read.stdout)
	}
	// Partial success: one configured source is missing.
	partialCfg := filepath.Join(w.home, "partial.json")
	w.write(t, partialCfg, `{"version":1,"sources":[{"id":"gone","harness":"codex","root":"`+filepath.Join(w.home, "gone")+`"},{"id":"pi","harness":"pi","root":"`+filepath.Join(w.dir, "pi")+`"}]}`)
	partial := spool(t, w, "list", "--config", partialCfg)
	if partial.code != 0 || partial.json["ok"] != true || partial.json["partial"] != true || len(partial.sessions()) != 1 {
		t.Fatalf("partial = %d %s", partial.code, partial.stdout)
	}
	// Invalid configuration.
	badCfg := filepath.Join(w.home, "bad.json")
	w.write(t, badCfg, `{"version":1,"sources":[{"id":"pi","harness":"pi","root":"relative"}]}`)
	if r := spool(t, w, "list", "--config", badCfg); r.code != 2 || r.errorCode() != "invalid_config" {
		t.Fatalf("invalid config = %d %s", r.code, r.stdout)
	}
	// Default config file in XDG_CONFIG_HOME is honored.
	w.write(t, filepath.Join(w.home, ".config/spool/config.json"), `{"version":1,"sources":[{"id":"pi","harness":"pi","root":"`+filepath.Join(w.dir, "pi")+`"}]}`)
	if r := spool(t, w, "list"); r.code != 0 || len(r.sessions()) != 1 {
		t.Fatalf("default config = %d %s", r.code, r.stdout)
	}
	// Unsupported format.
	legacy := filepath.Join(w.dir, "pi/legacy.jsonl")
	w.write(t, legacy, `{"type":"session","id":"old"}`+"\n")
	l := spool(t, w, "list", "--config", w.config, "--harness", "pi")
	if l.json["partial"] != true {
		t.Fatalf("legacy listing = %s", l.stdout)
	}
	// Missing session.
	os.Remove(filepath.Join(w.dir, "pi/--home-example-project--/2026-01-02T03-04-05-000Z_example-custom-id.jsonl"))
	if r := spool(t, w, "read", "--config", w.config, ref); r.code != 3 || r.errorCode() != "session_not_found" {
		t.Fatalf("missing = %d %s", r.code, r.stdout)
	}
	// Operational failure.
	allBad := filepath.Join(w.home, "allbad.json")
	w.write(t, allBad, `{"version":1,"sources":[{"id":"gone","harness":"codex","root":"`+filepath.Join(w.home, "gone")+`"}]}`)
	if r := spool(t, w, "list", "--config", allBad); r.code != 1 || r.errorCode() != "sources_unavailable" {
		t.Fatalf("operational = %d %s", r.code, r.stdout)
	}
}

func TestSubprocessUnsupportedRead(t *testing.T) {
	w := newWorkspace(t)
	path := filepath.Join(w.dir, "codex/2026/01/02/rollout-2026-01-02T08-00-00-0e5a0000-0000-4000-8000-000000000001.jsonl")
	ref := refWithPath(t, spool(t, w, "list", "--config", w.config), filepath.Base(path))
	data, _ := os.ReadFile(path)
	w.write(t, path, string(data)+`{"timestamp":"t","type":"session_meta","payload":{"id":"0e5a0000-0000-4000-8000-000000000001","history_mode":"paginated"}}`+"\n")
	if r := spool(t, w, "read", "--config", w.config, ref); r.code != 1 || r.errorCode() != "unsupported_format" {
		t.Fatalf("unsupported = %d %s", r.code, r.stdout)
	}
}
