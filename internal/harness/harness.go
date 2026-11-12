// Package harness interprets native session-file lines for each supported
// coding-agent harness. Adapters are pure functions of individual JSON lines:
// they never open files, follow references, or execute content. The history
// package owns file access, pagination, and response assembly.
package harness

import (
	"encoding/json"
	"fmt"
	"path"
	"strings"
)

// Error codes reported by adapters.
const (
	CodeUnsupportedFormat = "unsupported_format"
	CodeInvalidHistory    = "invalid_history"
)

// FormatError is a fatal, file-level format problem.
type FormatError struct {
	Code string
	Msg  string
}

func (e *FormatError) Error() string { return e.Code + ": " + e.Msg }

func unsupported(format string, args ...any) error {
	return &FormatError{Code: CodeUnsupportedFormat, Msg: fmt.Sprintf(format, args...)}
}

func invalidHistory(format string, args ...any) error {
	return &FormatError{Code: CodeInvalidHistory, Msg: fmt.Sprintf(format, args...)}
}

// Identity is the native session identity established by a record.
type Identity struct {
	NativeID string
	Cwd      *string
	Started  *string
}

// Entry is the adapter's interpretation of one valid JSON object line.
type Entry struct {
	Type      *string
	Subtype   *string
	ID        *string
	ParentID  *string
	Timestamp *string

	// Identity is non-nil when the record establishes session identity.
	Identity *Identity
	// SetsTitle reports that the record sets (or, when Title is nil, clears)
	// the native session title. Higher TitlePriority values win.
	SetsTitle     bool
	Title         *string
	TitlePriority int
	// Unsupported names a dependency on history outside this file. A file
	// containing such a record cannot be returned as a standalone session.
	Unsupported string
}

// Adapter interprets one harness's native records.
type Adapter interface {
	// Harness returns the harness name.
	Harness() string
	// Candidate reports whether a source-relative slash path may hold a
	// session. Non-candidates are ignored during discovery.
	Candidate(rel string) bool
	// Kind classifies a candidate path, e.g. "session" or "subagent".
	Kind(rel string) string
	// FixedHeader reports whether line 1 must establish identity. When false,
	// the first record carrying identity establishes it.
	FixedHeader() bool
	// Parse interprets one complete line that is a valid JSON object. A
	// FormatError is fatal for the file.
	Parse(line int64, raw []byte) (Entry, error)
}

// ForHarness returns the adapter for a configured harness name.
func ForHarness(name string) Adapter {
	switch name {
	case "pi":
		return Pi{}
	case "claude":
		return Claude{}
	case "codex":
		return Codex{}
	}
	return nil
}

func isJSONL(rel string) bool { return strings.HasSuffix(path.Base(rel), ".jsonl") }

// fields decodes the top-level members of a JSON object without converting
// numbers, so callers can extract only what they need.
func fields(raw []byte) (map[string]json.RawMessage, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// str returns a JSON string member, or nil when absent or not a string.
func str(m map[string]json.RawMessage, key string) *string {
	raw, ok := m[key]
	if !ok || len(raw) == 0 || raw[0] != '"' {
		return nil
	}
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return nil
	}
	return &s
}

// present reports whether key exists with a non-null value.
func present(m map[string]json.RawMessage, key string) bool {
	raw, ok := m[key]
	return ok && strings.TrimSpace(string(raw)) != "null"
}

func object(m map[string]json.RawMessage, key string) map[string]json.RawMessage {
	raw, ok := m[key]
	if !ok {
		return nil
	}
	o, err := fields(raw)
	if err != nil {
		return nil
	}
	return o
}

func nonEmpty(s *string) bool { return s != nil && *s != "" }
