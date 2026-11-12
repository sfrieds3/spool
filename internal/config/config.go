// Package config loads Spool's runtime source configuration.
//
// Configuration is a strict, versioned JSON document. It selects which local
// harness history roots Spool reads. It never contains remote hosts yet.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Version is the only supported configuration schema version.
const Version = 1

// Supported harness names.
const (
	HarnessPi     = "pi"
	HarnessClaude = "claude"
	HarnessCodex  = "codex"
)

// maxConfigBytes bounds configuration file reads.
const maxConfigBytes = 1 << 20

var sourceIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// IsHarness reports whether name is a supported harness.
func IsHarness(name string) bool {
	switch name {
	case HarnessPi, HarnessClaude, HarnessCodex:
		return true
	}
	return false
}

// ValidSourceID reports whether id is a valid source identifier.
func ValidSourceID(id string) bool { return sourceIDPattern.MatchString(id) }

// Source is one configured local history root.
type Source struct {
	ID      string
	Harness string
	Root    string
	Enabled bool
	// Default is true for built-in sources resolved without explicit
	// configuration. A missing default root means the harness has not been
	// used; a missing explicit root is an error.
	Default bool
}

// Config is the resolved source selection.
type Config struct {
	// Path is the configuration file that was loaded, or empty when built-in
	// defaults were used because no default file exists.
	Path    string
	Sources []Source
}

// Error reports invalid configuration. Callers map it to invalid_config.
type Error struct{ Msg string }

func (e *Error) Error() string { return "invalid configuration: " + e.Msg }

func invalid(format string, args ...any) error {
	return &Error{Msg: fmt.Sprintf(format, args...)}
}

// Env supplies process environment and home directory lookups. Tests inject
// fixed values so they never inspect real user histories.
type Env struct {
	Getenv func(string) string
	Home   func() (string, error)
}

// OSEnv returns an Env backed by the executing process.
func OSEnv() Env { return Env{Getenv: os.Getenv, Home: os.UserHomeDir} }

func (e Env) get(key string) string {
	if e.Getenv == nil {
		return ""
	}
	return e.Getenv(key)
}

func (e Env) home() (string, error) {
	if e.Home == nil {
		return "", errors.New("home directory unavailable")
	}
	home, err := e.Home()
	if err != nil {
		return "", err
	}
	if home == "" || !filepath.IsAbs(home) {
		return "", fmt.Errorf("home directory %q is not absolute", home)
	}
	return filepath.Clean(home), nil
}

// absEnv returns a cleaned absolute environment path, empty when unset, or an
// error when set to a relative value.
func (e Env) absEnv(key string) (string, error) {
	v := e.get(key)
	if v == "" {
		return "", nil
	}
	if !filepath.IsAbs(v) {
		return "", invalid("%s must be an absolute path", key)
	}
	return filepath.Clean(v), nil
}

// DefaultPath returns the default configuration file location:
// $XDG_CONFIG_HOME/spool/config.json, else ~/.config/spool/config.json.
func DefaultPath(env Env) (string, error) {
	xdg, err := env.absEnv("XDG_CONFIG_HOME")
	if err != nil {
		return "", err
	}
	if xdg != "" {
		return filepath.Join(xdg, "spool", "config.json"), nil
	}
	home, err := env.home()
	if err != nil {
		return "", invalid("cannot determine default configuration path: %v", err)
	}
	return filepath.Join(home, ".config", "spool", "config.json"), nil
}

// Load resolves the configuration. An explicit path must exist. Without an
// explicit path, an absent default file selects the built-in sources. Load
// never creates files.
func Load(explicitPath string, env Env) (*Config, error) {
	path := explicitPath
	if path == "" {
		p, err := DefaultPath(env)
		if err != nil {
			return nil, err
		}
		path = p
	}
	data, err := readLimited(path)
	if err != nil {
		if explicitPath == "" && errors.Is(err, fs.ErrNotExist) {
			sources, derr := DefaultSources(env)
			if derr != nil {
				return nil, derr
			}
			return &Config{Sources: sources}, nil
		}
		return nil, invalid("cannot read %s: %v", path, err)
	}
	cfg, err := Parse(data, env)
	if err != nil {
		return nil, err
	}
	cfg.Path = path
	return cfg, nil
}

func readLimited(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("not a regular file")
	}
	data, err := io.ReadAll(io.LimitReader(f, maxConfigBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxConfigBytes {
		return nil, fmt.Errorf("larger than %d bytes", maxConfigBytes)
	}
	return data, nil
}

type fileConfig struct {
	Version *int            `json:"version"`
	Sources json.RawMessage `json:"sources"`
}

type fileSource struct {
	ID      *string `json:"id"`
	Harness *string `json:"harness"`
	Root    *string `json:"root"`
	Enabled *bool   `json:"enabled"`
}

// Parse validates a configuration document. Omitting "sources" selects the
// built-in sources; an explicit array is the complete selection.
func Parse(data []byte, env Env) (*Config, error) {
	if err := checkDuplicateKeys(data); err != nil {
		return nil, err
	}
	var fc fileConfig
	if err := decodeStrict(data, &fc); err != nil {
		return nil, err
	}
	if fc.Version == nil {
		return nil, invalid("missing version")
	}
	if *fc.Version != Version {
		return nil, invalid("unsupported version %d", *fc.Version)
	}
	if fc.Sources == nil {
		sources, err := DefaultSources(env)
		if err != nil {
			return nil, err
		}
		return &Config{Sources: sources}, nil
	}
	if bytes.Equal(bytes.TrimSpace(fc.Sources), []byte("null")) {
		return nil, invalid("sources must be an array")
	}
	var raw []json.RawMessage
	if err := json.Unmarshal(fc.Sources, &raw); err != nil {
		return nil, invalid("sources must be an array")
	}
	cfg := &Config{Sources: make([]Source, 0, len(raw))}
	seen := map[string]bool{}
	for i, item := range raw {
		var fs fileSource
		if err := decodeStrict(item, &fs); err != nil {
			return nil, invalid("sources[%d]: %v", i, err.(*Error).Msg)
		}
		src, err := validateSource(i, fs)
		if err != nil {
			return nil, err
		}
		if seen[src.ID] {
			return nil, invalid("duplicate source id %q", src.ID)
		}
		seen[src.ID] = true
		cfg.Sources = append(cfg.Sources, src)
	}
	return cfg, nil
}

func validateSource(i int, fs fileSource) (Source, error) {
	if fs.ID == nil || !ValidSourceID(*fs.ID) {
		return Source{}, invalid("sources[%d]: id must match %s", i, sourceIDPattern)
	}
	if fs.Harness == nil || !IsHarness(*fs.Harness) {
		return Source{}, invalid("sources[%d]: harness must be one of pi, claude, codex", i)
	}
	if fs.Root == nil || !filepath.IsAbs(*fs.Root) {
		return Source{}, invalid("sources[%d]: root must be an absolute path", i)
	}
	if strings.ContainsRune(*fs.Root, 0) {
		return Source{}, invalid("sources[%d]: root contains NUL", i)
	}
	enabled := true
	if fs.Enabled != nil {
		enabled = *fs.Enabled
	}
	return Source{ID: *fs.ID, Harness: *fs.Harness, Root: filepath.Clean(*fs.Root), Enabled: enabled}, nil
}

func decodeStrict(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return invalid("%v", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return invalid("unexpected trailing data")
	}
	return nil
}

// checkDuplicateKeys rejects objects that repeat a key at any depth.
func checkDuplicateKeys(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var walk func() error
	walk = func() error {
		tok, err := dec.Token()
		if err != nil {
			return invalid("%v", err)
		}
		delim, ok := tok.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			keys := map[string]bool{}
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return invalid("%v", err)
				}
				key := kt.(string)
				if keys[key] {
					return invalid("duplicate key %q", key)
				}
				keys[key] = true
				if err := walk(); err != nil {
					return err
				}
			}
		case '[':
			for dec.More() {
				if err := walk(); err != nil {
					return err
				}
			}
		}
		if _, err := dec.Token(); err != nil {
			return invalid("%v", err)
		}
		return nil
	}
	return walk()
}
