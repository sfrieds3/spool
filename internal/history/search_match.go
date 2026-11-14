package history

import (
	"unicode"
	"unicode/utf8"
)

// literalMatcher is a rune KMP automaton. Canonicalizing each SimpleFold orbit
// preserves one-rune-to-one-rune matching and source offsets without normalization.
type literalMatcher struct {
	pattern       []rune
	failure       []int
	caseSensitive bool
}

func foldRune(r rune) rune {
	min := r
	for next := unicode.SimpleFold(r); next != r; next = unicode.SimpleFold(next) {
		if next < min {
			min = next
		}
	}
	return min
}

func newLiteralMatcher(query string, caseSensitive bool) literalMatcher {
	m := literalMatcher{caseSensitive: caseSensitive}
	for _, r := range query {
		m.pattern = append(m.pattern, m.key(r))
	}
	m.failure = make([]int, len(m.pattern))
	for i, j := 1, 0; i < len(m.pattern); i++ {
		for j > 0 && m.pattern[i] != m.pattern[j] {
			j = m.failure[j-1]
		}
		if m.pattern[i] == m.pattern[j] {
			j++
		}
		m.failure[i] = j
	}
	return m
}
func (m literalMatcher) key(r rune) rune {
	if m.caseSensitive {
		return r
	}
	return foldRune(r)
}
func (m literalMatcher) find(text string) (int, bool) {
	j, n := 0, 0
	for _, r := range text {
		k := m.key(r)
		for j > 0 && k != m.pattern[j] {
			j = m.failure[j-1]
		}
		if k == m.pattern[j] {
			j++
		}
		n++
		if j == len(m.pattern) {
			return n - j, true
		}
	}
	return 0, false
}

func snippet(text string, start, length int) (string, int, int, bool, bool) {
	total := utf8.RuneCountInString(text)
	begin := max(0, start-96)
	if total-begin < 512 {
		begin = max(0, total-512)
	}
	end := min(total, begin+512)
	// Match length is <= 256 bytes, so it always fits within the window.
	beginByte, endByte := 0, len(text)
	index := 0
	for i := range text {
		if index == begin {
			beginByte = i
		}
		if index == end {
			endByte = i
			break
		}
		index++
	}
	return text[beginByte:endByte], start - begin, start - begin + length, begin > 0, end < total
}
