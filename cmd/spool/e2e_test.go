package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// tree records every entry's path, type, and content hash beneath root.
func tree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		v := fmt.Sprintf("%v %d", info.Mode(), info.ModTime().UnixNano())
		if d.Type().IsRegular() {
			data, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			sum := sha256.Sum256(data)
			v += " " + hex.EncodeToString(sum[:])
		}
		out[p] = v
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestEndToEndAllHarnesses(t *testing.T) {
	w := newWorkspace(t)
	// A file outside every source root that traversal attempts target.
	outside := filepath.Join(filepath.Dir(w.dir), "outside.jsonl")
	w.write(t, outside, `{"type":"session","version":3,"id":"outside"}`+"\n")
	sessionsBefore := tree(t, w.dir)
	homeBefore := tree(t, w.home)

	list := spool(t, w, "list", "--config", w.config)
	if list.code != 0 || list.json["partial"] != false {
		t.Fatalf("list = %s", list.stdout)
	}
	harnesses := map[string]int{}
	for _, s := range list.sessions() {
		harnesses[s["harness"].(string)]++
		ref := s["ref"].(string)
		offset := "0"
		for pages := 0; ; pages++ {
			if pages > 20 {
				t.Fatal("pagination did not terminate")
			}
			r := spool(t, w, "read", "--config", w.config, "--limit", "3", "--offset", offset, ref)
			if r.code != 0 || r.json["content"] != "untrusted_history" || r.stderr != "" {
				t.Fatalf("read %s = %d %s", s["path"], r.code, r.stdout)
			}
			if r.json["eof"] == true {
				break
			}
			offset = string(r.json["next_offset"].(interface{ String() string }).String())
		}
	}
	if harnesses["pi"] != 1 || harnesses["claude"] != 2 || harnesses["codex"] != 2 {
		t.Fatalf("harnesses = %v", harnesses)
	}

	// Traversal and forged references fail without reading outside files.
	enc := func(s string) string { return "s1." + base64.RawURLEncoding.EncodeToString([]byte(s)) }
	for _, ref := range []string{
		enc(`["pi","pi","../outside.jsonl","outside"]`),
		enc(`["pi","pi","` + outside + `","outside"]`),
		enc(`["pi","pi","a/../../outside.jsonl","outside"]`),
		enc(`["elsewhere","pi","outside.jsonl","outside"]`),
	} {
		r := spool(t, w, "read", "--config", w.config, ref)
		if r.code != 2 || r.errorCode() != "invalid_request" || strings.Contains(r.stdout, `"outside"}`) {
			t.Fatalf("traversal %s = %d %s", ref, r.code, r.stdout)
		}
	}

	// Sources, directory entries, and the home/config directories are
	// unchanged: no caches or configuration files were created.
	if after := tree(t, w.dir); !equalMaps(sessionsBefore, after) {
		t.Fatal("session sources changed")
	}
	if after := tree(t, w.home); !equalMaps(homeBefore, after) {
		t.Fatalf("home changed: %v", keys(after))
	}
}

func equalMaps(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func keys(m map[string]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// TestNoProcessOrNetworkDependencies proves structurally that the spool
// binary cannot launch agents or open network clients: neither os/exec nor
// net is linked into it.
func TestNoProcessOrNetworkDependencies(t *testing.T) {
	gocmd, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go toolchain not on PATH")
	}
	out, err := exec.Command(gocmd, "list", "-deps", ".").Output()
	if err != nil {
		t.Fatal(err)
	}
	for _, dep := range strings.Fields(string(out)) {
		switch {
		case dep == "os/exec", dep == "net", strings.HasPrefix(dep, "net/http"), dep == "crypto/tls", dep == "plugin":
			t.Errorf("spool depends on %s", dep)
		}
	}
}
