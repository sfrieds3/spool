// Package safefile opens regular files beneath an os.Root without following
// symbolic links in any component and without blocking on special files.
package safefile

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

var (
	// ErrSymlink reports a path component that is a symbolic link.
	ErrSymlink = errors.New("path traverses a symbolic link")
	// ErrNotRegular reports a final component that is not a regular file, or
	// a file replaced between inspection and opening.
	ErrNotRegular = errors.New("not a regular file")
)

// OpenRegular opens a regular file at a slash path beneath root. Every
// component is inspected with Lstat; symbolic links are rejected. The file is
// opened read-only and nonblocking (on Unix), and the opened handle must be
// the same regular file that was inspected.
func OpenRegular(root *os.Root, rel string) (*os.File, os.FileInfo, error) {
	name := filepath.FromSlash(rel)
	parts := strings.Split(rel, "/")
	var final os.FileInfo
	for i := range parts {
		info, err := root.Lstat(filepath.FromSlash(strings.Join(parts[:i+1], "/")))
		if err != nil {
			return nil, nil, err
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			return nil, nil, ErrSymlink
		}
		final = info
	}
	if !final.Mode().IsRegular() {
		return nil, nil, ErrNotRegular
	}
	f, err := root.OpenFile(name, os.O_RDONLY|openNonblock, 0)
	if err != nil {
		return nil, nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, nil, err
	}
	if !info.Mode().IsRegular() || !os.SameFile(info, final) {
		f.Close()
		return nil, nil, ErrNotRegular
	}
	return f, info, nil
}
