// Package continuation coordinates explicitly authorized continuation actions.
// History and generated text are data, never commands or authority.
package continuation

import (
	"context"
	"errors"
	"unicode"

	"github.com/sfrieds3/spool/internal/history"
)

type History interface {
	List(context.Context, history.ListRequest) (*history.ListResponse, *history.Error)
	Read(context.Context, history.ReadRequest) (*history.ReadResponse, *history.Error)
	Describe(context.Context, string) (*history.Session, *history.Error)
}

// CleanLabel removes terminal framing and bidirectional presentation controls.
func CleanLabel(s string) string {
	out := []rune{}
	for _, r := range s {
		if unicode.IsControl(r) || r == 0x2028 || r == 0x2029 || r == 0x061c || r == 0x200e || r == 0x200f || r >= 0x202a && r <= 0x202e || r >= 0x2066 && r <= 0x2069 {
			continue
		}
		out = append(out, r)
	}
	return string(out)
}

// Catalog follows continuation offsets, not row counts. Failure discards all rows.
func Catalog(ctx context.Context, h History, harness string, diagnostic func(history.Diagnostic)) ([]history.Session, error) {
	var rows []history.Session
	refs, diags := map[string]bool{}, map[string]bool{}
	var offset int64
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		page, err := h.List(ctx, history.ListRequest{Harness: harness, Offset: offset, Limit: history.MaxLimit})
		if err != nil {
			return nil, err
		}
		for _, d := range page.Diagnostics {
			b, _ := history.Marshal(d)
			key := string(b)
			if !diags[key] && diagnostic != nil {
				diagnostic(d)
			}
			diags[key] = true
		}
		for _, s := range page.Sessions {
			if _, err := history.DecodeRef(s.Ref); err != nil {
				return nil, err
			}
			if !refs[s.Ref] {
				rows = append(rows, s)
				refs[s.Ref] = true
			}
		}
		if page.NextOffset == nil {
			return rows, nil
		}
		if *page.NextOffset <= offset {
			return nil, errors.New("catalog continuation offset did not advance")
		}
		offset = *page.NextOffset
	}
}
