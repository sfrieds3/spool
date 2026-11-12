package history

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/sfrieds3/spool/internal/config"
)

const (
	refPrefix = "s1."
	// MaxRefLength bounds accepted reference strings.
	MaxRefLength = 12 * 1024
	// maxRelPathBytes bounds source-relative paths that can be referenced.
	maxRelPathBytes = 4096
	// maxNativeIDBytes bounds native session identifiers.
	maxNativeIDBytes = 1024
)

// Ref is the decoded content of an opaque session reference. It is a
// locator, not a credential.
type Ref struct {
	SourceID string
	Harness  string
	Path     string // slash-separated, relative to the source root
	NativeID string
}

// Encode returns the opaque reference string. Encoding is deterministic, so
// unchanged inputs always produce the same reference.
func (r Ref) Encode() string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode([]string{r.SourceID, r.Harness, r.Path, r.NativeID})
	return refPrefix + base64.RawURLEncoding.EncodeToString(bytes.TrimSuffix(buf.Bytes(), []byte("\n")))
}

// DecodeRef strictly validates and decodes a reference string.
func DecodeRef(s string) (Ref, error) {
	bad := func(msg string) (Ref, error) {
		return Ref{}, newError(CodeInvalidRequest, "invalid session reference: %s", msg)
	}
	if len(s) > MaxRefLength {
		return bad("too long")
	}
	if !strings.HasPrefix(s, refPrefix) {
		return bad("unsupported reference version")
	}
	data, err := base64.RawURLEncoding.Strict().DecodeString(s[len(refPrefix):])
	if err != nil {
		return bad("malformed encoding")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	var parts []string
	if err := dec.Decode(&parts); err != nil {
		return bad("malformed content")
	}
	if _, err := dec.Token(); err != io.EOF {
		return bad("trailing content")
	}
	if len(parts) != 4 {
		return bad("wrong field count")
	}
	r := Ref{SourceID: parts[0], Harness: parts[1], Path: parts[2], NativeID: parts[3]}
	if !config.ValidSourceID(r.SourceID) {
		return bad("invalid source id")
	}
	if !config.IsHarness(r.Harness) {
		return bad("unsupported harness")
	}
	if msg := validRelPath(r.Path); msg != "" {
		return bad(msg)
	}
	if r.NativeID == "" || len(r.NativeID) > maxNativeIDBytes || !utf8.ValidString(r.NativeID) {
		return bad("invalid native id")
	}
	// Reject non-canonical encodings so each session has one reference.
	if r.Encode() != s {
		return bad("non-canonical encoding")
	}
	return r, nil
}

// validRelPath returns a reason when p is not a clean, relative,
// slash-separated path that stays inside its root.
func validRelPath(p string) string {
	switch {
	case p == "":
		return "empty path"
	case len(p) > maxRelPathBytes:
		return "path too long"
	case !utf8.ValidString(p):
		return "path is not valid UTF-8"
	case strings.ContainsAny(p, "\\\x00"):
		return "path contains an unsupported separator or NUL"
	case path.IsAbs(p):
		return "absolute path"
	case path.Clean(p) != p:
		return "non-canonical path"
	}
	for _, part := range strings.Split(p, "/") {
		if part == ".." || part == "." {
			return "path traversal"
		}
	}
	return ""
}
