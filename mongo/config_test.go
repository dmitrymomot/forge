package mongo_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	forgemongo "github.com/dmitrymomot/forge/mongo"
)

func TestDefaultConfig(t *testing.T) {
	cfg := forgemongo.DefaultConfig()
	assert.Empty(t, cfg.URI, "URI has no default; it is required")
	assert.Empty(t, cfg.Database)
	assert.Equal(t, uint64(100), cfg.MaxPoolSize)
	assert.Equal(t, uint64(0), cfg.MinPoolSize)
	assert.Equal(t, 10*time.Second, cfg.ConnectTimeout)
	assert.Equal(t, 10*time.Second, cfg.ServerSelectionTimeout)
	assert.Equal(t, time.Duration(0), cfg.MaxConnIdleTime)
	assert.Empty(t, cfg.ReadPreference)
	assert.Empty(t, cfg.ReadConcern)
	assert.Empty(t, cfg.WriteConcern)
	assert.Equal(t, 3, cfg.RetryAttempts)
	assert.Equal(t, time.Second, cfg.RetryInterval)

	// DefaultConfig alone is not usable: URI and Database are required.
	require.ErrorIs(t, cfg.Validate(), forgemongo.ErrInvalidConfig)

	// With a URI but no Database it still fails.
	cfg.URI = "mongodb://127.0.0.1:27017"
	require.ErrorIs(t, cfg.Validate(), forgemongo.ErrInvalidConfig)

	// With both URI and Database it validates (empty concerns => driver defaults).
	cfg.Database = "forge_test"
	require.NoError(t, cfg.Validate())
}

func TestConfig_Validate(t *testing.T) {
	base := func() forgemongo.Config {
		c := forgemongo.DefaultConfig()
		c.URI = "mongodb://127.0.0.1:27017"
		c.Database = "forge_test"
		return c
	}

	t.Run("empty URI", func(t *testing.T) {
		c := base()
		c.URI = ""
		require.ErrorIs(t, c.Validate(), forgemongo.ErrInvalidConfig)
	})
	t.Run("empty Database", func(t *testing.T) {
		c := base()
		c.Database = ""
		require.ErrorIs(t, c.Validate(), forgemongo.ErrInvalidConfig)
	})
	t.Run("unknown ReadPreference", func(t *testing.T) {
		c := base()
		c.ReadPreference = "bogus"
		require.ErrorIs(t, c.Validate(), forgemongo.ErrInvalidConfig)
	})
	t.Run("unknown ReadConcern", func(t *testing.T) {
		c := base()
		c.ReadConcern = "bogus"
		require.ErrorIs(t, c.Validate(), forgemongo.ErrInvalidConfig)
	})
	t.Run("unknown WriteConcern", func(t *testing.T) {
		c := base()
		c.WriteConcern = "bogus"
		require.ErrorIs(t, c.Validate(), forgemongo.ErrInvalidConfig)
	})
	t.Run("negative RetryAttempts", func(t *testing.T) {
		c := base()
		c.RetryAttempts = -1
		require.ErrorIs(t, c.Validate(), forgemongo.ErrInvalidConfig)
	})
	t.Run("negative durations rejected", func(t *testing.T) {
		c := base()
		c.ConnectTimeout = -1
		require.ErrorIs(t, c.Validate(), forgemongo.ErrInvalidConfig)
	})

	t.Run("valid concern names accepted", func(t *testing.T) {
		c := base()
		c.ReadPreference = "secondaryPreferred"
		c.ReadConcern = "majority"
		c.WriteConcern = "majority"
		require.NoError(t, c.Validate())
	})
	t.Run("numeric WriteConcern accepted", func(t *testing.T) {
		c := base()
		c.WriteConcern = "2"
		require.NoError(t, c.Validate())
	})
}

func TestConfig_EnvTags(t *testing.T) {
	want := map[string]string{
		"URI":                    "URI",
		"Database":               "DATABASE",
		"MaxPoolSize":            "MAX_POOL_SIZE",
		"MinPoolSize":            "MIN_POOL_SIZE",
		"ConnectTimeout":         "CONNECT_TIMEOUT",
		"ServerSelectionTimeout": "SERVER_SELECTION_TIMEOUT",
		"MaxConnIdleTime":        "MAX_CONN_IDLE_TIME",
		"ReadPreference":         "READ_PREFERENCE",
		"ReadConcern":            "READ_CONCERN",
		"WriteConcern":           "WRITE_CONCERN",
		"RetryAttempts":          "RETRY_ATTEMPTS",
		"RetryInterval":          "RETRY_INTERVAL",
	}
	typ := reflect.TypeFor[forgemongo.Config]()
	for fname, tag := range want {
		f, ok := typ.FieldByName(fname)
		require.Truef(t, ok, "field %s missing", fname)
		assert.Equalf(t, tag, f.Tag.Get("env"), "field %s env tag", fname)
	}
}

func TestParseConcerns(t *testing.T) {
	// The parsers are unexported; assert their behavior through Validate, which is
	// their only caller and surfaces every rejection as ErrInvalidConfig. Empty
	// strings must be accepted (driver default), valid names accepted, junk rejected.
	withURI := func(mut func(*forgemongo.Config)) forgemongo.Config {
		c := forgemongo.DefaultConfig()
		c.URI = "mongodb://127.0.0.1:27017"
		c.Database = "forge_test"
		mut(&c)
		return c
	}

	// Empty concerns are valid (driver defaults).
	require.NoError(t, withURI(func(*forgemongo.Config) {}).Validate())

	readPrefs := []string{"primary", "primaryPreferred", "secondary", "secondaryPreferred", "nearest"}
	for _, rp := range readPrefs {
		require.NoErrorf(t, withURI(func(c *forgemongo.Config) { c.ReadPreference = rp }).Validate(), "ReadPreference %q", rp)
	}
	readConcerns := []string{"local", "majority", "available", "linearizable", "snapshot"}
	for _, rc := range readConcerns {
		require.NoErrorf(t, withURI(func(c *forgemongo.Config) { c.ReadConcern = rc }).Validate(), "ReadConcern %q", rc)
	}
	writeConcerns := []string{"majority", "journaled", "unacknowledged", "0", "1", "2"}
	for _, wc := range writeConcerns {
		require.NoErrorf(t, withURI(func(c *forgemongo.Config) { c.WriteConcern = wc }).Validate(), "WriteConcern %q", wc)
	}

	// Junk is rejected for each concern.
	require.ErrorIs(t, withURI(func(c *forgemongo.Config) { c.ReadPreference = "nope" }).Validate(), forgemongo.ErrInvalidConfig)
	require.ErrorIs(t, withURI(func(c *forgemongo.Config) { c.ReadConcern = "nope" }).Validate(), forgemongo.ErrInvalidConfig)
	require.ErrorIs(t, withURI(func(c *forgemongo.Config) { c.WriteConcern = "nope" }).Validate(), forgemongo.ErrInvalidConfig)
	require.ErrorIs(t, withURI(func(c *forgemongo.Config) { c.WriteConcern = "-1" }).Validate(), forgemongo.ErrInvalidConfig)
}
