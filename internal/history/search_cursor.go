package history

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/sfrieds3/spool/internal/config"
)

type searchPosition struct {
	Row  int
	Line int64
}

func searchFingerprint(req SearchRequest, selected []config.Source, states []SourceStatus) string {
	// The limit is intentionally excluded so a caller can adjust page size.
	data, _ := Marshal(struct {
		Query   string          `json:"q"`
		Case    bool            `json:"c"`
		Harness string          `json:"h"`
		Sources []config.Source `json:"s"`
		States  []SourceStatus  `json:"t"`
	}{req.Query, req.CaseSensitive, req.Harness, selected, states})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func encodeSearchCursor(fingerprint string, p searchPosition) string {
	data, _ := Marshal([]any{fingerprint, p.Row, p.Line})
	return "q1." + base64.RawURLEncoding.EncodeToString(data)
}

func decodeSearchCursor(cursor, fingerprint string, count int) (searchPosition, *Error) {
	bad := func() (searchPosition, *Error) {
		return searchPosition{}, newError(CodeInvalidRequest, "invalid, incompatible or stale search cursor")
	}
	if cursor == "" {
		return searchPosition{}, nil
	}
	if len(cursor) > MaxSearchCursorBytes || !strings.HasPrefix(cursor, "q1.") {
		return bad()
	}
	data, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(cursor, "q1."))
	if err != nil || "q1."+base64.RawURLEncoding.EncodeToString(data) != cursor {
		return bad()
	}
	var values []json.RawMessage
	if json.Unmarshal(data, &values) != nil || len(values) != 3 {
		return bad()
	}
	var fp string
	var p searchPosition
	if json.Unmarshal(values[0], &fp) != nil || fp != fingerprint || json.Unmarshal(values[1], &p.Row) != nil || json.Unmarshal(values[2], &p.Line) != nil || p.Row < 0 || p.Row > count || p.Line < 0 || (p.Row == count && p.Line != 0) {
		return bad()
	}
	canonical, _ := Marshal([]any{fp, p.Row, p.Line})
	if !bytes.Equal(canonical, data) {
		return bad()
	}
	return p, nil
}
