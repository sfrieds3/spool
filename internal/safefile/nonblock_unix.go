//go:build unix

package safefile

import "syscall"

// openNonblock prevents opening a FIFO from blocking if a regular file is
// replaced between the type check and the open.
const openNonblock = syscall.O_NONBLOCK
