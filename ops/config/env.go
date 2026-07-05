package config

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/dmitrymomot/forge/core/structfields"
)

// LoadEnv populates a fresh T from environment variables via its `env` tags.
func LoadEnv[T any](opts ...Option) (T, error) {
	var t T
	if err := Populate(&t, opts...); err != nil {
		return t, err
	}
	return t, nil
}

// Populate fills dst (a non-nil *struct) from the environment via `env` tags,
// applying `default=` options and failing on missing `required` keys. If dst
// implements interface{ Validate() error }, that runs afterward.
func Populate(dst any, opts ...Option) error {
	c := newConfig(opts...)
	if err := c.applyDotenv(); err != nil {
		return err
	}
	if err := populateStruct(dst, "", c.lookup); err != nil {
		return err
	}
	if v, ok := dst.(interface{ Validate() error }); ok {
		if err := v.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func populateStruct(dst any, prefix string, lookup func(string) (string, bool)) error {
	var errs []error
	walkErr := structfields.Walk(dst, "env", func(f structfields.Field) error {
		if f.Tag.Ignored() {
			return nil
		}
		if f.Value.Kind() == reflect.Struct && f.Value.Type() != reflect.TypeFor[time.Time]() {
			sub := prefix
			if f.Tag.Name != "" {
				sub = prefix + f.Tag.Name + "_"
			}
			if err := populateStruct(f.Value.Addr().Interface(), sub, lookup); err != nil {
				errs = append(errs, err)
			}
			return nil
		}
		if f.Tag.Name == "" {
			return nil
		}
		key := prefix + f.Tag.Name
		raw, ok := lookup(key)
		if !ok || raw == "" {
			def, hasDefault := defaultOption(f)
			switch {
			case hasDefault:
				raw = def
			case f.Tag.HasOption("required"):
				errs = append(errs, fmt.Errorf("%w: %s", ErrRequiredMissing, key))
				return nil
			default:
				return nil
			}
		}
		if err := structfields.SetString(f, raw); err != nil {
			errs = append(errs, fmt.Errorf("%w: %s: %v", ErrParse, key, err))
		}
		return nil
	})
	if walkErr != nil {
		return walkErr
	}
	return errors.Join(errs...)
}

func defaultOption(f structfields.Field) (string, bool) {
	for _, o := range f.Tag.Options {
		if v, ok := strings.CutPrefix(o, "default="); ok {
			return v, true
		}
	}
	return "", false
}
