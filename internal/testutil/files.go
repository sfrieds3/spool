// Package testutil provides generic fixture file helpers for tests only.
package testutil

import (
	"os"
	"path/filepath"
	"testing"
)

// Executable creates a standalone fake shell executable in an isolated directory.
func Executable(t testing.TB, name, script string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nset -eu\n"+script), 0o700); err != nil {
		t.Fatalf("create fake executable %q: %v", path, err)
	}
	return path
}
