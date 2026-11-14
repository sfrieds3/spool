package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/sfrieds3/spool/internal/handoff"
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

// TestHandoffsDocExample checks that the documented example brief is accepted
// by the parser and that documented limits match the code.
func TestHandoffsDocExample(t *testing.T) {
	doc, err := os.ReadFile("../../docs/handoffs.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(doc)
	start := strings.Index(text, "## Brief format (version 1)")
	block := text[start:]
	open := strings.Index(block, "```json\n") + len("```json\n")
	example := block[open : open+strings.Index(block[open:], "```")]
	if _, herr := handoff.ParseBrief([]byte(example)); herr != nil {
		t.Fatalf("documented brief rejected: %v", herr)
	}
	for _, claim := range []string{
		fmt.Sprintf("| Brief input | %d KiB |", handoff.MaxInputBytes>>10),
		fmt.Sprintf("| `title` | %d |", handoff.MaxTitleBytes),
		fmt.Sprintf("| Items per list | %d |", handoff.MaxItems),
		fmt.Sprintf("| `sessions` | %d entries |", handoff.MaxSessions),
		fmt.Sprintf("| Encoded stored handoff | %d MiB |", handoff.MaxHandoffBytes>>20),
		handoff.Preamble,
	} {
		if !strings.Contains(text, claim) {
			t.Errorf("docs/handoffs.md no longer states %q", claim)
		}
	}
}
