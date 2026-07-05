package config_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/ops/config"
)

type dbCfg struct {
	Host string `env:"HOST,default=localhost"`
	Port int    `env:"PORT,default=5432"`
}

type appCfg struct {
	Name  string `env:"NAME,required"`
	Debug bool   `env:"DEBUG,default=false"`
	DB    dbCfg  `env:"DB"`
}

func TestLoadEnv_DefaultsAndNesting(t *testing.T) {
	lookup := func(k string) (string, bool) {
		m := map[string]string{"NAME": "svc", "DB_PORT": "6543"}
		v, ok := m[k]
		return v, ok
	}
	got, err := config.LoadEnv[appCfg](config.WithLookup(lookup))
	require.NoError(t, err)
	assert.Equal(t, "svc", got.Name)
	assert.False(t, got.Debug)                // default
	assert.Equal(t, "localhost", got.DB.Host) // nested default
	assert.Equal(t, 6543, got.DB.Port)        // nested env override, prefixed DB_
}

func TestLoadEnv_RequiredMissing(t *testing.T) {
	lookup := func(string) (string, bool) { return "", false }
	_, err := config.LoadEnv[appCfg](config.WithLookup(lookup))
	assert.ErrorIs(t, err, config.ErrRequiredMissing)
}

func TestLoadEnv_ParseError(t *testing.T) {
	lookup := func(k string) (string, bool) {
		if k == "NAME" {
			return "svc", true
		}
		if k == "DB_PORT" {
			return "not-int", true
		}
		return "", false
	}
	_, err := config.LoadEnv[appCfg](config.WithLookup(lookup))
	assert.ErrorIs(t, err, config.ErrParse)
}
