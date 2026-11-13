// Package strictjson decodes untrusted JSON documents strictly: one value,
// no duplicate keys at any depth, no unknown fields, and optional checks that
// required keys are present even when their value is null.
package strictjson

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
)

// CheckDuplicateKeys rejects documents that repeat a key in any object.
func CheckDuplicateKeys(data []byte) error {
	_, err := walk(data, false)
	return err
}

// Keys returns the key set of every object in the document, indexed by a
// path such as "", "workspace", or "sessions[]". Array elements share the
// path of their array with a "[]" suffix, so a key is reported present for a
// path only when every object at that path has it. Duplicate keys are
// rejected.
func Keys(data []byte) (map[string][]map[string]bool, error) {
	return walk(data, true)
}

func walk(data []byte, collect bool) (map[string][]map[string]bool, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var out map[string][]map[string]bool
	if collect {
		out = map[string][]map[string]bool{}
	}
	var visit func(path string) error
	visit = func(path string) error {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		delim, ok := tok.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			keys := map[string]bool{}
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return err
				}
				key := kt.(string)
				if keys[key] {
					return fmt.Errorf("duplicate key %q", key)
				}
				keys[key] = true
				child := key
				if path != "" {
					child = path + "." + key
				}
				if err := visit(child); err != nil {
					return err
				}
			}
			if collect {
				out[path] = append(out[path], keys)
			}
		case '[':
			for dec.More() {
				if err := visit(path + "[]"); err != nil {
					return err
				}
			}
		}
		_, err = dec.Token()
		return err
	}
	if err := visit(""); err != nil {
		return nil, err
	}
	return out, nil
}

// Decode decodes exactly one JSON value into v, rejecting unknown fields and
// trailing data.
func Decode(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		return errors.New("unexpected trailing data")
	}
	return nil
}

// RequireKeys checks that every object found at each path in required has
// all listed keys. Paths absent from the document are not checked.
func RequireKeys(keys map[string][]map[string]bool, required map[string][]string) error {
	paths := make([]string, 0, len(required))
	for p := range required {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		for _, obj := range keys[p] {
			var missing []string
			for _, k := range required[p] {
				if !obj[k] {
					missing = append(missing, k)
				}
			}
			if len(missing) > 0 {
				where := "object"
				if p != "" {
					where = strings.TrimSuffix(p, "[]")
				}
				return fmt.Errorf("%s is missing %s", where, strings.Join(missing, ", "))
			}
		}
	}
	return nil
}
