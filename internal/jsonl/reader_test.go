package jsonl

import (
	"errors"
	"io"
	"strings"
	"testing"
)

func TestReader(t *testing.T) {
	long := strings.Repeat("x", 100)
	input := "a\r\n\n" + long + "\nbb\n" + long + "tail"
	r := NewReader(strings.NewReader(input), 10)
	type got struct {
		n         int64
		data      string
		size      int64
		oversized bool
		term      bool
	}
	want := []got{
		{1, "a", 3, false, true},
		{2, "", 1, false, true},
		{3, "", 101, true, true},
		{4, "bb", 3, false, true},
		{5, "", 104, true, false},
	}
	for _, w := range want {
		l, err := r.Next()
		if err != nil {
			t.Fatal(err)
		}
		g := got{l.Number, string(l.Data), l.Size, l.Oversized, l.Terminated}
		if g != w {
			t.Fatalf("got %+v want %+v", g, w)
		}
	}
	if _, err := r.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("err = %v", err)
	}
	if r.Consumed() != int64(len(input)) {
		t.Fatalf("consumed %d of %d", r.Consumed(), len(input))
	}
}

func TestReaderLimitExcludesTerminator(t *testing.T) {
	r := NewReader(strings.NewReader("0123456789\n0123456789"), 10)
	for range 2 {
		l, err := r.Next()
		if err != nil || l.Oversized || len(l.Data) != 10 {
			t.Fatalf("line = %+v err %v", l, err)
		}
	}
}
