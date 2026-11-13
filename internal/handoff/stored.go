package handoff

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/sfrieds3/spool/internal/config"
	"github.com/sfrieds3/spool/internal/history"
	"github.com/sfrieds3/spool/internal/strictjson"
)

var idPattern = regexp.MustCompile(`^[0-9]{8}t[0-9]{6}z-[0-9a-f]{8}$`)

const (
	idTimeLayout      = "20060102t150405z"
	createdAtLayout   = "2006-01-02T15:04:05Z"
	handoffFileSuffix = ".json"
)

// ValidID reports whether id is a well-formed handoff identifier.
func ValidID(id string) bool { return idPattern.MatchString(id) }

// newID returns an identifier for creation second t with a random suffix.
func newID(t time.Time, random io.Reader) (string, error) {
	var b [4]byte
	if _, err := io.ReadFull(random, b[:]); err != nil {
		return "", err
	}
	return t.UTC().Format(idTimeLayout) + "-" + hex.EncodeToString(b[:]), nil
}

// fileName returns the stored file name for id.
func fileName(id string) string { return id + handoffFileSuffix }

// idFromFileName returns the ID for a stored handoff file name, or "".
func idFromFileName(name string) string {
	id, ok := strings.CutSuffix(name, handoffFileSuffix)
	if !ok || !ValidID(id) {
		return ""
	}
	return id
}

// requiredKeys lists every key of a version-1 stored handoff by object path.
var requiredKeys = map[string][]string{
	"":                      {"version", "id", "created_at", "origin", "title", "goal", "constraints", "decisions", "progress", "outstanding", "notes", "workspace", "sessions"},
	"origin":                {"host", "spool_version"},
	"workspace":             {"path", "path_provenance", "repository"},
	"workspace.repository":  {"remote", "branch", "commit", "provenance"},
	"sessions[]":            {"ref", "note", "harness", "source_id", "kind", "native_id", "original_path", "cwd", "title", "started_at"},
	"sessions[].started_at": {"value", "provenance"},
}

// decodeStored strictly decodes and validates stored handoff bytes whose
// file name is expectedID+".json". The returned message never contains
// stored content.
func decodeStored(data []byte, expectedID string) (*Handoff, string) {
	if len(data) > MaxHandoffBytes {
		return nil, fmt.Sprintf("handoff file exceeds %d bytes", MaxHandoffBytes)
	}
	if !utf8.Valid(data) {
		return nil, "handoff file is not valid UTF-8"
	}
	keys, err := strictjson.Keys(data)
	if err != nil {
		return nil, "handoff file is not valid JSON: " + err.Error()
	}
	// Check the version before the strict decode so a future file is
	// reported as unsupported rather than as having unknown fields.
	var probe struct {
		Version json.RawMessage `json:"version"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return nil, "handoff file is not a JSON object"
	}
	if string(probe.Version) != "1" {
		return nil, "unsupported handoff version"
	}
	if err := strictjson.RequireKeys(keys, requiredKeys); err != nil {
		return nil, "handoff file is incomplete: " + err.Error()
	}
	var h Handoff
	if err := strictjson.Decode(data, &h); err != nil {
		return nil, "handoff file does not match version 1: " + err.Error()
	}
	if msg := validateStored(&h, expectedID); msg != "" {
		return nil, msg
	}
	return &h, ""
}

// validateStored applies every semantic rule to a decoded handoff.
func validateStored(h *Handoff, expectedID string) string {
	if h.Version != Version {
		return "unsupported handoff version"
	}
	if !ValidID(h.ID) || h.ID != expectedID {
		return "handoff id does not match its file name"
	}
	created, err := time.Parse(createdAtLayout, h.CreatedAt)
	if err != nil || created.Format(createdAtLayout) != h.CreatedAt {
		return "created_at is not an RFC 3339 UTC second"
	}
	if created.Format(idTimeLayout) != h.ID[:len(idTimeLayout)] {
		return "created_at does not match the handoff id"
	}
	if h.Origin.Host != nil && !validHost(*h.Origin.Host) {
		return "origin.host is invalid"
	}
	if h.Origin.SpoolVersion == "" || len(h.Origin.SpoolVersion) > MaxVersionBytes || hasControl(h.Origin.SpoolVersion) {
		return "origin.spool_version is invalid"
	}
	check := func(name, s string, max int) string {
		if _, err := text(name, s, max); err != nil {
			return name + " is invalid"
		}
		return ""
	}
	optCheck := func(name string, s *string, max int) string {
		if s == nil {
			return ""
		}
		return check(name, *s, max)
	}
	if m := check("goal", h.Goal, MaxItemBytes); m != "" {
		return m
	}
	if m := optCheck("title", h.Title, MaxTitleBytes); m != "" {
		return m
	}
	for name, l := range map[string][]string{"constraints": h.Constraints, "decisions": h.Decisions, "progress": h.Progress, "outstanding": h.Outstanding, "notes": h.Notes} {
		if l == nil {
			return name + " must be an array"
		}
		if len(l) > MaxItems {
			return name + " has too many items"
		}
		for _, item := range l {
			if m := check(name, item, MaxItemBytes); m != "" {
				return m
			}
		}
	}
	w := h.Workspace
	if w.PathProvenance != ProvenanceProcessCwd && w.PathProvenance != ProvenanceAuthorReported {
		return "workspace.path_provenance is invalid"
	}
	if m := check("workspace.path", w.Path, MaxPathBytes); m != "" {
		return m
	}
	if checkAbsPath(w.Path) != "" {
		return "workspace.path is not an absolute clean path"
	}
	if r := w.Repository; r != nil {
		if r.Provenance != ProvenanceAuthorReported {
			return "workspace.repository.provenance is invalid"
		}
		if r.Remote == nil && r.Branch == nil && r.Commit == nil {
			return "workspace.repository is empty"
		}
		for _, f := range []*string{r.Remote, r.Branch, r.Commit} {
			if m := optCheck("workspace.repository", f, MaxRepoFieldBytes); m != "" {
				return m
			}
		}
	}
	if h.Sessions == nil {
		return "sessions must be an array"
	}
	if len(h.Sessions) > MaxSessions {
		return "sessions has too many entries"
	}
	seen := map[string]bool{}
	for _, s := range h.Sessions {
		if m := validateSnapshot(s); m != "" {
			return m
		}
		if seen[s.Ref] {
			return "sessions contains a duplicate reference"
		}
		seen[s.Ref] = true
	}
	return ""
}

func validateSnapshot(s SessionSnapshot) string {
	ref, err := history.DecodeRef(s.Ref)
	if err != nil {
		return "sessions[].ref is invalid"
	}
	if ref.Harness != s.Harness || ref.SourceID != s.SourceID || ref.NativeID != s.NativeID {
		return "sessions[] metadata does not match its reference"
	}
	if !config.IsHarness(s.Harness) || !config.ValidSourceID(s.SourceID) {
		return "sessions[] harness or source_id is invalid"
	}
	if s.Kind == "" || len(s.Kind) > maxSnapshotWordBytes || hasControl(s.Kind) {
		return "sessions[].kind is invalid"
	}
	if s.NativeID == "" || len(s.NativeID) > maxSnapshotIDBytes {
		return "sessions[].native_id is invalid"
	}
	if s.OriginalPath == "" || len(s.OriginalPath) > maxSnapshotPathBytes || strings.ContainsRune(s.OriginalPath, 0) {
		return "sessions[].original_path is invalid"
	}
	for _, f := range []*string{s.Cwd, s.Title} {
		if f != nil && (len(*f) > maxSnapshotTextBytes || strings.ContainsRune(*f, 0)) {
			return "sessions[] recorded metadata is invalid"
		}
	}
	if s.Note != nil {
		if _, err := text("note", *s.Note, MaxNoteBytes); err != nil {
			return "sessions[].note is invalid"
		}
	}
	if t := s.StartedAt; t != nil {
		if t.Value == "" || len(t.Value) > maxSnapshotWordBytes || hasControl(t.Value) || t.Provenance != history.ProvenanceNativeRecord {
			return "sessions[].started_at is invalid"
		}
	}
	return ""
}

func hasControl(s string) bool {
	for _, r := range s {
		if needsEscape(r) {
			return true
		}
	}
	return false
}

func validHost(h string) bool {
	return h != "" && len(h) <= MaxHostBytes && utf8.ValidString(h) && !hasControl(h) && !strings.ContainsRune(h, '\n')
}
