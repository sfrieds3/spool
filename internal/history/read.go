package history

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"unicode/utf8"

	"github.com/sfrieds3/spool/internal/config"
	"github.com/sfrieds3/spool/internal/harness"
	"github.com/sfrieds3/spool/internal/jsonl"
)

// ReadRequest selects a record page of one session.
type ReadRequest struct {
	Ref string
	// Offset is the number of physical lines already consumed.
	Offset int64
	Limit  int
}

func newLineReader(r io.Reader, max int) *jsonl.Reader { return jsonl.NewReader(r, max) }

func isEOF(err error) bool { return errors.Is(err, io.EOF) }

func validUTF8(s string) bool { return utf8.ValidString(s) }

// jsonObject returns data trimmed of surrounding whitespace when it is a
// single valid UTF-8 JSON object.
func jsonObject(data []byte) ([]byte, bool) {
	t := bytes.TrimSpace(data)
	if len(t) == 0 || t[0] != '{' || !utf8.Valid(t) || !json.Valid(t) {
		return nil, false
	}
	return t, true
}

// Read returns one page of native records from a referenced session, in
// physical file order. The whole file is scanned on each call to revalidate
// identity and detect unsupported composite-history markers; only the
// requested page is returned.
func (s *Service) Read(ctx context.Context, req ReadRequest) (*ReadResponse, *Error) {
	ref, rerr := DecodeRef(req.Ref)
	if rerr != nil {
		return nil, rerr.(*Error)
	}
	if err := checkPage(req.Offset, req.Limit); err != nil {
		return nil, err
	}
	src, ok := s.source(ref.SourceID)
	if !ok {
		return nil, newError(CodeInvalidRequest, "session reference names a source that is not configured and enabled")
	}
	adapter := harness.ForHarness(src.Harness)
	notFound := newError(CodeSessionNotFound, "session no longer exists at its recorded location")
	if src.Harness != ref.Harness {
		return nil, notFound
	}
	if !adapter.Candidate(ref.Path) {
		return nil, newError(CodeInvalidRequest, "session reference does not name a session file")
	}
	root, err := openSourceRoot(src.Root)
	if err != nil {
		if src.Default && errors.Is(err, fs.ErrNotExist) {
			return nil, notFound
		}
		return nil, newError(CodeSourceUnavailable, "source %s cannot be read: %s", src.ID, errText(err))
	}
	defer root.Close()
	f, info, err := openRegular(root, ref.Path)
	if err != nil {
		switch {
		case errors.Is(err, fs.ErrNotExist), errors.Is(err, errSymlink), errors.Is(err, errNotRegular), errors.Is(err, fs.ErrInvalid):
			return nil, notFound
		}
		return nil, newError(CodeSourceUnavailable, "session file cannot be opened: %s", errText(err))
	}
	defer f.Close()
	return s.readFile(ctx, src, ref, f, info, req)
}

func (s *Service) source(id string) (config.Source, bool) {
	for _, src := range s.sources {
		if src.ID == id && src.Enabled {
			return src, true
		}
	}
	return config.Source{}, false
}

func (s *Service) readFile(ctx context.Context, src config.Source, ref Ref, f *os.File, info os.FileInfo, req ReadRequest) (*ReadResponse, *Error) {
	adapter := harness.ForHarness(src.Harness)
	r := newLineReader(f, MaxLineBytes)
	diags := &diagnostics{max: MaxDiagnostics}
	var m meta
	var records []Record
	recordBudget := int64(MaxResponseBytes - envelopeReserve - diagnosticReserve - sessionReserve)
	var used int64
	pageOpen := true
	var next *int64
	eof := false
	path := ptr(ref.Path)

	stop := func(consumed int64) {
		pageOpen = false
		next = ptr(consumed)
	}
	// lineDiag records a line-scoped diagnostic. When the diagnostic budget
	// is full, the page ends before this line so its diagnostic is reported
	// by the next page instead of being lost.
	lineDiag := func(l jsonl.Line, code, msg string) bool {
		if len(diags.list) >= diags.max {
			stop(l.Number - 1)
			return false
		}
		diags.add(code, msg, nil, path, ptr(l.Number))
		return true
	}
	for n := 0; ; n++ {
		if n%1024 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, ctxError(err)
			}
		}
		l, err := r.Next()
		if err != nil {
			if isEOF(err) {
				if pageOpen {
					eof = true
				}
				break
			}
			return nil, newError(CodeSourceUnavailable, "session file cannot be read: %s", errText(err))
		}
		inPage := pageOpen && l.Number > req.Offset
		if inPage && len(records) == req.Limit {
			stop(l.Number - 1)
			inPage = false
		}
		header := adapter.FixedHeader() && l.Number == 1
		if !l.Terminated {
			if header {
				return nil, newError(CodeInvalidHistory, "session header is incomplete")
			}
			// An offset at or past an incomplete line still cannot skip it.
			if pageOpen && lineDiag(l, DiagIncompleteTail, "final line is not terminated and was withheld; retry from next_offset after the writer finishes") {
				stop(l.Number - 1)
			}
			break
		}
		if l.Oversized {
			if header {
				return nil, newError(CodeInvalidHistory, "session header exceeds the %d byte line limit", MaxLineBytes)
			}
			if inPage {
				lineDiag(l, DiagOversizedLine, "line exceeds the 4 MiB line limit and was omitted")
			}
			continue
		}
		obj, ok := jsonObject(l.Data)
		if !ok {
			if header {
				return nil, newError(CodeInvalidHistory, "session header is not a valid JSON object")
			}
			if inPage {
				lineDiag(l, DiagMalformedLine, "line is not a valid JSON object and was skipped")
			}
			continue
		}
		e, perr := adapter.Parse(l.Number, obj)
		if perr != nil {
			var fe *harness.FormatError
			if errors.As(perr, &fe) {
				return nil, &Error{Code: fe.Code, Message: fe.Msg}
			}
			return nil, newError(CodeInvalidHistory, "%v", perr)
		}
		m.observe(e)
		if m.unsupported != "" {
			return nil, newError(CodeUnsupportedFormat, "%s (line %d)", m.unsupported, l.Number)
		}
		if !inPage {
			continue
		}
		rec := Record{Line: l.Number, Type: e.Type, Subtype: e.Subtype, ID: e.ID, ParentID: e.ParentID, Timestamp: e.Timestamp, Native: json.RawMessage(bytes.Clone(obj))}
		b, merr := Marshal(rec)
		if merr != nil {
			return nil, newError(CodeInternal, "cannot encode record: %v", merr)
		}
		size := int64(len(b)) + 1
		if size > recordBudget {
			lineDiag(l, DiagRecordTooLarge, "record cannot fit in a response by itself and was omitted")
			continue
		}
		if used+size > recordBudget {
			stop(l.Number - 1)
			continue
		}
		used += size
		records = append(records, rec)
	}
	if m.identity == nil || m.identity.NativeID != ref.NativeID {
		return nil, newError(CodeSessionNotFound, "file no longer records the referenced session identity")
	}
	sess, diag := buildSession(src, ref.Path, info, &m, true, true)
	if sess == nil {
		return nil, newError(CodeInvalidHistory, "%s", diag.Message)
	}
	if diag != nil {
		diags.add(diag.Code, diag.Message, nil, path, nil)
	}
	resp := &ReadResponse{
		Version: ResponseVersion, OK: true,
		Content: ContentUntrustedHistory, View: ViewNativeFileOrder,
		Session: *sess, Records: records,
	}
	if resp.Records == nil {
		resp.Records = []Record{}
	}
	resp.Offset, resp.Limit = req.Offset, req.Limit
	resp.NextOffset, resp.EOF = next, eof && next == nil
	resp.Diagnostics = diags.list
	if resp.Diagnostics == nil {
		resp.Diagnostics = []Diagnostic{}
	}
	resp.OmittedDiagnostics = diags.omitted
	resp.Partial = diags.count() > 0
	return resp, nil
}
