//go:build !unix

package handoff

import "os"

func ownedByCurrentUser(os.FileInfo) bool { return true }
