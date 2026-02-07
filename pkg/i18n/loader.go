package i18n

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"strings"

	"gopkg.in/yaml.v3"
)

// WithJSONDir returns an Option that loads translations from JSON files in an fs.FS.
// The fs.FS root must contain language directories directly.
// File convention: {lang}/{namespace}.json
//
// Example structure:
//
//	en/common.json
//	en/errors.json
//	de/common.json
func WithJSONDir(fsys fs.FS) Option {
	return func(i *I18n) error {
		return loadDir(i, fsys, ".json", func(data []byte, v any) error {
			return json.Unmarshal(data, v)
		})
	}
}

// WithYAMLDir returns an Option that loads translations from YAML files in an fs.FS.
// The fs.FS root must contain language directories directly.
// File convention: {lang}/{namespace}.yaml or {lang}/{namespace}.yml
//
// Example structure:
//
//	en/common.yaml
//	fr/common.yml
func WithYAMLDir(fsys fs.FS) Option {
	return func(i *I18n) error {
		return loadDir(i, fsys, ".yaml", func(data []byte, v any) error {
			return yaml.Unmarshal(data, v)
		})
	}
}

func loadDir(i *I18n, fsys fs.FS, ext string, unmarshal func([]byte, any) error) error {
	return fs.WalkDir(fsys, ".", func(filePath string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			return nil
		}

		fileExt := strings.ToLower(path.Ext(filePath))

		// Case-insensitive comparison handles both .YAML and .yaml extensions across different systems
		var matches bool
		if ext == ".yaml" {
			matches = fileExt == ".yaml" || fileExt == ".yml"
		} else {
			matches = fileExt == ext
		}
		if !matches {
			return nil
		}

		// Extract lang from directory name and namespace from filename
		dir := path.Dir(filePath)
		if dir == "." || dir == "" {
			return fmt.Errorf("%w: file %q must be inside a language directory", ErrInvalidFile, filePath)
		}

		lang := path.Base(dir)
		namespace := strings.TrimSuffix(path.Base(filePath), path.Ext(filePath))

		data, err := fs.ReadFile(fsys, filePath)
		if err != nil {
			return fmt.Errorf("reading %q: %w", filePath, err)
		}

		var translations map[string]any
		if err := unmarshal(data, &translations); err != nil {
			return fmt.Errorf("%w: parsing %q: %s", ErrInvalidFile, filePath, err)
		}

		flattened := flattenTranslations(translations, "")

		for key, value := range flattened {
			compositeKey := buildKey(lang, namespace, key)
			i.translations[compositeKey] = value
		}

		if _, exists := i.pluralRules[lang]; !exists {
			i.pluralRules[lang] = GetPluralRuleForLanguage(lang)
		}

		return nil
	})
}
