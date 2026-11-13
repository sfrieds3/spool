package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/sfrieds3/spool/internal/config"
	"github.com/sfrieds3/spool/internal/history"
)

func TestSearchCLISharedServiceAndContinuation(t *testing.T) {
	w := newWorkspace(t)
	cli := w.run(t, "search", "Widget", "--config", w.config, "--limit", "1")
	if cli.code != 0 || cli.json["ok"] != true || cli.json["next_cursor"] == nil || cli.stderr != "" {
		t.Fatalf("first %d %s %q", cli.code, cli.stdout, cli.stderr)
	}
	cfg, err := config.Load(w.config, w.env())
	if err != nil {
		t.Fatal(err)
	}
	service, serr := history.NewService(cfg).Search(context.Background(), history.SearchRequest{Query: "Widget", Limit: 1})
	if serr != nil {
		t.Fatal(serr)
	}
	serialized, err := history.Marshal(service)
	if err != nil {
		t.Fatal(err)
	}
	if cli.stdout != string(serialized)+"\n" {
		t.Fatalf("CLI differs from service\nCLI %s\nservice %s", cli.stdout, serialized)
	}
	cursor := cli.json["next_cursor"].(string)
	second := w.run(t, "search", "--config", w.config, "--cursor", cursor, "--limit", "2", "Widget")
	if second.code != 0 || len(second.json["hits"].([]any)) == 0 {
		t.Fatalf("continuation: %s", second.stdout)
	}
	for _, hit := range second.json["hits"].([]any) {
		h := hit.(map[string]any)
		ref := h["session"].(map[string]any)["ref"].(string)
		line := h["line"].(json.Number)
		n, _ := line.Int64()
		r := w.run(t, "read", "--config", w.config, "--offset", strconv.FormatInt(n-1, 10), "--limit", "1", ref)
		if r.code != 0 || len(r.json["records"].([]any)) != 1 {
			t.Fatalf("roundtrip %s", r.stdout)
		}
	}
	// CLI reads an overriding explicit config, not implicit default roots.
	filtered := w.run(t, "search", "--config", w.config, "--harness", "pi", "example")
	if filtered.code != 0 || len(filtered.json["sources"].([]any)) != 1 {
		t.Fatalf("filter %s", filtered.stdout)
	}
	insensitive := w.run(t, "search", "--config", w.config, "WIDGET")
	sensitive := w.run(t, "search", "--config", w.config, "--case-sensitive", "WIDGET")
	if len(insensitive.json["hits"].([]any)) == 0 || len(sensitive.json["hits"].([]any)) != 0 {
		t.Fatalf("case mode insensitive=%s sensitive=%s", insensitive.stdout, sensitive.stdout)
	}
}

func TestSearchCLILiteralDashQueriesAndErrors(t *testing.T) {
	w := newWorkspace(t)
	path := filepath.Join(w.dir, "pi", "dash.jsonl")
	w.write(t, path, `{"type":"session","version":3,"id":"dash"}`+"\n"+`{"type":"message","message":{"role":"user","content":"-needle then --"}}`+"\n")
	for _, q := range []string{"-needle", "--"} {
		r := w.run(t, "search", "--config", w.config, "--", q)
		if r.code != 0 || len(r.json["hits"].([]any)) != 1 {
			t.Fatalf("dash %q = %d %s", q, r.code, r.stdout)
		}
	}
	for _, tt := range []struct {
		name string
		args []string
	}{
		{"missing", []string{"search", "--config", w.config}},
		{"extra", []string{"search", "one", "two", "--config", w.config}},
		{"unknown flag", []string{"search", "--bad", "needle"}},
		{"zero limit", []string{"search", "--limit", "0", "needle"}},
		{"bad query", []string{"search", "  ", "--config", w.config}},
		{"bad harness", []string{"search", "needle", "--harness", "other", "--config", w.config}},
		{"bad cursor", []string{"search", "needle", "--cursor", "bogus", "--config", w.config}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := w.run(t, tt.args...)
			if r.code != 2 || r.errorCode() != history.CodeInvalidRequest {
				t.Fatalf("got %d %s", r.code, r.stdout)
			}
		})
	}
}

func TestSearchCLIPartialCancellationAndSourceImmutability(t *testing.T) {
	w := newWorkspace(t)
	before := map[string][]byte{}
	err := filepath.WalkDir(w.dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			before[path] = nil
			return nil
		}
		b, err := os.ReadFile(path)
		before[path] = b
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	partialCfg := filepath.Join(w.home, "partial.json")
	w.write(t, partialCfg, `{"version":1,"sources":[{"id":"gone","harness":"pi","root":"`+filepath.Join(w.home, "gone")+`"},{"id":"pi","harness":"pi","root":"`+filepath.Join(w.dir, "pi")+`"}]}`)
	r := spool(t, w, "search", "--config", partialCfg, "files")
	if r.code != 0 || r.json["partial"] != true || len(r.json["hits"].([]any)) == 0 {
		t.Fatalf("partial %s", r.stdout)
	}
	var stdout, stderr bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	code := app{stdout: &stdout, stderr: &stderr, env: w.env(), ctx: ctx}.run([]string{"search", "--config", w.config, "files"})
	if code != 1 || !strings.Contains(stdout.String(), `"code":"canceled"`) {
		t.Fatalf("cancel %d %s", code, stdout.String())
	}
	after := map[string][]byte{}
	err = filepath.WalkDir(w.dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			after[path] = nil
			return nil
		}
		b, err := os.ReadFile(path)
		after[path] = b
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("source files or entries changed")
	}
}
