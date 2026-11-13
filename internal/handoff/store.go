package handoff

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/sfrieds3/spool/internal/config"
	"github.com/sfrieds3/spool/internal/history"
)

// errNoDir reports that the handoff directory does not exist.
var errNoDir = errors.New("handoff directory does not exist")

// tempFile is the subset of *os.File used to write a handoff.
type tempFile interface {
	io.Writer
	Sync() error
	Close() error
}

// fileOps are the storage operations Create performs inside the opened
// handoff directory. Tests replace them to inject failures.
type fileOps struct {
	create  func(root *os.Root, name string) (tempFile, error)
	link    func(root *os.Root, oldname, newname string) error
	remove  func(root *os.Root, name string) error
	syncDir func(root *os.Root) error
}

var defaultOps = fileOps{
	create: func(root *os.Root, name string) (tempFile, error) {
		return root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	},
	link:   func(root *os.Root, oldname, newname string) error { return root.Link(oldname, newname) },
	remove: func(root *os.Root, name string) error { return root.Remove(name) },
	syncDir: func(root *os.Root) error {
		d, err := root.Open(".")
		if err != nil {
			return err
		}
		defer d.Close()
		return d.Sync()
	},
}

func stateErr(format string, args ...any) *history.Error {
	return errorf(CodeStateUnavailable, format, args...)
}

func errText(err error) string {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Err.Error()
	}
	var le *os.LinkError
	if errors.As(err, &le) {
		return le.Err.Error()
	}
	return err.Error()
}

// openHandoffDir opens dir as the confinement root for handoff files after
// checking that it is a real directory (not a symbolic link) owned by the
// current user with no group or other permissions. It returns errNoDir when
// the directory does not exist.
func openHandoffDir(dir string) (*os.Root, error) {
	info, err := os.Lstat(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, errNoDir
		}
		return nil, stateErr("handoff directory %s cannot be inspected: %s", dir, errText(err))
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return nil, stateErr("handoff directory %s is a symbolic link; use a real directory", dir)
	}
	if !info.IsDir() {
		return nil, stateErr("handoff path %s is not a directory", dir)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, stateErr("handoff directory %s cannot be opened: %s", dir, errText(err))
	}
	opened, err := root.Stat(".")
	if err != nil || !opened.IsDir() || !os.SameFile(info, opened) {
		root.Close()
		return nil, stateErr("handoff directory %s changed while it was opened", dir)
	}
	if !ownedByCurrentUser(opened) {
		root.Close()
		return nil, stateErr("handoff directory %s is not owned by the current user", dir)
	}
	if opened.Mode().Perm()&0o077 != 0 {
		root.Close()
		return nil, stateErr("handoff directory %s is accessible to other users (mode %04o); run chmod 700 on it", dir, opened.Mode().Perm())
	}
	return root, nil
}

// canonical resolves symbolic links in the longest existing ancestor of p and
// appends the remaining cleaned components.
func canonical(p string) (string, error) {
	p = filepath.Clean(p)
	var rest []string
	for cur := p; ; {
		resolved, err := filepath.EvalSymlinks(cur)
		if err == nil {
			for i := len(rest) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, rest[i])
			}
			return resolved, nil
		}
		// Missing components can be appended to a resolved ancestor, but a
		// dangling symlink cannot: doing so would discard its target and
		// could hide an overlap with a history root. Fail closed instead.
		if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		if info, lerr := os.Lstat(cur); lerr == nil && info.Mode()&fs.ModeSymlink != 0 {
			return "", fmt.Errorf("cannot resolve dangling symbolic link %s", cur)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return p, nil
		}
		rest = append(rest, filepath.Base(cur))
		cur = parent
	}
}

// within reports whether p equals root or lies beneath it, component-wise.
func within(p, root string) bool {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// existingAncestors returns FileInfo for p and each of its ancestors that
// exists, following symbolic links.
func existingAncestors(p string) []os.FileInfo {
	var out []os.FileInfo
	for cur := filepath.Clean(p); ; {
		if info, err := os.Stat(cur); err == nil {
			out = append(out, info)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return out
		}
		cur = parent
	}
}

// checkExclusion rejects a handoff directory that equals, is inside, or
// contains any configured source root, enabled or not. Paths are compared
// lexically after canonicalization and by file identity, which also catches
// aliases on case-insensitive volumes.
func checkExclusion(dir string, sources []config.Source) *history.Error {
	cdir, err := canonical(dir)
	if err != nil {
		return stateErr("handoff directory %s cannot be resolved: %s", dir, errText(err))
	}
	dirAncestors := existingAncestors(cdir)
	dirInfo, dirErr := os.Stat(cdir)
	for _, src := range sources {
		croot, err := canonical(src.Root)
		if err != nil {
			return errorf(history.CodeInvalidConfig, "source %s root cannot be resolved to check the handoff directory: %s", src.ID, errText(err))
		}
		overlap := within(cdir, croot) || within(croot, cdir)
		if !overlap {
			if rootInfo, err := os.Stat(croot); err == nil {
				for _, a := range dirAncestors {
					if os.SameFile(a, rootInfo) {
						overlap = true
						break
					}
				}
			}
		}
		if !overlap && dirErr == nil {
			for _, a := range existingAncestors(croot) {
				if os.SameFile(a, dirInfo) {
					overlap = true
					break
				}
			}
		}
		if overlap {
			return errorf(history.CodeInvalidConfig, "handoff directory %s overlaps source %s root %s; set XDG_STATE_HOME outside history roots", dir, src.ID, src.Root)
		}
	}
	return nil
}

// readStored reads and validates one stored handoff through root. It returns
// fs.ErrNotExist when the file is absent, a message describing an invalid
// file, or an I/O error.
func readStored(root *os.Root, id string) (*Handoff, string, error) {
	f, _, err := openRegular(root, fileName(id))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, "", fs.ErrNotExist
		}
		if errors.Is(err, errSymlink) || errors.Is(err, errNotRegular) {
			return nil, "handoff entry is not a regular file", nil
		}
		return nil, "", err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, MaxHandoffBytes+1))
	if err != nil {
		return nil, "", err
	}
	h, msg := decodeStored(data, id)
	if msg != "" {
		return nil, msg, nil
	}
	return h, "", nil
}

func randomHex(r io.Reader) (string, error) {
	var b [8]byte
	if _, err := io.ReadFull(r, b[:]); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", b[:]), nil
}
