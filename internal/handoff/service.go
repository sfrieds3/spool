package handoff

import (
	"context"
	"crypto/rand"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"
	"unicode/utf8"

	"github.com/sfrieds3/spool/internal/config"
	"github.com/sfrieds3/spool/internal/history"
)

// maxLinkAttempts bounds identifier collisions during publication.
const maxLinkAttempts = 8

// Options supplies process dependencies. Zero values select the real
// process clock, host name, working directory, and random source.
type Options struct {
	// SpoolVersion is recorded as origin.spool_version.
	SpoolVersion string
	Now          func() time.Time
	Hostname     func() (string, error)
	Getwd        func() (string, error)
	Random       io.Reader
}

// Service creates, lists, and shows handoffs in one handoff directory.
type Service struct {
	history *history.Service
	sources []config.Source
	dir     string
	opts    Options
	ops     fileOps
}

// NewService returns a service storing handoffs under stateDir/handoffs and
// resolving session references through hist.
func NewService(cfg *config.Config, hist *history.Service, stateDir string, opts Options) *Service {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Hostname == nil {
		opts.Hostname = os.Hostname
	}
	if opts.Getwd == nil {
		opts.Getwd = os.Getwd
	}
	if opts.Random == nil {
		opts.Random = rand.Reader
	}
	if opts.SpoolVersion == "" {
		opts.SpoolVersion = "dev"
	}
	return &Service{history: hist, sources: cfg.Sources, dir: filepath.Join(stateDir, "handoffs"), opts: opts, ops: defaultOps}
}

// Dir returns the handoff directory.
func (s *Service) Dir() string { return s.dir }

// Create validates brief input, snapshots referenced sessions, and publishes
// a new handoff. Nothing is written unless validation and every reference
// succeed. Once published, later cleanup problems are reported as warnings.
func (s *Service) Create(ctx context.Context, input []byte) (*CreateResponse, *history.Error) {
	if err := ctx.Err(); err != nil {
		return nil, errorf(history.CodeCanceled, "operation canceled: %v", err)
	}
	brief, herr := ParseBrief(input)
	if herr != nil {
		return nil, herr
	}
	h := &Handoff{
		Version: Version, Title: brief.Title, Goal: brief.Goal,
		Constraints: brief.Constraints, Decisions: brief.Decisions, Progress: brief.Progress,
		Outstanding: brief.Outstanding, Notes: brief.Notes,
		Origin:   Origin{SpoolVersion: s.opts.SpoolVersion},
		Sessions: make([]SessionSnapshot, 0, len(brief.Sessions)),
	}
	for _, bs := range brief.Sessions {
		if err := ctx.Err(); err != nil {
			return nil, errorf(history.CodeCanceled, "operation canceled: %v", err)
		}
		sess, err := s.history.Describe(ctx, bs.Ref)
		if err != nil {
			return nil, err
		}
		if len(sess.OriginalPath) > maxSnapshotPathBytes {
			return nil, errorf(history.CodeInvalidRequest, "referenced session path exceeds %d bytes", maxSnapshotPathBytes)
		}
		h.Sessions = append(h.Sessions, SessionSnapshot{
			Ref: bs.Ref, Note: bs.Note, Harness: sess.Harness, SourceID: sess.SourceID, Kind: sess.Kind,
			NativeID: sess.NativeID, OriginalPath: sess.OriginalPath, Cwd: sess.Cwd, Title: sess.Title, StartedAt: sess.StartedAt,
		})
	}
	if brief.WorkspacePath != nil {
		h.Workspace = Workspace{Path: *brief.WorkspacePath, PathProvenance: ProvenanceAuthorReported}
	} else {
		wd, err := s.opts.Getwd()
		if err != nil {
			return nil, errorf(history.CodeInternal, "cannot determine the working directory: %s", errText(err))
		}
		if msg := checkAbsPath(wd); msg != "" || !utf8.ValidString(wd) {
			return nil, errorf(history.CodeInvalidRequest, "working directory cannot be recorded; supply workspace.path")
		}
		h.Workspace = Workspace{Path: wd, PathProvenance: ProvenanceProcessCwd}
	}
	h.Workspace.Repository = brief.Repository
	if host, err := s.opts.Hostname(); err == nil && validHost(host) {
		h.Origin.Host = &host
	}
	created := s.opts.Now().UTC().Truncate(time.Second)
	h.CreatedAt = created.Format(createdAtLayout)

	if err := checkExclusion(s.dir, s.sources); err != nil {
		return nil, err
	}
	var data []byte
	if err := s.assignID(h, created, &data); err != nil {
		return nil, err
	}

	if err := ctx.Err(); err != nil {
		return nil, errorf(history.CodeCanceled, "operation canceled: %v", err)
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return nil, stateErr("handoff directory %s cannot be created: %s", s.dir, errText(err))
	}
	root, err := openHandoffDir(s.dir)
	if err != nil {
		if errors.Is(err, errNoDir) {
			return nil, stateErr("handoff directory %s disappeared", s.dir)
		}
		return nil, err.(*history.Error)
	}
	defer root.Close()

	for attempt := 1; ; attempt++ {
		published, err := s.publish(ctx, root, h.ID, data)
		if err != nil {
			return nil, err
		}
		if published != nil {
			return &CreateResponse{Version: history.ResponseVersion, OK: true, Handoff: h, Warnings: published}, nil
		}
		if attempt == maxLinkAttempts {
			return nil, stateErr("could not allocate a unique handoff id after %d attempts", maxLinkAttempts)
		}
		if err := s.assignID(h, created, &data); err != nil {
			return nil, err
		}
	}
}

// assignID gives h a new identifier for its creation second, encodes it,
// and checks the result against the stored-handoff rules and size limit.
func (s *Service) assignID(h *Handoff, created time.Time, data *[]byte) *history.Error {
	id, err := newID(created, s.opts.Random)
	if err != nil {
		return errorf(history.CodeInternal, "cannot generate a handoff id: %v", err)
	}
	h.ID = id
	out, err := history.Marshal(h)
	if err != nil {
		return errorf(history.CodeInternal, "cannot encode handoff: %v", err)
	}
	out = append(out, '\n')
	if len(out) > MaxHandoffBytes {
		return errorf(history.CodeInvalidRequest, "encoded handoff exceeds %d bytes; shorten the brief or reference fewer sessions", MaxHandoffBytes)
	}
	if msg := validateStored(h, id); msg != "" {
		return errorf(history.CodeInternal, "constructed handoff is invalid: %s", msg)
	}
	*data = out
	return nil
}

// publish writes data to a temporary file and links it to the handoff's
// final name. It returns (nil, nil) when the name is already taken, a
// non-nil warning list (possibly empty) once published, or an error with
// nothing published.
func (s *Service) publish(ctx context.Context, root *os.Root, id string, data []byte) ([]history.Diagnostic, *history.Error) {
	suffix, err := randomHex(s.opts.Random)
	if err != nil {
		return nil, errorf(history.CodeInternal, "cannot generate a temporary name: %v", err)
	}
	tmp := ".tmp-" + id + "-" + suffix
	f, err := s.ops.create(root, tmp)
	if err != nil {
		return nil, stateErr("cannot create a file in %s: %s", s.dir, errText(err))
	}
	fail := func(what string, err error) ([]history.Diagnostic, *history.Error) {
		_ = s.ops.remove(root, tmp)
		return nil, stateErr("cannot %s handoff in %s: %s", what, s.dir, errText(err))
	}
	n, err := f.Write(data)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	if err != nil {
		f.Close()
		return fail("write", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return fail("sync", err)
	}
	if err := f.Close(); err != nil {
		return fail("close", err)
	}
	if err := ctx.Err(); err != nil {
		_ = s.ops.remove(root, tmp)
		return nil, errorf(history.CodeCanceled, "operation canceled: %v", err)
	}
	if err := s.ops.link(root, tmp, fileName(id)); err != nil {
		if errors.Is(err, fs.ErrExist) {
			_ = s.ops.remove(root, tmp)
			return nil, nil
		}
		return fail("publish", err)
	}
	warnings := []history.Diagnostic{}
	name := fileName(id)
	if err := s.ops.remove(root, tmp); err != nil {
		warnings = append(warnings, history.Diagnostic{Code: DiagTempCleanupFailed, Message: "handoff was published but its temporary file could not be removed: " + errText(err), Path: &tmp})
	}
	if err := s.ops.syncDir(root); err != nil {
		warnings = append(warnings, history.Diagnostic{Code: DiagDirectorySyncFailed, Message: "handoff was published but the directory could not be synced: " + errText(err), Path: &name})
	}
	return warnings, nil
}

// Show returns one stored handoff.
func (s *Service) Show(ctx context.Context, id string) (*ShowResponse, *history.Error) {
	if !ValidID(id) {
		return nil, errorf(history.CodeInvalidRequest, "invalid handoff id")
	}
	if err := ctx.Err(); err != nil {
		return nil, errorf(history.CodeCanceled, "operation canceled: %v", err)
	}
	notFound := errorf(CodeHandoffNotFound, "no handoff %s", id)
	root, err := openHandoffDir(s.dir)
	if err != nil {
		if errors.Is(err, errNoDir) {
			return nil, notFound
		}
		return nil, err.(*history.Error)
	}
	defer root.Close()
	h, msg, rerr := readStored(root, id)
	switch {
	case errors.Is(rerr, fs.ErrNotExist):
		return nil, notFound
	case rerr != nil:
		return nil, stateErr("handoff %s cannot be read: %s", id, errText(rerr))
	case msg != "":
		return nil, errorf(CodeHandoffInvalid, "handoff %s is invalid: %s", id, msg)
	}
	return &ShowResponse{Version: history.ResponseVersion, OK: true, Content: ContentHandoffBrief, Handoff: h}, nil
}

// ListRequest selects a page of handoffs.
type ListRequest struct {
	Offset int64
	Limit  int
}

// List returns handoff summaries in descending identifier order. Damaged
// entries are reported as diagnostics at their row position.
func (s *Service) List(ctx context.Context, req ListRequest) (*ListResponse, *history.Error) {
	if req.Offset < 0 {
		return nil, errorf(history.CodeInvalidRequest, "offset must be nonnegative")
	}
	if req.Limit < 1 || req.Limit > history.MaxLimit {
		return nil, errorf(history.CodeInvalidRequest, "limit must be between 1 and %d", history.MaxLimit)
	}
	resp := &ListResponse{Version: history.ResponseVersion, OK: true, Handoffs: []Summary{}}
	resp.Offset, resp.Limit = req.Offset, req.Limit
	resp.Diagnostics = []history.Diagnostic{}
	root, err := openHandoffDir(s.dir)
	if err != nil {
		if errors.Is(err, errNoDir) {
			resp.EOF = true
			return resp, nil
		}
		return nil, err.(*history.Error)
	}
	defer root.Close()
	d, err := root.Open(".")
	if err != nil {
		return nil, stateErr("handoff directory %s cannot be read: %s", s.dir, errText(err))
	}
	names, err := d.Readdirnames(-1)
	d.Close()
	if err != nil {
		return nil, stateErr("handoff directory %s cannot be read: %s", s.dir, errText(err))
	}
	var ids []string
	for _, n := range names {
		if id := idFromFileName(n); id != "" {
			ids = append(ids, id)
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(ids)))

	used := int64(listEnvelopeReserve)
	next := req.Offset
	for i := req.Offset; i < int64(len(ids)) && i < req.Offset+int64(req.Limit); i++ {
		if err := ctx.Err(); err != nil {
			return nil, errorf(history.CodeCanceled, "operation canceled: %v", err)
		}
		id := ids[i]
		h, msg, rerr := readStored(root, id)
		if rerr != nil || msg != "" {
			if len(resp.Diagnostics) >= history.MaxDiagnostics {
				break
			}
			code, text := DiagHandoffInvalid, "handoff is invalid: "+msg
			if rerr != nil {
				if errors.Is(rerr, fs.ErrNotExist) {
					// Removed since the directory was read.
					next = i + 1
					continue
				}
				code, text = DiagHandoffUnreadable, "handoff cannot be read: "+errText(rerr)
			}
			resp.Diagnostics = append(resp.Diagnostics, history.Diagnostic{Code: code, Message: text, Path: ptr(fileName(id))})
			next = i + 1
			continue
		}
		sum := summarize(h)
		b, err := history.Marshal(sum)
		if err != nil {
			return nil, errorf(history.CodeInternal, "cannot encode summary: %v", err)
		}
		if used+int64(len(b))+1 > history.MaxResponseBytes {
			break
		}
		used += int64(len(b)) + 1
		resp.Handoffs = append(resp.Handoffs, sum)
		next = i + 1
	}
	if next < int64(len(ids)) {
		resp.NextOffset = ptr(next)
	} else {
		resp.EOF = true
	}
	resp.Partial = len(resp.Diagnostics) > 0
	return resp, nil
}

// listEnvelopeReserve covers the list envelope and a full diagnostic list.
const listEnvelopeReserve = 64 << 10

func summarize(h *Handoff) Summary {
	goal, truncated := h.Goal, false
	if len(goal) > MaxGoalExcerptBytes {
		cut := MaxGoalExcerptBytes
		for cut > 0 && !utf8.RuneStart(goal[cut]) {
			cut--
		}
		goal, truncated = goal[:cut], true
	}
	return Summary{
		ID: h.ID, CreatedAt: h.CreatedAt, Title: h.Title, Goal: goal, GoalTruncated: truncated,
		Origin: h.Origin, WorkspacePath: h.Workspace.Path,
		OutstandingCount: len(h.Outstanding), SessionCount: len(h.Sessions),
	}
}

// Marshal serializes a response as Spool emits it.
func Marshal(v any) ([]byte, error) { return history.Marshal(v) }
