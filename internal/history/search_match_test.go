package history

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestLiteralMatcher(t *testing.T) {
	cases := []struct {
		query, text      string
		sensitive, found bool
		offset           int
	}{
		{"widget.*", "WIDGET.*", false, true, 0}, {"Widget", "widget", true, false, 0},
		{" a ", "x a y", false, true, 1}, {"K", "K", false, true, 0},
		{"s", "ſ", false, true, 0}, {"Σ", "ς", false, true, 0},
		{"ss", "ß", false, false, 0}, {"é", "e\u0301", false, false, 0},
		{"AbAbAc", "abababac", false, true, 2},
	}
	for _, tt := range cases {
		m := newLiteralMatcher(tt.query, tt.sensitive)
		got, ok := m.find(tt.text)
		if ok != tt.found || (ok && got != tt.offset) {
			t.Errorf("%q %q: (%d,%v), want (%d,%v)", tt.query, tt.text, got, ok, tt.offset, tt.found)
		}
	}
}

func TestSnippetRuneBoundariesAndBackfill(t *testing.T) {
	text := strings.Repeat("界", 700) + "match" + strings.Repeat("🙂", 10)
	start, found := newLiteralMatcher("match", true).find(text)
	if !found || start != 700 {
		t.Fatalf("match at %d %v", start, found)
	}
	s, a, b, before, after := snippet(text, start, 5)
	if !utf8.ValidString(s) || utf8.RuneCountInString(s) != 512 || a != 497 || b != 502 || !before || after || []rune(s)[a:b][0] != 'm' {
		t.Fatalf("snippet rune count=%d offsets=%d:%d before=%v after=%v", utf8.RuneCountInString(s), a, b, before, after)
	}
	s, a, b, before, after = snippet("αβγmatchδε", 3, 5)
	if s != "αβγmatchδε" || a != 3 || b != 8 || before || after {
		t.Fatalf("short: %q %d %d %v %v", s, a, b, before, after)
	}
	text = strings.Repeat("a", 200) + strings.Repeat("q", 256) + strings.Repeat("z", 1000)
	start, found = newLiteralMatcher(strings.Repeat("q", 256), true).find(text)
	s, a, b, before, after = snippet(text, start, 256)
	if !found || utf8.RuneCountInString(s) > 512 || b-a != 256 || !before || !after {
		t.Fatalf("long match %d %d:%d %v %v", start, a, b, before, after)
	}
}
