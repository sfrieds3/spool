package history

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"sort"

	"github.com/sfrieds3/spool/internal/safefile"
)

var (
	errSymlink    = safefile.ErrSymlink
	errNotRegular = safefile.ErrNotRegular
)

// openSourceRoot opens a source root. The root itself may be a symbolic link
// chosen by the user; it is resolved once here and the opened directory is
// the confinement boundary for all later access.
func openSourceRoot(root string) (*os.Root, error) {
	return os.OpenRoot(root)
}

type walkIssue struct {
	code, msg, path string
}

// discover returns the sorted source-relative slash paths of candidate files
// beneath root. Nested symbolic links are never followed, and special files
// are skipped. It reads directory entries only.
func discover(ctx context.Context, root *os.Root, candidate func(string) bool) ([]string, []walkIssue, error) {
	var files []string
	var issues []walkIssue
	err := fs.WalkDir(root.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
		if err != nil {
			if p == "." {
				return err
			}
			issues = append(issues, walkIssue{DiagDirectoryUnreadable, "directory cannot be read: " + errText(err), p})
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if p == "." {
			return nil
		}
		mode := d.Type()
		switch {
		case mode&fs.ModeSymlink != 0:
			issues = append(issues, walkIssue{DiagSymlinkSkipped, "nested symbolic link is not followed", p})
			return nil
		case d.IsDir():
			return nil
		case !candidate(p):
			return nil
		case !mode.IsRegular():
			issues = append(issues, walkIssue{DiagNotRegularFile, "candidate is not a regular file", p})
			return nil
		}
		if msg := validRelPath(p); msg != "" {
			issues = append(issues, walkIssue{DiagFileUnreadable, "candidate path cannot be referenced: " + msg, ""})
			return nil
		}
		files = append(files, p)
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	sort.Strings(files)
	return files, issues, nil
}

// openRegular opens a regular file at a slash path beneath root without
// following symbolic links in any component, and without blocking on special
// files.
func openRegular(root *os.Root, rel string) (*os.File, os.FileInfo, error) {
	return safefile.OpenRegular(root, rel)
}

// errText reduces filesystem errors to their cause, omitting absolute paths
// that the caller reports separately.
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
