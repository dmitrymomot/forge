package queue_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/async/queue"
)

func TestDefaultConfig(t *testing.T) {
	t.Parallel()
	cfg := queue.DefaultConfig()
	assert.Equal(t, 10, cfg.Concurrency)
	assert.Equal(t, time.Second, cfg.PollInterval)
	assert.Equal(t, 30*time.Second, cfg.Lease)
	assert.Equal(t, 25, cfg.MaxAttempts)
	assert.Equal(t, 0, cfg.ClaimBatch)
	require.NoError(t, cfg.Validate())
}

func TestConfig_ValidateRejects(t *testing.T) {
	t.Parallel()
	cases := []func(*queue.Config){
		func(c *queue.Config) { c.Concurrency = 0 },
		func(c *queue.Config) { c.PollInterval = 0 },
		func(c *queue.Config) { c.Lease = 0 },
		func(c *queue.Config) { c.MaxAttempts = 0 },
		func(c *queue.Config) { c.ClaimBatch = -1 },
	}
	for _, mutate := range cases {
		cfg := queue.DefaultConfig()
		mutate(&cfg)
		assert.ErrorIs(t, cfg.Validate(), queue.ErrInvalidConfig)
	}
}

func TestConfig_EveryFieldHasEnvTag(t *testing.T) {
	t.Parallel()
	typ := reflect.TypeFor[queue.Config]()
	for i := range typ.NumField() {
		f := typ.Field(i)
		assert.NotEmpty(t, f.Tag.Get("env"), "field %s missing env tag", f.Name)
	}
}
