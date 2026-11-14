package handoff

import (
	"os"

	"github.com/sfrieds3/spool/internal/safefile"
)

var (
	errSymlink    = safefile.ErrSymlink
	errNotRegular = safefile.ErrNotRegular
)

func openRegular(root *os.Root, name string) (*os.File, os.FileInfo, error) {
	return safefile.OpenRegular(root, name)
}
