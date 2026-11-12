// Package jsonl reads physical JSON Lines incrementally with a per-line byte
// limit. Oversized lines are discarded while scanning, never fully buffered.
package jsonl

import (
	"bufio"
	"errors"
	"io"
)

// Line is one physical line. Data excludes the line terminator and is valid
// only until the next call to Next.
type Line struct {
	// Number is the 1-based physical line number.
	Number int64
	// Data holds the line content, or nil when Oversized.
	Data []byte
	// Size is the number of bytes consumed, including any terminator.
	Size int64
	// Oversized is set when the content exceeded the reader's limit.
	Oversized bool
	// Terminated is false for a final line without a trailing newline.
	Terminated bool
}

// Reader yields physical lines from an underlying reader.
type Reader struct {
	br    *bufio.Reader
	max   int
	line  int64
	bytes int64
	buf   []byte
}

// NewReader returns a Reader that treats content longer than maxLine bytes as
// oversized.
func NewReader(r io.Reader, maxLine int) *Reader {
	return &Reader{br: bufio.NewReaderSize(r, 64*1024), max: maxLine}
}

// Consumed returns the number of bytes consumed so far.
func (r *Reader) Consumed() int64 { return r.bytes }

// Next returns the next physical line, or io.EOF when no bytes remain.
func (r *Reader) Next() (Line, error) {
	r.buf = r.buf[:0]
	var size int64
	oversized := false
	for {
		chunk, err := r.br.ReadSlice('\n')
		size += int64(len(chunk))
		if !oversized {
			content := len(r.buf) + len(chunk)
			if err == nil {
				content-- // the terminator does not count toward the limit
			}
			if content > r.max {
				oversized = true
				r.buf = r.buf[:0]
			} else {
				r.buf = append(r.buf, chunk...)
			}
		}
		switch {
		case err == nil:
			return r.finish(size, oversized, true), nil
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case errors.Is(err, io.EOF):
			if size == 0 {
				return Line{}, io.EOF
			}
			return r.finish(size, oversized, false), nil
		default:
			return Line{}, err
		}
	}
}

func (r *Reader) finish(size int64, oversized, terminated bool) Line {
	r.line++
	r.bytes += size
	l := Line{Number: r.line, Size: size, Oversized: oversized, Terminated: terminated}
	if !oversized {
		data := r.buf
		if terminated {
			data = data[:len(data)-1]
			if n := len(data); n > 0 && data[n-1] == '\r' {
				data = data[:n-1]
			}
		}
		l.Data = data
	}
	return l
}
