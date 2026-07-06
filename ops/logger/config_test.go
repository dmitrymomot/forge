package logger

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultConfig(t *testing.T) {
	c := DefaultConfig()
	assert.Equal(t, "info", c.Level)
	assert.Equal(t, "text", c.Format)
	assert.Empty(t, c.File)
	assert.False(t, c.AddSource)
}

func TestValidate(t *testing.T) {
	require.NoError(t, DefaultConfig().Validate())
	require.NoError(t, Config{Level: "WARNING", Format: "JSON"}.Validate()) // case-insensitive + alias

	err := Config{Level: "loud", Format: "text"}.Validate()
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidConfig)

	err = Config{Level: "info", Format: "yaml"}.Validate()
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidConfig)
}

func TestParseLevel(t *testing.T) {
	for in, want := range map[string]string{
		" debug ": "DEBUG", "INFO": "INFO", "warn": "WARN", "warning": "WARN", "Error": "ERROR",
	} {
		assert.Equal(t, want, parseLevel(in).String(), "input %q", in)
	}
}

func TestParseFormat(t *testing.T) {
	assert.Equal(t, FormatText, parseFormat("TEXT"))
	assert.Equal(t, FormatJSON, parseFormat(" json "))
}

func TestConfigEnvTags(t *testing.T) {
	typ := reflect.TypeFor[Config]()
	for field, tag := range map[string]string{
		"Level": "LOG_LEVEL", "Format": "LOG_FORMAT", "File": "LOG_FILE", "AddSource": "LOG_ADD_SOURCE",
	} {
		f, ok := typ.FieldByName(field)
		require.True(t, ok, "missing field %s", field)
		assert.Equal(t, tag, f.Tag.Get("env"))
	}
}
