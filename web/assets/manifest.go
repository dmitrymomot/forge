package assets

import "io/fs"

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
