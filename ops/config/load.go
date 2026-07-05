package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Load reads {dir}/{profile}.yaml, expands ${VAR:default} placeholders against
// the environment layer, and unmarshals into T. If *T implements
// interface{ SetDefaults() } it is applied before decode (yaml overwrites only
// present keys); interface{ Validate() error } runs after.
func Load[T any](dir string, opts ...Option) (T, error) {
	var t T
	c := newConfig(opts...)
	if err := c.applyDotenv(); err != nil {
		return t, err
	}
	path := filepath.Join(dir, c.fileName())
	data, err := os.ReadFile(path)
	if err != nil {
		return t, fmt.Errorf("%w: %v", ErrProfileFile, err)
	}
	expanded, err := substitute(string(data), c.lookup)
	if err != nil {
		return t, err
	}
	if sd, ok := any(&t).(interface{ SetDefaults() }); ok {
		sd.SetDefaults()
	}
	if err := yaml.Unmarshal([]byte(expanded), &t); err != nil {
		return t, fmt.Errorf("%w: %v", ErrYAML, err)
	}
	if v, ok := any(&t).(interface{ Validate() error }); ok {
		if err := v.Validate(); err != nil {
			return t, err
		}
	}
	return t, nil
}
