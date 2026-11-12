package harness

import "strings"

// Claude interprets Claude Code project transcripts and nested subagent
// transcripts. Identity comes from the first recorded sessionId; the file
// name and the encoded project directory name are never used to guess it.
type Claude struct{}

func (Claude) Harness() string           { return "claude" }
func (Claude) Candidate(rel string) bool { return isJSONL(rel) }
func (Claude) FixedHeader() bool         { return false }

func (Claude) Kind(rel string) string {
	if strings.Contains("/"+rel, "/subagents/") {
		return "subagent"
	}
	return "session"
}

func (Claude) Parse(_ int64, raw []byte) (Entry, error) {
	m, err := fields(raw)
	if err != nil {
		return Entry{}, invalidHistory("%v", err)
	}
	e := Entry{
		Type:      str(m, "type"),
		Subtype:   str(m, "subtype"),
		ID:        str(m, "uuid"),
		ParentID:  str(m, "parentUuid"),
		Timestamp: str(m, "timestamp"),
	}
	if sid := str(m, "sessionId"); nonEmpty(sid) {
		e.Identity = &Identity{NativeID: *sid, Cwd: str(m, "cwd"), Started: e.Timestamp}
	}
	if e.Type == nil {
		return e, nil
	}
	switch *e.Type {
	case "custom-title":
		e.SetsTitle, e.Title, e.TitlePriority = true, str(m, "customTitle"), 2
	case "ai-title":
		e.SetsTitle, e.Title, e.TitlePriority = true, str(m, "aiTitle"), 1
	case "fork-context-ref":
		// The writer stores a pointer to a parent transcript prefix instead of
		// the inherited records themselves.
		e.Unsupported = "fork-context-ref record depends on another transcript's history"
	}
	return e, nil
}
