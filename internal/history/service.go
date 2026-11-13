package history

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/sfrieds3/spool/internal/config"
	"github.com/sfrieds3/spool/internal/harness"
)

// Documented limits.
const (
	DefaultLimit = 50
	MaxLimit     = 500
	// MaxLineBytes is the largest native line Spool returns.
	MaxLineBytes = 4 << 20
	// MaxResponseBytes bounds every serialized list or read response.
	MaxResponseBytes = 8 << 20
	// MetadataScanBytes bounds how much of each file listing inspects.
	MetadataScanBytes = 1 << 20
	// MaxDiagnostics bounds diagnostics per response; the rest are counted.
	MaxDiagnostics = 64
	// maxMetadataField bounds title and cwd values in catalog rows.
	maxMetadataField     = 8 << 10
	maxDiagnosticMessage = 512
	// reserves keep envelope, session metadata, and diagnostics within the
	// response limit regardless of record sizes.
	diagnosticReserve = MaxDiagnostics * (maxDiagnosticMessage + maxRelPathBytes + 256)
	sessionReserve    = 64 << 10
	envelopeReserve   = 8 << 10
)

// Service implements list and read over configured local sources.
type Service struct {
	sources []config.Source
}

// NewService returns a service for the configured sources.
func NewService(cfg *config.Config) *Service {
	return &Service{sources: cfg.Sources}
}

// Marshal serializes a response exactly as Spool emits it: compact, without
// HTML escaping, preserving native JSON values byte-for-byte apart from
// insignificant whitespace.
func Marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

func checkPage(offset int64, limit int) *Error {
	if offset < 0 {
		return newError(CodeInvalidRequest, "offset must be nonnegative")
	}
	if limit < 1 || limit > MaxLimit {
		return newError(CodeInvalidRequest, "limit must be between 1 and %d", MaxLimit)
	}
	return nil
}

func ctxError(err error) *Error {
	return newError(CodeCanceled, "operation canceled: %v", err)
}

// ListRequest selects a catalog page.
type ListRequest struct {
	// Harness filters sources; empty means all harnesses.
	Harness string
	Offset  int64
	Limit   int
}

type row struct {
	src   int
	path  string
	issue *walkIssue
}

type openedSource struct {
	cfg    config.Source
	root   *os.Root
	status SourceStatus
}

// List returns one catalog page. Rows are candidate files (and skipped
// filesystem entries) ordered by source ID, then bytewise by relative path.
func (s *Service) List(ctx context.Context, req ListRequest) (*ListResponse, *Error) {
	if req.Harness != "" && !config.IsHarness(req.Harness) {
		return nil, newError(CodeInvalidRequest, "unknown harness %q", req.Harness)
	}
	if err := checkPage(req.Offset, req.Limit); err != nil {
		return nil, err
	}
	resp := &ListResponse{Version: ResponseVersion, OK: true, Sources: []SourceStatus{}, Sessions: []Session{}}
	resp.Offset, resp.Limit = req.Offset, req.Limit
	diags := &diagnostics{max: MaxDiagnostics}

	selected, opened, rows, catalogErr := s.catalog(ctx, req.Harness)
	defer closeOpened(opened)
	if catalogErr != nil {
		return nil, catalogErr
	}
	for _, o := range opened {
		resp.Sources = append(resp.Sources, o.status)
		if o.status.State == SourceError {
			diags.add(DiagSourceUnavailable, "source root cannot be read: "+*o.status.Error, ptr(o.cfg.ID), nil, nil)
		}
	}
	if len(selected) > 0 && allSourcesFailed(opened) {
		e := newError(CodeSourcesUnavailable, "all requested sources failed")
		e.Sources = resp.Sources
		return nil, e
	}

	used := int64(envelopeReserve + diagnosticReserve)
	for _, st := range resp.Sources {
		b, _ := Marshal(st)
		used += int64(len(b)) + 1
	}
	next := req.Offset
	for i := req.Offset; i < int64(len(rows)) && i < req.Offset+int64(req.Limit); i++ {
		if err := ctx.Err(); err != nil {
			return nil, ctxError(err)
		}
		r := rows[i]
		o := opened[r.src]
		if r.issue != nil {
			var p *string
			if r.issue.path != "" {
				p = ptr(r.issue.path)
			}
			diags.add(r.issue.code, r.issue.msg, ptr(o.cfg.ID), p, nil)
			next = i + 1
			continue
		}
		sess, diag := describe(o.cfg, o.root, r.path)
		if diag != nil {
			diags.add(diag.Code, diag.Message, ptr(o.cfg.ID), ptr(r.path), diag.Line)
			next = i + 1
			continue
		}
		b, err := Marshal(sess)
		if err != nil {
			return nil, newError(CodeInternal, "cannot encode session: %v", err)
		}
		if used+int64(len(b))+1 > MaxResponseBytes {
			break
		}
		used += int64(len(b)) + 1
		resp.Sessions = append(resp.Sessions, *sess)
		next = i + 1
	}
	if next < int64(len(rows)) {
		resp.NextOffset = ptr(next)
	} else {
		resp.EOF = true
	}
	resp.Diagnostics = diags.list
	if resp.Diagnostics == nil {
		resp.Diagnostics = []Diagnostic{}
	}
	resp.OmittedDiagnostics = diags.omitted
	resp.Partial = diags.count() > 0
	return resp, nil
}

// catalog selects source roots and discovers deterministic candidate/issue rows
// once. Both List and Search own the returned handles until closeOpened.
func (s *Service) catalog(ctx context.Context, filter string) ([]config.Source, []*openedSource, []row, *Error) {
	selected := make([]config.Source, 0, len(s.sources))
	for _, src := range s.sources {
		if src.Enabled && (filter == "" || src.Harness == filter) {
			selected = append(selected, src)
		}
	}
	sortSources(selected)
	var opened []*openedSource
	var rows []row
	for _, src := range selected {
		if err := ctx.Err(); err != nil {
			return selected, opened, nil, ctxError(err)
		}
		o := &openedSource{cfg: src, status: SourceStatus{ID: src.ID, Harness: src.Harness, Root: src.Root, State: SourceReady}}
		opened = append(opened, o)
		root, err := openSourceRoot(src.Root)
		if err != nil {
			if src.Default && errors.Is(err, fs.ErrNotExist) {
				o.status.State = SourceAbsent
				continue
			}
			o.status.State, o.status.Error = SourceError, ptr(errText(err))
			continue
		}
		o.root = root
		files, issues, err := discover(ctx, root, harness.ForHarness(src.Harness).Candidate)
		if err != nil {
			if cerr := ctx.Err(); cerr != nil {
				return selected, opened, nil, ctxError(cerr)
			}
			o.status.State, o.status.Error = SourceError, ptr(errText(err))
			continue
		}
		rows = append(rows, mergeRows(len(opened)-1, files, issues)...)
	}
	return selected, opened, rows, nil
}

func closeOpened(opened []*openedSource) {
	for _, o := range opened {
		if o.root != nil {
			_ = o.root.Close()
		}
	}
}

func allSourcesFailed(opened []*openedSource) bool {
	if len(opened) == 0 {
		return false
	}
	for _, o := range opened {
		if o.status.State != SourceError {
			return false
		}
	}
	return true
}

func sortSources(s []config.Source) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j].ID < s[j-1].ID; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// mergeRows interleaves sorted files and issues by path so each skipped
// entry is reported exactly once, at a stable catalog position.
func mergeRows(src int, files []string, issues []walkIssue) []row {
	rows := make([]row, 0, len(files)+len(issues))
	for _, f := range files {
		rows = append(rows, row{src: src, path: f})
	}
	for i := range issues {
		rows = append(rows, row{src: src, path: issues[i].path, issue: &issues[i]})
	}
	for i := 1; i < len(rows); i++ {
		for j := i; j > 0 && rows[j].path < rows[j-1].path; j-- {
			rows[j], rows[j-1] = rows[j-1], rows[j]
		}
	}
	return rows
}

// meta accumulates native metadata while scanning records.
type meta struct {
	identity      *harness.Identity
	title         *string
	titlePriority int
	titleSet      bool
	unsupported   string
}

func (m *meta) observe(e harness.Entry) {
	if e.Identity != nil {
		if m.identity == nil {
			id := *e.Identity
			m.identity = &id
		} else if m.identity.NativeID == e.Identity.NativeID {
			// Fill fields the identifying record did not carry.
			if m.identity.Cwd == nil {
				m.identity.Cwd = e.Identity.Cwd
			}
			if m.identity.Started == nil {
				m.identity.Started = e.Identity.Started
			}
		}
	}
	if e.SetsTitle && (!m.titleSet || e.TitlePriority >= m.titlePriority) {
		m.title, m.titlePriority, m.titleSet = e.Title, e.TitlePriority, true
	}
	if m.unsupported == "" && e.Unsupported != "" {
		m.unsupported = e.Unsupported
	}
}

func fileDiag(code, msg string, line *int64) *Diagnostic {
	return &Diagnostic{Code: code, Message: msg, Line: line}
}

func formatDiag(err error, line int64) *Diagnostic {
	var fe *harness.FormatError
	if errors.As(err, &fe) {
		return fileDiag(fe.Code, fe.Msg, ptr(line))
	}
	return fileDiag(DiagInvalidHistory, err.Error(), ptr(line))
}

// describe performs the bounded metadata scan for one candidate file.
func describe(src config.Source, root *os.Root, rel string) (*Session, *Diagnostic) {
	f, info, err := openRegular(root, rel)
	if err != nil {
		switch {
		case errors.Is(err, errSymlink):
			return nil, fileDiag(DiagSymlinkSkipped, "candidate path traverses a symbolic link", nil)
		case errors.Is(err, errNotRegular):
			return nil, fileDiag(DiagNotRegularFile, "candidate is not a regular file", nil)
		}
		return nil, fileDiag(DiagFileUnreadable, "candidate cannot be opened: "+errText(err), nil)
	}
	defer f.Close()
	adapter := harness.ForHarness(src.Harness)
	r := newLineReader(f, MetadataScanBytes)
	var m meta
	complete := false
	for r.Consumed() < MetadataScanBytes {
		l, err := r.Next()
		if err != nil {
			if isEOF(err) {
				complete = true
				break
			}
			return nil, fileDiag(DiagFileUnreadable, "candidate cannot be read: "+errText(err), nil)
		}
		header := adapter.FixedHeader() && l.Number == 1
		if l.Oversized || !l.Terminated {
			if header {
				if l.Oversized {
					return nil, fileDiag(DiagMetadataOversized, "session header exceeds the metadata scan budget", ptr(l.Number))
				}
				return nil, fileDiag(DiagIdentityUnavailable, "session header is incomplete", ptr(l.Number))
			}
			continue
		}
		obj, ok := jsonObject(l.Data)
		if !ok {
			if header {
				return nil, fileDiag(DiagInvalidHistory, "session header is not a JSON object", ptr(l.Number))
			}
			continue
		}
		e, err := adapter.Parse(l.Number, obj)
		if err != nil {
			return nil, formatDiag(err, l.Number)
		}
		m.observe(e)
		if m.unsupported != "" {
			return nil, fileDiag(DiagUnsupportedFormat, m.unsupported, ptr(l.Number))
		}
	}
	if m.identity == nil {
		if complete {
			return nil, fileDiag(DiagIdentityUnavailable, "no native session identity is recorded", nil)
		}
		return nil, fileDiag(DiagMetadataOversized, "no native session identity within the metadata scan budget", nil)
	}
	return buildSession(src, rel, info, &m, complete, false)
}

// buildSession assembles catalog metadata. When lenient, oversized title or
// cwd values become null with a warning diagnostic instead of failing.
func buildSession(src config.Source, rel string, info os.FileInfo, m *meta, complete, lenient bool) (*Session, *Diagnostic) {
	id := m.identity.NativeID
	if len(id) > maxNativeIDBytes || !validUTF8(id) {
		return nil, fileDiag(DiagIdentityUnavailable, "native session identity is too long or not valid UTF-8", nil)
	}
	cwd, title := m.identity.Cwd, m.title
	var warning *Diagnostic
	if (cwd != nil && len(*cwd) > maxMetadataField) || (title != nil && len(*title) > maxMetadataField) {
		warning = fileDiag(DiagMetadataOversized, "title or cwd exceeds the session metadata limit and is null; the native records retain it", nil)
		if !lenient {
			return nil, warning
		}
		if cwd != nil && len(*cwd) > maxMetadataField {
			cwd = nil
		}
		if title != nil && len(*title) > maxMetadataField {
			title = nil
		}
	}
	sess := &Session{
		Ref:              Ref{SourceID: src.ID, Harness: src.Harness, Path: rel, NativeID: id}.Encode(),
		SourceID:         src.ID,
		Harness:          src.Harness,
		Kind:             harness.ForHarness(src.Harness).Kind(rel),
		NativeID:         id,
		Path:             rel,
		OriginalPath:     filepath.Join(src.Root, filepath.FromSlash(rel)),
		Cwd:              cwd,
		Title:            title,
		FileModifiedAt:   &Timestamp{Value: info.ModTime().UTC().Format(time.RFC3339Nano), Provenance: ProvenanceFilesystemMtime},
		SizeBytes:        info.Size(),
		MetadataComplete: complete,
	}
	if m.identity.Started != nil {
		sess.StartedAt = &Timestamp{Value: *m.identity.Started, Provenance: ProvenanceNativeRecord}
	}
	return sess, warning
}
