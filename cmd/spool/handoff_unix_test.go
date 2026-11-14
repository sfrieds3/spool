//go:build unix

package main

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestHandoffRejectsFIFOInput(t *testing.T) {
	w := newWorkspace(t)
	name := filepath.Join(w.home, "brief.fifo")
	if err := syscall.Mkfifo(name, 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := spoolRaw(t, w, "", "handoff", "create", "--config", w.config, "--input", name)
	r := decode(t, code, out, errOut)
	if code != 2 || r.errorCode() != "invalid_request" || errOut != "" {
		t.Fatalf("FIFO input = %d %s %q", code, out, errOut)
	}
	if _, err := os.Stat(w.state()); !os.IsNotExist(err) {
		t.Fatal("rejected input created state")
	}
}
