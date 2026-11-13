package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// TestReadingDocExample verifies the documented continuation, partial, and
// EOF values against the synthetic fixtures.
func TestReadingDocExample(t *testing.T) {
	doc, err := os.ReadFile("../../docs/reading.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(doc)
	for _, claim := range []string{
		"lines 1–4 with `\"next_offset\":4,\"eof\":false,\"partial\":false`",
		"lines 5–10 with `\"next_offset\":null,\"eof\":true,\"partial\":false`",
		"default `50`, maximum `500`",
	} {
		if !strings.Contains(text, claim) {
			t.Fatalf("docs/reading.md no longer states %q", claim)
		}
	}
	w := newWorkspace(t)
	ref := w.run(t, "list", "--config", w.config, "--harness", "pi").sessions()[0]["ref"].(string)
	check := func(offset string, first, last int64, next any, eof bool) {
		r := w.run(t, "read", "--config", w.config, "--limit", "4", "--offset", offset, ref)
		if offset == "4" {
			r = w.run(t, "read", "--config", w.config, "--offset", offset, ref)
		}
		recs := r.json["records"].([]any)
		lineOf := func(i int) int64 {
			n, _ := recs[i].(map[string]any)["line"].(json.Number).Int64()
			return n
		}
		if lineOf(0) != first || lineOf(len(recs)-1) != last || r.json["next_offset"] != next || r.json["eof"] != eof || r.json["partial"] != false {
			t.Fatalf("offset %s: %s", offset, r.stdout)
		}
	}
	check("0", 1, 4, json.Number("4"), false)
	check("4", 5, 10, nil, true)
}

// TestFormatsDocFixtures ensures every fixture directory named in the format
// matrix exists and is exercised by the end-to-end listing.
func TestSearchDocExample(t *testing.T) {
	data, err := os.ReadFile("../../docs/search.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, claim := range []string{"--config FILE", "--cursor 'q1.…' widget", "`match_start`", "`next_cursor`", "`partial`", "128 MiB", "unredacted", "unterminated tail"} {
		if !strings.Contains(text, claim) {
			t.Errorf("search docs missing %q", claim)
		}
	}
	w := newWorkspace(t)
	first := w.run(t, "search", "--config", w.config, "--limit", "1", "widget")
	if first.code != 0 || first.json["content"] != "untrusted_history" || first.json["view"] != "native_file_order" || first.json["limit"] != json.Number("1") || first.json["next_cursor"] == nil {
		t.Fatalf("documented first page %s", first.stdout)
	}
	second := w.run(t, "search", "--config", w.config, "--limit", "20", "--cursor", first.json["next_cursor"].(string), "widget")
	if second.code != 0 || len(second.json["hits"].([]any)) == 0 {
		t.Fatalf("documented continuation %s", second.stdout)
	}
	for _, key := range []string{"version", "ok", "content", "view", "sources", "hits", "limit", "next_cursor", "eof", "partial", "diagnostics", "omitted_diagnostics"} {
		if _, ok := first.json[key]; !ok {
			t.Errorf("missing documented response field %s", key)
		}
	}
	for _, key := range []string{"session", "line", "type", "subtype", "id", "parent_id", "timestamp", "field", "snippet", "match_start", "match_end", "truncated_before", "truncated_after"} {
		if _, ok := first.json["hits"].([]any)[0].(map[string]any)[key]; !ok {
			t.Errorf("missing documented hit field %s", key)
		}
	}
}

func TestFormatsDocFixtures(t *testing.T) {
	doc, err := os.ReadFile("../../docs/formats.md")
	if err != nil {
		t.Fatal(err)
	}
	w := newWorkspace(t)
	sessions := w.run(t, "list", "--config", w.config).sessions()
	bySource := map[string]int{}
	for _, s := range sessions {
		bySource[s["source_id"].(string)]++
	}
	for _, src := range []string{"pi", "claude", "codex", "codex-archive"} {
		if !strings.Contains(string(doc), "testdata/sessions/"+src+")") {
			t.Errorf("formats.md does not link fixture %s", src)
		}
		if bySource[src] == 0 {
			t.Errorf("fixture %s has no listed session", src)
		}
	}
	if !strings.Contains(string(doc), "Compatibility with every installed release is not claimed") {
		t.Error("formats.md must not claim exhaustive compatibility")
	}
}
