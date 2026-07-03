package redis_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	forgeredis "github.com/dmitrymomot/forge/data/redis"
)

func TestDefaultConfig(t *testing.T) {
	cfg := forgeredis.DefaultConfig()
	assert.Equal(t, 10, cfg.PoolSize)
	assert.Equal(t, 5*time.Second, cfg.DialTimeout)
	assert.Equal(t, 3*time.Second, cfg.ReadTimeout)
	assert.Equal(t, 3*time.Second, cfg.WriteTimeout)
	assert.Equal(t, 3, cfg.RetryAttempts)
	assert.Equal(t, time.Second, cfg.RetryInterval)
	// Fields left at zero / driver-default on purpose.
	assert.Empty(t, cfg.Addresses, "Addresses has no default; the consumer must supply it")
	assert.Empty(t, cfg.MasterName)
	assert.Empty(t, cfg.Username)
	assert.Empty(t, cfg.Password)
	assert.Zero(t, cfg.DB)
	assert.Zero(t, cfg.MinIdleConns)
	assert.Zero(t, cfg.ConnMaxIdleTime)
	// DefaultConfig alone is not valid: Addresses is required.
	require.Error(t, cfg.Validate())
}

func TestConfig_Validate(t *testing.T) {
	good := forgeredis.DefaultConfig()
	good.Addresses = []string{"127.0.0.1:6379"}
	require.NoError(t, good.Validate())

	bad := map[string]forgeredis.Config{
		"empty addresses":    {Addresses: nil},
		"neg dial timeout":   {Addresses: []string{"127.0.0.1:6379"}, DialTimeout: -1},
		"neg read timeout":   {Addresses: []string{"127.0.0.1:6379"}, ReadTimeout: -1},
		"neg write timeout":  {Addresses: []string{"127.0.0.1:6379"}, WriteTimeout: -1},
		"neg conn idle":      {Addresses: []string{"127.0.0.1:6379"}, ConnMaxIdleTime: -1},
		"neg retry attempts": {Addresses: []string{"127.0.0.1:6379"}, RetryAttempts: -1},
		"neg retry interval": {Addresses: []string{"127.0.0.1:6379"}, RetryInterval: -1},
	}
	for name, cfg := range bad {
		t.Run(name, func(t *testing.T) {
			err := cfg.Validate()
			require.Error(t, err)
			assert.ErrorIs(t, err, forgeredis.ErrInvalidConfig)
		})
	}
}

func TestConfig_EnvTags(t *testing.T) {
	want := map[string]string{
		"Addresses":       "ADDRESSES",
		"MasterName":      "MASTER_NAME",
		"Username":        "USERNAME",
		"Password":        "PASSWORD",
		"DB":              "DB",
		"PoolSize":        "POOL_SIZE",
		"MinIdleConns":    "MIN_IDLE_CONNS",
		"DialTimeout":     "DIAL_TIMEOUT",
		"ReadTimeout":     "READ_TIMEOUT",
		"WriteTimeout":    "WRITE_TIMEOUT",
		"ConnMaxIdleTime": "CONN_MAX_IDLE_TIME",
		"RetryAttempts":   "RETRY_ATTEMPTS",
		"RetryInterval":   "RETRY_INTERVAL",
	}
	typ := reflect.TypeFor[forgeredis.Config]()
	for fname, tag := range want {
		f, ok := typ.FieldByName(fname)
		require.Truef(t, ok, "field %s missing", fname)
		assert.Equalf(t, tag, f.Tag.Get("env"), "field %s env tag", fname)
	}
}

func TestErrSentinels_Distinct(t *testing.T) {
	// The three sentinels are distinct, matchable values.
	assert.NotErrorIs(t, forgeredis.ErrConnect, forgeredis.ErrInvalidConfig)
	assert.NotErrorIs(t, forgeredis.ErrHealthcheck, forgeredis.ErrConnect)
	assert.NotErrorIs(t, forgeredis.ErrInvalidConfig, forgeredis.ErrHealthcheck)
}
