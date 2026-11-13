package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sfrieds3/spool/internal/handoff"
)

// runIn runs the CLI in-process with stdin and fixed handoff dependencies.
func (w workspace) runIn(t *testing.T, stdin string, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	a := app{stdin: strings.NewReader(stdin), stdout: &stdout, stderr: &stderr, env: w.env(), ctx: context.Background(),
		handoff: handoff.Options{
			Now:      func() time.Time { return time.Date(2026, 11, 12, 15, 30, 0, 0, time.UTC) },
			Hostname: func() (string, error) { return "example-host", nil },
			Getwd:    func() (string, error) { return "/home/example/project", nil },
		}}
	code := a.run(args)
	return code, stdout.String(), stderr.String()
}

func (w workspace) runJSON(t *testing.T, stdin string, args ...string) result {
	t.Helper()
	code, out, errOut := w.runIn(t, stdin, args...)
	return decode(t, code, out, errOut)
}

func TestHandoffCLI(t *testing.T) {
	w := newWorkspace(t)
	list := w.run(t, "list", "--config", w.config)
	piRef := refWithPath(t, list, "example-custom-id.jsonl")

	// Empty list before any handoff; no directory is created.
	r := w.runJSON(t, "", "handoff", "list", "--config", w.config)
	if r.code != 0 || len(r.json["handoffs"].([]any)) != 0 || r.json["eof"] != true {
		t.Fatalf("empty list = %s", r.stdout)
	}
	if _, err := os.Stat(w.state()); !os.IsNotExist(err) {
		t.Fatal("list created the state directory")
	}

	brief := fmt.Sprintf(`{"version":1,"goal":"Finish","outstanding":["docs"],"sessions":[{"ref":%q}]}`, piRef)
	r = w.runJSON(t, brief, "handoff", "create", "--config", w.config, "--input", "-")
	if r.code != 0 || r.json["ok"] != true || len(r.json["warnings"].([]any)) != 0 {
		t.Fatalf("create = %d %s", r.code, r.stdout)
	}
	h := r.json["handoff"].(map[string]any)
	id := h["id"].(string)
	if h["workspace"].(map[string]any)["path"] != "/home/example/project" || h["origin"].(map[string]any)["spool_version"] != "dev" {
		t.Fatalf("handoff = %v", h)
	}

	file := filepath.Join(w.home, "brief.json")
	w.write(t, file, `{"version":1,"goal":"Second"}`)
	if r := w.runJSON(t, "", "handoff", "create", "--config", w.config, "--input", file); r.code != 0 {
		t.Fatalf("create from file = %s", r.stdout)
	}

	r = w.runJSON(t, "", "handoff", "list", "--config", w.config, "--limit", "1")
	if r.code != 0 || len(r.json["handoffs"].([]any)) != 1 || r.json["eof"] != false {
		t.Fatalf("paged list = %s", r.stdout)
	}

	r = w.runJSON(t, "", "handoff", "show", id, "--config", w.config)
	if r.code != 0 || r.json["content"] != "handoff_brief" || r.json["handoff"].(map[string]any)["id"] != id {
		t.Fatalf("show = %s", r.stdout)
	}

	code, md, errOut := w.runIn(t, "", "handoff", "show", "--format", "markdown", "--config", w.config, id)
	if code != 0 || errOut != "" || !strings.HasPrefix(md, "# Spool handoff `"+id+"`") || !strings.Contains(md, "spool read "+piRef) || !strings.Contains(md, "## Outstanding work") {
		t.Fatalf("markdown = %d %q", code, md)
	}
}

func TestHandoffCLIErrors(t *testing.T) {
	w := newWorkspace(t)
	big := filepath.Join(w.home, "big.json")
	w.write(t, big, `{"version":1,"goal":"`+strings.Repeat("a", handoff.MaxInputBytes)+`"}`)
	fifoDir := t.TempDir()
	tests := []struct {
		name  string
		stdin string
		args  []string
		code  int
		err   string
	}{
		{"no subcommand", "", []string{"handoff"}, 2, "invalid_request"},
		{"unknown subcommand", "", []string{"handoff", "delete"}, 2, "invalid_request"},
		{"missing input", "", []string{"handoff", "create", "--config", w.config}, 2, "invalid_request"},
		{"missing input file", "", []string{"handoff", "create", "--config", w.config, "--input", filepath.Join(w.home, "nope.json")}, 2, "invalid_request"},
		{"input is a directory", "", []string{"handoff", "create", "--config", w.config, "--input", fifoDir}, 2, "invalid_request"},
		{"oversized input", "", []string{"handoff", "create", "--config", w.config, "--input", big}, 2, "invalid_request"},
		{"invalid brief", `{"version":1}`, []string{"handoff", "create", "--config", w.config, "--input", "-"}, 2, "invalid_request"},
		{"create extra arg", "", []string{"handoff", "create", "--input", "-", "x"}, 2, "invalid_request"},
		{"missing session", `{"version":1,"goal":"g","sessions":[{"ref":"s1.WyJwaSIsInBpIiwiZ29uZS5qc29ubCIsIngiXQ"}]}`, []string{"handoff", "create", "--config", w.config, "--input", "-"}, 3, "session_not_found"},
		{"list bad limit", "", []string{"handoff", "list", "--config", w.config, "--limit", "0"}, 2, "invalid_request"},
		{"list big limit", "", []string{"handoff", "list", "--config", w.config, "--limit", "501"}, 2, "invalid_request"},
		{"list negative offset", "", []string{"handoff", "list", "--config", w.config, "--offset", "-1"}, 2, "invalid_request"},
		{"list extra arg", "", []string{"handoff", "list", "x"}, 2, "invalid_request"},
		{"show no id", "", []string{"handoff", "show", "--config", w.config}, 2, "invalid_request"},
		{"show two ids", "", []string{"handoff", "show", "a", "b"}, 2, "invalid_request"},
		{"show bad format", "", []string{"handoff", "show", "--format", "html", "20261112t153000z-00000000"}, 2, "invalid_request"},
		{"show traversal", "", []string{"handoff", "show", "--config", w.config, "../../etc/passwd"}, 2, "invalid_request"},
		{"show paging flag", "", []string{"handoff", "show", "--limit", "3", "20261112t153000z-00000000"}, 2, "invalid_request"},
		{"show unknown", "", []string{"handoff", "show", "--config", w.config, "20261112t153000z-00000000"}, 3, "handoff_not_found"},
		{"markdown unknown", "", []string{"handoff", "show", "--format", "markdown", "--config", w.config, "20261112t153000z-00000000"}, 3, "handoff_not_found"},
		{"invalid config", "", []string{"handoff", "list", "--config", filepath.Join(w.home, "missing.json")}, 2, "invalid_config"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := w.runJSON(t, tt.stdin, tt.args...)
			if r.code != tt.code || r.json["ok"] != false || r.errorCode() != tt.err || r.stderr != "" {
				t.Fatalf("got %d %s stderr %q", r.code, r.stdout, r.stderr)
			}
		})
	}
	if _, err := os.Stat(filepath.Join(w.state(), "spool", "handoffs")); !os.IsNotExist(err) {
		t.Fatal("failed commands created a handoff directory")
	}

	// Relative XDG_STATE_HOME and insecure or damaged storage.
	rel := w
	relEnv := rel.env()
	inner := relEnv.Getenv
	relEnv.Getenv = func(k string) string {
		if k == "XDG_STATE_HOME" {
			return "relative/state"
		}
		return inner(k)
	}
	var stdout bytes.Buffer
	code := app{stdin: strings.NewReader(""), stdout: &stdout, stderr: &bytes.Buffer{}, env: relEnv, ctx: context.Background()}.run([]string{"handoff", "list", "--config", w.config})
	if r := decode(t, code, stdout.String(), ""); r.code != 2 || r.errorCode() != "invalid_config" {
		t.Fatalf("relative state = %s", r.stdout)
	}
	dir := filepath.Join(w.state(), "spool", "handoffs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	os.Chmod(dir, 0o755)
	if r := w.runJSON(t, `{"version":1,"goal":"g"}`, "handoff", "create", "--config", w.config, "--input", "-"); r.code != 1 || r.errorCode() != "state_unavailable" {
		t.Fatalf("insecure dir = %s", r.stdout)
	}
	os.Chmod(dir, 0o700)
	id := "20261112t153000z-00000001"
	w.write(t, filepath.Join(dir, id+".json"), "{")
	if r := w.runJSON(t, "", "handoff", "show", "--config", w.config, id); r.code != 1 || r.errorCode() != "handoff_invalid" {
		t.Fatalf("damaged show = %s", r.stdout)
	}
	if r := w.runJSON(t, "", "handoff", "list", "--config", w.config); r.code != 0 || r.json["partial"] != true {
		t.Fatalf("damaged list = %s", r.stdout)
	}
}

// spoolRaw runs the real executable with stdin and returns raw output.
func spoolRaw(t *testing.T, w workspace, stdin string, args ...string) (int, string, string) {
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
	cmd.Stdin = strings.NewReader(stdin)
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
	return code, stdout.String(), stderr.String()
}

// TestEndToEndHandoff exercises the handoff lifecycle through the real
// executable and verifies that only the handoff directory and file are
// written.
func TestEndToEndHandoff(t *testing.T) {
	w := newWorkspace(t)
	list := spool(t, w, "list", "--config", w.config)
	var piRef, codexRef string
	for _, s := range list.sessions() {
		switch s["harness"] {
		case "pi":
			piRef = s["ref"].(string)
		case "codex":
			if s["source_id"] == "codex" {
				codexRef = s["ref"].(string)
			}
		}
	}
	if piRef == "" || codexRef == "" {
		t.Fatal("fixture refs missing")
	}
	sessionsBefore := tree(t, w.dir)
	homeBefore := tree(t, w.home)

	brief := fmt.Sprintf(`{"version":1,"title":"Port","goal":"Finish the port","decisions":["Use the stdlib"],"outstanding":["Write docs"],
		"workspace":{"repository":{"branch":"main"}},
		"sessions":[{"ref":%q,"note":"plan"},{"ref":%q}]}`, piRef, codexRef)
	code, out, errOut := spoolRaw(t, w, brief, "handoff", "create", "--config", w.config, "--input", "-")
	created := decode(t, code, out, errOut)
	if created.code != 0 || errOut != "" {
		t.Fatalf("create = %d %s %s", code, out, errOut)
	}
	h := created.json["handoff"].(map[string]any)
	id := h["id"].(string)
	if ws := h["workspace"].(map[string]any); ws["path_provenance"] != "process_cwd" || !strings.HasSuffix(ws["path"].(string), filepath.Base(w.home)) {
		t.Fatalf("workspace = %v", ws)
	}

	listed := spool(t, w, "handoff", "list", "--config", w.config)
	if hs := listed.json["handoffs"].([]any); len(hs) != 1 || hs[0].(map[string]any)["id"] != id {
		t.Fatalf("list = %s", listed.stdout)
	}
	shown := spool(t, w, "handoff", "show", "--config", w.config, id)
	if shown.json["handoff"].(map[string]any)["goal"] != "Finish the port" {
		t.Fatalf("show = %s", shown.stdout)
	}
	code, md, errOut := spoolRaw(t, w, "", "handoff", "show", "--format", "markdown", "--config", w.config, id)
	if code != 0 || errOut != "" || !strings.Contains(md, handoff.Preamble) || !strings.Contains(md, "spool read "+codexRef) {
		t.Fatalf("markdown = %d %q", code, md)
	}

	// Sources are unchanged; home gained only the handoff directory chain
	// and the one handoff file.
	if after := tree(t, w.dir); !equalMaps(sessionsBefore, after) {
		t.Fatal("session sources changed")
	}
	added := map[string]bool{}
	for p := range tree(t, w.home) {
		if _, ok := homeBefore[p]; !ok {
			added[p] = true
		}
	}
	state := w.state()
	want := []string{state, filepath.Join(state, "spool"), filepath.Join(state, "spool", "handoffs"), filepath.Join(state, "spool", "handoffs", id+".json")}
	if len(added) != len(want) {
		t.Fatalf("added = %v", added)
	}
	for _, p := range want {
		if !added[p] {
			t.Fatalf("missing %s in %v", p, added)
		}
	}
}

// TestHandoffPackagesAvoidProcessAndNetwork checks the handoff service's own
// imports, complementing the whole-binary dependency check.
func TestHandoffPackagesAvoidProcessAndNetwork(t *testing.T) {
	gocmd, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go toolchain not on PATH")
	}
	out, err := exec.Command(gocmd, "list", "-deps", "../../internal/handoff").Output()
	if err != nil {
		t.Fatal(err)
	}
	for _, dep := range strings.Fields(string(out)) {
		if dep == "os/exec" || dep == "net" || strings.HasPrefix(dep, "net/http") {
			t.Errorf("internal/handoff depends on %s", dep)
		}
	}
}
