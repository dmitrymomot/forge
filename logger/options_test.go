package logger

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWithConfigReplacesBlock(t *testing.T) {
	c := defaultConfig()
	WithConfig(Config{Level: "debug", Format: "json", File: "/tmp/x"})(&c)
	assert.Equal(t, "debug", c.Level)
	assert.Equal(t, "json", c.Format)
	assert.Equal(t, "/tmp/x", c.File)
	assert.Nil(t, c.levelOverride)
	assert.Nil(t, c.formatOverride)
	assert.Nil(t, c.outputOverride)
	assert.Empty(t, c.extractors)
	assert.Empty(t, c.extraHandlers)
	assert.Empty(t, c.errs)
}

func TestWithLevelAndFormatOverrides(t *testing.T) {
	c := defaultConfig()
	WithLevel(slog.LevelError)(&c)
	WithFormat(FormatJSON)(&c)
	require.NotNil(t, c.levelOverride)
	require.NotNil(t, c.formatOverride)
	assert.Equal(t, slog.LevelError, *c.levelOverride)
	assert.Equal(t, FormatJSON, *c.formatOverride)
}

func TestWithFileSetsConfigAndRejectsEmpty(t *testing.T) {
	c := defaultConfig()
	WithFile("/var/log/app.log")(&c)
	assert.Equal(t, "/var/log/app.log", c.File)
	assert.Empty(t, c.errs)

	c2 := defaultConfig()
	WithFile("")(&c2)
	require.Len(t, c2.errs, 1)
	assert.ErrorIs(t, c2.errs[0], ErrInvalidConfig)
	assert.Empty(t, c2.File)
}

func TestWithOutputRejectsNil(t *testing.T) {
	c := defaultConfig()
	WithOutput(nil)(&c)
	require.Len(t, c.errs, 1)
	assert.ErrorIs(t, c.errs[0], ErrInvalidConfig)
	assert.Nil(t, c.outputOverride)
}

func TestWithHandlerAppendsAndRejectsNil(t *testing.T) {
	c := defaultConfig()
	WithHandler(slog.NewJSONHandler(nil, nil))(&c)
	assert.Len(t, c.extraHandlers, 1)

	c2 := defaultConfig()
	WithHandler(nil)(&c2)
	require.Len(t, c2.errs, 1)
	assert.ErrorIs(t, c2.errs[0], ErrInvalidConfig)
	assert.Empty(t, c2.extraHandlers)
}

func TestWithContextExtractorsFiltersNil(t *testing.T) {
	c := defaultConfig()
	ex := func(context.Context) (slog.Attr, bool) { return slog.Attr{}, false }
	WithContextExtractors(ex, nil, ex)(&c)
	assert.Len(t, c.extractors, 2)
	assert.Empty(t, c.errs)
}

func TestWithOutputStoresWriter(t *testing.T) {
	c := defaultConfig()
	var buf bytes.Buffer
	WithOutput(&buf)(&c)
	assert.Equal(t, &buf, c.outputOverride)
	assert.Empty(t, c.errs)
}

func TestWithConfigOrderDependence(t *testing.T) {
	// A later WithConfig replaces the whole block, dropping an earlier WithFile.
	c := defaultConfig()
	WithFile("/tmp/first")(&c)
	WithConfig(Config{Level: "info", Format: "text"})(&c)
	assert.Empty(t, c.File, "later WithConfig must replace the block set by WithFile")

	// WithFile after WithConfig is kept.
	c2 := defaultConfig()
	WithConfig(Config{Level: "info", Format: "text"})(&c2)
	WithFile("/tmp/second")(&c2)
	assert.Equal(t, "/tmp/second", c2.File)
}
