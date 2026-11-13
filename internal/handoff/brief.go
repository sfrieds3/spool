package handoff

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/sfrieds3/spool/internal/history"
	"github.com/sfrieds3/spool/internal/strictjson"
)

// Brief is validated authored input. Absent optional values are nil.
type Brief struct {
	Title       *string
	Goal        string
	Constraints []string
	Decisions   []string
	Progress    []string
	Outstanding []string
	Notes       []string
	// WorkspacePath is nil when the process working directory should be used.
	WorkspacePath *string
	Repository    *Repository
	Sessions      []BriefSession
}

// BriefSession is an authored session reference.
type BriefSession struct {
	Ref  string
	Note *string
}

type rawBrief struct {
	Version     json.RawMessage `json:"version"`
	Title       *string         `json:"title"`
	Goal        *string         `json:"goal"`
	Constraints []*string       `json:"constraints"`
	Decisions   []*string       `json:"decisions"`
	Progress    []*string       `json:"progress"`
	Outstanding []*string       `json:"outstanding"`
	Notes       []*string       `json:"notes"`
	Workspace   *rawWorkspace   `json:"workspace"`
	Sessions    []*rawBriefSess `json:"sessions"`
}

type rawWorkspace struct {
	Path       *string  `json:"path"`
	Repository *rawRepo `json:"repository"`
}

type rawRepo struct {
	Remote *string `json:"remote"`
	Branch *string `json:"branch"`
	Commit *string `json:"commit"`
}

type rawBriefSess struct {
	Ref  *string `json:"ref"`
	Note *string `json:"note"`
}

func invalidBrief(format string, args ...any) *history.Error {
	return errorf(history.CodeInvalidRequest, "invalid brief: "+format, args...)
}

// ParseBrief strictly validates a version-1 brief. Every failure is
// invalid_request.
func ParseBrief(data []byte) (*Brief, *history.Error) {
	if len(data) > MaxInputBytes {
		return nil, invalidBrief("input exceeds %d bytes", MaxInputBytes)
	}
	if !utf8.Valid(data) {
		return nil, invalidBrief("input is not valid UTF-8")
	}
	t := bytes.TrimSpace(data)
	if len(t) == 0 || t[0] != '{' {
		return nil, invalidBrief("input must be one JSON object")
	}
	if err := strictjson.CheckDuplicateKeys(t); err != nil {
		return nil, invalidBrief("%v", err)
	}
	var raw rawBrief
	if err := strictjson.Decode(t, &raw); err != nil {
		return nil, invalidBrief("%v", err)
	}
	if raw.Version == nil {
		return nil, invalidBrief("missing version")
	}
	if string(raw.Version) != "1" {
		return nil, invalidBrief("unsupported version %.32s", string(raw.Version))
	}
	b := &Brief{}
	var err *history.Error
	if raw.Goal == nil {
		return nil, invalidBrief("missing goal")
	}
	if b.Goal, err = text("goal", *raw.Goal, MaxItemBytes); err != nil {
		return nil, err
	}
	if b.Title, err = optText("title", raw.Title, MaxTitleBytes); err != nil {
		return nil, err
	}
	for _, l := range []struct {
		name string
		in   []*string
		out  *[]string
	}{
		{"constraints", raw.Constraints, &b.Constraints},
		{"decisions", raw.Decisions, &b.Decisions},
		{"progress", raw.Progress, &b.Progress},
		{"outstanding", raw.Outstanding, &b.Outstanding},
		{"notes", raw.Notes, &b.Notes},
	} {
		if *l.out, err = list(l.name, l.in); err != nil {
			return nil, err
		}
	}
	if w := raw.Workspace; w != nil {
		if b.WorkspacePath, err = optText("workspace.path", w.Path, MaxPathBytes); err != nil {
			return nil, err
		}
		if b.WorkspacePath != nil {
			if msg := checkAbsPath(*b.WorkspacePath); msg != "" {
				return nil, invalidBrief("workspace.path %s", msg)
			}
		}
		if r := w.Repository; r != nil {
			repo := &Repository{Provenance: ProvenanceAuthorReported}
			if repo.Remote, err = optText("workspace.repository.remote", r.Remote, MaxRepoFieldBytes); err != nil {
				return nil, err
			}
			if repo.Branch, err = optText("workspace.repository.branch", r.Branch, MaxRepoFieldBytes); err != nil {
				return nil, err
			}
			if repo.Commit, err = optText("workspace.repository.commit", r.Commit, MaxRepoFieldBytes); err != nil {
				return nil, err
			}
			if repo.Remote == nil && repo.Branch == nil && repo.Commit == nil {
				return nil, invalidBrief("workspace.repository must contain at least one value")
			}
			b.Repository = repo
		}
	}
	if len(raw.Sessions) > MaxSessions {
		return nil, invalidBrief("sessions has more than %d entries", MaxSessions)
	}
	seen := map[string]bool{}
	b.Sessions = []BriefSession{}
	for i, s := range raw.Sessions {
		if s == nil || s.Ref == nil {
			return nil, invalidBrief("sessions[%d].ref is required", i)
		}
		if *s.Ref == "" || len(*s.Ref) > history.MaxRefLength {
			return nil, invalidBrief("sessions[%d].ref must be a session reference of at most %d bytes", i, history.MaxRefLength)
		}
		if seen[*s.Ref] {
			return nil, invalidBrief("sessions[%d].ref duplicates an earlier reference", i)
		}
		seen[*s.Ref] = true
		note, err := optText("sessions[].note", s.Note, MaxNoteBytes)
		if err != nil {
			return nil, err
		}
		b.Sessions = append(b.Sessions, BriefSession{Ref: *s.Ref, Note: note})
	}
	return b, nil
}

func blank(s string) bool { return strings.TrimFunc(s, unicode.IsSpace) == "" }

// text validates a required authored string.
func text(name, s string, max int) (string, *history.Error) {
	switch {
	case blank(s):
		return "", invalidBrief("%s must not be empty or whitespace", name)
	case len(s) > max:
		return "", invalidBrief("%s exceeds %d bytes", name, max)
	case strings.ContainsRune(s, 0):
		return "", invalidBrief("%s contains NUL", name)
	}
	return s, nil
}

func optText(name string, s *string, max int) (*string, *history.Error) {
	if s == nil {
		return nil, nil
	}
	v, err := text(name, *s, max)
	if err != nil {
		return nil, err
	}
	return &v, nil
}

func list(name string, in []*string) ([]string, *history.Error) {
	if len(in) > MaxItems {
		return nil, invalidBrief("%s has more than %d items", name, MaxItems)
	}
	out := make([]string, 0, len(in))
	for _, item := range in {
		if item == nil {
			return nil, invalidBrief("%s items must be strings", name)
		}
		v, err := text(name+" item", *item, MaxItemBytes)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// checkAbsPath returns a problem description, or "" for an absolute clean
// path.
func checkAbsPath(p string) string {
	switch {
	case !filepath.IsAbs(p):
		return "must be an absolute path"
	case filepath.Clean(p) != p:
		return "must be a clean path"
	case len(p) > MaxPathBytes:
		return "is too long"
	}
	return ""
}
