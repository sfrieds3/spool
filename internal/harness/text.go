package harness

import (
	"encoding/json"
	"strconv"
	"strings"
)

// TextField is one allowlisted decoded native text leaf and its RFC 6901 pointer.
type TextField struct {
	Pointer string
	Text    string
}

// SearchText projects only documented message, tool output, and summary fields.
// Unknown record and block types cannot implicitly expand this allowlist.
func SearchText(name string, raw []byte) []TextField {
	m, err := fields(raw)
	if err != nil {
		return nil
	}
	var result []TextField
	add := func(o map[string]json.RawMessage, key, prefix string) {
		if s := str(o, key); s != nil {
			result = append(result, TextField{prefix + "/" + escapePointer(key), *s})
		}
	}
	blocks := func(raw json.RawMessage, prefix string, types map[string]bool) {
		var entries []json.RawMessage
		if json.Unmarshal(raw, &entries) != nil {
			return
		}
		for i, entry := range entries {
			block, err := fields(entry)
			if err == nil && types[value(str(block, "type"))] {
				add(block, "text", prefix+"/"+strconv.Itoa(i))
			}
		}
	}
	content := func(raw json.RawMessage, prefix string, types map[string]bool) {
		if len(raw) > 0 && raw[0] == '"' {
			var s string
			if json.Unmarshal(raw, &s) == nil {
				result = append(result, TextField{prefix, s})
			}
		} else {
			blocks(raw, prefix, types)
		}
	}
	switch name {
	case "pi":
		switch value(str(m, "type")) {
		case "message":
			msg := object(m, "message")
			content(msg["content"], "/message/content", map[string]bool{"text": true})
		case "branch_summary", "compaction":
			add(m, "summary", "")
		}
	case "claude":
		switch value(str(m, "type")) {
		case "summary":
			add(m, "summary", "")
		case "user", "assistant":
			msg := object(m, "message")
			raw := msg["content"]
			if len(raw) > 0 && raw[0] == '"' {
				content(raw, "/message/content", nil)
				break
			}
			var entries []json.RawMessage
			if json.Unmarshal(raw, &entries) != nil {
				break
			}
			for i, entry := range entries {
				block, err := fields(entry)
				if err != nil {
					continue
				}
				p := "/message/content/" + strconv.Itoa(i)
				switch value(str(block, "type")) {
				case "text":
					add(block, "text", p)
				case "tool_result":
					content(block["content"], p+"/content", map[string]bool{"text": true})
				}
			}
		}
	case "codex":
		payload := object(m, "payload")
		switch value(str(m, "type")) {
		case "response_item":
			switch value(str(payload, "type")) {
			case "message":
				blocks(payload["content"], "/payload/content", map[string]bool{"input_text": true, "output_text": true, "text": true})
			case "function_call_output":
				add(payload, "output", "/payload")
			}
		case "compacted":
			add(payload, "message", "/payload")
		}
	}
	return result
}

func value(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
func escapePointer(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "~", "~0"), "/", "~1")
}
