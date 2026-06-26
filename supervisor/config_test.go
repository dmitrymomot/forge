package supervisor_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/supervisor"
)

func TestExportedDefaultConfig(t *testing.T) {
	cfg := supervisor.DefaultConfig()
	assert.Equal(t, 30*time.Second, cfg.ShutdownTimeout)
	assert.True(t, cfg.Recover)
}

func TestConfig_Validate(t *testing.T) {
	require.NoError(t, supervisor.DefaultConfig().Validate())
	require.NoError(t, supervisor.Config{ShutdownTimeout: 0, Recover: false}.Validate())

	err := supervisor.Config{ShutdownTimeout: -1}.Validate()
	require.Error(t, err)
	assert.ErrorIs(t, err, supervisor.ErrInvalidConfig)
}

func TestConfig_EnvTags(t *testing.T) {
	want := map[string]string{
		"ShutdownTimeout": "SHUTDOWN_TIMEOUT",
		"Recover":         "RECOVER",
	}
	typ := reflect.TypeFor[supervisor.Config]()
	for name, tag := range want {
		f, ok := typ.FieldByName(name)
		require.Truef(t, ok, "field %s missing", name)
		assert.Equalf(t, tag, f.Tag.Get("env"), "field %s env tag", name)
	}
}
