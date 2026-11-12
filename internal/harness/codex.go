package harness

import (
	"encoding/json"
	"path"
	"strings"
)

// Codex interprets self-contained Codex rollout files
// (rollout-*.jsonl) in active and archived session roots.
//
// Line 1 must be a session_meta record whose payload.id is the session
// identity. Rollouts that declare paginated, based, or inherited history are
// composite: their full history lives partly outside the file (for example in
// a database or a parent rollout), so they are rejected as unsupported.
type Codex struct{}

func (Codex) Harness() string    { return "codex" }
func (Codex) Kind(string) string { return "session" }
func (Codex) FixedHeader() bool  { return true }

func (Codex) Candidate(rel string) bool {
	base := path.Base(rel)
	return strings.HasPrefix(base, "rollout-") && strings.HasSuffix(base, ".jsonl")
}

func (Codex) Parse(line int64, raw []byte) (Entry, error) {
	m, err := fields(raw)
	if err != nil {
		return Entry{}, invalidHistory("line %d: %v", line, err)
	}
	e := Entry{Type: str(m, "type"), Timestamp: str(m, "timestamp")}
	payload := object(m, "payload")
	if payload != nil {
		e.Subtype = str(payload, "type")
	}
	isMeta := e.Type != nil && *e.Type == "session_meta"
	if line == 1 {
		if !isMeta {
			return Entry{}, unsupported("line 1 is not a Codex session_meta record")
		}
		if payload == nil {
			return Entry{}, invalidHistory("session_meta has no payload object")
		}
		id := str(payload, "id")
		if !nonEmpty(id) {
			return Entry{}, invalidHistory("session_meta payload has no id")
		}
		started := str(payload, "timestamp")
		if started == nil {
			started = e.Timestamp
		}
		e.Identity = &Identity{NativeID: *id, Cwd: str(payload, "cwd"), Started: started}
	}
	if isMeta && payload != nil {
		e.Unsupported = compositeMarker(payload)
	}
	if e.Type != nil && *e.Type == "event_msg" && e.Subtype != nil && *e.Subtype == "thread_name_updated" {
		e.SetsTitle, e.Title, e.TitlePriority = true, str(payload, "thread_name"), 1
	}
	return e, nil
}

// compositeMarker reports why a session_meta payload depends on history
// outside the rollout file, or "" when it is self-contained.
//
// The marker fields come from the inspected Codex 0.164.0 session metadata
// structure. A plain forked_from_id is provenance only: legacy forks copy the
// inherited records into the new rollout.
func compositeMarker(p map[string]json.RawMessage) string {
	if mode := str(p, "history_mode"); mode != nil && *mode != "legacy" {
		return "session_meta history_mode " + *mode + " stores history outside the rollout file"
	}
	for _, key := range []string{"history_base", "forked_from_ordinal_exclusive", "subagent_history_start_ordinal"} {
		if present(p, key) {
			return "session_meta " + key + " depends on inherited history outside the rollout file"
		}
	}
	return ""
}
