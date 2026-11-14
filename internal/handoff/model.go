// Package handoff is Spool's transport-independent service for local
// continuation briefs. A handoff records content authored by an outgoing
// agent or user, provenance Spool observed when creating it, and snapshots of
// referenced source sessions. It never carries agent state, launches agents,
// or executes anything. The CLI (and later MCP) call Create, List, and Show
// and serialize the returned values.
package handoff

import (
	"fmt"

	"github.com/sfrieds3/spool/internal/history"
)

// Version is the brief and stored-handoff schema version.
const Version = 1

// ContentHandoffBrief labels show responses.
const ContentHandoffBrief = "handoff_brief"

// Error and diagnostic codes introduced by handoffs. Other codes are shared
// with the history service.
const (
	CodeHandoffNotFound  = "handoff_not_found"
	CodeHandoffInvalid   = "handoff_invalid"
	CodeStateUnavailable = "state_unavailable"

	DiagHandoffUnreadable   = "handoff_unreadable"
	DiagHandoffInvalid      = "handoff_invalid"
	DiagTempCleanupFailed   = "temp_cleanup_failed"
	DiagDirectorySyncFailed = "directory_sync_failed"
)

// Provenance values.
const (
	ProvenanceProcessCwd     = "process_cwd"
	ProvenanceAuthorReported = "author_reported"
)

// Documented limits. Text limits are UTF-8 byte lengths of decoded strings.
const (
	MaxInputBytes     = 256 << 10
	MaxTitleBytes     = 512
	MaxItemBytes      = 8 << 10
	MaxItems          = 100
	MaxPathBytes      = 4 << 10
	MaxRepoFieldBytes = 1 << 10
	MaxSessions       = 32
	MaxNoteBytes      = 2 << 10
	MaxHostBytes      = 255
	MaxVersionBytes   = 128
	// MaxHandoffBytes bounds the encoded stored handoff; creation rejects
	// larger results and reads stop beyond it.
	MaxHandoffBytes = 2 << 20
	// MaxGoalExcerptBytes bounds the goal in list summaries.
	MaxGoalExcerptBytes = 512

	maxSnapshotIDBytes   = 1 << 10
	maxSnapshotPathBytes = 8 << 10
	maxSnapshotTextBytes = 8 << 10
	maxSnapshotWordBytes = 64
)

// Handoff is the stored version-1 handoff. Every key is always present.
type Handoff struct {
	Version     int               `json:"version"`
	ID          string            `json:"id"`
	CreatedAt   string            `json:"created_at"`
	Origin      Origin            `json:"origin"`
	Title       *string           `json:"title"`
	Goal        string            `json:"goal"`
	Constraints []string          `json:"constraints"`
	Decisions   []string          `json:"decisions"`
	Progress    []string          `json:"progress"`
	Outstanding []string          `json:"outstanding"`
	Notes       []string          `json:"notes"`
	Workspace   Workspace         `json:"workspace"`
	Sessions    []SessionSnapshot `json:"sessions"`
}

// Origin is provenance Spool observed at creation.
type Origin struct {
	Host         *string `json:"host"`
	SpoolVersion string  `json:"spool_version"`
}

// Workspace records the workspace path and any author-reported repository
// details. Neither is verified against the filesystem.
type Workspace struct {
	Path           string      `json:"path"`
	PathProvenance string      `json:"path_provenance"`
	Repository     *Repository `json:"repository"`
}

// Repository holds author-reported, unverified repository details.
type Repository struct {
	Remote     *string `json:"remote"`
	Branch     *string `json:"branch"`
	Commit     *string `json:"commit"`
	Provenance string  `json:"provenance"`
}

// SessionSnapshot is a referenced session's metadata at creation. Title, cwd,
// and started_at are recorded by the session, not observed by Spool.
type SessionSnapshot struct {
	Ref          string             `json:"ref"`
	Note         *string            `json:"note"`
	Harness      string             `json:"harness"`
	SourceID     string             `json:"source_id"`
	Kind         string             `json:"kind"`
	NativeID     string             `json:"native_id"`
	OriginalPath string             `json:"original_path"`
	Cwd          *string            `json:"cwd"`
	Title        *string            `json:"title"`
	StartedAt    *history.Timestamp `json:"started_at"`
}

// Summary is one row of a handoff list.
type Summary struct {
	ID               string  `json:"id"`
	CreatedAt        string  `json:"created_at"`
	Title            *string `json:"title"`
	Goal             string  `json:"goal"`
	GoalTruncated    bool    `json:"goal_truncated"`
	Origin           Origin  `json:"origin"`
	WorkspacePath    string  `json:"workspace_path"`
	OutstandingCount int     `json:"outstanding_count"`
	SessionCount     int     `json:"session_count"`
}

// CreateResponse is a successful create result. Warnings report problems
// after the handoff was published; the handoff exists regardless.
type CreateResponse struct {
	Version  int                  `json:"version"`
	OK       bool                 `json:"ok"`
	Handoff  *Handoff             `json:"handoff"`
	Warnings []history.Diagnostic `json:"warnings"`
}

// ShowResponse is a successful show result.
type ShowResponse struct {
	Version int      `json:"version"`
	OK      bool     `json:"ok"`
	Content string   `json:"content"`
	Handoff *Handoff `json:"handoff"`
}

// ListResponse is a successful list result.
type ListResponse struct {
	Version  int       `json:"version"`
	OK       bool      `json:"ok"`
	Handoffs []Summary `json:"handoffs"`
	history.Page
}

func errorf(code, format string, args ...any) *history.Error {
	return &history.Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

func ptr[T any](v T) *T { return &v }
