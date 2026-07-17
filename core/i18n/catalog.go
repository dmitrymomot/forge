package i18n

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"strings"
)

// rawCatalog is one locale's parsed, flattened messages before compilation.
type rawCatalog struct {
	// messages maps a flattened dot-notation key to its template.
	messages map[string]string
	// plurals maps a flattened key to its per-category templates. A key
	// appears in exactly one of messages or plurals, never both.
	plurals map[string]map[PluralCategory]string
}

func newRawCatalog() *rawCatalog {
	return &rawCatalog{
		messages: make(map[string]string),
		plurals:  make(map[string]map[PluralCategory]string),
	}
}

// isPluralMap reports whether a JSON object is a plural message: every key is
// a CLDR category name and every value is a string. Anything else is a nested
// namespace, so {"save": "Save"} recurses and {"one": "x"} does not.
func isPluralMap(m map[string]any) bool {
	if len(m) == 0 {
		return false
	}
	for k, v := range m {
		if _, ok := categoryByName(k); !ok {
			return false
		}
		if _, ok := v.(string); !ok {
			return false
		}
	}
	return true
}

// loadFS walks fsys for {tag}/{namespace}.json and returns one rawCatalog per
// locale tag. The returned map's key set IS the bundle's supported locale
// set: this package validates tags against no list, so any directory that
// normalizes to a tag is a supported locale.
func loadFS(fsys fs.FS) (map[string]*rawCatalog, error) {
	out := make(map[string]*rawCatalog)
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(p, ".json") {
			return nil
		}
		dir, file := path.Split(p)
		dir = strings.TrimSuffix(dir, "/")
		if dir == "" || strings.Contains(dir, "/") {
			return nil // only {tag}/{ns}.json is a catalog; ignore anything else
		}
		tag := normalizeTag(dir)
		if tag == "" {
			return fmt.Errorf("%w: directory %q is not a language tag", ErrInvalidCatalog, dir)
		}
		b, err := fs.ReadFile(fsys, p)
		if err != nil {
			return fmt.Errorf("%w: read %s: %w", ErrInvalidCatalog, p, err)
		}
		var data map[string]any
		if err := json.Unmarshal(b, &data); err != nil {
			return fmt.Errorf("%w: parse %s: %w", ErrInvalidCatalog, p, err)
		}
		rc, ok := out[tag]
		if !ok {
			rc = newRawCatalog()
			out[tag] = rc
		}
		ns := strings.TrimSuffix(file, ".json")
		if err := rc.addNamespace(ns, data); err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// addNamespace flattens data under the namespace prefix into rc.
func (rc *rawCatalog) addNamespace(ns string, data map[string]any) error {
	return rc.flatten(ns, data)
}

func (rc *rawCatalog) flatten(prefix string, data map[string]any) error {
	for k, v := range data {
		key := k
		if prefix != "" {
			key = prefix + "." + k
		}
		switch val := v.(type) {
		case map[string]any:
			if isPluralMap(val) {
				if err := rc.setPlural(key, val); err != nil {
					return err
				}
				continue
			}
			if err := rc.flatten(key, val); err != nil {
				return err
			}
		case string:
			if err := rc.setMessage(key, val); err != nil {
				return err
			}
		default:
			// JSON numbers, bools: a translator writing 42 means "42".
			if err := rc.setMessage(key, fmt.Sprint(val)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (rc *rawCatalog) setMessage(key, tmpl string) error {
	if _, dup := rc.messages[key]; dup {
		return fmt.Errorf("%w: %q", ErrDuplicateKey, key)
	}
	if _, dup := rc.plurals[key]; dup {
		return fmt.Errorf("%w: %q defined as both a message and a plural", ErrDuplicateKey, key)
	}
	// A plain message may not shadow a plural form of an already-flattened
	// plural key: split at the LAST dot, since the base of a namespaced key
	// like "app.x.one" is "app.x", not "app" (a first-dot split would miss
	// this and only catch the collision from one iteration order).
	if idx := strings.LastIndex(key, "."); idx >= 0 {
		base, form := key[:idx], key[idx+1:]
		if _, isCat := categoryByName(form); isCat {
			if _, isPlural := rc.plurals[base]; isPlural {
				return fmt.Errorf("%w: %q collides with plural key %q", ErrDuplicateKey, key, base)
			}
		}
	}
	rc.messages[key] = tmpl
	return nil
}

func (rc *rawCatalog) setPlural(key string, forms map[string]any) error {
	if _, dup := rc.plurals[key]; dup {
		return fmt.Errorf("%w: %q", ErrDuplicateKey, key)
	}
	if _, dup := rc.messages[key]; dup {
		return fmt.Errorf("%w: %q defined as both a message and a plural", ErrDuplicateKey, key)
	}
	m := make(map[PluralCategory]string, len(forms))
	for name, v := range forms {
		cat, ok := categoryByName(name)
		if !ok {
			return fmt.Errorf("%w: %q has non-category form %q", ErrInvalidCatalog, key, name)
		}
		s, ok := v.(string)
		if !ok {
			return fmt.Errorf("%w: %q form %q is not a string", ErrInvalidCatalog, key, name)
		}
		m[cat] = s
	}
	// A plural key may not collide with an already-flattened "key.form"
	// message set from the other iteration order.
	for name := range forms {
		if _, dup := rc.messages[key+"."+name]; dup {
			return fmt.Errorf("%w: plural %q collides with message %q", ErrDuplicateKey, key, key+"."+name)
		}
	}
	rc.plurals[key] = m
	return nil
}
