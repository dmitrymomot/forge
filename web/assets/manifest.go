package assets

import (
	"encoding/json"
	"fmt"
	"io/fs"
)

// buildRuntime walks fsys, fingerprints every file, and returns the
// logical→Entry table plus the served-path→real-name reverse map.
func buildRuntime(fsys fs.FS) (map[string]Entry, map[string]string, error) {
	table := map[string]Entry{}
	reverse := map[string]string{}
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		hashed := injectHash(p, shortHash(data))
		table[p] = Entry{Path: hashed, Integrity: sri(data), real: p}
		reverse[hashed] = p
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return table, reverse, nil
}

// readFlatManifest parses a flat JSON manifest at path within fsys. Each value
// is either a hashed filename string, or an object {"file","integrity"}. It
// returns an unwrapped fs.ErrNotExist when the file is absent so the caller can
// fall back to runtime fingerprinting, and an ErrManifest-wrapped error when the
// JSON is malformed.
func readFlatManifest(fsys fs.FS, path string) (map[string]Entry, error) {
	data, err := fs.ReadFile(fsys, path)
	if err != nil {
		return nil, err // may be fs.ErrNotExist → caller falls back to runtime
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrManifest, err)
	}
	out := make(map[string]Entry, len(raw))
	for logical, rm := range raw {
		var s string
		if json.Unmarshal(rm, &s) == nil {
			out[logical] = Entry{Path: s}
			continue
		}
		var o struct {
			File      string `json:"file"`
			Integrity string `json:"integrity"`
		}
		if err := json.Unmarshal(rm, &o); err != nil || o.File == "" {
			return nil, fmt.Errorf("%w: entry %q", ErrManifest, logical)
		}
		out[logical] = Entry{Path: o.File, Integrity: o.Integrity}
	}
	return out, nil
}
