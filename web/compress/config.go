package compress

import (
	"compress/gzip"
	"errors"
	"fmt"
)

// ErrInvalidConfig marks an out-of-range Level or negative MinSize.
var ErrInvalidConfig = errors.New("compress: invalid config")

// Config is the env-loadable compression policy.
type Config struct {
	MinSize int `env:"COMPRESS_MIN_SIZE"` // bytes buffered before compressing kicks in
	Level   int `env:"COMPRESS_LEVEL"`    // gzip/flate level
}

// DefaultConfig returns MinSize=1024 and the default compression level.
func DefaultConfig() Config {
	return Config{MinSize: 1024, Level: gzip.DefaultCompression}
}

// Validate checks Level against the gzip/flate range and MinSize >= 0.
func (c Config) Validate() error {
	if c.Level != gzip.DefaultCompression && (c.Level < gzip.HuffmanOnly || c.Level > gzip.BestCompression) {
		return fmt.Errorf("%w: level %d", ErrInvalidConfig, c.Level)
	}
	if c.MinSize < 0 {
		return fmt.Errorf("%w: negative MinSize", ErrInvalidConfig)
	}
	return nil
}
