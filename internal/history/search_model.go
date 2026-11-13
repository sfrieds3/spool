package history

// Search limits bound content scanning independently of directory discovery.
const (
	MaxSearchFileBytes   = 64 << 20
	MaxSearchBytes       = 128 << 20
	MaxSearchRows        = 256
	MaxSearchQueryBytes  = 256
	MaxSearchCursorBytes = 4 << 10
)

const DiagScanLimit = "scan_limit"

// SearchRequest selects literal matches within configured local sources.
type SearchRequest struct {
	Query         string
	Harness       string
	CaseSensitive bool
	Limit         int
	Cursor        string
}

// SearchHit cites the first occurrence in the first matching field of a physical record.
type SearchHit struct {
	Session         Session `json:"session"`
	Line            int64   `json:"line"`
	Type            *string `json:"type"`
	Subtype         *string `json:"subtype"`
	ID              *string `json:"id"`
	ParentID        *string `json:"parent_id"`
	Timestamp       *string `json:"timestamp"`
	Field           string  `json:"field"`
	Snippet         string  `json:"snippet"`
	MatchStart      int     `json:"match_start"`
	MatchEnd        int     `json:"match_end"`
	TruncatedBefore bool    `json:"truncated_before"`
	TruncatedAfter  bool    `json:"truncated_after"`
}

// SearchResponse is a versioned, resumable native-file-order result page.
type SearchResponse struct {
	Version            int            `json:"version"`
	OK                 bool           `json:"ok"`
	Content            string         `json:"content"`
	View               string         `json:"view"`
	Sources            []SourceStatus `json:"sources"`
	Hits               []SearchHit    `json:"hits"`
	Limit              int            `json:"limit"`
	NextCursor         *string        `json:"next_cursor"`
	EOF                bool           `json:"eof"`
	Partial            bool           `json:"partial"`
	Diagnostics        []Diagnostic   `json:"diagnostics"`
	OmittedDiagnostics int            `json:"omitted_diagnostics"`
}
