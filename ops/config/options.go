package config

import "os"

type config struct {
	fileNameFn  func(profile string) string
	lookup      func(string) (string, bool)
	profile     string
	dotenvPaths []string
}

func newConfig(opts ...Option) config {
	c := config{
		fileNameFn: func(p string) string { return p + ".yaml" },
		lookup:     os.LookupEnv,
	}
	for _, o := range opts {
		o(&c)
	}
	return c
}

func (c config) activeProfile() string {
	if c.profile != "" {
		return c.profile
	}
	return Profile()
}

func (c config) fileName() string {
	return c.fileNameFn(c.activeProfile())
}

func (c config) applyDotenv() error {
	if len(c.dotenvPaths) == 0 {
		return nil
	}
	return Dotenv(c.dotenvPaths...)
}

// Option configures Load/LoadEnv/Populate.
type Option func(*config)

// WithDotenv loads these .env files (via Dotenv) before reading config.
func WithDotenv(paths ...string) Option {
	return func(c *config) { c.dotenvPaths = append(c.dotenvPaths, paths...) }
}

// WithProfile overrides APP_ENV/ENV detection for file selection.
func WithProfile(name string) Option { return func(c *config) { c.profile = name } }

// WithFileName customizes the profile→filename mapping (default profile+".yaml").
func WithFileName(fn func(profile string) string) Option {
	return func(c *config) {
		if fn != nil {
			c.fileNameFn = fn
		}
	}
}

// WithLookup overrides os.LookupEnv (test seam) for substitution and env reads.
func WithLookup(fn func(key string) (string, bool)) Option {
	return func(c *config) {
		if fn != nil {
			c.lookup = fn
		}
	}
}
