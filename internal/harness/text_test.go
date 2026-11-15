package harness

import (
	"reflect"
	"testing"
)

func TestSearchTextAllowlist(t *testing.T) {
	tests := []struct {
		name, harness, raw string
		want               []TextField
	}{
		{"pi string escape", "pi", `{"type":"message","message":{"role":"user","content":"a\\nb"},"title":"hidden"}`, []TextField{{"/message/content", "a\\nb"}}},
		{"pi message content regardless of role", "pi", `{"type":"message","message":{"role":"custom","content":"recorded text"}}`, []TextField{{"/message/content", "recorded text"}}},
		{"pi blocks", "pi", `{"type":"message","message":{"role":"toolResult","content":[{"type":"text","text":"first"},{"type":"image","data":"ignore"},{"type":"text","text":"second"},{"type":"thinking","text":"ignore"}]}}`, []TextField{{"/message/content/0/text", "first"}, {"/message/content/2/text", "second"}}},
		{"pi summaries", "pi", `{"type":"branch_summary","summary":"branch","data":"ignore"}`, []TextField{{"/summary", "branch"}}},
		{"pi compaction", "pi", `{"type":"compaction","summary":"compact"}`, []TextField{{"/summary", "compact"}}},
		{"claude string", "claude", `{"type":"assistant","message":{"content":"say hi"}}`, []TextField{{"/message/content", "say hi"}}},
		{"claude nested result", "claude", `{"type":"user","message":{"content":[{"type":"tool_use","input":{"command":"secret"}},{"type":"text","text":"hello"},{"type":"tool_result","content":[{"type":"image","data":"hidden"},{"type":"text","text":"output"}]},{"type":"tool_result","content":"plain"}]},"toolUseResult":{"stdout":"duplicate"}}`, []TextField{{"/message/content/1/text", "hello"}, {"/message/content/2/content/1/text", "output"}, {"/message/content/3/content", "plain"}}},
		{"claude summary", "claude", `{"type":"summary","summary":"summary"}`, []TextField{{"/summary", "summary"}}},
		{"codex message", "codex", `{"type":"response_item","payload":{"type":"message","content":[{"type":"input_text","text":"one"},{"type":"image","text":"ignore"},{"type":"output_text","text":"two"},{"type":"text","text":"three"}]}}`, []TextField{{"/payload/content/0/text", "one"}, {"/payload/content/2/text", "two"}, {"/payload/content/3/text", "three"}}},
		{"codex output", "codex", `{"type":"response_item","payload":{"type":"function_call_output","output":"{\\\"output\\\":\\\"literal\\\"}","arguments":"secret"}}`, []TextField{{"/payload/output", `{\"output\":\"literal\"}`}}},
		{"codex compaction", "codex", `{"type":"compacted","payload":{"message":"recap"}}`, []TextField{{"/payload/message", "recap"}}},
		{"unknown types", "codex", `{"type":"event_msg","payload":{"type":"agent_message","message":"mirror"}}`, nil},
		{"wrong shaped fields", "pi", `{"type":"message","message":{"role":"user","content":[{"type":"text","text":{"value":"hidden"}}]}}`, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SearchText(tt.harness, []byte(tt.raw))
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %#v want %#v", got, tt.want)
			}
		})
	}
}

func TestSearchTextDoesNotChangeAdapter(t *testing.T) {
	raw := []byte(`{"type":"message","id":"one","message":{"role":"user","content":"safe"}}`)
	before, err := Pi{}.Parse(2, raw)
	if err != nil {
		t.Fatal(err)
	}
	_ = SearchText("pi", raw)
	after, err := Pi{}.Parse(2, raw)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("adapter changed: %v %v %v", before, after, err)
	}
}
