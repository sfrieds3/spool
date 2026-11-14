package config

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func fakeEnv(home string, vars map[string]string) Env {
	return Env{
		Getenv: func(k string) string { return vars[k] },
		Home: func() (string, error) {
			if home == "" {
				return "", errors.New("no home")
			}
			return home, nil
		},
	}
}

func isInvalid(err error) bool {
	var ce *Error
	return errors.As(err, &ce)
}

func TestParseValid(t *testing.T) {
	data := `{"version":1,"sources":[
		{"id":"pi","harness":"pi","root":"/srv/pi/"},
		{"id":"c2","harness":"claude","root":"/srv/claude","enabled":false},
		{"id":"c3","harness":"claude","root":"/srv/claude2","enabled":true}
	]}`
	cfg, err := Parse([]byte(data), fakeEnv("/home/u", nil))
	if err != nil {
		t.Fatal(err)
	}
	want := []Source{
		{ID: "pi", Harness: "pi", Root: "/srv/pi", Enabled: true},
		{ID: "c2", Harness: "claude", Root: "/srv/claude", Enabled: false},
		{ID: "c3", Harness: "claude", Root: "/srv/claude2", Enabled: true},
	}
	if !reflect.DeepEqual(cfg.Sources, want) {
		t.Fatalf("sources = %#v", cfg.Sources)
	}
}

func TestParseEmptyAndOmittedSources(t *testing.T) {
	cfg, err := Parse([]byte(`{"version":1,"sources":[]}`), fakeEnv("/home/u", nil))
	if err != nil || len(cfg.Sources) != 0 {
		t.Fatalf("empty sources: %v %#v", err, cfg)
	}
	cfg, err = Parse([]byte(`{"version":1}`), fakeEnv("/home/u", nil))
	if err != nil || len(cfg.Sources) != 4 || !cfg.Sources[0].Default {
		t.Fatalf("omitted sources should use defaults: %v %#v", err, cfg)
	}
}

func TestParseInvalid(t *testing.T) {
	tests := map[string]string{
		"not json":             `nope`,
		"missing version":      `{"sources":[]}`,
		"unsupported version":  `{"version":2,"sources":[]}`,
		"unknown top field":    `{"version":1,"sources":[],"hosts":[]}`,
		"unknown source field": `{"version":1,"sources":[{"id":"a","harness":"pi","root":"/a","host":"x"}]}`,
		"duplicate top key":    `{"version":1,"version":1,"sources":[]}`,
		"duplicate nested key": `{"version":1,"sources":[{"id":"a","id":"b","harness":"pi","root":"/a"}]}`,
		"duplicate id":         `{"version":1,"sources":[{"id":"a","harness":"pi","root":"/a"},{"id":"a","harness":"codex","root":"/b"}]}`,
		"trailing value":       `{"version":1,"sources":[]} {}`,
		"trailing garbage":     `{"version":1,"sources":[]} x`,
		"unsupported harness":  `{"version":1,"sources":[{"id":"a","harness":"cursor","root":"/a"}]}`,
		"relative root":        `{"version":1,"sources":[{"id":"a","harness":"pi","root":"rel/a"}]}`,
		"missing root":         `{"version":1,"sources":[{"id":"a","harness":"pi"}]}`,
		"bad id":               `{"version":1,"sources":[{"id":"../x","harness":"pi","root":"/a"}]}`,
		"uppercase id":         `{"version":1,"sources":[{"id":"Pi","harness":"pi","root":"/a"}]}`,
		"null sources":         `{"version":1,"sources":null}`,
		"object sources":       `{"version":1,"sources":{}}`,
		"string version":       `{"version":"1","sources":[]}`,
		"enabled string":       `{"version":1,"sources":[{"id":"a","harness":"pi","root":"/a","enabled":"yes"}]}`,
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := Parse([]byte(data), fakeEnv("/home/u", nil))
			if !isInvalid(err) {
				t.Fatalf("err = %v, want config error", err)
			}
		})
	}
}

func TestLoadDefaultLookup(t *testing.T) {
	dir := t.TempDir()
	// Absent default file uses built-ins and creates nothing.
	env := fakeEnv("/home/u", map[string]string{"XDG_CONFIG_HOME": dir})
	cfg, err := Load("", env)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Path != "" || len(cfg.Sources) != 4 {
		t.Fatalf("cfg = %#v", cfg)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("Load created files: %v", entries)
	}
	// Present default file is loaded.
	path := filepath.Join(dir, "spool", "config.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"version":1,"sources":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load("", env)
	if err != nil || cfg.Path != path || len(cfg.Sources) != 0 {
		t.Fatalf("cfg = %#v err = %v", cfg, err)
	}
	// Home fallback when XDG is unset.
	p, err := DefaultPath(fakeEnv("/home/u", nil))
	if err != nil || p != "/home/u/.config/spool/config.json" {
		t.Fatalf("DefaultPath = %q %v", p, err)
	}
	// Relative XDG is invalid.
	if _, err := Load("", fakeEnv("/home/u", map[string]string{"XDG_CONFIG_HOME": "rel"})); !isInvalid(err) {
		t.Fatalf("relative XDG err = %v", err)
	}
	// Invalid default file is an error, not a fallback.
	if err := os.WriteFile(path, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load("", env); !isInvalid(err) {
		t.Fatalf("invalid default err = %v", err)
	}
}

func TestLoadExplicitPath(t *testing.T) {
	dir := t.TempDir()
	env := fakeEnv("/home/u", map[string]string{"XDG_CONFIG_HOME": dir})
	// Missing explicit file is an error even though defaults exist.
	if _, err := Load(filepath.Join(dir, "missing.json"), env); !isInvalid(err) {
		t.Fatalf("missing explicit err = %v", err)
	}
	// Explicit path suppresses default lookup.
	def := filepath.Join(dir, "spool", "config.json")
	os.MkdirAll(filepath.Dir(def), 0o755)
	os.WriteFile(def, []byte(`broken`), 0o644)
	explicit := filepath.Join(dir, "explicit.json")
	os.WriteFile(explicit, []byte(`{"version":1,"sources":[{"id":"x","harness":"codex","root":"/x"}]}`), 0o644)
	cfg, err := Load(explicit, env)
	if err != nil || cfg.Path != explicit || len(cfg.Sources) != 1 || cfg.Sources[0].Default {
		t.Fatalf("cfg = %#v err = %v", cfg, err)
	}
	// A directory is not a config file.
	if _, err := Load(dir, env); !isInvalid(err) {
		t.Fatalf("directory err = %v", err)
	}
}

func TestDefaultSources(t *testing.T) {
	tests := []struct {
		name string
		home string
		vars map[string]string
		want map[string]string
	}{
		{
			name: "linux home",
			home: "/home/user",
			want: map[string]string{
				"pi":            "/home/user/.pi/agent/sessions",
				"claude":        "/home/user/.claude/projects",
				"codex":         "/home/user/.codex/sessions",
				"codex-archive": "/home/user/.codex/archived_sessions",
			},
		},
		{
			name: "macos home",
			home: "/Users/user",
			want: map[string]string{
				"pi":            "/Users/user/.pi/agent/sessions",
				"claude":        "/Users/user/.claude/projects",
				"codex":         "/Users/user/.codex/sessions",
				"codex-archive": "/Users/user/.codex/archived_sessions",
			},
		},
		{
			name: "env overrides",
			home: "/home/user",
			vars: map[string]string{
				"PI_CODING_AGENT_DIR": "/opt/pi",
				"CLAUDE_CONFIG_DIR":   "/opt/claude/",
				"CODEX_HOME":          "/opt/codex",
			},
			want: map[string]string{
				"pi":            "/opt/pi/sessions",
				"claude":        "/opt/claude/projects",
				"codex":         "/opt/codex/sessions",
				"codex-archive": "/opt/codex/archived_sessions",
			},
		},
		{
			name: "pi session dir wins and no home needed",
			vars: map[string]string{
				"PI_CODING_AGENT_DIR":         "/opt/pi",
				"PI_CODING_AGENT_SESSION_DIR": "/data/pi-sessions",
				"CLAUDE_CONFIG_DIR":           "/opt/claude",
				"CODEX_HOME":                  "/opt/codex",
			},
			want: map[string]string{
				"pi":            "/data/pi-sessions",
				"claude":        "/opt/claude/projects",
				"codex":         "/opt/codex/sessions",
				"codex-archive": "/opt/codex/archived_sessions",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sources, err := DefaultSources(fakeEnv(tt.home, tt.vars))
			if err != nil {
				t.Fatal(err)
			}
			got := map[string]string{}
			for _, s := range sources {
				if !s.Default || !s.Enabled {
					t.Errorf("source %s not default/enabled", s.ID)
				}
				got[s.ID] = s.Root
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %v want %v", got, tt.want)
			}
		})
	}
}

func TestDefaultSourcesInvalidEnv(t *testing.T) {
	for _, key := range []string{"PI_CODING_AGENT_SESSION_DIR", "PI_CODING_AGENT_DIR", "CLAUDE_CONFIG_DIR", "CODEX_HOME"} {
		if _, err := DefaultSources(fakeEnv("/home/u", map[string]string{key: "relative/dir"})); !isInvalid(err) {
			t.Errorf("%s relative: err = %v", key, err)
		}
	}
	if _, err := DefaultSources(fakeEnv("", nil)); !isInvalid(err) {
		t.Errorf("missing home: err = %v", err)
	}
}

func TestExplicitSourcesAreNotDefault(t *testing.T) {
	cfg, err := Parse([]byte(`{"version":1,"sources":[{"id":"pi","harness":"pi","root":"/home/user/.pi/agent/sessions"}]}`), fakeEnv("/home/user", nil))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Sources[0].Default {
		t.Fatal("explicit source with a default-looking root must still be explicit")
	}
}

func TestDocumentedExampleLoads(t *testing.T) {
	doc, err := os.ReadFile("../../docs/configuration.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(doc)
	start := strings.Index(text, "```json\n")
	if start < 0 {
		t.Fatal("no json example in docs/configuration.md")
	}
	rest := text[start+len("```json\n"):]
	end := strings.Index(rest, "```")
	cfg, err := Parse([]byte(rest[:end]), fakeEnv("/home/example", nil))
	if err != nil {
		t.Fatalf("documented example: %v", err)
	}
	if len(cfg.Sources) != 4 || cfg.Sources[3].Enabled {
		t.Fatalf("documented example parsed as %#v", cfg.Sources)
	}
	if !strings.Contains(text, "Remote hosts are not supported yet") {
		t.Fatal("docs must state that SSH hosts are not supported yet")
	}
}

func TestStateDir(t *testing.T) {
	p, err := StateDir(fakeEnv("/home/u", nil))
	if err != nil || p != "/home/u/.local/state/spool" {
		t.Fatalf("home fallback = %q %v", p, err)
	}
	p, err = StateDir(fakeEnv("", map[string]string{"XDG_STATE_HOME": "/x/state/"}))
	if err != nil || p != "/x/state/spool" {
		t.Fatalf("xdg = %q %v", p, err)
	}
	if _, err := StateDir(fakeEnv("/home/u", map[string]string{"XDG_STATE_HOME": "rel"})); !isInvalid(err) {
		t.Fatalf("relative XDG_STATE_HOME err = %v", err)
	}
	if _, err := StateDir(fakeEnv("", nil)); !isInvalid(err) {
		t.Fatalf("missing home err = %v", err)
	}
}
