package config_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/ops/config"
)

type serverCfg struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
	Name string `yaml:"name"`
}

func (s *serverCfg) SetDefaults() {
	if s.Host == "" {
		s.Host = "127.0.0.1"
	}
}

func TestLoad_YAMLWithSubstitution(t *testing.T) {
	t.Setenv("HOST", "10.0.0.5")

	got, err := config.Load[serverCfg]("testdata", config.WithProfile("development"))
	require.NoError(t, err)
	assert.Equal(t, "10.0.0.5", got.Host) // ${HOST} substituted
	assert.Equal(t, 8080, got.Port)       // ${PORT:8080} default
	assert.Equal(t, "dev-service", got.Name)
}

func TestLoad_ProfileSelectionAndDefaults(t *testing.T) {
	t.Setenv("HOST", "") // empty -> ${HOST:0.0.0.0} uses default
	got, err := config.Load[serverCfg]("testdata", config.WithProfile("production"))
	require.NoError(t, err)
	assert.Equal(t, "0.0.0.0", got.Host)
	assert.Equal(t, 443, got.Port)
	assert.Equal(t, "prod-service", got.Name)
}

func TestLoad_MissingFile(t *testing.T) {
	_, err := config.Load[serverCfg]("testdata", config.WithProfile("nope"))
	assert.ErrorIs(t, err, config.ErrProfileFile)
}
