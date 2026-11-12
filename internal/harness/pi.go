package harness

// Pi interprets Pi session files with version 3 headers.
//
// Line 1 must be {"type":"session","version":3,"id":...}. Legacy v1/v2
// sessions are unsupported; Spool does not migrate them. Entries form a tree
// via id/parentId and are returned in native file order.
type Pi struct{}

func (Pi) Harness() string           { return "pi" }
func (Pi) Candidate(rel string) bool { return isJSONL(rel) }
func (Pi) Kind(string) string        { return "session" }
func (Pi) FixedHeader() bool         { return true }

func (Pi) Parse(line int64, raw []byte) (Entry, error) {
	m, err := fields(raw)
	if err != nil {
		return Entry{}, invalidHistory("line %d: %v", line, err)
	}
	e := Entry{Type: str(m, "type"), ID: str(m, "id"), ParentID: str(m, "parentId"), Timestamp: str(m, "timestamp")}
	if line == 1 {
		if e.Type == nil || *e.Type != "session" {
			return Entry{}, unsupported("line 1 is not a Pi session header")
		}
		if v, ok := m["version"]; !ok || string(v) != "3" {
			return Entry{}, unsupported("Pi session version is not 3 (legacy sessions are not migrated)")
		}
		if !nonEmpty(e.ID) {
			return Entry{}, invalidHistory("Pi session header has no id")
		}
		e.Identity = &Identity{NativeID: *e.ID, Cwd: str(m, "cwd"), Started: e.Timestamp}
		// The header is metadata, not a tree entry.
		e.ID = nil
		return e, nil
	}
	if e.Type == nil {
		return e, nil
	}
	switch *e.Type {
	case "message":
		if msg := object(m, "message"); msg != nil {
			e.Subtype = str(msg, "role")
		}
	case "custom", "custom_message":
		e.Subtype = str(m, "customType")
	case "usage":
		e.Subtype = str(m, "kind")
	case "session_info":
		e.SetsTitle = true
		e.Title = str(m, "name")
	}
	return e, nil
}
