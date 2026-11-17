package history

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/sfrieds3/spool/internal/config"
	"github.com/sfrieds3/spool/internal/harness"
)

// Filesystem issue paths may contain control bytes that expand sixfold in
// compact JSON. Reserve their worst-case encoding without changing List/Read.
const searchDiagnosticReserve = MaxDiagnostics * (6*(maxDiagnosticMessage+maxRelPathBytes+64) + 256)

type searchDiagnostics struct{ diagnostics }

func (d *searchDiagnostics) add(code, msg string, source, path *string, line *int64) {
	// Issue paths longer than a referenceable path cannot identify a native
	// record; omit rather than retaining unbounded filesystem names.
	if path != nil && len(*path) > maxRelPathBytes {
		path = nil
	}
	d.diagnostics.add(code, msg, source, path, line)
}

// Search scans confined native histories on demand. It never persists text.
func (s *Service) Search(ctx context.Context, req SearchRequest) (*SearchResponse, *Error) {
	if !utf8.ValidString(req.Query) || strings.TrimSpace(req.Query) == "" || len(req.Query) > MaxSearchQueryBytes {
		return nil, newError(CodeInvalidRequest, "query must be nonblank valid UTF-8 and at most 256 bytes")
	}
	if req.Harness != "" && !config.IsHarness(req.Harness) {
		return nil, newError(CodeInvalidRequest, "unknown harness")
	}
	if req.Limit < 1 || req.Limit > MaxLimit {
		return nil, newError(CodeInvalidRequest, "limit must be between 1 and %d", MaxLimit)
	}
	if len(req.Cursor) > MaxSearchCursorBytes || (req.Cursor != "" && !strings.HasPrefix(req.Cursor, "q1.")) {
		return nil, newError(CodeInvalidRequest, "invalid search cursor")
	}
	resp := &SearchResponse{Version: ResponseVersion, OK: true, Content: ContentUntrustedHistory, View: ViewNativeFileOrder, Sources: []SourceStatus{}, Hits: []SearchHit{}, Limit: req.Limit, Diagnostics: []Diagnostic{}}
	diags := &searchDiagnostics{diagnostics{max: MaxDiagnostics}}
	selected, opened, rows, catalogErr := s.catalog(ctx, req.Harness)
	defer closeOpened(opened)
	if catalogErr != nil {
		return nil, catalogErr
	}
	for _, o := range opened {
		resp.Sources = append(resp.Sources, o.status)
		if o.status.State == SourceError {
			diags.add(DiagSourceUnavailable, "source root cannot be read", ptr(o.cfg.ID), nil, nil)
		}
	}
	if len(selected) > 0 && allSourcesFailed(opened) {
		return nil, &Error{Code: CodeSourcesUnavailable, Message: "all requested sources failed", Sources: resp.Sources}
	}
	fp := searchFingerprint(req, selected, resp.Sources)
	position, cerr := decodeSearchCursor(req.Cursor, fp, len(rows))
	if cerr != nil {
		return nil, cerr
	}
	// Reserve fixed space for the bounded diagnostic collection and cursor;
	// charge source states and every hit using actual compact JSON encoding.
	used := int64(envelopeReserve + searchDiagnosticReserve + 4096)
	for _, st := range resp.Sources {
		b, err := Marshal(st)
		if err != nil {
			return nil, newError(CodeInternal, "cannot encode source status")
		}
		used += int64(len(b)) + 1
	}
	if used >= MaxResponseBytes {
		return nil, newError(CodeInternal, "search source metadata exceeds response limit")
	}
	emptyAllowance := MaxResponseBytes - used
	matcher := newLiteralMatcher(req.Query, req.CaseSensitive)
	var bytesRead int64
	var rowsUsed int
	for position.Row < len(rows) {
		if err := ctx.Err(); err != nil {
			return nil, ctxError(err)
		}
		if rowsUsed >= MaxSearchRows {
			diags.add(DiagScanLimit, "candidate row limit reached", nil, nil, nil)
			break
		}
		r := rows[position.Row]
		o := opened[r.src]
		if r.issue != nil {
			var path *string
			if r.issue.path != "" {
				path = ptr(r.issue.path)
			}
			diags.add(r.issue.code, r.issue.msg, ptr(o.cfg.ID), path, nil)
			position = searchPosition{Row: position.Row + 1}
			rowsUsed++
			continue
		}
		file, info, err := openRegular(o.root, r.path)
		if err != nil {
			code := DiagFileUnreadable
			switch {
			case errors.Is(err, errSymlink):
				code = DiagSymlinkSkipped
			case errors.Is(err, errNotRegular):
				code = DiagNotRegularFile
			}
			diags.add(code, "candidate cannot be opened as a confined regular file", ptr(o.cfg.ID), ptr(r.path), nil)
			position = searchPosition{Row: position.Row + 1}
			rowsUsed++
			continue
		}
		if info.Size() > MaxSearchFileBytes {
			_ = file.Close()
			diags.add(DiagSessionTooLarge, "session exceeds the 64 MiB validation limit", ptr(o.cfg.ID), ptr(r.path), nil)
			position = searchPosition{Row: position.Row + 1}
			rowsUsed++
			continue
		}
		if info.Size() < 0 {
			_ = file.Close()
			diags.add(DiagFileUnreadable, "candidate size is invalid", ptr(o.cfg.ID), ptr(r.path), nil)
			position = searchPosition{Row: position.Row + 1}
			rowsUsed++
			continue
		}
		if info.Size() > MaxSearchBytes-bytesRead {
			_ = file.Close()
			diags.add(DiagScanLimit, "native byte scan limit reached", ptr(o.cfg.ID), ptr(r.path), nil)
			break
		}
		rowsUsed++
		out := scanSearchFile(searchFileInput{ctx, o.cfg, o.root, r.path, file, info, position.Line, matcher, req.Limit - len(resp.Hits), MaxResponseBytes - used, emptyAllowance})
		_ = file.Close()
		bytesRead += out.bytesRead
		if err := ctx.Err(); err != nil {
			return nil, ctxError(err)
		}
		if out.invalidCursor {
			return nil, newError(CodeInvalidRequest, "search cursor is stale or outside the session")
		}
		if out.fatal != nil {
			diags.add(out.fatal.Code, out.fatal.Message, ptr(o.cfg.ID), ptr(r.path), out.fatal.Line)
			position = searchPosition{Row: position.Row + 1}
			continue
		}
		for _, d := range out.diagnostics {
			diags.add(d.Code, d.Message, ptr(o.cfg.ID), ptr(r.path), d.Line)
		}
		diags.omitted += out.omittedDiagnostics
		resp.Hits = append(resp.Hits, out.hits...)
		used += out.hitBytes
		if out.deferred {
			position.Line = out.resumeLine
			break
		}
		position = searchPosition{Row: position.Row + 1}
	}
	resp.EOF = position.Row == len(rows)
	if !resp.EOF {
		cursor := encodeSearchCursor(fp, position)
		resp.NextCursor = &cursor
	}
	resp.Diagnostics = diags.list
	if resp.Diagnostics == nil {
		resp.Diagnostics = []Diagnostic{}
	}
	resp.OmittedDiagnostics = diags.omitted
	resp.Partial = diags.count() > 0
	serialized, err := Marshal(resp)
	if err != nil {
		return nil, newError(CodeInternal, "cannot encode search response")
	}
	if len(serialized) > MaxResponseBytes {
		return nil, newError(CodeInternal, "search response exceeds serialization limit")
	}
	return resp, nil
}

type searchFileResult struct {
	hits               []SearchHit
	hitBytes           int64
	diagnostics        []Diagnostic
	omittedDiagnostics int
	fatal              *Diagnostic
	bytesRead          int64
	deferred           bool
	resumeLine         int64
	invalidCursor      bool
}

func (r *searchFileResult) addDiagnostic(code, message string, line *int64) {
	if len(r.diagnostics) >= MaxDiagnostics {
		r.omittedDiagnostics++
		return
	}
	r.diagnostics = append(r.diagnostics, Diagnostic{Code: code, Message: message, Line: line})
}

func (r *searchFileResult) deferAt(line int64, priorDiagnostics int) {
	r.deferred = true
	r.resumeLine = line - 1
	// Final Session metadata can make a provisional hit too large. Only
	// deliver diagnostics preceding the resulting cursor; subsequent lines
	// are revalidated and diagnosed on the next page.
	kept := r.diagnostics[:0]
	for _, d := range r.diagnostics {
		if d.Line == nil || *d.Line < line {
			kept = append(kept, d)
		}
	}
	r.diagnostics = kept
	r.omittedDiagnostics = priorDiagnostics - len(kept)
}

// countedExtent constrains every underlying read (including JSONL read-ahead
// and oversized-line draining) to the captured byte extent and checks context.
type countedExtent struct {
	ctx             context.Context
	source          io.Reader
	remaining, read int64
}

func (r *countedExtent) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	if r.remaining == 0 {
		return 0, io.EOF
	}
	if len(p) > 64<<10 {
		p = p[:64<<10]
	}
	if int64(len(p)) > r.remaining {
		p = p[:int(r.remaining)]
	}
	n, err := r.source.Read(p)
	r.read += int64(n)
	r.remaining -= int64(n)
	return n, err
}

type provisionalSearchHit struct {
	hit              SearchHit
	priorDiagnostics int
}

// searchFileInput bundles one captured candidate with its per-page allowance.
type searchFileInput struct {
	ctx                       context.Context
	src                       config.Source
	root                      *os.Root
	rel                       string
	file                      *os.File
	info                      os.FileInfo
	after                     int64
	matcher                   literalMatcher
	limit                     int
	allowance, emptyAllowance int64
}

func scanSearchFile(input searchFileInput) (out searchFileResult) {
	ctx, src, f, info, after, matcher, limit, allowance, emptyAllowance := input.ctx, input.src, input.file, input.info, input.after, input.matcher, input.limit, input.allowance, input.emptyAllowance
	reader := &countedExtent{ctx: ctx, source: f, remaining: info.Size()}
	defer func() { out.bytesRead = reader.read }()
	lines := newLineReader(reader, MaxLineBytes)
	adapter := harness.ForHarness(src.Harness)
	var metadata meta
	var provisional []provisionalSearchHit
	var provisionalBytes int64
	var lastLine int64
	for {
		if err := ctx.Err(); err != nil {
			return out
		}
		l, err := lines.Next()
		if err != nil {
			if !errors.Is(err, io.EOF) || reader.remaining != 0 {
				out.fatal = fileDiag(DiagFileUnreadable, "candidate changed or could not be completely read", nil)
			}
			break
		}
		lastLine = l.Number
		header := adapter.FixedHeader() && l.Number == 1
		if l.Oversized {
			out.fatal = fileDiag(DiagOversizedLine, "physical line exceeds the 4 MiB limit; standalone file was excluded", ptr(l.Number))
			break
		}
		if !l.Terminated {
			if header {
				out.fatal = fileDiag(DiagInvalidHistory, "required session header is incomplete", ptr(l.Number))
			} else if l.Number > after && !out.deferred {
				out.addDiagnostic(DiagIncompleteTail, "unterminated final record was withheld; restart after source changes", ptr(l.Number))
			}
			break
		}
		obj, ok := jsonObject(l.Data)
		if !ok {
			if header {
				out.fatal = fileDiag(DiagInvalidHistory, "required session header is not a JSON object", ptr(l.Number))
				break
			}
			if l.Number > after && !out.deferred {
				out.addDiagnostic(DiagMalformedLine, "line is not a valid JSON object", ptr(l.Number))
			}
			continue
		}
		e, parseErr := adapter.Parse(l.Number, obj)
		if parseErr != nil {
			var fe *harness.FormatError
			code := DiagInvalidHistory
			if errors.As(parseErr, &fe) && fe.Code == harness.CodeUnsupportedFormat {
				code = DiagUnsupportedFormat
			}
			out.fatal = fileDiag(code, "candidate has an invalid or unsupported standalone format", ptr(l.Number))
			break
		}
		if e.Identity != nil && metadata.identity != nil && e.Identity.NativeID != metadata.identity.NativeID {
			out.fatal = fileDiag(DiagInvalidHistory, "candidate has conflicting native session identities", ptr(l.Number))
			break
		}
		metadata.observe(e)
		if metadata.unsupported != "" {
			out.fatal = fileDiag(DiagUnsupportedFormat, "candidate depends on history outside this file", ptr(l.Number))
			break
		}
		if l.Number <= after || out.deferred {
			continue
		}
		for _, field := range harness.SearchText(src.Harness, obj) {
			start, found := matcher.find(field.Text)
			if !found {
				continue
			}
			text, begin, end, before, afterSnippet := snippet(field.Text, start, len(matcher.pattern))
			hit := SearchHit{Line: l.Number, Type: e.Type, Subtype: e.Subtype, ID: e.ID, ParentID: e.ParentID, Timestamp: e.Timestamp, Field: field.Pointer, Snippet: text, MatchStart: begin, MatchEnd: end, TruncatedBefore: before, TruncatedAfter: afterSnippet}
			// Native metadata can be almost a whole 4 MiB physical line. Charge
			// its actual JSON representation before retaining the hit; the
			// session (including late titles) is charged after validation.
			b, marshalErr := Marshal(hit)
			if marshalErr != nil {
				out.fatal = fileDiag(DiagInvalidHistory, "candidate hit could not be serialized", ptr(l.Number))
				break
			}
			size := int64(len(b)) + 1
			if size > emptyAllowance {
				out.addDiagnostic(DiagRecordTooLarge, "matching record exceeds an empty response allowance", ptr(l.Number))
				break
			}
			if len(provisional) >= limit || provisionalBytes+size > allowance {
				out.deferred = true
				out.resumeLine = l.Number - 1
				break
			}
			provisional = append(provisional, provisionalSearchHit{hit, len(out.diagnostics) + out.omittedDiagnostics})
			provisionalBytes += size
			break
		}
		if out.fatal != nil {
			break
		}
	}
	return finishSearchFile(out, input, lastLine, &metadata, provisional)
}

// finishSearchFile validates the captured file's stable identity and commits
// only hits fitting both the current and an otherwise empty response.
func finishSearchFile(out searchFileResult, input searchFileInput, lastLine int64, metadata *meta, provisional []provisionalSearchHit) searchFileResult {
	src, root, rel, f, info, after, allowance, emptyAllowance := input.src, input.root, input.rel, input.file, input.info, input.after, input.allowance, input.emptyAllowance
	if out.fatal != nil {
		return out
	}
	if after > lastLine {
		out.invalidCursor = true
		return out
	}
	if metadata.identity == nil {
		out.fatal = fileDiag(DiagIdentityUnavailable, "candidate has no native session identity", nil)
		return out
	}
	post, err := f.Stat()
	if err != nil || post.Size() < info.Size() || (post.Size() == info.Size() && !post.ModTime().Equal(info.ModTime())) || (err == nil && !os.SameFile(info, post)) {
		out.fatal = fileDiag(DiagFileUnreadable, "candidate changed during validation", nil)
		return out
	}
	pathInfo, err := root.Lstat(rel)
	if err != nil || !os.SameFile(info, pathInfo) {
		out.fatal = fileDiag(DiagFileUnreadable, "candidate was replaced during validation", nil)
		return out
	}
	session, warning := buildSession(src, rel, info, metadata, true, true)
	if session == nil {
		out.fatal = fileDiag(DiagIdentityUnavailable, "candidate has invalid native session metadata", nil)
		return out
	}
	out.hits = make([]SearchHit, 0, len(provisional))
	var provisionalBytes int64
	for i, entry := range provisional {
		hit := entry.hit
		hit.Session = *session
		b, err := Marshal(hit)
		if err != nil {
			out.fatal = fileDiag(DiagInvalidHistory, "candidate hit could not be serialized", ptr(hit.Line))
			return out
		}
		size := int64(len(b)) + 1
		if size > emptyAllowance {
			out.addDiagnostic(DiagRecordTooLarge, "matching record exceeds an empty response allowance", ptr(hit.Line))
			for j := i + 1; j < len(provisional); j++ {
				provisional[j].priorDiagnostics++
			}
			continue
		}
		if provisionalBytes+size > allowance {
			out.deferAt(hit.Line, entry.priorDiagnostics)
			break
		}
		out.hits = append(out.hits, hit)
		provisionalBytes += size
	}
	if warning != nil && !out.deferred {
		out.addDiagnostic(warning.Code, warning.Message, warning.Line)
	}
	out.hitBytes = provisionalBytes
	return out
}
