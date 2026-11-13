//go:build !unix

package handoff

import (
	"os"
	"testing"
)

// makeFifo substitutes a directory, another non-regular entry, where FIFOs
// are unavailable.
func makeFifo(t *testing.T, path string) {
	t.Helper()
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
}
